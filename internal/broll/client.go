package broll

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

func ValidateProvider(provider string) error {
	switch provider {
	case "pexels", "pixabay", "pond5-pd":
		return nil
	case "":
		return fmt.Errorf("--provider is required: pexels ($0), pixabay ($0), pond5-pd (unavailable); no automatic fallback")
	default:
		return fmt.Errorf("unknown B-roll provider %q; choose pexels, pixabay, or pond5-pd", provider)
	}
}

// ResolveID rejects conflicting selectors before credentials or files are read.
func ResolveID(provider, id string) (string, string, error) {
	if prefix, value, ok := strings.Cut(id, ":"); ok {
		if provider != "" && prefix != provider {
			return "", "", fmt.Errorf("ID provider %q conflicts with --provider %q", prefix, provider)
		}
		provider, id = prefix, value
	}
	if err := ValidateProvider(provider); err != nil {
		return "", "", err
	}
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != id {
		return "", "", fmt.Errorf("B-roll ID must be a positive integer, optionally prefixed by provider:")
	}
	return provider, id, nil
}

func validateQuality(quality string) error {
	switch quality {
	case "", "hd", "sd", "4k", "best":
		return nil
	default:
		return fmt.Errorf("--quality must be hd, sd, 4k, or best")
	}
}

func validateSearch(o SearchOptions) error {
	if strings.TrimSpace(o.Query) == "" {
		return fmt.Errorf("--query is required")
	}
	if o.Orientation != "" && o.Orientation != "portrait" && o.Orientation != "landscape" && o.Orientation != "square" {
		return fmt.Errorf("--orientation must be portrait, landscape, or square")
	}
	if o.Limit < 1 || o.Limit > 200 {
		return fmt.Errorf("--limit must be between 1 and 200 (Pexels maximum: 80)")
	}
	for _, v := range []float64{o.MinDuration, o.MaxDuration} {
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("duration bounds must be finite, non-negative seconds")
		}
	}
	if o.MaxDuration > 0 && o.MaxDuration < o.MinDuration {
		return fmt.Errorf("--max-duration must be at least --min-duration")
	}
	return validateQuality(o.Quality)
}

func (c *Client) Search(ctx context.Context, provider string, o SearchOptions) ([]Result, error) {
	if err := ValidateProvider(provider); err != nil {
		return nil, err
	}
	if err := validateSearch(o); err != nil {
		return nil, err
	}
	if provider == "pexels" && o.Limit > 80 {
		return nil, fmt.Errorf("Pexels --limit cannot exceed 80; search makes at most one request")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var results []Result
	var err error
	switch provider {
	case "pexels":
		results, err = c.searchPexels(ctx, o)
	case "pixabay":
		results, err = c.searchPixabay(ctx, o)
	case "pond5-pd":
		results, err = c.searchPond5(ctx, o)
	}
	if err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(results))
	for _, r := range results {
		if r.DurationS < o.MinDuration || (o.MaxDuration > 0 && r.DurationS > o.MaxDuration) {
			continue
		}
		if o.Orientation != "" && r.Orientation != o.Orientation {
			continue
		}
		r.Query = o.Query
		out = append(out, r)
		if err := c.rememberQuery(r); err != nil {
			return nil, err
		}
		if len(out) == o.Limit {
			break
		}
	}
	return out, nil
}

func (c *Client) Get(ctx context.Context, provider, id, quality string) (Result, error) {
	provider, id, err := ResolveID(provider, id)
	if err != nil {
		return Result{}, err
	}
	if err := validateQuality(quality); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var r Result
	switch provider {
	case "pexels":
		r, err = c.getPexels(ctx, id, quality)
	case "pixabay":
		r, err = c.getPixabay(ctx, id, quality)
	case "pond5-pd":
		r, err = c.getPond5(ctx, id, quality)
	}
	if err == nil {
		r.Query = c.recallQuery(r.ID)
	}
	return r, err
}
