package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	gosync "sync"

	"github.com/radjathaher/creative-cli/internal/registry"
)

// enrich fetches the OpenAPI spec and model page for the detail subset (minus
// routes reused from a fresh prior sync) in parallel and folds schema, caps, param map, and page prices into each route.
func (f *Fal) enrich(ctx context.Context, routes []registry.Route, skip []bool) {
	var (
		wg   gosync.WaitGroup
		mu   gosync.Mutex
		jobs = make(chan int)
	)
	for w := 0; w < max(f.Workers, 1); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				warns := f.enrichOne(ctx, &routes[i])
				mu.Lock()
				f.Warnings = append(f.Warnings, warns...)
				mu.Unlock()
			}
		}()
	}
	for i := range routes {
		if !skip[i] && needsDetail(routes[i]) && ctx.Err() == nil {
			jobs <- i
		}
	}
	close(jobs)
	wg.Wait()
}

func (f *Fal) enrichOne(ctx context.Context, r *registry.Route) (warns []string) {
	defaultRes := ""
	spec, err := f.getRetry(ctx, f.OpenAPIBase+"?endpoint_id="+url.QueryEscape(r.ModelID), nil)
	if err == nil {
		var schema json.RawMessage
		if schema, err = FalInputSchema(spec, r.ModelID); err == nil {
			r.Schema = schema
			r.Caps = CapsFromSchema(schema)
			r.ParamMap = ParamMapFromSchema(schema)
			defaultRes = schemaDefault(schema, r.ParamMap["resolution"])
		}
	}
	if err != nil {
		warns = append(warns, fmt.Sprintf("fal schema %s: %s", r.ModelID, shortErr(err)))
	}
	pageURL := f.PageBase + "/" + r.ModelID + "/llms.txt"
	page, err := f.getRetry(ctx, pageURL, nil)
	if err != nil {
		return append(warns, fmt.Sprintf("fal page %s: %s", r.ModelID, shortErr(err)))
	}
	applyPagePrice(&r.Price, string(page), pageURL, defaultRes, r.Family, r.Op)
	return warns
}

