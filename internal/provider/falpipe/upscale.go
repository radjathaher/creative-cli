// Package falpipe holds the fal.ai-backed pipelines (video upscaling, VEED
// captioning, and Gemini video analysis) layered on the shared fal client.
package falpipe

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/fal"
)

var upscaleEndpoints = map[string]string{
	"bytedance": "fal-ai/bytedance-upscaler/upscale/video",
	"topaz":     "fal-ai/topaz/upscale/video",
	"flashvsr":  "fal-ai/flashvsr/upscale/video",
	"seedvr":    "fal-ai/seedvr/upscale/video",
}

// UpscaleOpts configures a video upscale job.
type UpscaleOpts struct {
	Input            string
	Model            string // bytedance | topaz | flashvsr | seedvr
	Target           string // 720p | 1080p | 2k | 4k
	Fps              int    // 30 | 60
	Out              string
	NoWait           bool
	PollIntervalSecs int
	MaxWaitSecs      int
	// ByteDance tuning
	BytedancePreset   string
	BytedanceTier     string
	BytedanceFidelity string
	TopazModel        string
}

// Upscale resolves the input, submits the fal job, and (unless NoWait) polls,
// downloads, and returns the result envelope.
func Upscale(o UpscaleOpts, pretty, raw bool) (*core.Envelope, json.RawMessage, error) {
	endpoint, ok := upscaleEndpoints[o.Model]
	if !ok {
		return nil, nil, fmt.Errorf("unknown upscale model %q (want bytedance|topaz|flashvsr|seedvr)", o.Model)
	}
	if !o.NoWait && o.Out == "" {
		return nil, nil, fmt.Errorf("--out is required unless --no-wait is set")
	}
	client, err := fal.New()
	if err != nil {
		return nil, nil, err
	}
	started := time.Now()
	input, err := client.ResolveInput(o.Input)
	if err != nil {
		return nil, nil, err
	}
	body, err := upscalePayload(o, input.ResolvedURL)
	if err != nil {
		return nil, nil, err
	}
	core.Progress("upscaling with fal %s -> %s", o.Model, o.Target)
	qref, qraw, err := client.Submit(endpoint, body)
	if err != nil {
		return nil, qraw, err
	}

	env := &core.Envelope{
		Provider:  "fal",
		Endpoint:  endpoint,
		RequestID: qref.RequestID,
		Model:     o.Model,
		Input:     &input,
		Cost:      estimateCost(o.Model, o.Target, 0),
	}
	if o.NoWait {
		env.Output = "queued"
		env.ElapsedSeconds = roundSecs(started)
		return env, qraw, nil
	}

	core.Progress("queued %s; polling", qref.RequestID)
	result, resraw, err := client.Wait(qref, o.MaxWaitSecs, o.PollIntervalSecs)
	if err != nil {
		return nil, resraw, err
	}
	url, ok := fal.ExtractMediaURL(result)
	if !ok {
		return nil, resraw, fmt.Errorf("fal result missing a downloadable video url")
	}
	if _, err := client.Download(url, o.Out); err != nil {
		return nil, resraw, err
	}
	env.Output = url
	env.Out = o.Out
	env.ElapsedSeconds = roundSecs(started)
	core.Progress("done in %.3fs", env.ElapsedSeconds)
	return env, resraw, nil
}

func upscalePayload(o UpscaleOpts, videoURL string) (map[string]any, error) {
	switch o.Model {
	case "bytedance":
		if o.Target == "720p" {
			return nil, fmt.Errorf("bytedance fal upscaler supports --target 1080p|2k|4k, not 720p")
		}
		return map[string]any{
			"video_url":          videoURL,
			"target_fps":         fmt.Sprintf("%dfps", o.Fps),
			"target_resolution":  bytedanceTarget(o.Target),
			"enhancement_preset": orDefault(o.BytedancePreset, "aigc"),
			"fidelity":           orDefault(o.BytedanceFidelity, "high"),
			"enhancement_tier":   orDefault(o.BytedanceTier, "standard"),
		}, nil
	case "topaz":
		return map[string]any{
			"video_url":      videoURL,
			"upscale_factor": factorForTarget(o.Target),
			"model":          orDefault(o.TopazModel, "Proteus"),
			"target_fps":     o.Fps,
			"H264_output":    true,
		}, nil
	case "flashvsr":
		return map[string]any{
			"video_url":         videoURL,
			"upscale_factor":    factorForTarget(o.Target),
			"output_format":     "X264 (.mp4)",
			"output_quality":    "high",
			"output_write_mode": "balanced",
			"preserve_audio":    true,
		}, nil
	case "seedvr":
		return map[string]any{
			"video_url":         videoURL,
			"upscale_mode":      "target",
			"target_resolution": seedvrTarget(o.Target),
			"output_format":     "X264 (.mp4)",
			"output_quality":    "high",
			"output_write_mode": "balanced",
		}, nil
	}
	return nil, fmt.Errorf("unknown model %q", o.Model)
}

func bytedanceTarget(target string) string {
	switch target {
	case "2k":
		return "2k"
	case "4k":
		return "4k"
	default:
		return "1080p"
	}
}

func seedvrTarget(target string) string {
	switch target {
	case "720p":
		return "720p"
	case "1080p":
		return "1080p"
	case "2k":
		return "1440p"
	case "4k":
		return "2160p"
	default:
		return "1080p"
	}
}

func factorForTarget(target string) float64 {
	switch target {
	case "720p":
		return 1.5
	case "2k":
		return 3.0
	case "4k":
		return 4.0
	default:
		return 2.0
	}
}

// estimateCost mirrors upscale-cli's rate card; returns nil when unpriced.
func estimateCost(model, target string, seconds float64) *float64 {
	if seconds <= 0 {
		return nil
	}
	var rate float64
	switch model {
	case "bytedance":
		switch target {
		case "1080p":
			rate = 0.0072
		case "2k":
			rate = 0.0144
		case "4k":
			rate = 0.0288
		default:
			return nil
		}
	case "topaz":
		switch target {
		case "720p":
			rate = 0.0100
		case "1080p":
			rate = 0.0200
		default:
			rate = 0.0800
		}
	case "flashvsr":
		rate = megapixels(target) * 30.0 * 0.0005
	case "seedvr":
		rate = megapixels(target) * 30.0 * 0.0010
	default:
		return nil
	}
	return core.F(rate * seconds)
}

func megapixels(target string) float64 {
	switch target {
	case "720p":
		return 720.0 * 1280.0 / 1_000_000.0
	case "2k":
		return 1440.0 * 2560.0 / 1_000_000.0
	case "4k":
		return 2160.0 * 3840.0 / 1_000_000.0
	default:
		return 1080.0 * 1920.0 / 1_000_000.0
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func roundSecs(start time.Time) float64 {
	return float64(int64(time.Since(start).Seconds()*1000)) / 1000
}
