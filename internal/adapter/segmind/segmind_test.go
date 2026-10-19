package segmind

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radjathaher/creative-cli/internal/job"
	"github.com/radjathaher/creative-cli/internal/registry"
)

// seedanceRoute mirrors the synced segmind:seedance-2.5 route.
func seedanceRoute() registry.Route {
	return registry.Route{
		ID: "segmind:seedance-2.5#video.r2v", Provider: "segmind", ModelID: "seedance-2.5", Family: "seedance-2.5", Op: registry.OpVideoR2V,
		Schema: json.RawMessage(`{"properties":{
			"prompt":{"type":"string"},"first_frame_url":{"type":"string"},"last_frame_url":{"type":"string"},
			"reference_images":{"type":"array","maxItems":30},"reference_videos":{"type":"array","maxItems":10},
			"reference_audios":{"type":"array","maxItems":10},"duration":{"type":"integer"},"resolution":{"type":"string"},
			"aspect_ratio":{"type":"string"},"generate_audio":{"type":"boolean"},"seed":{"type":"integer"},"bitrate_mode":{"type":"string"}}}`),
		ParamMap: map[string]string{"prompt": "prompt", "first_frame": "first_frame_url", "last_frame": "last_frame_url",
			"image": "reference_images", "video": "reference_videos", "audio": "reference_audios", "duration": "duration",
			"resolution": "resolution", "aspect": "aspect_ratio", "generate_audio": "generate_audio", "seed": "seed"},
	}
}

func TestPrepareKeepsReferenceOrderAndUploads(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "b.png")
	if err := os.WriteFile(local, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	var uploads int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/upload-asset" || r.Header.Get("x-api-key") != "k" || !strings.Contains(string(body), "data:image/png;base64,") {
			t.Errorf("unexpected upload %s %s", r.URL.Path, body)
		}
		uploads++
		_, _ = io.WriteString(w, `{"file_urls":["https://cdn.segmind/b.png"]}`)
	}))
	defer srv.Close()
	a := &Adapter{StorageURL: srv.URL, Key: "k"}
	seed, audio := 7, false
	req := job.Request{
		Prompt: "@Image1 meets @Image2", Images: []string{"https://x/a.png", local, "https://x/c.png"},
		Videos: []string{"https://x/v1.mp4", "https://x/v2.mp4"}, Duration: 5, Resolution: "480p", Aspect: "9:16",
		Seed: &seed, GenerateAudio: &audio, Params: map[string]any{"bitrate_mode": "high"},
	}
	payload, err := a.Prepare(context.Background(), seedanceRoute(), req, false)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(payload, &got)
	imgs := got["reference_images"].([]any)
	if uploads != 1 || len(imgs) != 3 || imgs[0] != "https://x/a.png" || imgs[1] != "https://cdn.segmind/b.png" || imgs[2] != "https://x/c.png" {
		t.Fatalf("images out of order or not uploaded: %v (uploads %d)", imgs, uploads)
	}
	if vids := got["reference_videos"].([]any); vids[0] != "https://x/v1.mp4" || vids[1] != "https://x/v2.mp4" {
		t.Errorf("videos: %v", vids)
	}
	if got["duration"] != 5.0 || got["seed"] != 7.0 || got["generate_audio"] != false || got["aspect_ratio"] != "9:16" || got["bitrate_mode"] != "high" {
		t.Errorf("scalars: %v", got)
	}
}

func TestPrepareRules(t *testing.T) {
	a := &Adapter{}
	ctx := context.Background()
	if _, err := a.Prepare(ctx, seedanceRoute(), job.Request{Prompt: "p", FirstFrame: "f.png", Images: []string{"i.png"}}, true); err == nil {
		t.Error("seedance first frame + images accepted")
	}
	if _, err := a.Prepare(ctx, seedanceRoute(), job.Request{Prompt: "p", Params: map[string]any{"nope": 1}}, true); err == nil || !strings.Contains(err.Error(), "no such field") {
		t.Errorf("unknown --param: %v", err)
	}
	p, err := a.Prepare(ctx, seedanceRoute(), job.Request{Prompt: "p", FirstFrame: "./f.png"}, true)
	if err != nil || !strings.Contains(string(p), `"first_frame_url":"upload://./f.png"`) {
		t.Errorf("dry run placeholder: %s %v", p, err)
	}
}

func TestSubmitAndPoll(t *testing.T) {
	state := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/seedance-2.5":
			_, _ = io.WriteString(w, `{"request_id":"req1","status":"QUEUED"}`)
		case strings.HasSuffix(r.URL.Path, "/status"):
			_, _ = io.WriteString(w, state["status"])
		case r.URL.Path == "/v2/requests/req1":
			_, _ = io.WriteString(w, `{"output":{"video":"https://cdn.segmind/out.mp4"},"metrics":{"cost":0.5324}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	a := &Adapter{BaseURL: srv.URL, Key: "k"}
	r := seedanceRoute()
	sub, err := a.Submit(context.Background(), r, json.RawMessage(`{"prompt":"p"}`))
	if err != nil || sub.ProviderJobID != "req1" {
		t.Fatalf("submit: %+v %v", sub, err)
	}
	rec := job.Record{JobID: "j", ProviderJobID: sub.ProviderJobID, Poll: sub.Poll}
	cases := []struct {
		body, status string
		cost         float64
	}{
		{`{"status":"QUEUED"}`, job.StatusQueued, 0},
		{`{"status":"PROCESSING"}`, job.StatusRunning, 0},
		{`{"status":"COMPLETED"}`, job.StatusDone, 0.5324},
		{`{"status":"FAILED","error":"model temporarily unavailable"}`, job.StatusFailedUnbilled, 0},
		{`{"status":"FAILED","error":"Output flagged as NSFW content"}`, job.StatusBlockedSafety, 0},
	}
	for _, c := range cases {
		state["status"] = c.body
		res, err := a.Poll(context.Background(), r, rec)
		if err != nil || res.Status != c.status {
			t.Errorf("%s: status %s err %v", c.body, res.Status, err)
			continue
		}
		if c.status == job.StatusDone {
			if len(res.Outputs) != 1 || res.Outputs[0].URL != "https://cdn.segmind/out.mp4" || res.Outputs[0].MIME != "video/mp4" {
				t.Errorf("outputs: %+v", res.Outputs)
			}
			if res.ActualUSD == nil || *res.ActualUSD != c.cost {
				t.Errorf("cost: %v", res.ActualUSD)
			}
		}
	}
}
