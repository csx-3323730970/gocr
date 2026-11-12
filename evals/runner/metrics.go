package main

import (
	"math"
	"sort"
	"strings"

	"github.com/csx-3323730970/gocr/internal/report"
)

// GoldFinding is one human-labelled defect in a case.
type GoldFinding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Category string `json:"category"`
	Note     string `json:"note"`
}

// Gold is a case's ground truth.
type Gold struct {
	Source   string        `json:"source"`
	Findings []GoldFinding `json:"findings"`
}

type CaseMetrics struct {
	Name      string  `json:"name"`
	Gold      int     `json:"gold"`
	Predicted int     `json:"predicted"`
	Matched   int     `json:"matched"`
	AnchorOK  int     `json:"anchor_ok"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
	TokensIn  int     `json:"tokens_in"`
	TokensOut int     `json:"tokens_out"`
	LatencyMS int64   `json:"latency_ms"`
	Error     string  `json:"error,omitempty"`
}

// Metrics is the persisted baseline format.
type Metrics struct {
	GeneratedAt    string        `json:"generated_at"`
	Model          string        `json:"model"`
	PromptVersion  string        `json:"prompt_version"`
	Cases          int           `json:"cases"`
	Errors         int           `json:"errors"`
	Gold           int           `json:"gold"`
	Predicted      int           `json:"predicted"`
	Matched        int           `json:"matched"`
	Precision      float64       `json:"precision"`
	Recall         float64       `json:"recall"`
	F1             float64       `json:"f1"`
	AnchorAccuracy float64       `json:"anchor_accuracy"`
	TokensIn       int64         `json:"tokens_in"`
	TokensOut      int64         `json:"tokens_out"`
	P95LatencyMS   int64         `json:"p95_latency_ms"`
	PerCase        []CaseMetrics `json:"per_case"`
}

const lineTolerance = 2

// MatchFindings greedily pairs predictions with gold findings on the same file
// within +/-2 lines. It returns matched pairs and how many matched predictions
// had an exact/fuzzy anchor.
func MatchFindings(pred []report.Finding, gold []GoldFinding) (matched int, anchorOK int) {
	type pair struct {
		pi, gi, dist int
	}
	var pairs []pair
	for pi, p := range pred {
		for gi, g := range gold {
			if !sameFile(p.File, g.File) || g.Line <= 0 {
				continue
			}
			dist := abs(p.Line - g.Line)
			if dist <= lineTolerance {
				pairs = append(pairs, pair{pi: pi, gi: gi, dist: dist})
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].dist != pairs[j].dist {
			return pairs[i].dist < pairs[j].dist
		}
		return pairs[i].pi < pairs[j].pi
	})

	usedPred := make([]bool, len(pred))
	usedGold := make([]bool, len(gold))
	for _, p := range pairs {
		if usedPred[p.pi] || usedGold[p.gi] {
			continue
		}
		usedPred[p.pi] = true
		usedGold[p.gi] = true
		matched++
		method := pred[p.pi].Anchor.Method
		if method == "exact" || method == "fuzzy" {
			anchorOK++
		}
	}
	return matched, anchorOK
}

func sameFile(a, b string) bool {
	return normalizePath(a) == normalizePath(b)
}

func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(strings.TrimPrefix(p, "a/"), "b/")
	return p
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Aggregate computes overall metrics and per-case ratios.
func Aggregate(cases []CaseMetrics, model, promptVersion, generatedAt string) Metrics {
	m := Metrics{
		GeneratedAt:   generatedAt,
		Model:         model,
		PromptVersion: promptVersion,
		Cases:         len(cases),
		PerCase:       cases,
	}

	latencies := make([]int64, 0, len(cases))
	for _, c := range cases {
		if c.Error != "" {
			m.Errors++
			continue
		}
		m.Gold += c.Gold
		m.Predicted += c.Predicted
		m.Matched += c.Matched
		m.TokensIn += int64(c.TokensIn)
		m.TokensOut += int64(c.TokensOut)
		latencies = append(latencies, c.LatencyMS)
	}

	m.Precision = ratio(m.Matched, m.Predicted)
	m.Recall = ratio(m.Matched, m.Gold)
	if m.Precision+m.Recall > 0 {
		m.F1 = 2 * m.Precision * m.Recall / (m.Precision + m.Recall)
	}

	anchorOK := 0
	for _, c := range cases {
		anchorOK += c.AnchorOK
	}
	m.AnchorAccuracy = ratio(anchorOK, m.Matched)
	m.P95LatencyMS = percentile95(latencies)
	return m
}

func ratio(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func percentile95(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(math.Ceil(float64(len(sorted))*0.95)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
