package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/registry"
)

const (
	kiePricingURL = "https://api.kie.ai/client/v1/model-pricing/page"
	kieLLMsURL    = "https://docs.kie.ai/llms.txt"
	kiePriceSrc   = "https://kie.ai/pricing"
	// kieCreditUSD is the published credit price. The pricing API returns both
	// creditPrice and usdPrice, and every non-free row on 2026-10-19 divides to
	// $0.005 within rounding; usdPrice is preferred when present.
	kieCreditUSD = 0.005
)

// allowlistFamilies are the image and video families from the spec allowlist.
// Sources spend extra requests only on these: KIE fetches their doc pages for
// an input schema, and Segmind reads their pricing pages.
var allowlistFamilies = map[string]bool{
	"seedance-2.5": true, "gemini-omni-flash-1.1": true, "gpt-image-2": true, "gpt-image-2.5": true,
}

// KIE syncs kie.ai: prices from the public pricing API and input schemas from
// the OpenAPI blocks embedded in its Markdown doc pages.
type KIE struct {
	PricingURL string
	LLMsURL    string
	// DocBase replaces https://docs.kie.ai in doc links; tests use it.
	DocBase string
}

func (KIE) Name() string { return "kie" }

func (k KIE) Fetch(ctx context.Context) ([]registry.Route, error) {
	pricingURL, llmsURL := k.PricingURL, k.LLMsURL
	if pricingURL == "" {
		pricingURL = kiePricingURL
	}
	if llmsURL == "" {
		llmsURL = kieLLMsURL
	}
	rows, err := fetchKIEPrices(ctx, pricingURL)
	if err != nil {
		return nil, err
	}
	routes := kieRoutes(rows, Today())
	// Doc schemas are an enrichment: a failed doc page leaves the route priced
	// but schemaless instead of failing the whole provider.
	if index, err := Get(ctx, llmsURL, nil); err == nil {
		docs := map[string]json.RawMessage{}
		for _, u := range kieDocURLs(index) {
			if k.DocBase != "" {
				u = k.DocBase + strings.TrimPrefix(u, "https://docs.kie.ai")
			}
			body, err := Get(ctx, u, nil)
			if err != nil {
				continue
			}
			if model, schema, err := kieDocSchema(body); err == nil {
				docs[model] = schema
			}
		}
		routes = kieAttachSchemas(routes, docs)
	}
	return routes, nil
}

// kieRow is one SKU from the pricing API.
type kieRow struct {
	Description string `json:"modelDescription"`
	Interface   string `json:"interfaceType"`
	Credits     string `json:"creditPrice"`
	CreditUnit  string `json:"creditUnit"`
	USD         string `json:"usdPrice"`
	Anchor      string `json:"anchor"`
}

func fetchKIEPrices(ctx context.Context, endpoint string) ([]kieRow, error) {
	var all []kieRow
	for page := 1; page <= 50; page++ {
		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				Records []kieRow `json:"records"`
				Pages   int      `json:"pages"`
			} `json:"data"`
		}
		body, _ := json.Marshal(map[string]int{"pageNum": page, "pageSize": 100})
		if err := postJSON(ctx, endpoint, body, &resp); err != nil {
			return nil, err
		}
		if resp.Code != 200 {
			return nil, fmt.Errorf("kie pricing page %d: code %d %s", page, resp.Code, resp.Msg)
		}
		all = append(all, resp.Data.Records...)
		if page >= resp.Data.Pages || len(resp.Data.Records) == 0 {
			break
		}
	}
	return all, nil
}

func postJSON(ctx context.Context, endpoint string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{Status: resp.StatusCode, URL: endpoint, Body: string(raw[:min(len(raw), 300)])}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s: %w", endpoint, err)
	}
	return nil
}

var (
	kieOpPhrase = regexp.MustCompile(`(?i)\b(text|image|video|reference|speech|audio)-to-(video|image|speech|audio)\b`)
	kieRes      = regexp.MustCompile(`(?i)\b(360p|480p|540p|580p|720p|1080p|1440p|2160p|1k|2k|4k|8k)\b`)
	kieDur      = regexp.MustCompile(`(?i)\b(\d+)(?:\.\d+)?\s?s\b`)
	kieQuality  = regexp.MustCompile(`(?i)\b(low|medium|high)\b`)
)

