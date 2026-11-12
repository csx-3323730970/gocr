package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Summary counts findings by severity.
type Summary struct {
	Total   int
	Blocker int
	Major   int
	Minor   int
	Nit     int
}

func Summarize(findings []Finding) Summary {
	var s Summary
	s.Total = len(findings)
	for _, f := range findings {
		switch f.Severity {
		case SeverityBlocker:
			s.Blocker++
		case SeverityMajor:
			s.Major++
		case SeverityMinor:
			s.Minor++
		case SeverityNit:
			s.Nit++
		}
	}
	return s
}

// Sort orders findings by file and line.
func Sort(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
}

// RenderTerminal prints findings and a severity summary.
func RenderTerminal(w io.Writer, findings []Finding) error {
	sorted := append([]Finding(nil), findings...)
	Sort(sorted)

	for _, f := range sorted {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		if _, err := fmt.Fprintf(w, "%s  [%s] %s\n", loc, f.Severity, f.Message); err != nil {
			return err
		}
		if f.Rule != "" {
			if _, err := fmt.Fprintf(w, "    rule: %s\n", f.Rule); err != nil {
				return err
			}
		}
		if snippet := strings.TrimSpace(f.Snippet); snippet != "" {
			if _, err := fmt.Fprintf(w, "    > %s\n", snippet); err != nil {
				return err
			}
		}
		if f.Anchor.Method != "" && f.Anchor.Method != "exact" {
			if _, err := fmt.Fprintf(w, "    anchor: %s (%.2f)\n", f.Anchor.Method, f.Anchor.Confidence); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}

	s := Summarize(sorted)
	noun := "findings"
	if s.Total == 1 {
		noun = "finding"
	}
	_, err := fmt.Fprintf(w, "%d %s: %d blocker, %d major, %d minor, %d nit\n",
		s.Total, noun, s.Blocker, s.Major, s.Minor, s.Nit)
	return err
}
