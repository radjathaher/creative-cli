package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// agentGuide is shown by `creative --help` and `creative guide`. It is written
// for a consuming AI agent: it explains the mental model, the verb catalog, the
// credentials, the machine-readable output contract, and — most usefully — the
// DAG pipeline recipes that chain verbs into finished creatives.
const agentGuide = `creative — one binary to make, transform, and analyze media, then hand off to publishing.

Designed for AGENT CONSUMPTION. Non-interactive: every input is a flag, every
result is JSON on stdout. There is no prompt, menu, or confirmation. Progress and
errors go to stderr; stdout is pure JSON so you can pipe it into jq.

MENTAL MODEL
  Pick a verb by the ASSET you want out. Providers are an implementation detail
  selected by a smart --provider default you can override.

    MAKE                              TRANSFORM                    ANALYZE
    creative image    text->image     creative upscale  video->    creative transcribe audio/video->text
    creative video    text/ref->video creative caption  burn text  creative analyze    video->brief
    creative speech   text->voiceover creative dub      re-language
    creative music    text->music
    creative sfx      text->sound fx
    creative voice    clone/design

VERB CATALOG (default provider · required credential)
  image      codex-lb / OpenAI         CODEX_LB_API_KEY / OPENAI_API_KEY; add --image REF for img2img
  video      Segmind Seedance          SEGMIND_API_KEY     default; --provider fal selects Gemini Omni Flash 1.1 (FAL_KEY)
  speech     ElevenLabs                ELEVENLABS_API_KEY  --text --voice-id --model
  music      ElevenLabs                ELEVENLABS_API_KEY  --prompt (--duration-seconds)
  sfx        ElevenLabs                ELEVENLABS_API_KEY  --prompt (--duration-seconds)
  voice      ElevenLabs                ELEVENLABS_API_KEY  --audio sample(s) --name (clone) | --prompt (design)
  upscale    fal (bytedance)           FAL_KEY             INPUT; --model bytedance|topaz|flashvsr|seedvr --target 720p|1080p|2k|4k
  caption    fal->VEED (default)       FAL_KEY | ZAPCAP_*  INPUT; --provider veed|zapcap
  dub        ElevenLabs                ELEVENLABS_API_KEY  INPUT --language <iso>
  transcribe OpenAI Whisper            OPENAI_API_KEY      INPUT; --format text|srt|vtt|json|verbose-json
  analyze    fal->OpenRouter->Gemini   FAL_KEY             INPUT (video/YouTube url) -> recreation brief

CREDENTIALS  (resolved: env var first, then /run/secrets/<NAME>)
  OPENAI_API_KEY  CODEX_LB_API_KEY  SEGMIND_API_KEY  ELEVENLABS_API_KEY  FAL_KEY  ZAPCAP_API_KEY

IMAGE BACKENDS
  Set CODEX_LB_BASE_URL (including /v1) + CODEX_LB_API_KEY to prefer codex-lb.
  Partial Codex configuration is an error. With neither, use OPENAI_API_KEY and
  OPENAI_BASE_URL (default https://api.openai.com/v1; alias OPENAI_API_URL).
  CODEX_LB_EXHAUSTION_CODES explicitly allowlists structured pool errors that
  permit OpenAI fallback. Never retry another backend on timeout/network loss.
  Missing key error format: "<NAME> missing; export it or provide /run/secrets/<NAME>".

OUTPUT CONTRACT
  stdout: one JSON object (the "envelope"): {provider, endpoint, request_id, model,
          input, output, usage, estimated_cost, elapsed_seconds, out}. --pretty to
          indent, --raw to emit the untouched provider payload instead.
  stderr: human progress (upload / queued / polling / done in Xs).
  Binary assets (image/audio/video) require --out. Text results print to stdout
  (inside output) or write to --out.
  Async verbs (video, upscale, caption, dub, analyze) accept
  --poll-interval-secs, --max-wait-secs, and --no-wait (submit only, returns request_id).

DAG PIPELINE RECIPES (chain verbs; feed one --out into the next input)
  Static ad from a product photo:
    creative image  --prompt "matte black bottle on wet concrete, softbox" --out hero.png

  Talking-avatar UGC ad (image -> video -> voiceover -> burned captions):
    creative image  --prompt "founder headshot, studio light" --out face.png
    creative video  --prompt "founder talks to camera, subtle nod" --image face.png --out talk.mp4
    creative speech --text "This changed how we ship." --voice-id <id> --out vo.mp3
    creative transcribe vo.mp3 --format srt --out vo.srt
    creative caption talk.mp4 --provider veed --out ad.mp4
    # then: mux vo.mp3 + burn timing with ffmpeg, normalize, hand to meta-ads

  Reference-driven video, then finish to 4K social:
    creative video   --prompt "slow dolly across the product" --image hero.png --out clip.mp4
    creative upscale clip.mp4 --target 4k --out clip_4k.mp4
    creative caption clip_4k.mp4 --out final.mp4

  Low-cost Gemini Omni Flash draft with synchronized audio:
    creative video --provider fal --prompt "handheld product demo" --resolution 360p --duration-seconds 3 --out draft.mp4

  Study a viral reference, then recreate it:
    creative analyze https://youtube.com/watch?v=... --out brief.json
    # use the brief as the --prompt basis for creative video

  Sound design bed:
    creative music --prompt "upbeat driving synth, 120bpm" --out track.mp3
    creative sfx   --prompt "whoosh transition" --out whoosh.mp3

OUT OF SCOPE (by design): local ffmpeg stitching/normalization and publishing.
  Assemble with system ffmpeg (H.264 yuv420p + AAC + +faststart for Meta), then
  upload via the meta-ads CLI (paid) or post organically.

Run 'creative <verb> --help' for a verb's exact flags.`

func init() {
	register(func(root *cobra.Command) {
		root.AddCommand(&cobra.Command{
			Use:   "guide",
			Short: "Print the full agent usage guide (verbs, credentials, output contract, pipeline recipes).",
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Println(agentGuide)
				return nil
			},
		})
	})
}
