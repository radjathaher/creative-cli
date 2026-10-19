package sync

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/radjathaher/creative-cli/internal/registry"
)

// AllSources lists every provider source in the order their routes are merged.
// With full false, fal reuses prices from prev that are under 7 days old, which
// turns an 11-minute rate-limited crawl into seconds; CI passes full.
func AllSources(prev *registry.Catalog, full bool) []Source {
	return []Source{NewFalWithPrior(prev, full), NewOpenRouter(), KIE{}, Segmind{}, Manual{}}
}

// Run fetches every source concurrently and merges the results into one
// catalog. A failing source is recorded in Sources with its error and keeps
// the routes it had in prev, so one outage never wipes a provider from quotes.
func Run(ctx context.Context, sources []Source, prev *registry.Catalog) *registry.Catalog {
	type result struct {
		name   string
		routes []registry.Route
		err    error
		took   time.Duration
	}
	ch := make(chan result, len(sources))
	for _, s := range sources {
		go func(s Source) {
			start := time.Now()
			routes, err := s.Fetch(ctx)
			ch <- result{s.Name(), routes, err, time.Since(start)}
		}(s)
	}

	out := &registry.Catalog{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Sources:     map[string]registry.SourceInfo{},
	}
	byName := map[string]result{}
	for range sources {
		r := <-ch
		byName[r.name] = r
		fmt.Fprintf(os.Stderr, "sync %-10s %5d routes in %s", r.name, len(r.routes), r.took.Round(time.Second))
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "  ERROR: %v", r.err)
		}
		fmt.Fprintln(os.Stderr)
	}

	seen := map[string]bool{}
	for _, s := range sources {
		r := byName[s.Name()]
		info := registry.SourceInfo{FetchedAt: out.GeneratedAt, Routes: len(r.routes)}
		routes := r.routes
		if r.err != nil {
			info.Error = r.err.Error()
			if len(routes) == 0 && prev != nil {
				routes = previousRoutes(prev, s.Name())
				info.FetchedAt = prev.Sources[s.Name()].FetchedAt
				info.Routes = len(routes)
			}
		}
		out.Sources[s.Name()] = info
		for _, rt := range routes {
			if seen[rt.ID] {
				continue
			}
			seen[rt.ID] = true
			out.Routes = append(out.Routes, rt)
		}
	}
	return out
}

func previousRoutes(prev *registry.Catalog, provider string) []registry.Route {
	var out []registry.Route
	for _, r := range prev.Routes {
		if r.Provider == provider || (provider == "manual" && r.Price.Source == registry.SourceManual) {
			out = append(out, r)
		}
	}
	return out
}
