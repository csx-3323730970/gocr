package main

import (
	"testing"

	"github.com/csx-3323730970/gocr/internal/report"
)

func TestMatchFindings(t *testing.T) {
	pred := []report.Finding{
		{File: "a.go", Line: 10, Anchor: report.Anchor{Method: "exact"}},
		{File: "a/other.go", Line: 5, Anchor: report.Anchor{Method: "fuzzy"}},
		{File: "b.go", Line: 100, Anchor: report.Anchor{Method: "downgraded"}},
	}
	gold := []GoldFinding{
		{File: "a.go", Line: 12},
		{File: "other.go", Line: 5},
		{File: "b.go", Line: 1},
	}

	matched, anchorOK := MatchFindings(pred, gold)
	if matched != 2 {
		t.Fatalf("matched = %d, want 2", matched)
	}
	if anchorOK != 2 {
		t.Fatalf("anchorOK = %d, want 2", anchorOK)
	}
}

func TestMatchFindingsOneToOne(t *testing.T) {
	pred := []report.Finding{
		{File: "a.go", Line: 10},
		{File: "a.go", Line: 10},
	}
	gold := []GoldFinding{{File: "a.go", Line: 10}}

	matched, _ := MatchFindings(pred, gold)
	if matched != 1 {
		t.Fatalf("matched = %d, want 1", matched)
	}
}

func TestAggregate(t *testing.T) {
	cases := []CaseMetrics{
		{Name: "1", Gold: 2, Predicted: 3, Matched: 2, AnchorOK: 2, TokensIn: 10, TokensOut: 5, LatencyMS: 100},
		{Name: "2", Gold: 1, Predicted: 1, Matched: 0, AnchorOK: 0, TokensIn: 4, TokensOut: 1, LatencyMS: 200},
		{Name: "3", Error: "boom"},
	}

	m := Aggregate(cases, "model", "v1", "now")
	if m.Cases != 3 || m.Errors != 1 {
		t.Fatalf("cases/errors = %d/%d", m.Cases, m.Errors)
	}
	if m.Gold != 3 || m.Predicted != 4 || m.Matched != 2 {
		t.Fatalf("totals = %d/%d/%d", m.Gold, m.Predicted, m.Matched)
	}
	if m.Precision != 0.5 || m.Recall != 2.0/3.0 {
		t.Fatalf("precision/recall = %v/%v", m.Precision, m.Recall)
	}
	if m.AnchorAccuracy != 1 {
		t.Fatalf("anchor = %v", m.AnchorAccuracy)
	}
	if m.TokensIn != 14 || m.TokensOut != 6 {
		t.Fatalf("tokens = %d/%d", m.TokensIn, m.TokensOut)
	}
	if m.P95LatencyMS != 200 {
		t.Fatalf("p95 = %d", m.P95LatencyMS)
	}
}

func TestPercentile95(t *testing.T) {
	values := []int64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160, 170, 180, 190, 200}
	if got := percentile95(values); got != 190 {
		t.Fatalf("p95 = %d, want 190", got)
	}
}