// kieSKU is a pricing row reduced to its route key and tier.
type kieSKU struct {
	model, op, title string
	unit             string
	usd              float64
	match            map[string]string
}

func parseKIERow(r kieRow) (kieSKU, bool) {
	if strings.EqualFold(r.Interface, "chat") {
		return kieSKU{}, false // LLM chat is priced per token on OpenRouter; not an asset route
	}
	desc := strings.TrimSpace(r.Description)
	prefix, rest, _ := strings.Cut(desc, ",")
	phrase := strings.ToLower(kieOpPhrase.FindString(rest))
	model := ""
	if u, err := url.Parse(r.Anchor); err == nil {
		model = u.Query().Get("model")
	}
	if model == "" {
		model = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(prefix)), " ", "-")
		// Image SKUs such as "gpt-image-2-5-flare, text-to-image, 1K" name one
		// API model per mode, so the mode belongs in the model id.
		if phrase != "" && !strings.Contains(model, phrase) && !strings.Contains(model, "/") {
			model += "-" + phrase
		}
	}
	unit, scale := kieUnit(r.CreditUnit)
	usd, err := strconv.ParseFloat(strings.TrimSpace(r.USD), 64)
	if err != nil {
		credits, cerr := strconv.ParseFloat(strings.TrimSpace(r.Credits), 64)
		if cerr != nil {
			return kieSKU{}, false
		}
		usd = credits * kieCreditUSD
	}
	sku := kieSKU{model: model, title: strings.TrimSpace(prefix), unit: unit, usd: usd * scale, match: map[string]string{}}
	sku.op = kieOp(phrase, r.Interface, model, sku.title, desc)

	low := strings.ToLower(rest)
	if m := kieRes.FindString(low); m != "" {
		sku.match["resolution"] = m
	}
	switch {
	case strings.Contains(low, "with video"):
		sku.match["video_input"] = "true"
	case strings.Contains(low, "no video"), strings.Contains(low, "without video"):
		sku.match["video_input"] = "false"
	}
	if m := kieQuality.FindString(low); m != "" && r.Interface == "image" {
		sku.match["quality"] = m
	}
	// Only flat per-clip prices vary by duration; per-second prices already scale.
	if unit == registry.UnitRequest {
		if m := kieDur.FindStringSubmatch(low); m != nil {
			sku.match["duration"] = m[1]
		}
	}
	return sku, true
}

func kieUnit(u string) (unit string, scale float64) {
	u = strings.ToLower(u)
	switch {
	case strings.Contains(u, "second"):
		return registry.UnitSecond, 1
	case strings.Contains(u, "2 images"):
		return registry.UnitImage, 0.5
	case strings.Contains(u, "image"):
		return registry.UnitImage, 1
	case strings.Contains(u, "video"), strings.Contains(u, "request"), strings.Contains(u, "generation"), strings.Contains(u, "upscale"):
		return registry.UnitRequest, 1
	case strings.Contains(u, "1000 char"):
		return registry.UnitKChars, 1
	case strings.Contains(u, "megapixel"):
		return registry.UnitMegapixel, 1
	case strings.Contains(u, "million tokens"), strings.Contains(u, "1m tokens"):
		return registry.UnitMTokens, 1
	}
	return registry.UnitUnknown, 1
}

