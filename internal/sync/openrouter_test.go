package sync

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radjathaher/creative-cli/internal/registry"
)

func openRouterFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := filepath.Join("testdata", "openrouter")
	serve := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Write(b)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/videos/models", serve("videos.json"))
	mux.HandleFunc("/images/models", serve("images.json"))
	mux.HandleFunc("/models", serve("models.json"))
	mux.HandleFunc("/images/models/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/images/models/"), "/endpoints")
		serve(filepath.Join("endpoints", strings.ReplaceAll(id, "/", "_")+".json"))(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestOpenRouterFetch(t *testing.T) {
	srv := openRouterFixtureServer(t)
	o := NewOpenRouter()
	o.Base = srv.URL
	routes, err := o.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]registry.Route{}
	for _, r := range routes {
		if _, dup := by[r.ID]; dup {
			t.Errorf("duplicate route id %s", r.ID)
		}
		by[r.ID] = r
	}
	get := func(id string) registry.Route {
		t.Helper()
		r, ok := by[id]
		if !ok {
			t.Fatalf("missing route %s", id)
		}
		return r
	}

	// Seedance 2.5 serves four ops from one model id.
	for _, op := range []string{registry.OpVideoT2V, registry.OpVideoI2V, registry.OpVideoFLF2V, registry.OpVideoR2V} {
		r := get("openrouter:bytedance/seedance-2.5#" + op)
		if r.ModelID != "bytedance/seedance-2.5" || r.Family != "seedance-2.5" || r.Op != op {
			t.Errorf("seedance %s identity: %+v", op, r)
		}
	}
	seed := get("openrouter:bytedance/seedance-2.5#" + registry.OpVideoR2V)
	if p := seed.Price; p.Unit != registry.UnitMTokens || p.Formula != registry.FormulaSeedTok || *p.ValueUSD != 10.7 {
		t.Errorf("seedance price: %+v", p)
	}
	if c := seed.Caps; c.MinDuration != 4 || c.MaxDuration != 30 || len(c.Resolutions) != 2 || !c.FirstFrame || !c.LastFrame || !c.NativeAudio {
		t.Errorf("seedance caps: %+v", c)
	}
	assertUSD(t, "or seedance 720p 5s", seed.Price, registry.Job{Resolution: "720p", DurationSecs: 5}, 1.1556)
	assertUSD(t, "or seedance 720p 5s video in", seed.Price, registry.Job{Resolution: "720p", DurationSecs: 5, HasVideoInput: true}, 0.6912)

	s20 := get("openrouter:bytedance/seedance-2.0#" + registry.OpVideoT2V)
	assertUSD(t, "seedance 2.0 1080p video in", s20.Price, registry.Job{Resolution: "1080p", DurationSecs: 5, HasVideoInput: true}, 1.1421)
	if !hasTier(s20.Price, map[string]string{"resolution": "4k", "video_input": "true"}, 2.4) {
		t.Errorf("seedance 2.0 4k video-input tier missing: %+v", s20.Price.Tiers)
	}

	veo := get("openrouter:google/veo-3.1-fast#" + registry.OpVideoT2V)
	assertUSD(t, "veo default", veo.Price, registry.Job{Resolution: "1080p", DurationSecs: 8}, 0.96)
	assertUSD(t, "veo 4k", veo.Price, registry.Job{Resolution: "4K", DurationSecs: 8}, 2.4)
	if !strings.Contains(veo.Price.Note, "without_audio") {
		t.Errorf("veo note should keep the without-audio rates: %q", veo.Price.Note)
	}
	kling := get("openrouter:kwaivgi/kling-v3.0-pro#" + registry.OpVideoT2V)
	if *kling.Price.ValueUSD != 0.112 || kling.Family != "kling-3.0" || kling.Variant != "pro" {
		t.Errorf("kling: %s %s %+v", kling.Family, kling.Variant, kling.Price)
	}
	wan := get("openrouter:alibaba/wan-2.6#" + registry.OpVideoI2V)
	assertUSD(t, "wan 720p", wan.Price, registry.Job{Resolution: "720p", DurationSecs: 5}, 0.4)
	grok := get("openrouter:x-ai/grok-imagine-video#" + registry.OpVideoI2V)
	assertUSD(t, "grok cents 720p", grok.Price, registry.Job{Resolution: "720p", DurationSecs: 10}, 0.7)
	up := get("openrouter:black-forest-labs/flux-video-upscale")
	if up.Op != registry.OpVideoUpscale || up.Price.Unit != registry.UnitUnknown || !strings.Contains(up.Price.Note, "megapixel") {
		t.Errorf("flux upscale: %+v", up)
	}
	if a := get("openrouter:runway/aleph-2"); a.Op != registry.OpVideoEdit || *a.Price.ValueUSD != 0.28 {
		t.Errorf("aleph: %+v", a)
	}

	flare := get("openrouter:openai/gpt-image-2.5-flare#" + registry.OpImageEdit)
	if flare.Family != "gpt-image-2.5" || flare.Variant != "flare" || flare.Caps.MaxImages != 16 || flare.Price.Unit != registry.UnitMTokens || *flare.Price.ValueUSD != 30 {
		t.Errorf("flare: %+v", flare)
	}
	flux := get("openrouter:black-forest-labs/flux-3-image#" + registry.OpImageT2I)
	if flux.Price.Unit != registry.UnitImage || *flux.Price.ValueUSD != 0.048 {
		t.Errorf("flux-3 base: %+v", flux.Price)
	}
	assertUSD(t, "flux-3 4K", flux.Price, registry.Job{Resolution: "4K"}, 0.607)
	if p := get("openrouter:recraft/recraft-v4#" + registry.OpImageT2I).Price; p.Source != registry.SourceUnknown {
		t.Errorf("recraft without endpoints must be unknown: %+v", p)
	}
	if len(o.Warnings) == 0 {
		t.Error("expected a warning for the missing recraft endpoints")
	}

	gem := get("openrouter:google/gemini-3.8-flash#" + registry.OpUnderstandAud)
	if gem.Price.ValueUSD == nil || math.Abs(*gem.Price.ValueUSD-0.825) > 1e-9 {
		t.Errorf("gemini audio input price: %+v", gem.Price)
	}
	get("openrouter:google/gemini-3.8-flash#" + registry.OpUnderstandVid)
	get("openrouter:google/gemini-3.8-flash:batch#" + registry.OpUnderstandImg)
	get("openrouter:deepseek/deepseek-v4.1-flash-1017") // one op keeps the plain id
	get("openrouter:google/lyria-3-pro-preview#" + registry.OpMusicGenerate)
	get("openrouter:openai/gpt-audio#" + registry.OpVoiceTTS)
	if r := get("openrouter:google/gemini-3-pro-image#" + registry.OpImageT2I); r.Transport != "openrouter-image" {
		t.Errorf("image API models must not be duplicated from the chat catalog: %+v", r)
	}
	get("openrouter:google/gemini-3-pro-image#" + registry.OpUnderstandImg)
	for id := range by {
		if strings.HasPrefix(id, "openrouter:openrouter/") {
			t.Errorf("meta-router %s should be skipped", id)
		}
	}
}

func hasTier(p registry.Price, match map[string]string, v float64) bool {
	for _, t := range p.Tiers {
		if len(t.Match) != len(match) || math.Abs(t.ValueUSD-v) > 1e-9 {
			continue
		}
		ok := true
		for k, want := range match {
			ok = ok && t.Match[k] == want
		}
		if ok {
			return true
		}
	}
	return false
}
