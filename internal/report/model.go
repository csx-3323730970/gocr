package report

// Severity levels for a finding.
type Severity string

const (
	SeverityBlocker Severity = "blocker"
	SeverityMajor   Severity = "major"
	SeverityMinor   Severity = "minor"
	SeverityNit     Severity = "nit"
)

// Finding is the single source of truth for one review comment.
type Finding struct {
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Severity Severity `json:"severity"`
	Rule     string   `json:"rule,omitempty"`
	Message  string   `json:"message"`
	Snippet  string   `json:"snippet,omitempty"`
	Anchor   Anchor   `json:"anchor"`
}

// Anchor records how a finding's line number was resolved.
type Anchor struct {
	Confidence float64 `json:"confidence"` // 0..1
	Method     string  `json:"method"`     // exact / fuzzy / downgraded / dropped
	Original   int     `json:"original"`   // line number reported by the model
}
