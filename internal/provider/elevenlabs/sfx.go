package elevenlabs

// SfxParams configures a sound-effect generation.
type SfxParams struct {
	Text            string
	DurationSeconds *float64 // optional; the model auto-selects a length when nil
	OutputFormat    string
}

// SfxResult carries the generated audio bytes and the endpoint that produced
// them.
type SfxResult struct {
	Bytes    []byte
	Endpoint string
}

// Sfx generates a sound effect from a text prompt via /v1/sound-generation,
// returning the raw audio stream.
func (c *Client) Sfx(p SfxParams) (*SfxResult, error) {
	const endpoint = "/v1/sound-generation"
	body := map[string]any{"text": p.Text}
	if p.DurationSeconds != nil {
		body["duration_seconds"] = *p.DurationSeconds
	}
	data, _, err := c.postForBytes(endpoint, nil, body)
	if err != nil {
		return nil, err
	}
	return &SfxResult{Bytes: data, Endpoint: endpoint}, nil
}
