package core

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type videoMetadata struct {
	Width           int
	Height          int
	DurationSeconds float64
}

// ProbeVideo reads the dimensions and duration of a local or remote video.
func ProbeVideo(input string) (videoMetadata, error) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return videoMetadata{}, fmt.Errorf("ffprobe is required for video upscaling: %w", err)
	}
	cmd := exec.Command(
		"ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height:format=duration", "-of", "json", input,
	)
	raw, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return videoMetadata{}, fmt.Errorf("ffprobe %s: %s", input, strings.TrimSpace(string(exit.Stderr)))
		}
		return videoMetadata{}, fmt.Errorf("ffprobe %s: %w", input, err)
	}
	var result struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return videoMetadata{}, fmt.Errorf("decode ffprobe output: %w", err)
	}
	if len(result.Streams) == 0 || result.Streams[0].Width <= 0 || result.Streams[0].Height <= 0 {
		return videoMetadata{}, fmt.Errorf("ffprobe found no video dimensions in %s", input)
	}
	duration, err := strconv.ParseFloat(result.Format.Duration, 64)
	if err != nil || duration <= 0 {
		return videoMetadata{}, fmt.Errorf("ffprobe found no video duration in %s", input)
	}
	return videoMetadata{
		Width:           result.Streams[0].Width,
		Height:          result.Streams[0].Height,
		DurationSeconds: duration,
	}, nil
}
