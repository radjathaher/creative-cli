package registry

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// The snapshot is gzipped because provider schemas make the catalog several
// megabytes; `creative sync --out` writes it and CI commits it.
//
//go:embed data/routes.json.gz
var snapshotGz []byte

//go:embed data/allowlist.json
var allowlistJSON []byte

// pinsJSON holds the owner's per-family provider policy, for example
// gpt-image-2.5 only through OpenAI.
//
//go:embed data/pins.json
var pinsJSON []byte

// StaleAfter is the age at which quote marks prices stale so an agent can offer a sync.
const StaleAfter = 7 * 24 * time.Hour

// Loaded is the catalog in effect plus where it came from.
type Loaded struct {
	Catalog
	Origin    string // "snapshot" or the overlay path
	Allowlist Allowlist
	Pins      map[string][]string
}

// Allowlist restricts which families an op shows by default. Modalities absent
// from the map show every family.
type Allowlist map[string][]string

// Allowed reports whether a route is visible without --all.
func (a Allowlist) Allowed(r Route) bool {
	fams, ok := a[Modality(r.Op)]
	if !ok {
		return true
	}
	for _, f := range fams {
		if f == r.Family {
			return true
		}
	}
	return false
}

// CachePath is where `creative sync` writes the local overlay.
func CachePath() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "creative", "routes.json.gz")
}

// Load returns the newer of the embedded snapshot and the local overlay.
// Routes from the snapshot are verified; overlay routes keep verified=true only
// when the snapshot holds the same route id, because only CI-synced routes have
// been through review.
func Load() (*Loaded, error) {
	snap, err := decode(snapshotGz)
	if err != nil {
		return nil, fmt.Errorf("embedded snapshot: %w", err)
	}
	for i := range snap.Routes {
		snap.Routes[i].Verified = true
	}
	var allow Allowlist
	if err := json.Unmarshal(allowlistJSON, &allow); err != nil {
		return nil, fmt.Errorf("allowlist: %w", err)
	}
	pins := map[string][]string{}
	var rawPins map[string]json.RawMessage
	if err := json.Unmarshal(pinsJSON, &rawPins); err != nil {
		return nil, fmt.Errorf("pins: %w", err)
	}
	for fam, v := range rawPins {
		var provs []string
		if json.Unmarshal(v, &provs) == nil {
			pins[fam] = provs
		}
	}
	out := &Loaded{Catalog: *snap, Origin: "snapshot", Allowlist: allow, Pins: pins}

	raw, err := os.ReadFile(CachePath())
	if err != nil {
		return out, nil
	}
	over, err := decode(raw)
	if err != nil || over.GeneratedAt <= snap.GeneratedAt {
		return out, nil
	}
	known := make(map[string]bool, len(snap.Routes))
	for _, r := range snap.Routes {
		known[r.ID] = true
	}
	for i := range over.Routes {
		over.Routes[i].Verified = known[over.Routes[i].ID]
	}
	out.Catalog, out.Origin = *over, CachePath()
	return out, nil
}

func decode(gz []byte) (*Catalog, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	body, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	var c Catalog
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Write serializes a catalog as gzipped JSON with routes in a stable order, so
// CI diffs of the snapshot stay meaningful when decompressed.
func Write(path string, c *Catalog) error {
	sort.Slice(c.Routes, func(i, j int) bool { return c.Routes[i].ID < c.Routes[j].ID })
	body, err := json.MarshalIndent(c, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.ModTime = time.Time{}
	if _, err := zw.Write(body); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Find returns the route with the given id.
func (l *Loaded) Find(id string) (Route, bool) {
	for _, r := range l.Routes {
		if r.ID == id {
			return r, true
		}
	}
	return Route{}, false
}

// Select returns routes for an op, optionally narrowed by family, variant, and
// provider. Without all, the allowlist hides families outside it.
func (l *Loaded) Select(op, family, variant, provider string, all bool) []Route {
	var out []Route
	for _, r := range l.Routes {
		if op != "" && r.Op != op {
			continue
		}
		if family != "" && r.Family != family {
			continue
		}
		if variant != "" && r.Variant != variant {
			continue
		}
		if provider != "" && r.Provider != provider {
			continue
		}
		if !all && (r.Deprecated || !l.Allowlist.Allowed(r) || !l.pinned(r)) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// pinned reports whether the owner's provider policy allows this route.
func (l *Loaded) pinned(r Route) bool {
	provs, ok := l.Pins[r.Family]
	if !ok {
		return true
	}
	for _, p := range provs {
		if p == r.Provider {
			return true
		}
	}
	return false
}
