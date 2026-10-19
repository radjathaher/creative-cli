package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/registry"
)

// Fal syncs fal.ai. The model catalog and the per-endpoint OpenAPI specs and
// model pages are public; the pricing endpoint needs FAL_KEY but is free and
// read-only. Without a key every route is still listed, priced as unknown.
type Fal struct {
	APIBase     string        // https://api.fal.ai
	OpenAPIBase string        // queue OpenAPI per endpoint, keyed by ?endpoint_id=
	PageBase    string        // model pages; <base>/<endpoint>/llms.txt holds pricing prose
	Key         string        // overrides FAL_KEY when set
	BatchSize   int           // endpoint ids per pricing request; the API rejects more than 50
	Workers     int           // concurrent schema and page fetches
	Pause       time.Duration // between pricing requests; the API allows about one per 4 s
	Backoff     time.Duration // first retry delay after a 429

	// PriorUnpriced lists endpoint ids the pricing API rejected on an earlier
	// sync. One unknown id fails its whole batch with 404, and isolating it by
	// bisection costs about a dozen rate-limited requests, so these ids are
	// priced last, together, where a failure cannot slow the healthy batches.
	PriorUnpriced map[string]bool

	// Prior holds fal routes from the previous catalog; unless Full is set,
	// routes priced within 7 days are reused instead of refetched.
	Prior []registry.Route
	Full  bool

	// Warnings collects non-fatal problems (a missing key, endpoints the
	// pricing API rejected, schema fetch failures) for the sync report.
	Warnings []string
}

// NewFal returns a Fal source pointed at the production endpoints.
func NewFal() *Fal {
	return &Fal{
		APIBase:     "https://api.fal.ai",
		OpenAPIBase: "https://fal.ai/api/openapi/queue/openapi.json",
		PageBase:    "https://fal.ai/models",
		BatchSize:   50,
		Workers:     4,
		Pause:       4 * time.Second,
		Backoff:     4 * time.Second,
	}
}

func (f *Fal) Name() string { return "fal" }

// detailFamilies are the allowlisted image and video families whose schema,
// caps, and page prices are fetched; other modalities qualify by op instead.
var detailFamilies = map[string]bool{
	"gpt-image-2": true, "gpt-image-2.5": true, "seedance-2.5": true, "gemini-omni-flash-1.1": true,
}

func needsDetail(r registry.Route) bool {
	if r.Deprecated {
		return false
	}
	if detailFamilies[r.Family] {
		return true
	}
	switch registry.Modality(r.Op) {
	case "3d", "stt", "sfx":
		return true
	}
	return r.Op == registry.OpVideoUpscale
}

type falModel struct {
	EndpointID string `json:"endpoint_id"`
	Metadata   struct {
		DisplayName string `json:"display_name"`
		Category    string `json:"category"`
		Status      string `json:"status"`
	} `json:"metadata"`
}

type falPrice struct {
	EndpointID string   `json:"endpoint_id"`
	UnitPrice  *float64 `json:"unit_price"`
	Unit       *string  `json:"unit"`
	Currency   string   `json:"currency"`
}

// Fetch lists every fal endpoint, prices it, and enriches the detail subset
// with schema, caps, param map, and page-derived price tiers. Only a failed
// catalog listing is fatal.
func (f *Fal) Fetch(ctx context.Context) ([]registry.Route, error) {
	models, err := f.listModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("fal catalog: %w", err)
	}
	routes := make([]registry.Route, 0, len(models))
	for _, m := range models {
		r := NewRoute("fal", m.EndpointID, m.Metadata.DisplayName)
		r.Op, r.Family, r.Variant = Classify("fal", m.Metadata.Category, m.EndpointID, m.Metadata.DisplayName)
		r.Transport = "fal-queue"
		r.Deprecated = m.Metadata.Status != "" && m.Metadata.Status != "active"
		r.SourceURL = f.PageBase + "/" + m.EndpointID
		routes = append(routes, r)
	}
	reused := f.reusePrior(routes)

	key := f.Key
	if key == "" {
		key, _ = core.ReadSecret("FAL_KEY")
	}
	if key == "" {
		f.warn("FAL_KEY missing; fal prices left unknown")
	} else {
		var ids, suspect []string
		for i, r := range routes {
			if reused[i] {
				continue
			}
			if f.PriorUnpriced[r.ModelID] {
				suspect = append(suspect, r.ModelID)
			} else {
				ids = append(ids, r.ModelID)
			}
		}
		prices := f.fetchPrices(ctx, key, ids)
		for id, p := range f.fetchPrices(ctx, key, suspect) {
			prices[id] = p
		}
		for i := range routes {
			if p, ok := prices[routes[i].ModelID]; ok {
				routes[i].Price = falUnitPrice(p, routes[i].Family, f.APIBase+"/v1/models/pricing?endpoint_id="+url.QueryEscape(p.EndpointID))
			} else if !reused[i] {
				// Stamp the attempt so a later incremental sync treats this
				// unpriced endpoint as fresh instead of re-bisecting it.
				routes[i].Price.AsOf = Today()
			}
		}
	}
	f.enrich(ctx, routes, reused)
	return routes, nil
}

func (f *Fal) warn(format string, args ...any) {
	f.Warnings = append(f.Warnings, fmt.Sprintf(format, args...))
}

func (f *Fal) listModels(ctx context.Context) ([]falModel, error) {
	var all []falModel
	cursor := ""
	for page := 0; page < 200; page++ {
		u := f.APIBase + "/v1/models?limit=1000"
		if cursor != "" {
			u += "&cursor=" + url.QueryEscape(cursor)
		}
		var resp struct {
			Models     []falModel `json:"models"`
			HasMore    bool       `json:"has_more"`
			NextCursor string     `json:"next_cursor"`
		}
		body, err := f.getRetry(ctx, u, nil)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("decode %s: %w", u, err)
		}
		all = append(all, resp.Models...)
		if !resp.HasMore || resp.NextCursor == "" {
			return all, nil
		}
		cursor = resp.NextCursor
	}
	return all, errors.New("fal catalog: pagination did not terminate")
}

