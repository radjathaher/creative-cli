package cmd

import (
	"math"
	"time"

	"github.com/spf13/cobra"
)

// commonOpts are the global JSON-output toggles shared by every verb.
type commonOpts struct {
	pretty bool
	raw    bool
}

func readCommon(cmd *cobra.Command) commonOpts {
	p, _ := cmd.Flags().GetBool("pretty")
	r, _ := cmd.Flags().GetBool("raw")
	return commonOpts{pretty: p, raw: r}
}

// addProviderFlag registers --provider with a per-verb default backend.
func addProviderFlag(cmd *cobra.Command, def string) {
	cmd.Flags().String("provider", def, "backend provider")
}

// addOutFlag registers the -o/--out artifact path.
func addOutFlag(cmd *cobra.Command) {
	cmd.Flags().StringP("out", "o", "", "write the resulting asset to this path")
}

// addAsyncFlags registers the poll/wait controls shared by queue-based verbs.
func addAsyncFlags(cmd *cobra.Command) {
	cmd.Flags().Int("poll-interval-secs", 5, "seconds between status polls")
	cmd.Flags().Int("max-wait-secs", 1200, "max seconds to wait for an async job")
	cmd.Flags().Bool("no-wait", false, "submit and return the request id without waiting")
}

// addRefFlags registers repeatable reference inputs accepted as local paths or URLs.
func addRefFlags(cmd *cobra.Command) {
	cmd.Flags().StringArray("image", nil, "reference image (path or url), repeatable")
	cmd.Flags().StringArray("video", nil, "reference video (path or url), repeatable")
	cmd.Flags().StringArray("audio", nil, "reference audio (path or url), repeatable")
}

func flagStr(cmd *cobra.Command, name string) string { v, _ := cmd.Flags().GetString(name); return v }
func flagInt(cmd *cobra.Command, name string) int    { v, _ := cmd.Flags().GetInt(name); return v }
func flagBool(cmd *cobra.Command, name string) bool  { v, _ := cmd.Flags().GetBool(name); return v }
func flagStrs(cmd *cobra.Command, name string) []string {
	v, _ := cmd.Flags().GetStringArray(name)
	return v
}

// roundSecs returns elapsed seconds since start rounded to milliseconds.
func roundSecs(start time.Time) float64 {
	return math.Round(time.Since(start).Seconds()*1000) / 1000
}

// inputKind labels an input as "image" when references are present, else "prompt".
func inputKind(refs []string) string {
	if len(refs) > 0 {
		return "image"
	}
	return "prompt"
}
