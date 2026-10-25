package broll

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveID(t *testing.T) {
	for _, tc := range []struct{ provider, input, wantProvider, wantID string }{
		{"", "pexels:42", "pexels", "42"}, {"pixabay", "125", "pixabay", "125"},
		{"pond5-pd", "pond5-pd:1", "pond5-pd", "1"},
	} {
		provider, id, err := ResolveID(tc.provider, tc.input)
		if err != nil || provider != tc.wantProvider || id != tc.wantID {
			t.Fatalf("ResolveID(%q, %q) = %q, %q, %v", tc.provider, tc.input, provider, id, err)
		}
	}
	for _, tc := range []struct{ provider, id string }{
		{"", "123"}, {"pixabay", "pexels:123"}, {"other", "123"}, {"pexels", ""},
		{"pexels", "../file"}, {"pexels", "0"}, {"pexels", "-1"}, {"pexels", "+1"},
		{"pexels", "01"}, {"pexels", "1:2"}, {"", ":1"},
	} {
		if _, _, err := ResolveID(tc.provider, tc.id); err == nil {
			t.Errorf("accepted invalid selectors %+v", tc)
		}
	}
}

func TestSearchValidation(t *testing.T) {
	valid := SearchOptions{Query: "printing press", Limit: 20}
	for _, mutate := range []func(*SearchOptions){
		func(o *SearchOptions) { o.Query = " " },
		func(o *SearchOptions) { o.Orientation = "tall" },
		func(o *SearchOptions) { o.Limit = 0 },
		func(o *SearchOptions) { o.Limit = 201 },
		func(o *SearchOptions) { o.MinDuration = -1 },
		func(o *SearchOptions) { o.MinDuration = math.NaN() },
		func(o *SearchOptions) { o.MaxDuration = math.Inf(1) },
		func(o *SearchOptions) { o.MinDuration, o.MaxDuration = 10, 5 },
		func(o *SearchOptions) { o.Quality = "ultra" },
	} {
		o := valid
		mutate(&o)
		if err := validateSearch(o); err == nil {
			t.Errorf("accepted invalid search: %+v", o)
		}
	}
	if err := validateSearch(valid); err != nil {
		t.Fatal(err)
	}
	c := &Client{}
	if _, err := c.Search(context.Background(), "", valid); err == nil {
		t.Fatal("missing provider accepted")
	}
	valid.Limit = 81
	if _, err := c.Search(context.Background(), "pexels", valid); err == nil {
		t.Fatal("Pexels limit accepted above 80")
	}
}

func TestSearchFiltersAndRemembersQuery(t *testing.T) {
	c := pexelsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		videos := []map[string]any{}
		for i, duration := range []float64{4, 5, 10, 11} {
			videos = append(videos, map[string]any{
				"id": i + 1, "duration": duration, "url": "https://www.pexels.com/video/press-2/",
				"video_files": []map[string]any{{"file_type": "video/mp4", "width": 1280, "height": 720, "link": "https://cdn.example/clip.mp4"}},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"videos": videos})
	})
	c.CacheDir = t.TempDir()
	results, err := c.Search(context.Background(), "pexels", SearchOptions{
		Query: "printing press", Orientation: "landscape", MinDuration: 5, MaxDuration: 10, Limit: 20,
	})
	if err != nil || len(results) != 2 || results[0].DurationS != 5 || results[1].DurationS != 10 {
		t.Fatalf("filtered results = %+v, %v", results, err)
	}
	if c.recallQuery("pexels:2") != "printing press" {
		t.Fatal("query not persisted")
	}
	results, err = c.Search(context.Background(), "pexels", SearchOptions{Query: "none", MinDuration: 50, Limit: 20})
	if err != nil || results == nil || len(results) != 0 {
		t.Fatalf("empty results must be an array: %+v, %v", results, err)
	}
}

func TestPixabayCacheExpiryAndNoSecrets(t *testing.T) {
	calls := 0
	c := &Client{CacheDir: t.TempDir()}
	server := newJSONServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"hits":[],"total":0,"totalHits":0}`)
	})
	c.HTTP = server.Client()
	endpoint := server.URL + "/?key=fixture-private-key&q=press"
	for i := 0; i < 2; i++ {
		var response map[string]any
		if err := c.getJSON(context.Background(), "pixabay", endpoint, nil, &response); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("calls = %d, expected cached response", calls)
	}
	path := c.responsePath("pixabay", endpoint)
	body, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(body), "fixture-private-key") || strings.Contains(path, "fixture-private-key") {
		t.Fatalf("cache missing or exposed credential: %v", err)
	}
	var entry responseCache
	if err := json.Unmarshal(body, &entry); err != nil {
		t.Fatal(err)
	}
	entry.FetchedAt = time.Now().Add(-25 * time.Hour)
	body, _ = json.Marshal(entry)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := c.getJSON(context.Background(), "pixabay", endpoint, nil, &response); err != nil || calls != 2 {
		t.Fatalf("expired response not fetched: %d calls, %v", calls, err)
	}
	if c.responsePath("pexels", endpoint) != "" {
		t.Fatal("unexpected Pexels response cache")
	}
}

func TestFailedResponseIsNotCached(t *testing.T) {
	server := newJSONServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, r.URL.String())
	})
	c := &Client{HTTP: server.Client(), CacheDir: t.TempDir()}
	var result any
	err := c.getJSON(context.Background(), "pixabay", server.URL+"/?key=private-key", nil, &result)
	if err == nil || !strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "private-key") {
		t.Fatalf("unsafe error: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(c.CacheDir, "responses", "*.json"))
	if len(files) != 0 {
		t.Fatal("cached unsuccessful response")
	}
}

func TestPixabayInvalidSchemaDoesNotPoisonCache(t *testing.T) {
	for _, invalid := range []string{`{}`, `null`, `{"error":"temporary"}`, `{"hits":null}`, `{"hits":"invalid"}`} {
		t.Run(invalid, func(t *testing.T) {
			calls := 0
			server := newJSONServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					fmt.Fprint(w, invalid)
					return
				}
				fmt.Fprint(w, `{"hits":[]}`)
			})
			c := &Client{HTTP: server.Client(), CacheDir: t.TempDir(), PixabayURL: server.URL,
				ReadSecret: func(string) (string, error) { return "fixture-private-key", nil }}
			if _, err := c.searchPixabay(context.Background(), SearchOptions{Query: "press", Limit: 20}); err == nil {
				t.Fatal("invalid schema accepted")
			}
			results, err := c.searchPixabay(context.Background(), SearchOptions{Query: "press", Limit: 20})
			if err != nil || results == nil || calls != 2 {
				t.Fatalf("recovery blocked: calls=%d, results=%v, err=%v", calls, results, err)
			}
		})
	}
}
