package source

import (
	"fmt"
	"strings"
)

// RenderDiff renders files for the model prompt. Line numbers refer to the
// new file side for added/context lines and to the old side for removed lines.
// The returned bool reports whether the output was truncated.
func RenderDiff(files []FileDiff, maxBytes int) (string, bool) {
	var b strings.Builder
	truncated := false

	write := func(s string) bool {
		if maxBytes > 0 && b.Len()+len(s) > maxBytes {
			truncated = true
			return false
		}
		b.WriteString(s)
		return true
	}

	for _, f := range files {
		if !write(fmt.Sprintf("### FILE: %s (%s)\n", f.Path, f.Status)) {
			break
		}
		for _, h := range f.Hunks {
			header := fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
			if h.Header != "" {
				header += " " + h.Header
			}
			if !write(header + "\n") {
				break
			}
			for _, l := range h.Lines {
				var line string
				switch l.Kind {
				case LineAdded:
					line = fmt.Sprintf("+%5d | %s\n", l.NewNo, l.Text)
				case LineRemoved:
					line = fmt.Sprintf("-%5d | %s\n", l.OldNo, l.Text)
				default:
					line = fmt.Sprintf(" %5d | %s\n", l.NewNo, l.Text)
				}
				if !write(line) {
					break
				}
			}
			if truncated {
				break
			}
		}
		if truncated {
			break
		}
	}

	if truncated {
		b.WriteString("... (diff truncated)\n")
	}
	return b.String(), truncated
}
