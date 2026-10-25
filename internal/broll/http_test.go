package broll

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newJSONServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestAPIRedirectDoesNotForwardCredentials(t *testing.T) {
	second := newJSONServer(t, func(http.ResponseWriter, *http.Request) { t.Error("redirect followed") })
	first := newJSONServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL+"/?key=private-key", http.StatusFound)
	})
	c := &Client{HTTP: first.Client()}
	var out any
	err := c.getJSON(context.Background(), "pixabay", first.URL+"/?key=private-key", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "private-key") {
		t.Fatalf("unexpected redirect error: %v", err)
	}
}

func TestSuccessfulSecretEchoIsRefused(t *testing.T) {
	for _, body := range []string{
		`{"hits":[{"title":"private-key"}]}`,
		`{"hits":[],"next_page":"https://example.com?key=private-key"}`,
		`{"private-key":[]}`,
	} {
		server := newJSONServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		c := &Client{HTTP: server.Client(), CacheDir: t.TempDir()}
		var out any
		err := c.getJSON(context.Background(), "pixabay", server.URL+"/?key=private-key", nil, &out)
		if err == nil || strings.Contains(err.Error(), "private-key") || out != nil {
			t.Fatalf("credential not protected: out=%v, err=%v", out, err)
		}
	}
}

func TestVideoSelectionSafety(t *testing.T) {
	for _, file := range []videoFile{
		{URL: "file:///etc/passwd", MIME: "video/mp4", Width: 1920, Height: 1080},
		{URL: "https://user:secret@example.com/a.mp4", MIME: "video/mp4", Width: 1920, Height: 1080},
		{URL: "https://example.com/a.webm", MIME: "video/webm", Width: 1920, Height: 1080},
		{URL: "https://example.com/a.mp4", MIME: "video/mp4", Width: 0, Height: 1080},
	} {
		if _, ok := selectVideo([]videoFile{file}, "", "hd"); ok {
			t.Errorf("selected unsafe file %+v", file)
		}
	}
}
