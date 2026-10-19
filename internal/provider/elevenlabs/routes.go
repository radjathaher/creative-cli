package elevenlabs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
)

// Exported entry points used by the v1 adapter. They wrap the client helpers so
// each returns bytes or decoded JSON without touching the filesystem.

// Speak synthesises text and returns audio bytes.
func (c *Client) Speak(text, voiceID, model, format string) ([]byte, error) {
	res, err := c.Speech(SpeechParams{Text: text, VoiceID: voiceID, Model: model, OutputFormat: format})
	if err != nil {
		return nil, err
	}
	return res.Bytes, nil
}

// SoundEffect renders a prompt to audio bytes.
func (c *Client) SoundEffect(prompt string, seconds *float64, format string) ([]byte, error) {
	res, err := c.Sfx(SfxParams{Text: prompt, DurationSeconds: seconds, OutputFormat: format})
	if err != nil {
		return nil, err
	}
	return res.Bytes, nil
}

// Compose renders music and returns either bytes or a download URL.
func (c *Client) Compose(prompt string, lengthMs int, format string) ([]byte, string, error) {
	res, err := c.Music(MusicParams{Prompt: prompt, MusicLengthMs: lengthMs, OutputFormat: format})
	if err != nil {
		return nil, "", err
	}
	return res.Bytes, res.URL, nil
}

// Scribe transcribes a file with word-level timestamps.
func (c *Client) Scribe(source, model, language string) (json.RawMessage, error) {
	fields := map[string]string{"model_id": model, "timestamps_granularity": "word", "language_code": language}
	_, raw, err := c.postMultipart("/v1/speech-to-text", fields, []fileUpload{{field: "file", source: source}})
	return raw, err
}

// Clone creates an instant voice clone and returns the new voice object.
func (c *Client) Clone(name string, samples []string) (json.RawMessage, error) {
	_, raw, err := c.CloneVoice(VoiceParams{Name: name, Samples: samples})
	return raw, err
}

// Design previews voices from a description; previews carry base64 audio and
// a generated_voice_id that can be saved in the ElevenLabs voice library.
func (c *Client) Design(description, text string) (json.RawMessage, error) {
	body := map[string]any{"voice_description": description}
	if text != "" {
		body["text"] = text
	} else {
		body["auto_generate_text"] = true
	}
	data, _, err := c.postForJSON("/v1/text-to-voice/design", body)
	return data, err
}

// ChangeVoice runs speech-to-speech and returns audio bytes.
func (c *Client) ChangeVoice(source, voiceID, model string) ([]byte, error) {
	return c.multipartBytes("/v1/speech-to-speech/"+url.PathEscape(voiceID), map[string]string{"model_id": model}, "audio", source)
}

// Isolate removes background noise and returns audio bytes.
func (c *Client) Isolate(source string) ([]byte, error) {
	return c.multipartBytes("/v1/audio-isolation", nil, "audio", source)
}

// DubSubmit starts a dubbing job and returns its id.
func (c *Client) DubSubmit(source, targetLang string) (string, json.RawMessage, error) {
	fields := map[string]string{"target_lang": targetLang}
	var files []fileUpload
	if isURL(source) {
		fields["source_url"] = source
	} else {
		files = append(files, fileUpload{field: "file", source: source})
	}
	parsed, raw, err := c.postMultipart("/v1/dubbing", fields, files)
	if err != nil {
		return "", raw, err
	}
	id, _ := parsed["dubbing_id"].(string)
	if id == "" {
		return "", raw, fmt.Errorf("dubbing response missing dubbing_id")
	}
	return id, raw, nil
}

// DubStatus reports "dubbing", "dubbed", or "failed".
func (c *Client) DubStatus(id string) (string, json.RawMessage, error) {
	parsed, raw, err := c.getJSON("/v1/dubbing/" + url.PathEscape(id))
	if err != nil {
		return "", raw, err
	}
	s, _ := parsed["status"].(string)
	return s, raw, nil
}

// DubAudio downloads the dubbed track.
func (c *Client) DubAudio(id, lang string) ([]byte, error) {
	return c.getBytes(fmt.Sprintf("/v1/dubbing/%s/audio/%s", url.PathEscape(id), url.PathEscape(lang)))
}

func (c *Client) postForJSON(path string, body any) (json.RawMessage, string, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL()+path, bytes.NewReader(buf))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return nil, "", err
	}
	data, err := readBytes(resp)
	return data, resp.Header.Get("Content-Type"), err
}

func (c *Client) multipartBytes(path string, fields map[string]string, fileField, source string) ([]byte, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if v != "" {
			_ = w.WriteField(k, v)
		}
	}
	data, name, err := readSource(source)
	if err != nil {
		return nil, err
	}
	fw, err := w.CreateFormFile(fileField, name)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL()+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	return readBytes(resp)
}

func isURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}
