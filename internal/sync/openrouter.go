package sync

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	gosync "sync"

	"github.com/radjathaher/creative-cli/internal/registry"
)

// OpenRouter syncs the three public OpenRouter catalogs: the video API, the
// image API (prices live on each model's endpoints listing), and the chat
// models that generate images or audio or that read images, video, or audio.
// Plain text LLMs are skipped. None of these endpoints needs a key.
type OpenRouter struct {
	Base    string // https://openrouter.ai/api/v1
	Site    string // model pages, for SourceURL
	Workers int

	Warnings []string
}

// NewOpenRouter returns an OpenRouter source pointed at production.
func NewOpenRouter() *OpenRouter {
	return &OpenRouter{Base: "https://openrouter.ai/api/v1", Site: "https://openrouter.ai", Workers: 4}
}

func (o *OpenRouter) Name() string { return "openrouter" }

// Fetch combines the three catalogs. The video and chat listings are required;
// a failed image endpoints lookup only leaves that model's price unknown.
func (o *OpenRouter) Fetch(ctx context.Context) ([]registry.Route, error) {
	videos, err := o.videoRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("openrouter videos: %w", err)
	}
	images, imageIDs, err := o.imageRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("openrouter images: %w", err)
	}
	chat, err := o.chatRoutes(ctx, imageIDs)
	if err != nil {
		return nil, fmt.Errorf("openrouter models: %w", err)
	}
	routes := append(append(videos, images...), chat...)
	uniqueIDs(routes)
	return routes, nil
}

// uniqueIDs gives every route of a model that serves several ops an id of the
// form "openrouter:<model>#<op>", keeping ids unique while ModelID remains the
// id the API expects. Single-op models keep the plain "<provider>:<model>" id.
func uniqueIDs(routes []registry.Route) {
	n := map[string]int{}
	for _, r := range routes {
		n[r.ModelID]++
	}
	for i := range routes {
		if n[routes[i].ModelID] > 1 {
			routes[i].ID = routes[i].Provider + ":" + routes[i].ModelID + "#" + routes[i].Op
		}
	}
}

// newORRoutes builds one route per op for a model; Fetch makes the ids unique.
func (o *OpenRouter) newORRoutes(id, title, transport string, ops []string, price registry.Price, caps registry.Capabilities) []registry.Route {
	_, family, variant := Classify("openrouter", "", id, title)
	out := make([]registry.Route, 0, len(ops))
	for _, op := range ops {
		r := NewRoute("openrouter", id, title)
		r.Op, r.Family, r.Variant, r.Transport = op, family, variant, transport
		r.Price, r.Caps = price, caps
		r.SourceURL = o.Site + "/" + id
		out = append(out, r)
	}
	return out
}

type orVideoModel struct {
	ID                    string            `json:"id"`
	Name                  string            `json:"name"`
	PricingSKUs           map[string]string `json:"pricing_skus"`
	SupportedResolutions  []string          `json:"supported_resolutions"`
	SupportedDurations    []int             `json:"supported_durations"`
	SupportedAspectRatios []string          `json:"supported_aspect_ratios"`
	SupportedFrameImages  []string          `json:"supported_frame_images"`
	GenerateAudio         bool              `json:"generate_audio"`
}

func (o *OpenRouter) videoRoutes(ctx context.Context) ([]registry.Route, error) {
	var resp struct {
		Data []orVideoModel `json:"data"`
	}
	src := o.Base + "/videos/models"
	if err := GetJSON(ctx, src, nil, &resp); err != nil {
		return nil, err
	}
	var out []registry.Route
	for _, m := range resp.Data {
		_, family, _ := Classify("openrouter", "video", m.ID, m.Name)
		price := videoSKUPrice(m.PricingSKUs, family, src)
		caps := registry.Capabilities{
			Resolutions: lowerAll(m.SupportedResolutions),
			Aspects:     m.SupportedAspectRatios,
			NativeAudio: m.GenerateAudio,
		}
		if n := len(m.SupportedDurations); n > 0 {
			d := append([]int(nil), m.SupportedDurations...)
			sort.Ints(d)
			caps.MinDuration, caps.MaxDuration = d[0], d[n-1]
		}
		for _, f := range m.SupportedFrameImages {
			caps.FirstFrame = caps.FirstFrame || f == "first_frame"
			caps.LastFrame = caps.LastFrame || f == "last_frame"
		}
		out = append(out, o.newORRoutes(m.ID, m.Name, "openrouter-video", videoOps(m, caps), price, caps)...)
	}
	return out, nil
}

// videoOps lists what one video model can do: generation from text, from a
// first frame, from first and last frames, and from references when the
// model prices reference or video inputs.
func videoOps(m orVideoModel, caps registry.Capabilities) []string {
	k := strings.ToLower(m.ID)
	switch {
	case strings.Contains(k, "upscal"):
		return []string{registry.OpVideoUpscale}
	case strings.Contains(k, "edit") || strings.Contains(k, "aleph"):
		return []string{registry.OpVideoEdit}
	case strings.Contains(k, "avatar"):
		return []string{registry.OpVideoAvatar}
	}
	ops := []string{registry.OpVideoT2V}
	if caps.FirstFrame {
		ops = append(ops, registry.OpVideoI2V)
	}
	if caps.FirstFrame && caps.LastFrame {
		ops = append(ops, registry.OpVideoFLF2V)
	}
	for sku := range m.PricingSKUs {
		if strings.HasPrefix(sku, "reference") || strings.Contains(sku, "video_input") {
			return append(ops, registry.OpVideoR2V)
		}
	}
	return ops
}

