//go:build live

package sync

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/radjathaher/creative-cli/internal/registry"
)

// TestLiveKIESegmindManual hits only free catalog, pricing, and doc endpoints.
// Run with: CGO_ENABLED=0 go test -tags live -run LiveKIE -v ./internal/sync/
func TestLiveKIESegmindManual(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	all := map[string][]registry.Route{}
	for _, src := range []Source{KIE{}, Segmind{}, Manual{}} {
		routes, err := src.Fetch(ctx)
		if err != nil {
			t.Fatalf("%s: %v", src.Name(), err)
		}
		bySource := map[string]int{}
		withSchema, other := 0, 0
		for _, r := range routes {
			bySource[r.Price.Source]++
			if r.Schema != nil {
				withSchema++
			}
			if r.Op == registry.OpOther {
				other++
			}
		}
		t.Logf("%s: %d routes, price sources %v, %d with schema, %d op=other", src.Name(), len(routes), bySource, withSchema, other)
		all[src.Name()] = routes
	}
	find := func(src, id string) registry.Route {
		for _, r := range all[src] {
			if r.ID == id {
				return r
			}
		}
		t.Errorf("%s: no route %s", src, id)
		return registry.Route{}
	}
	show := func(label string, r registry.Route, j registry.Job) {
		e := registry.EstimateJob(r.Price, j)
		usd := "unknown"
		if e.USD != nil {
			usd = fmt.Sprintf("$%.4f", *e.USD)
		}
		t.Logf("%-44s %-10s %s  [%s, %s]", label, usd, e.Basis, r.Price.Source, r.Price.AsOf)
	}
	for _, res := range []string{"480p", "720p", "1080p"} {
		j := registry.Job{Resolution: res, DurationSecs: 5, Aspect: "16:9"}
		show("seedance-2.5 kie "+res+" 5s", find("kie", "kie:bytedance/seedance-2-5"), j)
		show("seedance-2.5 segmind "+res+" 5s", find("segmind", "segmind:seedance-2.5#video.t2v"), j)
	}
	for _, res := range []string{"360p", "720p", "1080p"} {
		for _, video := range []bool{false, true} {
			j := registry.Job{Resolution: res, DurationSecs: 6, HasVideoInput: video}
			tag := fmt.Sprintf("%s 6s video=%v", res, video)
			show("omni-1.1 kie "+tag, find("kie", "kie:google/gemini-omni-flash-1-1"), j)
			show("omni-1.1 segmind "+tag, find("segmind", "segmind:gemini-omni-1.1"), j)
		}
	}
	show("gpt-image-2.5 flare kie 1k", find("kie", "kie:gpt-image-2-5-flare-text-to-image"), registry.Job{Resolution: "1k"})
	show("topaz segmind 1080p 5s", find("segmind", "segmind:topaz-video-upscale"), registry.Job{Resolution: "1080p", DurationSecs: 5})
}
