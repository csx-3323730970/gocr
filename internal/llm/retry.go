package llm

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

// RetryPolicy implements exponential backoff with jitter.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
}

// Do retries fn while the error classifies as ErrRateLimited or ErrTransient.
func (p RetryPolicy) Do(ctx context.Context, fn func(context.Context) error) error {
	attempts := p.MaxAttempts
	if attempts <= 0 {
		attempts = 3
	}
	base := p.BaseDelay
	if base <= 0 {
		base = 500 * time.Millisecond
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !retryable(lastErr) || attempt == attempts-1 {
			return lastErr
		}

		delay := base << attempt
		if delay > 8*time.Second {
			delay = 8 * time.Second
		}
		delay = time.Duration(float64(delay) * (0.5 + rand.Float64()*0.5))

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func retryable(err error) bool {
	return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrTransient)
}

// RetryProvider wraps a provider with retry-on-transient-error semantics.
type RetryProvider struct {
	Inner  Provider
	Policy RetryPolicy
}

func NewRetryProvider(inner Provider, policy RetryPolicy) *RetryProvider {
	return &RetryProvider{Inner: inner, Policy: policy}
}

func (p *RetryProvider) Name() string { return p.Inner.Name() }

func (p *RetryProvider) Complete(ctx context.Context, req Request) (Response, error) {
	var out Response
	err := p.Policy.Do(ctx, func(ctx context.Context) error {
		resp, err := p.Inner.Complete(ctx, req)
		if err != nil {
			return err
		}
		out = resp
		return nil
	})
	if err != nil {
		return Response{}, err
	}
	return out, nil
}
