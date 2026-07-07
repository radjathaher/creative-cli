// Package segmind wraps Segmind's async video API, currently the Seedance 2.0
// family. It mirrors the shape of the falpipe package: resolve local references
// by uploading them, submit a job, poll to completion, and download the result.
package segmind

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
)

const (
	defaultBaseURL    = "https://api.segmind.com"
	defaultStorageURL = "https://workflows-api.segmind.com"
)

// modelSlugs maps the CLI-facing model names to Segmind endpoint slugs.
var modelSlugs = map[string]string{
	"mini":     "seedance-2.0-mini",
	"fast":     "seedance-2.0-fast",
	"standard": "seedance-2.0",
}

// GenerateOpts configures a Seedance video generation job.
type GenerateOpts struct {
	Prompt           string
	Model            string // mini | fast | standard
	Images           []string
	Videos           []string
	Audios           []string
	FirstFrame       string
	LastFrame        string
	Duration         int
	Resolution       string // 480p | 720p | 1080p | 4k
	AspectRatio      string
	GenerateAudio    bool
	Seed             int
	Out              string
	NoWait           bool
	PollIntervalSecs int
	MaxWaitSecs      int
}

// Generate uploads any local references, submits the Seedance job, and (unless
// NoWait) polls, downloads, and returns the result envelope.
func Generate(o GenerateOpts, pretty, raw bool) (*core.Envelope, json.RawMessage, error) {
	slug, ok := modelSlugs[o.Model]
	if !ok {
		return nil, nil, fmt.Errorf("unknown model %q (want mini|fast|standard)", o.Model)
	}
	if !o.NoWait && o.Out == "" {
		return nil, nil, fmt.Errorf("--out is required unless --no-wait is set")
	}
	if o.FirstFrame != "" && len(o.Images) > 0 {
		return nil, nil, fmt.Errorf("--first-frame cannot be combined with --image; use one image-to-video mode")
	}
	if o.LastFrame != "" && o.FirstFrame == "" {
		return nil, nil, fmt.Errorf("--last-frame requires --first-frame")
	}
	c, err := newClient()
	if err != nil {
		return nil, nil, err
	}
	started := time.Now()

	images, err := c.uploadRefs(o.Images)
	if err != nil {
		return nil, nil, err
	}
	videos, err := c.uploadRefs(o.Videos)
	if err != nil {
		return nil, nil, err
	}
	audios, err := c.uploadRefs(o.Audios)
	if err != nil {
		return nil, nil, err
	}
	firstFrame, err := c.uploadOptional(o.FirstFrame)
	if err != nil {
		return nil, nil, err
	}
	lastFrame, err := c.uploadOptional(o.LastFrame)
	if err != nil {
		return nil, nil, err
	}

	body := map[string]any{
		"prompt":           o.Prompt,
		"reference_images": images,
		"reference_videos": videos,
		"reference_audios": audios,
		"duration":         o.Duration,
		"resolution":       o.Resolution,
		"aspect_ratio":     o.AspectRatio,
		"generate_audio":   o.GenerateAudio,
		"seed":             o.Seed,
	}
	if firstFrame != "" {
		body["first_frame_url"] = firstFrame
	}
	if lastFrame != "" {
		body["last_frame_url"] = lastFrame
	}

	core.Progress("submitting seedance %s (%s %s)", o.Model, o.Resolution, o.AspectRatio)
	submitURL := fmt.Sprintf("%s/v2/%s", strings.TrimRight(c.baseURL, "/"), slug)
	parsed, sraw, err := c.postJSON(submitURL, body)
	if err != nil {
		return nil, sraw, err
	}
	requestID := asString(parsed["request_id"])

	env := &core.Envelope{
		Provider:  "segmind",
		Endpoint:  slug,
		RequestID: requestID,
		Model:     o.Model,
		Input:     &core.InputInfo{Kind: inputKind(o), Source: o.Prompt},
	}
	if o.NoWait {
		env.Output = "queued"
		env.ElapsedSeconds = roundSecs(started)
		return env, sraw, nil
	}

	core.Progress("queued %s; polling", requestID)
	result, rraw, err := c.wait(requestID, o.MaxWaitSecs, o.PollIntervalSecs)
	if err != nil {
		return nil, rraw, err
	}
	url, ok := extractOutputURL(result)
	if !ok {
		return nil, rraw, fmt.Errorf("segmind result missing a downloadable video url")
	}
	if _, err := core.Download(url, o.Out, nil); err != nil {
		return nil, rraw, err
	}
	env.Output = url
	env.Out = o.Out
	env.ElapsedSeconds = roundSecs(started)
	core.Progress("done in %.3fs", env.ElapsedSeconds)
	return env, rraw, nil
}

// client talks to Segmind's job and storage endpoints with an x-api-key credential.
type client struct {
	key        string
	baseURL    string
	storageURL string
}

