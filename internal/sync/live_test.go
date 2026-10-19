//go:build live

package sync

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/radjathaher/creative-cli/internal/registry"
)

// TestLive syncs fal and OpenRouter against production, using only free
// catalog, pricing, OpenAPI, and model-page endpoints, and prints route counts
// and sample estimates. Run with: go test -tags live -run TestLive ./internal/sync/
func TestLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	for _, src := range []interface {
		Source
		warnings() []string
	}{liveFal{NewFal()}, liveOR{NewOpenRouter()}} {
		start := time.Now()
		routes, err := src.Fetch(ctx)
		if err != nil {
			t.Errorf("%s: %v", src.Name(), err)
			continue
		}
		fmt.Printf("\n== %s: %d routes in %s, %d warnings\n", src.Name(), len(routes), time.Since(start).Round(time.Second), len(src.warnings()))
		for i, w := range src.warnings() {
			if i < 8 {
				fmt.Println("   warn:", w)
			}
		}
		printCounts(routes)
		printSamples(routes)
	}
}

type liveFal struct{ *Fal }

func (l liveFal) warnings() []string { return l.Warnings }

type liveOR struct{ *OpenRouter }

func (l liveOR) warnings() []string { return l.Warnings }

var liveFamilies = []string{"gpt-image-2", "gpt-image-2.5", "seedance-2.5", "gemini-omni-flash-1.1", "topaz"}

func printCounts(routes []registry.Route) {
	byOp, byFam, unknown := map[string]int{}, map[string]map[string]int{}, 0
	for _, r := range routes {
		byOp[r.Op]++
		if r.Price.ValueUSD == nil && len(r.Price.Tiers) == 0 {
			unknown++
		}
		if byFam[r.Family] == nil {
			byFam[r.Family] = map[string]int{}
		}
		byFam[r.Family][r.Op]++
	}
	var ops []string
	for op, n := range byOp {
		ops = append(ops, fmt.Sprintf("%s=%d", op, n))
	}
	sort.Strings(ops)
	fmt.Printf("   ops: %s\n   price unknown: %d\n", strings.Join(ops, " "), unknown)
	for _, f := range liveFamilies {
		fmt.Printf("   %-22s %v\n", f, byFam[f])
	}
}

func printSamples(routes []registry.Route) {
	samples := []struct {
		family, op string
		job        registry.Job
	}{
		{"seedance-2.5", registry.OpVideoT2V, registry.Job{Resolution: "720p", DurationSecs: 5}},
		{"gemini-omni-flash-1.1", registry.OpVideoR2V, registry.Job{Resolution: "720p", DurationSecs: 6, HasVideoInput: true}},
		{"topaz", registry.OpVideoUpscale, registry.Job{DurationSecs: 5}},
	}
	for _, s := range samples {
		for _, r := range routes {
			if r.Family != s.family || r.Op != s.op {
				continue
			}
			e := registry.EstimateJob(r.Price, s.job)
			usd := "n/a"
			if e.USD != nil {
				usd = fmt.Sprintf("$%.4f", *e.USD)
			}
			fmt.Printf("   sample %-48s %-8s %s [%s]\n", r.ID, usd, e.Basis, r.Price.Source)
		}
	}
}
