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
	Position         string // top | center | bottom
	Shadow           string // none | min | mid | max
	Font             string // Google Font family for both tiers
	FontWeight       int    // 100-900; 0 leaves the preset default
	FontColor        string // hex for baseline words
	HighlightColor   string // hex for highlighted words
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
	if c := veedCustomization(o); len(c) > 0 {
		body["customization"] = c
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

// veedCustomization builds the optional customization block; VEED only accepts
// Google Fonts, and each text tier can carry its own font, weight and colour.
func veedCustomization(o VeedOpts) map[string]any {
	c := map[string]any{}
	if o.Position != "" {
		c["position"] = o.Position
	}
	if o.Shadow != "" {
		c["shadow"] = o.Shadow
	}
	tier := func(color string) map[string]any {
		t := map[string]any{}
		if o.Font != "" {
			t["font"] = o.Font
		}
		if o.FontWeight > 0 {
			t["weight"] = o.FontWeight
		}
		if color != "" {
			t["color"] = color
		}
		return t
	}
	base, hi := tier(o.FontColor), tier(o.HighlightColor)
	if len(base) > 0 || len(hi) > 0 {
		tc := map[string]any{}
		if len(base) > 0 {
			tc["baseline"] = base
		}
		if len(hi) > 0 {
			tc["highlighted"] = hi
		}
		c["text_customizations"] = tc
	}
	return c
}
