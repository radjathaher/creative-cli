package cmd

import (
	"github.com/radjathaher/creative-cli/internal/adapter"
	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/quote"
	"github.com/radjathaher/creative-cli/internal/registry"
	"github.com/spf13/cobra"
)

// addRouteFlags registers the route selectors shared by models, quote, and the
// op commands.
func addRouteFlags(c *cobra.Command) {
	c.Flags().String("family", "", "model family, e.g. seedance-2.5, gemini-omni-flash-1.1, gpt-image-2.5")
	c.Flags().String("variant", "", "family variant, e.g. flare | sunburst")
	c.Flags().String("provider", "", "route provider, e.g. segmind | fal | kie | codex-lb | openai")
	c.Flags().Bool("all", false, "include families outside the image/video allowlist and deprecated routes")
}

// addJobFlags registers the shared job flags. Their values price the job in a
// quote and, for op commands, become the request sent to the provider.
func addJobFlags(c *cobra.Command) {
	c.Flags().Float64("duration", 0, "output duration in seconds (video/audio)")
	c.Flags().String("res", "", "resolution: 360p | 480p | 720p | 1080p | 4k (video) or 1k | 2k | 4k (image)")
	c.Flags().String("aspect", "", "aspect ratio, e.g. 9:16, 16:9, 1:1, adaptive")
	c.Flags().Int("count", 1, "number of images or clips")
	c.Flags().Int("chars", 0, "characters of text (TTS pricing)")
	c.Flags().Int("fps", 0, "target frame rate (upscale pricing and requests)")
	c.Flags().String("quality", "", "quality tier where the route has one, e.g. low | medium | high")
	addRefFlags(c)
}

// readQuoteRequest turns flags into a quote request. It probes the longest
// local reference video so routes with a per-video length limit can be excluded.
func readQuoteRequest(c *cobra.Command, op string) quote.Request {
	images, videos, audios := flagStrs(c, "image"), flagStrs(c, "video"), flagStrs(c, "audio")
	req := quote.Request{
		Op: op, Family: flagStr(c, "family"), Variant: flagStr(c, "variant"),
		Provider: flagStr(c, "provider"), All: flagBool(c, "all"), Runnable: runnableRoute,
		Images: len(images), Videos: len(videos), Audios: len(audios),
		Job: registry.Job{
			DurationSecs: flagFloat(c, "duration"), Resolution: flagStr(c, "res"),
			Aspect: flagStr(c, "aspect"), Count: flagInt(c, "count"), Chars: flagInt(c, "chars"),
			FPS: flagInt(c, "fps"), Quality: flagStr(c, "quality"), HasVideoInput: len(videos) > 0,
		},
	}
	if t := flagStr(c, "text"); t != "" && req.Job.Chars == 0 {
		req.Job.Chars = len([]rune(t))
	}
	for _, v := range videos {
		if md, err := core.ProbeVideo(v); err == nil && md.DurationSeconds > req.VideoSecs {
			req.VideoSecs = md.DurationSeconds
		}
	}
	return req
}

func runnableRoute(r registry.Route) bool {
	if r.Op == registry.OpBrollSearch || r.Op == registry.OpBrollGet {
		return r.Transport == "broll-http" && (r.Provider == "pexels" || r.Provider == "pixabay")
	}
	return adapter.Runnable(r)
}
