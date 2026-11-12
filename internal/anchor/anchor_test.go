package anchor

import (
	"testing"

	"github.com/csx-3323730970/gocr/internal/source"
)

func testFileDiff(extra ...source.DiffLine) source.FileDiff {
	lines := []source.DiffLine{
		{Kind: source.LineContext, OldNo: 1, NewNo: 1, Text: "package a"},
		{Kind: source.LineAdded, NewNo: 2, Text: "func f() {"},
		{Kind: source.LineRemoved, OldNo: 2, Text: "old line"},
		{Kind: source.LineAdded, NewNo: 3, Text: "\tx, _ := os.ReadFile(path)"},
		{Kind: source.LineContext, OldNo: 3, NewNo: 4, Text: "}"},
	}
	lines = append(lines, extra...)
	return source.FileDiff{Path: "a.go", Hunks: []source.Hunk{{Lines: lines}}}
}

func TestAnchorExact(t *testing.T) {
	fd := testFileDiff()
	got := Anchor(fd, 3, "\tx, _ := os.ReadFile(path)")
	if got.Method != MethodExact || got.Line != 3 || got.Confidence != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestAnchorFuzzyWithinWindow(t *testing.T) {
	fd := testFileDiff()
	got := Anchor(fd, 6, "\tx, _ := os.ReadFile(path)")
	if got.Method != MethodFuzzy || got.Line != 3 {
		t.Fatalf("got %+v", got)
	}
	if got.Confidence <= 0.8 || got.Confidence >= 1 {
		t.Fatalf("confidence = %v", got.Confidence)
	}
}

func TestAnchorHuntUnique(t *testing.T) {
	fd := testFileDiff()
	got := Anchor(fd, 500, "\tx, _ := os.ReadFile(pat)")
	if got.Method != MethodFuzzy || got.Line != 3 || got.Confidence != 0.6 {
		t.Fatalf("got %+v", got)
	}
}

func TestAnchorHuntMultiplePicksNearest(t *testing.T) {
	fd := testFileDiff(source.DiffLine{Kind: source.LineAdded, NewNo: 20, Text: "\tx, _ := os.ReadFile(path)"})
	got := Anchor(fd, 500, "\tx, _ := os.ReadFile(path)")
	if got.Method != MethodFuzzy || got.Line != 20 || got.Confidence != 0.5 {
		t.Fatalf("got %+v", got)
	}
}

func TestAnchorIgnoresRemovedLines(t *testing.T) {
	fd := testFileDiff()
	got := Anchor(fd, 2, "old line")
	if got.Method != MethodDowngraded || got.Line != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestAnchorDowngrade(t *testing.T) {
	fd := testFileDiff()
	got := Anchor(fd, 3, "completely different content")
	if got.Method != MethodDowngraded || got.Line != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestAnchorEmptySnippetDowngrades(t *testing.T) {
	fd := testFileDiff()
	got := Anchor(fd, 3, "   ")
	if got.Method != MethodDowngraded || got.Line != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestAnchorNoHunksDrops(t *testing.T) {
	got := Anchor(source.FileDiff{Path: "x"}, 1, "code")
	if got.Method != MethodDropped {
		t.Fatalf("got %+v", got)
	}
}

func TestNormalizeAndSimilarity(t *testing.T) {
	if got := Normalize("  a   b // comment"); got != "a b" {
		t.Fatalf("normalize = %q", got)
	}
	if got := Similarity("a b", "a   b"); got != 1 {
		t.Fatalf("similarity = %v", got)
	}
	if got := Similarity("abcdef", "xyz"); got >= 0.5 {
		t.Fatalf("similarity = %v", got)
	}
}
