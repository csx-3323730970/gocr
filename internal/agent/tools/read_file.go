// Package tools contains the agent's tool implementations.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

const maxReadFileBytes = 256 << 10

type ReadFile struct {
	Root string
}

func (t *ReadFile) Name() string { return "read_file" }

func (t *ReadFile) Description() string {
	return "Read a UTF-8 text file inside the repository root. Paths are relative to the root."
}

func (t *ReadFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}

// Invoke reads a file inside Root.
func (t *ReadFile) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("read_file: invalid args: %w", err)
	}

	path, err := SafePath(t.Root, a.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("read_file: %q is a directory", a.Path)
	}
	if info.Size() > maxReadFileBytes {
		return "", fmt.Errorf("read_file: file too large (%d bytes)", info.Size())
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
