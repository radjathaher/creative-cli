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
			Use:   "speech",
			Short: "Synthesize speech from text via ElevenLabs text-to-speech.",
			Args:  cobra.NoArgs,
			RunE:  runSpeech,
		}
		c.Flags().String("text", "", "text to speak (required)")
		c.Flags().String("voice-id", "JBFqnCBsd6RMkjVDRZzb", "ElevenLabs voice id")
		c.Flags().String("model", "eleven_v3", "ElevenLabs model id")
		c.Flags().String("output-format", "mp3_44100_128", "audio output format")
		addOutFlag(c)
		addProviderFlag(c, "elevenlabs")
		root.AddCommand(c)
	})
}

func runSpeech(cmd *cobra.Command, _ []string) error {
	common := readCommon(cmd)
	text := flagStr(cmd, "text")
	if text == "" {
		return fmt.Errorf("--text is required")
	}
	out := flagStr(cmd, "out")
	if out == "" {
		return fmt.Errorf("--out is required")
	}

	client, err := elevenlabs.New()
	if err != nil {
		return err
	}
	started := time.Now()
	core.Progress("synthesizing speech with %s", flagStr(cmd, "model"))
	res, err := client.Speech(elevenlabs.SpeechParams{
		Text:         text,
		VoiceID:      flagStr(cmd, "voice-id"),
		Model:        flagStr(cmd, "model"),
		OutputFormat: flagStr(cmd, "output-format"),
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
		Model:          flagStr(cmd, "model"),
		Input:          &core.InputInfo{Kind: "prompt", Source: text},
		Output:         "audio",
		ElapsedSeconds: elapsed,
		Out:            out,
	}
	return core.Emit(env, nil, common.pretty, common.raw)
}
