package falpipe

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/fal"
)

// understandEndpoint is fal's OpenRouter video-understanding route: it forwards
// the resolved video plus a text prompt to a Gemini model and returns the
// model's textual answer under result["output"].
const understandEndpoint = "openrouter/router/video"

// UnderstandOpts configures a video-understanding (recreation brief) job.
type UnderstandOpts struct {
	Input            string
	Model            string // OpenRouter model id, e.g. google/gemini-2.5-flash
	Temperature      float64
	MaxOutputTokens  int
	Prompt           string // optional extra instruction appended to the brief prompt
	NoWait           bool
	PollIntervalSecs int
	MaxWaitSecs      int
}

// Understand resolves the video (local file, http url, or YouTube url passed
// through untouched), asks Gemini via fal's OpenRouter route for a detailed
// video-recreation brief, and returns the brief in the result envelope.
func Understand(o UnderstandOpts, pretty, raw bool) (*core.Envelope, json.RawMessage, error) {
	client, err := fal.New()
	if err != nil {
		return nil, nil, err
	}
	started := time.Now()
	input, err := client.ResolveInput(o.Input)
	if err != nil {
		return nil, nil, err
	}
	body := map[string]any{
		"video_urls":  []string{input.ResolvedURL},
		"prompt":      buildUnderstandPrompt(o.Prompt),
		"model":       o.Model,
		"temperature": o.Temperature,
		"max_tokens":  o.MaxOutputTokens,
	}
	core.Progress("understanding video with %s", o.Model)
	qref, qraw, err := client.Submit(understandEndpoint, body)
	if err != nil {
		return nil, qraw, err
	}

	env := &core.Envelope{
		Provider:  "fal-openrouter-video",
		Endpoint:  understandEndpoint,
		RequestID: qref.RequestID,
		Model:     o.Model,
		Input:     &input,
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
	brief, ok := result["output"].(string)
	if !ok {
		return nil, resraw, fmt.Errorf("fal result missing output brief")
	}
	if u, ok := result["usage"]; ok {
		if usage, merr := json.Marshal(u); merr == nil {
			env.Usage = usage
		}
	}
	env.Output = brief
	env.ElapsedSeconds = roundSecs(started)
	core.Progress("done in %.3fs", env.ElapsedSeconds)
	return env, resraw, nil
}

// buildUnderstandPrompt instructs Gemini to return a plain-text brief detailed
// enough to recreate the source video in another AI video tool; a non-empty
// extra instruction is appended verbatim.
func buildUnderstandPrompt(extra string) string {
	prompt := "Create a detailed video recreation brief from this video. Return only the brief text, not JSON. Include scene/timestamp notes, camera framing and motion, subjects and actions, on-screen text, audio or spoken content, pacing, transitions, visual style, and enough concrete detail to recreate the same video in another AI video tool."
	if e := strings.TrimSpace(extra); e != "" {
		prompt += "\n\nExtra instruction: " + e
	}
	return prompt
}
