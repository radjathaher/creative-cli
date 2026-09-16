package cmd

import (
	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/provider/falpipe"
	"github.com/spf13/cobra"
)

func init() {
	register(func(root *cobra.Command) {
		c := &cobra.Command{
			Use:   "upscale <input>",
			Short: "Upscale a video to 1080p/2K/4K via fal (bytedance | topaz | flashvsr | seedvr).",
			Args:  cobra.ExactArgs(1),
			RunE:  runUpscale,
		}
		c.Flags().String("model", "bytedance", "bytedance | topaz | flashvsr | seedvr")
		c.Flags().String("target", "1080p", "720p | 1080p | 2k | 4k")
		c.Flags().Int("fps", 30, "target fps (topaz: 16-60; others: 30 | 60)")
		c.Flags().String("bytedance-preset", "aigc", "general | ugc | short_series | aigc | old_film")
		c.Flags().String("bytedance-tier", "standard", "fast | standard | pro")
		c.Flags().String("bytedance-fidelity", "high", "high | medium")
		c.Flags().String("topaz-model", "Proteus", "Topaz model name, e.g. Proteus, \"Gaia HQ\", \"Starlight Fast 2\"")
		c.Flags().Float64("topaz-factor", 0, "Topaz explicit upscale factor 1.0-4.0 (overrides --target)")
		c.Flags().Float64("topaz-recover-detail", -1, "Topaz recover_detail 0-1 (default: model default)")
		addOutFlag(c)
		addAsyncFlags(c)
		addProviderFlag(c, "fal")
		root.AddCommand(c)
	})
}

func runUpscale(cmd *cobra.Command, args []string) error {
	common := readCommon(cmd)
	env, raw, err := falpipe.Upscale(falpipe.UpscaleOpts{
		Input:              args[0],
		Model:              flagStr(cmd, "model"),
		Target:             flagStr(cmd, "target"),
		Fps:                flagInt(cmd, "fps"),
		Out:                flagStr(cmd, "out"),
		NoWait:             flagBool(cmd, "no-wait"),
		PollIntervalSecs:   flagInt(cmd, "poll-interval-secs"),
		MaxWaitSecs:        flagInt(cmd, "max-wait-secs"),
		BytedancePreset:    flagStr(cmd, "bytedance-preset"),
		BytedanceTier:      flagStr(cmd, "bytedance-tier"),
		BytedanceFidelity:  flagStr(cmd, "bytedance-fidelity"),
		TopazModel:         flagStr(cmd, "topaz-model"),
		TopazFactor:        flagFloat(cmd, "topaz-factor"),
		TopazRecoverDetail: flagFloat(cmd, "topaz-recover-detail"),
	}, common.pretty, common.raw)
	if err != nil {
		return err
	}
	return core.Emit(env, raw, common.pretty, common.raw)
}
