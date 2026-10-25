package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/radjathaher/creative-cli/internal/broll"
	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/registry"
)

const brollCLIKey = "cli-fixture-pexels-key"

const brollCLIVideo = `{"id":42,"width":3840,"height":2160,"duration":8,
"url":"https://www.pexels.com/video/old-printing-press-42/",
"image":"https://images.pexels.com/42.jpg",
"user":{"name":"Example Author","url":"https://www.pexels.com/@example"},
"video_files":[{"file_type":"video/mp4","quality":"hd","width":1280,"height":720,
"link":"https://cdn.broll.test/clip.mp4"}]}`

// This synthetic fixture contains an MP4 signature, not playable footage.
var brollCLIMP4 = []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom")

type brollCLITransport func(*http.Request) (*http.Response, error)

func (f brollCLITransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func setBrollCLITransport(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	t.Setenv("PEXELS_API_KEY", brollCLIKey)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	previous := core.SharedClient
	core.SharedClient = &http.Client{Transport: transport}
	t.Cleanup(func() { core.SharedClient = previous })
}

func routeBrollCLIToFixture(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	setBrollCLITransport(t, brollCLITransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.pexels.com" && r.URL.Host != "cdn.broll.test" {
			t.Errorf("unexpected external host: %s", r.URL.Host)
			return nil, errors.New("fixture refuses external network")
		}
		request := r.Clone(r.Context())
		localURL := *r.URL
		localURL.Scheme, localURL.Host = target.Scheme, target.Host
		request.URL, request.Host = &localURL, target.Host
		return server.Client().Transport.RoundTrip(request)
	}))
}

func executeBrollCLI(args ...string) (string, error) {
	root := NewRootCmd()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs(args)
	err := root.Execute()
	return output.String(), err
}

func TestBrollCommandRegistration(t *testing.T) {
	for _, name := range []string{registry.OpBrollSearch, registry.OpBrollGet} {
		count := 0
		for _, command := range NewRootCmd().Commands() {
			if command.Name() != name {
				continue
			}
			count++
			if command.Flags().Lookup("provider") == nil || command.Flags().Lookup("quality") == nil {
				t.Errorf("%s is missing B-roll flags", name)
			}
			for _, flag := range []string{"family", "route", "prompt", "dry-run", "max-cost"} {
				if command.Flags().Lookup(flag) != nil {
					t.Errorf("%s has generic generation flag --%s", name, flag)
				}
			}
		}
		if count != 1 {
			t.Errorf("%s registrations = %d, want 1", name, count)
		}
	}
}

func TestBrollInvalidArgumentsNeverUseNetwork(t *testing.T) {
	var requests atomic.Int32
	setBrollCLITransport(t, brollCLITransport(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("fixture forbids network")
	}))
	out := filepath.Join(t.TempDir(), "must-not-exist.mp4")
	for _, tc := range []struct {
		name, want string
		args       []string
	}{
		{"missing-provider", "--provider is required", []string{"broll.search", "--query", "press"}},
		{"unknown-provider", "unknown B-roll provider", []string{"broll.search", "--query", "press", "--provider", "other"}},
		{"missing-query", "--query is required", []string{"broll.search", "--provider", "pexels"}},
		{"bad-orientation", "--orientation", []string{"broll.search", "--query", "press", "--provider", "pexels", "--orientation", "diagonal"}},
		{"bad-limit", "--limit", []string{"broll.search", "--query", "press", "--provider", "pexels", "--limit", "0"}},
		{"pexels-limit", "cannot exceed 80", []string{"broll.search", "--query", "press", "--provider", "pexels", "--limit", "81"}},
		{"negative-duration", "duration bounds", []string{"broll.search", "--query", "press", "--provider", "pexels", "--min-duration", "-1"}},
		{"reversed-duration", "--max-duration", []string{"broll.search", "--query", "press", "--provider", "pexels", "--min-duration", "10", "--max-duration", "5"}},
		{"bad-quality", "--quality", []string{"broll.search", "--query", "press", "--provider", "pexels", "--quality", "ultra"}},
		{"get-missing-provider", "--provider is required", []string{"broll.get", "42", "--out", out}},
		{"get-conflicting-provider", "conflicts", []string{"broll.get", "pexels:42", "--provider", "pixabay", "--out", out}},
		{"get-malformed-id", "positive integer", []string{"broll.get", "pexels:../42", "--out", out}},
		{"get-missing-out", "--out is required", []string{"broll.get", "pexels:42"}},
		{"get-bad-quality", "--quality", []string{"broll.get", "pexels:42", "--out", out, "--quality", "ultra"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := executeBrollCLI(tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) || output != "" {
				t.Fatalf("output = %q, error = %v; want %q", output, err, tc.want)
			}
		})
	}
	if requests.Load() != 0 {
		t.Errorf("invalid arguments caused %d requests", requests.Load())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("invalid arguments created output")
	}
}

