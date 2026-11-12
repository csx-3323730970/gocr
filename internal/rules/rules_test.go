package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForMergesMatchingLayers(t *testing.T) {
	set, err := Build("", nil)
	if err != nil {
		t.Fatal(err)
	}

	goRules := set.For("internal/a.go")
	if !strings.Contains(goRules, "default rules") || !strings.Contains(goRules, "go rules") {
		t.Fatalf("go file should match default + go layers:\n%s", goRules)
	}

	mdRules := set.For("README.md")
	if !strings.Contains(mdRules, "default rules") {
		t.Fatalf("markdown should match default layer:\n%s", mdRules)
	}
	if strings.Contains(mdRules, "go rules") {
		t.Fatalf("markdown should not match go layer:\n%s", mdRules)
	}
}

func TestBuildProjectLayer(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".gocr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gocr", "rules.md"), []byte("- project rule"), 0o644); err != nil {
		t.Fatal(err)
	}

	set, err := Build(dir, []Layer{{Name: "cli", Doc: "- cli rule"}})
	if err != nil {
		t.Fatal(err)
	}
	got := set.For("x.go")
	for _, want := range []string{"project rules", "cli rule", "go rules"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
