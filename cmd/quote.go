package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/radjathaher/creative-cli/internal/account"
	"github.com/radjathaher/creative-cli/internal/quote"
	"github.com/radjathaher/creative-cli/internal/registry"
	"github.com/spf13/cobra"
)

func init() {
	register(func(root *cobra.Command) {
		c := &cobra.Command{
			Use:   "quote <op>",
			Short: "Rank every route that can run a job by estimated price (never runs it).",
			Long: `Rank every route that can run a job, cheapest first, with the price source,
its date, and whether the route is usable now (credentials and balance).

Routes that cannot accept the job's inputs (too long, wrong resolution, video
references on a route without them) are listed under "excluded" with the reason.
The quote never picks a route; pass --provider on the op command to run one.`,
			Example: `  creative quote video.r2v --family seedance-2.5 --res 480p --duration 16 --video ref.mp4 --table
  creative quote image.t2i --family gpt-image-2.5 --variant flare --res 1k --table`,
			Args: cobra.ExactArgs(1),
			RunE: runQuote,
		}
		addRouteFlags(c)
		addJobFlags(c)
		c.Flags().Bool("table", false, "print a human table instead of JSON")
		c.Flags().Bool("no-balance", false, "skip the live balance lookups")
		root.AddCommand(c)
	})
}

func runQuote(c *cobra.Command, args []string) error {
	l, err := registry.Load()
	if err != nil {
		return err
	}
	res := quote.Build(l, readQuoteRequest(c, args[0]), statusMap(!flagBool(c, "no-balance")))
	if len(res.Routes) == 0 && len(res.Excluded) == 0 {
		return fmt.Errorf("no routes for %s%s; try `creative models %s --all`", args[0], famSuffix(res.Family), args[0])
	}
	if flagBool(c, "table") {
		printQuoteTable(res)
		return nil
	}
	return emitJSON(c, res)
}

func famSuffix(f string) string {
	if f == "" {
		return ""
	}
	return " --family " + f
}

func statusMap(withBalance bool) map[string]account.Status {
	m := map[string]account.Status{}
	for _, s := range account.CheckAll(context.Background(), withBalance) {
		m[s.Provider] = s
	}
	return m
}

func printQuoteTable(res quote.Result) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "#\tPROVIDER\tMODEL\tCOST\tBASIS\tSOURCE\tAS OF\tUSABLE")
	for _, r := range res.Routes {
		cost := "?"
		if r.Estimate.USD != nil {
			cost = fmt.Sprintf("$%.4g", *r.Estimate.USD)
		}
		usable := "✓"
		if !r.Usable {
			usable = "✗ " + r.Why
		}
		if !r.Verified {
			usable += " (unverified route)"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Rank, r.Provider, short(r.ModelID, 44), cost,
			short(r.Estimate.Basis, 38), r.PriceSource, r.PriceAsOf, usable)
	}
	w.Flush()
	for _, e := range res.Excluded {
		fmt.Printf("  excluded %s: %s\n", e.RouteID, e.Reason)
	}
	if res.CheapestUsable != "" {
		fmt.Printf("→ cheapest usable now: %s\n", res.CheapestUsable)
	}
	if res.Stale {
		fmt.Printf("⚠ prices are older than 7 days (catalog %s); run `creative sync` to refresh\n", res.CatalogAsOf)
	}
}

func short(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
