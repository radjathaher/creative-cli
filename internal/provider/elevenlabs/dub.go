package elevenlabs

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
)

// DubOpts configures a dubbing job.
type DubOpts struct {
	Input            string // local file path or remote source URL
	TargetLang       string // target language ISO code
	Out              string
	NoWait           bool
	PollIntervalSecs int
	MaxWaitSecs      int
}

// Dub submits a dubbing job, polls until the requested language render reaches
// the "dubbed" status, then downloads the dubbed audio track. ElevenLabs dubbing
// is asynchronous: create returns a dubbing_id whose status transitions through
// "dubbing" to "dubbed". A remote input is passed as source_url; a local file is
// uploaded as the multipart "file" part.
func (c *Client) Dub(o DubOpts, _, _ bool) (*core.Envelope, json.RawMessage, error) {
	if !o.NoWait && o.Out == "" {
		return nil, nil, fmt.Errorf("--out is required unless --no-wait is set")
	}
	started := time.Now()

	fields := map[string]string{"target_lang": o.TargetLang}
	var files []fileUpload
	input := core.InputInfo{Source: o.Input}
	if core.IsRemoteRef(o.Input) {
		fields["source_url"] = o.Input
		input.Kind = "url"
		input.ResolvedURL = o.Input
	} else {
		files = append(files, fileUpload{field: "file", source: o.Input})
		input.Kind = "file"
	}

	core.Progress("submitting dubbing job -> %s", o.TargetLang)
	parsed, craw, err := c.postMultipart("/v1/dubbing", fields, files)
	if err != nil {
		return nil, craw, err
	}
	dubbingID, _ := parsed["dubbing_id"].(string)
	if dubbingID == "" {
		return nil, craw, fmt.Errorf("dubbing response missing dubbing_id")
	}

	env := &core.Envelope{
		Provider:  "elevenlabs",
		Endpoint:  "/v1/dubbing",
		RequestID: dubbingID,
		Input:     &input,
	}
	if o.NoWait {
		env.Output = "queued"
		env.ElapsedSeconds = roundSecs(started)
		return env, craw, nil
	}

	core.Progress("queued %s; polling", dubbingID)
	statusRaw, err := c.waitForDub(dubbingID, o.MaxWaitSecs, o.PollIntervalSecs)
	if err != nil {
		return nil, statusRaw, err
	}

	audioPath := fmt.Sprintf("/v1/dubbing/%s/audio/%s", dubbingID, o.TargetLang)
	core.Progress("downloading dubbed audio")
	data, err := c.getBytes(audioPath)
	if err != nil {
		return nil, statusRaw, err
	}
	if err := core.WriteFile(o.Out, data); err != nil {
		return nil, statusRaw, err
	}
	env.Output = "audio"
	env.Out = o.Out
	env.ElapsedSeconds = roundSecs(started)
	core.Progress("done in %.3fs", env.ElapsedSeconds)
	return env, statusRaw, nil
}

// waitForDub polls the dubbing status endpoint until the render is "dubbed" or
// the deadline elapses, returning the final status payload for --raw output.
func (c *Client) waitForDub(id string, maxWaitSecs, pollIntervalSecs int) (json.RawMessage, error) {
	if pollIntervalSecs <= 0 {
		pollIntervalSecs = 5
	}
	if maxWaitSecs <= 0 {
		maxWaitSecs = 1200
	}
	deadline := time.Now().Add(time.Duration(maxWaitSecs) * time.Second)
	for {
		parsed, raw, err := c.getJSON("/v1/dubbing/" + id)
		if err != nil {
			return raw, err
		}
		status, _ := parsed["status"].(string)
		switch status {
		case "dubbed":
			return raw, nil
		case "failed", "error":
			return raw, fmt.Errorf("dubbing %s failed: %s", id, truncate(raw))
		}
		if time.Now().After(deadline) {
			return raw, fmt.Errorf("timed out after %ds waiting for dubbing %s (status %q)", maxWaitSecs, id, status)
		}
		core.Progress("status %s; waiting %ds", status, pollIntervalSecs)
		time.Sleep(time.Duration(pollIntervalSecs) * time.Second)
	}
}
