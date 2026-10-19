// Package sync builds the route catalog from provider sources. Each source
// fetches one provider's catalog and prices using only free, unauthenticated or
// read-only endpoints; it never calls a generation endpoint.
package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/registry"
)

// Source fetches every route one provider offers. Implementations classify
// each route with Classify and fill Price and Caps as far as their data allows;
// a route with an unreadable price keeps Source "unknown" rather than being dropped.
type Source interface {
	Name() string
	Fetch(ctx context.Context) ([]registry.Route, error)
}

// Today is the as-of date stamped on prices fetched in this run.
func Today() string { return time.Now().UTC().Format("2006-01-02") }

// GetJSON fetches url with optional headers and decodes the body into out.
// Non-2xx responses become errors that include the status and a body excerpt.
func GetJSON(ctx context.Context, url string, headers map[string]string, out any) error {
	body, err := Get(ctx, url, headers)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s: %w", url, err)
	}
	return nil
}

// Get fetches url and returns the raw body.
func Get(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", core.UserAgent())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		excerpt := body
		if len(excerpt) > 300 {
			excerpt = excerpt[:300]
		}
		return nil, &HTTPError{Status: resp.StatusCode, URL: url, Body: string(excerpt)}
	}
	return body, nil
}

// HTTPError lets callers react to rate limits (429) with backoff.
type HTTPError struct {
	Status int
	URL    string
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http %d from %s: %s", e.Status, e.URL, e.Body)
}

// NewRoute fills the identity fields shared by every source.
func NewRoute(provider, modelID, title string) registry.Route {
	return registry.Route{
		ID:       provider + ":" + modelID,
		Provider: provider,
		ModelID:  modelID,
		Title:    title,
		Price:    registry.Price{Unit: registry.UnitUnknown, Source: registry.SourceUnknown},
	}
}
