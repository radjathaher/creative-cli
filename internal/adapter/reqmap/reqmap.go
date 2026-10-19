// Package reqmap turns a provider-neutral job.Request into a provider input
// object using a route's ParamMap and input schema. Adapters that speak a
// "JSON object of named fields" protocol (Segmind, KIE) share it and then apply
// their own provider rules to the result.
package reqmap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/job"
	"github.com/radjathaher/creative-cli/internal/registry"
)

// Uploader turns a local file into a URL the provider can fetch.
type Uploader func(ctx context.Context, path string) (string, error)

// DryRunPrefix marks a local file that a real run would upload first.
const DryRunPrefix = "upload://"

// Prop is the part of a JSON Schema property the mapper needs.
type Prop struct {
	Type any `json:"type"`
}

// Props returns the schema's top-level properties; nil when there is no schema.
func Props(schema json.RawMessage) map[string]Prop {
	var s struct {
		Properties map[string]Prop `json:"properties"`
	}
	_ = json.Unmarshal(schema, &s)
	return s.Properties
}

// TypeOf returns the property's JSON type, looking through ["x","null"] lists.
func (p Prop) TypeOf() string {
	switch t := p.Type.(type) {
	case string:
		return t
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok && s != "null" {
				return s
			}
		}
	}
	return ""
}

// Build maps req onto the route's provider fields. Reference lists keep the
// order the user gave. Local files are uploaded with up, or replaced by an
// upload:// placeholder when dryRun is set. --param values override shared
// flags and must name a field the schema declares.
func Build(ctx context.Context, r registry.Route, req job.Request, up Uploader, dryRun bool) (map[string]any, error) {
	props := Props(r.Schema)
	pm := r.ParamMap
	body := map[string]any{}
	resolve := func(ref string) (string, error) {
		switch {
		case ref == "" || core.IsRemoteRef(ref):
			return ref, nil
		case dryRun:
			return DryRunPrefix + ref, nil
		case up == nil:
			return "", fmt.Errorf("%s: route %s cannot take local files", ref, r.ID)
		}
		return up(ctx, ref)
	}
	set := func(flag string, value any) error {
		field := pm[flag]
		if field == "" {
			return fmt.Errorf("route %s does not accept --%s", r.ID, strings.ReplaceAll(flag, "_", "-"))
		}
		v, err := coerce(value, props[field].TypeOf())
		if err != nil {
			return fmt.Errorf("--%s: %w", flag, err)
		}
		body[field] = v
		return nil
	}
	media := func(flag string, refs []string) error {
		if len(refs) == 0 {
			return nil
		}
		urls := make([]string, len(refs))
		for i, ref := range refs {
			u, err := resolve(ref)
			if err != nil {
				return err
			}
			urls[i] = u
		}
		field := pm[flag]
		if field != "" && props[field].TypeOf() != "array" {
			if len(urls) > 1 {
				return fmt.Errorf("route %s takes one --%s, got %d", r.ID, flag, len(urls))
			}
			return set(flag, urls[0])
		}
		return set(flag, urls)
	}

	prompt := req.Prompt
	if prompt == "" {
		prompt = req.Text
	}
	steps := []struct {
		flag string
		val  any
		ok   bool
	}{
		{"prompt", prompt, prompt != ""},
		{"duration", req.Duration, req.Duration > 0},
		{"resolution", req.Resolution, req.Resolution != ""},
		{"aspect", req.Aspect, req.Aspect != ""},
		{"seed", deref(req.Seed), req.Seed != nil},
		{"generate_audio", derefB(req.GenerateAudio), req.GenerateAudio != nil},
		{"fps", req.FPS, req.FPS > 0},
		{"quality", req.Quality, req.Quality != ""},
		{"n", req.Count, req.Count > 1},
	}
	for _, s := range steps {
		if s.ok {
			if err := set(s.flag, s.val); err != nil {
				return nil, err
			}
		}
	}
	for _, m := range []struct {
		flag string
		refs []string
	}{
		{"image", req.Images}, {"video", req.Videos}, {"audio", req.Audios},
		{"first_frame", one(req.FirstFrame)}, {"last_frame", one(req.LastFrame)},
		{InputFlag(pm, req.Input), one(req.Input)},
	} {
		if err := media(m.flag, m.refs); err != nil {
			return nil, err
		}
	}
	return body, applyParams(ctx, r, props, body, req.Params, up, dryRun)
}