// fetchPrices queries the pricing API in batches. One unknown endpoint id
// fails a whole batch with 404, so failed batches are bisected until the bad
// ids are isolated and skipped.
func (f *Fal) fetchPrices(ctx context.Context, key string, ids []string) map[string]falPrice {
	out := map[string]falPrice{}
	size := max(f.BatchSize, 1)
	for i := 0; i < len(ids) && ctx.Err() == nil; i += size {
		f.priceBatch(ctx, key, ids[i:min(i+size, len(ids))], out)
	}
	return out
}

func (f *Fal) priceBatch(ctx context.Context, key string, ids []string, out map[string]falPrice) {
	q := url.Values{"endpoint_id": ids}
	u := f.APIBase + "/v1/models/pricing?" + q.Encode()
	sleepCtx(ctx, f.Pause)
	var resp struct {
		Prices []falPrice `json:"prices"`
	}
	body, err := f.getRetry(ctx, u, map[string]string{"Authorization": "Key " + key})
	if err == nil {
		err = json.Unmarshal(body, &resp)
	}
	if err != nil {
		if len(ids) > 1 && ctx.Err() == nil {
			h := len(ids) / 2
			f.priceBatch(ctx, key, ids[:h], out)
			f.priceBatch(ctx, key, ids[h:], out)
			return
		}
		f.warn("fal price %s: %v", ids[0], shortErr(err))
		return
	}
	for _, p := range resp.Prices {
		out[p.EndpointID] = p
	}
}

// getRetry is Get with exponential backoff on 429 and 5xx, honouring the
// context so a cancelled sync stops promptly.
func (f *Fal) getRetry(ctx context.Context, u string, headers map[string]string) ([]byte, error) {
	delay := f.Backoff
	for attempt := 0; ; attempt++ {
		body, err := Get(ctx, u, headers)
		var he *HTTPError
		if err == nil || attempt >= 6 || !errors.As(err, &he) || (he.Status != 429 && he.Status < 500) {
			return body, err
		}
		sleepCtx(ctx, delay)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

// falUnitPrice maps fal's free-text billing unit onto the registry units. A
// unit that cannot be turned into a job price keeps its number with an
// explanatory note, and fal's placeholder of 1 "unit" (listed for gpt-image
// and nano-banana) is treated as no price at all.
func falUnitPrice(p falPrice, family, sourceURL string) registry.Price {
	price := registry.Price{Unit: registry.UnitUnknown, Source: registry.SourceUnknown, SourceURL: sourceURL, AsOf: Today()}
	if p.UnitPrice == nil || p.Unit == nil {
		price.Note = "fal pricing API returned no unit price"
		return price
	}
	v := *p.UnitPrice
	unit := strings.ToLower(strings.TrimSpace(*p.Unit))
	price.ValueUSD, price.Source = registry.F(v), registry.SourceAPI
	if p.Currency != "" && p.Currency != "USD" {
		price.Note = "currency " + p.Currency
	}
	per := 1.0 // "5 seconds" and similar units bill per block
	if n, rest, ok := strings.Cut(unit, " "); ok {
		if x, err := strconv.ParseFloat(n, 64); err == nil && x > 0 && !strings.Contains(rest, "token") && !strings.Contains(rest, "char") && !strings.Contains(rest, "byte") {
			per, unit = x, rest
		}
	}
	switch unit {
	case "seconds", "second", "input seconds":
		price.Unit, price.ValueUSD = registry.UnitSecond, registry.F(v/per)
		if per != 1 {
			price.Note = fmt.Sprintf("billed per %g s block", per)
		}
		if unit == "input seconds" {
			price.Note = "per second of input media"
		}
	case "images", "image":
		price.Unit = registry.UnitImage
	case "megapixels", "processed megapixels":
		price.Unit = registry.UnitMegapixel
		if unit == "processed megapixels" || per != 1 {
			price.Note = "per megapixel processed (width × height × frames for video)"
		}
	case "minutes", "minute":
		price.Unit = registry.UnitMinute
	case "1000 characters", "1000 utf-8 bytes":
		price.Unit = registry.UnitKChars
	case "generations", "videos", "audios", "requests", "1", "trainings", "cloned voices":
		price.Unit = registry.UnitRequest
	case "1000 tokens", "1m tokens":
		price.Unit = registry.UnitMTokens
		if unit == "1000 tokens" {
			price.ValueUSD = registry.F(roundUSD(v * 1000))
		}
		if strings.HasPrefix(family, "seedance") {
			price.Formula = registry.FormulaSeedTok
		}
	case "credits", "train units":
		price.Unit = registry.UnitCredit
		price.Note = "USD per provider credit; credits per job unknown"
	case "units":
		if v == 1 {
			price.ValueUSD, price.Source = nil, registry.SourceUnknown
			price.Note = "fal lists a placeholder of 1 unit; real price not published by the API"
		} else {
			price.Note = fmt.Sprintf("fal bills $%g per opaque unit; units per job unknown", v)
		}
	case "compute seconds":
		price.Note = "billed per GPU compute second; job length unknown"
	default:
		price.Note = fmt.Sprintf("unrecognised fal unit %q", *p.Unit)
	}
	return price
}

// roundUSD trims float noise from unit conversions (0.0214 × 1000).
func roundUSD(v float64) float64 { return math.Round(v*1e9) / 1e9 }
