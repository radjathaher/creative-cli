package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbeVideoParsesFfprobeOutput(t *testing.T) {
	binDir := t.TempDir()
	ffprobe := filepath.Join(binDir, "ffprobe")
	script := `#!/bin/sh
printf '%s\n' '{"streams":[{"width":1280,"height":720}],"format":{"duration":"5.125"}}'
`
	if err := os.WriteFile(ffprobe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	metadata, err := ProbeVideo("input.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Width != 1280 || metadata.Height != 720 || metadata.DurationSeconds != 5.125 {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
}
