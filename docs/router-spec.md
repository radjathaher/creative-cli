# creative v1.0: generated asset router

Status: approved for P1 by the owner on 2026-10-19 (P1 spends no money and works on the `feat/router-v1` branch). P2 and later each need a separate go. The claims in this spec carry source tags, and the final section lists their verification status.

## Goal

Turn `creative` into one command-line router for every asset type the game and marketing pipeline needs: image, video, voice, music, SFX, 3D, upscaling, captions, and reading media back as text. Model parameters and prices are generated from provider catalogs and OpenAPI specs, so adding a model costs a sync rather than new code. The router never picks a provider on its own. For each job it shows every route ranked by price, and the agent, or the person, chooses one.

## Decisions

| Topic | Decision |
|---|---|
| Architecture | Generated only. The registry is synced from provider catalogs, specs, and price APIs. |
| Command shape | The op is the verb, for example `video.t2v`. `--family` names the model and `--provider` names the route. Shared flags work on every route, and `--param` reaches provider-specific fields. |
| Price rule | The direct provider, charging cost, is the baseline. An aggregator is used only where it is cheaper. fal is not a default; it is used where it is the cheapest or the only route. |
| Routing | Always explicit. When an op and family have two or more routes and no `--provider` is given, the command fails and lists the routes with their prices. |
| Fallback | None. That includes today's Codex-LB to OpenAI fallback, which becomes two separate routes. A failed job reports the error and the other routes, and the agent asks the person what to do. |
| Surface | Image and video show an allowlist of families. Voice, music, SFX, 3D, understanding, and STT show every route. `creative models --all` lists everything. |
| Recipes | None. Agents chain the verbs themselves. |
| Read side | Media understanding, plus a local history of jobs and assets. |
| Compatibility | v1.0 may break the old verbs and the envelope, but **every v0.2.4 capability and flag survives** (see the mapping below). |
| Freshness | The registry snapshot lives in the repo and is embedded in the binary. A daily CI sync opens a pull request when something changes. `creative sync` refreshes prices and the catalog locally. Data older than 7 days counts as stale, and the agent then offers a sync. The CLI never prompts. |
| Secrets | The owner adds keys to Infisical. A provider without a key is shown in a quote as `no_key` and is never hidden silently. |
| Nix | Release tarball. The `alif/nix` change goes on a branch for owner review. |

## Allowlist (image and video)

There are two image tiers, matching how the owner works. For **everyday images**, gpt-image-2 through Codex-LB is good enough, and it adds no marginal cost. For **use cases that need the highest quality**, use gpt-image-2.5. Flare is the fast variant with legible in-image text. Sunburst is for precise editing and maximum quality. The agent picks the variant to suit the need, and `quote` ranks the paid routes for whichever variant it picks.

