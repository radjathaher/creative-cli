package falad

import (
	"sort"
	"strings"

	"github.com/radjathaher/creative-cli/internal/adapter/reqmap"
	"github.com/radjathaher/creative-cli/internal/job"
)

// primaryKeys are the result fields that hold the generated asset, checked
// before any other field so the main output comes first.
var primaryKeys = []string{"video", "videos", "images", "image", "audio", "audio_file", "audio_url", "model_glb", "model_mesh", "model_urls", "mesh", "file"}

// Outputs collects every file object ({"url": ..., "content_type": ...}) in a
// fal result, primary fields first, plus top-level text for transcription and
// understanding routes. Inputs echoed back by some models are not filtered,
// because fal results do not mark them; duplicates are dropped by URL.
func Outputs(result map[string]any) []job.Output {
	var out []job.Output
	seen := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if u, ok := t["url"].(string); ok && strings.HasPrefix(u, "http") {
				if !seen[u] {
					seen[u] = true
					mime, _ := t["content_type"].(string)
					if mime == "" || mime == "application/octet-stream" {
						mime = reqmap.MIMEForURL(u)
					}
					out = append(out, job.Output{URL: u, MIME: mime})
				}
				return
			}
			for _, k := range sortedAnyKeys(t) {
				walk(t[k])
			}
		case []any:
			for _, x := range t {
				walk(x)
			}
		case string:
			if strings.HasPrefix(t, "https://") && looksLikeMedia(t) && !seen[t] {
				seen[t] = true
				out = append(out, job.Output{URL: t, MIME: reqmap.MIMEForURL(t)})
			}
		}
	}
	for _, k := range primaryKeys {
		if v, ok := result[k]; ok {
			walk(v)
		}
	}
	for _, k := range sortedAnyKeys(result) {
		walk(result[k])
	}
	for _, k := range []string{"text", "output", "transcript"} {
		if s, ok := result[k].(string); ok && s != "" {
			out = append(out, job.Output{Text: s, MIME: "text/plain"})
			break
		}
	}
	return out
}

func looksLikeMedia(u string) bool {
	m := reqmap.MIMEForURL(u)
	return strings.HasPrefix(m, "image/") || strings.HasPrefix(m, "video/") || strings.HasPrefix(m, "audio/") || strings.HasPrefix(m, "model/")
}

func sortedAnyKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