// kieOp asks Classify first and falls back to KIE's own wording. Multi-mode
// video endpoints without a mode in their SKU text (Seedance 2.5, Omni) accept
// a bare prompt, so they default to text-to-video.
func kieOp(phrase, iface, model, title, desc string) string {
	category := phrase
	if category == "" {
		category = iface
	}
	if op, _, _ := Classify("kie", category, model, title); op != registry.OpOther {
		return op
	}
	d := strings.ToLower(desc + " " + model)
	switch {
	case phrase == "image-to-image" || strings.Contains(d, "edit") && iface == "image":
		return registry.OpImageEdit
	case phrase == "text-to-image":
		return registry.OpImageT2I
	case phrase == "image-to-video":
		return registry.OpVideoI2V
	case phrase == "reference-to-video":
		return registry.OpVideoR2V
	case phrase == "video-to-video":
		return registry.OpVideoEdit
	case strings.Contains(d, "upscale"):
		if iface == "image" {
			return registry.OpImageUpscale
		}
		return registry.OpVideoUpscale
	case strings.Contains(d, "lip sync") || strings.Contains(d, "lipsync"):
		return registry.OpVideoLipsync
	case strings.Contains(d, "text to speech") || strings.Contains(d, "tts") || phrase == "text-to-speech":
		return registry.OpVoiceTTS
	case iface == "video" && phrase == "":
		return registry.OpVideoT2V
	case iface == "image" && phrase == "":
		return registry.OpImageT2I
	case iface == "music":
		switch {
		case strings.Contains(d, "extend"):
			return registry.OpMusicExtend
		case strings.Contains(d, "separat") || strings.Contains(d, "stem"):
			return registry.OpMusicStems
		case strings.Contains(d, "sound"):
			return registry.OpSFXGenerate
		}
		return registry.OpMusicGenerate
	}
	return registry.OpOther
}

// kieRoutes groups SKUs into one route per (model, op). Each SKU becomes a
// tier; SKUs that differ only in attributes a job cannot express (native audio
// on or off, token input vs output) collapse to the most expensive, so a quote
// never understates. Jobs that match no tier fall back to the cheapest SKU.
func kieRoutes(rows []kieRow, asOf string) []registry.Route {
	type key struct{ model, op string }
	groups := map[key][]kieSKU{}
	var order []key
	for _, r := range rows {
		sku, ok := parseKIERow(r)
		if !ok {
			continue
		}
		k := key{sku.model, sku.op}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], sku)
	}
	opsPerModel := map[string]int{}
	for _, k := range order {
		opsPerModel[k.model]++
	}
	var routes []registry.Route
	for _, k := range order {
		skus := groups[k]
		route := NewRoute("kie", k.model, skus[0].title)
		if opsPerModel[k.model] > 1 {
			route.ID += "#" + k.op
		}
		route.Transport = "kie-jobs"
		route.SourceURL = kiePriceSrc
		_, route.Family, route.Variant = Classify("kie", k.op, k.model, skus[0].title)
		route.Op = k.op
		route.Price = kiePrice(skus, asOf)
		routes = append(routes, route)
	}
	return routes
}

func kiePrice(skus []kieSKU, asOf string) registry.Price {
	units := map[string]int{}
	for _, s := range skus {
		units[s.unit]++
	}
	unit, best := registry.UnitUnknown, 0
	for u, n := range units {
		if n > best || n == best && u < unit {
			unit, best = u, n
		}
	}
	p := registry.Price{Unit: unit, Source: registry.SourceAPI, SourceURL: kiePricingURL, AsOf: asOf, Note: "1 credit = $0.005"}
	cheapest := skus[0].usd
	byMatch := map[string]int{}
	collapsed := false
	for _, s := range skus {
		cheapest = min(cheapest, s.usd)
		if len(s.match) == 0 && len(skus) == 1 {
			continue
		}
		mk := matchKey(s.match)
		tier := registry.PriceTier{Match: s.match, ValueUSD: s.usd}
		if s.unit != unit {
			tier.Unit = s.unit
		}
		if i, ok := byMatch[mk]; ok {
			collapsed = true
			if s.usd > p.Tiers[i].ValueUSD {
				p.Tiers[i] = tier
			}
			continue
		}
		byMatch[mk] = len(p.Tiers)
		p.Tiers = append(p.Tiers, tier)
	}
	// A lone tier with an empty match is just the base price.
	if len(p.Tiers) == 1 && len(p.Tiers[0].Match) == 0 {
		p.Tiers = nil
	}
	p.ValueUSD = registry.F(cheapest)
	if len(p.Tiers) > 0 {
		p.Note += "; jobs matching no tier are priced at the cheapest SKU"
	}
	if collapsed {
		p.Note += "; variants a job cannot select (e.g. audio on/off) collapse to the highest price"
	}
	sort.SliceStable(p.Tiers, func(i, j int) bool { return matchKey(p.Tiers[i].Match) < matchKey(p.Tiers[j].Match) })
	return p
}

func matchKey(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		keys = append(keys, k+"="+v)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}
