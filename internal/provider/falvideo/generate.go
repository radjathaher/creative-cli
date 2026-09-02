// Package falvideo generates videos through fal-hosted video models.
package falvideo

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/fal"
)

const omniBaseEndpoint = "google/gemini-omni-flash/v1.1"

// Generate submits a Gemini Omni Flash job and optionally waits for its video.
func Generate(o Options) (*core.Envelope, json.RawMessage, error) {
	if err := validate(o); err != nil {
		return nil, nil, err
	}
	client, err := fal.New()
	if err != nil {
		return nil, nil, err
	}
	started := time.Now()
	endpoint, body, kind, err := buildRequest(client, o)
	if err != nil {
		return nil, nil, err
	}

	core.Progress("submitting fal %s (%s %s)", o.Model, o.Resolution, o.AspectRatio)
	qref, qraw, err := client.Submit(endpoint, body)
	if err != nil {
		return nil, qraw, err
	}
	cost := float64(o.Duration) * omniRate(o.Resolution)
	env := &core.Envelope{
		Provider:  "fal",
		Endpoint:  endpoint,
		RequestID: qref.RequestID,
		Model:     o.Model,
		Input:     &core.InputInfo{Kind: kind, Source: o.Prompt},
		Cost:      core.F(cost),
	}
	if o.NoWait {
		env.Output = "queued"
		env.ElapsedSeconds = elapsed(started)
		return env, qraw, nil
	}

	core.Progress("queued %s; polling", qref.RequestID)
	result, raw, err := client.Wait(qref, o.MaxWaitSecs, o.PollIntervalSecs)
	if err != nil {
		return nil, raw, err
	}
	url, ok := fal.ExtractMediaURL(result)
	if !ok {
		return nil, raw, fmt.Errorf("fal result missing a downloadable video url")
	}
	if _, err := client.Download(url, o.Out); err != nil {
		return nil, raw, err
	}
	env.Output = url
	env.Out = o.Out
	env.ElapsedSeconds = elapsed(started)
	core.Progress("done in %.3fs", env.ElapsedSeconds)
	return env, raw, nil
}

func validate(o Options) error {
	if o.Model != "omni-1.1-flash" {
		return fmt.Errorf("unknown fal video model %q (want omni-1.1-flash)", o.Model)
	}
	if !o.NoWait && o.Out == "" {
		return fmt.Errorf("--out is required unless --no-wait is set")
	}
	if o.FirstFrame != "" && len(o.Images) > 0 {
		return fmt.Errorf("--first-frame cannot be combined with --image")
	}
	if o.LastFrame != "" && o.FirstFrame == "" {
		return fmt.Errorf("--last-frame requires --first-frame")
	}
	if len(o.Audios) > 0 {
		return fmt.Errorf("fal omni-1.1-flash does not accept --audio references")
	}
	if !o.GenerateAudio {
		return fmt.Errorf("fal omni-1.1-flash always generates synchronized audio")
	}
	if o.Duration < 3 || o.Duration > 10 {
		return fmt.Errorf("fal omni-1.1-flash requires --duration-seconds between 3 and 10")
	}
	if !oneOf(o.Resolution, "360p", "720p", "1080p", "4k") {
		return fmt.Errorf("fal omni-1.1-flash requires --resolution 360p|720p|1080p|4k")
	}
	if !oneOf(o.AspectRatio, "16:9", "9:16") {
		return fmt.Errorf("fal omni-1.1-flash requires --aspect-ratio 16:9|9:16")
	}
	if len(o.Images) > 10 {
		return fmt.Errorf("fal omni-1.1-flash accepts at most 10 --image references")
	}
	if len(o.Videos) > 3 {
		return fmt.Errorf("fal omni-1.1-flash accepts at most 3 --video references")
	}
	return nil
}

func buildRequest(client *fal.Client, o Options) (string, map[string]any, string, error) {
	common := map[string]any{
		"prompt":       o.Prompt,
		"aspect_ratio": o.AspectRatio,
		"resolution":   o.Resolution,
		"duration":     o.Duration,
	}
	if o.FirstFrame != "" {
		first, err := resolve(client, o.FirstFrame)
		if err != nil {
			return "", nil, "", err
		}
		common["image_url"] = first
		if o.LastFrame != "" {
			last, err := resolve(client, o.LastFrame)
			if err != nil {
				return "", nil, "", err
			}
			common["end_image_url"] = last
		}
		return omniBaseEndpoint + "/image-to-video", common, "image", nil
	}
	if len(o.Images) > 0 || len(o.Videos) > 0 {
		images, err := resolveAll(client, o.Images)
		if err != nil {
			return "", nil, "", err
		}
		videos, err := resolveAll(client, o.Videos)
		if err != nil {
			return "", nil, "", err
		}
		if len(images) > 0 {
			common["image_urls"] = images
		}
		if len(videos) > 0 {
			common["reference_video_urls"] = videos
		}
		return omniBaseEndpoint + "/reference-to-video", common, referenceKind(o), nil
	}
	return omniBaseEndpoint + "/text-to-video", common, "prompt", nil
}

func resolveAll(client *fal.Client, refs []string) ([]string, error) {
	resolved := make([]string, 0, len(refs))
	for _, ref := range refs {
		url, err := resolve(client, ref)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, url)
	}
	return resolved, nil
}

func resolve(client *fal.Client, ref string) (string, error) {
	input, err := client.ResolveInput(ref)
	if err != nil {
		return "", err
	}
	return input.ResolvedURL, nil
}

func referenceKind(o Options) string {
	if len(o.Images) > 0 {
		return "image"
	}
	return "video"
}

func omniRate(resolution string) float64 {
	switch resolution {
	case "360p":
		return 0.03
	case "1080p":
		return 0.15
	case "4k":
		return 0.30
	default:
		return 0.10
	}
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func elapsed(start time.Time) float64 {
	return math.Round(time.Since(start).Seconds()*1000) / 1000
}