// InputFlag picks the shared media flag that carries req.Input (the asset an
// upscale, caption, or transcription transforms) from its file type.
func InputFlag(pm map[string]string, input string) string {
	if input == "" {
		return "video"
	}
	mime := core.MIMEForPath(strings.SplitN(input, "?", 2)[0])
	for _, kind := range []string{"video", "image", "audio"} {
		if strings.HasPrefix(mime, kind+"/") && pm[kind] != "" {
			return kind
		}
	}
	for _, kind := range []string{"video", "image", "audio"} {
		if pm[kind] != "" {
			return kind
		}
	}
	return "video"
}

func applyParams(ctx context.Context, r registry.Route, props map[string]Prop, body map[string]any, params map[string]any, up Uploader, dryRun bool) error {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p, ok := props[k]
		if !ok {
			return fmt.Errorf("--param %s: route %s has no such field (valid: %s)", k, r.ID, strings.Join(fieldNames(props), ", "))
		}
		raw, err := uploadParam(ctx, k, params[k], up, dryRun)
		if err != nil {
			return fmt.Errorf("--param %s: %w", k, err)
		}
		v, err := coerce(raw, p.TypeOf())
		if err != nil {
			return fmt.Errorf("--param %s: %w", k, err)
		}
		body[k] = v
	}
	return nil
}

func fieldNames(props map[string]Prop) []string {
	out := make([]string, 0, len(props))
	for k := range props {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// coerce converts a flag value to the schema's declared type, so a duration
// becomes the integer one provider wants or the "6" string another enumerates.
func coerce(v any, typ string) (any, error) {
	s, isStr := v.(string)
	switch typ {
	case "string":
		switch t := v.(type) {
		case float64:
			return strconv.FormatFloat(t, 'f', -1, 64), nil
		case int:
			return strconv.Itoa(t), nil
		}
	case "integer":
		switch t := v.(type) {
		case float64:
			return int(t), nil
		case string:
			n, err := strconv.Atoi(strings.TrimSpace(t))
			if err != nil {
				return nil, fmt.Errorf("%q is not an integer", t)
			}
			return n, nil
		}
	case "number":
		if isStr {
			f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return nil, fmt.Errorf("%q is not a number", s)
			}
			return f, nil
		}
	case "boolean":
		if isStr {
			b, err := strconv.ParseBool(strings.TrimSpace(s))
			if err != nil {
				return nil, fmt.Errorf("%q is not a boolean", s)
			}
			return b, nil
		}
	case "array", "object":
		if isStr && (strings.HasPrefix(strings.TrimSpace(s), "[") || strings.HasPrefix(strings.TrimSpace(s), "{")) {
			var out any
			if err := json.Unmarshal([]byte(s), &out); err != nil {
				return nil, fmt.Errorf("invalid JSON: %w", err)
			}
			return out, nil
		}
	}
	return v, nil
}

func one(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefB(p *bool) bool { return p != nil && *p }

// IsSafety reports whether a provider error message describes a content
// moderation block rather than an ordinary failure.
func IsSafety(msg string) bool {
	m := strings.ToLower(msg)
	for _, w := range []string{"nsfw", "safety", "moderation", "content policy", "sensitive", "inappropriate", "prohibited", "violat"} {
		if strings.Contains(m, w) {
			return true
		}
	}
	return false
}

// MIMEForURL guesses an output's content type from its URL path.
func MIMEForURL(u string) string {
	return core.MIMEForPath(strings.SplitN(u, "?", 2)[0])
}

// uploadParam uploads a local file given through --param to a URL-typed field
// (any name ending in _url or _urls), so view slots such as back_image_url
// receive a fetchable URL rather than a path on this machine.
func uploadParam(ctx context.Context, field string, v any, up Uploader, dryRun bool) (any, error) {
	path, ok := v.(string)
	if !ok || !(strings.HasSuffix(field, "_url") || strings.HasSuffix(field, "_urls")) || core.IsRemoteRef(path) {
		return v, nil
	}
	if fi, err := os.Stat(path); err != nil || fi.IsDir() {
		return v, nil
	}
	if dryRun || up == nil {
		return DryRunPrefix + path, nil
	}
	return up(ctx, path)
}
