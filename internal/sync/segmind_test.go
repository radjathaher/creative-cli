package sync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/radjathaher/creative-cli/internal/registry"
)

func segFixture(t *testing.T, slug string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/segmind/" + slug + ".html")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func segAvg(v float64) registry.Price {
	return registry.Price{Unit: registry.UnitRequest, ValueUSD: registry.F(v), Source: registry.SourceAverage}
}

func TestSegmindPricingSeedance(t *testing.T) {
	p, ok := parseSegmindPricing(segFixture(t, "seedance-2.5"), segAvg(2.76))
	if !ok || p.Source != registry.SourcePage {
		t.Fatalf("unparsed: %+v", p)
	}
	if p.Unit != registry.UnitMTokens || p.Formula != registry.FormulaSeedTok || *p.ValueUSD != 10.9675 {
		t.Fatalf("token base: unit %s formula %s value %v", p.Unit, p.Formula, *p.ValueUSD)
	}
	// V1: $0.1065/s at 480p, $0.239 at 720p, $0.588 at 1080p (16:9).
	for res, perSec := range map[string]float64{"480p": 0.1064912685, "720p": 0.238926598, "1080p": 0.587569954} {
		e := registry.EstimateJob(p, registry.Job{Resolution: res, DurationSecs: 5})
		if e.USD == nil || abs(*e.USD/5-perSec) > 0.0001 {
			t.Errorf("%s: %v (%s), want %.4f/s", res, e.USD, e.Basis, perSec)
		}
	}
	e := registry.EstimateJob(p, registry.Job{Resolution: "720p", DurationSecs: 10, HasVideoInput: true})
	if e.USD == nil || abs(*e.USD-1.428768) > 0.001 {
		t.Errorf("720p video input: %v (%s)", e.USD, e.Basis)
	}
	// Aspect ratios that cost a different amount get their own tier.
	e = registry.EstimateJob(p, registry.Job{Resolution: "480p", DurationSecs: 10, Aspect: "1:1"})
	if e.USD == nil || abs(*e.USD/10-0.1018) > 0.0002 {
		t.Errorf("480p 1:1: %v (%s)", e.USD, e.Basis)
	}
}

func TestSegmindPricingPages(t *testing.T) {
	cases := []struct {
		slug string
		job  registry.Job
		want float64
		unit string
	}{
		{"gemini-omni-1.1", registry.Job{Resolution: "360p", DurationSecs: 6}, 0.0422 * 6, registry.UnitSecond},
		{"gemini-omni-1.1", registry.Job{Resolution: "720p", DurationSecs: 6}, 0.1267 * 6, registry.UnitSecond},
		{"gemini-omni-1.1", registry.Job{Resolution: "1080p", DurationSecs: 6}, 0.19 * 6, registry.UnitSecond},
		{"topaz-video-upscale", registry.Job{Resolution: "1080p", DurationSecs: 5}, 0.125, registry.UnitSecond},
		{"topaz-video-upscale", registry.Job{Resolution: "720p", DurationSecs: 5}, 0.0625, registry.UnitSecond},
		{"topaz-video-upscale", registry.Job{Resolution: "4k", DurationSecs: 5}, 0.5, registry.UnitSecond},
		{"topaz-video-upscale", registry.Job{Resolution: "1080p", DurationSecs: 5, FPS: 60}, 0.25, registry.UnitSecond},
		{"veed-subtitles", registry.Job{DurationSecs: 120}, 0.286, registry.UnitMinute},
		{"elevenlabs-music", registry.Job{DurationSecs: 60}, 0.1875, registry.UnitSecond},
		{"elevenlabs-v4-tts", registry.Job{Chars: 1000}, 0.16875, registry.UnitKChars},
		{"gemini-omni-flash", registry.Job{Resolution: "720p", DurationSecs: 5}, 0.5, registry.UnitSecond},
		{"gpt-image-2", registry.Job{Quality: "high"}, 0.22, registry.UnitImage},
	}
	for _, c := range cases {
		p, ok := parseSegmindPricing(segFixture(t, c.slug), segAvg(0.5))
		if !ok || p.Unit != c.unit {
			t.Errorf("%s: ok %v unit %s, want %s", c.slug, ok, p.Unit, c.unit)
			continue
		}
		e := registry.EstimateJob(p, c.job)
		if e.USD == nil || abs(*e.USD-c.want) > 1e-6 {
			t.Errorf("%s %+v: %v (%s), want %g", c.slug, c.job, e.USD, e.Basis, c.want)
		}
	}
	for slug, note := range map[string]string{"gpt-image-2.5-flare": "token-priced", "video-captioner": "GPU second"} {
		p, ok := parseSegmindPricing(segFixture(t, slug), segAvg(0.0569))
		if !ok || p.Source != registry.SourceAverage || !strings.Contains(p.Note, note) {
			t.Errorf("%s: %+v", slug, p)
		}
	}
}

