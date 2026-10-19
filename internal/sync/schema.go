package sync

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/radjathaher/creative-cli/internal/registry"
)

// schemaProp is the subset of a JSON Schema property that caps and param maps
// read. Nullable fields arrive as anyOf [{type: X}, {type: null}], so the
// accessors below look through anyOf for the first concrete branch.
type schemaProp struct {
	Type        string          `json:"type"`
	Enum        []any           `json:"enum"`
	Minimum     *float64        `json:"minimum"`
	Maximum     *float64        `json:"maximum"`
	MaxItems    *int            `json:"maxItems"`
	Description string          `json:"description"`
	AnyOf       []schemaProp    `json:"anyOf"`
	Items       json.RawMessage `json:"items"`
}

type objectSchema struct {
	Properties map[string]schemaProp `json:"properties"`
	Required   []string              `json:"required"`
}

func (p schemaProp) concrete() schemaProp {
	if p.Type != "" || len(p.Enum) > 0 || len(p.AnyOf) == 0 {
		return p
	}
	for _, a := range p.AnyOf {
		if a.Type != "null" {
			if a.Description == "" {
				a.Description = p.Description
			}
			return a
		}
	}
	return p
}

// Shared flag name → candidate provider field names, most specific first.
// The first candidate present in a schema wins.
var paramCandidates = []struct {
	flag   string
	fields []string
}{
	{"prompt", []string{"prompt", "text", "input_text"}},
	{"negative_prompt", []string{"negative_prompt"}},
	{"image", []string{"image_urls", "reference_image_urls", "input_image_urls", "images", "input_images", "reference_images", "image_url", "image"}},
	{"video", []string{"video_urls", "reference_video_urls", "videos", "video_url", "video"}},
	{"audio", []string{"audio_urls", "reference_audio_urls", "audio_url", "audio"}},
	{"mask", []string{"mask_url", "mask_image_url", "mask"}},
	{"mesh", []string{"model_url", "mesh_url", "input_mesh_url"}},
	{"duration", []string{"duration", "duration_seconds", "seconds", "video_duration"}},
	{"resolution", []string{"resolution", "target_resolution", "video_resolution"}},
	{"aspect", []string{"aspect_ratio"}},
	{"size", []string{"image_size", "size"}},
	{"seed", []string{"seed"}},
	{"first_frame", []string{"first_frame_url", "start_image_url", "first_frame_image", "start_frame_url"}},
	{"last_frame", []string{"end_image_url", "last_frame_url", "last_frame_image", "tail_image_url", "end_frame_url"}},
	{"fps", []string{"fps", "target_fps", "frames_per_second"}},
	{"factor", []string{"upscale_factor", "factor", "scale"}},
	{"model", []string{"model"}},
	{"n", []string{"num_images", "n", "num_outputs"}},
	{"quality", []string{"quality"}},
	{"background", []string{"background"}},
	{"output_format", []string{"output_format"}},
	{"generate_audio", []string{"generate_audio"}},
	{"language", []string{"language_code", "language"}},
	{"voice", []string{"voice_id", "voice"}},
}

var (
	reFirstFrame = regexp.MustCompile(`(?i)\b(start(ing)?|first|initial) frame`)
	reLastFrame  = regexp.MustCompile(`(?i)\b(last|end(ing)?|final) frame`)
	reMaxSecs    = regexp.MustCompile(`(?i)(?:at most|up to|maximum of|max(?:imum)?|no longer than|not exceed)\s+(\w+)\s+sec(?:ond)?s?`)
	reSecsRange  = regexp.MustCompile(`(?i)\b\d+(?:\.\d+)?\s*to\s*(\d+)(?:\.\d+)?\s*sec(?:ond)?s?`)
	reUpTo       = regexp.MustCompile(`(?i)(?:up to|at most|maximum of|max(?:imum)?)\s+(\w+)\s+(?:\w+\s+)?(?:videos?|images?|files?|audios?|clips?|urls?)\b`)
)

var wordNumbers = map[string]int{
	"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9,
	"ten": 10, "eleven": 11, "twelve": 12, "fifteen": 15, "twenty": 20, "thirty": 30, "sixty": 60,
}

func parseCount(s string) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return wordNumbers[strings.ToLower(s)]
}

// frameRole reports whether a single-image field is a first or last frame,
// judged by its name and then by its description.
func frameRole(name string, p schemaProp) string {
	for _, c := range paramCandidates {
		if c.flag == "first_frame" || c.flag == "last_frame" {
			for _, f := range c.fields {
				if f == name {
					return c.flag
				}
			}
		}
	}
	if name == "image_url" || name == "image" {
		switch {
		case reLastFrame.MatchString(p.Description):
			return "last_frame"
		case reFirstFrame.MatchString(p.Description):
			return "first_frame"
		}
	}
	return ""
}

