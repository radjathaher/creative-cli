// Package falpipe holds the fal.ai-backed pipelines (video upscaling, VEED
// captioning, and Gemini video analysis) layered on the shared fal client.
package falpipe

import (
	"encoding/json"
	"fmt"
	"math"
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
	TopazFactor        float64 // explicit upscale factor; 0 derives it from --target
	TopazRecoverDetail float64 // 0-1; negative leaves the model default
	BytedancePreset    string
	BytedanceTier      string
	BytedanceFidelity  string
	TopazModel         string
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
	if o.Model == "topaz" {
		if o.Fps < 16 || o.Fps > 60 {
			return nil, nil, fmt.Errorf("--fps must be 16-60 for topaz")
		}
	} else if o.Fps != 30 && o.Fps != 60 {
		return nil, nil, fmt.Errorf("--fps must be 30 or 60")
	}
	if o.Target != "720p" && o.Target != "1080p" && o.Target != "2k" && o.Target != "4k" {
		return nil, nil, fmt.Errorf("unknown target %q (want 720p|1080p|2k|4k)", o.Target)
	}
	metadata, err := core.ProbeVideo(o.Input)
	if err != nil {
		return nil, nil, err
	}
	factor := 0.0
	if o.Model == "topaz" || o.Model == "flashvsr" {
		factor, err = factorForTarget(o.Target, metadata.Width, metadata.Height)
		if err != nil {
			return nil, nil, err
		}
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
	body, err := upscalePayload(o, input.ResolvedURL, factor)
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
		Cost:      estimateCost(o, metadata.DurationSeconds),
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

func upscalePayload(o UpscaleOpts, videoURL string, factor float64) (map[string]any, error) {
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
		if o.TopazFactor > 0 {
			factor = o.TopazFactor
		}
		body := map[string]any{
			"video_url":      videoURL,
			"upscale_factor": factor,
			"model":          orDefault(o.TopazModel, "Proteus"),
			"target_fps":     o.Fps,
			"H264_output":    true,
		}
		if o.TopazRecoverDetail >= 0 {
			body["recover_detail"] = o.TopazRecoverDetail
		}
		return body, nil
	case "flashvsr":
		return map[string]any{
			"video_url":         videoURL,
			"upscale_factor":    factor,
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

func factorForTarget(target string, width, height int) (float64, error) {
	shortEdge := min(width, height)
	if shortEdge <= 0 {
		return 0, fmt.Errorf("input video dimensions must be positive")
	}
	var targetEdge int
	switch target {
	case "720p":
		targetEdge = 720
	case "1080p":
		targetEdge = 1080
	case "2k":
		targetEdge = 1440
	case "4k":
		targetEdge = 2160
	default:
		return 0, fmt.Errorf("unknown target %q (want 720p|1080p|2k|4k)", target)
	}
	factor := float64(targetEdge) / float64(shortEdge)
	if factor < 1 {
		return 0, fmt.Errorf("--target %s is smaller than the %dx%d input", target, width, height)
	}
	if factor > 4 {
		return 0, fmt.Errorf("--target %s needs %.3fx upscale; fal supports at most 4x", target, factor)
	}
	return math.Round(factor*1000) / 1000, nil
}

// estimateCost mirrors the provider rate cards; returns nil when unpriced.
func estimateCost(o UpscaleOpts, seconds float64) *float64 {
	if seconds <= 0 {
		return nil
	}
	var rate float64
	switch o.Model {
	case "bytedance":
		switch o.Target {
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
		switch o.Target {
		case "720p":
			rate = 0.0100
		case "1080p":
			rate = 0.0200
		default:
			rate = 0.0800
		}
		if o.Fps == 60 {
			rate *= 2
		}
		if o.TopazModel == "Gaia 2" {
			rate /= 2
		}
	case "flashvsr":
		rate = megapixels(o.Target) * float64(o.Fps) * 0.0005
	case "seedvr":
		rate = megapixels(o.Target) * float64(o.Fps) * 0.0010
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
