package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/radjathaher/creative-cli/internal/account"
	"github.com/spf13/cobra"
)

func init() {
	register(func(root *cobra.Command) {
		c := &cobra.Command{
			Use:   "keys",
			Short: "Show which provider credentials are configured and their live balances.",
			Long: `Show which provider credentials resolve (env var first, then /run/secrets/<NAME>)
and, where the provider has a free read-only endpoint, the remaining balance.
Secret values are never printed.`,
			Args: cobra.NoArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				st := account.CheckAll(context.Background(), !flagBool(c, "no-balance"))
				if !flagBool(c, "table") {
					return emitJSON(c, st)
				}
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "PROVIDER\tKEY\tBALANCE\tNOTE")
				for _, s := range st {
					key := "✓"
					if !s.Configured {
						key = "✗ missing " + strings.Join(s.Missing, ", ")
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Provider, key, s.Balance, s.Note)
				}
				return w.Flush()
			},
		}
		c.Flags().Bool("table", false, "print a human table instead of JSON")
		c.Flags().Bool("no-balance", false, "skip the live balance lookups")
		root.AddCommand(c)
	})
}