// ParamMapFromSchema maps shared flag names (prompt, image, video, audio,
// duration, resolution, aspect, seed, first_frame, last_frame, fps, factor,
// model, ...) onto the field names a provider input schema actually uses. A
// single image field that the schema describes as the starting frame maps to
// first_frame rather than image, so ordered reference images never land in a
// frame slot by accident.
func ParamMapFromSchema(schema json.RawMessage) map[string]string {
	var s objectSchema
	if json.Unmarshal(schema, &s) != nil || len(s.Properties) == 0 {
		return nil
	}
	out := map[string]string{}
	frameFields := map[string]bool{}
	for name, p := range s.Properties {
		if role := frameRole(name, p.concrete()); role != "" {
			if _, taken := out[role]; !taken {
				out[role] = name
			}
			frameFields[name] = true
		}
	}
	for _, c := range paramCandidates {
		if _, done := out[c.flag]; done {
			continue
		}
		for _, f := range c.fields {
			if _, ok := s.Properties[f]; ok && !frameFields[f] {
				out[c.flag] = f
				break
			}
		}
	}
	return out
}

// CapsFromSchema derives input limits from a provider input schema: duration
// range, resolution and aspect enums, how many images, videos, and audios a
// request takes, the longest reference video, frame slots, and native audio.
// Limits the schema does not state stay zero, which quote treats as unknown.
func CapsFromSchema(schema json.RawMessage) registry.Capabilities {
	var caps registry.Capabilities
	var s objectSchema
	if json.Unmarshal(schema, &s) != nil {
		return caps
	}
	pm := ParamMapFromSchema(schema)
	prop := func(flag string) (schemaProp, bool) {
		name, ok := pm[flag]
		if !ok {
			return schemaProp{}, false
		}
		return s.Properties[name].concrete(), true
	}
	if p, ok := prop("duration"); ok {
		caps.MinDuration, caps.MaxDuration = numericRange(p)
	}
	if p, ok := prop("resolution"); ok {
		caps.Resolutions = enumStrings(p)
	}
	if p, ok := prop("aspect"); ok {
		caps.Aspects = enumStrings(p)
	}
	if p, ok := prop("image"); ok {
		caps.MaxImages = itemCount(p)
	}
	if p, ok := prop("video"); ok {
		caps.MaxVideos = itemCount(p)
		if m := reMaxSecs.FindStringSubmatch(p.Description); m != nil {
			caps.MaxVideoSecs = parseCount(m[1])
		} else if m := reSecsRange.FindStringSubmatch(p.Description); m != nil {
			caps.MaxVideoSecs = parseCount(m[1])
		}
	}
	if p, ok := prop("audio"); ok {
		caps.MaxAudios = itemCount(p)
	}
	_, caps.FirstFrame = pm["first_frame"]
	_, caps.LastFrame = pm["last_frame"]
	_, caps.NativeAudio = pm["generate_audio"]
	return caps
}

// itemCount is 1 for a scalar field and maxItems for an array, falling back to
// an "up to N videos" phrase in the description when maxItems is absent.
func itemCount(p schemaProp) int {
	if p.Type != "array" {
		return 1
	}
	if p.MaxItems != nil {
		return *p.MaxItems
	}
	if m := reUpTo.FindStringSubmatch(p.Description); m != nil {
		return parseCount(m[1])
	}
	return 0
}

// numericRange reads min and max from an enum of numbers or numeric strings
// ("auto", "4", ..., "30") or from minimum/maximum.
func numericRange(p schemaProp) (lo, hi int) {
	if len(p.Enum) > 0 {
		var nums []int
		for _, e := range p.Enum {
			switch v := e.(type) {
			case float64:
				nums = append(nums, int(v))
			case string:
				if n, err := strconv.ParseFloat(strings.TrimSuffix(v, "s"), 64); err == nil {
					nums = append(nums, int(n))
				}
			}
		}
		if len(nums) > 0 {
			sort.Ints(nums)
			return nums[0], nums[len(nums)-1]
		}
	}
	if p.Minimum != nil {
		lo = int(*p.Minimum)
	}
	if p.Maximum != nil {
		hi = int(*p.Maximum)
	}
	return lo, hi
}

func enumStrings(p schemaProp) []string {
	var out []string
	for _, e := range p.Enum {
		if s, ok := e.(string); ok && s != "auto" {
			out = append(out, s)
		}
	}
	return out
}
