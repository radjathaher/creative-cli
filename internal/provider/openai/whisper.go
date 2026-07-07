package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/radjathaher/creative-cli/internal/core"
)

// TranscribeParams configures an audio transcription or translation request.
type TranscribeParams struct {
	File        string
	Model       string
	Format      string   // text | json | srt | vtt | verbose_json
	Language    string   // optional ISO language hint (transcription only)
	Prompt      string   // optional style/vocabulary hint
	Temperature *float64 // optional sampling temperature
	Translate   bool     // translate to English instead of transcribing
}

// TranscribeResult carries the transcript text plus provider metadata. The Raw
// field holds the untouched response bytes for --raw output.
type TranscribeResult struct {
	Transcript string
	Raw        json.RawMessage
	Model      string
	Endpoint   string
}

// Transcribe uploads an audio/video file to Whisper. With Translate it targets
// /v1/audio/translations (English output); otherwise /v1/audio/transcriptions.
func Transcribe(p TranscribeParams) (*TranscribeResult, error) {
	key, err := core.ReadSecret("OPENAI_API_KEY")
	if err != nil {
		return nil, err
	}
	if p.Model == "" {
		p.Model = "whisper-1"
	}
	if p.Format == "" {
		p.Format = "text"
	}
	endpoint := "/v1/audio/transcriptions"
	if p.Translate {
		endpoint = "/v1/audio/translations"
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	data, name, err := readRef(p.File)
	if err != nil {
		return nil, err
	}
	fw, err := w.CreateFormFile("file", name)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(data); err != nil {
		return nil, err
	}
	_ = w.WriteField("model", p.Model)
	_ = w.WriteField("response_format", p.Format)
	writeFieldIf(w, "language", p.Language)
	writeFieldIf(w, "prompt", p.Prompt)
	if p.Temperature != nil {
		_ = w.WriteField("temperature", strconv.FormatFloat(*p.Temperature, 'f', -1, 64))
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	req, _ := http.NewRequest(http.MethodPost, baseURL()+strings.TrimPrefix(endpoint, "/v1"), &buf)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return nil, err
	}

	// The response is either JSON or plain text depending on response_format;
	// DecodeJSON hands back the untouched bytes in both cases and only errors on
	// a non-2xx status.
	_, raw, err := core.DecodeJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("transcription failed: %w", err)
	}
	return &TranscribeResult{
		Transcript: strings.TrimRight(string(raw), "\n"),
		Raw:        json.RawMessage(raw),
		Model:      p.Model,
		Endpoint:   endpoint,
	}, nil
}
