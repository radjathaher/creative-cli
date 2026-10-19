// Package kie is the job adapter for KIE's market jobs API (transport
// kie-jobs): createTask with {model, input}, then recordInfo until the task
// succeeds or fails. KIE result URLs expire after 14 days, so outputs carry
// url_expires_at and the engine must download them.
package kie

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/radjathaher/creative-cli/internal/adapter"
	"github.com/radjathaher/creative-cli/internal/adapter/reqmap"
	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/job"
	"github.com/radjathaher/creative-cli/internal/registry"
)

const (
	defaultBaseURL   = "https://api.kie.ai"
	defaultUploadURL = "https://kieai.redpandaai.co"
	creditUSD        = 0.005
	resultTTL        = 14 * 24 * time.Hour
	// omniClipMax is the longest slice of one reference video Omni reads.
	omniClipMax = 10.0
)

// Adapter implements job.Adapter for KIE.
type Adapter struct {
	BaseURL   string
	UploadURL string
	// Key overrides KIE_API_KEY; tests set it.
	Key string
	// Probe returns a video's duration in seconds; tests replace it.
	Probe func(ref string) (float64, error)
	// Now is the clock for url_expires_at; tests replace it.
	Now func() time.Time
}

var _ job.Adapter = (*Adapter)(nil)

func init() { adapter.Register(New(), "kie-jobs") }

// New returns an adapter that reads KIE_API_URL and KIE_UPLOAD_URL when set.
func New() *Adapter {
	return &Adapter{BaseURL: envOr("KIE_API_URL", defaultBaseURL), UploadURL: envOr("KIE_UPLOAD_URL", defaultUploadURL)}
}

type pollState struct {
	TaskID string `json:"task_id"`
}

func (a *Adapter) Prepare(ctx context.Context, r registry.Route, req job.Request, dryRun bool) (json.RawMessage, error) {
	if req.LastFrame != "" && req.FirstFrame == "" {
		return nil, fmt.Errorf("--last-frame requires --first-frame")
	}
	seedance := strings.HasPrefix(r.Family, "seedance")
	if req.FirstFrame != "" && (seedance || r.ParamMap["video"] == "video_list") && (len(req.Images) > 0 || len(req.Videos) > 0) {
		return nil, fmt.Errorf("--first-frame cannot be combined with --image or --video on %s; they are separate modes", r.ID)
	}
	// Seedance 2.5 on KIE answers 422 to a first frame unless the aspect is
	// "adaptive" (verified live 2026-10-19).
	if seedance && req.FirstFrame != "" && r.ParamMap["aspect"] != "" {
		switch req.Aspect {
		case "":
			req.Aspect = "adaptive"
		case "adaptive":
		default:
			return nil, fmt.Errorf("%s with --first-frame needs --aspect adaptive (KIE rejects %q)", r.ID, req.Aspect)
		}
	}
	input, err := reqmap.Build(ctx, r, req, a.upload, dryRun)
	if err != nil {
		return nil, err
	}
	// Omni takes reference videos as trimmed clips, not bare URLs.
	if field := r.ParamMap["video"]; field == "video_list" && len(req.Videos) > 0 {
		if _, user := req.Params[field]; !user {
			urls, _ := input[field].([]string)
			clips := make([]map[string]any, len(urls))
			for i, u := range urls {
				ends, err := a.clipEnd(req.Videos[i])
				if err != nil {
					return nil, err
				}
				clips[i] = map[string]any{"url": u, "start": 0, "ends": ends}
			}
			input[field] = clips
		}
	}
	return json.Marshal(map[string]any{"model": r.ModelID, "input": input})
}

// clipEnd uses the whole reference video up to Omni's 10 s window.
func (a *Adapter) clipEnd(ref string) (float64, error) {
	probe := a.Probe
	if probe == nil {
		probe = func(ref string) (float64, error) {
			m, err := core.ProbeVideo(ref)
			return m.DurationSeconds, err
		}
	}
	d, err := probe(ref)
	if err != nil {
		return 0, fmt.Errorf("video_list needs each reference video's length: %w (or pass --param video_list='[...]')", err)
	}
	return math.Min(math.Floor(d*100)/100, omniClipMax), nil
}

func (a *Adapter) Submit(ctx context.Context, r registry.Route, payload json.RawMessage) (job.Submission, error) {
	env, raw, err := a.call(ctx, http.MethodPost, a.base()+"/api/v1/jobs/createTask", payload)
	if err != nil {
		return job.Submission{}, err
	}
	var data struct {
		TaskID string `json:"taskId"`
	}
	_ = json.Unmarshal(env.Data, &data)
	if data.TaskID == "" {
		return job.Submission{}, fmt.Errorf("kie createTask %s: no taskId in %s", r.ModelID, truncate(raw))
	}
	poll, _ := json.Marshal(pollState{TaskID: data.TaskID})
	return job.Submission{ProviderJobID: data.TaskID, Payload: payload, Poll: poll}, nil
}

type record struct {
	State           string `json:"state"`
	ResultJSON      string `json:"resultJson"`
	FailCode        any    `json:"failCode"`
	FailMsg         string `json:"failMsg"`
	CreditsConsumed any    `json:"creditsConsumed"`
}

