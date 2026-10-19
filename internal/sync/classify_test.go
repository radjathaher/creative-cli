package sync

import (
	"testing"

	"github.com/radjathaher/creative-cli/internal/registry"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		provider, category, id string
		op, family, variant    string
	}{
		// Seedance 2.5 joins across all four providers.
		{"segmind", "imageToVideo", "seedance-2.5", registry.OpVideoI2V, "seedance-2.5", ""},
		{"kie", "", "bytedance/seedance-2-5", "", "seedance-2.5", ""},
		{"kie", "", "bytedance-seedance-2-5", "", "seedance-2.5", ""},
		{"fal", "text-to-video", "bytedance/seedance-2.5/us/text-to-video", registry.OpVideoT2V, "seedance-2.5", ""},
		{"fal", "image-to-video", "bytedance/seedance-2.5/us/reference-to-video", registry.OpVideoR2V, "seedance-2.5", ""},
		{"openrouter", "video", "bytedance/seedance-2.5", registry.OpVideoT2V, "seedance-2.5", ""},
		{"fal", "image-to-video", "bytedance/seedance-2.0/fast/image-to-video", registry.OpVideoI2V, "seedance-2.0", "fast"},
		{"openrouter", "video", "bytedance/seedance-1-5-pro", "", "seedance-1.5", "pro"},
		{"fal", "image-to-video", "fal-ai/bytedance/seedance/v1.5/pro/image-to-video", registry.OpVideoI2V, "seedance-1.5", "pro"},

		// Gemini Omni Flash, with and without the 1.1 version.
		{"fal", "image-to-video", "google/gemini-omni-flash/v1.1/reference-to-video", registry.OpVideoR2V, "gemini-omni-flash-1.1", ""},
		{"fal", "text-to-video", "google/gemini-omni-flash/v1.1/text-to-video", registry.OpVideoT2V, "gemini-omni-flash-1.1", ""},
		{"kie", "", "google-gemini-omni-flash-1-1", "", "gemini-omni-flash-1.1", ""},
		{"segmind", "textToVideo", "gemini-omni-1.1", registry.OpVideoT2V, "gemini-omni-flash-1.1", ""},
		{"segmind", "videoToVideo", "gemini-omni-1.1-video-extend", registry.OpVideoExtend, "gemini-omni-flash-1.1", ""},
		{"segmind", "textToVideo", "gemini-omni-flash", registry.OpVideoT2V, "gemini-omni-flash", ""},
		{"fal", "image-to-video", "google/gemini-omni-flash/reference-to-video", registry.OpVideoR2V, "gemini-omni-flash", ""},

		// gpt-image families and variants.
		{"fal", "image-to-image", "openai/gpt-image-2.5/flare/edit", registry.OpImageEdit, "gpt-image-2.5", "flare"},
		{"fal", "text-to-image", "openai/gpt-image-2.5/sunburst/text-to-image", registry.OpImageT2I, "gpt-image-2.5", "sunburst"},
		{"kie", "", "gpt-image-2-5-sunburst-text-to-image", registry.OpImageT2I, "gpt-image-2.5", "sunburst"},
		{"openrouter", "image", "openai/gpt-image-2.5-flare", registry.OpImageT2I, "gpt-image-2.5", "flare"},
		{"segmind", "textToImage", "gpt-image-2.5-flare", registry.OpImageT2I, "gpt-image-2.5", "flare"},
		{"fal", "text-to-image", "openai/gpt-image-2", registry.OpImageT2I, "gpt-image-2", ""},
		{"segmind", "textToImage", "gpt-image-2", registry.OpImageT2I, "gpt-image-2", ""},
		{"kie", "", "gpt-image-2-text-to-image", registry.OpImageT2I, "gpt-image-2", ""},
		{"fal", "text-to-image", "fal-ai/gpt-image-1-mini", registry.OpImageT2I, "gpt-image-1", "mini"},

		// Video upscalers.
		{"fal", "video-to-video", "topaz/upscale/video/precision", registry.OpVideoUpscale, "topaz", "precision"},
		{"fal", "video-to-video", "fal-ai/seedvr/upscale/video", registry.OpVideoUpscale, "seedvr", ""},
		{"fal", "video-to-video", "fal-ai/flashvsr/upscale/video", registry.OpVideoUpscale, "flashvsr", ""},
		{"fal", "video-to-video", "fal-ai/bytedance-upscaler/upscale/video", registry.OpVideoUpscale, "bytedance-upscaler", ""},
		{"fal", "video-to-video", "topaz/interpolate/video", registry.OpOther, "topaz", ""},
		{"fal", "image-to-image", "topaz/upscale/image/precision", registry.OpImageUpscale, "topaz", "precision"},

		// 3D.
		{"fal", "image-to-3d", "tripo3d/h3.1/multiview-to-3d", registry.Op3DI2Mesh, "tripo-h3.1", ""},
		{"fal", "image-to-3d", "tripo3d/tripo/v2.5/image-to-3d", registry.Op3DI2Mesh, "tripo-2.5", ""},
		{"fal", "text-to-3d", "tripo3d/p1/text-to-3d", registry.Op3DT2Mesh, "tripo-p1", ""},
		{"fal", "image-to-3d", "meshy/v7.1/image-to-3d", registry.Op3DI2Mesh, "meshy-7.1", ""},
		{"fal", "image-to-3d", "fal-ai/meshy/v6-preview/image-to-3d", registry.Op3DI2Mesh, "meshy-6.0", "preview"},
		{"fal", "image-to-3d", "fal-ai/hunyuan3d-v3/image-to-3d", registry.Op3DI2Mesh, "hunyuan3d-3.0", ""},
		{"fal", "image-to-3d", "fal-ai/hunyuan3d/v2/mini", registry.Op3DI2Mesh, "hunyuan3d-2.0", "mini"},
		{"segmind", "imageTo3d", "hunyuan-3d-3.1-pro-image-to-3d", registry.Op3DI2Mesh, "hunyuan3d-3.1", "pro"},
		{"fal", "3d-to-3d", "fal-ai/meshy/rigging", registry.Op3DRig, "meshy", ""},
		{"fal", "3d-to-3d", "fal-ai/meshy/rigging/multi-animation", registry.Op3DAnimate, "meshy", ""},
		{"fal", "3d-to-3d", "meshy/v7/retexture", registry.Op3DTexture, "meshy-7.0", ""},

		// Audio and captions.
		{"fal", "text-to-audio", "fal-ai/elevenlabs/sound-effects/v2", registry.OpSFXGenerate, "eleven-sfx-v2", ""},
		{"fal", "video-to-video", "cassetteai/video-sound-effects-generator", registry.OpSFXFromVideo, "cassette", ""},
		{"fal", "speech-to-text", "fal-ai/elevenlabs/speech-to-text/scribe-v2", registry.OpSTT, "eleven-scribe-v2", ""},
		{"fal", "speech-to-text", "fal-ai/whisper", registry.OpSTT, "whisper", ""},
		{"segmind", "audioToText", "whisper-large-v3", registry.OpSTT, "whisper", ""},
		{"kie", "", "elevenlabs/speech-to-text", registry.OpSTT, "eleven-scribe", ""},
		{"fal", "text-to-speech", "fal-ai/elevenlabs/tts/eleven-v3", registry.OpVoiceTTS, "eleven-tts-v3", ""},
		{"fal", "text-to-speech", "elevenlabs/tts/eleven-v4-turbo", registry.OpVoiceTTS, "eleven-tts-v4", "turbo"},
		{"segmind", "textToAudio", "elevenlabs-v4-tts", registry.OpVoiceTTS, "eleven-tts-v4", ""},
		{"fal", "text-to-speech", "fal-ai/elevenlabs/tts/turbo-v2.5", registry.OpVoiceTTS, "eleven-tts-turbo-v2.5", "turbo"},
		{"segmind", "audioToText", "tts-elevenlabs-with-timestamps", registry.OpVoiceTTS, "eleven-tts", ""},
		{"fal", "video-to-video", "veed/subtitles", registry.OpCaptionBurn, "veed", ""},
		{"segmind", "videoToVideo", "veed-subtitles", registry.OpCaptionBurn, "veed", ""},
		{"fal", "video-to-video", "fal-ai/auto-caption", registry.OpCaptionBurn, "fal-ai/auto-caption", ""},
		{"fal", "vision", "fal-ai/florence-2-large/caption", registry.OpUnderstandImg, "fal-ai/florence-2-large/caption", ""},
		{"fal", "text-to-audio", "fal-ai/minimax-music/v2", registry.OpMusicGenerate, "minimax-music-2.0", ""},

		// Others keep the id as family.
		{"fal", "training", "fal-ai/some-trainer", registry.OpOther, "fal-ai/some-trainer", ""},
		{"fal", "", "Acme/Mystery", registry.OpOther, "acme/mystery", ""},
		{"fal", "text-to-video", "fal-ai/kling-video/v2.1/master/text-to-video", registry.OpVideoT2V, "kling-2.1", "master"},
		{"fal", "text-to-video", "fal-ai/wan/v2.2-a14b/text-to-video", registry.OpVideoT2V, "wan-2.2", ""},
		{"segmind", "imageToVideo", "wan2.7-i2v", registry.OpVideoI2V, "wan-2.7", ""},
	}
	for _, c := range cases {
		op, fam, variant := Classify(c.provider, c.category, c.id, "")
		if c.op != "" && op != c.op {
			t.Errorf("%s %q: op = %q, want %q", c.provider, c.id, op, c.op)
		}
		if fam != c.family {
			t.Errorf("%s %q: family = %q, want %q", c.provider, c.id, fam, c.family)
		}
		if variant != c.variant {
			t.Errorf("%s %q: variant = %q, want %q", c.provider, c.id, variant, c.variant)
		}
	}
}
