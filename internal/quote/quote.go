// Package quote ranks the routes that can serve a job by estimated price. It
// never chooses a route: it reports every candidate, why any were excluded,
// and which one is the cheapest that can run right now.
package quote

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/radjathaher/creative-cli/internal/account"
	"github.com/radjathaher/creative-cli/internal/registry"
)

// Request is a job as the quote sees it: the priced quantities plus the input
// counts that decide whether a route can accept the job at all.
type Request struct {
	Op, Family, Variant, Provider string
	All                           bool
	Job                           registry.Job
	Images, Videos, Audios        int
	VideoSecs                     float64 // longest reference video, when known
	// Runnable reports whether this build has an adapter for a route; nil
	// treats every route as runnable.
	Runnable func(registry.Route) bool
}

// Candidate is one ranked route.
type Candidate struct {
	Rank        int                   `json:"rank"`
	RouteID     string                `json:"route_id"`
	Provider    string                `json:"provider"`
	ModelID     string                `json:"model_id"`
	Family      string                `json:"family"`
	Variant     string                `json:"variant,omitempty"`
	Estimate    registry.Estimate     `json:"estimate"`
	PriceSource string                `json:"price_source"`
	PriceAsOf   string                `json:"price_as_of,omitempty"`
	PriceNote   string                `json:"price_note,omitempty"`
	Verified    bool                  `json:"verified"`
	Usable      bool                  `json:"usable"`
	Why         string                `json:"why_not_usable,omitempty"`
	CapsKnown   bool                  `json:"caps_known"`
	Caps        registry.Capabilities `json:"caps"`
}

// Excluded is a route that matched the op and family but cannot take the job.
type Excluded struct {
	RouteID string `json:"route_id"`
	Reason  string `json:"reason"`
}

// Result is the full quote.
type Result struct {
	Op             string       `json:"op"`
	Family         string       `json:"family,omitempty"`
	Job            registry.Job `json:"job"`
	CatalogAsOf    string       `json:"catalog_as_of"`
	CatalogOrigin  string       `json:"catalog_origin"`
	Stale          bool         `json:"stale"`
	Routes         []Candidate  `json:"routes"`
	Excluded       []Excluded   `json:"excluded,omitempty"`
	CheapestUsable string       `json:"cheapest_usable,omitempty"`
}

// Build ranks candidates. statuses supplies key and balance state per provider.
func Build(l *registry.Loaded, req Request, statuses map[string]account.Status) Result {
	res := Result{
		Op: req.Op, Family: req.Family, Job: req.Job,
		CatalogAsOf: l.GeneratedAt, CatalogOrigin: l.Origin,
		Stale: l.Age(time.Now()) > registry.StaleAfter,
	}
	for _, r := range l.Select(req.Op, req.Family, req.Variant, req.Provider, req.All) {
		if reason := reject(r, req); reason != "" {
			res.Excluded = append(res.Excluded, Excluded{r.ID, reason})
			continue
		}
		c := Candidate{
			RouteID: r.ID, Provider: r.Provider, ModelID: r.ModelID, Family: r.Family, Variant: r.Variant,
			Estimate: registry.EstimateJob(r.Price, req.Job), PriceSource: r.Price.Source,
			PriceAsOf: r.Price.AsOf, PriceNote: r.Price.Note, Verified: r.Verified,
			CapsKnown: len(r.Schema) > 0, Caps: r.Caps,
		}
		st, ok := statuses[r.Provider]
		switch {
		case req.Runnable != nil && !req.Runnable(r):
			c.Why = "no adapter for transport " + r.Transport + " in this build"
		case !ok:
			c.Why = "no credentials known for " + r.Provider
		case !st.Configured:
			c.Why = "missing " + strings.Join(st.Missing, ", ")
		case st.BalanceUSD != nil && c.Estimate.USD != nil && *st.BalanceUSD < *c.Estimate.USD:
			c.Why = fmt.Sprintf("balance %s below estimate", st.Balance)
		default:
			c.Usable = true
		}
		res.Routes = append(res.Routes, c)
	}
	sort.SliceStable(res.Routes, func(i, j int) bool { return less(res.Routes[i], res.Routes[j]) })
	for i := range res.Routes {
		res.Routes[i].Rank = i + 1
		if res.CheapestUsable == "" && res.Routes[i].Usable && res.Routes[i].Estimate.USD != nil {
			res.CheapestUsable = res.Routes[i].RouteID
		}
	}
	return res
}

// less orders priced routes by cost, then unpriced ones; within equal cost,
// usable routes come first.
func less(a, b Candidate) bool {
	ap, bp := a.Estimate.USD != nil, b.Estimate.USD != nil
	if ap != bp {
		return ap
	}
	if ap && *a.Estimate.USD != *b.Estimate.USD {
		return *a.Estimate.USD < *b.Estimate.USD
	}
	if a.Usable != b.Usable {
		return a.Usable
	}
	return a.RouteID < b.RouteID
}

// reject explains why a route cannot accept the job, or returns "". Routes
// without a schema are kept, because their limits are unknown rather than
// known to be violated; the candidate reports caps_known=false.
func reject(r registry.Route, req Request) string {
	c, j := r.Caps, req.Job
	if j.DurationSecs > 0 {
		if c.MinDuration > 0 && j.DurationSecs < float64(c.MinDuration) {
			return fmt.Sprintf("duration %gs below minimum %ds", j.DurationSecs, c.MinDuration)
		}
		if c.MaxDuration > 0 && j.DurationSecs > float64(c.MaxDuration) {
			return fmt.Sprintf("duration %gs above maximum %ds", j.DurationSecs, c.MaxDuration)
		}
	}
	if j.Resolution != "" && len(c.Resolutions) > 0 && !containsFold(c.Resolutions, j.Resolution) {
		return fmt.Sprintf("resolution %s not in %v", j.Resolution, c.Resolutions)
	}
	if j.Aspect != "" && len(c.Aspects) > 0 && !containsFold(c.Aspects, j.Aspect) && !containsFold(c.Aspects, "adaptive") {
		return fmt.Sprintf("aspect %s not in %v", j.Aspect, c.Aspects)
	}
	known := len(r.Schema) > 0
	if msg := countLimit("image", req.Images, c.MaxImages, known); msg != "" {
		return msg
	}
	if msg := countLimit("video", req.Videos, c.MaxVideos, known); msg != "" {
		return msg
	}
	if msg := countLimit("audio", req.Audios, c.MaxAudios, known); msg != "" {
		return msg
	}
	if req.VideoSecs > 0 && c.MaxVideoSecs > 0 && req.VideoSecs > float64(c.MaxVideoSecs) {
		return fmt.Sprintf("reference video %.1fs longer than %ds", req.VideoSecs, c.MaxVideoSecs)
	}
	return ""
}

func countLimit(kind string, n, max int, schemaKnown bool) string {
	switch {
	case n == 0:
		return ""
	case max > 0 && n > max:
		return fmt.Sprintf("%d %s references exceed maximum %d", n, kind, max)
	case max == 0 && schemaKnown:
		return "route accepts no " + kind + " references"
	}
	return ""
}

func containsFold(xs []string, x string) bool {
	for _, v := range xs {
		if strings.EqualFold(v, x) {
			return true
		}
	}
	return false
}