func (a *Adapter) Poll(ctx context.Context, r registry.Route, rec job.Record) (job.PollResult, error) {
	var st pollState
	_ = json.Unmarshal(rec.Poll, &st)
	id := firstNonEmpty(st.TaskID, rec.ProviderJobID)
	if id == "" {
		return job.PollResult{}, fmt.Errorf("kie job %s has no task id to poll", rec.JobID)
	}
	env, raw, err := a.call(ctx, http.MethodGet, a.base()+"/api/v1/jobs/recordInfo?taskId="+url.QueryEscape(id), nil)
	if err != nil {
		return job.PollResult{}, err
	}
	var d record
	if err := json.Unmarshal(env.Data, &d); err != nil {
		return job.PollResult{}, fmt.Errorf("kie recordInfo %s: %w", id, err)
	}
	res := job.PollResult{Raw: raw}
	credits, hasCredits := number(d.CreditsConsumed)
	if hasCredits {
		res.ActualUSD = registry.F(credits * creditUSD)
	}
	switch strings.ToLower(d.State) {
	case "waiting", "queuing", "":
		res.Status = job.StatusQueued
	case "generating":
		res.Status = job.StatusRunning
	case "success":
		res.Outputs = a.outputs(d.ResultJSON)
		res.Status = job.StatusDone
		if len(res.Outputs) == 0 {
			res.Status, res.Error = job.StatusUncertain, "kie reported success but resultJson has no resultUrls"
		}
	case "fail":
		res.Error = strings.TrimSpace(fmt.Sprintf("%v %s", orEmpty(d.FailCode), d.FailMsg))
		switch {
		case reqmap.IsSafety(d.FailMsg):
			res.Status = job.StatusBlockedSafety
		case !hasCredits || credits == 0:
			res.Status = job.StatusFailedUnbilled
		default:
			res.Status = job.StatusFailedBilled
		}
	default:
		res.Status = job.StatusRunning
	}
	return res, nil
}

func (a *Adapter) outputs(resultJSON string) []job.Output {
	var rj struct {
		ResultURLs []string `json:"resultUrls"`
	}
	if json.Unmarshal([]byte(resultJSON), &rj) != nil {
		return nil
	}
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	expires := now().UTC().Add(resultTTL).Format(time.RFC3339)
	outs := make([]job.Output, 0, len(rj.ResultURLs))
	for _, u := range rj.ResultURLs {
		outs = append(outs, job.Output{URL: u, MIME: reqmap.MIMEForURL(u), URLExpiresAt: expires})
	}
	return outs
}

// envelope is KIE's response wrapper; code repeats the HTTP status in the body.
type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func (a *Adapter) call(ctx context.Context, method, endpoint string, body []byte) (envelope, []byte, error) {
	key, err := a.key()
	if err != nil {
		return envelope{}, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return envelope{}, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return a.send(req, key)
}

func (a *Adapter) send(req *http.Request, key string) (envelope, []byte, error) {
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return envelope{}, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return envelope{}, nil, err
	}
	var env envelope
	_ = json.Unmarshal(raw, &env)
	code := env.Code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code = resp.StatusCode
	}
	switch {
	case code == http.StatusPaymentRequired:
		return env, raw, fmt.Errorf("KIE credits insufficient: top up the account or use another route (%s)", env.Msg)
	case code != 0 && code != http.StatusOK:
		return env, raw, fmt.Errorf("kie %s: code %d %s", req.URL.Path, code, firstNonEmpty(env.Msg, truncate(raw)))
	}
	return env, raw, nil
}

// upload sends one local file to KIE's file stream endpoint. Uploaded files
// expire after 24 hours, which is ample for submit.
func (a *Adapter) upload(ctx context.Context, path string) (string, error) {
	key, err := a.key()
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("uploadPath", "creative/uploads")
	_ = w.WriteField("fileName", filepath.Base(path))
	part, err := w.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	core.Progress("uploading %s to kie", path)
	endpoint := strings.TrimRight(firstNonEmpty(a.UploadURL, defaultUploadURL), "/") + "/api/file-stream-upload"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	env, raw, err := a.send(req, key)
	if err != nil {
		return "", fmt.Errorf("kie upload %s: %w", path, err)
	}
	var data struct {
		DownloadURL string `json:"downloadUrl"`
	}
	_ = json.Unmarshal(env.Data, &data)
	if data.DownloadURL == "" {
		return "", fmt.Errorf("kie upload %s: no downloadUrl in %s", path, truncate(raw))
	}
	return data.DownloadURL, nil
}

func (a *Adapter) base() string {
	return strings.TrimRight(firstNonEmpty(a.BaseURL, defaultBaseURL), "/")
}

func (a *Adapter) key() (string, error) {
	if a.Key != "" {
		return a.Key, nil
	}
	return core.ReadSecret("KIE_API_KEY")
}

func number(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	}
	return 0, false
}

func orEmpty(v any) any {
	if v == nil {
		return ""
	}
	return v
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func envOr(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

func truncate(b []byte) string {
	if len(b) > 300 {
		return string(b[:300]) + "…"
	}
	return string(b)
}
