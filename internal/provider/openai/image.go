// Package openai wraps the OpenAI HTTP API surface used by creative: image
// generation/editing (gpt-image-2) and audio transcription (whisper).
package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/radjathaher/creative-cli/internal/core"
)

const defaultBaseURL = "https://api.openai.com/v1"

// baseURL honours OPENAI_API_URL so the CLI can target OpenAI-compatible hosts.
func baseURL() string {
	if v := strings.TrimSpace(os.Getenv("OPENAI_API_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultBaseURL
}

// ImageParams configures an image generation or edit request.
type ImageParams struct {
	Prompt       string
	Model        string
	Size         string
	Quality      string
	Background   string
	OutputFormat string
	N            int
	Refs         []string // when non-empty, uses the edits (img2img) endpoint
}

// ImageResult carries the decoded bytes of the first image plus provider metadata.
type ImageResult struct {
	Bytes    []byte
	Raw      json.RawMessage
	Model    string
	Usage    json.RawMessage
	Endpoint string
}

type imageResponse struct {
	Data []struct {
		B64JSON string `json:"b64_json"`
		URL     string `json:"url"`
	} `json:"data"`
	Usage json.RawMessage `json:"usage"`
}

// Generate produces images. With Refs it uses /v1/images/edits (img2img); other-
// wise /v1/images/generations (text2img). The first image is returned decoded.
func Generate(p ImageParams) (*ImageResult, error) {
	key, err := core.ReadSecret("OPENAI_API_KEY")
	if err != nil {
		return nil, err
	}
	if p.Model == "" {
		p.Model = "gpt-image-2"
	}
	if p.N <= 0 {
		p.N = 1
	}

	var resp *http.Response
	endpoint := "/v1/images/generations"
	if len(p.Refs) > 0 {
		endpoint = "/v1/images/edits"
		resp, err = p.postEdits(key)
	} else {
		resp, err = p.postGenerations(key)
	}
	if err != nil {
		return nil, err
	}

	_, raw, err := core.DecodeJSON(resp)
	if err != nil {
		return nil, err
	}
	var parsed imageResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode image response: %w", err)
	}
	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("no image returned")
	}
	d := parsed.Data[0]
	res := &ImageResult{Raw: raw, Model: p.Model, Usage: parsed.Usage, Endpoint: endpoint}
	switch {
	case d.B64JSON != "":
		b, derr := base64.StdEncoding.DecodeString(d.B64JSON)
		if derr != nil {
			return nil, fmt.Errorf("decode b64 image: %w", derr)
		}
		res.Bytes = b
	case d.URL != "":
		b, derr := fetch(d.URL)
		if derr != nil {
			return nil, derr
		}
		res.Bytes = b
	default:
		return nil, fmt.Errorf("image response missing b64_json and url")
	}
	return res, nil
}

func (p ImageParams) postGenerations(key string) (*http.Response, error) {
	body := map[string]any{"model": p.Model, "prompt": p.Prompt, "n": p.N}
	putIf(body, "size", p.Size)
	putIf(body, "quality", p.Quality)
	putIf(body, "background", p.Background)
	putIf(body, "output_format", p.OutputFormat)
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, baseURL()+"/images/generations", bytes.NewReader(buf))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", core.UserAgent())
	return core.SharedClient.Do(req)
}

func (p ImageParams) postEdits(key string) (*http.Response, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("model", p.Model)
	_ = w.WriteField("prompt", p.Prompt)
	_ = w.WriteField("n", strconv.Itoa(p.N))
	writeFieldIf(w, "size", p.Size)
	writeFieldIf(w, "quality", p.Quality)
	writeFieldIf(w, "output_format", p.OutputFormat)
	for _, ref := range p.Refs {
		data, name, err := readRef(ref)
		if err != nil {
			return nil, err
		}
		fw, err := w.CreateFormFile("image[]", name)
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	req, _ := http.NewRequest(http.MethodPost, baseURL()+"/images/edits", &buf)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("User-Agent", core.UserAgent())
	return core.SharedClient.Do(req)
}

// readRef loads an image reference from a local path or remote URL.
func readRef(ref string) (data []byte, name string, err error) {
	if core.IsRemoteRef(ref) {
		b, ferr := fetch(ref)
		if ferr != nil {
			return nil, "", ferr
		}
		return b, "ref.png", nil
	}
	b, ferr := os.ReadFile(ref)
	if ferr != nil {
		return nil, "", fmt.Errorf("read reference %s: %w", ref, ferr)
	}
	return b, filepath.Base(ref), nil
}

func fetch(url string) ([]byte, error) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: http %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func putIf(m map[string]any, key, val string) {
	if val != "" {
		m[key] = val
	}
}

func writeFieldIf(w *multipart.Writer, key, val string) {
	if val != "" {
		_ = w.WriteField(key, val)
	}
}
