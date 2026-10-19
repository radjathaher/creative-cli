package sync

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/radjathaher/creative-cli/internal/registry"
)

func loadKIERows(t *testing.T) []kieRow {
	t.Helper()
	var resp struct {
		Data struct {
			Records []kieRow `json:"records"`
		} `json:"data"`
	}
	raw, err := os.ReadFile("testdata/kie/pricing.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Data.Records
}

func routeByID(t *testing.T, routes []registry.Route, id string) registry.Route {
	t.Helper()
	for _, r := range routes {
		if r.ID == id {
			return r
		}
	}
	var ids []string
	for _, r := range routes {
		ids = append(ids, r.ID)
	}
	t.Fatalf("route %s not found; have %s", id, strings.Join(ids, ", "))
	return registry.Route{}
}

func wantUSD(t *testing.T, label string, p registry.Price, j registry.Job, want float64) {
	t.Helper()
	e := registry.EstimateJob(p, j)
	if e.USD == nil {
		t.Fatalf("%s: no estimate (%s)", label, e.Basis)
	}
	if math.Abs(*e.USD-want) > 1e-6 {
		t.Fatalf("%s: got $%.6f (%s), want $%.6f", label, *e.USD, e.Basis, want)
	}
}

func TestKIECreditRate(t *testing.T) {
	for _, r := range loadKIERows(t) {
		usd, _ := strconv.ParseFloat(r.USD, 64)
		credits, _ := strconv.ParseFloat(r.Credits, 64)
		if credits > 0 && math.Abs(usd/credits-kieCreditUSD) > 0.0001 {
			t.Errorf("%s: $%s for %s credits is not $0.005/credit", r.Description, r.USD, r.Credits)
		}
	}
}

func TestKIERoutesGroupSKUsIntoTiers(t *testing.T) {
	routes := kieRoutes(loadKIERows(t), "2026-10-19")
	for _, r := range routes {
		if strings.Contains(r.ModelID, "claude") {
			t.Errorf("chat SKU %s became a route", r.ID)
		}
		if r.Transport != "kie-jobs" || r.Price.Source != registry.SourceAPI {
			t.Errorf("%s: transport %q source %q", r.ID, r.Transport, r.Price.Source)
		}
	}

	sd := routeByID(t, routes, "kie:bytedance/seedance-2-5")
	if sd.Op != registry.OpVideoT2V || sd.Family != "seedance-2.5" || sd.Price.Unit != registry.UnitSecond {
		t.Fatalf("seedance route: op %s family %s unit %s", sd.Op, sd.Family, sd.Price.Unit)
	}
	wantUSD(t, "seedance 480p", sd.Price, registry.Job{Resolution: "480p", DurationSecs: 5}, 0.70)
	wantUSD(t, "seedance 720p", sd.Price, registry.Job{Resolution: "720p", DurationSecs: 5}, 0.315*5)
	wantUSD(t, "seedance 1080p", sd.Price, registry.Job{Resolution: "1080p", DurationSecs: 5}, 0.79*5)
	wantUSD(t, "seedance 720p video", sd.Price, registry.Job{Resolution: "720p", DurationSecs: 5, HasVideoInput: true}, 0.19*5)

	om := routeByID(t, routes, "kie:google/gemini-omni-flash-1-1")
	if om.Family != "gemini-omni-flash-1.1" || om.Price.Unit != registry.UnitRequest {
		t.Fatalf("omni route: family %s unit %s", om.Family, om.Price.Unit)
	}
	wantUSD(t, "omni 6s 720p", om.Price, registry.Job{Resolution: "720p", DurationSecs: 6}, 0.42)
	wantUSD(t, "omni 10s 1080p", om.Price, registry.Job{Resolution: "1080p", DurationSecs: 10}, 0.63)
	wantUSD(t, "omni 720p video", om.Price, registry.Job{Resolution: "720p", DurationSecs: 6, HasVideoInput: true}, 0.84)
	wantUSD(t, "omni 4k video", om.Price, registry.Job{Resolution: "4k", DurationSecs: 8, HasVideoInput: true}, 1.26)

	for _, id := range []string{"kie:gpt-image-2-text-to-image", "kie:gpt-image-2-5-flare-text-to-image", "kie:gpt-image-2-5-sunburst-image-to-image"} {
		r := routeByID(t, routes, id)
		wantUSD(t, id+" 1k", r.Price, registry.Job{Resolution: "1K"}, 0.03)
		wantUSD(t, id+" 2k", r.Price, registry.Job{Resolution: "2k"}, 0.05)
		wantUSD(t, id+" 4k", r.Price, registry.Job{Resolution: "4K"}, 0.08)
	}
	flare := routeByID(t, routes, "kie:gpt-image-2-5-flare-text-to-image")
	if flare.Op != registry.OpImageT2I || flare.Family != "gpt-image-2.5" || flare.Variant != "flare" {
		t.Fatalf("flare: op %s family %s variant %s", flare.Op, flare.Family, flare.Variant)
	}
	edit := routeByID(t, routes, "kie:gpt-image-2-5-sunburst-image-to-image")
	if edit.Op != registry.OpImageEdit || edit.Variant != "sunburst" {
		t.Fatalf("sunburst i2i: op %s variant %s", edit.Op, edit.Variant)
	}
}

func TestKIEDocSchema(t *testing.T) {
	cases := []struct {
		file, model       string
		images, videos    int
		minDur, maxDur    int
		first, last       bool
		videoField, image string
	}{
		{"gemini-omni-flash-1-1.md", "google/gemini-omni-flash-1-1", 7, 1, 4, 10, true, true, "video_list", "image_urls"},
		{"seedance-2-5.md", "bytedance/seedance-2-5", 30, 10, 4, 15, true, true, "reference_video_urls", "reference_image_urls"},
	}
	for _, c := range cases {
		page, err := os.ReadFile("testdata/kie/" + c.file)
		if err != nil {
			t.Fatal(err)
		}
		model, schema, err := kieDocSchema(page)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if model != c.model {
			t.Errorf("%s: model %q, want %q", c.file, model, c.model)
		}
		if strings.Contains(string(schema), "x-apidog") {
			t.Errorf("%s: schema keeps doc-tool annotations", c.file)
		}
		var r registry.Route
		applySchema(&r, schema)
		caps := r.Caps
		if caps.MaxImages != c.images || caps.MaxVideos != c.videos || caps.FirstFrame != c.first || caps.LastFrame != c.last {
			t.Errorf("%s: caps %+v", c.file, caps)
		}
		if caps.MinDuration != c.minDur || caps.MaxDuration < c.maxDur {
			t.Errorf("%s: duration %d-%d, want %d-%d+", c.file, caps.MinDuration, caps.MaxDuration, c.minDur, c.maxDur)
		}
		if r.ParamMap["video"] != c.videoField || r.ParamMap["image"] != c.image {
			t.Errorf("%s: param map %v", c.file, r.ParamMap)
		}
	}
}

func TestKIEFetchOffline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/pricing":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"pageNum":1`) {
				t.Errorf("unexpected pricing body %s", body)
			}
			http.ServeFile(w, r, "testdata/kie/pricing.json")
		case r.URL.Path == "/llms.txt":
			http.ServeFile(w, r, "testdata/kie/llms.txt")
		case strings.HasPrefix(r.URL.Path, "/market/"):
			name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			http.ServeFile(w, r, "testdata/kie/"+name)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	routes, err := KIE{PricingURL: srv.URL + "/pricing", LLMsURL: srv.URL + "/llms.txt", DocBase: srv.URL}.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	om := routeByID(t, routes, "kie:google/gemini-omni-flash-1-1")
	if om.Schema == nil || om.Caps.MaxVideos != 1 {
		t.Fatalf("omni has no doc schema: caps %+v", om.Caps)
	}
	r2v := routeByID(t, routes, "kie:google/gemini-omni-flash-1-1#video.r2v")
	wantUSD(t, "omni r2v", r2v.Price, registry.Job{Resolution: "720p", DurationSecs: 6, HasVideoInput: true}, 0.84)
	routeByID(t, routes, "kie:bytedance/seedance-2-5#video.i2v")
	routeByID(t, routes, "kie:bytedance/seedance-2-5#video.r2v")
	if f := routeByID(t, routes, "kie:gpt-image-2-5-flare-text-to-image"); f.Schema == nil {
		t.Error("flare t2i has no doc schema")
	}
}
