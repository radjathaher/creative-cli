package falpipe

import "testing"

func TestFactorForTargetUsesInputDimensions(t *testing.T) {
	tests := []struct {
		target        string
		width, height int
		want          float64
	}{
		{target: "1080p", width: 1280, height: 720, want: 1.5},
		{target: "1080p", width: 960, height: 540, want: 2},
		{target: "4k", width: 1920, height: 1080, want: 2},
		{target: "1080p", width: 720, height: 1280, want: 1.5},
	}
	for _, test := range tests {
		got, err := factorForTarget(test.target, test.width, test.height)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Errorf("factorForTarget(%q, %d, %d) = %v, want %v", test.target, test.width, test.height, got, test.want)
		}
	}
	if _, err := factorForTarget("720p", 1920, 1080); err == nil {
		t.Fatal("accepted a target smaller than the input")
	}
	if _, err := factorForTarget("4k", 640, 360); err == nil {
		t.Fatal("accepted a factor above fal's 4x limit")
	}
}

func TestTopazEstimateIncludesFpsAndModelDiscount(t *testing.T) {
	base := UpscaleOpts{Model: "topaz", Target: "1080p", Fps: 30, TopazModel: "Proteus"}
	assertCost(t, estimateCost(base, 10), 0.20)
	base.Fps = 60
	assertCost(t, estimateCost(base, 10), 0.40)
	base.TopazModel = "Gaia 2"
	assertCost(t, estimateCost(base, 10), 0.20)
}

func assertCost(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("cost = %#v, want %.2f", got, want)
	}
}
