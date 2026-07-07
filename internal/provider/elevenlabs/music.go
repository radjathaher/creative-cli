package elevenlabs

import (
	"encoding/json"
	"net/url"
	"strings"
)

// MusicParams configures a music composition.
type MusicParams struct {
	Prompt        string
	MusicLengthMs int // optional; 0 lets the model choose a duration
	OutputFormat  string
}

// MusicResult carries either the composed audio bytes or, when the endpoint
// answers with a JSON envelope, a URL from which the track can be downloaded.
type MusicResult struct {
	Bytes    []byte
	URL      string
	Endpoint string
}

// Music composes a track from a text prompt via /v1/music. The endpoint normally
// streams raw audio, but a JSON response (detected via Content-Type) is treated
// as an envelope carrying a downloadable audio URL.
func (c *Client) Music(p MusicParams) (*MusicResult, error) {
	const endpoint = "/v1/music"
	q := url.Values{}
	if p.OutputFormat != "" {
		q.Set("output_format", p.OutputFormat)
	}
	body := map[string]any{"prompt": p.Prompt}
	if p.MusicLengthMs > 0 {
		body["music_length_ms"] = p.MusicLengthMs
	}
	data, ctype, err := c.postForBytes(endpoint, q, body)
	if err != nil {
		return nil, err
	}
	if strings.Contains(ctype, "application/json") {
		var parsed map[string]any
		_ = json.Unmarshal(data, &parsed)
		return &MusicResult{URL: extractAudioURL(parsed), Endpoint: endpoint}, nil
	}
	return &MusicResult{Bytes: data, Endpoint: endpoint}, nil
}

// extractAudioURL scans a JSON envelope for the first plausible audio URL field.
func extractAudioURL(m map[string]any) string {
	for _, k := range []string{"audio_url", "url", "output_url", "download_url"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
