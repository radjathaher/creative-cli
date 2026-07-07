package cmd

import (
	"encoding/json"

	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/provider/falpipe"
	"github.com/radjathaher/creative-cli/internal/provider/zapcap"
	"github.com/spf13/cobra"
)

func init() {
	register(func(root *cobra.Command) {
		c := &cobra.Command{
			Use:   "caption <input>",
			Short: "Burn subtitles onto a video via fal VEED or ZapCap.",
			Args:  cobra.ExactArgs(1),
			RunE:  runCaption,
		}
		addProviderFlag(c, "veed")
		c.Flags().String("preset", "simple", "caption style preset (veed)")
		c.Flags().String("language", "", "source language override (veed: optional; zapcap: defaults en)")
		c.Flags().String("template-id", "", "caption template id (zapcap, required)")
		addOutFlag(c)
		addAsyncFlags(c)
		root.AddCommand(c)
	})
}

func runCaption(cmd *cobra.Command, args []string) error {
	common := readCommon(cmd)
	var (
		env *core.Envelope
		raw json.RawMessage
		err error
	)
	if flagStr(cmd, "provider") == "zapcap" {
		env, raw, err = zapcap.Render(zapcap.RenderOpts{
			Input:            args[0],
			TemplateID:       flagStr(cmd, "template-id"),
			Language:         flagStr(cmd, "language"),
			Out:              flagStr(cmd, "out"),
			NoWait:           flagBool(cmd, "no-wait"),
			PollIntervalSecs: flagInt(cmd, "poll-interval-secs"),
			MaxWaitSecs:      flagInt(cmd, "max-wait-secs"),
		}, common.pretty, common.raw)
	} else {
		env, raw, err = falpipe.Veed(falpipe.VeedOpts{
			Input:            args[0],
			Preset:           flagStr(cmd, "preset"),
			Language:         flagStr(cmd, "language"),
			Out:              flagStr(cmd, "out"),
			NoWait:           flagBool(cmd, "no-wait"),
			PollIntervalSecs: flagInt(cmd, "poll-interval-secs"),
			MaxWaitSecs:      flagInt(cmd, "max-wait-secs"),
		}, common.pretty, common.raw)
	}
	if err != nil {
		return err
	}
	return core.Emit(env, raw, common.pretty, common.raw)
}
