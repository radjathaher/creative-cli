package cmd

import (
	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/provider/falpipe"
	"github.com/spf13/cobra"
)

func init() {
	register(func(root *cobra.Command) {
		c := &cobra.Command{
			Use:   "understand <video>",
			Short: "Describe a video as a recreation brief via fal's openrouter/router/video (Gemini).",
			Args:  cobra.ExactArgs(1),
			RunE:  runUnderstand,
		}
		c.Flags().String("model", "google/gemini-2.5-flash", "OpenRouter model id")
		c.Flags().Float64("temperature", 1.0, "sampling temperature (0-2)")
		c.Flags().Int("max-output-tokens", 4096, "max output tokens")
		c.Flags().String("prompt", "", "extra instruction appended to the brief prompt")
		addAsyncFlags(c)
		root.AddCommand(c)
	})
}

func runUnderstand(cmd *cobra.Command, args []string) error {
	common := readCommon(cmd)
	temperature, _ := cmd.Flags().GetFloat64("temperature")
	env, raw, err := falpipe.Understand(falpipe.UnderstandOpts{
		Input:            args[0],
		Model:            flagStr(cmd, "model"),
		Temperature:      temperature,
		MaxOutputTokens:  flagInt(cmd, "max-output-tokens"),
		Prompt:           flagStr(cmd, "prompt"),
		NoWait:           flagBool(cmd, "no-wait"),
		PollIntervalSecs: flagInt(cmd, "poll-interval-secs"),
		MaxWaitSecs:      flagInt(cmd, "max-wait-secs"),
	}, common.pretty, common.raw)
	if err != nil {
		return err
	}
	return core.Emit(env, raw, common.pretty, common.raw)
}
