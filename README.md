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
| `creative image` | OpenAI gpt-image-2 | `OPENAI_API_KEY` | text→image; `--image REF` for img2img |
| `creative video` | Segmind Seedance | `SEGMIND_API_KEY` | text/reference→video |
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
