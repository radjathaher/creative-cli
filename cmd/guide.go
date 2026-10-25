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

ROUTER (v0.3+, preferred): op = what you make, family = model, provider = route.
  creative keys --table                      which keys resolve + live balances (KIE, ElevenLabs)
  creative ops --table                       every operation (image.t2i, video.r2v, voice.tts, stt, 3d.i2mesh, ...)
  creative models <op> --table [--all]       families x providers for an op (image/video use an allowlist)
  creative quote <op> --family F [job flags] --table
                                             every route that ACCEPTS the inputs, cheapest first, with price
                                             source/date and USABLE (key + balance). Never runs anything.
  creative describe <route-id>               flags, limits, price, input schema of one route
  creative <op> --family F --provider P [--prompt|--prompt-file] [--image/--video/--audio ...in order]
       [--duration --res --aspect --fps --quality --seed] [--param key=value]
       [--run NAME --max-cost USD --max-run-cost USD] [--dry-run] [--no-wait] --out FILE
  creative jobs list --run NAME --table      job history + cost total; jobs get <id>; jobs fetch <id> (resume)
  creative sync                              refresh models + prices (free; ~1-2 min); prices older than 7 days
                                             are flagged stale in quotes - ask the user before syncing.

  Rules: routing is explicit (2+ routes and no --provider = error listing prices); no fallback between
  providers; jobs are saved before polling so --no-wait + jobs fetch is always safe. Owner pins:
  gpt-image-2 -> codex-lb (or openai); gpt-image-2.5 (flare|sunburst) -> openai only.
  Owner policy: never gpt-image for realistic people; no AI music/SFX in ads (fine for the game).

  Typical routes (2026-10-19 prices; always re-quote):
    image.t2i   gpt-image-2 codex-lb ($0) | gpt-image-2.5 flare/sunburst openai (~$0.014 medium, ~$0.053 high)
    video.t2v/i2v/r2v  seedance-2.5 segmind ($0.1065/s 480p, $0.239/s 720p; video refs OK)
    video.r2v   gemini-omni-flash-1.1 fal ($0.10/s 720p; up to 3 ref videos) - segmind omni has no video refs
    video.upscale INPUT --family topaz --variant precision --provider fal --fps 50
                --param model="Gaia HQ" --param upscale_factor=1.5 --param recover_detail=0.2 --param H264_output=true
    voice.tts   elevenlabs direct (--text, --param voice_id=...)   stt INPUT scribe-v2 elevenlabs (word timestamps)
    voice.dub INPUT --param language=es (elevenlabs only) · music/sfx/3d: quote first

STOCK B-ROLL (free, no AI generation)
  creative broll.search --query "old printing press" --provider pexels --table
       [--orientation portrait|landscape|square] [--min-duration 5 --max-duration 30] [--limit 20]
  creative broll.get pexels:12345 --out press.mp4
  creative broll.get 125 --provider pixabay --quality 4k --out clip.mp4
  Providers: pexels (PEXELS_API_KEY), pixabay (PIXABAY_API_KEY), pond5-pd (no key; unavailable).
  No silent provider choice or fallback. Prefixed IDs infer the provider; conflicting flags fail.
  Search: JSON array with licence/source/author URLs and cost_usd: 0; --table for human rows.
  At most one API request per search; filters may return fewer results. Limit: Pexels 80, Pixabay 200.
  Pixabay caches API responses for 24 hours under $XDG_CACHE_HOME/creative/broll (normally ~/.cache).
  Quality: sd|hd|4k|best caps the longest edge at 960|1920|4096|unlimited pixels; default hd, never 4K.
  Download: FILE + FILE.json provenance (licence, author, source/download URLs, selected dimensions,
  duration, UTC fetched_at, SHA-256, attribution, last local query if known). Never overwrites either file.
  get emits one JSON object; both commands keep normalized output with --raw.
  Free stock is not automatic ad clearance: no implied endorsement or standalone resale.
  Pond5 API returns HTTP 403; licence fields and download endpoint are unverified. No invented results.
  Pond5 help limits Public Domain Project videos to editorial use, not ads:
  https://www.pond5.com/help/en/articles/10086064-where-can-i-find-free-footage-photos-and-music
  Pexels/Pixabay: fixture-tested; live verification needs their keys.

LEGACY VERBS (v0.2, still work; prefer the router above):
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
  analyze    fal->OpenRouter->Gemini   FAL_KEY             INPUT (video/YouTube url) -> recreation brief; default google/gemini-3.8-flash

CREDENTIALS  (resolved: env var first, then /run/secrets/<NAME>)
  OPENAI_API_KEY  CODEX_LB_API_KEY  SEGMIND_API_KEY  ELEVENLABS_API_KEY  FAL_KEY  ZAPCAP_API_KEY
  PEXELS_API_KEY  PIXABAY_API_KEY

IMAGE BACKENDS
  Set CODEX_LB_BASE_URL (including /v1) + CODEX_LB_API_KEY to prefer codex-lb.
  Partial Codex configuration is an error. With neither, use OPENAI_API_KEY and
  OPENAI_BASE_URL (default https://api.openai.com/v1; alias OPENAI_API_URL).
  CODEX_LB_EXHAUSTION_CODES explicitly allowlists structured pool errors that
  permit OpenAI fallback. Never retry another backend on timeout/network loss.
  Missing key error format: "<NAME> missing; export it or provide /run/secrets/<NAME>".

OUTPUT CONTRACT
  Generation stdout: one JSON object (the "envelope"): {provider, endpoint, request_id, model,
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
