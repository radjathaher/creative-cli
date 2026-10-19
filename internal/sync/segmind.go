package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/radjathaher/creative-cli/internal/registry"
)

const segmindListURL = "https://api.spotprod.segmind.com/inference-model-information/list"

// Segmind syncs the public Segmind catalog. The catalog publishes only an
// observed average cost per request, so curated slugs are re-priced from their
// public pricing pages (see segmind_pricing.go).
type Segmind struct {
	ListURL string
	// PageURL builds a pricing page URL for a slug; tests point it at fixtures.
	PageURL func(slug string) string
	// PricedSlugs overrides the curated list of slugs whose pages are read.
	PricedSlugs []string
}

func (Segmind) Name() string { return "segmind" }

func (s Segmind) Fetch(ctx context.Context) ([]registry.Route, error) {
	listURL := s.ListURL
	if listURL == "" {
		listURL = segmindListURL
	}
	body, err := Get(ctx, listURL, nil)
	if err != nil {
		return nil, err
	}
	routes, err := segmindRoutes(body, Today())
	if err != nil {
		return nil, err
	}
	pageURL := s.PageURL
	if pageURL == nil {
		pageURL = segmindPricingURL
	}
	slugs := s.PricedSlugs
	if slugs == nil {
		slugs = append([]string(nil), segmindPricedSlugs...)
		for _, r := range routes {
			if allowlistFamilies[r.Family] && !r.Deprecated {
				slugs = append(slugs, r.ModelID)
			}
		}
	}
	pages := map[string][]byte{}
	for _, slug := range slugs {
		if _, done := pages[slug]; done {
			continue
		}
		if page, err := Get(ctx, pageURL(slug), map[string]string{"User-Agent": browserUA}); err == nil {
			pages[slug] = page
		}
	}
	return applySegmindPages(routes, pages, Today()), nil
}

type segmindModel struct {
	Slug       string          `json:"slug"`
	Title      string          `json:"title"`
	Type       string          `json:"type"`
	AvgCost    *float64        `json:"average_cost"`
	Deprecated bool            `json:"is_depreciated"`
	ComingSoon bool            `json:"coming_soon"`
	Schema     json.RawMessage `json:"parameters_schema"`
}

