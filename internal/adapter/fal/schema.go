package falad

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/radjathaher/creative-cli/internal/registry"
)

// normalizeSchema copies the first non-null type out of anyOf for each
// property. fal declares optional fields as anyOf [{type: X}, {type: null}],
// and without a top-level type the shared mapper could not coerce
// "--param recover_detail=0.2" into the number fal expects.
func normalizeSchema(schema json.RawMessage) json.RawMessage {
	var s map[string]any
	if json.Unmarshal(schema, &s) != nil {
		return schema
	}
	props, _ := s["properties"].(map[string]any)
	for _, v := range props {
		p, ok := v.(map[string]any)
		if !ok || p["type"] != nil {
			continue
		}
		branches, _ := p["anyOf"].([]any)
		for _, b := range branches {
			bm, _ := b.(map[string]any)
			if t, ok := bm["type"].(string); ok && t != "null" {
				p["type"] = t
				if bm["enum"] != nil && p["enum"] == nil {
					p["enum"] = bm["enum"]
				}
				break
			}
		}
	}
	out, err := json.Marshal(s)
	if err != nil {
		return schema
	}
	return out
}

// checkEnums rejects values outside a field's enum before any money is spent.
// A case-only mismatch ("720P") is corrected to the schema's spelling.
func checkEnums(r registry.Route, body map[string]any) error {
	var s struct {
		Properties map[string]struct {
			Enum []any `json:"enum"`
		} `json:"properties"`
	}
	if json.Unmarshal(r.Schema, &s) != nil {
		return nil
	}
	fields := make([]string, 0, len(body))
	for k := range body {
		fields = append(fields, k)
	}
	sort.Strings(fields)
	for _, k := range fields {
		enum := s.Properties[k].Enum
		if len(enum) == 0 {
			continue
		}
		got := fmt.Sprint(body[k])
		ok := false
		for _, e := range enum {
			if want := fmt.Sprint(e); strings.EqualFold(want, got) {
				if want != got {
					body[k] = e
				}
				ok = true
				break
			}
		}
		if !ok {
			vals := make([]string, len(enum))
			for i, e := range enum {
				vals[i] = fmt.Sprint(e)
			}
			return fmt.Errorf("route %s: %s=%v is not one of %s", r.ID, k, body[k], strings.Join(vals, ", "))
		}
	}
	return nil
}