// The HTML tables are the fallback when a page lacks the pricingData payload.
func TestSegmindPricingHTMLFallback(t *testing.T) {
	strip := func(b []byte) []byte {
		s := string(b)
		if i := strings.Index(s, "<script>"); i >= 0 {
			s = s[:i]
		}
		return []byte(s)
	}
	p, ok := parseSegmindPricing(strip(segFixture(t, "seedance-2.5")), segAvg(2.76))
	if !ok || p.Formula != registry.FormulaSeedTok {
		t.Fatalf("seedance html: %+v", p)
	}
	e := registry.EstimateJob(p, registry.Job{Resolution: "720p", DurationSecs: 5})
	if e.USD == nil || abs(*e.USD-0.2389*5) > 1e-6 {
		t.Errorf("seedance html 720p: %v (%s)", e.USD, e.Basis)
	}
	p, ok = parseSegmindPricing(strip(segFixture(t, "topaz-video-upscale")), segAvg(0.43))
	if e := registry.EstimateJob(p, registry.Job{Resolution: "1080p", DurationSecs: 4}); !ok || e.USD == nil || abs(*e.USD-0.1) > 1e-6 {
		t.Errorf("topaz html: %+v", p)
	}
	if _, ok := parseSegmindPricing([]byte("<html>nothing here</html>"), segAvg(1)); ok {
		t.Error("an empty page parsed")
	}
}

func TestSegmindRoutes(t *testing.T) {
	body, err := os.ReadFile("testdata/segmind/list.json")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := segmindRoutes(body, "2026-10-19")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range routes {
		if r.ModelID == "future-model" {
			t.Error("coming-soon model became a route")
		}
		if r.Transport != "segmind-v2" {
			t.Errorf("%s: transport %q", r.ID, r.Transport)
		}
	}
	sd := routeByID(t, routes, "segmind:seedance-2.5")
	if sd.Family != "seedance-2.5" || sd.Price.Source != registry.SourceAverage || sd.Price.Note != "observed average per request" {
		t.Fatalf("seedance: %+v", sd.Price)
	}
	if sd.Caps.MaxVideos != 10 || sd.Caps.MaxImages != 30 || sd.Caps.MaxAudios != 10 || sd.Caps.MaxDuration != 30 || sd.Caps.MinDuration != 4 {
		t.Errorf("seedance caps: %+v", sd.Caps)
	}
	for _, op := range []string{registry.OpVideoT2V, registry.OpVideoFLF2V, registry.OpVideoR2V} {
		if op != sd.Op {
			routeByID(t, routes, "segmind:seedance-2.5#"+op)
		}
	}
	om := routeByID(t, routes, "segmind:gemini-omni-1.1")
	if om.Family != "gemini-omni-flash-1.1" || om.Caps.MaxVideos != 0 || len(om.Caps.Resolutions) != 4 || !om.Caps.FirstFrame {
		t.Errorf("omni-1.1: family %s caps %+v", om.Family, om.Caps)
	}
}

func TestSegmindFetchOffline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/list" {
			http.ServeFile(w, r, "testdata/segmind/list.json")
			return
		}
		slug := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/models/"), "/pricing")
		if _, err := os.Stat("testdata/segmind/" + slug + ".html"); err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "testdata/segmind/"+slug+".html")
	}))
	defer srv.Close()
	s := Segmind{ListURL: srv.URL + "/list", PageURL: func(slug string) string { return srv.URL + "/models/" + slug + "/pricing" }}
	routes, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t2v := routeByID(t, routes, "segmind:seedance-2.5#video.t2v")
	if t2v.Price.Source != registry.SourcePage || !strings.HasSuffix(t2v.Price.SourceURL, "/models/seedance-2.5/pricing") {
		t.Errorf("seedance sibling not re-priced: %+v", t2v.Price)
	}
	tts := routeByID(t, routes, "segmind:elevenlabs-v4-tts")
	if tts.Price.Unit != registry.UnitKChars {
		t.Errorf("tts price: %+v", tts.Price)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
