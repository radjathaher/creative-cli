package sync

import (
	"context"
	"fmt"
	"strings"

	"github.com/radjathaher/creative-cli/internal/registry"
)

type orParam struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
	Min    *float64 `json:"min"`
	Max    *float64 `json:"max"`
}

type orImageModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Architecture struct {
		InputModalities []string `json:"input_modalities"`
	} `json:"architecture"`
	SupportedParameters map[string]orParam `json:"supported_parameters"`
	Endpoints           string             `json:"endpoints"`
}

type orImageEndpoint struct {
	ProviderName string `json:"provider_name"`
	Pricing      []struct {
		Billable string  `json:"billable"`
		Unit     string  `json:"unit"`
		Variant  string  `json:"variant"`
		CostUSD  float64 `json:"cost_usd"`
	} `json:"pricing"`
}

// imageRoutes lists the image API models and prices each one from its
// endpoints listing. It returns the model ids so the chat catalog can skip
// the same models.
func (o *OpenRouter) imageRoutes(ctx context.Context) ([]registry.Route, map[string]bool, error) {
	var resp struct {
		Data []orImageModel `json:"data"`
	}
	if err := GetJSON(ctx, o.Base+"/images/models", nil, &resp); err != nil {
		return nil, nil, err
	}
	prices := make([]registry.Price, len(resp.Data))
	errs := make([]error, len(resp.Data))
	forEach(len(resp.Data), o.Workers, func(i int) {
		u := o.Base + "/images/models/" + escapeModelPath(resp.Data[i].ID) + "/endpoints"
		var ep struct {
			Endpoints []orImageEndpoint `json:"endpoints"`
		}
		if errs[i] = GetJSON(ctx, u, nil, &ep); errs[i] == nil {
			prices[i] = imageEndpointPrice(ep.Endpoints, u)
		} else {
			prices[i] = registry.Price{Unit: registry.UnitUnknown, Source: registry.SourceUnknown, SourceURL: u}
		}
	})
	ids := map[string]bool{}
	var out []registry.Route
	for i, m := range resp.Data {
		if errs[i] != nil {
			o.warn("openrouter image endpoints %s: %s", m.ID, shortErr(errs[i]))
		}
		ids[m.ID] = true
		caps := registry.Capabilities{
			Resolutions: lowerAll(m.SupportedParameters["resolution"].Values),
			Aspects:     m.SupportedParameters["aspect_ratio"].Values,
		}
		if ref := m.SupportedParameters["input_references"]; ref.Max != nil {
			caps.MaxImages = int(*ref.Max)
		}
		out = append(out, o.newORRoutes(m.ID, m.Name, "openrouter-image", imageOps(m, caps), prices[i], caps)...)
	}
	return out, ids, nil
}

func imageOps(m orImageModel, caps registry.Capabilities) []string {
	k := strings.ToLower(m.ID)
	switch {
	case strings.Contains(k, "upscal"):
		return []string{registry.OpImageUpscale}
	case strings.Contains(k, "background") || strings.Contains(k, "bg-remov"):
		return []string{registry.OpImageBGRemove}
	}
	ops := []string{registry.OpImageT2I}
	if caps.MaxImages > 0 || contains(m.Architecture.InputModalities, "image") {
		ops = append(ops, registry.OpImageEdit)
	}
	return ops
}

// imageEndpointPrice picks the cheapest provider endpoint by its base output
// price and turns its output_image rows into a base price plus resolution
// tiers ("1k", "2k", "4k" match the job's lowercased --resolution).
func imageEndpointPrice(eps []orImageEndpoint, src string) registry.Price {
	best := registry.Price{Unit: registry.UnitUnknown, Source: registry.SourceUnknown, SourceURL: src, AsOf: Today()}
	var alts []string
	for _, ep := range eps {
		p := registry.Price{Source: registry.SourceAPI, SourceURL: src, AsOf: Today()}
		for _, row := range ep.Pricing {
			if row.Billable != "output_image" {
				continue
			}
			unit, v := imageUnit(row.Unit, row.CostUSD)
			p.Unit = unit
			variant := strings.ToLower(row.Variant)
			if variant == "" || ((variant == "1k" || variant == "1024") && p.ValueUSD == nil) {
				p.ValueUSD = registry.F(v)
			}
			if variant != "" {
				p.Tiers = append(p.Tiers, registry.PriceTier{Match: map[string]string{"resolution": variant}, ValueUSD: v})
			}
		}
		if p.ValueUSD == nil && len(p.Tiers) > 0 {
			p.ValueUSD = registry.F(p.Tiers[0].ValueUSD)
		}
		if p.ValueUSD == nil {
			continue
		}
		alts = append(alts, fmt.Sprintf("%s $%g/%s", ep.ProviderName, *p.ValueUSD, p.Unit))
		if best.ValueUSD == nil || *p.ValueUSD < *best.ValueUSD {
			best = p
		}
	}
	if best.Unit == registry.UnitMTokens {
		best.Note = "per 1M output image tokens; token count depends on size and quality"
	}
	if len(alts) > 1 {
		best.Note = joinNote(best.Note, "endpoints: "+strings.Join(alts, ", "))
	}
	sortTiers(best.Tiers)
	return best
}

