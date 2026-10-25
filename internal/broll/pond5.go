package broll

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

const pond5EditorialHelpURL = "https://www.pond5.com/help/en/articles/10086064-where-can-i-find-free-footage-photos-and-music"

func (c *Client) searchPond5(ctx context.Context, o SearchOptions) ([]Result, error) {
	u, err := url.Parse(c.Pond5URL)
	if err != nil {
		return nil, fmt.Errorf("pond5-pd search unavailable: invalid endpoint; cannot verify per-item public-domain licence")
	}
	query := u.Query()
	query.Set("kw", o.Query)
	query.Set("free", "1")
	query.Set("mt", "footage")
	u.RawQuery = query.Encode()
	var response json.RawMessage
	if err := c.getJSON(ctx, "pond5-pd", u.String(), nil, &response); err != nil {
		return nil, fmt.Errorf("pond5-pd search unavailable: cannot verify per-item public-domain licence: %w", err)
	}
	// A successful response alone cannot establish a clip's licence or download rights.
	return nil, fmt.Errorf("pond5-pd search unavailable: response schema is unverified; cannot verify per-item public-domain licence; Pond5 identifies Public Domain Project videos as editorial-only: %s", pond5EditorialHelpURL)
}

func (c *Client) getPond5(_ context.Context, _, _ string) (Result, error) {
	return Result{}, fmt.Errorf("pond5-pd download unavailable: no verified item metadata or download endpoint; cannot verify per-item public-domain licence; Pond5 identifies Public Domain Project videos as editorial-only: %s", pond5EditorialHelpURL)
}
