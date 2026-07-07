package cmd

import (
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/provider/openai"
	"github.com/spf13/cobra"
)

func init() {
	register(func(root *cobra.Command) {
		c := &cobra.Command{
			Use:   "transcribe <input>",
			Short: "Transcribe or translate an audio/video file to text via OpenAI Whisper.",
			Args:  cobra.ExactArgs(1),
			RunE:  runTranscribe,
		}
		c.Flags().String("model", "whisper-1", "OpenAI audio model id")
		c.Flags().String("format", "text", "text | json | srt | vtt | verbose_json")
		c.Flags().String("language", "", "ISO language code hint (transcription only)")
		c.Flags().String("prompt", "", "prompt to guide model style/vocabulary")
		c.Flags().Float64("temperature", 0, "sampling temperature (sent only when provided)")
		c.Flags().Bool("translate", false, "translate to English instead of transcribing")
		addOutFlag(c)
		addProviderFlag(c, "openai")
		root.AddCommand(c)
	})
}

func runTranscribe(cmd *cobra.Command, args []string) error {
	common := readCommon(cmd)
	var temperature *float64
	if cmd.Flags().Changed("temperature") {
		v, _ := cmd.Flags().GetFloat64("temperature")
		temperature = core.F(v)
	}
	out := flagStr(cmd, "out")

	started := time.Now()
	if flagBool(cmd, "translate") {
		core.Progress("translating %s with %s", args[0], flagStr(cmd, "model"))
	} else {
		core.Progress("transcribing %s with %s", args[0], flagStr(cmd, "model"))
	}

	res, err := openai.Transcribe(openai.TranscribeParams{
		File:        args[0],
		Model:       flagStr(cmd, "model"),
		Format:      flagStr(cmd, "format"),
		Language:    flagStr(cmd, "language"),
		Prompt:      flagStr(cmd, "prompt"),
		Temperature: temperature,
		Translate:   flagBool(cmd, "translate"),
	})
	if err != nil {
		return err
	}
	elapsed := roundSecs(started)
	core.Progress("done in %.3fs", elapsed)

	env := &core.Envelope{
		Provider:       "openai",
		Endpoint:       res.Endpoint,
		Model:          res.Model,
		Input:          &core.InputInfo{Kind: "audio", Source: args[0]},
		Output:         res.Transcript,
		ElapsedSeconds: elapsed,
	}
	if out != "" {
		if err := core.WriteFile(out, []byte(res.Transcript)); err != nil {
			return err
		}
		env.Out = out
	}
	return core.Emit(env, res.Raw, common.pretty, common.raw)
}
