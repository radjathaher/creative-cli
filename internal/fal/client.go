// Package fal implements the shared fal.ai queue + storage client used by the
// upscale, caption (VEED), and analyze verbs. It is a faithful Go port of the
// core that the sibling Rust CLIs (fal-cli, upscale-cli, storyboard-cli) each
// copy-pasted, consolidated here so the logic lives in exactly one place.
package fal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
)

const (
	DefaultQueueURL       = "https://queue.fal.run"
	DefaultRestURL        = "https://rest.fal.ai"
	DefaultCDNURL         = "https://v3.fal.media"
	DefaultCDNFallbackURL = "https://fal.media"

	multipartThreshold = 100 * 1024 * 1024 // upload files larger than this in chunks
	multipartChunkSize = 10 * 1024 * 1024
)

// Client talks to fal's queue and storage endpoints with a FAL_KEY credential.
type Client struct {
	key            string
	queueURL       string
	restURL        string
	cdnURL         string
	cdnFallbackURL string
}

// New builds a Client, reading FAL_KEY from the environment or /run/secrets.
func New() (*Client, error) {
	key, err := core.ReadSecret("FAL_KEY")
	if err != nil {
		return nil, err
	}
	return &Client{
		key:            key,
		queueURL:       envOr("FAL_QUEUE_URL", DefaultQueueURL),
		restURL:        envOr("FAL_REST_URL", DefaultRestURL),
		cdnURL:         envOr("FAL_CDN_URL", DefaultCDNURL),
		cdnFallbackURL: envOr("FAL_CDN_FALLBACK_URL", DefaultCDNFallbackURL),
	}, nil
}

func envOr(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

func (c *Client) authHeader() string   { return "Key " + c.key }
func (c *Client) bearerHeader() string { return "Bearer " + c.key }

// QueueRef captures the identifiers returned by a queue submission. It prefers
// the server-provided status/response URLs over reconstructed ones.
type QueueRef struct {
	Endpoint    string
	RequestID   string
	StatusURL   string
	ResponseURL string
}

type queueStatus struct {
	Status      string `json:"status"`
	RequestID   string `json:"request_id"`
	ResponseURL string `json:"response_url"`
	StatusURL   string `json:"status_url"`
}

type cdnToken struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"`
	BaseURL   string `json:"base_url"`
}

// ResolveInput returns a fal-hosted URL for a reference: remote refs pass
// through untouched, local files are uploaded first.
func (c *Client) ResolveInput(input string) (core.InputInfo, error) {
	if core.IsRemoteRef(input) {
		return core.InputInfo{Kind: "url", Source: input, ResolvedURL: input}, nil
	}
	fi, err := os.Stat(input)
	if err != nil || fi.IsDir() {
		return core.InputInfo{}, fmt.Errorf("input must be a local file or http(s) URL: %s", input)
	}
	core.Progress("uploading %s (%s)", input, core.MIMEForPath(input))
	url, err := c.UploadFile(input)
	if err != nil {
		return core.InputInfo{}, err
	}
	return core.InputInfo{Kind: "file", Source: input, ResolvedURL: url}, nil
}

// UploadFile pushes a local file to fal storage, trying three methods in order
// (fal v3 CDN, legacy CDN, GCS storage) and returning the first URL that works.
func (c *Client) UploadFile(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	name := filepath.Base(path)
	contentType := core.MIMEForPath(path)
	var errs []string

	if url, err := c.uploadV3(path, name, contentType, fi.Size()); err == nil {
		return url, nil
	} else {
		errs = append(errs, "fal_v3: "+err.Error())
	}
	bytesData, rerr := os.ReadFile(path)
	if rerr != nil {
		return "", fmt.Errorf("read %s: %w", path, rerr)
	}
	if url, err := c.uploadCDN(bytesData, name, contentType); err == nil {
		return url, nil
	} else {
		errs = append(errs, "cdn: "+err.Error())
	}
	if url, err := c.uploadStorage(bytesData, name, contentType); err == nil {
		return url, nil
	} else {
		errs = append(errs, "storage: "+err.Error())
	}
	return "", fmt.Errorf("all fal upload methods failed: %s", strings.Join(errs, " | "))
}

func (c *Client) cdnToken() (cdnToken, error) {
	url := strings.TrimRight(c.restURL, "/") + "/storage/auth/token?storage_type=fal-cdn-v3"
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return cdnToken{}, err
	}
	_, raw, err := core.DecodeJSON(resp)
	if err != nil {
		return cdnToken{}, err
	}
	var tok cdnToken
	if err := json.Unmarshal(raw, &tok); err != nil {
		return cdnToken{}, fmt.Errorf("decode CDN token: %w", err)
	}
	return tok, nil
}

