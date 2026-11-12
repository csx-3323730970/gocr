package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIProvider speaks the OpenAI /chat/completions protocol, which covers
// DeepSeek, Kimi and local Ollama gateways.
type OpenAIProvider struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

func NewOpenAI(baseURL, apiKey, model string) *OpenAIProvider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.deepseek.com"
	}
	return &OpenAIProvider{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *OpenAIProvider) Name() string { return "openai-compatible" }

// Complete sends one chat completion request.
func (p *OpenAIProvider) Complete(ctx context.Context, req Request) (Response, error) {
	body, err := json.Marshal(buildChatRequest(p.Model, req))
	if err != nil {
		return Response{}, fmt.Errorf("%w: encode request: %v", ErrFatal, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(), bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("%w: %v", ErrFatal, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	client := p.HTTP
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, fmt.Errorf("%w: %v", ErrTransient, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return Response{}, fmt.Errorf("%w: read response: %v", ErrTransient, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Response{}, classifyHTTPError(resp.StatusCode, extractErrorMessage(raw))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return Response{}, fmt.Errorf("%w: decode response: %v", ErrTransient, err)
	}
	if len(cr.Choices) == 0 {
		return Response{}, fmt.Errorf("%w: empty choices", ErrTransient)
	}

	msg := cr.Choices[0].Message
	out := Response{
		Text: msg.Content,
		Usage: Usage{
			InputTokens:  cr.Usage.PromptTokens,
			OutputTokens: cr.Usage.CompletionTokens,
		},
	}
	for _, tc := range msg.ToolCalls {
		args := strings.TrimSpace(tc.Function.Arguments)
		if args == "" {
			args = "{}"
		}
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: json.RawMessage(args),
		})
	}
	return out, nil
}

func (p *OpenAIProvider) endpoint() string {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	return base + "/chat/completions"
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function wireFunctionCall `json:"function"`
}

type wireFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireTool struct {
	Type     string       `json:"type"`
	Function wireToolSpec `json:"function"`
}

type wireToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type wireResponseFormat struct {
	Type string `json:"type"`
}

type chatRequest struct {
	Model          string              `json:"model"`
	Messages       []wireMessage       `json:"messages"`
	Tools          []wireTool          `json:"tools,omitempty"`
	ResponseFormat *wireResponseFormat `json:"response_format,omitempty"`
	MaxTokens      int                 `json:"max_tokens,omitempty"`
	Temperature    float64             `json:"temperature"`
}

func buildChatRequest(model string, req Request) chatRequest {
	msgs := make([]wireMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, wireMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		wm := wireMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			args := strings.TrimSpace(string(tc.Args))
			if args == "" {
				args = "{}"
			}
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
				ID:       tc.ID,
				Type:     "function",
				Function: wireFunctionCall{Name: tc.Name, Arguments: args},
			})
		}
		msgs = append(msgs, wm)
	}

	tools := make([]wireTool, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, wireTool{
			Type: "function",
			Function: wireToolSpec{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			},
		})
	}

	cr := chatRequest{
		Model:       model,
		Messages:    msgs,
		Tools:       tools,
		MaxTokens:   req.MaxTok,
		Temperature: 0,
	}
	if req.JSONMode {
		cr.ResponseFormat = &wireResponseFormat{Type: "json_object"}
	}
	return cr
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func classifyHTTPError(status int, msg string) error {
	msg = truncate(msg, 300)
	switch {
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("%w: status %d: %s", ErrRateLimited, status, msg)
	case status >= 500, status == http.StatusRequestTimeout:
		return fmt.Errorf("%w: status %d: %s", ErrTransient, status, msg)
	default:
		return fmt.Errorf("%w: status %d: %s", ErrFatal, status, msg)
	}
}

func extractErrorMessage(raw []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil {
		if payload.Error.Message != "" {
			return payload.Error.Message
		}
		if payload.Message != "" {
			return payload.Message
		}
	}
	return strings.TrimSpace(string(raw))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
