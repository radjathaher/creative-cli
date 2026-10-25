package broll

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Pixabay requires a 24-hour response cache. Only API data is cached; API keys
// and credential-bearing pagination URLs are never persisted.
type responseCache struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Body      json.RawMessage `json:"body"`
}

func (c *Client) responsePath(provider, endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || c.CacheDir == "" || provider != "pixabay" {
		return ""
	}
	q := u.Query()
	q.Del("key")
	u.RawQuery = q.Encode()
	return filepath.Join(c.CacheDir, "responses", cacheName(u.String())+".json")
}

func cacheName(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:])
}

func (c *Client) cachedResponse(provider, endpoint string) []byte {
	path := c.responsePath(provider, endpoint)
	if path == "" {
		return nil
	}
	body, err := os.ReadFile(path)
	var cached responseCache
	if err != nil || json.Unmarshal(body, &cached) != nil {
		return nil
	}
	age := time.Since(cached.FetchedAt)
	if age < 0 || age >= 24*time.Hour {
		return nil
	}
	return cached.Body
}

func (c *Client) cacheResponse(provider, endpoint string, body []byte) error {
	path := c.responsePath(provider, endpoint)
	if path == "" {
		return nil
	}
	// Keep only documented Pixabay result fields. Other response fields can
	// contain signed navigation URLs or credentials echoed by a proxy.
	var response map[string]json.RawMessage
	if json.Unmarshal(body, &response) != nil {
		return nil
	}
	var hits []json.RawMessage
	if json.Unmarshal(response["hits"], &hits) != nil || hits == nil {
		return nil
	}
	filtered := map[string]json.RawMessage{}
	for _, key := range []string{"hits", "total", "totalHits"} {
		if value, ok := response[key]; ok {
			filtered[key] = value
		}
	}
	data, err := json.Marshal(filtered)
	if err != nil {
		return fmt.Errorf("pixabay: cannot encode response cache")
	}
	u, _ := url.Parse(endpoint)
	key := u.Query().Get("key")
	if key != "" {
		var value any
		_ = json.Unmarshal(data, &value)
		if containsSecret(value, key) {
			return fmt.Errorf("pixabay: response contained an API credential; cache refused")
		}
	}
	data, err = json.Marshal(responseCache{FetchedAt: time.Now().UTC(), Body: data})
	if err != nil {
		return fmt.Errorf("pixabay: cannot encode response cache")
	}
	return writeCache(path, data)
}

func (c *Client) rememberQuery(r Result) error {
	if c.CacheDir == "" {
		return nil
	}
	data, err := json.Marshal(r.Query)
	if err != nil {
		return err
	}
	return writeCache(filepath.Join(c.CacheDir, "queries", cacheName(r.ID)+".json"), data)
}

func (c *Client) recallQuery(id string) string {
	if c.CacheDir == "" {
		return ""
	}
	body, err := os.ReadFile(filepath.Join(c.CacheDir, "queries", cacheName(id)+".json"))
	var query string
	if err == nil {
		_ = json.Unmarshal(body, &query)
	}
	return query
}

func writeCache(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("cannot create B-roll cache: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".broll-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
