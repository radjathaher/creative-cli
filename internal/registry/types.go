// Package registry holds the generated catalog of routes: one provider serving
// one model endpoint for one operation. The catalog is produced by `creative
// sync` from provider catalogs, OpenAPI specs, and pricing sources, embedded in
// the binary as a snapshot, and optionally overlaid by a newer local cache.
package registry

import (
	"encoding/json"
	"time"
)

// Op names use the closed `<modality>.<op>` vocabulary from docs/router-spec.md.
// Routes whose operation cannot be classified use OpOther so that they remain
// visible rather than silently dropped.
const (
	OpImageT2I      = "image.t2i"
	OpImageEdit     = "image.edit"
	OpImageInpaint  = "image.inpaint"
	OpImageBGRemove = "image.bg-remove"
	OpImageUpscale  = "image.upscale"
	OpVideoT2V      = "video.t2v"
	OpVideoI2V      = "video.i2v"
	OpVideoFLF2V    = "video.flf2v"
	OpVideoR2V      = "video.r2v"
	OpVideoEdit     = "video.edit"
	OpVideoExtend   = "video.extend"
	OpVideoLipsync  = "video.lipsync"
	OpVideoAvatar   = "video.avatar"
	OpVideoUpscale  = "video.upscale"
	OpVoiceTTS      = "voice.tts"
	OpVoiceClone    = "voice.clone"
	OpVoiceDesign   = "voice.design"
	OpVoiceChange   = "voice.change"
	OpVoiceDub      = "voice.dub"
	OpVoiceIsolate  = "voice.isolate"
	OpMusicGenerate = "music.generate"
	OpMusicStems    = "music.stems"
	OpMusicExtend   = "music.extend"
	OpSFXGenerate   = "sfx.generate"
	OpSFXFromVideo  = "sfx.from-video"
	Op3DT2Mesh      = "3d.t2mesh"
	Op3DI2Mesh      = "3d.i2mesh"
	Op3DTexture     = "3d.texture"
	Op3DRig         = "3d.rig"
	Op3DAnimate     = "3d.animate"
	Op3DConvert     = "3d.convert"
	OpUnderstandVid = "understand.video"
	OpUnderstandImg = "understand.image"
	OpUnderstandAud = "understand.audio"
	OpSTT           = "stt"
	OpCaptionBurn   = "caption.burn"
	OpTextChat      = "text.chat"
	OpBrollSearch   = "broll.search"
	OpBrollGet      = "broll.get"
	OpOther         = "other"
)

// Ops lists every operation in display order.
var Ops = []string{
	OpImageT2I, OpImageEdit, OpImageInpaint, OpImageBGRemove, OpImageUpscale,
	OpVideoT2V, OpVideoI2V, OpVideoFLF2V, OpVideoR2V, OpVideoEdit, OpVideoExtend,
	OpVideoLipsync, OpVideoAvatar, OpVideoUpscale,
	OpVoiceTTS, OpVoiceClone, OpVoiceDesign, OpVoiceChange, OpVoiceDub, OpVoiceIsolate,
	OpMusicGenerate, OpMusicStems, OpMusicExtend, OpSFXGenerate, OpSFXFromVideo,
	Op3DT2Mesh, Op3DI2Mesh, Op3DTexture, Op3DRig, Op3DAnimate, Op3DConvert,
	OpUnderstandVid, OpUnderstandImg, OpUnderstandAud, OpSTT, OpCaptionBurn, OpTextChat,
	OpBrollSearch, OpBrollGet, OpOther,
}

// Price sources, from most to least trustworthy. A route never disappears for
// lack of a price; it carries SourceUnknown instead.
const (
	SourceAPI     = "api"     // a provider pricing API answered for this endpoint
	SourcePage    = "page"    // parsed from the provider's public pricing page
	SourceManual  = "manual"  // hand-maintained table in data/manual-prices.json
	SourceAverage = "avg"     // provider publishes only an observed average per request
	SourceUnknown = "unknown" // no price could be read
)

