// Package openai adapts the synchronous OpenAI-shaped routes (OpenAI direct and
// Codex-LB) to the job engine: images, speech, and transcription. These calls
// answer in one request, so Submit returns the outputs immediately.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/radjathaher/creative-cli/internal/adapter"
	"github.com/radjathaher/creative-cli/internal/job"
	provider "github.com/radjathaher/creative-cli/internal/provider/openai"
	"github.com/radjathaher/creative-cli/internal/registry"
)

// Adapter serves transports "openai" and "codex-lb".
type Adapter struct{}

type payload struct {
	Backend       string   `json:"backend"`
	Op            string   `json:"op"`
	Model         string   `json:"model"`
	Prompt        string   `json:"prompt,omitempty"`
	Size          string   `json:"size,omitempty"`
	Quality       string   `json:"quality,omitempty"`
	Background    string   `json:"background,omitempty"`
	OutputFormat  string   `json:"output_format,omitempty"`
	N             int      `json:"n,omitempty"`
	Refs          []string `json:"refs,omitempty"`
	File          string   `json:"file,omitempty"`
	Format        string   `json:"format,omitempty"`
	Language      string   `json:"language,omitempty"`
	Granularities []string `json:"timestamp_granularities,omitempty"`
}

// Prepare maps the request. References stay as paths or URLs; the provider
// package uploads them in the multipart edit request.
func (Adapter) Prepare(_ context.Context, r registry.Route, req job.Request, _ bool) (json.RawMessage, error) {
	p := payload{Backend: r.Provider, Op: r.Op, Model: r.ModelID, Prompt: req.Prompt}
	switch r.Op {
	case registry.OpImageT2I, registry.OpImageEdit:
		if p.Prompt == "" {
			return nil, fmt.Errorf("--prompt is required")
		}
		p.Refs = req.Images
		if r.Op == registry.OpImageEdit && len(p.Refs) == 0 {
			return nil, fmt.Errorf("image.edit needs at least one --image")
		}
		p.Size = str(req.Params, "size", imageSize(req.Resolution, req.Aspect))
		p.Quality = req.Quality
		p.Background = str(req.Params, "background", "")
		p.OutputFormat = str(req.Params, "output_format", "")
		p.N = max(req.Count, 1)
	case registry.OpSTT:
		if req.Input == "" {
			return nil, fmt.Errorf("stt needs an input file")
		}
		p.File, p.Language = req.Input, str(req.Params, "language", "")
		p.Format = str(req.Params, "format", "verbose_json")
		if g := str(req.Params, "timestamps", ""); g != "" {
			p.Granularities = strings.Split(g, ",")
		}
	default:
		return nil, fmt.Errorf("op %s is not supported on %s", r.Op, r.Provider)
	}
	return json.Marshal(p)
}

// Submit performs the call and returns the result immediately.
func (Adapter) Submit(ctx context.Context, r registry.Route, raw json.RawMessage) (job.Submission, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return job.Submission{}, err
	}
	switch p.Op {
	case registry.OpSTT:
		res, err := provider.Transcribe(provider.TranscribeParams{
			File: p.File, Model: p.Model, Format: p.Format, Language: p.Language,
			Prompt: p.Prompt, Granularities: p.Granularities,
		})
		if err != nil {
			return job.Submission{}, err
		}
		return job.Submission{Immediate: []job.Output{{Text: res.Transcript, MIME: "application/json"}}}, nil
	default:
		res, err := provider.Generate(provider.ImageParams{
			Context: ctx, Backend: p.Backend, Prompt: p.Prompt, Model: p.Model, Size: p.Size,
			Quality: p.Quality, Background: p.Background, OutputFormat: p.OutputFormat, N: p.N, Refs: p.Refs,
		})
		if err != nil {
			return job.Submission{}, err
		}
		sub := job.Submission{Immediate: []job.Output{{Data: res.Bytes, MIME: "image/" + orDefault(p.OutputFormat, "png")}}}
		if usd := imageCost(p.Backend, res.Usage); usd != nil {
			sub.ActualUSD = usd
		}
		return sub, nil
	}
}

// Poll is never reached: every call here is synchronous.
func (Adapter) Poll(context.Context, registry.Route, job.Record) (job.PollResult, error) {
	return job.PollResult{}, fmt.Errorf("openai routes are synchronous")
}

// imageCost turns reported token usage into dollars at the published gpt-image
// rates ($8 per 1M input tokens, $30 per 1M output tokens). Codex-LB runs on
// the pooled subscription, so it carries no marginal cost.
func imageCost(backend string, usage json.RawMessage) *float64 {
	if backend == "codex-lb" {
		zero := 0.0
		return &zero
	}
	var u struct {
		In  float64 `json:"input_tokens"`
		Out float64 `json:"output_tokens"`
	}
	if json.Unmarshal(usage, &u) != nil || u.Out == 0 {
		return nil
	}
	usd := (u.In*8 + u.Out*30) / 1e6
	return &usd
}

// imageSize maps --res and --aspect onto gpt-image sizes; --param size wins.
func imageSize(res, aspect string) string {
	long := map[string]string{"": "1536", "1k": "1536", "2k": "2048", "4k": "3840"}[strings.ToLower(res)]
	short := map[string]string{"": "1024", "1k": "1024", "2k": "1360", "4k": "2160"}[strings.ToLower(res)]
	if long == "" {
		return ""
	}
	switch aspect {
	case "", "1:1":
		if res == "" {
			return ""
		}
		return short + "x" + short
	case "9:16", "2:3", "3:4", "4:5":
		return short + "x" + long
	default:
		return long + "x" + short
	}
}

func str(m map[string]any, k, def string) string {
	if v, ok := m[k]; ok {
		return fmt.Sprint(v)
	}
	return def
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func init() { adapter.Register(Adapter{}, "openai", "codex-lb") }
