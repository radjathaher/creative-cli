package registry

// Stock operations are built into the CLI rather than fetched from generation
// catalogs. Add them after loading either snapshot so an older cache cannot hide
// them or mark the blocked Pond5 integration as verified.
func (l *Loaded) addBrollRoutes() {
	kept := l.Routes[:0]
	for _, r := range l.Routes {
		if r.Op != OpBrollSearch && r.Op != OpBrollGet {
			kept = append(kept, r)
		}
	}
	l.Routes = kept
	for _, p := range []struct{ name, docs string }{
		{"pexels", "https://www.pexels.com/api/documentation/"},
		{"pixabay", "https://pixabay.com/api/docs/"},
		{"pond5-pd", "https://www.pond5.com/help/en/articles/10086064-where-can-i-find-free-footage-photos-and-music"},
	} {
		for _, op := range []string{OpBrollSearch, OpBrollGet} {
			transport, note := "broll-http", "Free stock footage; licence and other rights still apply."
			if p.name == "pond5-pd" {
				transport = "pond5-unverified"
				note = "Unavailable: API blocked; per-item public-domain licence and download API unverified. Pond5 help limits Public Domain Project videos to editorial use."
			}
			l.Routes = append(l.Routes, Route{
				ID: p.name + ":" + op, Provider: p.name, ModelID: op, Op: op,
				Family: "stock-footage", Title: p.name + " stock footage", Transport: transport,
				SourceURL: p.docs,
				Price:     Price{Unit: UnitRequest, ValueUSD: F(0), Source: SourceManual, SourceURL: p.docs, AsOf: "2026-10-25", Note: note},
			})
		}
	}
}