var (
	reTokenSKU  = regexp.MustCompile(`^video_tokens(?:_(\d+p|\dk))?(_with_video_input)?$`)
	reSecondSKU = regexp.MustCompile(`^((?:text_to_video_|image_to_video_)?duration_seconds(?:_with_audio)?|cents_per_video_output_second|cents_per_second_output)(?:_(\d+p|\dk))?$`)
)

// secondSKUOrder ranks per-second SKU families: generic output rates first,
// then the with-audio rate (models default to generating audio), then the
// text- and image-specific rates for models that publish only those.
var secondSKUOrder = []string{
	"cents_per_video_output_second", "cents_per_second_output", "duration_seconds",
	"duration_seconds_with_audio", "text_to_video_duration_seconds", "image_to_video_duration_seconds",
}

// videoSKUPrice turns OpenRouter's pricing_skus into one price. Seedance-style
// models bill video tokens, with cheaper rates when the request carries input
// video (tier video_input=true) and separate 1080p/4K rates; the rest bill per
// output second, in dollars or in cents, optionally per resolution. Keys the
// job model cannot select on (without-audio, per-reference surcharges, the
// less preferred per-second family) are listed in the note, not dropped.
func videoSKUPrice(skus map[string]string, family, src string) registry.Price {
	p := registry.Price{Unit: registry.UnitUnknown, Source: registry.SourceUnknown, SourceURL: src, AsOf: Today()}
	keys := make([]string, 0, len(skus))
	for k := range skus {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	seconds := map[string]map[string]float64{} // sku family -> resolution ("" = any) -> USD/s
	var other []string
	for _, k := range keys {
		v, ok := parsePrice(skus[k])
		if !ok {
			continue
		}
		if m := reTokenSKU.FindStringSubmatch(k); m != nil {
			p.Unit, p.Source = registry.UnitMTokens, registry.SourceAPI
			if strings.HasPrefix(family, "seedance") {
				p.Formula = registry.FormulaSeedTok
			}
			match := map[string]string{}
			if m[1] != "" {
				match["resolution"] = strings.ToLower(m[1])
			}
			if m[2] != "" {
				match["video_input"] = "true"
			}
			if len(match) == 0 {
				p.ValueUSD = registry.F(roundUSD(v * 1e6))
			} else {
				p.Tiers = append(p.Tiers, registry.PriceTier{Match: match, ValueUSD: roundUSD(v * 1e6)})
			}
			continue
		}
		if m := reSecondSKU.FindStringSubmatch(k); m != nil {
			if strings.HasPrefix(m[1], "cents") {
				v /= 100
			}
			if seconds[m[1]] == nil {
				seconds[m[1]] = map[string]float64{}
			}
			seconds[m[1]][strings.ToLower(m[2])] = roundUSD(v)
			continue
		}
		other = append(other, k+"="+skus[k])
	}
	if p.Unit != registry.UnitMTokens {
		for _, base := range secondSKUOrder {
			rates, ok := seconds[base]
			if !ok {
				continue
			}
			p.Unit, p.Source = registry.UnitSecond, registry.SourceAPI
			for res, v := range rates {
				if res == "" {
					p.ValueUSD = registry.F(v)
				} else {
					p.Tiers = append(p.Tiers, registry.PriceTier{Match: map[string]string{"resolution": res}, ValueUSD: v})
				}
			}
			delete(seconds, base)
			break
		}
	}
	for base, rates := range seconds {
		for res, v := range rates {
			other = append(other, strings.TrimSuffix(base+"_"+res, "_")+"="+strconv.FormatFloat(v, 'g', -1, 64)+"/s")
		}
	}
	if p.ValueUSD == nil && len(p.Tiers) > 0 {
		// No resolution-free rate: the cheapest tier is the base, which is the
		// right answer when a job omits --resolution and the model defaults low.
		best := p.Tiers[0].ValueUSD
		for _, t := range p.Tiers {
			best = min(best, t.ValueUSD)
		}
		p.ValueUSD = registry.F(best)
	}
	if len(other) > 0 {
		sort.Strings(other)
		p.Note = "other skus: " + strings.Join(other, ", ")
	}
	sortTiers(p.Tiers)
	return p
}

func lowerAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToLower(s)
	}
	return out
}

// parsePrice reads OpenRouter's decimal-string prices; negative values mark
// dynamically routed models and count as unknown.
func parsePrice(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil && v >= 0
}

// forEach runs fn over n items with bounded concurrency.
func forEach(n, workers int, fn func(i int)) {
	var wg gosync.WaitGroup
	jobs := make(chan int)
	for w := 0; w < max(workers, 1); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

func escapeModelPath(id string) string {
	parts := strings.Split(id, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func (o *OpenRouter) warn(format string, args ...any) {
	o.Warnings = append(o.Warnings, fmt.Sprintf(format, args...))
}
