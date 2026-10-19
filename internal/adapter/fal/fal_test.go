package falad

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radjathaher/creative-cli/internal/job"
	"github.com/radjathaher/creative-cli/internal/registry"
	"github.com/radjathaher/creative-cli/internal/sync"
)

// fixtureRoute builds a route the way sync does, from a recorded fal OpenAPI spec.
func fixtureRoute(t *testing.T, endpoint string) registry.Route {
	t.Helper()
	path := filepath.Join("..", "..", "sync", "testdata", "fal", "openapi", strings.ReplaceAll(endpoint, "/", "_")+".json")
	spec, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := sync.FalInputSchema(spec, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	return registry.Route{ID: "fal:" + endpoint, Provider: "fal", ModelID: endpoint, Schema: schema, ParamMap: sync.ParamMapFromSchema(schema)}
}

func prepare(t *testing.T, r registry.Route, req job.Request) map[string]any {
	t.Helper()
	raw, err := New().Prepare(context.Background(), r, req, true)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestPrepareOmniR2VKeepsReferenceOrder(t *testing.T) {
	r := fixtureRoute(t, "google/gemini-omni-flash/v1.1/reference-to-video")
	body := prepare(t, r, job.Request{
		Prompt:     "<IMAGE_REF_0> walks through <VIDEO_REF_0>",
		Images:     []string{"persona.png", "https://cdn.example/b.png"},
		Videos:     []string{"perf-2.mp4", "https://cdn.example/perf-1.mp4"},
		Duration:   6,
		Resolution: "720P",
		Aspect:     "9:16",
	})
	want := map[string]any{
		"prompt":               "<IMAGE_REF_0> walks through <VIDEO_REF_0>",
		"image_urls":           []any{"upload://persona.png", "https://cdn.example/b.png"},
		"reference_video_urls": []any{"upload://perf-2.mp4", "https://cdn.example/perf-1.mp4"},
		"duration":             float64(6),
		"resolution":           "720p",
		"aspect_ratio":         "9:16",
	}
	assertBody(t, body, want)
}

func TestPrepareTopazUpscale(t *testing.T) {
	r := fixtureRoute(t, "topaz/upscale/video/precision")
	body := prepare(t, r, job.Request{
		Input:  "https://cdn.example/take.mp4",
		FPS:    50,
		Extra:  map[string]string{"model": "Gaia HQ", "factor": "1.5"},
		Params: map[string]any{"recover_detail": "0.2"},
	})
	assertBody(t, body, map[string]any{
		"video_url":      "https://cdn.example/take.mp4",
		"target_fps":     float64(50),
		"upscale_factor": 1.5,
		"model":          "Gaia HQ",
		"recover_detail": 0.2,
	})
}

func TestPrepareSeedanceFirstFrameAndStringDuration(t *testing.T) {
	r := fixtureRoute(t, "bytedance/seedance-2.5/us/image-to-video")
	body := prepare(t, r, job.Request{Prompt: "go", FirstFrame: "start.png", LastFrame: "end.png", Duration: 6})
	if body["image_url"] != "upload://start.png" || body["end_image_url"] != "upload://end.png" || body["duration"] != "6" {
		t.Errorf("seedance i2v body: %v", body)
	}
}

func TestPrepareMeshInput(t *testing.T) {
	r := fixtureRoute(t, "fal-ai/meshy/rigging")
	body := prepare(t, r, job.Request{Input: "hero.glb"})
	if body["model_url"] != "upload://hero.glb" {
		t.Errorf("rigging body: %v", body)
	}
}

func TestPrepareRejectsBadInput(t *testing.T) {
	r := fixtureRoute(t, "google/gemini-omni-flash/v1.1/reference-to-video")
	a := New()
	_, err := a.Prepare(context.Background(), r, job.Request{Prompt: "x", Params: map[string]any{"bogus": "1"}}, true)
	if err == nil || !strings.Contains(err.Error(), "reference_video_urls") {
		t.Errorf("unknown --param should list valid keys, got %v", err)
	}
	_, err = a.Prepare(context.Background(), r, job.Request{Prompt: "x", Resolution: "8k"}, true)
	if err == nil || !strings.Contains(err.Error(), "360p") {
		t.Errorf("bad resolution should list the enum, got %v", err)
	}
	_, err = a.Prepare(context.Background(), r, job.Request{Prompt: "x", FirstFrame: "a.png"}, true)
	if err == nil {
		t.Error("omni r2v has no first-frame field; --first-frame must fail")
	}
	_, err = a.Prepare(context.Background(), r, job.Request{Prompt: "x", Extra: map[string]string{"factor": "2"}}, true)
	if err == nil {
		t.Error("unknown adapter flag must fail")
	}
}

// queueServer imitates fal's queue: submit, then status and result per request id.
func queueServer(t *testing.T, statuses []string, result string, resultCode int) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	calls := 0
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Key test" {
			http.Error(w, "unauthorized", 401)
			return
		}
		switch {
		case r.Method == http.MethodPost:
			base := srv.URL + "/" + strings.Trim(r.URL.Path, "/") + "/requests/req-1"
			json.NewEncoder(w).Encode(map[string]string{"request_id": "req-1", "status_url": base + "/status", "response_url": base})
		case strings.HasSuffix(r.URL.Path, "/status"):
			s := statuses[min(calls, len(statuses)-1)]
			calls++
			json.NewEncoder(w).Encode(map[string]string{"status": s})
		default:
			w.WriteHeader(resultCode)
			w.Write([]byte(result))
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FAL_KEY", "test")
	t.Setenv("FAL_QUEUE_URL", srv.URL)
	return srv
}

func runJob(t *testing.T, r registry.Route) []job.PollResult {
	t.Helper()
	a := New()
	sub, err := a.Submit(context.Background(), r, json.RawMessage(`{"prompt":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if sub.ProviderJobID != "req-1" {
		t.Fatalf("provider job id %q", sub.ProviderJobID)
	}
	rec := job.Record{ProviderJobID: sub.ProviderJobID, Poll: sub.Poll}
	var out []job.PollResult
	for i := 0; i < 5; i++ {
		res, err := a.Poll(context.Background(), r, rec)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, res)
		if job.Terminal(res.Status) {
			break
		}
	}
	return out
}

func TestSubmitPollDone(t *testing.T) {
	queueServer(t, []string{"IN_QUEUE", "IN_PROGRESS", "COMPLETED"},
		`{"video":{"url":"https://v3.fal.media/files/out.mp4","content_type":"video/mp4"},"seed":7}`, 200)
	res := runJob(t, registry.Route{ID: "fal:google/gemini-omni-flash/v1.1/reference-to-video", ModelID: "google/gemini-omni-flash/v1.1/reference-to-video"})
	got := []string{}
	for _, r := range res {
		got = append(got, r.Status)
	}
	if strings.Join(got, ",") != "queued,running,done" {
		t.Fatalf("statuses %v", got)
	}
	last := res[len(res)-1]
	if len(last.Outputs) != 1 || last.Outputs[0].URL != "https://v3.fal.media/files/out.mp4" || last.Outputs[0].MIME != "video/mp4" {
		t.Errorf("outputs %+v", last.Outputs)
	}
}

func TestPollSafetyBlock(t *testing.T) {
	queueServer(t, []string{"COMPLETED"}, `{"detail":[{"msg":"Output flagged by content checker (content_policy_violation)","type":"content_policy_violation"}]}`, 422)
	res := runJob(t, registry.Route{ID: "fal:x", ModelID: "x/y"})
	if res[0].Status != job.StatusBlockedSafety {
		t.Errorf("status %s err %s", res[0].Status, res[0].Error)
	}
}

func TestPollFailureUnbilled(t *testing.T) {
	queueServer(t, []string{"COMPLETED"}, `{"detail":"Internal server error"}`, 500)
	res := runJob(t, registry.Route{ID: "fal:x", ModelID: "x/y"})
	if res[0].Status != job.StatusFailedUnbilled || res[0].Error == "" {
		t.Errorf("status %s err %s", res[0].Status, res[0].Error)
	}
}

func TestOutputs(t *testing.T) {
	var images, mesh, stt map[string]any
	json.Unmarshal([]byte(`{"images":[{"url":"https://fal.media/a.png","content_type":"image/png"},{"url":"https://fal.media/b.png"}],"description":""}`), &images)
	json.Unmarshal([]byte(`{"model_mesh":{"url":"https://fal.media/m.glb","content_type":"application/octet-stream"},"thumbnail":{"url":"https://fal.media/t.png"}}`), &mesh)
	json.Unmarshal([]byte(`{"text":"hello world","words":[{"text":"hello","start":0.1}]}`), &stt)
	if o := Outputs(images); len(o) != 2 || o[1].MIME != "image/png" {
		t.Errorf("images %+v", o)
	}
	if o := Outputs(mesh); len(o) != 2 || o[0].URL != "https://fal.media/m.glb" {
		t.Errorf("mesh %+v", o)
	}
	if o := Outputs(stt); len(o) != 1 || o[0].Text != "hello world" {
		t.Errorf("stt %+v", o)
	}
}

func assertBody(t *testing.T, got, want map[string]any) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("body\n got %s\nwant %s", g, w)
	}
}
