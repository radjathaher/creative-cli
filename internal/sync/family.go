package sync

import (
	"regexp"
	"sort"
	"strings"
)

// brands is the ordered list of model brand stems that anchor a family name.
// Order matters: the first brand that occurs anywhere in an id wins, so more
// specific stems (hunyuan3d, nano-banana, gpt-image) precede generic ones, and
// short stems that collide with ordinary words (gen, ray, dia, sync, music) sit
// at the end where they only catch ids that nothing else claimed.
var brands = []string{
	"seedance", "seedream", "kling", "veo", "hailuo", "wan", "flux", "nano-banana", "gpt-image",
	"meshy", "minimax-music", "minimax-speech", "grok-imagine-video",
	"grok-imagine-image", "grok-imagine", "runway", "aleph", "happyhorse", "pixverse", "omnihuman",
	"qwen-image", "imagen", "ideogram", "recraft", "luma", "hunyuan3d", "hunyuan", "topaz", "sora", "vidu",
	"ltxv", "ltx", "magi", "midjourney", "dreamina", "lyria", "tripo", "rodin", "trellis",
	"chatterbox", "kokoro", "orpheus", "whisper", "heygen", "latentsync",
	"bria", "seedvr", "flashvsr", "bytedance-upscaler", "z-image", "hidream", "stable-audio", "mmaudio",
	"ace-step", "cassette", "beatoven", "mirelo", "kandinsky", "reve", "gemini", "lucy", "marey", "mochi",
	"cogvideox", "veed", "music", "gen", "ray", "dia", "sync",
}

// tiers are id tokens that name a variant of a model rather than the model.
var tiers = map[string]bool{
	"pro": true, "standard": true, "std": true, "fast": true, "turbo": true, "mini": true, "lite": true,
	"max": true, "prime": true, "flash": true, "hd": true, "ultra": true, "plus": true, "dev": true,
	"schnell": true, "klein": true, "master": true, "preview": true, "draft": true, "sunburst": true,
	"flare": true, "precision": true, "creative": true, "generative": true,
}

var brandRes = func() []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(brands))
	for i, b := range brands {
		stem := strings.ReplaceAll(regexp.QuoteMeta(b), "-", "[-_]?")
		out[i] = regexp.MustCompile(`(?:^|[/-])(` + stem + `)(?:[-/_]?(?:video|image|tts))?[-/_]?((?:v|o)?\d+(?:[.\-p]\d+)?)?`)
	}
	return out
}()

var (
	reOmni       = regexp.MustCompile(`gemini-omni(?:-flash)?[-/_]v?(\d+)[.\-p](\d+)`)
	reTripo      = regexp.MustCompile(`tripo3d/(?:tripo/)?v?([hp]?\d+(?:\.\d+)?)(?:/|$)`)
	reHailuo     = regexp.MustCompile(`minimax/h(\d)|\bminimax-h(\d)|\bh(\d)([-/])`)
	reWan        = regexp.MustCompile(`wan(\d)`)
	reElevenV    = regexp.MustCompile(`(multilingual|turbo|flash)[-_]v(\d+(?:\.\d+)?)`)
	reAnyV       = regexp.MustCompile(`(?:^|[-_/])v(\d+(?:\.\d+)?)(?:[-_/]|$)`)
	reVerSep     = regexp.MustCompile(`(\d)[-p](\d)`)
	reLongDigits = regexp.MustCompile(`^\d{4,}$`)
	reAllDigits  = regexp.MustCompile(`^\d+$`)
)