| Family | Routes, cheapest first | Note |
|---|---|---|
| gpt-image-2 | Codex-LB (pool, $0 marginal) · OpenAI · KIE (flat $0.03 at 1K) · OpenRouter · fal · Segmind | Codex-LB is confirmed working in v0.2.4. |
| gpt-image-2.5 (flare, sunburst) | KIE (flat $0.03 at 1K, $0.05 at 2K, $0.08 at 4K) · OpenAI (about $0.06 at medium, $0.21 at high) · OpenRouter (OpenAI's rate) · Segmind (+25%) · fal | Codex-LB does not serve 2.5. The org does have 2.5 access: `/v1/models` returns 200, and the game pipeline used Sunburst through curl. The HTTP 400 seen on 2026-10-19 happened because the fleet wrapper in `alif/nix` `modules/agents/shims.nix` always exports Codex-LB settings, so the "direct" test still went to Codex-LB. In v1.0, Codex-LB and OpenAI are separate routes chosen by `--provider`. The owner treats KIE output as full quality. **Never use gpt-image for realistic people.** That rule has been owner policy since 2026-10-07. |
| seedance-2.5 | **Standard route: Segmind**, used for every Seedance run, including every run with video references (STREET01, POD01b, FGBUG07) and every persona clip. Other routes: OpenRouter ($0.103/s at 480p, $0.231/s at 720p, the official price, no 1080p) · Segmind ($0.1065, $0.239, $0.588, the cheapest 1080p route) · KIE ($0.14, $0.315, $0.79) · fal ($0.206, $0.462, $1.137) | Segmind's per-token rates are on its model pricing page, and the catalog API publishes only an average, so the sync has to read that page. |
| gemini-omni-flash-1.1 | **With video references** (the Omni podcast takes ROT29, ROT30, and POD02): fal r2v ($0.10/s, up to 3 videos of 3 s or less, up to 10 images; the route those runs used, because Segmind's Omni has no video-reference field) · KIE (flat $0.84 per clip, `video_list`, plus `character_ids` and `audio_ids` presets). **Without video references:** Segmind `gemini-omni-1.1` ($0.042/s at 360p, $0.127 at 720p, $0.19 at 1080p) · KIE (flat $0.42 for a 6 s clip) · Google ($0.10/s) · fal ($0.10/s) | Video-reference support comes from the 2026-10-19 schemas. Segmind `gemini-omni-1.1` has no video references. Segmind `gemini-omni-flash` takes one beta "restyle" video, which is not reference-to-video. With video references, fal is cheaper for takes under 8.4 s, which covers the 3–9 s production takes, and KIE is cheaper only for full 10 s takes. KIE's character presets may solve outfit drift between takes and deserve one test. |

Prices are list prices from the 2026-10-19 catalog pull and change over time. The CLI always reads them from the registry.

## Providers

```
 ADAPTER        KEY                           STATUS        USED FOR
 ─────────────  ────────────────────────────  ────────────  ──────────────────────────────────────────────
 codex-lb       CODEX_LB_BASE_URL + _API_KEY  have          gpt-image through the pool
 openai         OPENAI_API_KEY                have          gpt-image direct, STT, TTS
 kie            KIE_API_KEY                   have, $0 bal  Omni Flash, Suno, cheap 11labs TTS, image upscale, gpt-image
 openrouter     OPENROUTER_API_KEY            MISSING       Seedance 2.5, video/image understanding, LLM text
 elevenlabs     ELEVENLABS_API_KEY            have          clone, design, change, dub, music, SFX, Scribe STT
 fal            FAL_KEY                       have          video upscale, captions (VEED), 3D, voice/SFX/music extras
 google         GEMINI_API_KEY                MISSING       Omni Flash direct (2nd route), Gemini STT/TTS, Lyria
 segmind        SEGMIND_API_KEY               have          Seedance 2.5 (production; ordered refs, own upload endpoint), Omni without video refs, VEED subtitles, 11labs routes
 zapcap         ZAPCAP_API_KEY                have?         styled caption templates (kept from v0.2.4)
 3d direct      TRIPO_API_KEY / MESHY_API_KEY MISSING       optional only: the game pipeline already runs Tripo + Meshy on fal in USD
```

fal covers the game 3D pipeline today, with Tripo and Meshy priced in USD on fal. The exact per-mesh and rigging prices conflict between sources (see V5 and V6 below). fal is also the production route for Topaz video upscaling at a base rate of $0.01/s (V3), which beats Segmind's $0.025/s. Topaz doubles the price at 50 fps or more according to the Outline doc, and the price model must account for that.

## Every v0.2.4 verb, mapped

| v0.2.4 verb | Flags that must keep working | v1.0 op(s) |
|---|---|---|
| `image` | `--image` refs, `--background`, `--quality`, `--size`, `--n`, `--output-format`, `--timeout` | `image.t2i`, `image.edit` |
| `video` | `--first-frame`, `--last-frame`, `--image/--video/--audio` refs, `--seed`, `--no-generate-audio`, `--resolution`, `--aspect-ratio`, `--duration-seconds` | `video.t2v`, `video.i2v`, `video.flf2v`, `video.r2v`, `video.edit` |
| `upscale` | bytedance, topaz, flashvsr, and seedvr models; `--target` from 720p to 4k; `--fps` from 16 to 60; topaz factor, model, and recover_detail; bytedance tier, preset, and fidelity | `video.upscale` (and the new `image.upscale`) |
| `caption` | VEED `--font`, `--font-color`, `--font-weight`, `--highlight-color`, `--position`, `--shadow`, `--preset`, `--language`; ZapCap `--template-id` | `caption.burn` |
| `speech` | `--voice-id`, `--model`, `--output-format` | `voice.tts` |
| `voice` | clone from `--audio` samples plus `--name`; design from a prompt | `voice.clone`, `voice.design` |
| `dub` | `--language` | `voice.dub` |
| `music` | `--prompt`, `--duration-seconds`, `--output-format` | `music.generate` |
| `sfx` | `--prompt`, `--duration-seconds` | `sfx.generate` |
| `transcribe` | `--format` text, json, srt, vtt, or verbose_json; `--language`, `--prompt`, `--temperature`, `--translate` | `stt` |
| `analyze` | video file or YouTube URL to a brief; `--prompt`, `--temperature`, `--max-output-tokens` | `understand.video` |

Shared flags map onto each route's provider fields through the route's `param_map`. Provider-only flags, such as the topaz and VEED options, move to `--param key=value` and are validated against the route schema. The guide lists the old-to-new mapping for each flag. The stale defaults also change: `whisper-1` becomes gpt-transcribe or Scribe, and `gemini-2.5-flash` becomes Gemini 3.8 Flash. Like everything else, these are choices the agent makes from a quote.

New ops in v1.0 are `voice.change`, `voice.isolate`, `music.stems`, `music.extend`, `sfx.from-video`, `image.upscale`, `image.bg-remove`, `3d.t2mesh`, `3d.i2mesh`, `3d.texture`, `3d.rig`, `3d.animate`, `3d.convert` (glb, fbx, usdz), `understand.image`, `understand.audio`, and `text.chat`.

## A day in the life

These examples follow the production workflows in Outline (Growth & Marketing, then Workflows, then Runs & Records). Real ad runs have cost $4.60–8.30 each, and 1–3 takes per run were rejected.

**Podcast ad, the ROT29 and POD02 shape, about $8 with a $12 run cap**

```
 (agent vision: seed photo → JSON, identity changed on purpose)                           $0
 video.t2v  --family seedance-2.5 --provider segmind --res 480p --duration 4 --aspect 9:16
            --no-generate-audio --run ROT31 --no-wait                                     ~$0.43 persona clip
 (ffmpeg: eyes-open frame, ~15 s muted performance reference)                             $0
 video.r2v  --family gemini-omni-flash-1.1 --provider fal --res 720p --duration 3–9
            --image persona.png --video perf.mp4 --run ROT31 --max-run-cost 12 --no-wait ×6–14 in parallel
 jobs list --run ROT31 · jobs fetch <id> · reject a take (burned subtitles / dropped words) ~$0.10/s per take
 video.upscale --provider fal --model topaz --param model="Gaia HQ" --factor 1.5 --fps 50
               --recover-detail 0.2   (only the seconds the edit uses)                    ~$0.54–1.06
 stt --provider elevenlabs --timestamps word   (cross-check: fal whisper)                 cents
 (local: Kinetic Serif finisher, ffmpeg Meta export, rclone to Nextcloud, meta-ads)       $0
 jobs list --run ROT31 --cost                                                             run total
```

**Seedance multi-shot take, the STREET01 and FGBUG shape**

```
 video.r2v --family seedance-2.5 --provider segmind --res 480p --duration 16–30
           --image @1 --image @2 --video @1 --audio @1   (order kept as @Image n / @Video n)
           --prompt-file shot.json --run STREET02 --no-wait                ~$1.70 (16 s) · ~$3.20 (30 s)
 Segmind failure that was not billed → status failed_unbilled → resubmit on the same route
```

**One 3D game character, about $1 on fal (no new keys)**

```
 image.t2i --family gpt-image-2.5 --variant sunburst  front / side / back (not a realistic person)  ~$0.03–0.06 each
 3d.i2mesh --provider fal --family tripo-h3.1 --image front --image side --image back (in that order) $0.60 multiview
 3d.rig --provider fal --family meshy --param clips=walk,idle                                         $0.20 + $0.12/clip
 sfx.generate --provider fal --family eleven-sfx-v2 "armor footsteps on stone"                        ~$0.002/s
 (local: Blender, gltf-transform)
```

## Production requirements (from the Outline workflows and runs)

Today's real workflows break unless all of these hold:

1. **Resume and concurrency.** `--no-wait` exists on every async op, and the provider job ID is written to the job store *before* polling starts. `jobs get` and `jobs fetch` resume any job, and the CLI refuses to resubmit a job that is still running or already done. The job store is safe for 6–14 `creative` processes writing at once, either through one file per job or through file locking. Long foreground calls were killed on macOS before the request ID printed, which is why most runs fell back to curl.
2. **A capability-aware quote.** A route appears in a quote only if it accepts the inputs given: video references, ordered references, the duration range (Omni up to 10 s, Seedance 4–30 s), resolution, and aspect ratio. Out-of-range input fails before submit.
3. **Ordered references.** `--image`, `--video`, and `--audio` keep their order, so `@Image n` and `@Video n` in the prompt stay valid. Uploads go to each provider's own storage, Segmind `upload-asset` among them, and the uploaded URLs are recorded. `--first-frame` combined with `--image` is rejected.
4. **STT word timestamps.** `stt --timestamps word`, routed to ElevenLabs Scribe, fal Whisper, and gpt-4o-transcribe.
5. **Real Topaz flags.** `--model`, `--factor`, `--fps`, and `--recover-detail` are first-class flags. The price includes the fps tier, which doubles at 50 fps or more.
6. **Job statuses** `queued`, `running`, `done`, `failed_unbilled`, `failed_billed`, `blocked_safety`, and `uncertain`. When the provider reports a billed cost, such as Segmind's, it is stored as `cost.actual`.
7. **Run grouping.** `--run <name>` tags jobs, `--max-run-cost USD` caps the total across a run, and `jobs list --run X --cost` sums it. Combined with `--prompt-file`, each job record stores a hash of its prompt.
8. **Policy guards in the guide.** No gpt-image for realistic people, and no AI music or SFX in ads, per the SFX + Music playbook. Both remain fine for the game.
9. **Wait limits.** The default maximum wait is 20 minutes or more, because Seedance takes of 16–30 s run for 7–12 minutes.

## System map

```
                        ┌──────────────── creative (Go binary) ─────────────────┐
  agent / human ──────► │ ops · models · quote · describe · <op> · jobs · sync  │
                        └───────┬──────────────────┬──────────────────┬─────────┘
                                ▼                  ▼                  ▼
                       ┌────────────────┐  ┌──────────────┐  ┌────────────────────┐
                       │ registry       │  │ job engine   │  │ job store          │
                       │ embedded snap  │  │ upload →     │  │ ~/.local/share/    │
                       │ + local cache  │  │ submit →     │  │ creative/jobs.jsonl│
                       │ + allowlist    │  │ poll →       │  └────────────────────┘
                       └───────┬────────┘  │ download     │
               sync ◄──────────┘           └──────┬───────┘
                 │                                │ one adapter per transport
                 ▼                                ▼
  fal models+OAS+pricing · OpenRouter models   codex-lb · openai · kie · openrouter · elevenlabs
  +endpoints · KIE llms.txt+OAS+pricing API ·  fal · google · segmind · zapcap · tripo/meshy
  Segmind list · ElevenLabs/OpenAI OAS
  hand price tables (price_source:"page", checked_at) for 11labs, OpenAI, Google, Tripo, Meshy, ZapCap
```

## Data model

```
 Route  (one provider × one model endpoint)
 ├─ id · provider · transport · op · family · tier
 ├─ schema       JSON Schema of the provider input
 ├─ param_map    shared flag → provider field
 ├─ price        { unit, value_usd, source: api|avg|page, as_of }
 ├─ verified     true when the route came from the CI snapshot; false when it came only from a local sync
 └─ allowlisted · deprecated

 Job  (one line per run in jobs.jsonl)
 ├─ job_id · route_id · provider_job_id · status
 ├─ request (secrets removed) · cost { estimated, actual? } · prices_as_of
 └─ outputs [ { path, url, mime, url_expires_at? } ] · created_at · elapsed_seconds
```

Envelope v1, printed as one JSON object on stdout: `{op, family, provider, route_id, model_id, job_id, provider_job_id, status, cost:{estimated, actual}, prices_as_of, verified, outputs:[{path,url,mime}], elapsed_seconds}`.

Two safety flags apply to every run. `--dry-run` prints the resolved payload and the quote, and spends nothing. `--max-cost USD` refuses to submit a job whose estimate is above the limit.

## Phased plan

| Phase | Scope | Proof before calling it done |
|---|---|---|
| P1 registry and read-only | `internal/registry` (types, embed, overlay, allowlist); `internal/sync` covering fal, OpenRouter, KIE, Segmind, the ElevenLabs and OpenAI specs, and the hand price tables; `ops`, `models`, `quote`, `describe`, `sync` | `go test ./...` passes. A sync builds the registry. Quote prices match the raw catalogs for 10 sampled routes. No generation calls. |
| P2 production lanes | The job engine and job store (resume, concurrency, run cap, statuses), envelope v1, `--dry-run`, `--max-cost`. Adapters, in production order: Segmind Seedance 2.5 (ordered references, upload), fal Omni r2v, fal Topaz, ElevenLabs Scribe and the STT routes with word timestamps, and Codex-LB plus OpenAI images including 2.5 | Every production requirement above passes against recorded fixtures. One real job per lane, about $3 in total, and one 6-take parallel run that is killed and then resumed. |
| P3 game and cheaper routes | fal 3D (Tripo, Meshy rigging and clips), fal SFX, the KIE adapter (gpt-image-2.5 and Omni with `video_list` and character presets), the OpenRouter adapter (Seedance) | One mesh, one rig, and one paid test per new route, about $3. A route is ranked as equal in `quote` only after its test passes. |
| P3b everything else | All voice ops, music, the remaining SFX ops, understanding, captions (VEED and ZapCap), and image upscaling and background removal. The owner will use these but has not yet designed how, so they get the same generated surface and are built after the production lanes | One real job per op |
| P4 ship | Delete the old verbs and `internal/provider/*`; rewrite the guide and README, including the flag mapping; add `.github/workflows/sync.yml`; tag v1.0.0; bump `alif/nix` on a branch | CI passes. The release tarballs exist. `nix build` passes on the branch. `creative quote` works from the Nix build. |

Workers: at most 2 clx-01 subagents, and only after go.

## Blast radius

- **Add:** `internal/registry/`, `internal/sync/`, `internal/adapter/*/`, `internal/jobs/`, `registry/{routes.json,allowlist.json,overrides.json,prices-manual.json}`, `cmd/{ops,models,quote,describe,sync,jobs,run}.go`, and `.github/workflows/sync.yml`.
- **Reuse:** `internal/core/` for secrets, HTTP, download, and MIME. Also reuse `internal/fal/client.go` (queue and upload), the Codex-LB backend checks in `internal/provider/openai/image-backend.go` without its fallback, and the request-building logic of every current provider package, moved into the adapters.
- **Delete in P4:** `cmd/{image,video,speech,music,sfx,voice,upscale,caption,dub,transcribe,analyze}.go` and `internal/provider/*`.
- **Outside the repo:** `alif/nix/pkgs/by-name/cr/creative-cli/package.nix` (version, the four hashes, and the usage text), on a branch only.

## Risks

- **A wrong `param_map` costs money.** For example, a 1080p 10-second job could go out instead of a cheap draft. The guards are `--dry-run`, `--max-cost`, and a price estimate on every run.
- **KIE is an unofficial reseller.** It can change without notice, and its results expire after 14 days. The guards are a mandatory `--out` download and `url_expires_at` in the job store.
- **Local sync can add untested routes.** They are marked `verified:false`, and errors from them name the source and the date.
- **Price units are mixed:** seconds, tokens, megapixels, credits, and requests. The raw unit is kept, a normalized estimate is shown per job, and an unconvertible price is shown as unknown, never as zero.
- **No provider may be dropped because its price is unknown.** When a catalog API publishes only an average, as Segmind does, or no price at all, the sync reads that provider's per-model pricing page. A route whose price still cannot be read stays in every quote, labelled `price_source: avg|unknown`. This rule exists because an early version of this research left Segmind out on exactly these grounds, even though Segmind turned out to be the cheapest route for Seedance 2.5 at 1080p, Omni at 360p, and Topaz upscaling.
- **Some catalogs have placeholder prices.** fal lists gpt-image-2.5 and nano-banana-2.1 at 1 "unit", and Tripo on fal is priced in credits. These show as unknown.

## Open questions

1. **The KIE key.** The devbox key `…d4e0` shows 0 credits, while the corporate dashboard shows 202 credits. Replace the key in Infisical with one from the corporate account. This blocks the KIE gpt-image-2.5 and KIE Omni tests.
2. **`OPENROUTER_API_KEY`.** It is optional. It only adds a Seedance route about 3% cheaper than Segmind, plus understanding. `GEMINI_API_KEY`, `TRIPO_API_KEY`, and `MESHY_API_KEY` are optional too.
3. **The fleet wrapper in `alif/nix` `modules/agents/shims.nix`.** Once v1.0 ships, it should only export credentials, not imply a route. That change belongs on the same `alif/nix` branch as the package bump.
4. **Captions.** The Outline workflows now finish captions locally with Kinetic Serif, outside the CLI, so no local caption route is needed in `creative`. VEED and ZapCap stay as paid routes.
5. **Codex-LB to OpenAI fallback.** It stays removed. Codex-LB and OpenAI are separate routes, and Codex-LB cannot serve 2.5 anyway.

## Sources and verification (audit 2026-10-19)

Source tags: **API** is a live provider API call made on 2026-10-19. **PAGE** is a provider pricing page read on 2026-10-19. **RUN** is an Outline run record. **DOC** is an Outline workflow doc. **3RD** is a third-party site. **ASSUMED** means nobody has checked it.

| # | Claim | Source | Status |
|---|---|---|---|
| V1 | Seedance 2.5 on Segmind costs $10.97 per 1M tokens, which is $0.1065/s at 480p, $0.239/s at 720p, and $0.588/s at 1080p | PAGE segmind.com/models/seedance-2.5/pricing | verified (list price) |
| V2 | Seedance 2.5 on OpenRouter costs $10.70 per 1M video tokens and offers only 480p and 720p | API openrouter.ai/api/v1/videos/models | verified; the per-second figure uses ByteDance's token formula (3RD eastprompt) |
| V3 | Topaz on fal has a base rate of $0.01/s | API fal /v1/models/pricing | verified base rate; the 2x price at 50 fps or more comes from DOC only |
| V4 | Omni 1.1 reference-to-video on fal costs $0.10/s at 720p | RUN ROT29, ROT30, POD02 tables | **CONFLICT**: the fal pricing API gives a base of $0.03/s with no resolution multiplier shown, and the run tables may be estimates. Settle it with the fal invoice or a P2 test job |
| V5 | Meshy rigging on fal costs $0.20 plus $0.12 per clip | DOC (game pipeline) | **CONFLICT**: the fal API lists `meshy/rigging` at $0.80 per generation and `rigging/multi-animation` at $0.08 |
| V6 | Tripo on fal costs $0.30 for H3.1, $0.60 for multiview, and $0.50 for P1 | DOC (game pipeline) | **UNVERIFIED**: the fal API prices Tripo at $0.01 per credit, and the number of credits per operation is unknown |
| V7 | Omni 1.1 on fal accepts up to 3 reference videos, each 3 s or shorter, and up to 10 images | API fal OpenAPI schema | verified |
| V8 | Segmind `gemini-omni-1.1` takes no reference videos | API Segmind catalog `parameters_schema` | verified |
| V9 | KIE Omni 1.1 costs $0.84 per clip with video input and $0.42 per 6 s clip without it | API KIE `model-pricing/page` | verified (list price); not tested because of the key |
| V10 | KIE gpt-image-2 and 2.5 cost $0.03 at 1K, $0.05 at 2K, and $0.08 at 4K | API KIE `model-pricing/page` | verified (list price); the owner accepts KIE quality |
| V11 | The OpenAI org has gpt-image-2.5 access | API `GET /v1/models/gpt-image-2.5-*` returns 200 | verified; no paid call has been made through the CLI yet |
| V12 | The fleet wrapper always exports Codex-LB settings | FILE `/etc/profiles/per-user/root/bin/creative`, from `alif/nix` `shims.nix` | verified |
| V13 | Seedance 2.5 with video references always goes through Segmind | RUN STREET01, POD01b, FGBUG07 | verified |
| V14 | Real ad runs cost $4.60–8.30, and Seedance takes of 16–30 s run for 7–12 minutes | RUN STREET01 ($4.62), ROT29 ($8.31); DOC for the timing | verified for cost, DOC only for timing |
| V15 | gpt-image is banned for realistic people, and AI music and SFX are banned in ads | DOC (owner policy 2026-10-07, SFX + Music playbook) | verified in Outline |
| V16 | The devbox KIE key has 0 credits while the corporate account has 202 | API `/api/v1/chat/credit`, plus an owner screenshot | verified |
| V17 | OpenAI gpt-image-2.5 costs about $0.06 at medium quality | DOC (game pipeline curl runs) | unverified; OpenAI publishes no per-image table for 2.5 |

