package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSafePath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.go", "package a\n")

	if _, err := SafePath(root, "a.go"); err != nil {
		t.Fatalf("safe path rejected: %v", err)
	}
	if _, err := SafePath(root, "../outside.go"); !errors.Is(err, ErrOutsideRepo) {
		t.Fatalf("traversal err = %v", err)
	}
	abs := filepath.Join(root, "a.go")
	if _, err := SafePath(root, abs); !errors.Is(err, ErrOutsideRepo) {
		t.Fatalf("absolute path err = %v", err)
	}
}

func TestReadFile(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.go", "package a\n")
	tr := &ReadFile{Root: root}

	out, err := tr.Invoke(context.Background(), json.RawMessage(`{"path":"a.go"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "package a\n" {
		t.Fatalf("out = %q", out)
	}

	if _, err := tr.Invoke(context.Background(), json.RawMessage(`{"path":"../x"}`)); !errors.Is(err, ErrOutsideRepo) {
		t.Fatalf("traversal err = %v", err)
	}
	if _, err := tr.Invoke(context.Background(), json.RawMessage(`{"path":"."}`)); err == nil {
		t.Fatal("expected directory error")
	}
}

func TestGrep(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.go", "package a\n// TODO: fix\n")
	writeTestFile(t, root, "sub/b.go", "// TODO: another\n")
	writeTestFile(t, root, "vendor/c.go", "// TODO: ignored\n")

	g := &Grep{Root: root}
	out, err := g.Invoke(context.Background(), json.RawMessage(`{"pattern":"TODO"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.go:2") || !strings.Contains(out, "sub/b.go:1") {
		t.Fatalf("matches = %q", out)
	}
	if strings.Contains(out, "vendor/c.go") {
		t.Fatalf("vendor should be skipped: %q", out)
	}

	out, err = g.Invoke(context.Background(), json.RawMessage(`{"pattern":"ZZZ"}`))
	if err != nil || out != "no matches" {
		t.Fatalf("out = %q, err = %v", out, err)
	}

	if _, err := g.Invoke(context.Background(), json.RawMessage(`{"pattern":"["}`)); err == nil {
		t.Fatal("expected invalid regexp error")
	}
}

func TestHunkContext(t *testing.T) {
	root := t.TempDir()
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, "line "+strings.Repeat("x", i%3))
	}
	writeTestFile(t, root, "a.go", strings.Join(lines, "\n"))

	h := &HunkContext{Root: root}
	out, err := h.Invoke(context.Background(), json.RawMessage(`{"file":"a.go","line":15}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "   15 |") || !strings.Contains(out, "    5 |") {
		t.Fatalf("context = %q", out)
	}

	if _, err := h.Invoke(context.Background(), json.RawMessage(`{"file":"a.go","line":999}`)); err == nil {
		t.Fatal("expected EOF error")
	}
}
