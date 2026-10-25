package broll

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const pixabayTestKey = "fixture-only-secret+with&symbols"

const pixabayFixture = `{
  "total": 500, "totalHits": 500,
  "hits": [
    {
      "id": 125, "pageURL": "https://pixabay.com/videos/id-125/",
      "tags": "flowers, yellow, blossom", "duration": 12,
      "user": "Coverr-Free-Footage", "user_id": 1281706,
      "videos": {
        "large": {"url": "https://cdn.example/landscape-4k.mp4", "width": 3840, "height": 2160, "thumbnail": "https://cdn.example/landscape-4k.jpg"},
        "medium": {"url": "https://cdn.example/landscape-hd.mp4", "width": 1920, "height": 1080, "thumbnail": "https://cdn.example/landscape-hd.jpg"},
        "small": {"url": "https://cdn.example/landscape-720.mp4", "width": 1280, "height": 720},
        "tiny": {"url": "https://cdn.example/landscape-sd.mp4", "width": 640, "height": 360, "thumbnail": "https://cdn.example/landscape-sd.jpg"}
      }
    },
    {
      "id": 126, "pageURL": "https://pixabay.com/videos/id-126/",
      "tags": "portrait, flowers", "duration": 8.5,
      "user": "Jane Doe", "user_id": 42,
      "videos": {
        "large": {"url": "", "width": 0, "height": 0},
        "medium": {"url": "https://cdn.example/portrait-hd.mp4", "width": 1080, "height": 1920, "thumbnail": "https://cdn.example/portrait-hd.jpg"}
      }
    },
    {
      "id": 127, "pageURL": "https://pixabay.com/videos/id-127/",
      "tags": "square, flowers", "duration": 6,
      "videos": {"medium": {"url": "https://cdn.example/square.mp4", "width": 1080, "height": 1080}}
    },
    {"id": 0, "duration": 12, "pageURL": "https://pixabay.com/videos/id-0/", "videos": {"medium": {"url": "https://cdn.example/invalid.mp4", "width": 1920, "height": 1080}}},
    {"id": 128, "duration": 12, "pageURL": "https://pixabay.com/videos/id-128/", "videos": {"medium": {"url": "", "width": 1920, "height": 1080}}},
    {"id": 129, "duration": 12, "pageURL": "https://pixabay.com/videos/id-129/", "videos": {"medium": {"url": "https://cdn.example/invalid.mp4", "width": 0, "height": 0}}}
  ]
}`

func pixabayTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Client{
		HTTP: server.Client(), PixabayURL: server.URL + "/api/videos/", CacheDir: t.TempDir(),
		ReadSecret: func(name string) (string, error) {
			if name != "PIXABAY_API_KEY" {
				t.Errorf("secret name = %q", name)
			}
			return pixabayTestKey, nil
		},
	}
}

func TestPixabaySearchRequestAndMetadata(t *testing.T) {
	requests := 0
	query := "yellow flowers & café"
	c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/videos/" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("key") != pixabayTestKey || q.Get("q") != query || q.Get("per_page") != "20" {
			t.Error("query, credential, or page size did not survive URL encoding")
		}
		if q.Get("page") != "" || q.Get("orientation") != "" || r.Header.Get("Authorization") != "" {
			t.Error("unexpected pagination, unsupported orientation parameter, or authorization header")
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing user agent")
		}
		io.WriteString(w, pixabayFixture)
	})
	results, err := c.searchPixabay(context.Background(), SearchOptions{Query: query, Limit: 20})
	if err != nil || len(results) != 3 {
		t.Fatalf("search returned %d results, error %v", len(results), err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one", requests)
	}
	r := results[0]
	if r.ID != "pixabay:125" || r.Provider != "pixabay" || r.Title != "flowers, yellow, blossom" || r.DurationS != 12 {
		t.Errorf("incorrect video metadata: %+v", r)
	}
	if r.Width != 1920 || r.Height != 1080 || r.Orientation != "landscape" {
		t.Errorf("metadata does not match HD rendition: %+v", r)
	}
	if r.DownloadURL != "https://cdn.example/landscape-hd.mp4" || r.PreviewURL != r.DownloadURL || r.ThumbnailURL != "https://cdn.example/landscape-hd.jpg" {
		t.Errorf("incorrect rendition URLs: %+v", r)
	}
	if r.PageURL != "https://pixabay.com/videos/id-125/" || r.Author != "Coverr-Free-Footage" || r.AuthorURL != "https://pixabay.com/users/Coverr-Free-Footage-1281706/" {
		t.Errorf("incorrect provenance: %+v", r)
	}
	if r.Licence != "Pixabay Content License" || r.LicenceURL != "https://pixabay.com/service/license-summary/" || r.CostUSD != 0 || r.Query != query {
		t.Errorf("incorrect licence, price, or query: %+v", r)
	}
	if results[1].AuthorURL != "https://pixabay.com/users/Jane%20Doe-42/" || results[2].AuthorURL != "" {
		t.Error("author URL must use escaped source metadata or remain empty")
	}
}

