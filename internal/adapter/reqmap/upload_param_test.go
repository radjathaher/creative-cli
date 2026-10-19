package reqmap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadParam(t *testing.T) {
	f := filepath.Join(t.TempDir(), "back.png")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	up := func(_ context.Context, p string) (string, error) { return "https://cdn/" + filepath.Base(p), nil }
	got, _ := uploadParam(context.Background(), "back_image_url", f, up, false)
	if got != "https://cdn/back.png" {
		t.Errorf("local path in a URL field not uploaded: %v", got)
	}
	got, _ = uploadParam(context.Background(), "back_image_url", f, up, true)
	if got != DryRunPrefix+f {
		t.Errorf("dry run should show placeholder: %v", got)
	}
	got, _ = uploadParam(context.Background(), "face_count", f, up, false)
	if got != f {
		t.Errorf("non-URL field must pass through: %v", got)
	}
}
