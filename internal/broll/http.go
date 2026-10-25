package broll

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/radjathaher/creative-cli/internal/core"
)

// getJSON never includes request URLs, response bodies, or transport errors in
// errors: Pixabay credentials travel in the query string and may be echoed.
func (c *Client) getJSON(ctx context.Context, provider, endpoint string, headers map[string]string, out any) error {
	if body := c.cachedResponse(provider, endpoint); body != nil && json.Unmarshal(body, out) == nil {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%s: cannot create API request", provider)
	}
	req.Header.Set("User-Agent", core.UserAgent())
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	client := c.httpClient()
	// API redirects can carry credentials to another origin. Require an
	// explicit endpoint correction instead of forwarding a key automatically.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s: API request canceled or timed out", provider)
		}
		return fmt.Errorf("%s: API request failed (network or TLS error)", provider)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: API HTTP %d; no provider fallback", provider, resp.StatusCode)
	}
	const maxResponse = 16 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(body) > maxResponse {
		return fmt.Errorf("%s: cannot read API response (incomplete or too large)", provider)
	}
	var payload any
	if json.Unmarshal(body, &payload) != nil {
		return fmt.Errorf("%s: invalid JSON response; expected API data, not a login or challenge page", provider)
	}
	for _, secret := range []string{req.URL.Query().Get("key"), req.Header.Get("Authorization")} {
		if secret != "" && containsSecret(payload, secret) {
			return fmt.Errorf("%s: response contained an API credential; output refused", provider)
		}
	}
	if json.Unmarshal(body, out) != nil {
		return fmt.Errorf("%s: unexpected API response schema", provider)
	}
	return c.cacheResponse(provider, endpoint, body)
}

func containsSecret(value any, secret string) bool {
	switch v := value.(type) {
	case string:
		return strings.Contains(v, secret) || strings.Contains(v, url.QueryEscape(secret)) || strings.Contains(v, url.PathEscape(secret))
	case []any:
		for _, item := range v {
			if containsSecret(item, secret) {
				return true
			}
		}
	case map[string]any:
		for key, item := range v {
			if containsSecret(key, secret) || containsSecret(item, secret) {
				return true
			}
		}
	}
	return false
}

func (c *Client) httpClient() http.Client {
	if c.HTTP != nil {
		return *c.HTTP
	}
	return *core.SharedClient
}
