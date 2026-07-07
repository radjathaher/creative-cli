package falpipe

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/fal"
)

// veedEndpoint is fal's VEED subtitle-burning route: it accepts a video url and
// returns a captioned video rendered with the requested preset.
const veedEndpoint = "veed/subtitles"

// VeedOpts configures a VEED subtitle-burning job on fal.
type VeedOpts struct {
	Input            string
	Preset           string // caption style preset, e.g. simple
	Language         string // optional source language override
	Out              string
	NoWait           bool
	PollIntervalSecs int
	MaxWaitSecs      int
}

// Veed resolves the input video, submits the VEED subtitle job, and (unless
// NoWait) polls, downloads the captioned video, and returns the envelope.
func Veed(o VeedOpts, pretty, raw bool) (*core.Envelope, json.RawMessage, error) {
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
	body := map[string]any{
		"video_url": input.ResolvedURL,
		"preset":    orDefault(o.Preset, "simple"),
	}
	if o.Language != "" {
		body["language"] = o.Language
	}
	core.Progress("captioning with fal veed (preset %s)", orDefault(o.Preset, "simple"))
	qref, qraw, err := client.Submit(veedEndpoint, body)
	if err != nil {
		return nil, qraw, err
	}

	env := &core.Envelope{
		Provider:  "fal-veed",
		Endpoint:  veedEndpoint,
		RequestID: qref.RequestID,
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
