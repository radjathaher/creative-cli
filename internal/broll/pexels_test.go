package broll

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const pexelsFixture = `{
  "id":42,"width":2160,"height":3840,"duration":12,
  "url":"https://www.pexels.com/video/older-man-at-desk-42/",
  "image":"https://images.pexels.com/42.jpg",
  "user":{"name":"Alex","url":"https://www.pexels.com/@alex"},
  "video_files":[
    {"quality":"hd","file_type":"video/mp4","width":1920,"height":1080,"link":"https://cdn.example/landscape.mp4"},
    {"quality":"hd","file_type":"video/mp4","width":1080,"height":1920,"link":"https://cdn.example/hd.mp4"},
    {"quality":"hd","file_type":"video/mp4","width":720,"height":1280,"link":"https://cdn.example/720.mp4"},
    {"quality":"sd","file_type":"video/mp4","width":540,"height":960,"link":"https://cdn.example/sd.mp4"},
    {"quality":"hd","file_type":"video/mp4","width":2160,"height":3840,"link":"https://cdn.example/4k.mp4"},
    {"quality":"hd","file_type":"video/mp4","width":4320,"height":7680,"link":"https://cdn.example/8k.mp4"},
    {"quality":"hd","file_type":"video/mp4","width":1000,"height":1000,"link":"https://cdn.example/square.mp4"},
    {"quality":"hd","file_type":"video/webm","width":1080,"height":1920,"link":"https://cdn.example/not-mp4.webm"},
    {"quality":"hls","file_type":"application/x-mpegURL","width":null,"height":null,"link":"https://cdn.example/list.m3u8"}
  ]
}`

func pexelsTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Client{
		HTTP: server.Client(), PexelsURL: server.URL + "/v1/videos",
		ReadSecret: func(name string) (string, error) {
			if name != "PEXELS_API_KEY" {
				t.Errorf("secret name = %q", name)
			}
			return "fixture-secret-do-not-print", nil
		},
	}
}

func TestPexelsSearchRequestAndMetadata(t *testing.T) {
	requests := 0
	c := pexelsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/v1/videos/search" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "fixture-secret-do-not-print" || r.Header.Get("User-Agent") == "" {
			t.Error("required headers are missing or changed")
		}
		q := r.URL.Query()
		if q.Get("query") != "older man & desk" || q.Get("orientation") != "portrait" || q.Get("per_page") != "80" {
			t.Errorf("query parameters = %v", q)
		}
		if q.Get("key") != "" || q.Get("min_duration") != "" || q.Get("max_duration") != "" {
			t.Error("search includes unsupported or secret query parameters")
		}
		fmt.Fprintf(w, `{"videos":[%s,{"id":43,"video_files":[]}],"next_page":"/must-not-follow"}`, pexelsFixture)
	})
	results, err := c.searchPexels(context.Background(), SearchOptions{
		Query: "older man & desk", Orientation: "portrait", Limit: 100, MinDuration: 5, MaxDuration: 15,
	})
	if err != nil || len(results) != 1 || requests != 1 {
		t.Fatalf("results = %+v, requests = %d, error = %v", results, requests, err)
	}
	r := results[0]
	if r.ID != "pexels:42" || r.Provider != "pexels" || r.Title != "older man at desk" || r.DurationS != 12 {
		t.Errorf("identity = %+v", r)
	}
	if r.Width != 1080 || r.Height != 1920 || r.Orientation != "portrait" || r.DownloadURL != "https://cdn.example/hd.mp4" {
		t.Errorf("selected file = %+v", r)
	}
	if r.Author != "Alex" || r.AuthorURL != "https://www.pexels.com/@alex" || r.PageURL != "https://www.pexels.com/video/older-man-at-desk-42/" {
		t.Errorf("attribution = %+v", r)
	}
	if r.Licence != "Pexels License" || r.LicenceURL != "https://www.pexels.com/license/" || r.CostUSD != 0 || r.Query != "older man & desk" {
		t.Errorf("provenance = %+v", r)
	}
	if r.PreviewURL != r.DownloadURL || r.ThumbnailURL != "https://images.pexels.com/42.jpg" {
		t.Errorf("preview = %+v", r)
	}
}

func TestPexelsSearchVariants(t *testing.T) {
	for _, tc := range []struct{ name, orientation, quality, file string }{
		{"default", "portrait", "", "hd"},
		{"sd", "portrait", "sd", "sd"},
		{"hd", "portrait", "hd", "hd"},
		{"4k", "portrait", "4k", "4k"},
		{"best", "portrait", "best", "8k"},
		{"square", "square", "hd", "square"},
		{"landscape", "landscape", "hd", "landscape"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := pexelsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("per_page") != "20" {
					t.Error("default limit is not 20")
				}
				fmt.Fprintf(w, `{"videos":[%s]}`, pexelsFixture)
			})
			results, err := c.searchPexels(context.Background(), SearchOptions{
				Query: "man", Orientation: tc.orientation, Quality: tc.quality,
			})
			if err != nil || len(results) != 1 {
				t.Fatalf("results = %+v, error = %v", results, err)
			}
			if results[0].DownloadURL != "https://cdn.example/"+tc.file+".mp4" {
				t.Errorf("variant = %s", results[0].DownloadURL)
			}
		})
	}
}