func (c *Client) uploadV3(path, name, contentType string, size int64) (string, error) {
	tok, err := c.cdnToken()
	if err != nil {
		return "", err
	}
	if size > multipartThreshold {
		return c.uploadV3Multipart(path, name, contentType, tok, size)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(c.cdnURL, "/") + "/files/upload"
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	req.Header.Set("Authorization", tok.TokenType+" "+tok.Token)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Fal-File-Name", name)
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return "", err
	}
	parsed, _, err := core.DecodeJSON(resp)
	if err != nil {
		return "", err
	}
	return stringField(parsed, "access_url", "v3 upload missing access_url")
}

func (c *Client) uploadV3Multipart(path, name, contentType string, tok cdnToken, size int64) (string, error) {
	auth := tok.TokenType + " " + tok.Token
	createURL := strings.TrimRight(tok.BaseURL, "/") + "/files/upload/multipart"
	req, _ := http.NewRequest(http.MethodPost, createURL, nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Fal-File-Name", name)
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return "", err
	}
	created, _, err := core.DecodeJSON(resp)
	if err != nil {
		return "", err
	}
	accessURL, err := stringField(created, "access_url", "multipart missing access_url")
	if err != nil {
		return "", err
	}
	uploadID, err := stringField(created, "uploadId", "multipart missing uploadId")
	if err != nil {
		return "", err
	}

	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	type part struct {
		PartNumber int64  `json:"partNumber"`
		ETag       string `json:"etag"`
	}
	var parts []part
	buf := make([]byte, multipartChunkSize)
	for n := int64(1); ; n++ {
		read, rerr := io.ReadFull(f, buf)
		if read == 0 {
			if errors.Is(rerr, io.EOF) {
				break
			}
			if rerr != nil && !errors.Is(rerr, io.ErrUnexpectedEOF) {
				return "", rerr
			}
		}
		chunk := buf[:read]
		partURL := fmt.Sprintf("%s/multipart/%s/%d", strings.TrimRight(accessURL, "/"), uploadID, n)
		preq, _ := http.NewRequest(http.MethodPut, partURL, bytes.NewReader(chunk))
		preq.Header.Set("Authorization", auth)
		preq.Header.Set("Content-Type", contentType)
		preq.Header.Set("Accept-Encoding", "identity")
		preq.Header.Set("User-Agent", core.UserAgent())
		presp, perr := core.SharedClient.Do(preq)
		if perr != nil {
			return "", perr
		}
		etag := presp.Header.Get("etag")
		presp.Body.Close()
		if presp.StatusCode < 200 || presp.StatusCode >= 300 {
			return "", fmt.Errorf("multipart part %d failed: http %d", n, presp.StatusCode)
		}
		if etag == "" {
			return "", fmt.Errorf("multipart part %d missing etag", n)
		}
		parts = append(parts, part{PartNumber: n, ETag: etag})
		if errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF) {
			break
		}
	}

	completeURL := fmt.Sprintf("%s/multipart/%s/complete", strings.TrimRight(accessURL, "/"), uploadID)
	body, _ := json.Marshal(map[string]any{"parts": parts})
	creq, _ := http.NewRequest(http.MethodPost, completeURL, bytes.NewReader(body))
	creq.Header.Set("Authorization", auth)
	creq.Header.Set("Content-Type", "application/json")
	creq.Header.Set("User-Agent", core.UserAgent())
	cresp, err := core.SharedClient.Do(creq)
	if err != nil {
		return "", err
	}
	if _, _, err := core.DecodeJSON(cresp); err != nil {
		return "", err
	}
	return accessURL, nil
}

func (c *Client) uploadCDN(data []byte, name, contentType string) (string, error) {
	url := strings.TrimRight(c.cdnFallbackURL, "/") + "/files/upload"
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	req.Header.Set("Authorization", c.bearerHeader())
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Fal-File-Name", name)
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return "", err
	}
	parsed, _, err := core.DecodeJSON(resp)
	if err != nil {
		return "", err
	}
	return stringField(parsed, "access_url", "cdn upload missing access_url")
}

func (c *Client) uploadStorage(data []byte, name, contentType string) (string, error) {
	initURL := strings.TrimRight(c.restURL, "/") + "/storage/upload/initiate?storage_type=gcs"
	body, _ := json.Marshal(map[string]any{"file_name": name, "content_type": contentType})
	req, _ := http.NewRequest(http.MethodPost, initURL, bytes.NewReader(body))
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return "", err
	}
	init, _, err := core.DecodeJSON(resp)
	if err != nil {
		return "", err
	}
	uploadURL, err := stringField(init, "upload_url", "storage missing upload_url")
	if err != nil {
		return "", err
	}
	fileURL, err := stringField(init, "file_url", "storage missing file_url")
	if err != nil {
		return "", err
	}
	preq, _ := http.NewRequest(http.MethodPut, uploadURL, bytes.NewReader(data))
	preq.Header.Set("Content-Type", contentType)
	preq.Header.Set("User-Agent", core.UserAgent())
	presp, err := core.SharedClient.Do(preq)
	if err != nil {
		return "", err
	}
	presp.Body.Close()
	if presp.StatusCode < 200 || presp.StatusCode >= 300 {
		return "", fmt.Errorf("storage PUT failed: http %d", presp.StatusCode)
	}
	return fileURL, nil
}

