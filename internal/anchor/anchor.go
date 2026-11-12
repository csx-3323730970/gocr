// Package anchor maps model-reported line numbers onto real diff lines.
package anchor

import (
	"strings"

	"github.com/csx-3323730970/gocr/internal/source"
)

type Method string

const (
	MethodExact      Method = "exact"
	MethodFuzzy      Method = "fuzzy"
	MethodDowngraded Method = "downgraded"
	MethodDropped    Method = "dropped"
)

const (
	fuzzyWindow    = 10
	fuzzyThreshold = 0.8
)

type Result struct {
	Line       int
	Confidence float64
	Method     Method
	Original   int
}

// Anchor maps a model-reported line to a commentable line in the diff.
// Order: exact match, fuzzy match within +/-10 lines, unique hunt across the
// file, then explicit downgrade (line 0) or drop.
func Anchor(fd source.FileDiff, claimLine int, snippet string) Result {
	original := claimLine
	if len(fd.Hunks) == 0 {
		return Result{Original: original, Method: MethodDropped}
	}

	type candidate struct {
		line int
		text string
	}
	var candidates []candidate
	for _, h := range fd.Hunks {
		for _, l := range h.Lines {
			if l.Kind == source.LineRemoved {
				continue
			}
			candidates = append(candidates, candidate{line: l.NewNo, text: l.Text})
		}
	}
	if len(candidates) == 0 {
		return Result{Original: original, Method: MethodDropped}
	}

	normSnippet := Normalize(snippet)

	if claimLine > 0 && normSnippet != "" {
		for _, c := range candidates {
			if c.line == claimLine && snippetMatches(normSnippet, c.text) {
				return Result{Line: claimLine, Confidence: 1, Method: MethodExact, Original: original}
			}
		}
	}

	if normSnippet != "" {
		bestScore := 0.0
		bestLine := 0
		bestDist := 0
		for _, c := range candidates {
			dist := absInt(c.line - claimLine)
			if claimLine > 0 && dist > fuzzyWindow {
				continue
			}
			score := Similarity(normSnippet, c.text)
			if score > bestScore || (score == bestScore && bestLine != 0 && dist < bestDist) {
				bestScore = score
				bestLine = c.line
				bestDist = dist
			}
		}
		if bestScore >= fuzzyThreshold {
			return Result{
				Line:       bestLine,
				Confidence: bestScore * 0.9,
				Method:     MethodFuzzy,
				Original:   original,
			}
		}

		var hits []candidate
		for _, c := range candidates {
			if Similarity(normSnippet, c.text) >= fuzzyThreshold {
				hits = append(hits, c)
			}
		}
		switch len(hits) {
		case 1:
			return Result{Line: hits[0].line, Confidence: 0.6, Method: MethodFuzzy, Original: original}
		case 0:
		default:
			nearest := hits[0]
			nearestDist := absInt(nearest.line - claimLine)
			for _, c := range hits[1:] {
				if d := absInt(c.line - claimLine); d < nearestDist {
					nearest = c
					nearestDist = d
				}
			}
			return Result{Line: nearest.line, Confidence: 0.5, Method: MethodFuzzy, Original: original}
		}
	}

	return Result{Line: 0, Confidence: 0, Method: MethodDowngraded, Original: original}
}

func snippetMatches(normSnippet, line string) bool {
	normLine := Normalize(line)
	if normLine == "" {
		return false
	}
	if normLine == normSnippet {
		return true
	}
	// Multi-line snippets are common; require enough context to avoid matching "}".
	return len(normLine) >= 4 && strings.Contains(normSnippet, normLine)
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
