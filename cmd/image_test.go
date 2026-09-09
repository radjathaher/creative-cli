package cmd

import "testing"

func TestImageDefaultsToSunburst(t *testing.T) {
	imageCmd, _, err := NewRootCmd().Find([]string{"image"})
	if err != nil {
		t.Fatal(err)
	}
	model := imageCmd.Flags().Lookup("model")
	if model == nil {
		t.Fatal("image command has no --model flag")
	}
	if model.DefValue != "gpt-image-2.5-sunburst" {
		t.Fatalf("default image model = %q, want gpt-image-2.5-sunburst", model.DefValue)
	}
	quality := imageCmd.Flags().Lookup("quality")
	if quality == nil {
		t.Fatal("image command has no --quality flag")
	}
	if quality.Usage != "low | medium | high | xhigh | max | auto" {
		t.Fatalf("quality help = %q", quality.Usage)
	}
}
