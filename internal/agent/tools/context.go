package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	hunkContextRadius = 10
	maxContextFile    = 1 << 20
)

type HunkContext struct {
	Root string
}

func (t *HunkContext) Name() string { return "hunk_context" }

func (t *HunkContext) Description() string {
	return "Return surrounding source lines for a file and line in the diff."
}

func (t *HunkContext) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"}},"required":["file","line"]}`)
}

// Invoke returns context around a diff line.
func (t *HunkContext) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var a struct {
		File string `json:"file"`
		Line int    `json:"line"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("hunk_context: invalid args: %w", err)
	}
	if a.Line <= 0 {
		return "", fmt.Errorf("hunk_context: line must be > 0")
	}

	path, err := SafePath(t.Root, a.File)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > maxContextFile {
		return "", fmt.Errorf("hunk_context: file too large (%d bytes)", info.Size())
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if a.Line > len(lines) {
		return "", fmt.Errorf("hunk_context: line %d beyond EOF (%d)", a.Line, len(lines))
	}

	start := a.Line - hunkContextRadius
	if start < 1 {
		start = 1
	}
	end := a.Line + hunkContextRadius
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%5d | %s\n", i, lines[i-1])
	}
	return b.String(), nil
}
