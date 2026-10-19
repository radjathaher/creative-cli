package sync

import (
	"regexp"
	"strings"
	gosync "sync"

	"github.com/radjathaher/creative-cli/internal/registry"
)

// Classify assigns an op, a canonical model family, and a variant to a route.
// category is the provider's own label when it has one (fal "text-to-video",
// Segmind "imageToVideo", a coarse "video"), and may be empty; when it is not
// a recognised label the category is inferred from the model id.
//
// The family must join the same model across providers, so "seedance-2.5" on
// Segmind, KIE, fal, and OpenRouter all classify to the same string. The family
// is derived from modelID alone, because titles carry marketing words that
// would split families. Anything unmatched keeps family = lowercased model id
// and, when its op is unknown, op = other, so nothing is hidden.
func Classify(provider, category, modelID, title string) (op, family, variant string) {
	id := strings.ToLower(modelID)
	cat := normCategory(category)
	if cat == "" {
		cat = inferCategory(id, strings.ToLower(category))
	}
	op = classifyOp(cat, id)
	family, variant = classifyFamily(modelID)
	if family == "" {
		family = id
	}
	return op, family, variant
}

// categories maps provider labels, compacted to lowercase letters and digits,
// onto fal's kebab-case vocabulary, which classifyOp switches on.
var categories = map[string]string{
	"texttoimage": "text-to-image", "imagetoimage": "image-to-image", "inpainting": "image-to-image",
	"imagetovideo": "image-to-video", "texttovideo": "text-to-video", "videotovideo": "video-to-video",
	"audiotovideo": "audio-to-video", "texttoaudio": "text-to-audio", "audiotoaudio": "audio-to-audio",
	"audiototext": "audio-to-text", "speechtotext": "speech-to-text", "imagetotext": "image-to-text",
	"videototext": "video-to-text", "imageto3d": "image-to-3d", "textto3d": "text-to-3d", "3dto3d": "3d-to-3d",
	"texttotext": "llm", "texttoembed": "llm", "llm": "llm", "videotoaudio": "video-to-audio",
	"voice": "text-to-speech", "texttospeech": "text-to-speech", "speechtospeech": "speech-to-speech",
	"imagetodata": "image-to-json", "imagetojson": "image-to-json", "texttojson": "text-to-json", "json": "json",
	"videotoimage": "video-to-video", "vision": "vision", "worlds": "worlds", "training": "training",
}

func normCategory(category string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(category) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		}
	}
	return categories[b.String()]
}

var inferRules = []struct {
	re  *regexp.Regexp
	cat string
}{
	{regexp.MustCompile(`speech-to-text|\bstt\b|transcri|scribe|whisper`), "speech-to-text"},
	{regexp.MustCompile(`text-to-speech|\btts\b`), "text-to-speech"},
	{regexp.MustCompile(`(?:multiview|multi-image|image)-to-3d`), "image-to-3d"},
	{regexp.MustCompile(`text-to-3d`), "text-to-3d"},
	{regexp.MustCompile(`rigging|retexture|remesh`), "3d-to-3d"},
	{regexp.MustCompile(`reference-to-video|\br2v\b|image-to-video|\bi2v\b|image2video|img2vid`), "image-to-video"},
	{regexp.MustCompile(`text-to-video|\bt2v\b|text2video`), "text-to-video"},
	{regexp.MustCompile(`video-to-video|\bv2v\b|video2video|video-?edit|video-extend|upscal\w*[-/]video|video[-/]upscal`), "video-to-video"},
	{regexp.MustCompile(`text-to-image|\bt2i\b|text2image`), "text-to-image"},
	{regexp.MustCompile(`image-to-image|\bi2i\b|image2image|[-/]edit\b|upscal`), "image-to-image"},
	{regexp.MustCompile(`sound|sfx|music|song|audio`), "text-to-audio"},
}

// hintCategories maps a coarse provider label ("video") to the category a
// model with no op words in its id most likely serves.
var hintCategories = map[string]string{
	"video": "text-to-video", "image": "text-to-image", "audio": "text-to-audio", "music": "text-to-audio",
	"speech": "text-to-speech", "tts": "text-to-speech", "3d": "image-to-3d", "chat": "llm", "text": "llm",
}

func inferCategory(id, hint string) string {
	for _, r := range inferRules {
		if r.re.MatchString(id) {
			return r.cat
		}
	}
	return hintCategories[strings.TrimSpace(hint)]
}

var (
	reCaption    = regexp.MustCompile(`subtitle|auto-?caption`)
	reVoiceClone = regexp.MustCompile(`voice[-_]?clon|create-voice`)
	reVoiceDsgn  = regexp.MustCompile(`voice[-_]?design`)
	reTTS        = regexp.MustCompile(`text-to-speech|\btts\b`)
	reSTT        = regexp.MustCompile(`speech-to-text|scribe|whisper|transcri`)
	reSFX        = regexp.MustCompile(`sound-?effect|\bsfx\b|foley`)
	reReference  = regexp.MustCompile(`reference|\br2v\b`)
)

