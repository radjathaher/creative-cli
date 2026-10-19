package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/radjathaher/creative-cli/internal/adapter"
	_ "github.com/radjathaher/creative-cli/internal/adapter/elevenlabs"
	_ "github.com/radjathaher/creative-cli/internal/adapter/kie"
	_ "github.com/radjathaher/creative-cli/internal/adapter/openai"
	_ "github.com/radjathaher/creative-cli/internal/adapter/segmind"
	"github.com/radjathaher/creative-cli/internal/job"
	"github.com/radjathaher/creative-cli/internal/quote"
	"github.com/radjathaher/creative-cli/internal/registry"
	"github.com/spf13/cobra"
)

// transformOps take the media they transform as a positional INPUT argument.
var transformOps = map[string]bool{
	registry.OpVideoUpscale: true, registry.OpImageUpscale: true, registry.OpImageBGRemove: true,
	registry.OpSTT: true, registry.OpCaptionBurn: true, registry.OpVoiceDub: true,
	registry.OpVoiceIsolate: true, registry.OpVoiceChange: true, registry.OpUnderstandVid: true,
	registry.OpUnderstandImg: true, registry.OpUnderstandAud: true, registry.OpMusicStems: true,
	registry.OpSFXFromVideo: true, registry.Op3DRig: true, registry.Op3DAnimate: true,
	registry.Op3DTexture: true, registry.Op3DConvert: true, registry.OpVideoEdit: true,
	registry.OpVideoExtend: true, registry.OpVideoLipsync: true,
}

func init() {
	register(func(root *cobra.Command) {
		for _, op := range registry.Ops {
			if op == registry.OpOther {
				continue
			}
			root.AddCommand(newOpCmd(op))
		}
	})
}

func newOpCmd(op string) *cobra.Command {
	use := op
	args := cobra.NoArgs
	if transformOps[op] {
		use, args = op+" [INPUT]", cobra.MaximumNArgs(1)
	}
	c := &cobra.Command{
		Use:   use,
		Short: "Run " + op + " on one explicit route (see `creative quote " + op + "`).",
		Long: `Run ` + op + ` on exactly one route. Routes are never chosen silently: if the
family has more than one route, pass --provider (the error lists the routes with
prices). The job is saved before polling, so --no-wait and later
` + "`creative jobs fetch <id>`" + ` are always safe.`,
		Args: args,
		RunE: func(c *cobra.Command, a []string) error { return runOp(c, op, a) },
	}
	addRouteFlags(c)
	addJobFlags(c)
	f := c.Flags()
	f.String("prompt", "", "text prompt")
	f.String("prompt-file", "", "read the prompt from this file (hashed into the job record)")
	f.String("text", "", "text to speak (voice.tts)")
	f.String("first-frame", "", "starting frame image (path or url)")
	f.String("last-frame", "", "ending frame image (path or url)")
	f.Int("seed", -1, "seed; -1 lets the provider choose")
	f.Bool("no-generate-audio", false, "disable provider-generated synchronized audio")
	f.StringArray("param", nil, "provider field passthrough key=value, validated against the route schema (repeatable)")
	f.String("run", "", "group this job under a run name (for --max-run-cost and `jobs list --run`)")
	f.Float64("max-cost", 0, "refuse to submit when the estimate exceeds this many USD")
	f.Float64("max-run-cost", 0, "refuse to submit when the run's total would exceed this many USD")
	f.Bool("dry-run", false, "print the resolved route, estimate, and payload; submit nothing")
	f.StringP("out", "o", "", "write the result here (several outputs get -2, -3 suffixes)")
	f.Bool("no-wait", false, "submit, save the job, and return without polling")
	f.Int("poll-interval-secs", 5, "seconds between status polls")
	f.Int("max-wait-secs", 1800, "max seconds to wait; the job keeps running and can be fetched later")
	return c
}

