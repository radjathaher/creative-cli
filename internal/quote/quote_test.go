package quote

import (
	"encoding/json"
	"testing"

	"github.com/radjathaher/creative-cli/internal/registry"
)

// A route whose schema maps --image but states no maxItems must accept
// images; one whose schema has no image field must not.
func TestRejectImageCounts(t *testing.T) {
	schema := json.RawMessage(`{"properties":{"image_urls":{"type":"array"}}}`)
	mapped := registry.Route{Schema: schema, ParamMap: map[string]string{"image": "image_urls"}}
	if msg := reject(mapped, Request{Images: 3}); msg != "" {
		t.Errorf("mapped image field rejected: %s", msg)
	}
	unmapped := registry.Route{Schema: json.RawMessage(`{"properties":{"prompt":{"type":"string"}}}`)}
	if msg := reject(unmapped, Request{Images: 1}); msg == "" {
		t.Error("route without an image field accepted images")
	}
}
