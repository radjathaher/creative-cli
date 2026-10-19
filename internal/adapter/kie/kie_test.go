package kie

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
	"time"

	"github.com/radjathaher/creative-cli/internal/job"
	"github.com/radjathaher/creative-cli/internal/registry"
)

// omniRoute and seedanceRoute mirror the synced KIE routes and doc schemas.
func omniRoute() registry.Route {
	return registry.Route{
		ID: "kie:google/gemini-omni-flash-1-1#video.r2v", Provider: "kie", ModelID: "google/gemini-omni-flash-1-1",
		Family: "gemini-omni-flash-1.1", Op: registry.OpVideoR2V,
		Schema: json.RawMessage(`{"properties":{"prompt":{"type":"string"},"image_urls":{"type":"array"},
			"first_frame_url":{"type":"string"},"video_list":{"type":"array"},"duration":{"type":"string","enum":["4","6","8","10"]},
			"resolution":{"type":"string"},"aspect_ratio":{"type":"string"},"character_ids":{"type":"array"}}}`),
		ParamMap: map[string]string{"prompt": "prompt", "image": "image_urls", "first_frame": "first_frame_url",
			"video": "video_list", "duration": "duration", "resolution": "resolution", "aspect": "aspect_ratio"},
	}
}

func seedanceRoute() registry.Route {
	return registry.Route{
		ID: "kie:bytedance/seedance-2-5#video.i2v", Provider: "kie", ModelID: "bytedance/seedance-2-5", Family: "seedance-2.5", Op: registry.OpVideoI2V,
		Schema: json.RawMessage(`{"properties":{"prompt":{"type":"string"},"first_frame_url":{"type":"string"},"last_frame_url":{"type":"string"},
			"reference_image_urls":{"type":"array"},"reference_video_urls":{"type":"array"},"duration":{"type":"integer"},"aspect_ratio":{"type":"string"}}}`),
		ParamMap: map[string]string{"prompt": "prompt", "first_frame": "first_frame_url", "last_frame": "last_frame_url",
			"image": "reference_image_urls", "video": "reference_video_urls", "duration": "duration", "aspect": "aspect_ratio"},
	}
}

func TestPrepareOmniVideoList(t *testing.T) {
	a := &Adapter{Probe: func(ref string) (float64, error) {
		return map[string]float64{"https://x/short.mp4": 2.9, "https://x/long.mp4": 31}[ref], nil
	}}
	req := job.Request{Prompt: "p", Images: []string{"https://x/a.png", "https://x/b.png"},
		Videos: []string{"https://x/short.mp4", "https://x/long.mp4"}, Duration: 6, Resolution: "720p",
		Params: map[string]any{"character_ids": `["c1"]`}}
	payload, err := a.Prepare(context.Background(), omniRoute(), req, false)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Model string         `json:"model"`
		Input map[string]any `json:"input"`
	}
	_ = json.Unmarshal(payload, &got)
	if got.Model != "google/gemini-omni-flash-1-1" || got.Input["duration"] != "6" {
		t.Fatalf("model/duration: %s", payload)
	}
	clips := got.Input["video_list"].([]any)
	c0, c1 := clips[0].(map[string]any), clips[1].(map[string]any)
	if c0["url"] != "https://x/short.mp4" || c0["ends"] != 2.9 || c1["url"] != "https://x/long.mp4" || c1["ends"] != 10.0 || c0["start"] != 0.0 {
		t.Errorf("video_list: %v", clips)
	}
	if imgs := got.Input["image_urls"].([]any); imgs[0] != "https://x/a.png" || imgs[1] != "https://x/b.png" {
		t.Errorf("image order: %v", imgs)
	}
	if ids := got.Input["character_ids"].([]any); ids[0] != "c1" {
		t.Errorf("param passthrough: %v", ids)
	}
}

