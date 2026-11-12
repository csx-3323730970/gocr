// Package llm talks to one OpenAI-compatible provider.
package llm

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	ErrRateLimited = errors.New("llm: rate limited")
	ErrTransient   = errors.New("llm: transient error")
	ErrFatal       = errors.New("llm: fatal error")
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type Request struct {
	System   string
	Messages []Message
	Tools    []ToolSpec
	JSONMode bool
	MaxTok   int
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

func (u Usage) Total() int { return u.InputTokens + u.OutputTokens }

type Response struct {
	Text      string
	ToolCalls []ToolCall
	Usage     Usage
}

type Provider interface {
	Name() string
	Complete(ctx context.Context, req Request) (Response, error)
}