// classifyOp ports the research normalizer's rules onto the closed op
// vocabulary. Id keywords that are unambiguous across providers come first,
// because providers file the same model under different categories.
func classifyOp(cat, k string) string {
	videoish := strings.HasSuffix(cat, "-to-video") || strings.HasPrefix(cat, "video-")
	switch {
	case reCaption.MatchString(k) || (videoish && strings.Contains(k, "caption")):
		return registry.OpCaptionBurn
	case reVoiceClone.MatchString(k):
		return registry.OpVoiceClone
	case reVoiceDsgn.MatchString(k):
		return registry.OpVoiceDesign
	case reTTS.MatchString(k) && !reSTT.MatchString(k):
		return registry.OpVoiceTTS
	case reSTT.MatchString(k):
		return registry.OpSTT
	case reSFX.MatchString(k):
		if strings.HasPrefix(cat, "video-") || strings.Contains(k, "video") {
			return registry.OpSFXFromVideo
		}
		return registry.OpSFXGenerate
	case strings.Contains(k, "rigging") || (strings.Contains(k, "animation") && cat == "3d-to-3d"):
		if strings.Contains(k, "animat") {
			return registry.Op3DAnimate
		}
		return registry.Op3DRig
	case videoish && reReference.MatchString(k) && !has(k, `upscal|lip-?sync`):
		return registry.OpVideoR2V
	}
	return classifyByCategory(cat, k)
}

func classifyByCategory(cat, k string) string {
	switch cat {
	case "text-to-image":
		if has(k, `upscal`) {
			return registry.OpImageUpscale
		}
		return registry.OpImageT2I
	case "image-to-image":
		return classifyImageEdit(k)
	case "text-to-video":
		if has(k, `avatar`) {
			return registry.OpVideoAvatar
		}
		return registry.OpVideoT2V
	case "image-to-video", "audio-to-video":
		return classifyImageToVideo(cat, k)
	case "video-to-video":
		return classifyVideoEdit(k)
	case "text-to-speech":
		return registry.OpVoiceTTS
	case "speech-to-speech":
		return registry.OpVoiceChange
	case "text-to-audio":
		switch {
		case has(k, `music|song|lyria|ace-step|cassette|beatoven|stable-audio`):
			return registry.OpMusicGenerate
		case has(k, `speech|dialog|voice`):
			return registry.OpVoiceTTS
		}
		return registry.OpOther
	case "audio-to-audio":
		return classifyAudioEdit(k)
	case "video-to-audio":
		return registry.OpSFXFromVideo
	case "speech-to-text", "audio-to-text":
		return registry.OpSTT
	case "vision", "image-to-text", "image-to-json":
		if has(k, `video`) {
			return registry.OpUnderstandVid
		}
		return registry.OpUnderstandImg
	case "video-to-text":
		return registry.OpUnderstandVid
	case "text-to-3d":
		return registry.Op3DT2Mesh
	case "image-to-3d":
		return registry.Op3DI2Mesh
	case "3d-to-3d":
		switch {
		case has(k, `textur`):
			return registry.Op3DTexture
		case has(k, `convert|export|format`):
			return registry.Op3DConvert
		}
		return registry.OpOther
	case "llm", "text-to-text", "json", "text-to-json":
		return registry.OpTextChat
	}
	return registry.OpOther
}

func classifyImageEdit(k string) string {
	switch {
	case has(k, `upscal|super-?res|clarity|esrgan|seedvr|topaz/upscale`):
		return registry.OpImageUpscale
	case has(k, `background|rembg|bg-?remov|birefnet|remove-bg`):
		return registry.OpImageBGRemove
	case has(k, `inpaint|fill|eras|object-remov`):
		return registry.OpImageInpaint
	}
	return registry.OpImageEdit
}

func classifyImageToVideo(cat, k string) string {
	switch {
	case has(k, `lip-?sync`):
		return registry.OpVideoLipsync
	case has(k, `avatar|omnihuman|talking|infinitalk|speech-to-video`):
		return registry.OpVideoAvatar
	case has(k, `motion-control|animate`):
		return registry.OpVideoEdit
	case has(k, `first-last|flf|start-end|keyframe`):
		return registry.OpVideoFLF2V
	case cat == "audio-to-video":
		return registry.OpVideoAvatar
	}
	return registry.OpVideoI2V
}

func classifyVideoEdit(k string) string {
	switch {
	case has(k, `upscal|seedvr|flashvsr|super-?res|enhance`):
		return registry.OpVideoUpscale
	case has(k, `lip-?sync|sync-lips|latentsync`):
		return registry.OpVideoLipsync
	case has(k, `extend`):
		return registry.OpVideoExtend
	case has(k, `background|matting|interpolat|frame`):
		return registry.OpOther
	case has(k, `dub`):
		return registry.OpVoiceDub
	}
	return registry.OpVideoEdit
}

func classifyAudioEdit(k string) string {
	switch {
	case has(k, `isolat|separat|denois|clean|demucs`):
		return registry.OpVoiceIsolate
	case has(k, `stem`):
		return registry.OpMusicStems
	case has(k, `voice-?chang|speech-to-speech|convert`):
		return registry.OpVoiceChange
	case has(k, `dub`):
		return registry.OpVoiceDub
	case has(k, `extend`):
		return registry.OpMusicExtend
	}
	return registry.OpOther
}

var hasCache gosync.Map // pattern -> *regexp.Regexp

// has reports whether s matches pattern. Patterns are package literals, so the
// cache stops growing after the first classification run; sync.Map keeps it
// safe when sources classify concurrently.
func has(s, pattern string) bool {
	re, ok := hasCache.Load(pattern)
	if !ok {
		re, _ = hasCache.LoadOrStore(pattern, regexp.MustCompile(pattern))
	}
	return re.(*regexp.Regexp).MatchString(s)
}
