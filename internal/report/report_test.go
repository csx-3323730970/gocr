package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func sampleFindings() []Finding {
	return []Finding{
		{File: "b.go", Line: 10, Severity: SeverityMajor, Message: "err ignored", Snippet: "x, _ := f()", Anchor: Anchor{Method: "fuzzy", Confidence: 0.9, Original: 8}},
		{File: "a.go", Line: 0, Severity: SeverityNit, Message: "file-level note", Anchor: Anchor{Method: "downgraded"}},
	}
}

func TestRenderTerminal(t *testing.T) {
	var b bytes.Buffer
	if err := RenderTerminal(&b, sampleFindings()); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"b.go:10", "[major]", "err ignored", "anchor: fuzzy", "2 findings: 0 blocker, 1 major, 0 minor, 1 nit"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Index(out, "a.go") > strings.Index(out, "b.go:10") {
		t.Fatalf("findings not sorted by file:\n%s", out)
	}
}

func TestRenderJSON(t *testing.T) {
	var b bytes.Buffer
	if err := RenderJSON(&b, sampleFindings()); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Findings []Finding `json:"findings"`
	}
	if err := json.Unmarshal(b.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Findings) != 2 || payload.Findings[0].File != "a.go" {
		t.Fatalf("payload = %+v", payload)
	}
}