// classifyFamily derives the canonical family and variant from a model id. It
// returns an empty family when no brand matches, and the caller then falls
// back to the lowercased id so the route stays visible.
func classifyFamily(modelID string) (family, variant string) {
	s := strings.ToLower(strings.TrimSpace(modelID))
	variant = variantOf(s)

	// Families whose ids differ too much between providers for the generic rule.
	if strings.Contains(s, "gemini-omni") {
		if m := reOmni.FindStringSubmatch(s); m != nil {
			return "gemini-omni-flash-" + m[1] + "." + m[2], ""
		}
		return "gemini-omni-flash", ""
	}
	if m := reTripo.FindStringSubmatch(s); m != nil {
		return "tripo-" + m[1], variant
	}
	if strings.Contains(s, "elevenlabs") || strings.Contains(s, "eleven-") {
		return elevenFamily(s), variant
	}

	s = reHailuo.ReplaceAllStringFunc(s, func(m string) string {
		sub := reHailuo.FindStringSubmatch(m)
		return "hailuo-" + sub[1] + sub[2] + sub[3] + sub[4]
	})
	s = strings.ReplaceAll(s, "kling-video", "kling")
	s = strings.ReplaceAll(s, "hunyuan-3d", "hunyuan3d")
	s = reWan.ReplaceAllString(s, "wan-$1")

	for i, re := range brandRes {
		m := re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		brand, ver := brands[i], m[2]
		if strings.HasPrefix(ver, "o") {
			return brand + "-" + ver, variant
		}
		if reLongDigits.MatchString(ver) {
			ver = ""
		}
		if ver == "" {
			return brand, variant
		}
		ver = normVersion(ver, brand)
		if brand == "gpt-image" {
			// OpenAI names these models with bare integers (gpt-image-2), and no
			// provider writes "2.0", so the integer form is the canonical one.
			ver = strings.TrimSuffix(ver, ".0")
		}
		return brand + "-" + ver, variant
	}
	return "", variant
}

// normVersion turns "v2-5", "2p5" and "2" into "2.5", "2.5" and "2.0" so the
// same model joins across providers that spell versions differently.
func normVersion(v, brand string) string {
	s := strings.TrimPrefix(v, "v")
	s = reVerSep.ReplaceAllString(s, "$1.$2")
	if brand == "wan" && len(s) == 2 && reAllDigits.MatchString(s) {
		s = s[:1] + "." + s[1:]
	}
	if brand == "hailuo" && len(s) == 2 && s[0] == '0' {
		s = s[1:] + ".0"
	}
	if reAllDigits.MatchString(s) {
		s += ".0"
	}
	return s
}

// elevenFamily names ElevenLabs models by product and version, for example
// eleven-tts-v3, eleven-sfx-v2, eleven-scribe-v2, or eleven-tts-turbo-v2.5.
func elevenFamily(s string) string {
	sub := "other"
	switch {
	case strings.Contains(s, "music"):
		sub = "music"
	case strings.Contains(s, "sound") || strings.Contains(s, "sfx"):
		sub = "sfx"
	case strings.Contains(s, "speech-to-text") || strings.Contains(s, "scribe"):
		sub = "scribe"
	case strings.Contains(s, "isolat"):
		sub = "isolate"
	case strings.Contains(s, "voice-chang") || strings.Contains(s, "speech-to-speech"):
		sub = "voice-change"
	case strings.Contains(s, "dub"):
		sub = "dub"
	case strings.Contains(s, "clone"):
		sub = "clone"
	case strings.Contains(s, "design"):
		sub = "voice-design"
	case strings.Contains(s, "tts") || strings.Contains(s, "text-to-speech") || strings.Contains(s, "dialog") ||
		strings.Contains(s, "eleven-v") || strings.Contains(s, "multilingual"):
		sub = "tts"
	}
	if m := reElevenV.FindStringSubmatch(s); m != nil {
		return "eleven-" + sub + "-" + m[1] + "-v" + m[2]
	}
	// Strip the vendor word first so "elevenlabs" never reads as a version.
	rest := strings.NewReplacer("elevenlabs", "", "eleven-v", "-v").Replace(s)
	if m := reAnyV.FindStringSubmatch(rest); m != nil {
		return "eleven-" + sub + "-v" + m[1]
	}
	return "eleven-" + sub
}

// variantOf collects tier words from the id, sorted and joined with "+".
func variantOf(s string) string {
	seen := map[string]bool{}
	for _, t := range idTokens(s) {
		if tiers[t] {
			seen[t] = true
		}
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return strings.Join(out, "+")
}

// idTokens splits an id on path separators, underscores, and whitespace, and
// on hyphens that precede a letter, so "gpt-image-2-5-sunburst" yields
// "gpt", "image-2-5", "sunburst" and version digits stay attached.
func idTokens(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '/' || c == '_' || c == ':' || c == ' ' || c == '\t':
			flush()
		case c == '-' && i+1 < len(s) && s[i+1] >= 'a' && s[i+1] <= 'z':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}