func segmindRoutes(body []byte, asOf string) ([]registry.Route, error) {
	var list struct {
		Models []segmindModel `json:"Inference Model Information List"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("decode segmind list: %w", err)
	}
	if len(list.Models) == 0 {
		return nil, fmt.Errorf("segmind list: no models")
	}
	var routes []registry.Route
	for _, m := range list.Models {
		if m.ComingSoon || m.Slug == "" {
			continue // not callable yet
		}
		r := NewRoute("segmind", m.Slug, m.Title)
		r.Transport = "segmind-v2"
		r.Op, r.Family, r.Variant = Classify("segmind", m.Type, m.Slug, m.Title)
		r.Deprecated = m.Deprecated
		r.SourceURL = "https://www.segmind.com/models/" + m.Slug
		if m.AvgCost != nil && *m.AvgCost > 0 {
			r.Price = registry.Price{
				Unit: registry.UnitRequest, ValueUSD: registry.F(*m.AvgCost),
				Source: registry.SourceAverage, SourceURL: segmindListURL, AsOf: asOf,
				Note: "observed average per request",
			}
		}
		if schema := pruneJSONSchema(m.Schema); schema != nil {
			applySchema(&r, schema)
		}
		routes = append(routes, r)
		routes = append(routes, modeSiblings(r)...)
	}
	return routes, nil
}

func pruneJSONSchema(raw json.RawMessage) json.RawMessage {
	var v map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil || v["properties"] == nil {
		return nil
	}
	out, err := json.Marshal(pruneSchema(v))
	if err != nil {
		return nil
	}
	return out
}

// modeSiblings adds one route per extra mode a multi-mode endpoint accepts,
// because providers label such endpoints with a single category (Segmind
// calls Seedance 2.5 "imageToVideo") while quotes are asked per op. Siblings
// share everything but Op and carry "#<op>" in their ID.
func modeSiblings(r registry.Route) []registry.Route {
	pm := r.ParamMap
	if pm == nil {
		return nil
	}
	var ops []string
	switch r.Op {
	case registry.OpVideoT2V, registry.OpVideoI2V:
		if pm["prompt"] != "" && !requiresMedia(pm, r.Schema) {
			ops = append(ops, registry.OpVideoT2V)
		}
		if pm["first_frame"] != "" {
			ops = append(ops, registry.OpVideoI2V)
		}
		if pm["first_frame"] != "" && pm["last_frame"] != "" {
			ops = append(ops, registry.OpVideoFLF2V)
		}
		if isArrayField(r.Schema, pm["image"]) || isArrayField(r.Schema, pm["video"]) {
			ops = append(ops, registry.OpVideoR2V)
		}
	case registry.OpImageT2I:
		if pm["image"] != "" {
			ops = append(ops, registry.OpImageEdit)
		}
	}
	var out []registry.Route
	for _, op := range ops {
		if op == r.Op {
			continue
		}
		sib := r
		sib.ID = r.ID + "#" + op
		sib.Op = op
		out = append(out, sib)
	}
	return out
}

// schemaGaps are fields KIE and Segmind use that the shared candidate lists
// in schema.go do not cover yet. They fill a flag only when it is still empty.
var schemaGaps = []struct{ flag, field string }{
	{"video", "video_list"}, {"video", "reference_videos"},
	{"audio", "reference_audios"},
	{"first_frame", "first_frame"}, {"last_frame", "last_frame"},
}

var (
	gapMaxItems = regexp.MustCompile(`(?i)(?:maximum of|up to|at most)\s+(\w+)\s+(?:item|video|clip)`)
	gapRange    = regexp.MustCompile(`(\d+)\s*[-–]\s*(\d+)\s*s(?:ec|econds)?\b`)
)

// gapProp reads what the gap fill needs, including oneOf const enums, which
// Segmind uses for resolution and aspect ratio.
type gapProp struct {
	Type        string `json:"type"`
	MaxItems    int    `json:"maxItems"`
	Description string `json:"description"`
	OneOf       []struct {
		Const any `json:"const"`
	} `json:"oneOf"`
}

// applySchema sets a route's schema, param map, and caps from the shared
// helpers, then fills the gaps listed above.
func applySchema(r *registry.Route, schema json.RawMessage) {
	r.Schema = schema
	r.ParamMap = ParamMapFromSchema(schema)
	r.Caps = CapsFromSchema(schema)
	var s struct {
		Properties map[string]gapProp `json:"properties"`
	}
	if json.Unmarshal(schema, &s) != nil || r.ParamMap == nil {
		return
	}
	for _, g := range schemaGaps {
		p, ok := s.Properties[g.field]
		if !ok || r.ParamMap[g.flag] != "" {
			continue
		}
		r.ParamMap[g.flag] = g.field
		n := 1
		if p.Type == "array" {
			n = p.MaxItems
			if m := gapMaxItems.FindStringSubmatch(p.Description); n == 0 && m != nil {
				n = parseCount(m[1])
			}
		}
		switch g.flag {
		case "video":
			r.Caps.MaxVideos = n
		case "audio":
			r.Caps.MaxAudios = n
		case "first_frame":
			r.Caps.FirstFrame = true
		case "last_frame":
			r.Caps.LastFrame = true
		}
	}
	if d := s.Properties[r.ParamMap["duration"]]; r.Caps.MaxDuration == 0 {
		if m := gapRange.FindStringSubmatch(d.Description); m != nil {
			r.Caps.MinDuration, r.Caps.MaxDuration = parseCount(m[1]), parseCount(m[2])
		}
	}
	if len(r.Caps.Resolutions) == 0 {
		r.Caps.Resolutions = oneOfConsts(s.Properties[r.ParamMap["resolution"]])
	}
	if len(r.Caps.Aspects) == 0 {
		r.Caps.Aspects = oneOfConsts(s.Properties[r.ParamMap["aspect"]])
	}
}

func oneOfConsts(p gapProp) []string {
	var out []string
	for _, c := range p.OneOf {
		if c.Const != nil {
			out = append(out, fmt.Sprint(c.Const))
		}
	}
	return out
}

// requiresMedia reports whether the schema requires an image or video input,
// which rules out a text-only mode.
func requiresMedia(pm map[string]string, schema json.RawMessage) bool {
	var s struct {
		Required []string `json:"required"`
	}
	_ = json.Unmarshal(schema, &s)
	for _, req := range s.Required {
		for _, k := range []string{"image", "video", "first_frame"} {
			if pm[k] == req {
				return true
			}
		}
	}
	return false
}

func isArrayField(schema json.RawMessage, field string) bool {
	var s struct {
		Properties map[string]gapProp `json:"properties"`
	}
	if field == "" || json.Unmarshal(schema, &s) != nil {
		return false
	}
	return s.Properties[field].Type == "array"
}
