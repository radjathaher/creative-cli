package sync

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/radjathaher/creative-cli/internal/registry"
)

// ManualPricesJSON is the hand-maintained route table for providers without a
// price API (OpenAI, Codex-LB, ElevenLabs, Google, ZapCap, Meshy, Tripo). Each
// entry is a registry.Route whose price cites the page it was read from.
//
//go:embed data/manual-prices.json
var ManualPricesJSON []byte

// manualAsOf is the date the table was last checked by hand.
const manualAsOf = "2026-10-19"

// Manual serves the hand-maintained table as a Source.
type Manual struct {
	// Data overrides the embedded table; tests use it.
	Data []byte
}

func (Manual) Name() string { return "manual" }

func (m Manual) Fetch(context.Context) ([]registry.Route, error) {
	data := m.Data
	if data == nil {
		data = ManualPricesJSON
	}
	return manualRoutes(data)
}

// manualRoutes decodes and validates the table. A malformed entry fails the
// whole source, because a hand table that silently loses rows hides routes.
func manualRoutes(data []byte) ([]registry.Route, error) {
	var routes []registry.Route
	if err := json.Unmarshal(data, &routes); err != nil {
		return nil, fmt.Errorf("decode manual prices: %w", err)
	}
	seen := map[string]bool{}
	for i := range routes {
		r := &routes[i]
		if r.ID == "" {
			r.ID = r.Provider + ":" + r.ModelID
		}
		switch {
		case r.Provider == "" || r.ModelID == "":
			return nil, fmt.Errorf("manual route %d: provider and model_id are required", i)
		case r.Family == "":
			return nil, fmt.Errorf("manual route %s: family is required", r.ID)
		case !slices.Contains(registry.Ops, r.Op):
			return nil, fmt.Errorf("manual route %s: unknown op %q", r.ID, r.Op)
		case r.Price.SourceURL == "" && r.SourceURL == "":
			return nil, fmt.Errorf("manual route %s: source_url is required", r.ID)
		case seen[r.ID]:
			return nil, fmt.Errorf("manual route %s: duplicate id", r.ID)
		}
		seen[r.ID] = true
		r.Price.Source = registry.SourceManual
		if r.Price.SourceURL == "" {
			r.Price.SourceURL = r.SourceURL
		}
		if r.Price.AsOf == "" {
			r.Price.AsOf = manualAsOf
		}
		if r.Price.Unit == "" {
			r.Price.Unit = registry.UnitUnknown
		}
	}
	return routes, nil
}
