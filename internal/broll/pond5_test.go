package broll

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPond5SearchQuery(t *testing.T) {
	for _, query := range []string{"old printing press", "tired older man", "history & art + film"} {
		t.Run(query, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				want := url.Values{"kw": {query}, "free": {"1"}, "mt": {"footage"}}
				if r.Method != http.MethodGet || !reflect.DeepEqual(r.URL.Query(), want) {
					t.Errorf("request = %s %s; want GET with %v", r.Method, r.URL, want)
				}
				if r.Header.Get("Authorization") != "" {
					t.Error("Pond5 request must not send credentials")
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"items":[]}`)
			}))
			defer server.Close()
			client := &Client{HTTP: server.Client(), Pond5URL: server.URL}
			results, err := client.searchPond5(context.Background(), SearchOptions{
				Query: query, Orientation: "portrait", Quality: "hd", MinDuration: 5, MaxDuration: 10, Limit: 20,
			})
			if err == nil || results != nil || !strings.Contains(err.Error(), "schema is unverified") {
				t.Fatalf("unverified response: results=%v err=%v", results, err)
			}
			if calls.Load() != 1 {
				t.Errorf("requests = %d; want 1", calls.Load())
			}
		})
	}
}

func TestPond5SearchFailsClosed(t *testing.T) {
	cases := []struct {
		name, contentType, body, errorPart string
		status                             int
	}{
		{"blocked", "text/html", "<html>private-response-marker: enable JS</html>", "403", http.StatusForbidden},
		{"missing", "application/json", `{"error":"private-response-marker"}`, "404", http.StatusNotFound},
		{"html-success", "text/html", "<html>private-response-marker</html>", "cannot verify", http.StatusOK},
		{"unknown-json", "application/json", `{"results":[]}`, "schema is unverified", http.StatusOK},
		{"non-public-domain", "application/json", `{"items":[{"id":"123","public_domain":false,"licence":"paid","download_url":"https://invalid.example/clip.mp4"}]}`, "schema is unverified", http.StatusOK},
		{"untrusted-public-domain-claim", "application/json", `{"items":[{"id":"123","public_domain":true,"licence":"public domain","download_url":"https://invalid.example/clip.mp4"}]}`, "schema is unverified", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client := &Client{HTTP: server.Client(), Pond5URL: server.URL}
			results, err := client.searchPond5(context.Background(), SearchOptions{Query: "old printing press"})
			if err == nil || results != nil {
				t.Fatalf("results=%v err=%v; want no results and an error", results, err)
			}
			if !strings.Contains(err.Error(), tc.errorPart) || !strings.Contains(err.Error(), "cannot verify per-item public-domain licence") {
				t.Errorf("unexpected error: %v", err)
			}
			if strings.Contains(err.Error(), "private-response-marker") {
				t.Errorf("error exposed response body: %v", err)
			}
		})
	}
}

func TestPond5DownloadDoesNotGuessEndpoint(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), Pond5URL: server.URL}
	result, err := client.getPond5(context.Background(), "123", "hd")
	if err == nil || !reflect.DeepEqual(result, Result{}) {
		t.Fatalf("result=%v err=%v; want empty result and an error", result, err)
	}
	if !strings.Contains(err.Error(), "no verified item metadata or download endpoint") {
		t.Errorf("unexpected error: %v", err)
	}
	if calls.Load() != 0 {
		t.Errorf("unverified download made %d requests", calls.Load())
	}
}
