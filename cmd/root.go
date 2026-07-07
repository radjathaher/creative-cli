package cmd

import (
	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/spf13/cobra"
)

// registrations holds one attach-func per verb. Each cmd/*.go registers itself
// from its init(), so adding a verb never edits a shared list — keeping parallel
// development conflict-free.
var registrations []func(*cobra.Command)

func register(fn func(*cobra.Command)) { registrations = append(registrations, fn) }

// NewRootCmd assembles the `creative` command from every registered verb.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "creative",
		Short:         "Unified media-creation CLI for agents: make, transform, and analyze image/video/audio.",
		Long:          agentGuide,
		Version:       core.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().Bool("pretty", false, "pretty-print JSON output")
	root.PersistentFlags().Bool("raw", false, "emit the untouched provider payload instead of the envelope")
	root.SetVersionTemplate("creative {{.Version}}\n")
	for _, fn := range registrations {
		fn(root)
	}
	return root
}