func TestBrollSearchJSONAndTable(t *testing.T) {
	var requests atomic.Int32
	routeBrollCLIToFixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1/videos/search" || r.Header.Get("Authorization") != brollCLIKey {
			t.Error("search path or authorization differs")
		}
		if r.URL.Query().Get("query") == "empty" {
			fmt.Fprint(w, `{"videos":[]}`)
			return
		}
		if r.URL.Query().Get("per_page") != "2" || r.URL.Query().Get("orientation") != "landscape" {
			t.Error("search flags did not reach the provider")
		}
		fmt.Fprintf(w, `{"videos":[%s]}`, brollCLIVideo)
	})
	args := []string{"broll.search", "--provider", "pexels", "--query", "old printing press", "--orientation", "landscape", "--min-duration", "5", "--max-duration", "10", "--limit", "2"}
	for _, format := range []string{"json", "pretty", "table"} {
		t.Run(format, func(t *testing.T) {
			command := append([]string{}, args...)
			if format != "json" {
				command = append(command, "--"+format)
			}
			output, err := executeBrollCLI(command...)
			if err != nil || strings.Contains(output, brollCLIKey) {
				t.Fatalf("command failed: %v", err)
			}
			if format == "table" {
				for _, cell := range []string{"ID", "TITLE", "pexels:42", "1280x720", "$0", "Pexels License", "Example Author"} {
					if !strings.Contains(output, cell) {
						t.Errorf("table is missing %q: %s", cell, output)
					}
				}
				return
			}
			var results []broll.Result
			if json.Unmarshal([]byte(output), &results) != nil || len(results) != 1 || results[0].ID != "pexels:42" || results[0].CostUSD != 0 {
				t.Fatalf("invalid JSON result array: %s", output)
			}
			if format == "pretty" && !strings.Contains(output, "\n  {") {
				t.Error("--pretty did not indent the JSON array")
			}
		})
	}
	output, err := executeBrollCLI("broll.search", "--provider", "pexels", "--query", "empty")
	if err != nil || strings.TrimSpace(output) != "[]" || requests.Load() != 4 {
		t.Fatalf("empty output = %q, requests = %d, error = %v", output, requests.Load(), err)
	}
}

func TestBrollPrefixedGetWritesMediaAndSidecar(t *testing.T) {
	var requests atomic.Int32
	routeBrollCLIToFixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/clip.mp4" {
			if r.Header.Get("Authorization") != "" {
				t.Error("API credential leaked to the media host")
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(brollCLIMP4)
			return
		}
		switch r.URL.Path {
		case "/v1/videos/search":
			fmt.Fprintf(w, `{"videos":[%s]}`, brollCLIVideo)
		case "/v1/videos/videos/42":
			fmt.Fprint(w, brollCLIVideo)
		default:
			t.Errorf("unexpected fixture path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	if _, err := executeBrollCLI("broll.search", "--provider", "pexels", "--query", "old printing press"); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "clips", "press.mp4")
	output, err := executeBrollCLI("broll.get", "pexels:42", "--out", out)
	if err != nil {
		t.Fatal(err)
	}
	media, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(media, brollCLIMP4) {
		t.Fatalf("media differs: %v", err)
	}
	body, err := os.ReadFile(out + ".json")
	var sidecar broll.Sidecar
	var receipt broll.DownloadResult
	if err != nil || json.Unmarshal(body, &sidecar) != nil || json.Unmarshal([]byte(output), &receipt) != nil {
		t.Fatalf("sidecar or receipt is invalid: %v", err)
	}
	hash := sha256.Sum256(brollCLIMP4)
	if receipt.Out != out || receipt.SidecarPath != out+".json" || receipt.Sidecar != sidecar || sidecar.SHA256 != hex.EncodeToString(hash[:]) || sidecar.Bytes != int64(len(media)) {
		t.Error("receipt, checksum, or sidecar differs")
	}
	if sidecar.ID != "pexels:42" || sidecar.Provider != "pexels" || sidecar.Query != "old printing press" || sidecar.Licence != "Pexels License" || sidecar.Author != "Example Author" || sidecar.Width != 1280 || sidecar.Height != 720 || sidecar.DurationS != 8 {
		t.Errorf("sidecar metadata differs: %+v", sidecar)
	}
	if _, err := time.Parse(time.RFC3339, sidecar.FetchedAt); err != nil || !strings.HasSuffix(sidecar.FetchedAt, "Z") || !strings.Contains(sidecar.AttributionNote, "Do not imply endorsement") {
		t.Error("sidecar timestamp or attribution note is missing")
	}
	if strings.Contains(string(body)+output, brollCLIKey) || requests.Load() != 3 {
		t.Errorf("secret disclosure or unexpected request count: %d", requests.Load())
	}
}

func TestBrollRunnableRoutes(t *testing.T) {
	for _, op := range []string{registry.OpBrollSearch, registry.OpBrollGet} {
		for _, provider := range []string{"pexels", "pixabay", "pond5-pd"} {
			route := registry.Route{Op: op, Provider: provider, Transport: "broll-http"}
			if got, want := runnableRoute(route), provider != "pond5-pd"; got != want {
				t.Errorf("%s %s runnable = %v, want %v", provider, op, got, want)
			}
			route.Transport = "other"
			if runnableRoute(route) {
				t.Errorf("%s %s accepted an unsupported transport", provider, op)
			}
		}
	}
}
