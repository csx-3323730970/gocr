package llm

import "context"

// Waiter blocks until the caller may issue another request.
type Waiter interface {
	Wait(ctx context.Context) error
}

// RateLimitedProvider applies a shared limiter before every attempt.
type RateLimitedProvider struct {
	Inner   Provider
	Limiter Waiter
}

func NewRateLimitedProvider(inner Provider, limiter Waiter) *RateLimitedProvider {
	return &RateLimitedProvider{Inner: inner, Limiter: limiter}
}

func (p *RateLimitedProvider) Name() string { return p.Inner.Name() }

func (p *RateLimitedProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if p.Limiter != nil {
		if err := p.Limiter.Wait(ctx); err != nil {
			return Response{}, err
		}
	}
	return p.Inner.Complete(ctx, req)
}
