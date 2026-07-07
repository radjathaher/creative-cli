// Package elevenlabs wraps the ElevenLabs HTTP API surface used by creative:
// text-to-speech, sound effects, music composition, instant voice cloning, and
// dubbing. All requests authenticate with the xi-api-key header and share the
// process-wide HTTP client.
package elevenlabs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
)

const defaultBaseURL = "https://api.elevenlabs.io"

// baseURL honours ELEVENLABS_API_URL so the CLI can target compatible hosts.
func baseURL() string {
	if v := strings.TrimSpace(os.Getenv("ELEVENLABS_API_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultBaseURL
}

// Client issues authenticated requests against the ElevenLabs REST surface.
type Client struct {
	key string
}

// New reads ELEVENLABS_API_KEY and returns a ready client.
func New() (*Client, error) {
	key, err := core.ReadSecret("ELEVENLABS_API_KEY")
	if err != nil {
		return nil, err
	}
	return &Client{key: key}, nil
}

// do stamps the auth and user-agent headers before dispatching a request.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("xi-api-key", c.key)
	req.Header.Set("User-Agent", core.UserAgent())
	return core.SharedClient.Do(req)
}

// postForBytes posts a JSON body and returns the raw response bytes together
// with the response Content-Type. Generation endpoints (TTS, sound effects,
// music) answer with binary audio, but some may fall back to a JSON envelope
// carrying a download URL, so the caller inspects the content type to decide.
func (c *Client) postForBytes(path string, query url.Values, body any) ([]byte, string, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, "", err
	}
	u := baseURL() + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(buf))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := c.do(req)
	if err != nil {
		return nil, "", err
	}
	ctype := resp.Header.Get("Content-Type")
	data, err := readBytes(resp)
	return data, ctype, err
}

// getBytes fetches a binary resource (e.g. a rendered dubbing track).
func (c *Client) getBytes(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL()+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	return readBytes(resp)
}

// getJSON fetches and decodes a JSON resource (e.g. dubbing job status).
func (c *Client) getJSON(path string) (map[string]any, []byte, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL()+path, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return nil, nil, err
	}
	return core.DecodeJSON(resp)
}

// fileUpload names a multipart file part and the local path or remote URL whose
// contents fill it.
type fileUpload struct {
	field  string
	source string
}

// postMultipart submits a multipart/form-data request, streaming each fileUpload
// as a file part and returning the decoded JSON response.
func (c *Client) postMultipart(path string, fields map[string]string, files []fileUpload) (map[string]any, []byte, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if v != "" {
			_ = w.WriteField(k, v)
		}
	}
	for _, f := range files {
		data, name, err := readSource(f.source)
		if err != nil {
			return nil, nil, err
		}
		fw, err := w.CreateFormFile(f.field, name)
		if err != nil {
			return nil, nil, err
		}
		if _, err := fw.Write(data); err != nil {
			return nil, nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL()+path, &buf)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return nil, nil, err
	}
	return core.DecodeJSON(resp)
}

// readSource loads a sample from a local path or remote URL for multipart upload.
func readSource(src string) (data []byte, name string, err error) {
	if core.IsRemoteRef(src) {
		req, _ := http.NewRequest(http.MethodGet, src, nil)
		req.Header.Set("User-Agent", core.UserAgent())
		resp, ferr := core.SharedClient.Do(req)
		if ferr != nil {
			return nil, "", ferr
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, "", fmt.Errorf("fetch %s: http %d", src, resp.StatusCode)
		}
		b, rerr := io.ReadAll(resp.Body)
		if rerr != nil {
			return nil, "", rerr
		}
		return b, filepath.Base(src), nil
	}
	b, ferr := os.ReadFile(src)
	if ferr != nil {
		return nil, "", fmt.Errorf("read %s: %w", src, ferr)
	}
	return b, filepath.Base(src), nil
}

// readBytes drains a response, turning a non-2xx status into an error carrying
// the (textual) error body ElevenLabs returns on failure.
func readBytes(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(data))
	}
	return data, nil
}

func truncate(b []byte) string {
	const n = 4000
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…(truncated)"
}

// roundSecs returns elapsed seconds since start rounded to milliseconds.
func roundSecs(start time.Time) float64 {
	return float64(int64(time.Since(start).Seconds()*1000)) / 1000
}
