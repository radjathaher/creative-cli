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
			Use:   "voice",
			Short: "Clone a voice from audio samples via ElevenLabs instant voice cloning.",
			Args:  cobra.NoArgs,
			RunE:  runVoice,
		}
		c.Flags().String("name", "", "name for the cloned voice (required)")
		c.Flags().StringArray("audio", nil, "voice sample (path or url), repeatable; at least one required")
		addProviderFlag(c, "elevenlabs")
		root.AddCommand(c)
	})
}

func runVoice(cmd *cobra.Command, _ []string) error {
	common := readCommon(cmd)
	name := flagStr(cmd, "name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	samples := flagStrs(cmd, "audio")
	if len(samples) == 0 {
		return fmt.Errorf("at least one --audio sample is required")
	}

	client, err := elevenlabs.New()
	if err != nil {
		return err
	}
	started := time.Now()
	core.Progress("cloning voice %q from %d sample(s)", name, len(samples))
	parsed, raw, err := client.CloneVoice(elevenlabs.VoiceParams{
		Name:    name,
		Samples: samples,
	})
	if err != nil {
		return err
	}
	elapsed := roundSecs(started)
	core.Progress("done in %.3fs", elapsed)

	env := &core.Envelope{
		Provider:       "elevenlabs",
		Endpoint:       "/v1/voices/add",
		Input:          &core.InputInfo{Kind: "audio", Source: name},
		Output:         parsed,
		ElapsedSeconds: elapsed,
	}
	return core.Emit(env, raw, common.pretty, common.raw)
}
