package core

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Download streams url to dest, creating parent directories as needed. The
// optional auth callback may set an Authorization header (e.g. for provider CDNs
// that gate downloads). Returns the number of bytes written.
func Download(url, dest string, auth func(*http.Request)) (int64, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", UserAgent())
	if auth != nil {
		auth(req)
	}
	resp, err := SharedClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
		return 0, fmt.Errorf("download http %d: %s", resp.StatusCode, string(body))
	}
	if dir := filepath.Dir(dest); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return 0, err
		}
	}
	f, err := os.Create(dest)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return io.Copy(f, resp.Body)
}

// WriteFile writes raw bytes to dest, creating parent directories. Used for
// provider responses that arrive as bytes (audio, base64-decoded images) rather
// than a downloadable URL.
func WriteFile(dest string, data []byte) error {
	if dir := filepath.Dir(dest); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(dest, data, 0o644)
}
