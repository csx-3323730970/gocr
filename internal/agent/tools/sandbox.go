package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrOutsideRepo = errors.New("tools: path escapes repository root")

// SafePath resolves rel inside root and rejects traversal, absolute paths,
// volume-relative paths and symlink escapes.
func SafePath(root, rel string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("tools: empty repository root")
	}
	if strings.TrimSpace(rel) == "" {
		return "", errors.New("tools: empty path")
	}
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", ErrOutsideRepo
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(absRoot, rel))
	if err != nil {
		return "", err
	}

	relCheck, err := filepath.Rel(absRoot, target)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(os.PathSeparator)) {
		return "", ErrOutsideRepo
	}

	// Follow symlinks when the target exists; reject escapes through them.
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		resolvedRoot, rootErr := filepath.EvalSymlinks(absRoot)
		if rootErr != nil {
			return "", rootErr
		}
		r, relErr := filepath.Rel(resolvedRoot, resolved)
		if relErr != nil || r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) {
			return "", ErrOutsideRepo
		}
	}
	return target, nil
}
