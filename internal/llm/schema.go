package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/csx-3323730970/gocr/internal/report"
)

// ValidateFindings enforces the output contract:
// {"findings":[{"file","line","severity","message","snippet"}]}.
func ValidateFindings(raw []byte) ([]report.Finding, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, errors.New("empty model output")
	}

	var out struct {
		Findings []report.Finding `json:"findings"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("invalid findings JSON: %w", err)
	}
	if out.Findings == nil {
		return nil, errors.New(`missing "findings" array`)
	}

	for i := range out.Findings {
		f := &out.Findings[i]
		f.File = strings.TrimSpace(f.File)
		if f.File == "" {
			return nil, fmt.Errorf("finding %d: file is required", i)
		}
		if f.Line < 0 {
			return nil, fmt.Errorf("finding %d: line must be >= 0", i)
		}
		sev, ok := normalizeSeverity(f.Severity)
		if !ok {
			return nil, fmt.Errorf("finding %d: invalid severity %q", i, f.Severity)
		}
		f.Severity = sev
		if strings.TrimSpace(f.Message) == "" {
			return nil, fmt.Errorf("finding %d: message is required", i)
		}
	}
	return out.Findings, nil
}

func normalizeSeverity(s report.Severity) (report.Severity, bool) {
	switch strings.ToLower(strings.TrimSpace(string(s))) {
	case "blocker", "critical":
		return report.SeverityBlocker, true
	case "major", "error":
		return report.SeverityMajor, true
	case "minor", "warning":
		return report.SeverityMinor, true
	case "nit", "info":
		return report.SeverityNit, true
	default:
		return "", false
	}
}