// Price units. Estimate multiplies ValueUSD by the job quantity in this unit.
const (
	UnitSecond     = "second"    // seconds of output media
	UnitMinute     = "minute"    // minutes of input or output media
	UnitImage      = "image"     // per output image
	UnitRequest    = "request"   // per call, regardless of size
	UnitMegapixel  = "megapixel" // per output megapixel
	UnitKChars     = "1k_chars"  // per thousand input characters
	UnitMTokens    = "1m_tokens" // per million tokens; requires a Formula
	UnitCredit     = "credit"    // provider credits with an unknown per-job count
	UnitUnknown    = "unknown"
	FormulaSeedTok = "seedance_video_tokens" // width*height*24*seconds/1024
)

// Route is one provider endpoint that performs one operation for one model family.
type Route struct {
	ID        string `json:"id"` // "<provider>:<model_id>"
	Provider  string `json:"provider"`
	ModelID   string `json:"model_id"`
	Transport string `json:"transport,omitempty"`
	Op        string `json:"op"`
	Family    string `json:"family"`
	Variant   string `json:"variant,omitempty"` // e.g. flare, sunburst, pro, fast
	Title     string `json:"title,omitempty"`

	Price Price        `json:"price"`
	Caps  Capabilities `json:"caps"`
	// Schema is the provider's input JSON Schema when the source publishes one.
	Schema json.RawMessage `json:"schema,omitempty"`
	// ParamMap maps shared flag names (prompt, image, video, duration, ...) to the
	// provider's field names, derived from the schema and data/overrides.json.
	ParamMap map[string]string `json:"param_map,omitempty"`

	Deprecated bool   `json:"deprecated,omitempty"`
	Verified   bool   `json:"verified"` // true only for routes in the CI snapshot
	SourceURL  string `json:"source_url,omitempty"`
}

// Price describes how much one job on a route costs. Tiers refine the default
// value for specific job attributes such as resolution or video input.
type Price struct {
	Unit      string      `json:"unit"`
	ValueUSD  *float64    `json:"value_usd,omitempty"` // nil when unknown
	Formula   string      `json:"formula,omitempty"`
	Tiers     []PriceTier `json:"tiers,omitempty"`
	Source    string      `json:"source"`
	SourceURL string      `json:"source_url,omitempty"`
	AsOf      string      `json:"as_of,omitempty"` // YYYY-MM-DD
	Note      string      `json:"note,omitempty"`
}

// PriceTier overrides the unit price (and optionally the unit) when every key in
// Match equals the corresponding job attribute, e.g. {"resolution":"1080p"}.
type PriceTier struct {
	Match    map[string]string `json:"match"`
	Unit     string            `json:"unit,omitempty"`
	ValueUSD float64           `json:"value_usd"`
}

// Capabilities are the input limits a route accepts. Zero values mean
// "unknown", which quote treats as permissive but flags in its output.
type Capabilities struct {
	MinDuration  int      `json:"min_duration,omitempty"`
	MaxDuration  int      `json:"max_duration,omitempty"`
	Resolutions  []string `json:"resolutions,omitempty"`
	Aspects      []string `json:"aspects,omitempty"`
	MaxImages    int      `json:"max_images,omitempty"`
	MaxVideos    int      `json:"max_videos,omitempty"`
	MaxAudios    int      `json:"max_audios,omitempty"`
	MaxVideoSecs int      `json:"max_video_secs,omitempty"`
	FirstFrame   bool     `json:"first_frame,omitempty"`
	LastFrame    bool     `json:"last_frame,omitempty"`
	NativeAudio  bool     `json:"native_audio,omitempty"`
}

// SourceInfo records the outcome of syncing one provider.
type SourceInfo struct {
	FetchedAt string `json:"fetched_at"`
	Routes    int    `json:"routes"`
	Error     string `json:"error,omitempty"`
}

// Catalog is the serialized registry snapshot.
type Catalog struct {
	GeneratedAt string                `json:"generated_at"`
	Sources     map[string]SourceInfo `json:"sources"`
	Routes      []Route               `json:"routes"`
}

// Age reports how old the catalog is; unparsable timestamps count as infinitely old.
func (c *Catalog) Age(now time.Time) time.Duration {
	t, err := time.Parse(time.RFC3339, c.GeneratedAt)
	if err != nil {
		return 1<<63 - 1
	}
	return now.Sub(t)
}

// Modality returns the part of an op before the dot ("video" for "video.r2v").
func Modality(op string) string {
	for i := 0; i < len(op); i++ {
		if op[i] == '.' {
			return op[:i]
		}
	}
	return op
}

// F builds a *float64 for literal prices.
func F(v float64) *float64 { return &v }
