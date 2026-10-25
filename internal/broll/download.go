package broll

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
)

type Sidecar struct {
	Result
	FetchedAt       string `json:"fetched_at"`
	AttributionNote string `json:"attribution_note"`
	SHA256          string `json:"sha256"`
	Bytes           int64  `json:"bytes"`
}

type DownloadResult struct {
	Sidecar
	Out         string `json:"out"`
	SidecarPath string `json:"sidecar"`
}

// Download publishes the completed media and its sidecar without replacing
// existing files. A failed transfer leaves neither a partial clip nor provenance.
func (c *Client) Download(ctx context.Context, r Result, out string) (DownloadResult, error) {
	if err := validateDownload(r, out); err != nil {
		return DownloadResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return DownloadResult{}, err
	}
	media, err := os.CreateTemp(filepath.Dir(out), ".broll-media-*")
	if err != nil {
		return DownloadResult{}, err
	}
	defer os.Remove(media.Name())
	defer media.Close()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	n, hash, err := c.fetchMedia(ctx, r.DownloadURL, media)
	if err != nil {
		return DownloadResult{}, err
	}
	if err := media.Close(); err != nil {
		return DownloadResult{}, err
	}
	sidecar := Sidecar{Result: r, FetchedAt: time.Now().UTC().Format(time.RFC3339),
		AttributionNote: attribution(r), SHA256: hash, Bytes: n}
	if err := publishPair(media.Name(), out, sidecar); err != nil {
		return DownloadResult{}, err
	}
	return DownloadResult{Sidecar: sidecar, Out: out, SidecarPath: out + ".json"}, nil
}

func validateDownload(r Result, out string) error {
	if out == "" {
		return fmt.Errorf("--out is required")
	}
	if _, _, err := ResolveID(r.Provider, r.ID); err != nil {
		return err
	}
	if r.Provider == "pond5-pd" {
		return fmt.Errorf("pond5-pd: no verified public-domain download metadata; download refused")
	}
	if r.Licence == "" || !validURL(r.LicenceURL) || !validURL(r.PageURL) || !validURL(r.DownloadURL) || r.Width <= 0 || r.Height <= 0 || r.DurationS <= 0 {
		return fmt.Errorf("incomplete B-roll media or licence metadata; download refused")
	}
	for _, path := range []string{out, out + ".json"} {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("output already exists: %s; choose a new --out", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (c *Client) fetchMedia(ctx context.Context, endpoint string, dest io.Writer) (int64, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, "", fmt.Errorf("cannot create B-roll download request")
	}
	req.Header.Set("User-Agent", core.UserAgent())
	client := c.httpClient()
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("B-roll download failed (network error or timeout)")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("B-roll download HTTP %d", resp.StatusCode)
	}
	contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if contentType != "" && contentType != "video/mp4" && contentType != "application/octet-stream" && contentType != "binary/octet-stream" {
		return 0, "", fmt.Errorf("B-roll download is not an MP4 response")
	}
	var header [12]byte
	if _, err := io.ReadFull(resp.Body, header[:]); err != nil || string(header[4:8]) != "ftyp" {
		return 0, "", fmt.Errorf("B-roll download has no MP4 file signature")
	}
	hash := sha256.New()
	w := io.MultiWriter(dest, hash)
	if _, err := w.Write(header[:]); err != nil {
		return 0, "", err
	}
	n, err := io.Copy(w, resp.Body)
	if err != nil {
		return 0, "", fmt.Errorf("B-roll download interrupted; partial file discarded")
	}
	return n + int64(len(header)), hex.EncodeToString(hash.Sum(nil)), nil
}

func publishPair(mediaPath, out string, sidecar Sidecar) error {
	body, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(out), ".broll-sidecar-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(append(body, '\n'))
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	// Hard links make the no-overwrite rule atomic, including concurrent CLI
	// runs. Both temporary files are on the destination filesystem.
	if err := os.Link(mediaPath, out); err != nil {
		return err
	}
	if err := os.Link(f.Name(), out+".json"); err != nil {
		if cleanupErr := os.Remove(out); cleanupErr != nil {
			return fmt.Errorf("sidecar failed: %w; remove incomplete output %s: %v", err, out, cleanupErr)
		}
		return err
	}
	return nil
}

func attribution(r Result) string {
	credit := fmt.Sprintf("Footage by %s on %s (%s). ", r.Author, r.Provider, r.PageURL)
	if r.Provider == "pexels" {
		return credit + "Do not imply endorsement. No standalone resale. Check other rights before advertising use."
	}
	return credit + "No standalone resale or distribution. Check other rights before advertising use."
}
