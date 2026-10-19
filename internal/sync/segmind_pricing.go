package sync

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/radjathaher/creative-cli/internal/registry"
)

// segmindPricedSlugs are always re-priced from their pricing pages. Routes in
// allowlisted families are added at sync time.
var segmindPricedSlugs = []string{
	"seedance-2.5", "seedance-2.5-draft-final", "gemini-omni-1.1", "gemini-omni-flash",
	"gpt-image-2", "gpt-image-2.5-flare", "gpt-image-2.5-sunburst", "topaz-video-upscale",
	"veed-subtitles", "elevenlabs-music", "elevenlabs-v4-tts", "video-captioner",
}

// browserUA is sent to segmind.com, whose pricing pages are server-rendered
// for browsers.
const browserUA = "Mozilla/5.0 (X11; Linux x86_64) creative-cli"

func segmindPricingURL(slug string) string {
	return "https://www.segmind.com/models/" + slug + "/pricing"
}

// applySegmindPages replaces the average price on every route (siblings
// included) whose slug has a pricing page. An unparsable page keeps the
// average and says so.
func applySegmindPages(routes []registry.Route, pages map[string][]byte, asOf string) []registry.Route {
	for i := range routes {
		page, ok := pages[routes[i].ModelID]
		if !ok {
			continue
		}
		p, ok := parseSegmindPricing(page, routes[i].Price)
		if !ok {
			routes[i].Price.Note = strings.TrimPrefix(routes[i].Price.Note+"; pricing page unparsed", "; ")
			continue
		}
		p.SourceURL = segmindPricingURL(routes[i].ModelID)
		p.AsOf = asOf
		routes[i].Price = p
	}
	return routes
}

// parseSegmindPricing reads a pricing page. Every page embeds the model's
// machine-readable price as a "pricingData" object in its Next.js payload,
// which is the generic path for any slug. Token-priced pages also render
// exact per-second or per-quality tables, which win when present.
func parseSegmindPricing(page []byte, avg registry.Price) (registry.Price, bool) {
	pd, hasPD := segPricingData(page)
	if hasPD && pd.Type == "PER_MILLION_TOKENS" {
		if p, ok := parseSegmindHTML(page, avg); ok && len(p.Tiers) > 0 {
			return p, true
		}
	}
	if hasPD {
		if p, ok := priceFromPricingData(pd, avg); ok {
			return p, true
		}
	}
	return parseSegmindHTML(page, avg)
}

type segPricing struct {
	Type        string   `json:"pricing_type"`
	Rates       segRates `json:"rates"`
	Description string   `json:"description"`
	GPUSecond   *float64 `json:"fast_pricing"`
}

type segRates struct {
	PerOutputSecond *float64         `json:"rate_per_output_duration_second"`
	PerCharacter    *float64         `json:"rate_per_character"`
	PerMinute       *float64         `json:"rate_per_minute"`
	TranslationMin  float64          `json:"translation_rate_per_minute"`
	DynamicMult     float64          `json:"dynamic_preset_multiplier"`
	HighResMult     float64          `json:"high_res_multiplier"`
	MinimumMinutes  float64          `json:"minimum_minutes"`
	HighFPSMult     float64          `json:"high_fps_multiplier"`
	HighFPSAbove    int              `json:"high_fps_threshold"`
	KeyNames        []string         `json:"key_names"`
	Prices          []map[string]any `json:"prices"`
	TokenRates      *struct {
		Prices []map[string]any `json:"prices"`
	} `json:"token_rates"`
	OutputToken      *float64 `json:"output_token"`
	VideoOutputToken *float64 `json:"video_output_token"`
}

var segPDKey = regexp.MustCompile(`\\?"pricingData\\?":`)

