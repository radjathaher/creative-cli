package registry_test

import (
	"strings"
	"testing"
	"time"

	"github.com/radjathaher/creative-cli/internal/registry"
)

func TestBrollRoutesPresentWithoutOverlay(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	loaded, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Origin != "snapshot" {
		t.Fatalf("origin = %q, want embedded snapshot", loaded.Origin)
	}
	assertBrollRoutes(t, loaded)
	for _, op := range []string{registry.OpBrollSearch, registry.OpBrollGet} {
		count := 0
		for _, candidate := range registry.Ops {
			if candidate == op {
				count++
			}
		}
		if count != 1 {
			t.Errorf("operation %q registered %d times, want once", op, count)
		}
	}
}

func TestBrollRoutesRestoredInNewerOverlay(t *testing.T) {
	for _, includeOther := range []bool{false, true} {
		name := "empty"
		if includeOther {
			name = "unrelated-route"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			overlay := newerBrollOverlay(t)
			if includeOther {
				overlay.Routes = []registry.Route{{ID: "fixture:other", Provider: "fixture", Op: registry.OpImageT2I, Family: "fixture"}}
			}
			if err := registry.Write(registry.CachePath(), overlay); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				loaded, err := registry.Load()
				if err != nil {
					t.Fatal(err)
				}
				if loaded.Origin != registry.CachePath() {
					t.Fatalf("newer overlay was not loaded: %q", loaded.Origin)
				}
				assertBrollRoutes(t, loaded)
				if includeOther {
					if _, ok := loaded.Find("fixture:other"); !ok {
						t.Error("unrelated overlay route was removed")
					}
				}
			}
		})
	}
}

func TestBrollOverlayCannotEnablePond5OrDuplicateBuiltins(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	overlay := newerBrollOverlay(t)
	for _, provider := range []string{"pexels", "pixabay", "pond5-pd"} {
		for _, op := range []string{registry.OpBrollSearch, registry.OpBrollGet} {
			malicious := registry.Route{
				ID: provider + ":" + op, Provider: provider, Op: op,
				Family: "stock-footage", Transport: "broll-http", Verified: true,
				Price: registry.Price{ValueUSD: registry.F(99), Unit: registry.UnitSecond},
			}
			overlay.Routes = append(overlay.Routes, malicious, malicious)
		}
	}
	overlay.Routes = append(overlay.Routes, registry.Route{
		ID: "unreviewed:broll.search", Provider: "unreviewed", Op: registry.OpBrollSearch,
		Family: "stock-footage", Verified: true,
	})
	if err := registry.Write(registry.CachePath(), overlay); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		loaded, err := registry.Load()
		if err != nil {
			t.Fatal(err)
		}
		assertBrollRoutes(t, loaded)
		if len(loaded.Routes) != 6 {
			t.Errorf("built-in routes not replaced cleanly: %d routes", len(loaded.Routes))
		}
	}
}

func newerBrollOverlay(t *testing.T) *registry.Catalog {
	t.Helper()
	loaded, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	stamp, err := time.Parse(time.RFC3339, loaded.GeneratedAt)
	if err != nil {
		t.Fatalf("invalid snapshot timestamp: %v", err)
	}
	return &registry.Catalog{GeneratedAt: stamp.Add(time.Hour).Format(time.RFC3339), Routes: []registry.Route{}}
}

func assertBrollRoutes(t *testing.T, loaded *registry.Loaded) {
	t.Helper()
	routes := loaded.Select("", "stock-footage", "", "", true)
	if len(routes) != 6 {
		t.Fatalf("stock-footage routes = %d, want six", len(routes))
	}
	counts := make(map[string]int)
	for _, route := range loaded.Routes {
		counts[route.ID]++
	}
	for _, provider := range []string{"pexels", "pixabay", "pond5-pd"} {
		for _, op := range []string{registry.OpBrollSearch, registry.OpBrollGet} {
			id := provider + ":" + op
			if counts[id] != 1 {
				t.Errorf("route %q registered %d times, want once", id, counts[id])
			}
			route, ok := loaded.Find(id)
			if !ok {
				t.Errorf("missing route %q", id)
				continue
			}
			if route.Provider != provider || route.Op != op || route.ModelID != op || route.Family != "stock-footage" {
				t.Errorf("incorrect route identity: %+v", route)
			}
			if route.Price.ValueUSD == nil || *route.Price.ValueUSD != 0 || route.Price.Unit != registry.UnitRequest || route.Price.Source != registry.SourceManual {
				t.Errorf("route %q must have a known zero request price: %+v", id, route.Price)
			}
			if route.SourceURL == "" || route.Price.SourceURL != route.SourceURL {
				t.Errorf("route %q must retain its primary source", id)
			}
			if provider == "pond5-pd" {
				if route.Transport != "pond5-unverified" || route.Verified {
					t.Errorf("unsafe Pond5 registration: %+v", route)
				}
				if !strings.Contains(strings.ToLower(route.Price.Note), "unavailable") {
					t.Error("Pond5 route must explain its unavailable state")
				}
			} else if route.Transport != "broll-http" {
				t.Errorf("route %q has wrong transport %q", id, route.Transport)
			}
		}
	}
	for _, op := range []string{registry.OpBrollSearch, registry.OpBrollGet} {
		if got := loaded.Select(op, "stock-footage", "", "", false); len(got) != 3 {
			t.Errorf("default discovery must show three providers for %q, got %d", op, len(got))
		}
	}
}
