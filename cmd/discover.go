package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/radjathaher/creative-cli/internal/registry"
	"github.com/spf13/cobra"
)

func init() {
	register(func(root *cobra.Command) {
		ops := &cobra.Command{
			Use:   "ops",
			Short: "List every operation with its route and family counts.",
			Args:  cobra.NoArgs,
			RunE:  runOps,
		}
		ops.Flags().Bool("all", false, "count families outside the allowlist too")
		ops.Flags().Bool("table", false, "print a human table instead of JSON")

		models := &cobra.Command{
			Use:   "models <op>",
			Short: "List the families and providers that serve an operation.",
			Args:  cobra.ExactArgs(1),
			RunE:  runModels,
		}
		addRouteFlags(models)
		models.Flags().Bool("table", false, "print a human table instead of JSON")

		describe := &cobra.Command{
			Use:   "describe <route-id>",
			Short: "Show one route: price, limits, flag mapping, and input schema.",
			Args:  cobra.ExactArgs(1),
			RunE:  runDescribe,
		}
		root.AddCommand(ops, models, describe)
	})
}

type opSummary struct {
	Op        string   `json:"op"`
	Routes    int      `json:"routes"`
	Families  int      `json:"families"`
	Providers []string `json:"providers"`
}

func runOps(c *cobra.Command, _ []string) error {
	l, err := registry.Load()
	if err != nil {
		return err
	}
	var out []opSummary
	for _, op := range registry.Ops {
		routes := l.Select(op, "", "", "", flagBool(c, "all"))
		if len(routes) == 0 {
			continue
		}
		fams, provs := map[string]bool{}, map[string]bool{}
		for _, r := range routes {
			fams[r.Family], provs[r.Provider] = true, true
		}
		out = append(out, opSummary{op, len(routes), len(fams), keys(provs)})
	}
	if !flagBool(c, "table") {
		return emitJSON(c, out)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "OP\tROUTES\tFAMILIES\tPROVIDERS")
	for _, s := range out {
		fmt.Fprintf(w, "%s\t%d\t%d\t%v\n", s.Op, s.Routes, s.Families, s.Providers)
	}
	return w.Flush()
}

type modelRow struct {
	Family    string   `json:"family"`
	Variants  []string `json:"variants,omitempty"`
	Providers []string `json:"providers"`
	RouteIDs  []string `json:"route_ids"`
}

func runModels(c *cobra.Command, args []string) error {
	l, err := registry.Load()
	if err != nil {
		return err
	}
	byFam := map[string]*modelRow{}
	for _, r := range l.Select(args[0], flagStr(c, "family"), flagStr(c, "variant"), flagStr(c, "provider"), flagBool(c, "all")) {
		m := byFam[r.Family]
		if m == nil {
			m = &modelRow{Family: r.Family}
			byFam[r.Family] = m
		}
		m.RouteIDs = append(m.RouteIDs, r.ID)
		m.Providers = appendUnique(m.Providers, r.Provider)
		if r.Variant != "" {
			m.Variants = appendUnique(m.Variants, r.Variant)
		}
	}
	rows := make([]modelRow, 0, len(byFam))
	for _, m := range byFam {
		rows = append(rows, *m)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Family < rows[j].Family })
	if !flagBool(c, "table") {
		return emitJSON(c, rows)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FAMILY\tVARIANTS\tPROVIDERS\tROUTES")
	for _, m := range rows {
		fmt.Fprintf(w, "%s\t%v\t%v\t%d\n", m.Family, m.Variants, m.Providers, len(m.RouteIDs))
	}
	return w.Flush()
}

func runDescribe(c *cobra.Command, args []string) error {
	l, err := registry.Load()
	if err != nil {
		return err
	}
	r, ok := l.Find(args[0])
	if !ok {
		return fmt.Errorf("unknown route %q; list routes with `creative models <op> --all`", args[0])
	}
	return emitJSON(c, r)
}

func emitJSON(c *cobra.Command, v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if readCommon(c).pretty {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func appendUnique(xs []string, x string) []string {
	for _, v := range xs {
		if v == x {
			return xs
		}
	}
	return append(xs, x)
}
