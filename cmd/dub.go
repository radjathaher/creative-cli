package cmd

import (
	"fmt"

	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/provider/elevenlabs"
	"github.com/spf13/cobra"
)

func init() {
	register(func(root *cobra.Command) {
		c := &cobra.Command{
			Use:   "dub <input>",
			Short: "Dub a video or audio file into another language via ElevenLabs.",
			Args:  cobra.ExactArgs(1),
			RunE:  runDub,
		}
		c.Flags().String("language", "", "target language ISO code (required)")
		addOutFlag(c)
		addAsyncFlags(c)
		addProviderFlag(c, "elevenlabs")
		root.AddCommand(c)
	})
}

func runDub(cmd *cobra.Command, args []string) error {
	common := readCommon(cmd)
	lang := flagStr(cmd, "language")
	if lang == "" {
		return fmt.Errorf("--language is required")
	}

	client, err := elevenlabs.New()
	if err != nil {
		return err
	}
	env, raw, err := client.Dub(elevenlabs.DubOpts{
		Input:            args[0],
		TargetLang:       lang,
		Out:              flagStr(cmd, "out"),
		NoWait:           flagBool(cmd, "no-wait"),
		PollIntervalSecs: flagInt(cmd, "poll-interval-secs"),
		MaxWaitSecs:      flagInt(cmd, "max-wait-secs"),
	}, common.pretty, common.raw)
	if err != nil {
		return err
	}
	return core.Emit(env, raw, common.pretty, common.raw)
}
