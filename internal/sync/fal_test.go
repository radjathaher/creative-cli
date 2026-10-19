package sync

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/radjathaher/creative-cli/internal/registry"
)

// falFixtureServer serves the recorded fal responses. The pricing handler
// mimics the real API: any unknown endpoint id fails the whole batch with 404,
// and the first pricing call is rate limited once to exercise the backoff.
func falFixtureServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	dir := filepath.Join("testdata", "fal")
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("fixture %s: %v", name, err)
		}
		return b
	}
	var prices struct {
		Prices []falPrice `json:"prices"`
	}
	if err := json.Unmarshal(read("pricing.json"), &prices); err != nil {
		t.Fatal(err)
	}
	byID := map[string]falPrice{}
	for _, p := range prices.Prices {
		byID[p.EndpointID] = p
	}
	var priceCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "Mg" {
			w.Write(read("models-2.json"))
			return
		}
		w.Write(read("models-1.json"))
	})
	mux.HandleFunc("/v1/models/pricing", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Key test-key" {
			http.Error(w, `{"error":"unauthorized"}`, 401)
			return
		}
		if atomic.AddInt32(&priceCalls, 1) == 1 {
			http.Error(w, `{"error":"rate limited"}`, 429)
			return
		}
		var out []falPrice
		for _, id := range r.URL.Query()["endpoint_id"] {
			p, ok := byID[id]
			if !ok {
				http.Error(w, `{"error":{"type":"not_found","message":"Endpoint(s) not found"}}`, 404)
				return
			}
			out = append(out, p)
		}
		json.NewEncoder(w).Encode(map[string]any{"prices": out, "has_more": false})
	})
	mux.HandleFunc("/openapi", func(w http.ResponseWriter, r *http.Request) {
		name := strings.ReplaceAll(r.URL.Query().Get("endpoint_id"), "/", "_") + ".json"
		b, err := os.ReadFile(filepath.Join(dir, "openapi", name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	})
	mux.HandleFunc("/models/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/models/"), "/llms.txt")
		b, err := os.ReadFile(filepath.Join(dir, "pages", strings.ReplaceAll(id, "/", "_")+".txt"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &priceCalls
}

func testFal(srv *httptest.Server) *Fal {
	f := NewFal()
	f.APIBase, f.OpenAPIBase, f.PageBase = srv.URL, srv.URL+"/openapi", srv.URL+"/models"
	f.Key, f.Pause, f.Backoff, f.BatchSize = "test-key", 0, 0, 8
	return f
}

func TestFalFetch(t *testing.T) {
	srv, calls := falFixtureServer(t)
	f := testFal(srv)
	routes, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 15 {
		t.Fatalf("routes = %d, want 15 across two catalog pages", len(routes))
	}
	if *calls < 3 {
		t.Errorf("pricing calls = %d; expected a retry after 429 and a bisection after 404", *calls)
	}
	by := map[string]registry.Route{}
	for _, r := range routes {
		by[r.ModelID] = r
	}

	seed := by["bytedance/seedance-2.5/us/text-to-video"]
	if seed.Op != registry.OpVideoT2V || seed.Family != "seedance-2.5" || seed.ID != "fal:bytedance/seedance-2.5/us/text-to-video" {
		t.Errorf("seedance identity: %+v", seed)
	}
	if seed.Price.Unit != registry.UnitMTokens || seed.Price.Formula != registry.FormulaSeedTok || *seed.Price.ValueUSD != 21.4 {
		t.Errorf("seedance price: %+v", seed.Price)
	}
	assertUSD(t, "seedance 720p 5s", seed.Price, registry.Job{Resolution: "720p", DurationSecs: 5}, 2.3112)
	assertUSD(t, "seedance 1080p 5s", seed.Price, registry.Job{Resolution: "1080p", DurationSecs: 5}, 5.6869)
	if seed.Caps.MinDuration != 4 || seed.Caps.MaxDuration != 30 || !seed.Caps.NativeAudio {
		t.Errorf("seedance caps: %+v", seed.Caps)
	}

	r2v := by["bytedance/seedance-2.5/us/reference-to-video"]
	if r2v.Op != registry.OpVideoR2V || r2v.ParamMap["video"] != "video_urls" || r2v.ParamMap["image"] != "image_urls" || r2v.ParamMap["audio"] != "audio_urls" {
		t.Errorf("seedance r2v: op %s params %v", r2v.Op, r2v.ParamMap)
	}
	// fal states these limits only in prose: "Up to 10 videos. Each video must be 1.8 to 30.2 seconds".
	if c := r2v.Caps; c.MaxVideos != 10 || c.MaxImages != 30 || c.MaxAudios != 10 || c.MaxVideoSecs != 30 {
		t.Errorf("seedance r2v caps: %+v", r2v.Caps)
	}

	i2v := by["bytedance/seedance-2.5/us/image-to-video"]
	if i2v.ParamMap["first_frame"] != "image_url" || i2v.ParamMap["last_frame"] != "end_image_url" || !i2v.Caps.FirstFrame || !i2v.Caps.LastFrame {
		t.Errorf("seedance i2v frames: %v %+v", i2v.ParamMap, i2v.Caps)
	}

	omni := by["google/gemini-omni-flash/v1.1/reference-to-video"]
	if omni.Op != registry.OpVideoR2V || omni.Family != "gemini-omni-flash-1.1" {
		t.Errorf("omni identity: %s %s", omni.Op, omni.Family)
	}
	if c := omni.Caps; c.MaxVideos != 3 || c.MaxImages != 10 || c.MaxVideoSecs != 10 || c.MinDuration != 3 || c.MaxDuration != 10 {
		t.Errorf("omni caps: %+v", c)
	}
	if omni.Price.Source != registry.SourcePage || len(omni.Price.Tiers) != 4 {
		t.Errorf("omni price: %+v", omni.Price)
	}
	assertUSD(t, "omni 720p 6s", omni.Price, registry.Job{Resolution: "720p", DurationSecs: 6}, 0.60)
	assertUSD(t, "omni 360p 6s", omni.Price, registry.Job{Resolution: "360p", DurationSecs: 6}, 0.18)
	assertUSD(t, "omni default res 6s", omni.Price, registry.Job{DurationSecs: 6}, 0.60)

	topaz := by["topaz/upscale/video/precision"]
	if topaz.Op != registry.OpVideoUpscale || topaz.ParamMap["factor"] != "upscale_factor" || topaz.ParamMap["fps"] != "target_fps" || topaz.ParamMap["model"] != "model" {
		t.Errorf("topaz: %s %v", topaz.Op, topaz.ParamMap)
	}
	assertUSD(t, "topaz 5s", topaz.Price, registry.Job{DurationSecs: 5}, 0.05)
	assertUSD(t, "topaz 1080p 5s", topaz.Price, registry.Job{Resolution: "1080p", DurationSecs: 5}, 0.10)
	assertUSD(t, "topaz 1080p 5s 60fps", topaz.Price, registry.Job{Resolution: "1080p", DurationSecs: 5, FPS: 60}, 0.20)

	ups := by["fal-ai/bytedance-upscaler/upscale/video"]
	assertUSD(t, "bytedance upscaler 4k 10s 60fps", ups.Price, registry.Job{Resolution: "4k", DurationSecs: 10, FPS: 60}, 0.576)

	img := by["openai/gpt-image-2"]
	if img.Price.Unit != registry.UnitMTokens || img.Price.ValueUSD == nil || *img.Price.ValueUSD != 30 || img.Price.Source != registry.SourcePage {
		t.Errorf("gpt-image placeholder should be replaced by page token rate: %+v", img.Price)
	}
	if e := registry.EstimateJob(img.Price, registry.Job{}); e.USD != nil {
		t.Errorf("gpt-image token price must not estimate without a token count: %+v", e)
	}

	if p := by["tripo3d/h3.1/multiview-to-3d"].Price; p.Unit != registry.UnitCredit || p.Note == "" {
		t.Errorf("tripo credits: %+v", p)
	}
	if p := by["fal-ai/hunyuan3d-v3/image-to-3d"].Price; p.Unit != registry.UnitRequest || *p.ValueUSD != 0.375 {
		t.Errorf("hunyuan3d page request price: %+v", p)
	}
	if p := by["fal-ai/meshy/rigging"].Price; p.Unit != registry.UnitRequest || *p.ValueUSD != 0.8 || !strings.Contains(p.Note, "$0.20 per request") {
		t.Errorf("meshy rigging keeps API price and notes the page: %+v", p)
	}
	if r := by["fal-ai/meshy/rigging"]; r.Op != registry.Op3DRig || r.ParamMap["mesh"] != "model_url" {
		t.Errorf("meshy rigging: %s %v", r.Op, r.ParamMap)
	}
	if p := by["fal-ai/elevenlabs/speech-to-text/scribe-v2"].Price; p.Unit != registry.UnitMinute {
		t.Errorf("scribe: %+v", p)
	}
	if p := by["fal-ai/triposr"].Price; p.Source != registry.SourceUnknown {
		t.Errorf("triposr has no price and must stay unknown: %+v", p)
	}
	if !by["fal-ai/old-model/v1"].Deprecated {
		t.Error("deprecated status not carried")
	}
	if flux := by["fal-ai/flux/dev"]; flux.Schema != nil || flux.Price.Unit != registry.UnitMegapixel {
		t.Errorf("flux should be priced but not detail-fetched: %+v", flux)
	}
	if len(f.Warnings) == 0 {
		t.Error("expected a warning for the unpriced triposr endpoint")
	}
}

func TestFalPriorUnpricedIsolated(t *testing.T) {
	srv, calls := falFixtureServer(t)
	f := testFal(srv)
	f.BatchSize = 50
	f.PriorUnpriced = map[string]bool{"fal-ai/triposr": true, "fal-ai/old-model/v1": true}
	if _, err := f.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	// One 429 retry and one healthy batch, then the two suspects fail together
	// and are split once; the healthy ids are never bisected.
	if *calls != 5 {
		t.Errorf("pricing calls = %d, want 5", *calls)
	}
}

func TestFalFetchWithoutKey(t *testing.T) {
	srv, _ := falFixtureServer(t)
	f := testFal(srv)
	f.Key = ""
	t.Setenv("FAL_KEY", "")
	routes, err := f.Fetch(context.Background())
	if err != nil || len(routes) != 15 {
		t.Fatalf("routes %d err %v", len(routes), err)
	}
}

func TestFalUnitPrice(t *testing.T) {
	cases := []struct {
		unit   string
		value  float64
		family string
		want   string
		usd    *float64
	}{
		{"seconds", 0.03, "x", registry.UnitSecond, registry.F(0.03)},
		{"5 seconds", 0.5, "x", registry.UnitSecond, registry.F(0.1)},
		{"images", 0.04, "x", registry.UnitImage, registry.F(0.04)},
		{"processed megapixels", 0.001, "x", registry.UnitMegapixel, registry.F(0.001)},
		{"minutes", 0.008, "x", registry.UnitMinute, registry.F(0.008)},
		{"1000 characters ", 0.1, "x", registry.UnitKChars, registry.F(0.1)},
		{"generations", 0.8, "x", registry.UnitRequest, registry.F(0.8)},
		{"videos", 0.1, "x", registry.UnitRequest, registry.F(0.1)},
		{"1000 tokens", 0.0112, "seedance-2.0", registry.UnitMTokens, registry.F(11.2)},
		{"1000 tokens", 0.002, "llm", registry.UnitMTokens, registry.F(2)},
		{"credits", 0.01, "tripo-h3.1", registry.UnitCredit, registry.F(0.01)},
		{"units", 1, "gpt-image-2.5", registry.UnitUnknown, nil},
		{"units", 0.015, "hunyuan3d-3.0", registry.UnitUnknown, registry.F(0.015)},
		{"compute seconds", 0.0005, "x", registry.UnitUnknown, registry.F(0.0005)},
	}
	for _, c := range cases {
		u := c.unit
		p := falUnitPrice(falPrice{UnitPrice: registry.F(c.value), Unit: &u, Currency: "USD"}, c.family, "")
		if p.Unit != c.want {
			t.Errorf("%q: unit %q, want %q", c.unit, p.Unit, c.want)
		}
		if (p.ValueUSD == nil) != (c.usd == nil) || (c.usd != nil && math.Abs(*p.ValueUSD-*c.usd) > 1e-9) {
			t.Errorf("%q: value %v, want %v", c.unit, p.ValueUSD, c.usd)
		}
		if (p.Formula == registry.FormulaSeedTok) != strings.HasPrefix(c.family, "seedance") && c.want == registry.UnitMTokens {
			t.Errorf("%q %s: formula %q", c.unit, c.family, p.Formula)
		}
		if c.unit == "units" && c.value == 1 && p.Source != registry.SourceUnknown {
			t.Errorf("placeholder unit must be unknown, got %s", p.Source)
		}
	}
}

func assertUSD(t *testing.T, name string, p registry.Price, j registry.Job, want float64) {
	t.Helper()
	e := registry.EstimateJob(p, j)
	if e.USD == nil || math.Abs(*e.USD-want) > 0.001 {
		t.Errorf("%s: estimate %v (%s), want %.4f", name, e.USD, e.Basis, want)
	}
}

func TestFalWithFreshPriorSkipsPricing(t *testing.T) {
	srv, calls := falFixtureServer(t)
	first, err := testFal(srv).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	before := *calls
	prev := &registry.Catalog{Routes: first}

	f := NewFalWithPrior(prev, false)
	f.APIBase, f.OpenAPIBase, f.PageBase = srv.URL, srv.URL+"/openapi", srv.URL+"/models"
	f.Key, f.Pause, f.Backoff = "test-key", 0, 0
	second, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if *calls != before {
		t.Errorf("fresh prior made %d pricing calls, want 0", *calls-before)
	}
	for i := range second {
		if second[i].Price.Unit != first[i].Price.Unit || len(second[i].Schema) != len(first[i].Schema) {
			t.Errorf("%s not reused: %+v", second[i].ID, second[i].Price)
		}
	}

	// A stale price is refetched; Full refetches everything.
	stale := append([]registry.Route(nil), first...)
	for i := range stale {
		if stale[i].ModelID == "fal-ai/flux/dev" {
			stale[i].Price.AsOf = "2020-01-01"
		}
	}
	f = NewFalWithPrior(&registry.Catalog{Routes: stale}, false)
	f.APIBase, f.OpenAPIBase, f.PageBase = srv.URL, srv.URL+"/openapi", srv.URL+"/models"
	f.Key, f.Pause, f.Backoff = "test-key", 0, 0
	before = *calls
	if _, err := f.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if *calls-before != 1 {
		t.Errorf("stale prior made %d pricing calls, want 1", *calls-before)
	}
}
