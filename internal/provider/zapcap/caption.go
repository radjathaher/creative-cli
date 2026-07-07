// Package zapcap wraps the ZapCap subtitle-rendering API used by the caption
// verb's zapcap engine. Unlike the fal-backed engines it manages its own upload,
// task creation, and polling loop against https://api.zapcap.ai.
package zapcap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
)

const defaultBaseURL = "https://api.zapcap.ai"

// baseURL honours ZAPCAP_API_URL so the CLI can target a proxy or staging host.
func baseURL() string {
	if v := strings.TrimSpace(os.Getenv("ZAPCAP_API_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultBaseURL
}

// Client talks to the ZapCap API with an x-api-key credential.
type Client struct {
	key  string
	base string
}

// New builds a Client, reading ZAPCAP_API_KEY from the environment or /run/secrets.
func New() (*Client, error) {
	key, err := core.ReadSecret("ZAPCAP_API_KEY")
	if err != nil {
		return nil, err
	}
	return &Client{key: key, base: baseURL()}, nil
}

// RenderOpts configures a ZapCap caption-burning job.
type RenderOpts struct {
	Input            string
	TemplateID       string // required caption template id
	Language         string // source language, defaults to en
	Out              string
	NoWait           bool
	PollIntervalSecs int
	MaxWaitSecs      int
}

// Render uploads or imports the video, creates a captioning task, and (unless
// NoWait) polls until completion, downloads the rendered video, and returns the
// result envelope.
func Render(o RenderOpts, pretty, raw bool) (*core.Envelope, json.RawMessage, error) {
	if o.TemplateID == "" {
		return nil, nil, fmt.Errorf("--template-id is required for the zapcap provider")
	}
	if !o.NoWait && o.Out == "" {
		return nil, nil, fmt.Errorf("--out is required unless --no-wait is set")
	}
	client, err := New()
	if err != nil {
		return nil, nil, err
	}
	started := time.Now()

	var input core.InputInfo
	var videoID string
	if core.IsRemoteRef(o.Input) {
		core.Progress("importing video url into zapcap")
		videoID, err = client.importVideoURL(o.Input)
		input = core.InputInfo{Kind: "url", Source: o.Input, ResolvedURL: o.Input}
	} else {
		core.Progress("uploading %s to zapcap", o.Input)
		videoID, err = client.uploadVideo(o.Input)
		input = core.InputInfo{Kind: "file", Source: o.Input}
	}
	if err != nil {
		return nil, nil, err
	}

	language := o.Language
	if language == "" {
		language = "en"
	}
	core.Progress("creating zapcap task (template %s)", o.TemplateID)
	taskID, err := client.createTask(videoID, o.TemplateID, language)
	if err != nil {
		return nil, nil, err
	}

	env := &core.Envelope{
		Provider:  "zapcap",
		Endpoint:  fmt.Sprintf("/videos/%s/task", videoID),
		RequestID: taskID,
		Input:     &input,
	}
	if o.NoWait {
		env.Output = "queued"
		env.ElapsedSeconds = roundSecs(started)
		return env, nil, nil
	}

	core.Progress("queued task %s; polling", taskID)
	result, resraw, err := client.waitTask(videoID, taskID, o.MaxWaitSecs, o.PollIntervalSecs)
	if err != nil {
		return nil, resraw, err
	}
	url, ok := extractDownloadURL(result)
	if !ok {
		return nil, resraw, fmt.Errorf("zapcap task missing downloadUrl")
	}
	if _, err := core.Download(url, o.Out, nil); err != nil {
		return nil, resraw, err
	}
	env.Output = url
	env.Out = o.Out
	env.ElapsedSeconds = roundSecs(started)
	core.Progress("done in %.3fs", env.ElapsedSeconds)
	return env, resraw, nil
}

// uploadVideo posts a local file to /videos as multipart form data and returns
// the created video id.
func (c *Client) uploadVideo(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(data); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	req, _ := http.NewRequest(http.MethodPost, c.base+"/videos", &buf)
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("User-Agent", core.UserAgent())
	parsed, _, err := c.do(req)
	if err != nil {
		return "", err
	}
	return extractID(parsed, "upload response missing id")
}

// importVideoURL registers a public video url via /videos/url and returns its id.
func (c *Client) importVideoURL(videoURL string) (string, error) {
	body, _ := json.Marshal(map[string]any{"url": videoURL})
	req, _ := http.NewRequest(http.MethodPost, c.base+"/videos/url", bytes.NewReader(body))
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", core.UserAgent())
	parsed, _, err := c.do(req)
	if err != nil {
		return "", err
	}
	return extractID(parsed, "url import response missing id")
}

// createTask starts a captioning task for a video and returns its task id. The
// task auto-approves so the render proceeds without a manual review step.
func (c *Client) createTask(videoID, templateID, language string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"templateId":  templateID,
		"autoApprove": true,
		"language":    language,
	})
	url := fmt.Sprintf("%s/videos/%s/task", c.base, videoID)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", core.UserAgent())
	parsed, _, err := c.do(req)
	if err != nil {
		return "", err
	}
	if id := asString(parsed["taskId"]); id != "" {
		return id, nil
	}
	if id := asString(parsed["id"]); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("task response missing taskId")
}

// taskStatus fetches the current status object for a task.
func (c *Client) taskStatus(videoID, taskID string) (map[string]any, json.RawMessage, error) {
	url := fmt.Sprintf("%s/videos/%s/task/%s", c.base, videoID, taskID)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("User-Agent", core.UserAgent())
	return c.do(req)
}

// waitTask polls a task until it reports a terminal status, returning the final
// status object on success or an error on failure/timeout.
func (c *Client) waitTask(videoID, taskID string, maxWaitSecs, pollIntervalSecs int) (map[string]any, json.RawMessage, error) {
	deadline := time.Now().Add(time.Duration(maxWaitSecs) * time.Second)
	interval := time.Duration(pollIntervalSecs)
	if interval < 1 {
		interval = 1
	}
	interval *= time.Second
	for {
		status, raw, err := c.taskStatus(videoID, taskID)
		if err != nil {
			return nil, raw, err
		}
		switch strings.ToLower(asString(status["status"])) {
		case "completed", "complete", "succeeded", "success":
			return status, raw, nil
		case "failed", "error", "errored", "canceled", "cancelled":
			return nil, raw, fmt.Errorf("zapcap task failed: %s", string(raw))
		default:
			if time.Now().After(deadline) {
				return nil, raw, fmt.Errorf("timeout waiting for task %s", taskID)
			}
			time.Sleep(interval)
		}
	}
}

func (c *Client) do(req *http.Request) (map[string]any, json.RawMessage, error) {
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	return core.DecodeJSON(resp)
}

func extractID(m map[string]any, missingMsg string) (string, error) {
	for _, key := range []string{"id", "videoId", "video_id"} {
		if s := asString(m[key]); s != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("%s", missingMsg)
}

func extractDownloadURL(m map[string]any) (string, bool) {
	for _, key := range []string{"downloadUrl", "download_url", "url"} {
		if s := asString(m[key]); s != "" {
			return s, true
		}
	}
	return "", false
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func roundSecs(start time.Time) float64 {
	return math.Round(time.Since(start).Seconds()*1000) / 1000
}
