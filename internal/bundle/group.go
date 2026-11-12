package bundle

import (
	"sort"
	"strings"

	"github.com/csx-3323730970/gocr/internal/source"
)

// Group clusters files by top-level directory and splits large clusters.
// Deterministic: groups and paths are sorted.
func Group(files []source.FileDiff, maxPerBundle int) []Bundle {
	if maxPerBundle <= 0 {
		maxPerBundle = 5
	}

	groups := make(map[string][]string)
	for _, f := range files {
		key := topDir(f.Path)
		groups[key] = append(groups[key], f.Path)
	}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var bundles []Bundle
	for _, k := range keys {
		paths := groups[k]
		sort.Strings(paths)
		for start := 0; start < len(paths); start += maxPerBundle {
			end := start + maxPerBundle
			if end > len(paths) {
				end = len(paths)
			}
			bundles = append(bundles, Bundle{Paths: append([]string(nil), paths[start:end]...)})
		}
	}
	return bundles
}

func topDir(path string) string {
	if idx := strings.IndexByte(path, '/'); idx >= 0 {
		return path[:idx]
	}
	return "."
}