func TestPixabaySearchOrientationAndQuality(t *testing.T) {
	tests := []struct {
		name, orientation, quality, id, suffix string
		width, height                          int
	}{
		{"default-hd", "landscape", "", "125", "landscape-hd.mp4", 1920, 1080},
		{"explicit-hd", "landscape", "hd", "125", "landscape-hd.mp4", 1920, 1080},
		{"sd", "landscape", "sd", "125", "landscape-sd.mp4", 640, 360},
		{"4k", "landscape", "4k", "125", "landscape-4k.mp4", 3840, 2160},
		{"best", "landscape", "best", "125", "landscape-4k.mp4", 3840, 2160},
		{"portrait", "portrait", "hd", "126", "portrait-hd.mp4", 1080, 1920},
		{"square", "square", "hd", "127", "square.mp4", 1080, 1080},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, pixabayFixture) })
			results, err := c.searchPixabay(context.Background(), SearchOptions{Query: "flowers", Orientation: tt.orientation, Quality: tt.quality})
			if err != nil || len(results) != 1 {
				t.Fatalf("search returned %d results, error %v", len(results), err)
			}
			r := results[0]
			if r.ID != "pixabay:"+tt.id || r.Width != tt.width || r.Height != tt.height || !strings.HasSuffix(r.DownloadURL, tt.suffix) {
				t.Errorf("incorrect rendition: %+v", r)
			}
		})
	}
}

func TestPixabaySearchPageSize(t *testing.T) {
	for _, tt := range []struct{ limit, want int }{{0, 20}, {1, 3}, {2, 3}, {3, 3}, {20, 20}, {200, 200}, {201, 200}} {
		t.Run(fmt.Sprint(tt.limit), func(t *testing.T) {
			requests := 0
			c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Query().Get("per_page") != fmt.Sprint(tt.want) {
					t.Errorf("per_page = %q, want %d", r.URL.Query().Get("per_page"), tt.want)
				}
				io.WriteString(w, `{"total": 0, "hits": []}`)
			})
			results, err := c.searchPixabay(context.Background(), SearchOptions{Query: "flowers", Limit: tt.limit})
			if err != nil || results == nil || len(results) != 0 || requests != 1 {
				t.Fatalf("empty results = %#v, requests = %d, error %v", results, requests, err)
			}
		})
	}
}

func TestPixabayQueryLength(t *testing.T) {
	requests := 0
	c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		io.WriteString(w, `{"hits": []}`)
	})
	if _, err := c.searchPixabay(context.Background(), SearchOptions{Query: strings.Repeat("é", 100)}); err != nil {
		t.Fatalf("100 Unicode characters rejected: %v", err)
	}
	if _, err := c.searchPixabay(context.Background(), SearchOptions{Query: strings.Repeat("é", 101)}); err == nil {
		t.Fatal("overlong query accepted")
	}
	if requests != 1 {
		t.Errorf("invalid query reached API: %d requests", requests)
	}
}

func TestPixabayGetByID(t *testing.T) {
	requests := 0
	c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		q := r.URL.Query()
		if q.Get("id") != "126" || q.Get("key") != pixabayTestKey || q.Get("q") != "" {
			t.Error("detail request must contain ID and key, not a search query")
		}
		io.WriteString(w, pixabayFixture)
	})
	r, err := c.getPixabay(context.Background(), "126", "hd")
	if err != nil || r.ID != "pixabay:126" || r.Width != 1080 || r.Height != 1920 || r.DurationS != 8.5 || r.Query != "" || requests != 1 {
		t.Fatalf("detail = %+v, requests = %d, error %v", r, requests, err)
	}
}

func TestPixabayGetRejectsInvalidResponses(t *testing.T) {
	for _, id := range []string{"999", "128", "129"} {
		t.Run(id, func(t *testing.T) {
			c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, pixabayFixture) })
			if _, err := c.getPixabay(context.Background(), id, "hd"); err == nil {
				t.Fatal("missing ID or missing rendition accepted")
			}
		})
	}
	for _, id := range []string{"", "abc", "0", "-1", "1?key=bad"} {
		c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid ID reached API") })
		if _, err := c.getPixabay(context.Background(), id, "hd"); err == nil {
			t.Errorf("invalid ID %q accepted", id)
		}
	}
}