// segPricingData extracts the pricingData object. In the RSC payload it sits
// inside a JavaScript string, so its quotes arrive escaped; braces do not, and
// counting them outside string literals finds the end of the object.
func segPricingData(page []byte) (segPricing, bool) {
	s := string(page)
	loc := segPDKey.FindStringIndex(s)
	if loc == nil {
		return segPricing{}, false
	}
	escaped := s[loc[0]] == '\\'
	start := loc[1]
	depth, inStr := 0, false
	for i := start; i < len(s); i++ {
		switch {
		case escaped && strings.HasPrefix(s[i:], `\\\"`):
			i += 3 // a quote escaped inside a JSON string value
		case escaped && strings.HasPrefix(s[i:], `\\`):
			i++
		case escaped && strings.HasPrefix(s[i:], `\"`):
			inStr = !inStr
			i++
		case !escaped && s[i] == '\\':
			i++
		case !escaped && s[i] == '"':
			inStr = !inStr
		case !inStr && s[i] == '{':
			depth++
		case !inStr && s[i] == '}':
			depth--
			if depth == 0 {
				raw := s[start : i+1]
				if escaped {
					if json.Unmarshal([]byte(`"`+raw+`"`), &raw) != nil {
						return segPricing{}, false
					}
				}
				var wrap struct {
					Pricing segPricing `json:"pricing"`
				}
				if json.Unmarshal([]byte(raw), &wrap) != nil {
					return segPricing{}, false
				}
				return wrap.Pricing, true
			}
		}
	}
	return segPricing{}, false
}

var segAboutList = regexp.MustCompile(`(?i)\$([0-9.]+)(?: per second)? at (\d{3,4}p|[0-9]k)`)

func priceFromPricingData(pd segPricing, avg registry.Price) (registry.Price, bool) {
	p := registry.Price{Source: registry.SourcePage, Note: pd.Description}
	r := pd.Rates
	switch {
	case r.PerOutputSecond != nil:
		p.Unit, p.ValueUSD = registry.UnitSecond, r.PerOutputSecond
	case r.PerCharacter != nil:
		p.Unit, p.ValueUSD = registry.UnitKChars, registry.F(*r.PerCharacter*1000)
	case r.PerMinute != nil:
		p.Unit, p.ValueUSD = registry.UnitMinute, r.PerMinute
		if r.HighResMult > 0 {
			for _, rs := range []string{"1440p", "2k", "4k"} {
				p.Tiers = append(p.Tiers, registry.PriceTier{Match: map[string]string{"resolution": rs}, ValueUSD: *r.PerMinute * r.HighResMult})
			}
		}
		p.Note = fmt.Sprintf("per input minute, basic preset; dynamic presets x%g, above 1080p x%g, translation +$%g/min, minimum %g min",
			r.DynamicMult, r.HighResMult, r.TranslationMin, r.MinimumMinutes)
	case pd.Type == "PER_INPUT_VIDEO_SECOND" && len(r.Prices) > 0:
		return segHeightPrice(p, r), true
	case pd.Type == "MULTI_KEY_BASED" && len(r.Prices) > 0:
		return segKeyedPrice(p, r), true
	case pd.Type == "PER_MILLION_TOKENS":
		if m := segAboutList.FindAllStringSubmatch(pd.Description, -1); len(m) > 0 {
			p.Unit = registry.UnitSecond
			for _, x := range m {
				v, _ := strconv.ParseFloat(x[1], 64)
				p.Tiers = append(p.Tiers, registry.PriceTier{Match: map[string]string{"resolution": strings.ToLower(x[2])}, ValueUSD: v})
			}
			p.ValueUSD = registry.F(defaultTier(p.Tiers))
			p.Note = "approximate per-second rates from the page; billed on tokens. " + pd.Description
			return p, true
		}
		out := r.OutputToken
		if out == nil {
			out = r.VideoOutputToken
		}
		if out == nil || avg.ValueUSD == nil {
			return p, false
		}
		avg.Note = fmt.Sprintf("token-priced ($%g per 1M output tokens); per-request average shown", *out)
		return avg, true
	case pd.GPUSecond != nil && avg.ValueUSD != nil:
		avg.Note = fmt.Sprintf("billed $%g per GPU second; per-request average shown", *pd.GPUSecond)
		return avg, true
	default:
		return p, false
	}
	return p, true
}

