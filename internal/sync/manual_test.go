package sync

import (
	"context"
	"os"
	"testing"

	"github.com/radjathaher/creative-cli/internal/registry"
)

func TestManualTableIsValid(t *testing.T) {
	routes, err := Manual{}.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	providers := map[string]int{}
	for _, r := range routes {
		providers[r.Provider]++
		if r.Op == "" || r.Family == "" || r.Price.SourceURL == "" || r.Transport == "" {
			t.Errorf("%s: op %q family %q source_url %q transport %q", r.ID, r.Op, r.Family, r.Price.SourceURL, r.Transport)
		}
		if r.Price.Source != registry.SourceManual || r.Price.AsOf != "2026-10-19" {
			t.Errorf("%s: source %s as_of %s", r.ID, r.Price.Source, r.Price.AsOf)
		}
		// Unknown prices must stay unknown, never zero, except the Codex-LB pool.
		if r.Price.ValueUSD != nil && *r.Price.ValueUSD == 0 && r.Provider != "codex-lb" {
			t.Errorf("%s: zero price outside codex-lb", r.ID)
		}
	}
	for _, p := range []string{"openai", "codex-lb", "elevenlabs", "google", "zapcap"} {
		if providers[p] == 0 {
			t.Errorf("no %s routes", p)
		}
	}

	byID := map[string]registry.Route{}
	for _, r := range routes {
		byID[r.ID] = r
	}
	high := registry.EstimateJob(byID["openai:gpt-image-2"].Price, registry.Job{Quality: "high"})
	if high.USD == nil || *high.USD != 0.211 {
		t.Errorf("openai gpt-image-2 high: %+v", high)
	}
	if e := registry.EstimateJob(byID["codex-lb:gpt-image-2"].Price, registry.Job{}); e.USD == nil || *e.USD != 0 {
		t.Errorf("codex-lb: %+v", e)
	}
	flare := byID["openai:gpt-image-2.5-flare"]
	if flare.Family != "gpt-image-2.5" || flare.Variant != "flare" || flare.Price.ValueUSD != nil {
		t.Errorf("openai flare: %+v", flare)
	}
	omni := registry.EstimateJob(byID["google:gemini-omni-1.1-flash"].Price, registry.Job{Resolution: "720p", DurationSecs: 6})
	if omni.USD == nil || abs(*omni.USD-0.6) > 1e-9 {
		t.Errorf("google omni: %+v", omni)
	}
}

func TestManualTableRejectsBadRows(t *testing.T) {
	for _, f := range []string{"bad-op.json", "no-source.json"} {
		data, err := os.ReadFile("testdata/manual/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := (Manual{Data: data}).Fetch(context.Background()); err == nil {
			t.Errorf("%s: accepted", f)
		}
	}
}