// Submit posts a job body to a queue endpoint and returns its QueueRef.
func (c *Client) Submit(endpoint string, body any) (QueueRef, json.RawMessage, error) {
	url := strings.TrimRight(c.queueURL, "/") + "/" + strings.Trim(endpoint, "/")
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return QueueRef{}, nil, err
	}
	_, raw, err := core.DecodeJSON(resp)
	if err != nil {
		return QueueRef{}, raw, err
	}
	var qs queueStatus
	if err := json.Unmarshal(raw, &qs); err != nil {
		return QueueRef{}, raw, fmt.Errorf("decode queue status: %w", err)
	}
	return QueueRef{
		Endpoint:    endpoint,
		RequestID:   qs.RequestID,
		StatusURL:   qs.StatusURL,
		ResponseURL: qs.ResponseURL,
	}, raw, nil
}

// Status fetches the current queue status for a job.
func (c *Client) Status(q QueueRef) (map[string]any, json.RawMessage, error) {
	url := q.StatusURL
	if url == "" {
		url = fmt.Sprintf("%s/%s/requests/%s/status",
			strings.TrimRight(c.queueURL, "/"), strings.Trim(q.Endpoint, "/"), q.RequestID)
	}
	return c.get(url)
}

// Result fetches the completed job result.
func (c *Client) Result(q QueueRef) (map[string]any, json.RawMessage, error) {
	url := q.ResponseURL
	if url == "" {
		url = fmt.Sprintf("%s/%s/requests/%s",
			strings.TrimRight(c.queueURL, "/"), strings.Trim(q.Endpoint, "/"), q.RequestID)
	}
	return c.get(url)
}

func (c *Client) get(url string) (map[string]any, json.RawMessage, error) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	return core.DecodeJSON(resp)
}

// Wait polls a queue job until it reaches COMPLETED (returns the result) or
// FAILED (returns an error), bounded by maxWaitSecs.
func (c *Client) Wait(q QueueRef, maxWaitSecs, pollIntervalSecs int) (map[string]any, json.RawMessage, error) {
	deadline := time.Now().Add(time.Duration(maxWaitSecs) * time.Second)
	interval := time.Duration(max(pollIntervalSecs, 1)) * time.Second
	for {
		status, _, err := c.Status(q)
		if err != nil {
			return nil, nil, err
		}
		switch strings.ToUpper(asString(status["status"])) {
		case "COMPLETED":
			return c.Result(q)
		case "FAILED":
			raw, _ := json.Marshal(status)
			return nil, nil, fmt.Errorf("fal task failed: %s", string(raw))
		default:
			if time.Now().After(deadline) {
				return nil, nil, fmt.Errorf("timeout waiting for %s", q.RequestID)
			}
			time.Sleep(interval)
		}
	}
}

// Download streams a fal result URL to disk.
func (c *Client) Download(url, dest string) (int64, error) {
	return core.Download(url, dest, nil)
}

// ExtractMediaURL walks a result object looking for a downloadable media URL,
// checking the common fal shapes first (video.url, video_url, url, output) and
// falling back to a recursive scan for any string ending in a media extension.
func ExtractMediaURL(result map[string]any) (string, bool) {
	if v, ok := result["video"].(map[string]any); ok {
		if u := asString(v["url"]); isDownloadable(u) {
			return u, true
		}
	}
	for _, key := range []string{"video_url", "url", "output", "result", "file_url", "audio_url", "image_url"} {
		if u := asString(result[key]); isDownloadable(u) {
			return u, true
		}
	}
	return findMediaURL(result)
}

func findMediaURL(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		if isDownloadable(t) {
			return t, true
		}
	case map[string]any:
		for _, val := range t {
			if u, ok := findMediaURL(val); ok {
				return u, true
			}
		}
	case []any:
		for _, val := range t {
			if u, ok := findMediaURL(val); ok {
				return u, true
			}
		}
	}
	return "", false
}

func isDownloadable(s string) bool {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return false
	}
	lower := strings.ToLower(s)
	for _, ext := range []string{".mp4", ".mov", ".webm", ".mkv", ".avi", ".png", ".jpg", ".jpeg", ".webp", ".mp3", ".wav", ".m4a"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
}

func stringField(m map[string]any, key, missingMsg string) (string, error) {
	if s := asString(m[key]); s != "" {
		return s, nil
	}
	return "", errors.New(missingMsg)
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
