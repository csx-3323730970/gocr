package job

import (
	"context"
	"time"

	"golang.org/x/time/rate"
)

// Limiter guards provider RPM budget.
type Limiter struct {
	rpm *rate.Limiter
}

func NewLimiter(rpm int) *Limiter {
	if rpm <= 0 {
		rpm = 60
	}
	return &Limiter{
		rpm: rate.NewLimiter(rate.Every(time.Minute/time.Duration(rpm)), rpm),
	}
}

// Wait blocks until a provider request token is available.
// Per-bundle token budgets are enforced by agent.Agent; TPM throttling and a
// cross-bundle global budget are intentionally out of scope for this
// single-node design.
func (l *Limiter) Wait(ctx context.Context) error {
	return l.rpm.Wait(ctx)
}