func TestPixabayDefaultEndpoint(t *testing.T) {
	c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") })
	c.PixabayURL = ""
	c.HTTP = &http.Client{Transport: pixabayRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "pixabay.com" || r.URL.Path != "/api/videos/" {
			t.Error("wrong default API endpoint")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"hits": []}`)), Header: http.Header{}}, nil
	})}
	if _, err := c.searchPixabay(context.Background(), SearchOptions{Query: "flowers"}); err != nil {
		t.Fatal(err)
	}
}

func TestPixabayRejectsMissingResults(t *testing.T) {
	for _, payload := range []string{`{}`, `null`, `{"error": "unauthorized"}`, `{"hits": null}`} {
		t.Run(payload, func(t *testing.T) {
			c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, payload) })
			if _, err := c.searchPixabay(context.Background(), SearchOptions{Query: "flowers"}); err == nil {
				t.Error("missing results accepted as successful search")
			}
			if _, err := c.getPixabay(context.Background(), "125", "hd"); err == nil {
				t.Error("missing results accepted as successful detail")
			}
		})
	}
}

func TestPixabayRejectsUnusableMetadata(t *testing.T) {
	for _, invalid := range []string{`"duration": 0`, `"duration": -1`, `"pageURL": ""`, `"pageURL": "file:///tmp/video"`} {
		t.Run(invalid, func(t *testing.T) {
			original := `"duration": 12`
			if strings.HasPrefix(invalid, `"pageURL"`) {
				original = `"pageURL": "https://pixabay.com/videos/id-125/"`
			}
			payload := strings.Replace(pixabayFixture, original, invalid, 1)
			c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, payload) })
			results, err := c.searchPixabay(context.Background(), SearchOptions{Query: "flowers"})
			if err != nil || len(results) != 2 {
				t.Errorf("invalid metadata not filtered: %d results, error %v", len(results), err)
			}
			if _, err := c.getPixabay(context.Background(), "125", "hd"); err == nil {
				t.Error("invalid metadata accepted for download")
			}
		})
	}
}

func TestPixabayErrorsDoNotExposeKeys(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				io.WriteString(w, "reflected key="+pixabayTestKey)
			})
			_, err := c.searchPixabay(context.Background(), SearchOptions{Query: "flowers"})
			assertPixabaySafeError(t, err)
		})
	}
	t.Run("invalid-json", func(t *testing.T) {
		c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "invalid "+pixabayTestKey) })
		_, err := c.getPixabay(context.Background(), "125", "hd")
		assertPixabaySafeError(t, err)
	})
	t.Run("transport", func(t *testing.T) {
		c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") })
		c.HTTP = &http.Client{Transport: pixabayRoundTripper(func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("failed at " + r.URL.String() + " secret=" + pixabayTestKey)
		})}
		_, err := c.searchPixabay(context.Background(), SearchOptions{Query: "flowers"})
		assertPixabaySafeError(t, err)
	})
	t.Run("endpoint", func(t *testing.T) {
		c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") })
		c.PixabayURL = "https://bad%host/?key=" + pixabayTestKey
		_, err := c.searchPixabay(context.Background(), SearchOptions{Query: "flowers"})
		assertPixabaySafeError(t, err)
	})
	for _, key := range []string{"", "   ", pixabayTestKey} {
		c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("missing key reached API") })
		c.ReadSecret = func(string) (string, error) { return key, errors.New("secret error " + pixabayTestKey) }
		_, err := c.searchPixabay(context.Background(), SearchOptions{Query: "flowers"})
		assertPixabaySafeError(t, err)
	}
	for _, key := range []string{"", "   "} {
		c := pixabayTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("blank key reached API") })
		c.ReadSecret = func(string) (string, error) { return key, nil }
		_, err := c.searchPixabay(context.Background(), SearchOptions{Query: "flowers"})
		assertPixabaySafeError(t, err)
	}
}

func assertPixabaySafeError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, forbidden := range []string{pixabayTestKey, url.QueryEscape(pixabayTestKey), "key=", "http://", "https://"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Error("error disclosed a request URL, response body, or secret")
		}
	}
}

type pixabayRoundTripper func(*http.Request) (*http.Response, error)

func (f pixabayRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
