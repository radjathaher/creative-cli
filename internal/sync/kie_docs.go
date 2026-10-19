package sync

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/radjathaher/creative-cli/internal/registry"
)

var kieDocLink = regexp.MustCompile(`\((https://docs\.kie\.ai/market/[^)\s]+\.md)\)`)

// kieDocURLs picks the English market doc pages whose family is allowlisted.
func kieDocURLs(index []byte) []string {
	var urls []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(index), "\n") {
		m := kieDocLink.FindStringSubmatch(line)
		if m == nil || seen[m[1]] {
			continue
		}
		slug := strings.TrimSuffix(m[1][strings.LastIndex(m[1], "/")+1:], ".md")
		if _, fam, _ := Classify("kie", "", slug, line); allowlistFamilies[fam] {
			seen[m[1]] = true
			urls = append(urls, m[1])
		}
	}
	return urls
}

// kieAttachSchemas copies doc schemas onto routes by API model name. Routes
// for a multi-mode endpoint gain sibling routes for each mode the schema
// proves it accepts, so `video.r2v` quotes see KIE Omni and Seedance.
func kieAttachSchemas(routes []registry.Route, docs map[string]json.RawMessage) []registry.Route {
	have := map[string]bool{}
	for _, r := range routes {
		have[r.ModelID+"|"+r.Op] = true
	}
	var out []registry.Route
	for _, r := range routes {
		schema, ok := docs[r.ModelID]
		if !ok {
			out = append(out, r)
			continue
		}
		applySchema(&r, schema)
		out = append(out, r)
		for _, sib := range modeSiblings(r) {
			if !have[sib.ModelID+"|"+sib.Op] {
				sib.ID = "kie:" + r.ModelID + "#" + sib.Op
				out = append(out, sib)
			}
		}
	}
	return out
}

// kieDocSchema extracts the API model name and the `input` object schema from
// a KIE doc page, which embeds its OpenAPI 3.0.1 spec as a fenced YAML block.
func kieDocSchema(page []byte) (model string, schema json.RawMessage, err error) {
	text := string(page)
	start := strings.Index(text, "```yaml\n")
	if start < 0 {
		return "", nil, fmt.Errorf("no openapi yaml block")
	}
	text = text[start+len("```yaml\n"):]
	if end := strings.Index(text, "\n```"); end >= 0 {
		text = text[:end]
	}
	var spec map[string]any
	if err := yaml.Unmarshal([]byte(text), &spec); err != nil {
		return "", nil, fmt.Errorf("parse openapi yaml: %w", err)
	}
	paths, _ := spec["paths"].(map[string]any)
	for _, p := range paths {
		body := dig(p, "post", "requestBody", "content", "application/json", "schema", "properties")
		props, ok := body.(map[string]any)
		if !ok {
			continue
		}
		model = firstEnum(props["model"])
		input, ok := props["input"].(map[string]any)
		if model == "" || !ok {
			continue
		}
		raw, err := json.Marshal(pruneSchema(input))
		if err != nil {
			return "", nil, err
		}
		return model, raw, nil
	}
	return "", nil, fmt.Errorf("no createTask body with model and input")
}

func dig(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func firstEnum(v any) string {
	m, _ := v.(map[string]any)
	if e, ok := m["enum"].([]any); ok && len(e) > 0 {
		return fmt.Sprint(e[0])
	}
	if d, ok := m["default"].(string); ok {
		return d
	}
	return ""
}

// pruneSchema drops doc-tool annotations and examples, which make up most of
// the bytes and carry nothing the router validates against.
func pruneSchema(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if strings.HasPrefix(k, "x-") || k == "examples" || k == "example" {
				continue
			}
			out[k] = pruneSchema(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = pruneSchema(val)
		}
		return out
	}
	return v
}
