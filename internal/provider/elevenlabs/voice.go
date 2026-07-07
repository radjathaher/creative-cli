package elevenlabs

import "encoding/json"

// VoiceParams configures an instant voice clone (IVC).
type VoiceParams struct {
	Name        string
	Description string
	Samples     []string // one or more audio sample paths or URLs
}

// CloneVoice creates an instant voice clone from the supplied audio samples via
// /v1/voices/add (a multipart upload with repeated "files" parts). It returns
// the decoded voice object, which includes the new voice_id.
func (c *Client) CloneVoice(p VoiceParams) (map[string]any, json.RawMessage, error) {
	fields := map[string]string{"name": p.Name}
	if p.Description != "" {
		fields["description"] = p.Description
	}
	files := make([]fileUpload, 0, len(p.Samples))
	for _, s := range p.Samples {
		files = append(files, fileUpload{field: "files", source: s})
	}
	return c.postMultipart("/v1/voices/add", fields, files)
}
