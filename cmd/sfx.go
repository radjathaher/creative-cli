package cmd

import (
	"fmt"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/provider/elevenlabs"
	"github.com/spf13/cobra"
)

func init() {
	register(func(root *cobra.Command) {
		c := &cobra.Command{
			Use:   "sfx",
			Short: "Generate a sound effect from a text prompt via ElevenLabs.",
			Args:  cobra.NoArgs,
			RunE:  runSfx,
		}
		c.Flags().String("prompt", "", "sound effect description (required)")
		c.Flags().Float64("duration-seconds", 0, "target length in seconds (optional; model auto-selects when unset)")
		addOutFlag(c)
		addProviderFlag(c, "elevenlabs")
		root.AddCommand(c)
	})
}

func runSfx(cmd *cobra.Command, _ []string) error {
	common := readCommon(cmd)
	prompt := flagStr(cmd, "prompt")
	if prompt == "" {
		return fmt.Errorf("--prompt is required")
	}
	out := flagStr(cmd, "out")
	if out == "" {
		return fmt.Errorf("--out is required")
	}

	var duration *float64
	if cmd.Flags().Changed("duration-seconds") {
		v, _ := cmd.Flags().GetFloat64("duration-seconds")
		duration = core.F(v)
	}

	client, err := elevenlabs.New()
	if err != nil {
		return err
	}
	started := time.Now()
	core.Progress("generating sound effect")
	res, err := client.Sfx(elevenlabs.SfxParams{
		Text:            prompt,
		DurationSeconds: duration,
	})
	if err != nil {
		return err
	}
	if err := core.WriteFile(out, res.Bytes); err != nil {
		return err
	}
	elapsed := roundSecs(started)
	core.Progress("done in %.3fs", elapsed)

	env := &core.Envelope{
		Provider:       "elevenlabs",
		Endpoint:       res.Endpoint,
		Input:          &core.InputInfo{Kind: "prompt", Source: prompt},
		Output:         "audio",
		ElapsedSeconds: elapsed,
		Out:            out,
	}
	return core.Emit(env, nil, common.pretty, common.raw)
}
