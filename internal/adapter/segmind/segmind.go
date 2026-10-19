// Package segmind is the job adapter for Segmind's v2 async API (transport
// segmind-v2). It works for any Segmind slug: the request is mapped through
// the route's param map and schema, local references go to Segmind's own
// asset storage, and the job is polled through its status and response URLs.
package segmind

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/radjathaher/creative-cli/internal/adapter"
	"github.com/radjathaher/creative-cli/internal/adapter/reqmap"
	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/job"
	"github.com/radjathaher/creative-cli/internal/registry"
)

const (
	defaultBaseURL    = "https://api.segmind.com"
	defaultStorageURL = "https://workflows-api.segmind.com"
)

// Adapter implements job.Adapter for Segmind.
type Adapter struct {
	BaseURL    string
	StorageURL string
	// Key overrides SEGMIND_API_KEY; tests set it.
	Key string
}

var _ job.Adapter = (*Adapter)(nil)

func init() { adapter.Register(New(), "segmind-v2") }

// New returns an adapter that reads its endpoints from SEGMIND_API_URL and
// SEGMIND_STORAGE_URL when set.
func New() *Adapter {
	return &Adapter{BaseURL: envOr("SEGMIND_API_URL", defaultBaseURL), StorageURL: envOr("SEGMIND_STORAGE_URL", defaultStorageURL)}
}

// pollState is persisted in the job record so any process can resume.
type pollState struct {
	RequestID   string `json:"request_id"`
	StatusURL   string `json:"status_url"`
	ResponseURL string `json:"response_url"`
}

func (a *Adapter) Prepare(ctx context.Context, r registry.Route, req job.Request, dryRun bool) (json.RawMessage, error) {
	if req.LastFrame != "" && req.FirstFrame == "" {
		return nil, fmt.Errorf("--last-frame requires --first-frame")
	}
	// Owner rule: Seedance first-frame mode and reference images are separate
	// modes on Segmind; combining them silently drops one.
	if strings.HasPrefix(r.Family, "seedance") && req.FirstFrame != "" && len(req.Images) > 0 {
		return nil, fmt.Errorf("--first-frame cannot be combined with --image on %s; use one image-to-video mode", r.ID)
	}
	body, err := reqmap.Build(ctx, r, req, a.upload, dryRun)
	if err != nil {
		return nil, err
	}
	return json.Marshal(body)
}

func (a *Adapter) Submit(ctx context.Context, r registry.Route, payload json.RawMessage) (job.Submission, error) {
	parsed, raw, err := a.do(ctx, http.MethodPost, a.base()+"/v2/"+r.ModelID, payload)
	if err != nil {
		return job.Submission{}, fmt.Errorf("segmind submit %s: %w", r.ModelID, err)
	}
	id := str(parsed["request_id"])
	if id == "" {
		return job.Submission{}, fmt.Errorf("segmind submit %s: no request_id in %s", r.ModelID, truncate(raw))
	}
	st := pollState{
		RequestID:   id,
		StatusURL:   firstNonEmpty(str(parsed["status_url"]), a.base()+"/v2/requests/"+id+"/status"),
		ResponseURL: firstNonEmpty(str(parsed["response_url"]), a.base()+"/v2/requests/"+id),
	}
	poll, _ := json.Marshal(st)
	return job.Submission{ProviderJobID: id, Payload: payload, Poll: poll}, nil
}

func (a *Adapter) Poll(ctx context.Context, r registry.Route, rec job.Record) (job.PollResult, error) {
	var st pollState
	if err := json.Unmarshal(rec.Poll, &st); err != nil || st.StatusURL == "" {
		if rec.ProviderJobID == "" {
			return job.PollResult{}, fmt.Errorf("segmind job %s has no request id to poll", rec.JobID)
		}
		st = pollState{RequestID: rec.ProviderJobID, StatusURL: a.base() + "/v2/requests/" + rec.ProviderJobID + "/status", ResponseURL: a.base() + "/v2/requests/" + rec.ProviderJobID}
	}
	status, raw, err := a.do(ctx, http.MethodGet, st.StatusURL, nil)
	if err != nil {
		return job.PollResult{}, fmt.Errorf("segmind status %s: %w", st.RequestID, err)
	}
	res := job.PollResult{Raw: raw, ActualUSD: findCost(status)}
	switch s := strings.ToUpper(str(status["status"])); s {
	case "COMPLETED", "SUCCEEDED", "SUCCESS":
		result, rraw, err := a.do(ctx, http.MethodGet, st.ResponseURL, nil)
		if err != nil {
			return job.PollResult{}, fmt.Errorf("segmind result %s: %w", st.RequestID, err)
		}
		res.Raw = rraw
		if c := findCost(result); c != nil {
			res.ActualUSD = c
		}
		res.Outputs = outputs(result)
		if len(res.Outputs) == 0 {
			res.Status, res.Error = job.StatusUncertain, "segmind reported completion but the result has no media url"
			return res, nil
		}
		res.Status = job.StatusDone
	case "FAILED", "ERROR", "CANCELLED", "CANCELED":
		res.Error = failureMessage(status)
		res.Status = job.StatusFailedUnbilled // Segmind does not bill failed requests
		if reqmap.IsSafety(res.Error) {
			res.Status = job.StatusBlockedSafety
		}
	case "QUEUED", "PENDING", "":
		res.Status = job.StatusQueued
	default:
		res.Status = job.StatusRunning
	}
	return res, nil
}

