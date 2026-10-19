package job

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/registry"
)

// Options control one run.
type Options struct {
	Run          string
	Out          string
	NoWait       bool
	DryRun       bool
	PollInterval time.Duration
	MaxWait      time.Duration
	MaxCost      float64 // per job; 0 disables
	MaxRunCost   float64 // across Run; 0 disables
	PricesAsOf   string
}

// ErrDryRun is returned with the prepared record when nothing was submitted.
var ErrDryRun = errors.New("dry run: nothing submitted")

// Start prepares, guards, submits, persists, and (unless NoWait) waits for a job.
// The record is saved immediately after submit, before any polling, so the
// provider job id survives a killed process.
func Start(ctx context.Context, a Adapter, r registry.Route, req Request, est registry.Estimate, o Options) (*Record, error) {
	rec := &Record{
		JobID: NewID(), Run: o.Run, Op: r.Op, Family: r.Family, Provider: r.Provider,
		RouteID: r.ID, ModelID: r.ModelID, Status: StatusQueued, Verified: r.Verified,
		Request: req, OutPath: o.Out, CreatedAt: now(), PricesAsOf: o.PricesAsOf,
		Cost: Cost{EstimatedUSD: est.USD, Basis: est.Basis},
	}
	if p := req.Prompt + req.Text; p != "" {
		sum := sha256.Sum256([]byte(p))
		rec.PromptSHA256 = hex.EncodeToString(sum[:])
	}
	if err := guardCost(est, o); err != nil {
		return nil, err
	}
	payload, err := a.Prepare(ctx, r, req, o.DryRun)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", r.ID, err)
	}
	rec.Payload = payload
	if o.DryRun {
		return rec, ErrDryRun
	}

	start := time.Now()
	sub, err := a.Submit(ctx, r, payload)
	if err != nil {
		return nil, fmt.Errorf("%s submit: %w", r.ID, err)
	}
	rec.ProviderJobID, rec.Poll, rec.Status = sub.ProviderJobID, sub.Poll, StatusRunning
	if sub.ActualUSD != nil {
		rec.Cost.ActualUSD = sub.ActualUSD
	}
	if err := Save(rec); err != nil {
		return rec, fmt.Errorf("job submitted as %s but could not be saved: %w", sub.ProviderJobID, err)
	}
	if sub.ProviderJobID != "" {
		core.Progress("submitted %s → %s (job %s)", r.ID, sub.ProviderJobID, rec.JobID)
	}
	if len(sub.Immediate) > 0 {
		return finish(rec, PollResult{Status: StatusDone, Outputs: sub.Immediate, ActualUSD: sub.ActualUSD}, start)
	}
	if o.NoWait {
		return rec, nil
	}
	return Wait(ctx, a, r, rec, o, start)
}

func guardCost(est registry.Estimate, o Options) error {
	if o.MaxCost > 0 {
		if est.USD == nil {
			return fmt.Errorf("--max-cost set but the price is unknown (%s); drop --max-cost to run anyway", est.Basis)
		}
		if *est.USD > o.MaxCost {
			return fmt.Errorf("estimate $%.4f exceeds --max-cost $%.4f (%s)", *est.USD, o.MaxCost, est.Basis)
		}
	}
	if o.MaxRunCost > 0 && o.Run != "" {
		spent, err := RunSpend(o.Run)
		if err != nil {
			return err
		}
		add := 0.0
		if est.USD != nil {
			add = *est.USD
		}
		if spent+add > o.MaxRunCost {
			return fmt.Errorf("run %s has spent $%.2f; this job ($%.2f) would exceed --max-run-cost $%.2f", o.Run, spent, add, o.MaxRunCost)
		}
	}
	return nil
}

// Wait polls a saved job until it is terminal or MaxWait passes. A timeout
// leaves the job running in the store so `creative jobs fetch` can resume it.
func Wait(ctx context.Context, a Adapter, r registry.Route, rec *Record, o Options, start time.Time) (*Record, error) {
	interval := o.PollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(o.MaxWait)
	for {
		res, err := a.Poll(ctx, r, *rec)
		if err != nil {
			core.Progress("poll %s: %v (will retry)", rec.ProviderJobID, err)
		} else if res.Status != "" && res.Status != rec.Status {
			core.Progress("%s: %s", rec.JobID, res.Status)
			rec.Status = res.Status
			_ = Save(rec)
		}
		if err == nil && Terminal(res.Status) {
			return finish(rec, res, start)
		}
		if o.MaxWait > 0 && time.Now().After(deadline) {
			_ = Save(rec)
			return rec, fmt.Errorf("still %s after %s; resume with `creative jobs fetch %s`", rec.Status, o.MaxWait, rec.JobID)
		}
		select {
		case <-ctx.Done():
			_ = Save(rec)
			return rec, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// finish records the terminal state and downloads outputs to --out. With
// several outputs, the first goes to --out and the rest get -2, -3 suffixes.
func finish(rec *Record, res PollResult, start time.Time) (*Record, error) {
	rec.Status, rec.Error = res.Status, res.Error
	if res.ActualUSD != nil {
		rec.Cost.ActualUSD = res.ActualUSD
	}
	rec.Outputs = res.Outputs
	if !start.IsZero() {
		rec.ElapsedSecs = time.Since(start).Seconds()
	}
	var dlErr error
	if rec.Status == StatusDone && rec.OutPath != "" {
		for i := range rec.Outputs {
			out := &rec.Outputs[i]
			if len(out.Data) > 0 {
				dest := numbered(rec.OutPath, i)
				if err := core.WriteFile(dest, out.Data); err != nil {
					dlErr = err
					continue
				}
				out.Path = dest
				continue
			}
			if out.URL == "" {
				if out.Text != "" && i == 0 {
					dlErr = core.WriteFile(rec.OutPath, []byte(out.Text))
					out.Path = rec.OutPath
				}
				continue
			}
			dest := numbered(rec.OutPath, i)
			if _, err := core.Download(out.URL, dest, nil); err != nil {
				dlErr = fmt.Errorf("download %s: %w", out.URL, err)
				continue
			}
			out.Path = dest
		}
	}
	if err := Save(rec); err != nil {
		return rec, err
	}
	if rec.Status != StatusDone {
		return rec, fmt.Errorf("%s %s: %s", rec.RouteID, rec.Status, rec.Error)
	}
	return rec, dlErr
}

func numbered(path string, i int) string {
	if i == 0 {
		return path
	}
	ext := filepath.Ext(path)
	return fmt.Sprintf("%s-%d%s", strings.TrimSuffix(path, ext), i+1, ext)
}

// Refetch downloads a finished job's URL outputs again, for example to a new
// --out path. Byte outputs from synchronous providers were written at the time
// and are not stored, so they are left as they are.
func Refetch(rec *Record) (*Record, error) {
	return finish(rec, PollResult{Status: StatusDone, Outputs: rec.Outputs, ActualUSD: rec.Cost.ActualUSD}, time.Time{})
}