func runOp(c *cobra.Command, op string, args []string) error {
	l, err := registry.Load()
	if err != nil {
		return err
	}
	qr := readQuoteRequest(c, op)
	res := quote.Build(l, qr, statusMap(false))
	route, cand, err := pickRoute(l, res, qr)
	if err != nil {
		return err
	}
	a, err := adapter.For(route)
	if err != nil {
		return err
	}
	req, err := readJobRequest(c, args)
	if err != nil {
		return err
	}
	out := flagStr(c, "out")
	if out == "" && !flagBool(c, "dry-run") && !flagBool(c, "no-wait") {
		return fmt.Errorf("--out is required (or pass --no-wait and fetch later)")
	}
	opts := job.Options{
		Run: flagStr(c, "run"), Out: out, NoWait: flagBool(c, "no-wait"), DryRun: flagBool(c, "dry-run"),
		PollInterval: time.Duration(flagInt(c, "poll-interval-secs")) * time.Second,
		MaxWait:      time.Duration(flagInt(c, "max-wait-secs")) * time.Second,
		MaxCost:      flagFloat(c, "max-cost"), MaxRunCost: flagFloat(c, "max-run-cost"),
		PricesAsOf: cand.PriceAsOf,
	}
	rec, err := job.Start(context.Background(), a, route, req, cand.Estimate, opts)
	if errors.Is(err, job.ErrDryRun) {
		return emitJSON(c, rec)
	}
	if rec != nil {
		if eerr := emitJSON(c, rec); eerr != nil {
			return eerr
		}
	}
	return err
}

// pickRoute enforces explicit routing: exactly one candidate must remain.
func pickRoute(l *registry.Loaded, res quote.Result, qr quote.Request) (registry.Route, quote.Candidate, error) {
	switch len(res.Routes) {
	case 0:
		msg := fmt.Sprintf("no route for %s", qr.Op)
		for _, e := range res.Excluded {
			msg += fmt.Sprintf("\n  excluded %s: %s", e.RouteID, e.Reason)
		}
		return registry.Route{}, quote.Candidate{}, errors.New(msg + "\nlist options with `creative models " + qr.Op + " --all`")
	case 1:
		r, _ := l.Find(res.Routes[0].RouteID)
		return r, res.Routes[0], nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d routes match; choose one with --provider (and --family/--variant):", len(res.Routes))
	for _, c := range res.Routes {
		cost := "price unknown"
		if c.Estimate.USD != nil {
			cost = fmt.Sprintf("$%.4g", *c.Estimate.USD)
		}
		fmt.Fprintf(&b, "\n  %-10s %-48s %s", c.Provider, c.RouteID, cost)
	}
	return registry.Route{}, quote.Candidate{}, errors.New(b.String())
}

func readJobRequest(c *cobra.Command, args []string) (job.Request, error) {
	req := job.Request{
		Prompt: flagStr(c, "prompt"), Text: flagStr(c, "text"),
		Images: flagStrs(c, "image"), Videos: flagStrs(c, "video"), Audios: flagStrs(c, "audio"),
		FirstFrame: flagStr(c, "first-frame"), LastFrame: flagStr(c, "last-frame"),
		Duration: flagFloat(c, "duration"), Resolution: flagStr(c, "res"), Aspect: flagStr(c, "aspect"),
		FPS: flagInt(c, "fps"), Quality: flagStr(c, "quality"), Count: flagInt(c, "count"),
	}
	if len(args) == 1 {
		req.Input = args[0]
	}
	if pf := flagStr(c, "prompt-file"); pf != "" {
		b, err := os.ReadFile(pf)
		if err != nil {
			return req, err
		}
		req.Prompt = string(b)
	}
	if req.FirstFrame != "" && len(req.Images) > 0 {
		return req, fmt.Errorf("--first-frame cannot be combined with --image references")
	}
	if s := flagInt(c, "seed"); s >= 0 {
		req.Seed = &s
	}
	if flagBool(c, "no-generate-audio") {
		f := false
		req.GenerateAudio = &f
	}
	params, err := parseParams(flagStrs(c, "param"))
	if err != nil {
		return req, err
	}
	req.Params = params
	return req, nil
}

// parseParams turns key=value pairs into typed values: numbers and booleans
// become JSON numbers and booleans, everything else stays a string.
func parseParams(kvs []string) (map[string]any, error) {
	if len(kvs) == 0 {
		return nil, nil
	}
	out := map[string]any{}
	for _, kv := range kvs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--param %q must be key=value", kv)
		}
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			out[k] = n
		} else if b, err := strconv.ParseBool(v); err == nil {
			out[k] = b
		} else {
			out[k] = v
		}
	}
	return out, nil
}
