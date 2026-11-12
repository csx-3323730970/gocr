// Package selector decides which changed files enter review.
// Named selector because "select" is a Go keyword.
package selector

import "github.com/bmatcuk/doublestar/v4"

// DefaultIgnoreGlobs are noise paths that almost never deserve review.
var DefaultIgnoreGlobs = []string{
	"vendor/**", "**/vendor/**",
	"node_modules/**", "**/node_modules/**",
	"dist/**", "**/dist/**",
	"bin/**", "**/bin/**",
	"**/*.min.js", "**/*.min.css",
	"go.sum", "**/go.sum",
	"package-lock.json", "pnpm-lock.yaml", "yarn.lock",
}

type Filter struct {
	Allowlist    []string
	IgnoreGlobs  []string
	MaxFileBytes int64
}

// Keep reports whether a changed file should be reviewed.
func (f Filter) Keep(path string, size int64) bool {
	if f.MaxFileBytes > 0 && size > f.MaxFileBytes {
		return false
	}
	if matchAny(f.IgnoreGlobs, path) {
		return false
	}
	if len(f.Allowlist) > 0 && !matchAny(f.Allowlist, path) {
		return false
	}
	return true
}

func matchAny(globs []string, path string) bool {
	for _, g := range globs {
		if ok, err := doublestar.Match(g, path); err == nil && ok {
			return true
		}
	}
	return false
}
