package elevenlabs

import "net/url"

// SpeechParams configures a text-to-speech synthesis.
type SpeechParams struct {
	Text         string
	VoiceID      string
	Model        string
	OutputFormat string
}

// SpeechResult carries the synthesised audio bytes and the endpoint that
// produced them.
type SpeechResult struct {
	Bytes    []byte
	Endpoint string
}

// Speech synthesises Text with the given voice and model. The ElevenLabs API
// takes output_format as a query parameter while text and model_id live in the
// JSON body; the response is the raw audio stream.
func (c *Client) Speech(p SpeechParams) (*SpeechResult, error) {
	endpoint := "/v1/text-to-speech/" + p.VoiceID
	q := url.Values{}
	if p.OutputFormat != "" {
		q.Set("output_format", p.OutputFormat)
	}
	body := map[string]any{"text": p.Text, "model_id": p.Model}
	data, _, err := c.postForBytes(endpoint, q, body)
	if err != nil {
		return nil, err
	}
	return &SpeechResult{Bytes: data, Endpoint: endpoint}, nil
}
