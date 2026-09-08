# creative

One binary to **make, transform, and analyze** media — image, video, and audio —
then hand the finished asset off to publishing (Meta ads or organic).

Built for **agent consumption**: fully non-interactive, every input is a flag,
every result is JSON on stdout. There is no prompt, menu, or confirmation.
`stdout` is pure JSON (pipe it into `jq`); `stderr` carries human progress.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/radjathaher/creative-cli/main/scripts/install.sh | bash
```

Or build from source (Go 1.25+):

```bash
go build -o creative .
```

## Verbs

| Verb | Default provider | Credential | What it does |
|------|------------------|------------|--------------|
| `creative image` | codex-lb when configured, otherwise OpenAI | `CODEX_LB_API_KEY` / `OPENAI_API_KEY` | gpt-image-2 text→image; `--image REF` for img2img |
| `creative video` | Segmind Seedance; fal Gemini Omni Flash | `SEGMIND_API_KEY` / `FAL_KEY` | text/reference→video with provider-generated audio |
| `creative speech` | ElevenLabs | `ELEVENLABS_API_KEY` | text→voiceover |
| `creative music` | ElevenLabs | `ELEVENLABS_API_KEY` | text→music |
| `creative sfx` | ElevenLabs | `ELEVENLABS_API_KEY` | text→sound effect |
| `creative voice` | ElevenLabs | `ELEVENLABS_API_KEY` | clone / design a voice |
| `creative upscale` | fal (bytedance) | `FAL_KEY` | video→higher-resolution video |
| `creative caption` | fal→VEED | `FAL_KEY` / `ZAPCAP_API_KEY` | burn subtitles onto video |
| `creative dub` | ElevenLabs | `ELEVENLABS_API_KEY` | re-language a video |
| `creative transcribe` | OpenAI Whisper | `OPENAI_API_KEY` | audio/video→text |
| `creative analyze` | fal→Gemini | `FAL_KEY` | video→recreation brief |

Credentials resolve from the environment first, then `/run/secrets/<NAME>` — the
same convention every sibling CLI uses.

## Image backend selection

Set both `CODEX_LB_BASE_URL` and `CODEX_LB_API_KEY` to prefer your load balancer.
The base URL includes the API prefix, for example `https://codex-lb.cakrawala.ai/v1`.
Do not use the text Responses prefix `/backend-api/codex` for Images API calls.
If only one Codex setting is present, the command reports a configuration error.

Without Codex settings, images use `OPENAI_API_KEY` and `OPENAI_BASE_URL`
(default `https://api.openai.com/v1`). The existing `OPENAI_API_URL` variable
remains a lower-priority alias for the image base URL. Other verbs are unchanged.

```bash
export CODEX_LB_BASE_URL=https://codex-lb.cakrawala.ai/v1
# Supply CODEX_LB_API_KEY through your secret manager.
creative image --prompt "a red cube on white" --out cube.png
```

Runtime fallback is disabled unless `CODEX_LB_EXHAUSTION_CODES` contains an
explicit comma-separated allowlist of your server's pool-exhaustion error codes.
Only a matching structured error can select direct OpenAI, which also requires
`OPENAI_API_KEY`. Timeouts, connection loss, and other errors never trigger a
second generation: the first server might still be working. Do not allowlist
generic rate-limit or server-error codes.

Image requests have a ten-minute total timeout, including downloads and fallback.
Use `--timeout 25m` when your image backend needs more time. Backend selection is
automatic; the image command does not need a `--provider` flag.

## Output contract

`stdout` is a single JSON envelope:

```json
{"provider":"openai","endpoint":"/v1/images/generations","model":"gpt-image-2",
 "input":{"kind":"prompt","source":"..."},"output":"image",
 "elapsed_seconds":4.12,"out":"hero.png"}
```

`--pretty` indents it; `--raw` emits the untouched provider payload instead.
Async verbs (`video`, `upscale`, `caption`, `dub`, `analyze`) accept
`--poll-interval-secs`, `--max-wait-secs`, and `--no-wait`.

## Video providers

Segmind Seedance remains the default. Select fal Gemini Omni Flash 1.1 with
`--provider fal`; `--model auto` resolves to the correct provider model.

```bash
creative video --provider fal --prompt "handheld product demo" \
  --resolution 360p --duration-seconds 3 --out draft.mp4
```

Omni accepts 3–10 second output at 360p, 720p, 1080p, or 4K. With no references,
the command uses text-to-video. `--first-frame` selects image-to-video.
Repeatable `--image` and `--video` flags select multimodal reference-to-video.
Omni always generates synchronized audio and does not accept `--audio` or
`--seed`.

## Video upscaling

Topaz and FlashVSR calculate their scale factor from the input dimensions, so
`--target 1080p` means 1080p instead of a fixed 2× resize. Video upscaling
requires `ffprobe` on `PATH`; Nix installations provide it automatically.

## Pipelines

`creative --help` (or `creative guide`) prints the full agent playbook, including
DAG recipes that chain verbs into finished creatives — e.g. image → video →
voiceover → burned captions, then normalize with ffmpeg and upload via the
`meta-ads` CLI.

## Scope

Local media assembly (ffmpeg stitching/normalization) and publishing are
deliberately **out of scope** — `creative` produces the assets; you assemble with
system `ffmpeg` (H.264 `yuv420p` + AAC + `+faststart` for Meta) and publish
separately.
