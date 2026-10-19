// Package falad runs jobs on any fal.ai queue route. Request mapping is
// generic: the route's ParamMap names the provider fields and its stored input
// schema decides types, so a new fal model needs a sync, not code.
package falad

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	gosync "sync"

	"github.com/radjathaher/creative-cli/internal/adapter/reqmap"
	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/fal"
	"github.com/radjathaher/creative-cli/internal/job"
	"github.com/radjathaher/creative-cli/internal/registry"
)

// Adapter implements job.Adapter for the fal queue. The client is created on
// first use so a dry run needs no FAL_KEY.
type Adapter struct {
	once   gosync.Once
	client *fal.Client
	err    error
}

// New returns a fal adapter.
func New() *Adapter { return &Adapter{} }

var _ job.Adapter = (*Adapter)(nil)

func (a *Adapter) fal() (*fal.Client, error) {
	a.once.Do(func() { a.client, a.err = fal.New() })
	return a.client, a.err
}

// pollState is what Submit stores in the job record so Poll can resume.
type pollState struct {
	Endpoint    string `json:"endpoint"`
	RequestID   string `json:"request_id"`
	StatusURL   string `json:"status_url,omitempty"`
	ResponseURL string `json:"response_url,omitempty"`
}

// Prepare maps req onto the route's fields, uploads local references in the
// order given (or shows upload:// placeholders on a dry run), and validates
// --param keys and enum values against the route's schema.
func (a *Adapter) Prepare(ctx context.Context, r registry.Route, req job.Request, dryRun bool) (json.RawMessage, error) {
	r.Schema = normalizeSchema(r.Schema)
	upload := func(_ context.Context, path string) (string, error) {
		c, err := a.fal()
		if err != nil {
			return "", err
		}
		info, err := c.ResolveInput(path)
		return info.ResolvedURL, err
	}
	resolve := func(ref string) (string, error) {
		switch {
		case core.IsRemoteRef(ref):
			return ref, nil
		case dryRun:
			return reqmap.DryRunPrefix + ref, nil
		}
		return upload(ctx, ref)
	}

	params := map[string]any{}
	for k, v := range req.Params {
		params[k] = v
	}
	// Adapter flags such as --model or --factor are shared names; route them
	// through the param map so the schema validates and types them.
	for _, k := range sortedKeys(req.Extra) {
		field := r.ParamMap[k]
		if field == "" {
			return nil, fmt.Errorf("route %s does not accept --%s", r.ID, k)
		}
		if _, set := params[field]; !set {
			params[field] = req.Extra[k]
		}
	}
	// A mesh being rigged or retextured travels in a model_url-style field,
	// which reqmap's image/video/audio input detection does not cover.
	if isMesh(req.Input) {
		field := r.ParamMap["mesh"]
		if field == "" {
			return nil, fmt.Errorf("route %s does not take a 3D model input", r.ID)
		}
		u, err := resolve(req.Input)
		if err != nil {
			return nil, err
		}
		params[field], req.Input = u, ""
	}
	req.Params = params

	body, err := reqmap.Build(ctx, r, req, upload, dryRun)
	if err != nil {
		return nil, err
	}
	if err := checkEnums(r, body); err != nil {
		return nil, err
	}
	return json.Marshal(body)
}

// Submit queues the body on the route's endpoint. The request id is the
// provider job id, and the queue's status and response URLs are kept for Poll.
func (a *Adapter) Submit(ctx context.Context, r registry.Route, payload json.RawMessage) (job.Submission, error) {
	c, err := a.fal()
	if err != nil {
		return job.Submission{}, err
	}
	ref, _, err := c.Submit(r.ModelID, payload)
	if err != nil {
		return job.Submission{}, err
	}
	if ref.RequestID == "" {
		return job.Submission{}, fmt.Errorf("fal %s: submit returned no request_id", r.ModelID)
	}
	state, _ := json.Marshal(pollState{ref.Endpoint, ref.RequestID, ref.StatusURL, ref.ResponseURL})
	return job.Submission{ProviderJobID: ref.RequestID, Payload: payload, Poll: state}, nil
}

// Poll checks the queue once. A completed request is fetched and its media
// URLs collected; a failed one is classified as a safety block or as an
// unbilled failure, since fal does not charge for requests that error.
func (a *Adapter) Poll(ctx context.Context, r registry.Route, rec job.Record) (job.PollResult, error) {
	c, err := a.fal()
	if err != nil {
		return job.PollResult{}, err
	}
	var st pollState
	if err := json.Unmarshal(rec.Poll, &st); err != nil || st.RequestID == "" {
		st = pollState{Endpoint: r.ModelID, RequestID: rec.ProviderJobID}
	}
	if st.Endpoint == "" {
		st.Endpoint = r.ModelID
	}
	ref := fal.QueueRef{Endpoint: st.Endpoint, RequestID: st.RequestID, StatusURL: st.StatusURL, ResponseURL: st.ResponseURL}
	status, raw, err := c.Status(ref)
	if err != nil {
		return job.PollResult{}, err
	}
	s, _ := status["status"].(string)
	switch strings.ToUpper(s) {
	case "IN_QUEUE":
		return job.PollResult{Status: job.StatusQueued, Raw: raw}, nil
	case "IN_PROGRESS":
		return job.PollResult{Status: job.StatusRunning, Raw: raw}, nil
	case "COMPLETED":
	default:
		if msg := errorText(status); msg != "" {
			return failure(msg, raw), nil
		}
		return job.PollResult{Status: job.StatusRunning, Raw: raw}, nil
	}
	if msg := errorText(status); msg != "" {
		return failure(msg, raw), nil
	}
	result, raw, err := c.Result(ref)
	if err != nil {
		// fal answers a failed request's result with an error status and the
		// reason in the body, so a result error after COMPLETED is a job failure.
		return failure(err.Error(), raw), nil
	}
	outs := Outputs(result)
	if len(outs) == 0 {
		return job.PollResult{Status: job.StatusFailedUnbilled, Error: "fal completed without any output", Raw: raw}, nil
	}
	return job.PollResult{Status: job.StatusDone, Outputs: outs, Raw: raw}, nil
}

func failure(msg string, raw json.RawMessage) job.PollResult {
	if reqmap.IsSafety(msg) || strings.Contains(strings.ToLower(msg), "content_policy") {
		return job.PollResult{Status: job.StatusBlockedSafety, Error: msg, Raw: raw}
	}
	return job.PollResult{Status: job.StatusFailedUnbilled, Error: msg, Raw: raw}
}

// errorText pulls an error message from a fal status or error body, which
// uses "error" (string) or "detail" (string or list of {msg}).
func errorText(m map[string]any) string {
	if s, ok := m["error"].(string); ok && s != "" {
		return s
	}
	switch d := m["detail"].(type) {
	case string:
		return d
	case []any:
		var msgs []string
		for _, x := range d {
			if o, ok := x.(map[string]any); ok {
				if s, ok := o["msg"].(string); ok {
					msgs = append(msgs, s)
				}
			}
		}
		return strings.Join(msgs, "; ")
	}
	return ""
}

func isMesh(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.SplitN(path, "?", 2)[0])) {
	case ".glb", ".gltf", ".fbx", ".obj", ".usdz", ".stl":
		return true
	}
	return false
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
