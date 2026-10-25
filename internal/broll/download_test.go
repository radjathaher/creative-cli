package broll

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testMP4 = []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom")

func downloadFixture(url string) Result {
	return Result{ID: "pexels:42", Provider: "pexels", Title: "printing press", DurationS: 5,
		Width: 1280, Height: 720, Orientation: "landscape", PageURL: "https://www.pexels.com/video/press-42/",
		Author: "Example Author", AuthorURL: "https://www.pexels.com/@author/", DownloadURL: url,
		Licence: "Pexels License", LicenceURL: "https://www.pexels.com/license/", Query: "old printing press"}
}

func TestDownloadWritesSidecarAndChecksum(t *testing.T) {
	server := newJSONServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("User-Agent") == "" {
			t.Error("download headers do not match contract")
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(testMP4)
	})
	c := &Client{HTTP: server.Client()}
	out := filepath.Join(t.TempDir(), "clips", "press.mp4")
	r := downloadFixture(server.URL)
	result, err := c.Download(context.Background(), r, out)
	if err != nil {
		t.Fatal(err)
	}
	media, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(media, testMP4) {
		t.Fatalf("media differs: %v", err)
	}
	body, err := os.ReadFile(out + ".json")
	var sidecar Sidecar
	if err != nil || json.Unmarshal(body, &sidecar) != nil {
		t.Fatalf("sidecar unreadable: %v", err)
	}
	hash := sha256.Sum256(testMP4)
	if sidecar.Result != r || sidecar.Bytes != int64(len(testMP4)) || sidecar.SHA256 != hex.EncodeToString(hash[:]) {
		t.Fatalf("sidecar metadata differs: %+v", sidecar)
	}
	if _, err := time.Parse(time.RFC3339, sidecar.FetchedAt); err != nil || !strings.HasSuffix(sidecar.FetchedAt, "Z") {
		t.Fatalf("fetched_at must be UTC: %q", sidecar.FetchedAt)
	}
	if !strings.Contains(sidecar.AttributionNote, "Do not imply endorsement") || !strings.Contains(sidecar.AttributionNote, "standalone resale") {
		t.Fatalf("licence note missing: %q", sidecar.AttributionNote)
	}
	if result.Out != out || result.SidecarPath != out+".json" || result.CostUSD != 0 {
		t.Fatalf("download result: %+v", result)
	}
	assertNoTemporaryFiles(t, filepath.Dir(out))
}

func TestDownloadFailureLeavesNoArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
	}{
		{"http-error", "text/plain", "secret-body", 403},
		{"html", "text/html", "<html>login</html>", 200},
		{"fake-mp4", "video/mp4", "<html>login</html>", 200},
		{"empty", "application/octet-stream", "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newJSONServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			c := &Client{HTTP: server.Client()}
			out := filepath.Join(t.TempDir(), "press.mp4")
			if _, err := c.Download(context.Background(), downloadFixture(server.URL), out); err == nil {
				t.Fatal("failed download accepted")
			}
			entries, err := os.ReadDir(filepath.Dir(out))
			if err != nil || len(entries) != 0 {
				t.Fatalf("failure left artifacts: %v, %v", entries, err)
			}
		})
	}
}

func TestDownloadNeverOverwrites(t *testing.T) {
	server := newJSONServer(t, func(http.ResponseWriter, *http.Request) { t.Error("existing output caused a request") })
	c := &Client{HTTP: server.Client()}
	for _, suffix := range []string{"", ".json"} {
		out := filepath.Join(t.TempDir(), "press.mp4")
		if err := os.WriteFile(out+suffix, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Download(context.Background(), downloadFixture(server.URL), out); err == nil {
			t.Fatal("existing output accepted")
		}
		body, _ := os.ReadFile(out + suffix)
		if string(body) != "keep" {
			t.Fatal("output overwritten")
		}
	}
}

func TestInterruptedDownloadLeavesNoArtifacts(t *testing.T) {
	server := newJSONServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write(testMP4)
	})
	c := &Client{HTTP: server.Client()}
	out := filepath.Join(t.TempDir(), "press.mp4")
	if _, err := c.Download(context.Background(), downloadFixture(server.URL), out); err == nil {
		t.Fatal("truncated download accepted")
	}
	files, _ := os.ReadDir(filepath.Dir(out))
	if len(files) != 0 {
		t.Fatalf("truncated download left files: %v", files)
	}
}

func TestSidecarCommitFailureRollsBackMedia(t *testing.T) {
	dir := t.TempDir()
	media, out := filepath.Join(dir, "temporary"), filepath.Join(dir, "press.mp4")
	if err := os.WriteFile(media, testMP4, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out+".json", []byte("other writer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publishPair(media, out, Sidecar{}); err == nil {
		t.Fatal("concurrent sidecar overwrite allowed")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("media not rolled back after sidecar failure")
	}
	body, _ := os.ReadFile(out + ".json")
	if string(body) != "other writer" {
		t.Fatal("other writer's sidecar modified")
	}
	assertNoTemporaryFiles(t, dir)
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, ".broll-*"))
	if len(files) != 0 {
		t.Fatalf("temporary files left behind: %v", files)
	}
}