func imageUnit(unit string, v float64) (string, float64) {
	switch unit {
	case "image":
		return registry.UnitImage, v
	case "megapixel":
		return registry.UnitMegapixel, v
	case "token":
		return registry.UnitMTokens, roundUSD(v * 1e6)
	case "request":
		return registry.UnitRequest, v
	}
	return registry.UnitUnknown, v
}

type orChatModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Architecture struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	Pricing        map[string]any `json:"pricing"`
	ExpirationDate *string        `json:"expiration_date"`
}

// chatRoutes keeps chat models that generate images or audio, or that read
// images, video, or audio. Each capability becomes its own route so quotes
// for understand.video list only models that accept video.
func (o *OpenRouter) chatRoutes(ctx context.Context, skip map[string]bool) ([]registry.Route, error) {
	var resp struct {
		Data []orChatModel `json:"data"`
	}
	src := o.Base + "/models"
	if err := GetJSON(ctx, src, nil, &resp); err != nil {
		return nil, err
	}
	var out []registry.Route
	for _, m := range resp.Data {
		if strings.HasPrefix(m.ID, "openrouter/") {
			continue // meta-routers that pick a model at request time
		}
		in, outM := m.Architecture.InputModalities, m.Architecture.OutputModalities
		type opPrice struct {
			op, key, what string
		}
		var ops []opPrice
		switch {
		case contains(outM, "image") && !skip[m.ID]:
			ops = append(ops, opPrice{registry.OpImageT2I, "image_output", "output image tokens"})
			if contains(in, "image") {
				ops = append(ops, opPrice{registry.OpImageEdit, "image_output", "output image tokens"})
			}
		case contains(outM, "audio") && has(strings.ToLower(m.ID), `lyria|music|song`):
			ops = append(ops, opPrice{registry.OpMusicGenerate, "audio_output", "output audio tokens"})
		case contains(outM, "audio"):
			ops = append(ops, opPrice{registry.OpVoiceTTS, "audio_output", "output audio tokens"})
		}
		if contains(in, "video") {
			ops = append(ops, opPrice{registry.OpUnderstandVid, "prompt", "input tokens"})
		}
		if contains(in, "audio") {
			ops = append(ops, opPrice{registry.OpUnderstandAud, "audio", "input audio tokens"})
		}
		if contains(in, "image") {
			ops = append(ops, opPrice{registry.OpUnderstandImg, "image", "input image tokens"})
		}
		if len(ops) == 0 {
			continue
		}
		names := make([]string, len(ops))
		for i, x := range ops {
			names[i] = x.op
		}
		routes := o.newORRoutes(m.ID, m.Name, "openrouter-chat", names, registry.Price{}, registry.Capabilities{})
		for i := range routes {
			routes[i].Price = chatPrice(m.Pricing, ops[i].key, ops[i].what, src)
			routes[i].Deprecated = m.ExpirationDate != nil && *m.ExpirationDate != ""
		}
		out = append(out, routes...)
	}
	return out, nil
}

// chatPrice reads one token rate, falling back to the prompt or completion
// rate when the modality-specific key is absent. Token counts per job are not
// known in advance, so the estimate stays nil and the note names the rate.
func chatPrice(pr map[string]any, key, what, src string) registry.Price {
	p := registry.Price{Unit: registry.UnitMTokens, Source: registry.SourceUnknown, SourceURL: src, AsOf: Today()}
	fallback := "prompt"
	if strings.HasSuffix(key, "_output") {
		fallback = "completion"
	}
	for _, k := range []string{key, fallback} {
		s, _ := pr[k].(string)
		if v, ok := parsePrice(s); ok {
			p.ValueUSD, p.Source = registry.F(roundUSD(v*1e6)), registry.SourceAPI
			p.Note = "per 1M " + what
			break
		}
	}
	if s, _ := pr["completion"].(string); s != "" && !strings.HasSuffix(key, "_output") {
		if v, ok := parsePrice(s); ok {
			p.Note = joinNote(p.Note, fmt.Sprintf("output $%g per 1M tokens", roundUSD(v*1e6)))
		}
	}
	return p
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