func TestPrepareSeedanceAdaptiveAspect(t *testing.T) {
	a := &Adapter{}
	p, err := a.Prepare(context.Background(), seedanceRoute(), job.Request{Prompt: "p", FirstFrame: "https://x/f.png", Duration: 5}, true)
	if err != nil || !strings.Contains(string(p), `"aspect_ratio":"adaptive"`) || !strings.Contains(string(p), `"duration":5`) {
		t.Fatalf("adaptive not set: %s %v", p, err)
	}
	if _, err := a.Prepare(context.Background(), seedanceRoute(), job.Request{Prompt: "p", FirstFrame: "https://x/f.png", Aspect: "9:16"}, true); err == nil {
		t.Error("explicit 9:16 with first frame accepted")
	}
	if _, err := a.Prepare(context.Background(), seedanceRoute(), job.Request{Prompt: "p", FirstFrame: "https://x/f.png", Images: []string{"https://x/i.png"}}, true); err == nil {
		t.Error("first frame + images accepted")
	}
}

func TestUploadSubmitPoll(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "ref.png")
	_ = os.WriteFile(local, []byte("png"), 0o644)
	var task string
	credits := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("auth header %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/file-stream-upload":
			if err := r.ParseMultipartForm(1 << 20); err != nil || r.FormValue("uploadPath") == "" {
				t.Errorf("multipart: %v", err)
			}
			_, _ = io.WriteString(w, `{"success":true,"code":200,"data":{"downloadUrl":"https://tmp.kie/ref.png"}}`)
		case "/api/v1/jobs/createTask":
			if credits {
				_, _ = io.WriteString(w, `{"code":402,"msg":"Credits insufficient"}`)
				return
			}
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "https://tmp.kie/ref.png") {
				t.Errorf("upload url not in payload: %s", body)
			}
			_, _ = io.WriteString(w, `{"code":200,"msg":"success","data":{"taskId":"t1"}}`)
		case "/api/v1/jobs/recordInfo":
			if r.URL.Query().Get("taskId") != "t1" {
				t.Errorf("taskId %q", r.URL.Query().Get("taskId"))
			}
			_, _ = io.WriteString(w, task)
		}
	}))
	defer srv.Close()
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	a := &Adapter{BaseURL: srv.URL, UploadURL: srv.URL, Key: "k", Now: func() time.Time { return now }}
	r := omniRoute()
	payload, err := a.Prepare(context.Background(), r, job.Request{Prompt: "p", Images: []string{local}}, false)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := a.Submit(context.Background(), r, payload)
	if err != nil || sub.ProviderJobID != "t1" {
		t.Fatalf("submit: %+v %v", sub, err)
	}
	rec := job.Record{JobID: "j", ProviderJobID: "t1", Poll: sub.Poll}
	data := func(s string) string { return `{"code":200,"msg":"success","data":` + s + `}` }
	cases := []struct {
		body, status string
		usd          float64
	}{
		{data(`{"state":"waiting"}`), job.StatusQueued, -1},
		{data(`{"state":"generating"}`), job.StatusRunning, -1},
		{data(`{"state":"success","resultJson":"{\"resultUrls\":[\"https://r.kie/o.mp4\"]}","creditsConsumed":84}`), job.StatusDone, 0.42},
		{data(`{"state":"fail","failCode":"500","failMsg":"Internal Error","creditsConsumed":0}`), job.StatusFailedUnbilled, 0},
		{data(`{"state":"fail","failCode":"500","failMsg":"Internal Error","creditsConsumed":84}`), job.StatusFailedBilled, 0.42},
		{data(`{"state":"fail","failCode":"500","failMsg":"Internal Error"}`), job.StatusFailedUnbilled, -1}, // KIE ElevenLabs TTS, seen live
		{data(`{"state":"fail","failMsg":"Content violates safety policy"}`), job.StatusBlockedSafety, -1},
	}
	for _, c := range cases {
		task = c.body
		res, err := a.Poll(context.Background(), r, rec)
		if err != nil || res.Status != c.status {
			t.Errorf("%s: %s %v", c.body, res.Status, err)
			continue
		}
		if c.usd >= 0 && (res.ActualUSD == nil || *res.ActualUSD != c.usd) {
			t.Errorf("%s: cost %v", c.body, res.ActualUSD)
		}
		if c.status == job.StatusDone && (len(res.Outputs) != 1 || res.Outputs[0].URLExpiresAt != "2026-11-02T00:00:00Z" || res.Outputs[0].MIME != "video/mp4") {
			t.Errorf("outputs: %+v", res.Outputs)
		}
	}
	credits = true
	if _, err := a.Submit(context.Background(), r, payload); err == nil || !strings.Contains(err.Error(), "KIE credits insufficient") {
		t.Errorf("402: %v", err)
	}
}
