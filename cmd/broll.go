package cmd

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/radjathaher/creative-cli/internal/broll"
	"github.com/radjathaher/creative-cli/internal/registry"
	"github.com/spf13/cobra"
)

func init() {
	register(func(root *cobra.Command) {
		root.AddCommand(newBrollSearchCmd(), newBrollGetCmd())
	})
}

func newBrollSearchCmd() *cobra.Command {
	c := &cobra.Command{
		Use: registry.OpBrollSearch, Short: "Search free stock footage on one explicit provider.",
		Long: "Search one provider, with at most one API request. Duration and orientation filters may return fewer than --limit results. Pond5 is unavailable until its public-domain API and per-item licence can be verified.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			options := broll.SearchOptions{
				Query: flagStr(c, "query"), Orientation: flagStr(c, "orientation"), Quality: flagStr(c, "quality"),
				MinDuration: flagFloat(c, "min-duration"), MaxDuration: flagFloat(c, "max-duration"), Limit: flagInt(c, "limit"),
			}
			results, err := broll.NewClient().Search(c.Context(), flagStr(c, "provider"), options)
			if err != nil {
				return err
			}
			if !flagBool(c, "table") {
				return emitBrollJSON(c, results)
			}
			return printBrollTable(c, results)
		},
	}
	addBrollFlags(c)
	f := c.Flags()
	f.String("query", "", "search keywords")
	f.String("orientation", "", "portrait | landscape | square")
	f.Float64("min-duration", 0, "minimum duration in seconds")
	f.Float64("max-duration", 0, "maximum duration in seconds; 0 means no maximum")
	f.Int("limit", 20, "maximum results (Pexels: 1–80; Pixabay: 1–200)")
	f.Bool("table", false, "print a human table instead of JSON")
	return c
}

func newBrollGetCmd() *cobra.Command {
	c := &cobra.Command{
		Use: registry.OpBrollGet + " <id>", Short: "Download a stock MP4 and its licence sidecar.",
		Long: "Download a provider ID or provider-prefixed ID, such as pexels:12345. Writes FILE and FILE.json; refuses to overwrite either file. Does not imply advertising clearance.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if flagStr(c, "out") == "" {
				return fmt.Errorf("--out is required")
			}
			client := broll.NewClient()
			r, err := client.Get(c.Context(), flagStr(c, "provider"), args[0], flagStr(c, "quality"))
			if err != nil {
				return err
			}
			result, err := client.Download(c.Context(), r, flagStr(c, "out"))
			if err != nil {
				return err
			}
			return emitBrollJSON(c, result)
		},
	}
	addBrollFlags(c)
	c.Flags().StringP("out", "o", "", "destination MP4; provenance goes to FILE.json")
	return c
}

func addBrollFlags(c *cobra.Command) {
	c.Flags().String("provider", "", "pexels | pixabay | pond5-pd (unavailable); never selected silently")
	c.Flags().String("quality", "hd", "maximum MP4 size: sd (960), hd (1920), 4k (4096), best (unlimited), longest edge in pixels")
}

func emitBrollJSON(c *cobra.Command, value any) error {
	enc := json.NewEncoder(c.OutOrStdout())
	enc.SetEscapeHTML(false)
	if readCommon(c).pretty {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(value)
}

func printBrollTable(c *cobra.Command, results []broll.Result) error {
	w := tabwriter.NewWriter(c.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTITLE\tSECONDS\tSIZE\tORIENTATION\tCOST\tLICENCE\tAUTHOR\tSOURCE")
	for _, r := range results {
		fmt.Fprintf(w, "%s\t%s\t%g\t%dx%d\t%s\t$0\t%s\t%s\t%s\n",
			r.ID, short(r.Title, 48), r.DurationS, r.Width, r.Height, r.Orientation, r.Licence, short(r.Author, 24), r.PageURL)
	}
	return w.Flush()
}
