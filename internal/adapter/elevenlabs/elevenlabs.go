// Package elevenlabs adapts ElevenLabs direct to the job engine. Most calls are
// synchronous and return audio bytes; dubbing is the one asynchronous job.
package elevenlabs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/radjathaher/creative-cli/internal/adapter"
	"github.com/radjathaher/creative-cli/internal/job"
	provider "github.com/radjathaher/creative-cli/internal/provider/elevenlabs"
	"github.com/radjathaher/creative-cli/internal/registry"
)

// Adapter serves transport "elevenlabs".
type Adapter struct{}

// DefaultVoice is the v0.2.4 `speech` default, kept so voice.tts works without --param voice_id.
const DefaultVoice = "JBFqnCBsd6RMkjVDRZzb"

type payload struct {
	Op       string   `json:"op"`
	Model    string   `json:"model,omitempty"`
	Text     string   `json:"text,omitempty"`
	Prompt   string   `json:"prompt,omitempty"`
	VoiceID  string   `json:"voice_id,omitempty"`
	Format   string   `json:"output_format,omitempty"`
	Seconds  float64  `json:"seconds,omitempty"`
	Input    string   `json:"input,omitempty"`
	Samples  []string `json:"samples,omitempty"`
	Name     string   `json:"name,omitempty"`
	Language string   `json:"language,omitempty"`
}

// Prepare maps the request onto one ElevenLabs operation.
func (Adapter) Prepare(_ context.Context, r registry.Route, req job.Request, _ bool) (json.RawMessage, error) {
	p := payload{Op: r.Op, Model: modelFor(r), Text: req.Text, Prompt: req.Prompt,
		VoiceID: param(req, "voice_id", ""), Format: param(req, "output_format", ""),
		Seconds: req.Duration, Input: req.Input, Language: param(req, "language", "")}
	need := func(ok bool, msg string) error {
		if !ok {
			return fmt.Errorf("%s: %s", r.Op, msg)
		}
		return nil
	}
	var err error
	switch r.Op {
	case registry.OpVoiceTTS:
		if p.Text == "" {
			p.Text = p.Prompt
		}
		if p.VoiceID == "" {
			p.VoiceID = DefaultVoice
		}
		err = need(p.Text != "", "--text is required")
	case registry.OpSFXGenerate, registry.OpMusicGenerate:
		err = need(p.Prompt != "", "--prompt is required")
	case registry.OpSTT, registry.OpVoiceIsolate:
		err = need(p.Input != "", "an input file is required")
	case registry.OpVoiceChange:
		err = need(p.Input != "" && p.VoiceID != "", "an input file and --param voice_id are required")
	case registry.OpVoiceDub:
		err = need(p.Input != "" && p.Language != "", "an input file and --param language=<iso> are required")
	case registry.OpVoiceClone:
		p.Samples, p.Name = req.Audios, param(req, "name", "")
		err = need(len(p.Samples) > 0 && p.Name != "", "--audio samples and --param name are required")
	case registry.OpVoiceDesign:
		err = need(p.Prompt != "", "--prompt (voice description) is required")
	default:
		err = fmt.Errorf("op %s is not supported on elevenlabs", r.Op)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

// Submit runs synchronous ops at once; dubbing returns a job id to poll.
func (Adapter) Submit(_ context.Context, r registry.Route, raw json.RawMessage) (job.Submission, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return job.Submission{}, err
	}
	c, err := provider.New()
	if err != nil {
		return job.Submission{}, err
	}
	audio := func(b []byte, err error) (job.Submission, error) {
		if err != nil {
			return job.Submission{}, err
		}
		return job.Submission{Immediate: []job.Output{{Data: b, MIME: "audio/mpeg"}}}, nil
	}
	text := func(b json.RawMessage, err error) (job.Submission, error) {
		if err != nil {
			return job.Submission{}, err
		}
		return job.Submission{Immediate: []job.Output{{Text: string(b), MIME: "application/json"}}}, nil
	}
	switch p.Op {
	case registry.OpVoiceTTS:
		return audio(c.Speak(p.Text, p.VoiceID, p.Model, p.Format))
	case registry.OpSFXGenerate:
		var secs *float64
		if p.Seconds > 0 {
			secs = &p.Seconds
		}
		return audio(c.SoundEffect(p.Prompt, secs, p.Format))
	case registry.OpMusicGenerate:
		b, u, err := c.Compose(p.Prompt, int(p.Seconds*1000), p.Format)
		if err != nil {
			return job.Submission{}, err
		}
		if len(b) == 0 && u != "" {
			return job.Submission{Immediate: []job.Output{{URL: u, MIME: "audio/mpeg"}}}, nil
		}
		return audio(b, nil)
	case registry.OpSTT:
		return text(c.Scribe(p.Input, p.Model, p.Language))
	case registry.OpVoiceIsolate:
		return audio(c.Isolate(p.Input))
	case registry.OpVoiceChange:
		return audio(c.ChangeVoice(p.Input, p.VoiceID, p.Model))
	case registry.OpVoiceClone:
		return text(c.Clone(p.Name, p.Samples))
	case registry.OpVoiceDesign:
		return text(c.Design(p.Prompt, p.Text))
	case registry.OpVoiceDub:
		id, _, err := c.DubSubmit(p.Input, p.Language)
		if err != nil {
			return job.Submission{}, err
		}
		poll, _ := json.Marshal(map[string]string{"language": p.Language})
		return job.Submission{ProviderJobID: id, Poll: poll}, nil
	}
	return job.Submission{}, fmt.Errorf("op %s is not supported on elevenlabs", p.Op)
}

// Poll checks a dubbing job and downloads the track when it is ready.
func (Adapter) Poll(_ context.Context, _ registry.Route, rec job.Record) (job.PollResult, error) {
	c, err := provider.New()
	if err != nil {
		return job.PollResult{}, err
	}
	status, raw, err := c.DubStatus(rec.ProviderJobID)
	if err != nil {
		return job.PollResult{}, err
	}
	switch status {
	case "dubbed":
		var poll struct{ Language string }
		_ = json.Unmarshal(rec.Poll, &poll)
		b, err := c.DubAudio(rec.ProviderJobID, poll.Language)
		if err != nil {
			return job.PollResult{}, err
		}
		return job.PollResult{Status: job.StatusDone, Outputs: []job.Output{{Data: b, MIME: "audio/mpeg"}}, Raw: raw}, nil
	case "failed", "error":
		return job.PollResult{Status: job.StatusFailedBilled, Error: string(raw), Raw: raw}, nil
	}
	return job.PollResult{Status: job.StatusRunning, Raw: raw}, nil
}

// modelFor maps a route's model id onto the API model_id. Route ids for
// non-model operations (dubbing_v1, sound_effects) carry no model_id.
func modelFor(r registry.Route) string {
	switch r.Op {
	case registry.OpVoiceTTS, registry.OpSTT:
		return r.ModelID
	case registry.OpVoiceChange:
		return "eleven_multilingual_sts_v2"
	}
	return ""
}

func param(req job.Request, k, def string) string {
	if v, ok := req.Params[k]; ok {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	if v, ok := req.Extra[k]; ok {
		return v
	}
	return def
}

func init() { adapter.Register(Adapter{}, "elevenlabs") }