func newClient() (*client, error) {
	key, err := core.ReadSecret("SEGMIND_API_KEY")
	if err != nil {
		return nil, err
	}
	return &client{
		key:        key,
		baseURL:    envOr("SEGMIND_API_URL", defaultBaseURL),
		storageURL: envOr("SEGMIND_STORAGE_URL", defaultStorageURL),
	}, nil
}

func envOr(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

func (c *client) statusURL(id string) string {
	return fmt.Sprintf("%s/v2/requests/%s/status", strings.TrimRight(c.baseURL, "/"), id)
}

func (c *client) responseURL(id string) string {
	return fmt.Sprintf("%s/v2/requests/%s", strings.TrimRight(c.baseURL, "/"), id)
}

// wait polls a Segmind job until COMPLETED (returns the result) or FAILED
// (returns an error), bounded by maxWaitSecs.
func (c *client) wait(id string, maxWaitSecs, pollIntervalSecs int) (map[string]any, json.RawMessage, error) {
	deadline := time.Now().Add(time.Duration(maxWaitSecs) * time.Second)
	interval := time.Duration(max(pollIntervalSecs, 1)) * time.Second
	for {
		status, _, err := c.get(c.statusURL(id))
		if err != nil {
			return nil, nil, err
		}
		switch strings.ToUpper(asString(status["status"])) {
		case "COMPLETED":
			return c.get(c.responseURL(id))
		case "FAILED":
			raw, _ := json.Marshal(status)
			return nil, nil, fmt.Errorf("segmind task failed: %s", string(raw))
		default:
			if time.Now().After(deadline) {
				return nil, nil, fmt.Errorf("timeout waiting for %s", id)
			}
			time.Sleep(interval)
		}
	}
}

func (c *client) postJSON(url string, body any) (map[string]any, json.RawMessage, error) {
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	return core.DecodeJSON(resp)
}

func (c *client) get(url string) (map[string]any, json.RawMessage, error) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	return core.DecodeJSON(resp)
}

func (c *client) uploadRefs(refs []string) ([]string, error) {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		u, err := c.uploadOne(r)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func (c *client) uploadOptional(ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	return c.uploadOne(ref)
}

// uploadOne resolves a reference to a Segmind-hosted URL: remote refs pass
// through untouched, local files are uploaded to storage as data URLs first.
func (c *client) uploadOne(ref string) (string, error) {
	if core.IsRemoteRef(ref) {
		return ref, nil
	}
	dataURL, err := fileToDataURL(ref)
	if err != nil {
		return "", err
	}
	core.Progress("uploading %s (%s)", ref, core.MIMEForPath(ref))
	url := strings.TrimRight(c.storageURL, "/") + "/upload-asset"
	parsed, _, err := c.postJSON(url, map[string]any{"data_urls": []string{dataURL}})
	if err != nil {
		return "", err
	}
	if u := firstUploadedURL(parsed); u != "" {
		return u, nil
	}
	return "", fmt.Errorf("upload response missing file url for %s", ref)
}

func fileToDataURL(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return fmt.Sprintf("data:%s;base64,%s", core.MIMEForPath(path), base64.StdEncoding.EncodeToString(b)), nil
}

func firstUploadedURL(m map[string]any) string {
	if arr, ok := m["file_urls"].([]any); ok {
		for _, v := range arr {
			if s := asString(v); s != "" {
				return s
			}
		}
	}
	return asString(m["url"])
}

// extractOutputURL prefers the result's "output" subtree, then scans the whole
// object for a downloadable video URL.
func extractOutputURL(result map[string]any) (string, bool) {
	if output, ok := result["output"]; ok {
		if u, ok := findMediaURL(output); ok {
			return u, true
		}
	}
	return findMediaURL(result)
}

// findMediaURL walks a decoded result looking for a string that ends in a video
// extension, checking the common Segmind shapes first before a recursive scan.
func findMediaURL(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		if isDownloadable(t) {
			return t, true
		}
	case map[string]any:
		for _, key := range []string{"video", "video_url", "url", "output", "result", "file_url"} {
			if val, ok := t[key]; ok {
				if u, ok := findMediaURL(val); ok {
					return u, true
				}
			}
		}
		for _, val := range t {
			if u, ok := findMediaURL(val); ok {
				return u, true
			}
		}
	case []any:
		for _, val := range t {
			if u, ok := findMediaURL(val); ok {
				return u, true
			}
		}
	}
	return "", false
}

func isDownloadable(s string) bool {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return false
	}
	lower := strings.ToLower(s)
	for _, ext := range []string{".mp4", ".mov", ".webm", ".mkv", ".avi"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
}

func inputKind(o GenerateOpts) string {
	switch {
	case len(o.Images) > 0 || o.FirstFrame != "":
		return "image"
	case len(o.Videos) > 0:
		return "video"
	default:
		return "prompt"
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func roundSecs(start time.Time) float64 {
	return math.Round(time.Since(start).Seconds()*1000) / 1000
}
