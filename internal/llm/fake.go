package llm

import (
	"context"
	"sync"
)

// FakeProvider is a deterministic test double. Responses are returned in order;
// the last response repeats once the queue is exhausted.
type FakeProvider struct {
	mu        sync.Mutex
	Responses []Response
	Err       error
	Calls     int
}

func (f *FakeProvider) Name() string { return "fake" }

func (f *FakeProvider) Complete(_ context.Context, _ Request) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	idx := f.Calls
	f.Calls++
	if f.Err != nil {
		return Response{}, f.Err
	}
	if len(f.Responses) == 0 {
		return Response{}, nil
	}
	if idx >= len(f.Responses) {
		idx = len(f.Responses) - 1
	}
	return f.Responses[idx], nil
}

func (f *FakeProvider) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Calls
}
