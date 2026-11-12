package report

import (
	"encoding/json"
	"io"
)

// RenderJSON writes {"findings":[...]} as indented JSON.
func RenderJSON(w io.Writer, findings []Finding) error {
	sorted := append([]Finding(nil), findings...)
	Sort(sorted)
	if sorted == nil {
		sorted = []Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		Findings []Finding `json:"findings"`
	}{Findings: sorted})
}