func TestPexelsGet(t *testing.T) {
	c := pexelsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/videos/videos/42" || r.URL.RawQuery != "" {
			t.Errorf("detail path = %s", r.URL.String())
		}
		if r.Header.Get("Authorization") != "fixture-secret-do-not-print" {
			t.Error("detail authorization missing")
		}
		fmt.Fprint(w, pexelsFixture)
	})
	r, err := c.getPexels(context.Background(), "42", "4k")
	if err != nil || r.ID != "pexels:42" || r.Width != 2160 || r.Height != 3840 || r.DownloadURL != "https://cdn.example/4k.mp4" {
		t.Fatalf("result = %+v, error = %v", r, err)
	}
	if r.Query != "" {
		t.Error("detail invented a search query")
	}
}

func TestPexelsErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"unauthorized", "fixture-secret-do-not-print", "401", 401},
		{"rate-limited", "fixture-secret-do-not-print", "429", 429},
		{"invalid-json", "fixture-secret-do-not-print", "", 200},
		{"wrong-id", `{"id":43}`, "ID", 200},
		{"no-variant", `{"id":42,"duration":12,"url":"https://www.pexels.com/video/42/","video_files":[]}`, "MP4", 200},
		{"incomplete-metadata", `{"id":42,"video_files":[]}`, "missing or invalid", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			c := pexelsTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				requests++
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			_, err := c.getPexels(context.Background(), "42", "hd")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if requests != 1 || strings.Contains(err.Error(), "fixture-secret-do-not-print") {
				t.Errorf("requests = %d, unsafe error = %v", requests, err)
			}
		})
	}
}

func TestPexelsMissingCredentialAndInvalidID(t *testing.T) {
	requests := 0
	c := pexelsTestClient(t, func(http.ResponseWriter, *http.Request) { requests++ })
	c.ReadSecret = func(string) (string, error) {
		return "", errors.New("fixture-secret-do-not-print")
	}
	_, err := c.searchPexels(context.Background(), SearchOptions{Query: "man"})
	if err == nil || !strings.Contains(err.Error(), "PEXELS_API_KEY") || strings.Contains(err.Error(), "fixture-secret-do-not-print") {
		t.Fatalf("credential error = %v", err)
	}
	for _, id := range []string{"0", "-1", "../../secret", "42?key=fixture-secret-do-not-print"} {
		if _, err := c.getPexels(context.Background(), id, "hd"); err == nil {
			t.Errorf("invalid ID accepted: %q", id)
		}
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0", requests)
	}
}

func TestPexelsNoPlayableResults(t *testing.T) {
	c := pexelsTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"videos":[
			{"id":1,"duration":12,"url":"https://www.pexels.com/video/1/","video_files":[{"file_type":"video/webm","width":1280,"height":720,"link":"https://cdn.example/a.webm"}]},
			{"id":2,"duration":12,"url":"https://www.pexels.com/video/2/","video_files":[{"file_type":"video/mp4","width":3840,"height":2160,"link":"https://cdn.example/b.mp4"}]},
			{"id":0,"duration":12,"url":"https://www.pexels.com/video/0/","video_files":[{"file_type":"video/mp4","width":1280,"height":720,"link":"https://cdn.example/c.mp4"}]}
		]}`)
	})
	results, err := c.searchPexels(context.Background(), SearchOptions{Query: "man"})
	if err != nil || results == nil || len(results) != 0 {
		t.Fatalf("results = %+v, error = %v", results, err)
	}
}

func TestPexelsFallbackTitle(t *testing.T) {
	for _, page := range []string{"", "https://www.pexels.com/video/42/", "https://example.com/fake-title-42/", ":invalid"} {
		v := pexelsVideo{ID: 42, URL: page}
		if title := v.title(); title != "Pexels video 42" {
			t.Errorf("title = %q for %q", title, page)
		}
	}
}

func TestPexelsSearchRejectsUnexpectedPayload(t *testing.T) {
	c := pexelsTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"error":"unexpected provider payload"}`)
	})
	_, err := c.searchPexels(context.Background(), SearchOptions{Query: "man"})
	if err == nil || !strings.Contains(err.Error(), "videos array") {
		t.Fatalf("payload error = %v", err)
	}
}

func TestPexelsSearchRejectsEchoedCredential(t *testing.T) {
	c := pexelsTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"error":"fixture-secret-do-not-print"}`)
	})
	_, err := c.searchPexels(context.Background(), SearchOptions{Query: "man"})
	if err == nil || !strings.Contains(err.Error(), "credential") || strings.Contains(err.Error(), "fixture-secret-do-not-print") {
		t.Fatalf("credential error = %v", err)
	}
}

func TestPexelsSearchDropsIncompleteMetadata(t *testing.T) {
	for _, fixture := range []string{
		strings.Replace(pexelsFixture, `"duration":12`, `"duration":0`, 1),
		strings.Replace(pexelsFixture, "https://www.pexels.com/video/older-man-at-desk-42/", "file:///tmp/source", 1),
	} {
		c := pexelsTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `{"videos":[%s]}`, fixture)
		})
		results, err := c.searchPexels(context.Background(), SearchOptions{Query: "man"})
		if err != nil || len(results) != 0 {
			t.Fatalf("results = %+v, error = %v", results, err)
		}
	}
}

type pexelsFailTransport struct{}

func (pexelsFailTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport failure: fixture-secret-do-not-print")
}

func TestPexelsTransportErrorsAreSafe(t *testing.T) {
	c := pexelsTestClient(t, func(http.ResponseWriter, *http.Request) {
		t.Error("transport should not contact the server")
	})
	c.HTTP = &http.Client{Transport: pexelsFailTransport{}}
	_, err := c.searchPexels(context.Background(), SearchOptions{Query: "man"})
	if err == nil || strings.Contains(err.Error(), "fixture-secret-do-not-print") {
		t.Fatalf("transport error = %v", err)
	}
}
