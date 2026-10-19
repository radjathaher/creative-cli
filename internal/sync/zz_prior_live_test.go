//go:build live

package sync

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/radjathaher/creative-cli/internal/registry"
)

func TestLivePrior(t *testing.T) {
	ctx := context.Background()
	s := time.Now()
	full, err := NewFal().Fetch(ctx)
	fmt.Println("full", len(full), err, time.Since(s))
	s = time.Now()
	f := NewFalWithPrior(&registry.Catalog{Routes: full}, false)
	inc, err := f.Fetch(ctx)
	fmt.Println("incremental", len(inc), err, time.Since(s), "warn", len(f.Warnings))
}
