package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/radjathaher/creative-cli/internal/registry"
	csync "github.com/radjathaher/creative-cli/internal/sync"
	"github.com/spf13/cobra"
)

func init() {
	register(func(root *cobra.Command) {
		c := &cobra.Command{
			Use:   "sync",
			Short: "Refresh models and prices from every provider into the local cache (free).",
			Long: `Refresh the route catalog: models, input schemas, limits, and prices, read
from free catalog and pricing endpoints only. Nothing is generated or billed.

The result goes to the local cache (` + "~/.cache/creative/routes.json.gz" + `), which
overrides the snapshot embedded in the binary while it is newer. Routes that
exist only in the cache are marked verified=false until a release includes them.
CI runs this with --out to refresh the embedded snapshot.`,
			Args: cobra.NoArgs,
			RunE: runSync,
		}
		c.Flags().String("out", "", "write here instead of the local cache (CI: internal/registry/data/routes.json.gz)")
		c.Flags().StringSlice("only", nil, "sync only these providers, e.g. --only kie,segmind")
		c.Flags().Bool("full", false, "refetch every fal price (slow: fal rate-limits its pricing API); CI uses this")
		root.AddCommand(c)
	})
}

func runSync(c *cobra.Command, _ []string) error {
	prev, err := registry.Load()
	if err != nil {
		return err
	}
	sources := csync.AllSources(&prev.Catalog, flagBool(c, "full"))
	if only := flagStrSlice(c, "only"); len(only) > 0 {
		sources = filterSources(sources, only)
		if len(sources) == 0 {
			return fmt.Errorf("--only matched no provider (have fal, openrouter, kie, segmind, manual)")
		}
	}
	cat := csync.Run(context.Background(), sources, &prev.Catalog)
	if len(sources) < len(csync.AllSources(nil, false)) {
		cat = mergeInto(&prev.Catalog, cat, sources)
	}
	path := flagStr(c, "out")
	if path == "" {
		path = registry.CachePath()
	}
	if err := registry.Write(path, cat); err != nil {
		return err
	}
	type summary struct {
		Path    string                         `json:"path"`
		Routes  int                            `json:"routes"`
		Sources map[string]registry.SourceInfo `json:"sources"`
	}
	return emitJSON(c, summary{path, len(cat.Routes), cat.Sources})
}

func filterSources(all []csync.Source, only []string) []csync.Source {
	var out []csync.Source
	for _, s := range all {
		for _, o := range only {
			if strings.EqualFold(s.Name(), strings.TrimSpace(o)) {
				out = append(out, s)
			}
		}
	}
	return out
}

// mergeInto keeps the previous routes of providers that were not synced.
func mergeInto(prev, fresh *registry.Catalog, synced []csync.Source) *registry.Catalog {
	done := map[string]bool{}
	for _, s := range synced {
		done[s.Name()] = true
	}
	for name, info := range prev.Sources {
		if !done[name] {
			fresh.Sources[name] = info
		}
	}
	for _, r := range prev.Routes {
		src := r.Provider
		if r.Price.Source == registry.SourceManual {
			src = "manual"
		}
		if !done[src] {
			fresh.Routes = append(fresh.Routes, r)
		}
	}
	return fresh
}

func flagStrSlice(c *cobra.Command, name string) []string {
	v, _ := c.Flags().GetStringSlice(name)
	return v
}
