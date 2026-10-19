package registry

import (
	"fmt"
	"strings"
)

// Job describes the quantities a quote needs to price a request. Fields left at
// their zero value are simply not used by the matching unit.
type Job struct {
	DurationSecs  float64
	Resolution    string // 360p | 480p | 720p | 1080p | 2k | 4k | 1K | 2K | 4K
	Aspect        string
	Count         int // images or clips; defaults to 1
	Chars         int
	FPS           int
	HasVideoInput bool
	Quality       string
}

// attrs exposes the job as the string map that PriceTier.Match is tested against.
func (j Job) attrs() map[string]string {
	m := map[string]string{
		"resolution":  strings.ToLower(j.Resolution),
		"aspect":      j.Aspect,
		"quality":     strings.ToLower(j.Quality),
		"video_input": fmt.Sprint(j.HasVideoInput),
	}
	if j.FPS > 0 {
		if j.FPS >= 50 {
			m["fps_tier"] = "high"
		} else {
			m["fps_tier"] = "normal"
		}
	}
	if j.DurationSecs > 0 {
		m["duration"] = fmt.Sprint(int(j.DurationSecs))
	}
	return m
}

// Estimate is the priced outcome of a job on one route.
type Estimate struct {
	USD   *float64 `json:"usd,omitempty"` // nil when the price cannot be computed
	Basis string   `json:"basis"`         // human-readable derivation, e.g. "6 s × $0.10/s"
}

// EstimateJob prices a job against a route. It never invents a number: when the
// unit cannot be converted with the job's quantities, USD stays nil and Basis
// says why.
func EstimateJob(p Price, j Job) Estimate {
	unit, value, ok := p.Unit, p.ValueUSD, p.ValueUSD != nil
	if t := matchTier(p.Tiers, j.attrs()); t != nil {
		value, ok = F(t.ValueUSD), true
		if t.Unit != "" {
			unit = t.Unit
		}
	}
	if !ok {
		return Estimate{Basis: "price unknown (" + p.Source + ")"}
	}
	count := float64(max(j.Count, 1))
	v := *value
	switch unit {
	case UnitSecond:
		if j.DurationSecs <= 0 {
			return Estimate{Basis: fmt.Sprintf("$%g/s; pass --duration to price", v)}
		}
		return est(v*j.DurationSecs*count, "%g s × $%.4g/s", j.DurationSecs, v)
	case UnitMinute:
		if j.DurationSecs <= 0 {
			return Estimate{Basis: fmt.Sprintf("$%g/min; pass --duration to price", v)}
		}
		return est(v*j.DurationSecs/60*count, "%g s × $%.4g/min", j.DurationSecs, v)
	case UnitImage, UnitRequest:
		return est(v*count, "%g × $%.4g/%s", count, v, unit)
	case UnitKChars:
		if j.Chars <= 0 {
			return Estimate{Basis: fmt.Sprintf("$%g per 1k chars; pass --chars to price", v)}
		}
		return est(v*float64(j.Chars)/1000, "%d chars × $%.4g/1k", j.Chars, v)
	case UnitMTokens:
		if p.Formula != FormulaSeedTok {
			return Estimate{Basis: fmt.Sprintf("$%g per 1M tokens; token count unknown", v)}
		}
		tokens, err := seedanceTokens(j)
		if err != nil {
			return Estimate{Basis: err.Error()}
		}
		return est(v*tokens/1e6*count, "%.0f video tokens × $%.4g/1M", tokens, v)
	default:
		return Estimate{Basis: fmt.Sprintf("$%g per %s; cannot convert to a job price", v, unit)}
	}
}

func est(usd float64, format string, args ...any) Estimate {
	return Estimate{USD: F(usd), Basis: fmt.Sprintf(format, args...)}
}

// matchTier returns the tier whose Match keys all equal the job attributes,
// preferring the tier with the most keys so specific tiers beat general ones.
func matchTier(tiers []PriceTier, attrs map[string]string) *PriceTier {
	var best *PriceTier
	for i := range tiers {
		t := &tiers[i]
		ok := true
		for k, want := range t.Match {
			if !strings.EqualFold(attrs[k], want) {
				ok = false
				break
			}
		}
		if ok && (best == nil || len(t.Match) > len(best.Match)) {
			best = t
		}
	}
	return best
}

// seedanceTokens applies ByteDance's billing formula, width × height × 24 fps ×
// seconds / 1024, using 16:9 frame sizes (portrait swaps the axes, same area).
func seedanceTokens(j Job) (float64, error) {
	frames := map[string][2]float64{
		"480p":  {854, 480},
		"720p":  {1280, 720},
		"1080p": {1920, 1080},
	}
	wh, ok := frames[strings.ToLower(j.Resolution)]
	if !ok {
		return 0, fmt.Errorf("token price needs --resolution 480p|720p|1080p")
	}
	if j.DurationSecs <= 0 {
		return 0, fmt.Errorf("token price needs --duration")
	}
	return wh[0] * wh[1] * 24 * j.DurationSecs / 1024, nil
}
