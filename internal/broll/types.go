// Package broll searches stock footage and records the source and licence of
// each download. Provider selection is explicit; errors never trigger fallback.
package broll

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/radjathaher/creative-cli/internal/core"
)

type SearchOptions struct {
	Query, Orientation, Quality string
	MinDuration, MaxDuration    float64
	Limit                       int
}

// Result describes the selected MP4, not the provider's original resolution.
type Result struct {
	ID           string  `json:"id"`
	Provider     string  `json:"provider"`
	Title        string  `json:"title"`
	DurationS    float64 `json:"duration_s"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	Orientation  string  `json:"orientation"`
	PreviewURL   string  `json:"preview_url"`
	ThumbnailURL string  `json:"thumbnail_url"`
	PageURL      string  `json:"page_url"`
	Author       string  `json:"author"`
	AuthorURL    string  `json:"author_url"`
	Licence      string  `json:"licence"`
	LicenceURL   string  `json:"licence_url"`
	CostUSD      float64 `json:"cost_usd"`
	DownloadURL  string  `json:"download_url"`
	Query        string  `json:"query,omitempty"`
}

type Client struct {
	HTTP                            *http.Client
	PexelsURL, PixabayURL, Pond5URL string
	ReadSecret                      func(string) (string, error)
	CacheDir                        string
}

func NewClient() *Client {
	base, err := os.UserCacheDir()
	cache := ""
	if err == nil {
		cache = filepath.Join(base, "creative", "broll")
	}
	return &Client{
		HTTP: core.SharedClient, ReadSecret: core.ReadSecret, CacheDir: cache,
		PexelsURL:  "https://api.pexels.com/v1/videos",
		PixabayURL: "https://pixabay.com/api/videos/",
		Pond5URL:   "https://www.pond5.com/api/v2/search",
	}
}
