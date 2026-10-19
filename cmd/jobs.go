package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/radjathaher/creative-cli/internal/adapter"
	"github.com/radjathaher/creative-cli/internal/job"
	"github.com/radjathaher/creative-cli/internal/registry"
	"github.com/spf13/cobra"
)

func init() {
	register(func(root *cobra.Command) {
		jobs := &cobra.Command{Use: "jobs", Short: "List, inspect, and resume saved jobs."}

		list := &cobra.Command{
			Use:   "list",
			Short: "List saved jobs, optionally by run, with cost totals.",
			Args:  cobra.NoArgs,
			RunE:  runJobsList,
		}
		list.Flags().String("run", "", "only jobs in this run")
		list.Flags().String("status", "", "only jobs with this status")
		list.Flags().String("since", "", "only jobs created after this RFC3339 time or duration ago (e.g. 24h)")
		list.Flags().Bool("table", false, "print a human table instead of JSON")

		get := &cobra.Command{
			Use:   "get <job-id>",
			Short: "Print one saved job record.",
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, a []string) error {
				rec, err := job.Load(a[0])
				if err != nil {
					return err
				}
				return emitJSON(c, rec)
			},
		}

		fetch := &cobra.Command{
			Use:   "fetch <job-id>",
			Short: "Resume a job: poll until done and download its outputs.",
			Long: `Resume a job saved by an op command. A running job is polled until it is
terminal; a finished job's outputs are downloaded again. Nothing is resubmitted.`,
			Args: cobra.ExactArgs(1),
			RunE: runJobsFetch,
		}
		fetch.Flags().StringP("out", "o", "", "download here (defaults to the job's original --out)")
		fetch.Flags().Int("poll-interval-secs", 5, "seconds between status polls")
		fetch.Flags().Int("max-wait-secs", 1800, "max seconds to wait")

		jobs.AddCommand(list, get, fetch)
		root.AddCommand(jobs)
	})
}

func runJobsList(c *cobra.Command, _ []string) error {
	f := job.Filter{Run: flagStr(c, "run"), Status: flagStr(c, "status")}
	if s := flagStr(c, "since"); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			f.Since = time.Now().Add(-d)
		} else if t, err := time.Parse(time.RFC3339, s); err == nil {
			f.Since = t
		} else {
			return fmt.Errorf("--since wants a duration (24h) or RFC3339 time")
		}
	}
	recs, err := job.List(f)
	if err != nil {
		return err
	}
	total, unknown := 0.0, 0
	for _, r := range recs {
		switch {
		case r.Status == job.StatusFailedUnbilled:
		case r.Cost.ActualUSD != nil:
			total += *r.Cost.ActualUSD
		case r.Cost.EstimatedUSD != nil:
			total += *r.Cost.EstimatedUSD
		default:
			unknown++
		}
	}
	if !flagBool(c, "table") {
		type out struct {
			Jobs        []job.Record `json:"jobs"`
			TotalUSD    float64      `json:"total_usd"`
			UnpricedJob int          `json:"unpriced_jobs"`
		}
		return emitJSON(c, out{recs, total, unknown})
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "JOB\tRUN\tOP\tROUTE\tSTATUS\tCOST\tOUT")
	for _, r := range recs {
		cost := "?"
		if r.Cost.ActualUSD != nil {
			cost = fmt.Sprintf("$%.4g", *r.Cost.ActualUSD)
		} else if r.Cost.EstimatedUSD != nil {
			cost = fmt.Sprintf("~$%.4g", *r.Cost.EstimatedUSD)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.JobID, r.Run, r.Op, r.RouteID, r.Status, cost, r.OutPath)
	}
	fmt.Fprintf(w, "\tTOTAL\t\t\t\t$%.4g\t(%d unpriced)\n", total, unknown)
	return w.Flush()
}

func runJobsFetch(c *cobra.Command, a []string) error {
	rec, err := job.Load(a[0])
	if err != nil {
		return err
	}
	if out := flagStr(c, "out"); out != "" {
		rec.OutPath = out
	}
	l, err := registry.Load()
	if err != nil {
		return err
	}
	route, ok := l.Find(rec.RouteID)
	if !ok {
		route = registry.Route{ID: rec.RouteID, Provider: rec.Provider, ModelID: rec.ModelID, Op: rec.Op}
	}
	ad, err := adapter.For(route)
	if err != nil {
		return err
	}
	opts := job.Options{
		PollInterval: time.Duration(flagInt(c, "poll-interval-secs")) * time.Second,
		MaxWait:      time.Duration(flagInt(c, "max-wait-secs")) * time.Second,
	}
	if rec.Status == job.StatusDone {
		got, err := job.Refetch(rec)
		if eerr := emitJSON(c, got); eerr != nil {
			return eerr
		}
		return err
	}
	if job.Terminal(rec.Status) {
		return emitJSON(c, rec)
	}
	rec.Status = job.StatusRunning
	got, err := job.Wait(context.Background(), ad, route, rec, opts, time.Time{})
	if got != nil {
		if eerr := emitJSON(c, got); eerr != nil {
			return eerr
		}
	}
	return err
}
