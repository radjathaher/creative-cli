package sync

import (
	"time"

	"github.com/radjathaher/creative-cli/internal/registry"
)

// priorMaxAge is how old a prior route's price may be before an incremental
// sync fetches it again; it matches the registry's staleness threshold.
const priorMaxAge = 7 * 24 * time.Hour

// NewFalWithPrior returns a fal source that reuses prices, schemas, caps, and
// param maps from prev for endpoints priced within the last 7 days, so a
// local sync with a fresh snapshot only lists the catalog and fetches new or
// stale endpoints. full forces a complete refetch, as CI does. Endpoints that
// had no price in prev are priced together in a trailing batch.
func NewFalWithPrior(prev *registry.Catalog, full bool) *Fal {
	f := NewFal()
	f.Full = full
	if prev == nil {
		return f
	}
	f.PriorUnpriced = map[string]bool{}
	for _, r := range prev.Routes {
		if r.Provider != "fal" {
			continue
		}
		f.Prior = append(f.Prior, r)
		if r.Price.Source == registry.SourceUnknown {
			f.PriorUnpriced[r.ModelID] = true
		}
	}
	return f
}

// reusePrior copies fresh prior data into routes and reports which routes
// need no fetching. Identity fields always come from the live catalog, so a
// renamed or reclassified endpoint is picked up without a full sync.
func (f *Fal) reusePrior(routes []registry.Route) []bool {
	reused := make([]bool, len(routes))
	if f.Full || len(f.Prior) == 0 {
		return reused
	}
	prior := make(map[string]registry.Route, len(f.Prior))
	for _, r := range f.Prior {
		prior[r.ModelID] = r
	}
	now := time.Now().UTC()
	for i := range routes {
		p, ok := prior[routes[i].ModelID]
		if !ok {
			continue
		}
		asOf, err := time.Parse("2006-01-02", p.Price.AsOf)
		if err != nil || now.Sub(asOf) > priorMaxAge {
			continue
		}
		routes[i].Price, routes[i].Schema, routes[i].Caps, routes[i].ParamMap = p.Price, p.Schema, p.Caps, p.ParamMap
		reused[i] = true
	}
	return reused
}
