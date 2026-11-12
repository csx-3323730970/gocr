// Package rules merges rule documents in layers:
// default -> language -> project -> CLI.
//
// Merge semantics are append-only: every matching layer contributes to the
// prompt. Replacing lower layers with a catch-all project rule silently drops
// language rules, which is a known source of bugs in review tools.
package rules

import (
	"embed"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

//go:embed docs/*.md
var docsFS embed.FS

type Layer struct {
	Name  string
	Paths []string // doublestar globs; empty matches every path
	Doc   string
}

type Set struct {
	Layers []Layer
}

// For returns the merged rule document for path.
func (s Set) For(path string) string {
	var b strings.Builder
	for _, l := range s.Layers {
		if strings.TrimSpace(l.Doc) == "" || !l.matches(path) {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## ")
		b.WriteString(l.Name)
		b.WriteString(" rules\n")
		b.WriteString(strings.TrimSpace(l.Doc))
	}
	return b.String()
}

func (l Layer) matches(path string) bool {
	if len(l.Paths) == 0 {
		return true
	}
	for _, g := range l.Paths {
		if ok, err := doublestar.Match(g, path); err == nil && ok {
			return true
		}
	}
	return false
}

// Build assembles built-in, project (<repo>/.gocr/rules.md) and extra layers.
func Build(repoRoot string, extra []Layer) (Set, error) {
	defaultDoc, err := docsFS.ReadFile("docs/default.md")
	if err != nil {
		return Set{}, err
	}
	goDoc, err := docsFS.ReadFile("docs/go.md")
	if err != nil {
		return Set{}, err
	}

	layers := []Layer{
		{Name: "default", Doc: string(defaultDoc)},
		{Name: "go", Paths: []string{"**/*.go", "*.go"}, Doc: string(goDoc)},
	}

	if repoRoot != "" {
		doc, err := os.ReadFile(filepath.Join(repoRoot, ".gocr", "rules.md"))
		switch {
		case err == nil:
			layers = append(layers, Layer{Name: "project", Doc: string(doc)})
		case errors.Is(err, os.ErrNotExist):
		default:
			return Set{}, err
		}
	}

	layers = append(layers, extra...)
	return Set{Layers: layers}, nil
}

// FromFiles loads rule documents from CLI-provided files.
func FromFiles(paths []string) ([]Layer, error) {
	var layers []Layer
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		layers = append(layers, Layer{Name: filepath.Base(p), Doc: string(b)})
	}
	return layers, nil
}
