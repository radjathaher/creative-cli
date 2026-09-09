package cmd

import (
	"fmt"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
	"github.com/radjathaher/creative-cli/internal/provider/openai"
	"github.com/spf13/cobra"
)

func init() {
	register(func(root *cobra.Command) {
		c := &cobra.Command{
			Use:   "image",
			Short: "Generate or edit an image via GPT Image 2.5 (add --image REF for img2img).",
			Args:  cobra.NoArgs,
			RunE:  runImage,
		}
		c.Flags().String("prompt", "", "text prompt (required)")
		c.Flags().StringArray("image", nil, "reference image for img2img (path or url), repeatable")
		c.Flags().String("model", "gpt-image-2.5-sunburst", "OpenAI image model id")
		c.Flags().String("size", "auto", "1024x1024 | 1536x1024 | 1024x1536 | auto")
		c.Flags().String("quality", "auto", "low | medium | high | xhigh | max | auto")
		c.Flags().String("background", "", "transparent | opaque | auto")
		c.Flags().String("output-format", "png", "png | jpeg | webp")
		c.Flags().Int("n", 1, "number of images (the first is written to --out)")
		c.Flags().Duration("timeout", 10*time.Minute, "total image request timeout, including fallback and downloads")
		addOutFlag(c)
		root.AddCommand(c)
	})
}

func runImage(cmd *cobra.Command, _ []string) error {
	common := readCommon(cmd)
	prompt := flagStr(cmd, "prompt")
	if prompt == "" {
		return fmt.Errorf("--prompt is required")
	}
	out := flagStr(cmd, "out")
	if out == "" {
		return fmt.Errorf("--out is required")
	}
	refs := flagStrs(cmd, "image")
	timeout, err := cmd.Flags().GetDuration("timeout")
	if err != nil || timeout <= 0 {
		return fmt.Errorf("--timeout must be a positive duration")
	}

	started := time.Now()
	if len(refs) > 0 {
		core.Progress("editing image (img2img) with %d ref(s)", len(refs))
	} else {
		core.Progress("generating image with %s", flagStr(cmd, "model"))
	}

	res, err := openai.Generate(openai.ImageParams{
		Context:      cmd.Context(),
		Timeout:      timeout,
		Prompt:       prompt,
		Model:        flagStr(cmd, "model"),
		Size:         flagStr(cmd, "size"),
		Quality:      flagStr(cmd, "quality"),
		Background:   flagStr(cmd, "background"),
		OutputFormat: flagStr(cmd, "output-format"),
		N:            flagInt(cmd, "n"),
		Refs:         refs,
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
		Provider:       res.Provider,
		Endpoint:       res.Endpoint,
		Model:          res.Model,
		Input:          &core.InputInfo{Kind: inputKind(refs), Source: prompt},
		Output:         "image",
		Usage:          res.Usage,
		ElapsedSeconds: elapsed,
		Out:            out,
	}
	return core.Emit(env, res.Raw, common.pretty, common.raw)
}
