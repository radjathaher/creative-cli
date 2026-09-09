// Package openai wraps the OpenAI HTTP API surface used by creative: image
// generation/editing (GPT Image 2.5) and audio transcription (whisper).
package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	Context      context.Context
	Timeout      time.Duration
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
	Provider string
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
	backend, err := resolveImageBackend()
	if err != nil {
		return nil, err
	}
	if p.Model == "" {
		p.Model = "gpt-image-2.5-sunburst"
	}
	if p.N <= 0 {
		p.N = 1
	}
	if p.Context == nil {
		p.Context = context.Background()
	}
	if p.Timeout <= 0 {
		p.Timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(p.Context, p.Timeout)
	defer cancel()
	p.Context = ctx
	return p.generateWithBackend(backend, true)
}

func (p ImageParams) generateWithBackend(backend imageBackend, allowFallback bool) (*ImageResult, error) {
	var resp *http.Response
	var err error
	base, _ := url.Parse(backend.base)
	endpoint := strings.TrimRight(base.Path, "/") + "/images/generations"
	if len(p.Refs) > 0 {
		endpoint = strings.TrimRight(base.Path, "/") + "/images/edits"
		resp, err = p.postEdits(backend)
	} else {
		resp, err = p.postGenerations(backend)
	}
	if err != nil {
		return nil, err
	}

	_, raw, err := core.DecodeJSON(resp)
	if err != nil {
		if allowFallback && backend.name == "codex-lb" && allowsImageFallback(resp.StatusCode, raw) {
			direct, configErr := directImageBackend()
			if configErr != nil {
				return nil, fmt.Errorf("codex-lb rejected generation; OpenAI fallback unavailable: %w", configErr)
			}
			core.Progress("codex-lb pool exhausted; using OpenAI fallback")
			return p.generateWithBackend(direct, false)
		}
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
	res := &ImageResult{Provider: backend.name, Raw: raw, Model: p.Model, Usage: parsed.Usage, Endpoint: endpoint}
	switch {
	case d.B64JSON != "":
		b, derr := base64.StdEncoding.DecodeString(d.B64JSON)
		if derr != nil {
			return nil, fmt.Errorf("decode b64 image: %w", derr)
		}
		res.Bytes = b
	case d.URL != "":
		b, derr := fetchContext(p.Context, d.URL)
		if derr != nil {
			return nil, derr
		}
		res.Bytes = b
	default:
		return nil, fmt.Errorf("image response missing b64_json and url")
	}
	return res, nil
}

func (p ImageParams) postGenerations(backend imageBackend) (*http.Response, error) {
	body := map[string]any{"model": p.Model, "prompt": p.Prompt, "n": p.N}
	putIf(body, "size", p.Size)
	putIf(body, "quality", p.Quality)
	putIf(body, "background", p.Background)
	putIf(body, "output_format", p.OutputFormat)
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(p.Context, http.MethodPost, backend.base+"/images/generations", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+backend.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", core.UserAgent())
	return sendImageRequest(req)
}

func (p ImageParams) postEdits(backend imageBackend) (*http.Response, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("model", p.Model)
	_ = w.WriteField("prompt", p.Prompt)
	_ = w.WriteField("n", strconv.Itoa(p.N))
	writeFieldIf(w, "size", p.Size)
	writeFieldIf(w, "quality", p.Quality)
	writeFieldIf(w, "background", p.Background)
	writeFieldIf(w, "output_format", p.OutputFormat)
	for _, ref := range p.Refs {
		data, name, err := readRefContext(p.Context, ref)
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
	req, err := http.NewRequestWithContext(p.Context, http.MethodPost, backend.base+"/images/edits", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+backend.key)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("User-Agent", core.UserAgent())
	return sendImageRequest(req)
}

// readRef loads an image reference from a local path or remote URL.
func readRef(ref string) (data []byte, name string, err error) {
	return readRefContext(context.Background(), ref)
}

func readRefContext(ctx context.Context, ref string) (data []byte, name string, err error) {
	if core.IsRemoteRef(ref) {
		b, ferr := fetchContext(ctx, ref)
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
	return fetchContext(context.Background(), url)
}

func fetchContext(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
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
