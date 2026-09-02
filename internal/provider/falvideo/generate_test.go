package falvideo

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radjathaher/creative-cli/internal/fal"
)

func TestBuildRequestSelectsEndpointFromInputs(t *testing.T) {
	client := &fal.Client{}
	base := Options{
		Prompt:        "test",
		Model:         "omni-1.1-flash",
		Duration:      5,
		Resolution:    "720p",
		AspectRatio:   "16:9",
		GenerateAudio: true,
		NoWait:        true,
	}
	tests := []struct {
		name     string
		mutate   func(*Options)
		endpoint string
		kind     string
		field    string
	}{
		{name: "text", mutate: func(*Options) {}, endpoint: omniBaseEndpoint + "/text-to-video", kind: "prompt"},
		{name: "first frame", mutate: func(o *Options) { o.FirstFrame = "https://example.com/start.png" }, endpoint: omniBaseEndpoint + "/image-to-video", kind: "image", field: "image_url"},
		{name: "references", mutate: func(o *Options) { o.Videos = []string{"https://example.com/ref.mp4"} }, endpoint: omniBaseEndpoint + "/reference-to-video", kind: "video", field: "reference_video_urls"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := base
			test.mutate(&opts)
			endpoint, body, kind, err := buildRequest(client, opts)
			if err != nil {
				t.Fatal(err)
			}
			if endpoint != test.endpoint || kind != test.kind {
				t.Fatalf("got endpoint=%q kind=%q, want endpoint=%q kind=%q", endpoint, kind, test.endpoint, test.kind)
			}
			if test.field != "" && body[test.field] == nil {
				t.Fatalf("payload missing %q", test.field)
			}
		})
	}
}

func TestGenerateTextNoWait(t *testing.T) {
	var requestPath string
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &requestBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"request_id":"req-123"}`)
	}))
	defer server.Close()
	t.Setenv("FAL_KEY", "test-key")
	t.Setenv("FAL_QUEUE_URL", server.URL)

	env, _, err := Generate(Options{
		Prompt:           "A lighthouse at dusk",
		Model:            "omni-1.1-flash",
		Duration:         3,
		Resolution:       "360p",
		AspectRatio:      "16:9",
		GenerateAudio:    true,
		NoWait:           true,
		PollIntervalSecs: 1,
		MaxWaitSecs:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if requestPath != "/"+omniBaseEndpoint+"/text-to-video" {
		t.Fatalf("request path = %q", requestPath)
	}
	if requestBody["resolution"] != "360p" {
		t.Fatalf("resolution = %#v", requestBody["resolution"])
	}
	if env.Cost == nil || *env.Cost != 0.09 {
		t.Fatalf("estimated cost = %#v", env.Cost)
	}
}

func TestValidateRejectsUnsupportedFalInputs(t *testing.T) {
	base := Options{
		Model:         "omni-1.1-flash",
		Duration:      5,
		Resolution:    "720p",
		AspectRatio:   "16:9",
		GenerateAudio: true,
		NoWait:        true,
	}
	tests := []Options{
		func() Options { o := base; o.Duration = 2; return o }(),
		func() Options { o := base; o.Resolution = "480p"; return o }(),
		func() Options { o := base; o.Audios = []string{"audio.mp3"}; return o }(),
		func() Options { o := base; o.GenerateAudio = false; return o }(),
	}
	for _, opts := range tests {
		if err := validate(opts); err == nil {
			t.Fatalf("validate accepted unsupported options: %#v", opts)
		}
	}
}
