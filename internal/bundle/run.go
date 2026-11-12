package bundle

import (
	"context"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/csx-3323730970/gocr/internal/report"
)

type ReviewFunc func(ctx context.Context, b Bundle) ([]report.Finding, error)

// Run reviews bundles concurrently with a bounded worker count. Budgets live
// in agent.Agent (one per bundle); the pipeline callback decides how bundle
// failures degrade. An error returned by fn cancels the whole group.
func Run(ctx context.Context, bundles []Bundle, concurrency int, fn ReviewFunc) ([]report.Finding, error) {
	if concurrency <= 0 {
		concurrency = 1
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	var mu sync.Mutex
	var all []report.Finding

	for _, b := range bundles {
		g.Go(func() error {
			findings, err := fn(ctx, b)
			if err != nil {
				return err
			}
			mu.Lock()
			all = append(all, findings...)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return all, nil
}
