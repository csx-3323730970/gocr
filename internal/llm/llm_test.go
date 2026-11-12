package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOpenAIComplete(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok","tool_calls":[{"id":"c1","function":{"name":"read_file","arguments":"{\"path\":\"a.go\"}"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	}))
	defer srv.Close()

	p := NewOpenAI(srv.URL, "test-key", "m1")
	resp, err := p.Complete(context.Background(), Request{
		System:   "sys",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools:    []ToolSpec{{Name: "read_file", Description: "d", Schema: json.RawMessage(`{"type":"object"}`)}},
		JSONMode: true,
		MaxTok:   100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "ok" || resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "read_file" {
		t.Fatalf("tool calls = %+v", resp.ToolCalls)
	}
	if string(resp.ToolCalls[0].Args) != `{"path":"a.go"}` {
		t.Fatalf("args = %s", resp.ToolCalls[0].Args)
	}

	if body["model"] != "m1" {
		t.Fatalf("model = %v", body["model"])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %v", body["messages"])
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "sys" {
		t.Fatalf("system message = %v", first)
	}
	rf, _ := body["response_format"].(map[string]any)
	if rf["type"] != "json_object" {
		t.Fatalf("response_format = %v", body["response_format"])
	}
}

func TestOpenAIErrorClassification(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusTooManyRequests, ErrRateLimited},
		{http.StatusInternalServerError, ErrTransient},
		{http.StatusBadRequest, ErrFatal},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
		}))
		p := NewOpenAI(srv.URL, "", "m")
		_, err := p.Complete(context.Background(), Request{})
		if !errors.Is(err, c.want) {
			t.Errorf("status %d: err = %v, want %v", c.status, err, c.want)
		}
		srv.Close()
	}
}

func TestOpenAIBadJSONIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `not json`)
	}))
	defer srv.Close()

	_, err := NewOpenAI(srv.URL, "", "m").Complete(context.Background(), Request{})
	if !errors.Is(err, ErrTransient) {
		t.Fatalf("err = %v", err)
	}
}

func TestRetryPolicyRetriesTransient(t *testing.T) {
	attempts := 0
	err := (RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond}).Do(context.Background(), func(context.Context) error {
		attempts++
		if attempts < 3 {
			return fmt.Errorf("%w: boom", ErrTransient)
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("err = %v, attempts = %d", err, attempts)
	}
}

func TestRetryPolicyStopsOnFatal(t *testing.T) {
	attempts := 0
	err := (RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond}).Do(context.Background(), func(context.Context) error {
		attempts++
		return fmt.Errorf("%w: nope", ErrFatal)
	})
	if !errors.Is(err, ErrFatal) || attempts != 1 {
		t.Fatalf("err = %v, attempts = %d", err, attempts)
	}
}

func TestRetryPolicyHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond}).Do(ctx, func(context.Context) error {
		return fmt.Errorf("%w: boom", ErrTransient)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

type memCache struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (m *memCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	return v, ok, nil
}

func (m *memCache) Put(_ context.Context, key string, payload []byte, _ int, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = payload
	return nil
}

func TestCachingProvider(t *testing.T) {
	inner := &FakeProvider{Responses: []Response{{Text: "first", Usage: Usage{InputTokens: 1, OutputTokens: 2}}}}
	cache := &memCache{data: map[string][]byte{}}
	p := NewCachingProvider(inner, cache, "m1")

	req := Request{Messages: []Message{{Role: "user", Content: "hello"}}}
	first, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if inner.CallCount() != 1 {
		t.Fatalf("inner calls = %d, want 1", inner.CallCount())
	}
	if first.Text != second.Text {
		t.Fatalf("cached response mismatch: %q vs %q", first.Text, second.Text)
	}

	different, _ := p.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "other"}}})
	if different.Text != "first" {
		t.Fatalf("unexpected response %q", different.Text)
	}
	if inner.CallCount() != 2 {
		t.Fatalf("inner calls = %d, want 2", inner.CallCount())
	}
}

type errLimiter struct{}

func (errLimiter) Wait(context.Context) error { return errors.New("limiter closed") }

func TestRateLimitedProvider(t *testing.T) {
	inner := &FakeProvider{Responses: []Response{{Text: "x"}}}
	p := NewRateLimitedProvider(inner, errLimiter{})
	if _, err := p.Complete(context.Background(), Request{}); err == nil {
		t.Fatal("expected limiter error")
	}
	if inner.CallCount() != 0 {
		t.Fatalf("inner calls = %d, want 0", inner.CallCount())
	}
}

func TestValidateFindings(t *testing.T) {
	valid := []byte(`{"findings":[{"file":"a.go","line":3,"severity":"Major","message":"err ignored","snippet":"x, _ := f()"}]}`)
	findings, err := ValidateFindings(valid)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Severity != "major" {
		t.Fatalf("findings = %+v", findings)
	}

	cases := [][]byte{
		[]byte(`not json`),
		[]byte(`{"nope":[]}`),
		[]byte(`{"findings":[{"line":3,"severity":"major","message":"x"}]}`),
		[]byte(`{"findings":[{"file":"a.go","line":3,"severity":"huge","message":"x"}]}`),
		[]byte(`{"findings":[{"file":"a.go","line":3,"severity":"major","message":""}]}`),
	}
	for _, raw := range cases {
		if _, err := ValidateFindings(raw); err == nil {
			t.Errorf("expected error for %s", strings.TrimSpace(string(raw)))
		}
	}
}