// segHeightPrice prices per-second upscaling by output height. The page's
// fps multiplier is mapped onto the fps_tier job attribute.
func segHeightPrice(p registry.Price, r segRates) registry.Price {
	p.Unit = registry.UnitSecond
	add := func(resolution string, v float64) {
		p.Tiers = append(p.Tiers, registry.PriceTier{Match: map[string]string{"resolution": resolution}, ValueUSD: v})
		if r.HighFPSMult > 0 {
			p.Tiers = append(p.Tiers, registry.PriceTier{Match: map[string]string{"resolution": resolution, "fps_tier": "high"}, ValueUSD: v * r.HighFPSMult})
		}
	}
	for _, e := range r.Prices {
		v, ok := e["rate_per_second"].(float64)
		if !ok {
			continue
		}
		switch h, _ := e["max_height"].(float64); {
		case h == 0:
			p.ValueUSD = registry.F(v)
			add("1440p", v)
			add("2k", v)
			add("4k", v)
		case h <= 720:
			for _, rs := range []string{"360p", "480p", "540p", "720p"} {
				add(rs, v)
			}
		default:
			add(fmt.Sprintf("%dp", int(h)), v)
		}
	}
	p.Note = "per second by output height; resolution is the upscale target"
	if r.HighFPSMult > 0 {
		p.Note += fmt.Sprintf("; x%g above %d fps (quote's fps_tier marks only 50+ fps as high, so 31-49 fps is under-quoted)", r.HighFPSMult, r.HighFPSAbove)
	}
	return p
}

// jobAttr maps Segmind key names onto the attributes PriceTier.Match can test.
var jobAttr = map[string]string{"resolution": "resolution", "aspect_ratio": "aspect", "video_input": "video_input", "quality": "quality", "duration": "duration"}

// segKeyedPrice turns a keyed price grid into tiers. A grid with a duration
// key is linear in seconds, so it becomes per-second tiers. Keys a job cannot
// express collapse to their highest price. Aspect-specific tiers are kept only
// where they differ from the 16:9 reference, which also serves jobs with no
// aspect.
func segKeyedPrice(p registry.Price, r segRates) registry.Price {
	perSecond := slices.Contains(r.KeyNames, "duration")
	p.Unit = registry.UnitRequest
	if perSecond {
		p.Unit = registry.UnitSecond
	}
	base, aspectTiers := map[string]registry.PriceTier{}, map[string]registry.PriceTier{}
	for _, e := range r.Prices {
		v, ok := e["cost"].(float64)
		if !ok {
			continue
		}
		match := map[string]string{}
		for _, k := range r.KeyNames {
			attr, known := jobAttr[k]
			if !known || (perSecond && k == "duration") {
				continue
			}
			match[attr] = fmt.Sprint(e[k])
		}
		if perSecond {
			d, _ := e["duration"].(float64)
			if d <= 0 {
				continue
			}
			v /= d
		}
		aspect := match["aspect"]
		delete(match, "aspect")
		k := matchKey(match)
		if aspect == "" || aspect == "16:9" {
			if t, ok := base[k]; !ok || v > t.ValueUSD {
				base[k] = registry.PriceTier{Match: match, ValueUSD: v}
			}
		}
		if aspect != "" {
			am := map[string]string{"aspect": aspect}
			for kk, vv := range match {
				am[kk] = vv
			}
			aspectTiers[matchKey(am)] = registry.PriceTier{Match: am, ValueUSD: v}
		}
	}
	for _, t := range base {
		p.Tiers = append(p.Tiers, t)
	}
	for _, t := range aspectTiers {
		ref := base[matchKey(withoutKey(t.Match, "aspect"))]
		if fmt.Sprintf("%.4f", ref.ValueUSD) != fmt.Sprintf("%.4f", t.ValueUSD) {
			p.Tiers = append(p.Tiers, t)
		}
	}
	sort.Slice(p.Tiers, func(i, j int) bool { return matchKey(p.Tiers[i].Match) < matchKey(p.Tiers[j].Match) })
	if len(p.Tiers) > 0 {
		p.ValueUSD = registry.F(defaultTier(p.Tiers))
	}
	if tr := r.TokenRates; tr != nil && perSecond {
		// Seedance: list price is per million video tokens. The exact
		// per-second tiers above stay authoritative; the token formula covers
		// jobs no tier matches.
		for _, e := range tr.Prices {
			if e["video_input"] == false {
				if v, ok := e["rate_per_million_tokens"].(float64); ok {
					p.Unit, p.Formula, p.ValueUSD = registry.UnitMTokens, registry.FormulaSeedTok, registry.F(v)
					for i := range p.Tiers {
						p.Tiers[i].Unit = registry.UnitSecond
					}
					break
				}
			}
		}
	}
	return p
}

func withoutKey(m map[string]string, key string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if k != key {
			out[k] = v
		}
	}
	return out
}