func (a *Adapter) base() string {
	return strings.TrimRight(firstNonEmpty(a.BaseURL, defaultBaseURL), "/")
}

func (a *Adapter) key() (string, error) {
	if a.Key != "" {
		return a.Key, nil
	}
	return core.ReadSecret("SEGMIND_API_KEY")
}

// upload sends one local file to Segmind asset storage as a data URL.
func (a *Adapter) upload(ctx context.Context, path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	dataURL := "data:" + core.MIMEForPath(path) + ";base64," + base64.StdEncoding.EncodeToString(b)
	body, _ := json.Marshal(map[string]any{"data_urls": []string{dataURL}})
	core.Progress("uploading %s to segmind", path)
	storage := strings.TrimRight(firstNonEmpty(a.StorageURL, defaultStorageURL), "/")
	parsed, raw, err := a.do(ctx, http.MethodPost, storage+"/upload-asset", body)
	if err != nil {
		return "", fmt.Errorf("segmind upload %s: %w", path, err)
	}
	if arr, ok := parsed["file_urls"].([]any); ok && len(arr) > 0 && str(arr[0]) != "" {
		return str(arr[0]), nil
	}
	if u := str(parsed["url"]); u != "" {
		return u, nil
	}
	return "", fmt.Errorf("segmind upload %s: no file url in %s", path, truncate(raw))
}

func (a *Adapter) do(ctx context.Context, method, url string, body []byte) (map[string]any, json.RawMessage, error) {
	key, err := a.key()
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	return core.DecodeJSON(resp)
}

// outputs collects every media URL in a result, preferring the "output" subtree.
func outputs(result map[string]any) []job.Output {
	var urls []string
	seen := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			if isMedia(t) && !seen[t] {
				seen[t] = true
				urls = append(urls, t)
			}
		case map[string]any:
			for _, k := range sortedKeys(t) {
				walk(t[k])
			}
		case []any:
			for _, x := range t {
				walk(x)
			}
		}
	}
	if out, ok := result["output"]; ok {
		walk(out)
	}
	if len(urls) == 0 {
		walk(result)
	}
	outs := make([]job.Output, len(urls))
	for i, u := range urls {
		outs[i] = job.Output{URL: u, MIME: reqmap.MIMEForURL(u)}
	}
	return outs
}

func isMedia(s string) bool {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return false
	}
	return reqmap.MIMEForURL(s) != "application/octet-stream" || strings.Contains(s, "/output/")
}

// findCost reads metrics.cost (or a top-level cost) as the billed amount.
func findCost(m map[string]any) *float64 {
	for _, v := range []any{dig(m, "metrics", "cost"), m["cost"], dig(m, "metrics", "cost_usd")} {
		switch t := v.(type) {
		case float64:
			return &t
		case string:
			var f float64
			if _, err := fmt.Sscan(t, &f); err == nil {
				return &f
			}
		}
	}
	return nil
}

func failureMessage(m map[string]any) string {
	for _, k := range []string{"error", "message", "detail", "error_message", "reason"} {
		switch t := m[k].(type) {
		case string:
			if t != "" {
				return t
			}
		case map[string]any:
			if s := failureMessage(t); s != "" {
				return s
			}
		}
	}
	raw, _ := json.Marshal(m)
	return "segmind task failed: " + truncate(raw)
}

func dig(m map[string]any, keys ...string) any {
	var v any = m
	for _, k := range keys {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[k]
	}
	return v
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic order for multi-output results
	return keys
}

func str(v any) string { s, _ := v.(string); return s }

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
