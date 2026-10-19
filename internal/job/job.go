// Package job runs one generation on one route and records it durably. The
// provider job id is persisted before any polling starts, so a killed process
// can always resume with `creative jobs fetch`, and a job that is running or
// done is never submitted twice.
package job

import (
	"context"
	"encoding/json"
	"time"

	"github.com/radjathaher/creative-cli/internal/registry"
)

// Status values. failed_unbilled lets a caller resubmit on the same route
// knowing nothing was charged; uncertain means the outcome is unknown (for
// example a timeout after submit) and must not be resubmitted blindly.
const (
	StatusQueued         = "queued"
	StatusRunning        = "running"
	StatusDone           = "done"
	StatusFailedUnbilled = "failed_unbilled"
	StatusFailedBilled   = "failed_billed"
	StatusBlockedSafety  = "blocked_safety"
	StatusUncertain      = "uncertain"
)

// Terminal reports whether a status will not change again.
func Terminal(s string) bool {
	switch s {
	case StatusDone, StatusFailedUnbilled, StatusFailedBilled, StatusBlockedSafety:
		return true
	}
	return false
}

// Request is the provider-neutral job built from shared flags. Reference
// slices keep the order the user gave, so @Image1/@Video1 in prompts stay valid.
type Request struct {
	Prompt        string            `json:"prompt,omitempty"`
	Text          string            `json:"text,omitempty"`
	Images        []string          `json:"images,omitempty"`
	Videos        []string          `json:"videos,omitempty"`
	Audios        []string          `json:"audios,omitempty"`
	FirstFrame    string            `json:"first_frame,omitempty"`
	LastFrame     string            `json:"last_frame,omitempty"`
	Input         string            `json:"input,omitempty"` // the media being transformed (upscale, stt, caption)
	Duration      float64           `json:"duration,omitempty"`
	Resolution    string            `json:"resolution,omitempty"`
	Aspect        string            `json:"aspect,omitempty"`
	Seed          *int              `json:"seed,omitempty"`
	GenerateAudio *bool             `json:"generate_audio,omitempty"`
	FPS           int               `json:"fps,omitempty"`
	Quality       string            `json:"quality,omitempty"`
	Count         int               `json:"count,omitempty"`
	Params        map[string]any    `json:"params,omitempty"` // --param key=value passthrough
	Extra         map[string]string `json:"extra,omitempty"`  // adapter-specific flags (voice id, model, format)
}

// Output is one produced asset.
type Output struct {
	Path         string `json:"path,omitempty"`
	URL          string `json:"url,omitempty"`
	MIME         string `json:"mime,omitempty"`
	URLExpiresAt string `json:"url_expires_at,omitempty"`
	Text         string `json:"text,omitempty"` // for text results (stt, understanding)
	// Data carries bytes from synchronous providers; the engine writes it to
	// the output path and never persists it in the job record.
	Data []byte `json:"-"`
}

// Cost holds the quote estimate and, when the provider reports it, the bill.
type Cost struct {
	EstimatedUSD *float64 `json:"estimated_usd,omitempty"`
	ActualUSD    *float64 `json:"actual_usd,omitempty"`
	Basis        string   `json:"basis,omitempty"`
}

// Record is the persisted state of one job; it is also the v1 output envelope.
type Record struct {
	JobID         string          `json:"job_id"`
	Run           string          `json:"run,omitempty"`
	Op            string          `json:"op"`
	Family        string          `json:"family"`
	Provider      string          `json:"provider"`
	RouteID       string          `json:"route_id"`
	ModelID       string          `json:"model_id"`
	ProviderJobID string          `json:"provider_job_id,omitempty"`
	Status        string          `json:"status"`
	Error         string          `json:"error,omitempty"`
	Cost          Cost            `json:"cost"`
	PricesAsOf    string          `json:"prices_as_of,omitempty"`
	Verified      bool            `json:"verified"`
	PromptSHA256  string          `json:"prompt_sha256,omitempty"`
	Request       Request         `json:"request"`
	Payload       json.RawMessage `json:"payload,omitempty"` // provider body actually sent, secrets never included
	Poll          json.RawMessage `json:"poll,omitempty"`    // adapter state needed to resume (status urls)
	Outputs       []Output        `json:"outputs,omitempty"`
	OutPath       string          `json:"out,omitempty"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
	ElapsedSecs   float64         `json:"elapsed_seconds"`
}

// Submission is what an adapter returns after a successful submit.
type Submission struct {
	ProviderJobID string
	Payload       json.RawMessage
	Poll          json.RawMessage
	// Immediate holds outputs for synchronous providers that answer at once;
	// the engine then skips polling.
	Immediate []Output
	ActualUSD *float64
}

// PollResult is one status check.
type PollResult struct {
	Status    string
	Outputs   []Output
	ActualUSD *float64
	Error     string
	Raw       json.RawMessage
}

// Adapter speaks one transport. Build validates and maps the request without
// network side effects beyond uploads, so --dry-run can show the payload.
type Adapter interface {
	// Prepare uploads local references and returns the provider body.
	Prepare(ctx context.Context, r registry.Route, req Request, dryRun bool) (json.RawMessage, error)
	Submit(ctx context.Context, r registry.Route, payload json.RawMessage) (Submission, error)
	Poll(ctx context.Context, r registry.Route, rec Record) (PollResult, error)
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }
