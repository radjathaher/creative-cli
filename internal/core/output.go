package core

import (
	"encoding/json"
	"fmt"
	"os"
)

// Envelope is the uniform machine-readable result printed to stdout by every
// verb. stdout is pure JSON so agents can pipe it straight into `jq`; stderr
// carries human progress only. This mirrors storyboard-cli's output contract,
// the most evolved of the sibling CLIs.
type Envelope struct {
	Provider       string          `json:"provider"`
	Endpoint       string          `json:"endpoint,omitempty"`
	RequestID      string          `json:"request_id,omitempty"`
	Model          string          `json:"model,omitempty"`
	Input          *InputInfo      `json:"input,omitempty"`
	Output         any             `json:"output,omitempty"`
	Usage          json.RawMessage `json:"usage,omitempty"`
	Cost           *float64        `json:"estimated_cost,omitempty"`
	ElapsedSeconds float64         `json:"elapsed_seconds"`
	Out            string          `json:"out,omitempty"`
}

// InputInfo records how a reference input (prompt, file, or URL) was resolved.
type InputInfo struct {
	Kind        string `json:"kind"`
	Source      string `json:"source"`
	ResolvedURL string `json:"resolved_url,omitempty"`
}

// Emit writes the result to stdout. With useRaw the untouched provider payload
// is printed verbatim; otherwise the envelope is marshalled. pretty toggles
// indentation.
func Emit(env *Envelope, raw json.RawMessage, pretty, useRaw bool) error {
	var v any = env
	if useRaw && len(raw) > 0 {
		v = raw
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if pretty {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}

// Progress prints a human-readable status line to stderr.
func Progress(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// F is a convenience for building a *float64 for the optional Cost field.
func F(v float64) *float64 { return &v }
