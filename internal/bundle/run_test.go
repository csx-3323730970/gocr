package bundle

import (
	"context"
	"testing"

	"go.uber.org/goleak"

	"github.com/csx-3323730970/gocr/internal/report"
)

func TestRunAggregatesAndDoesNotLeak(t *testing.T) {
	defer goleak.VerifyNone(t)

	bundles := []Bundle{
		{Paths: []string{"a.go"}},
		{Paths: []string{"b.go"}},
		{Paths: []string{"c.go"}},
	}

	got, err := Run(context.Background(), bundles, 2, func(_ context.Context, b Bundle) ([]report.Finding, error) {
		return []report.Finding{{File: b.Paths[0]}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(bundles) {
		t.Fatalf("got %d findings, want %d", len(got), len(bundles))
	}
}
