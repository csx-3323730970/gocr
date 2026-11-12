package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/csx-3323730970/gocr/internal/llm"
)

type fakeTool struct {
	name  string
	calls int
	out   string
	err   error
}

func (t *fakeTool) Name() string            { return t.name }
func (t *fakeTool) Description() string     { return "test tool" }
func (t *fakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (t *fakeTool) Invoke(context.Context, json.RawMessage) (string, error) {
	t.calls++
	return t.out, t.err
}

func TestReviewParsesFencedJSON(t *testing.T) {
	provider := &llm.FakeProvider{Responses: []llm.Response{{
		Text:  "```json\n{\"findings\":[{\"file\":\"a.go\",\"line\":1,\"severity\":\"major\",\"message\":\"x\",\"snippet\":\"code\"}]}\n```",
		Usage: llm.Usage{InputTokens: 10, OutputTokens: 5},
	}}}

	a := &Agent{Provider: provider, MaxRounds: 2}
	res, err := a.Review(context.Background(), "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 || res.Findings[0].File != "a.go" {
		t.Fatalf("findings = %+v", res.Findings)
	}
	if res.Rounds != 1 || res.Usage.Total() != 15 {
		t.Fatalf("result = %+v", res)
	}
}

func TestReviewRunsToolLoop(t *testing.T) {
	tool := &fakeTool{name: "read_file", out: "file content"}
	provider := &llm.FakeProvider{Responses: []llm.Response{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "read_file", Args: json.RawMessage(`{"path":"a.go"}`)}}},
		{Text: `{"findings":[]}`},
	}}

	a := &Agent{Provider: provider, Tools: []Tool{tool}, MaxRounds: 3}
	res, err := a.Review(context.Background(), "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if tool.calls != 1 {
		t.Fatalf("tool calls = %d, want 1", tool.calls)
	}
	if len(res.Findings) != 0 || res.Rounds != 2 {
		t.Fatalf("result = %+v", res)
	}
}

func TestReviewRepairsInvalidJSON(t *testing.T) {
	provider := &llm.FakeProvider{Responses: []llm.Response{
		{Text: "not json"},
		{Text: `{"findings":[]}`},
	}}
	a := &Agent{Provider: provider, MaxRounds: 3}
	if _, err := a.Review(context.Background(), "sys", "user"); err != nil {
		t.Fatal(err)
	}
	if provider.CallCount() != 2 {
		t.Fatalf("calls = %d, want 2", provider.CallCount())
	}
}

func TestReviewMaxRounds(t *testing.T) {
	provider := &llm.FakeProvider{Responses: []llm.Response{{
		ToolCalls: []llm.ToolCall{{ID: "c", Name: "unknown", Args: json.RawMessage(`{}`)}},
	}}}
	a := &Agent{Provider: provider, MaxRounds: 2}
	_, err := a.Review(context.Background(), "sys", "user")
	if !errors.Is(err, ErrMaxRounds) {
		t.Fatalf("err = %v", err)
	}
	if provider.CallCount() != 2 {
		t.Fatalf("calls = %d, want 2", provider.CallCount())
	}
}

func TestReviewBudgetExceeded(t *testing.T) {
	provider := &llm.FakeProvider{Responses: []llm.Response{{
		Text:  `{"findings":[]}`,
		Usage: llm.Usage{InputTokens: 100, OutputTokens: 50},
	}}}
	a := &Agent{Provider: provider, MaxRounds: 2, TokenBudget: 100}
	_, err := a.Review(context.Background(), "sys", "user")
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("err = %v", err)
	}
}
