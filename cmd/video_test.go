package cmd

import "testing"

func TestVideoModelDefaultsByProvider(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		want     string
	}{
		{provider: "segmind", model: "auto", want: "mini"},
		{provider: "segmind", model: "fast", want: "fast"},
		{provider: "fal", model: "auto", want: "omni-1.1-flash"},
		{provider: "fal", model: "omni-1.1-flash", want: "omni-1.1-flash"},
	}
	for _, test := range tests {
		got, err := videoModel(test.provider, test.model)
		if err != nil {
			t.Fatalf("videoModel(%q, %q): %v", test.provider, test.model, err)
		}
		if got != test.want {
			t.Errorf("videoModel(%q, %q) = %q, want %q", test.provider, test.model, got, test.want)
		}
	}
	if _, err := videoModel("other", "auto"); err == nil {
		t.Fatal("videoModel accepted an unknown provider")
	}
}
