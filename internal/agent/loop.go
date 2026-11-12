// Package agent runs the tool-use loop for one bundle.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/csx-3323730970/gocr/internal/llm"
	"github.com/csx-3323730970/gocr/internal/obs"
	"github.com/csx-3323730970/gocr/internal/report"
)

var (
	ErrMaxRounds       = errors.New("agent: max rounds exceeded")
	ErrBudgetExceeded  = errors.New("agent: token budget exceeded")
	errUnknownToolName = errors.New("agent: unknown tool")
)

// Tool is the agent's hand: read_file / grep / hunk_context.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Invoke(ctx context.Context, args json.RawMessage) (string, error)
}

type Agent struct {
	Provider    llm.Provider
	Tools       []Tool
	MaxRounds   int
	TokenBudget int
}

type Result struct {
	Findings []report.Finding
	Usage    llm.Usage
	Rounds   int
}

// Review runs the tool-use loop for one bundle and returns validated findings.
func (a *Agent) Review(ctx context.Context, systemPrompt, userPrompt string) (Result, error) {
	maxRounds := a.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 4
	}

	specs := make([]llm.ToolSpec, 0, len(a.Tools))
	for _, t := range a.Tools {
		specs = append(specs, llm.ToolSpec{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      t.Schema(),
		})
	}

	messages := []llm.Message{{Role: "user", Content: userPrompt}}
	var result Result
	var lastErr error

	for round := 1; round <= maxRounds; round++ {
		resp, err := a.Provider.Complete(ctx, llm.Request{
			System:   systemPrompt,
			Messages: messages,
			Tools:    specs,
			MaxTok:   4096,
		})
		if err != nil {
			obs.LLMRequestsTotal.WithLabelValues(a.Provider.Name(), "error").Inc()
			return result, err
		}
		obs.LLMRequestsTotal.WithLabelValues(a.Provider.Name(), "ok").Inc()
		obs.LLMTokensTotal.WithLabelValues("input").Add(float64(resp.Usage.InputTokens))
		obs.LLMTokensTotal.WithLabelValues("output").Add(float64(resp.Usage.OutputTokens))

		result.Usage.InputTokens += resp.Usage.InputTokens
		result.Usage.OutputTokens += resp.Usage.OutputTokens
		result.Rounds = round

		if a.TokenBudget > 0 && result.Usage.Total() > a.TokenBudget {
			return result, fmt.Errorf("%w: used %d tokens", ErrBudgetExceeded, result.Usage.Total())
		}

		if len(resp.ToolCalls) == 0 {
			findings, parseErr := llm.ValidateFindings([]byte(extractJSON(resp.Text)))
			if parseErr == nil {
				result.Findings = findings
				return result, nil
			}
			lastErr = parseErr
			messages = append(messages,
				llm.Message{Role: "assistant", Content: resp.Text},
				llm.Message{Role: "user", Content: repairPrompt(parseErr)},
			)
			continue
		}

		messages = append(messages, llm.Message{
			Role:      "assistant",
			Content:   resp.Text,
			ToolCalls: resp.ToolCalls,
		})
		for _, call := range resp.ToolCalls {
			out, invokeErr := a.invokeTool(ctx, call)
			if invokeErr != nil {
				out = "error: " + invokeErr.Error()
			}
			messages = append(messages, llm.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    truncateText(out, 32<<10),
			})
		}
	}

	if lastErr != nil {
		return result, fmt.Errorf("%w: last parse error: %v", ErrMaxRounds, lastErr)
	}
	return result, ErrMaxRounds
}

func (a *Agent) invokeTool(ctx context.Context, call llm.ToolCall) (string, error) {
	for _, t := range a.Tools {
		if t.Name() != call.Name {
			continue
		}
		return t.Invoke(ctx, call.Args)
	}
	return "", fmt.Errorf("%w: %q", errUnknownToolName, call.Name)
}

// extractJSON tolerates markdown code fences and leading/trailing prose.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if idx := strings.IndexByte(s, '\n'); idx >= 0 {
			s = s[idx+1:]
		}
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

func repairPrompt(err error) string {
	return fmt.Sprintf(`上一次输出不是合法的 findings JSON：%v。
请只输出一个 JSON 对象，不要任何解释或 markdown 代码块，格式为：
{"findings":[{"file":"新文件路径","line":行号,"severity":"blocker|major|minor|nit","message":"问题说明","snippet":"该行原始代码"}]}
如果没有问题，输出 {"findings":[]}。`, err)
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n... (truncated)"
}
