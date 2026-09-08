package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func imageEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"CODEX_LB_BASE_URL", "CODEX_LB_API_KEY", "CODEX_LB_EXHAUSTION_CODES", "OPENAI_BASE_URL", "OPENAI_API_URL"} {
		t.Setenv(name, "")
	}
	t.Setenv("OPENAI_API_KEY", "direct-key")
}

func TestImageBackendConfiguration(t *testing.T) {
	imageEnv(t)
	t.Setenv("OPENAI_API_URL", "https://legacy.example/v1/")
	b, err := resolveImageBackend()
	if err != nil || b.base != "https://legacy.example/v1" {
		t.Fatalf("legacy configuration: %+v %v", b, err)
	}
	t.Setenv("OPENAI_BASE_URL", "https://direct.example/custom/v1/")
	b, err = resolveImageBackend()
	if err != nil || b.base != "https://direct.example/custom/v1" {
		t.Fatalf("direct configuration: %+v %v", b, err)
	}
	t.Setenv("CODEX_LB_BASE_URL", "https://lb.example/v1")
	if _, err = resolveImageBackend(); err == nil {
		t.Fatal("partial configuration accepted")
	}
	t.Setenv("CODEX_LB_API_KEY", "lb-key")
	b, err = resolveImageBackend()
	if err != nil || b.name != "codex-lb" || b.key != "lb-key" {
		t.Fatalf("LB configuration: %+v %v", b, err)
	}
	for _, base := range []string{":bad", "file:///tmp/image", "https://user:pass@example.com/v1", "https://example.com/v1?token=secret"} {
		if _, err := checkedImageBackend("test", base, "key"); err == nil {
			t.Fatalf("accepted invalid base %q", base)
		}
	}
}

func TestImageGenerationAndEdits(t *testing.T) {
	for _, edit := range []bool{false, true} {
		t.Run(map[bool]string{false: "generation", true: "edit"}[edit], func(t *testing.T) {
			imageEnv(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				endpoint := "/custom/v1/images/generations"
				if edit {
					endpoint = "/custom/v1/images/edits"
					if err := r.ParseMultipartForm(1024 * 1024); err != nil {
						t.Error(err)
						return
					}
					if r.MultipartForm != nil {
						defer r.MultipartForm.RemoveAll()
					}
					if r.FormValue("background") != "transparent" || len(r.MultipartForm.File["image[]"]) != 1 {
						t.Error("missing multipart fields")
					}
				}
				if r.URL.Path != endpoint || r.Header.Get("Authorization") != "Bearer lb-key" {
					t.Errorf("wrong route or credential: %s", r.URL.Path)
				}
				io.WriteString(w, `{"data":[{"b64_json":"aW1hZ2U="}]}`)
			}))
			defer server.Close()
			t.Setenv("CODEX_LB_BASE_URL", server.URL+"/custom/v1/")
			t.Setenv("CODEX_LB_API_KEY", "lb-key")
			p := ImageParams{Prompt: "test", Background: "transparent"}
			if edit {
				ref := filepath.Join(t.TempDir(), "ref.png")
				if err := os.WriteFile(ref, []byte("reference"), 0600); err != nil {
					t.Fatal(err)
				}
				p.Refs = []string{ref}
			}
			res, err := Generate(p)
			if err != nil || res.Provider != "codex-lb" || string(res.Bytes) != "image" {
				t.Fatalf("generation: %+v %v", res, err)
			}
			if !strings.HasPrefix(res.Endpoint, "/custom/v1/images/") {
				t.Fatalf("incorrect endpoint metadata: %s", res.Endpoint)
			}
		})
	}
}

func TestImageFallback(t *testing.T) {
	for _, tc := range []struct {
		name, body, codes string
		fallback          bool
	}{
		{"explicit exhaustion", `{"error":{"code":"pool_empty"}}`, "pool_empty", true},
		{"no default", `{"error":{"code":"pool_empty"}}`, "", false},
		{"generic error", `{"error":{"code":"internal_error"}}`, "pool_empty", false},
		{"message only", `{"error":{"message":"pool_empty"}}`, "pool_empty", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imageEnv(t)
			var calls atomic.Int32
			direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer direct-key" {
					t.Error("LB credential leaked to direct backend")
				}
				io.WriteString(w, `{"data":[{"b64_json":"aW1hZ2U="}]}`)
			}))
			defer direct.Close()
			lb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				io.WriteString(w, tc.body)
			}))
			defer lb.Close()
			t.Setenv("CODEX_LB_BASE_URL", lb.URL+"/v1")
			t.Setenv("CODEX_LB_API_KEY", "lb-key")
			t.Setenv("CODEX_LB_EXHAUSTION_CODES", tc.codes)
			t.Setenv("OPENAI_BASE_URL", direct.URL+"/v1")
			res, err := Generate(ImageParams{Prompt: "test"})
			if tc.fallback {
				if err != nil || calls.Load() != 1 || res.Provider != "openai" {
					t.Fatalf("fallback: %+v %v, calls=%d", res, err, calls.Load())
				}
			} else if err == nil || calls.Load() != 0 {
				t.Fatalf("unsafe fallback: %v, calls=%d", err, calls.Load())
			}
		})
	}
}

func TestImageTimeoutAndRedirect(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "redirect"}[redirect], func(t *testing.T) {
			imageEnv(t)
			var calls atomic.Int32
			direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
			}))
			defer direct.Close()
			lb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if redirect {
					http.Redirect(w, r, direct.URL, http.StatusTemporaryRedirect)
					return
				}
				io.Copy(io.Discard, r.Body)
				select {
				case <-r.Context().Done():
				case <-time.After(time.Second):
				}
			}))
			defer lb.Close()
			t.Setenv("CODEX_LB_BASE_URL", lb.URL+"/v1")
			t.Setenv("CODEX_LB_API_KEY", "lb-key")
			t.Setenv("OPENAI_BASE_URL", direct.URL+"/v1")
			t.Setenv("CODEX_LB_EXHAUSTION_CODES", "pool_empty")
			_, err := Generate(ImageParams{Context: context.Background(), Timeout: 50 * time.Millisecond})
			if err == nil || calls.Load() != 0 {
				t.Fatalf("unsafe retry: %v, calls=%d", err, calls.Load())
			}
			if !redirect && !strings.Contains(err.Error(), "deadline exceeded") {
				t.Fatalf("unexpected timeout error: %v", err)
			}
		})
	}
}