// FalInputSchema extracts the request body schema of the POST operation from a
// fal queue OpenAPI document and trims it to type, properties, required, and
// property order, inlining local $refs so the stored schema stands alone.
func FalInputSchema(spec []byte, endpointID string) (json.RawMessage, error) {
	var doc struct {
		Paths map[string]map[string]struct {
			RequestBody struct {
				Content map[string]struct {
					Schema map[string]any `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(spec, &doc); err != nil {
		return nil, fmt.Errorf("decode openapi: %w", err)
	}
	op, ok := doc.Paths["/"+endpointID]["post"]
	if !ok {
		return nil, fmt.Errorf("openapi has no POST /%s", endpointID)
	}
	body := op.RequestBody.Content["application/json"].Schema
	resolved, _ := inlineRefs(body, doc.Components.Schemas, 0).(map[string]any)
	if resolved == nil {
		return nil, fmt.Errorf("openapi POST /%s has no JSON body schema", endpointID)
	}
	trimmed := map[string]any{"type": "object", "properties": resolved["properties"]}
	for _, k := range []string{"required", "x-fal-order-properties"} {
		if v, ok := resolved[k]; ok {
			trimmed[k] = v
		}
	}
	return json.Marshal(trimmed)
}

// inlineRefs replaces {"$ref": "#/components/schemas/X"} with X, to a bounded
// depth so recursive schemas cannot loop.
func inlineRefs(v any, comps map[string]any, depth int) any {
	switch t := v.(type) {
	case map[string]any:
		if ref, ok := t["$ref"].(string); ok && depth < 6 {
			if target, ok := comps[strings.TrimPrefix(ref, "#/components/schemas/")]; ok {
				return inlineRefs(target, comps, depth+1)
			}
		}
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = inlineRefs(x, comps, depth)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = inlineRefs(x, comps, depth)
		}
		return out
	}
	return v
}

func schemaDefault(schema json.RawMessage, field string) string {
	if field == "" {
		return ""
	}
	var s struct {
		Properties map[string]struct {
			Default any `json:"default"`
		} `json:"properties"`
	}
	if json.Unmarshal(schema, &s) != nil {
		return ""
	}
	if d, ok := s.Properties[field].Default.(string); ok {
		return strings.ToLower(d)
	}
	return ""
}

var (
	reRes         = regexp.MustCompile(`(?i)\b(360p|480p|540p|580p|720p|768p|1080p|1440p|2k|4k)\b`)
	reUSD         = regexp.MustCompile(`\$([0-9]*\.?[0-9]+)`)
	rePerBlock    = regexp.MustCompile(`(?i)(?:per (\d+) seconds?|(\d+) seconds? costs)`)
	rePerSecond   = regexp.MustCompile(`(?i)per second|/s\b|per \d+ seconds|\d+ seconds? costs`)
	reTokTier     = regexp.MustCompile(`(?i)\$([\d.]+)\**\s*per 1000 tokens for ((?:\d+p(?:,? and |, )?)+)`)
	rePerRequest  = regexp.MustCompile(`(?i)^(?:your request will cost\s+)?\$([\d.]+)\**\s*per (?:request|generation|call|run)\b`)
	reWillCost    = regexp.MustCompile(`(?i)(?:will cost|costs?)\s+\$[\d.]+`)
	reOptionPrice = regexp.MustCompile(`\$([\d.]+)\s*\(([^)]+)\)`)
	reImgTokens   = regexp.MustCompile(`(?i)image tokens \(per 1m\):\s*\$([\d.]+) input.*?\$([\d.]+) output`)
	reFPSDouble   = regexp.MustCompile(`(?i)(?:50|60) ?fps[^.]*doubl|doubl[^.]*(?:50|60) ?fps`)
	reSentence    = regexp.MustCompile(`\.\s+|\n`)
)

// applyPagePrice reads the pricing prose on a fal model page (llms.txt). The
// API gives one base rate per endpoint; the page adds resolution tiers, the
// 1080p token rate for Seedance, and real prices for endpoints the API lists in
// opaque units. The pricing prose is always copied into the note so conflicts
// between page and API stay visible.
func applyPagePrice(p *registry.Price, page, pageURL, defaultRes, family, op string) {
	text := pricingProse(page)
	if text == "" {
		return
	}
	note := strings.ReplaceAll(text, "\n", " ")
	if len(note) > 400 {
		note = strings.ToValidUTF8(note[:400], "") + "…"
	}
	p.Note = joinNote(p.Note, "fal page: "+note)
	unconvertible := p.ValueUSD == nil || p.Unit == registry.UnitUnknown || p.Unit == registry.UnitCredit

	if p.Formula == registry.FormulaSeedTok {
		for _, m := range reTokTier.FindAllStringSubmatch(text, -1) {
			v, _ := strconv.ParseFloat(m[1], 64)
			for _, res := range reRes.FindAllString(m[2], -1) {
				addTier(p, strings.ToLower(res), "", roundUSD(v*1000))
			}
		}
		return
	}
	if tiers := resolutionTiers(text); len(tiers) > 0 && (p.Unit == registry.UnitSecond || unconvertible) {
		p.Unit, p.Source, p.SourceURL = registry.UnitSecond, registry.SourcePage, pageURL
		p.Tiers = nil
		for res, v := range tiers {
			addTier(p, res, "", v)
		}
		base, ok := tiers[defaultRes]
		if !ok {
			base = lowest(tiers)
		}
		p.ValueUSD = registry.F(base)
	} else if m := rePerRequest.FindStringSubmatch(text); m != nil && unconvertible {
		v, _ := strconv.ParseFloat(m[1], 64)
		p.Unit, p.ValueUSD, p.Source, p.SourceURL = registry.UnitRequest, registry.F(v), registry.SourcePage, pageURL
	} else if opts := reOptionPrice.FindAllStringSubmatch(text, -1); len(opts) > 0 && unconvertible && reWillCost.MatchString(text) {
		applyOptionPrices(p, opts, pageURL)
	} else if m := reImgTokens.FindStringSubmatch(text); m != nil && unconvertible {
		v, _ := strconv.ParseFloat(m[2], 64)
		p.Unit, p.ValueUSD, p.Source, p.SourceURL = registry.UnitMTokens, registry.F(v), registry.SourcePage, pageURL
		p.Note = joinNote(p.Note, "price is per 1M output image tokens; token count depends on size and quality")
	}

	// Frame rate doubles the price on some upscalers. The page states it for
	// some endpoints; for Topaz video it comes from the Outline game-pipeline
	// doc (router spec V3), since the page only says the cost depends on fps.
	topaz := family == "topaz" && op == registry.OpVideoUpscale
	if p.Unit == registry.UnitSecond && p.ValueUSD != nil && (reFPSDouble.MatchString(text) || topaz) {
		for _, t := range append([]registry.PriceTier(nil), p.Tiers...) {
			m := map[string]string{"fps_tier": "high"}
			for k, v := range t.Match {
				m[k] = v
			}
			p.Tiers = append(p.Tiers, registry.PriceTier{Match: m, Unit: t.Unit, ValueUSD: t.ValueUSD * 2})
		}
		p.Tiers = append(p.Tiers, registry.PriceTier{Match: map[string]string{"fps_tier": "high"}, ValueUSD: *p.ValueUSD * 2})
		if topaz {
			p.Note = joinNote(p.Note, "2x at >=50 fps per Outline doc (unverified)")
		}
	}
	sortTiers(p.Tiers)
}

// pricingProse returns the page's first two lines that state a dollar price,
// with markdown emphasis removed.
func pricingProse(page string) string {
	var parts []string
	for _, line := range strings.Split(page, "\n") {
		l := strings.TrimSpace(strings.ReplaceAll(line, "**", ""))
		if l == "" || strings.Contains(l, "FAL_KEY") || strings.HasPrefix(l, "-") || !reUSD.MatchString(l) {
			continue
		}
		if has(l, `(?i)cost|price|billing|billed|charged|per request|per second`) {
			parts = append(parts, l)
		}
		if len(parts) == 2 {
			break
		}
	}
	return strings.Join(parts, "\n")
}

// resolutionTiers finds the first sentence that prices output per second by
// resolution and returns USD per second keyed by lowercase resolution. Each
// semicolon segment, or failing that each comma clause, pairs its resolutions
// with its dollar amounts in order; the first price seen for a resolution wins,
// and a "per 10 seconds" divisor anywhere in the sentence applies throughout.
func resolutionTiers(text string) map[string]float64 {
	for _, sentence := range reSentence.Split(text, -1) {
		if !rePerSecond.MatchString(sentence) || len(reRes.FindAllString(sentence, -1)) < 2 {
			continue
		}
		div := 1.0
		if m := rePerBlock.FindStringSubmatch(sentence); m != nil {
			div, _ = strconv.ParseFloat(m[1]+m[2], 64)
		}
		out := map[string]float64{}
		for _, seg := range strings.Split(sentence, ";") {
			if !pairPrices(seg, div, out) {
				for _, clause := range strings.Split(seg, ",") {
					pairPrices(clause, div, out)
				}
			}
		}
		if len(out) >= 2 {
			return out
		}
	}
	return nil
}

func pairPrices(clause string, div float64, out map[string]float64) bool {
	res, usd := reRes.FindAllString(clause, -1), reUSD.FindAllStringSubmatch(clause, -1)
	if len(res) == 0 || len(res) != len(usd) {
		return false
	}
	for i := range res {
		k := strings.ToLower(res[i])
		if _, seen := out[k]; !seen {
			v, _ := strconv.ParseFloat(usd[i][1], 64)
			out[k] = roundUSD(v / div)
		}
	}
	return true
}

func addTier(p *registry.Price, res, unit string, v float64) {
	p.Tiers = append(p.Tiers, registry.PriceTier{Match: map[string]string{"resolution": res}, Unit: unit, ValueUSD: v})
}

func lowest(m map[string]float64) float64 {
	first, best := true, 0.0
	for _, v := range m {
		if first || v < best {
			best, first = v, false
		}
	}
	return best
}

// sortTiers orders tiers by value then key count so snapshots diff cleanly.
func sortTiers(t []registry.PriceTier) {
	sort.SliceStable(t, func(i, j int) bool {
		if len(t[i].Match) != len(t[j].Match) {
			return len(t[i].Match) < len(t[j].Match)
		}
		return t[i].ValueUSD < t[j].ValueUSD
	})
}

func joinNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// applyOptionPrices handles pages that price a request by option, such as
// Tripo: "$0.20 (without textures), $0.30 (with standard textures), or $0.40
// (with HD textures)". The default is the option labelled standard or default,
// else the first one; each option also becomes a quality tier, and add-ons the
// page mentions stay in the note.
func applyOptionPrices(p *registry.Price, opts [][]string, pageURL string) {
	p.Unit, p.Source, p.SourceURL, p.Tiers = registry.UnitRequest, registry.SourcePage, pageURL, nil
	def := -1.0
	for _, o := range opts {
		v, err := strconv.ParseFloat(o[1], 64)
		if err != nil {
			continue
		}
		label := strings.ToLower(o[2])
		p.Tiers = append(p.Tiers, registry.PriceTier{Match: map[string]string{"quality": optionSlug(label)}, ValueUSD: v})
		if def < 0 || strings.Contains(label, "standard") || strings.Contains(label, "default") {
			def = v
		}
	}
	if def >= 0 {
		p.ValueUSD = registry.F(def)
	}
}

func optionSlug(label string) string {
	switch {
	case strings.Contains(label, "without"):
		return "none"
	case strings.Contains(label, "hd"):
		return "hd"
	case strings.Contains(label, "standard"):
		return "standard"
	}
	return strings.Join(strings.Fields(label), "-")
}
