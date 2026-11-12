package source

import (
	"strings"
	"testing"
)

func TestRenderDiff(t *testing.T) {
	files := []FileDiff{{
		Path:   "a.go",
		Status: "modified",
		Hunks: []Hunk{{
			OldStart: 1, OldLines: 2, NewStart: 1, NewLines: 2,
			Lines: []DiffLine{
				{Kind: LineContext, OldNo: 1, NewNo: 1, Text: "package a"},
				{Kind: LineRemoved, OldNo: 2, Text: "old"},
				{Kind: LineAdded, NewNo: 2, Text: "new"},
			},
		}},
	}}

	out, truncated := RenderDiff(files, 0)
	if truncated {
		t.Fatal("unexpected truncation")
	}
	for _, want := range []string{"### FILE: a.go", "+", "-", "new", "old"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}

	_, truncated = RenderDiff(files, 10)
	if !truncated {
		t.Fatal("expected truncation")
	}
}
