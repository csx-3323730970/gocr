package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxGrepMatches   = 100
	maxGrepFileBytes = 512 << 10
)

type Grep struct {
	Root string
}

func (t *Grep) Name() string { return "grep" }

func (t *Grep) Description() string {
	return "Search a regular expression across text files inside the repository root."
}

func (t *Grep) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"}},"required":["pattern"]}`)
}

// Invoke greps the repository within Root.
func (t *Grep) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("grep: invalid args: %w", err)
	}
	if strings.TrimSpace(a.Pattern) == "" {
		return "", fmt.Errorf("grep: pattern is required")
	}
	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return "", fmt.Errorf("grep: invalid pattern: %w", err)
	}

	base := t.Root
	if a.Path != "" {
		base, err = SafePath(t.Root, a.Path)
		if err != nil {
			return "", err
		}
	}

	var matches []string
	walkErr := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if path != base && skipGrepDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxGrepFileBytes {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		head := make([]byte, 512)
		n, _ := io.ReadFull(f, head)
		if bytes.IndexByte(head[:n], 0) >= 0 {
			return nil
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil
		}

		rel, _ := filepath.Rel(t.Root, path)
		rel = filepath.ToSlash(rel)
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64<<10), maxGrepFileBytes)
		for lineNo := 1; scanner.Scan(); lineNo++ {
			line := scanner.Text()
			if re.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d: %s", rel, lineNo, strings.TrimSpace(line)))
				if len(matches) >= maxGrepMatches {
					return filepath.SkipAll
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return nil
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if len(matches) == 0 {
		return "no matches", nil
	}
	return strings.Join(matches, "\n"), nil
}

func skipGrepDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "bin":
		return true
	default:
		return false
	}
}
