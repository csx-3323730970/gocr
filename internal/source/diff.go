package source

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrOutputTooLarge = errors.New("source: diff output exceeds limit")
	ErrMalformedDiff  = errors.New("source: malformed diff")
)

type LineKind int

const (
	LineContext LineKind = iota // unchanged context
	LineAdded                   // added line (new-side number valid)
	LineRemoved                 // removed line (old-side number valid)
)

// DiffLine is one physical line in a hunk. Text excludes the leading +/-
// but keeps the original indentation.
type DiffLine struct {
	Kind  LineKind
	OldNo int // 0 when the old side has no such line
	NewNo int // 0 when the new side has no such line
	Text  string
}

// Hunk is one @@ block of a unified diff.
type Hunk struct {
	OldStart, OldLines int
	NewStart, NewLines int
	Lines              []DiffLine
	Header             string // trailing function context of the @@ header
}

// FileDiff describes the change of a single file.
type FileDiff struct {
	Path     string // new-side path
	OldPath  string // differs on rename
	Status   string // added / modified / deleted / renamed
	IsBinary bool
	Hunks    []Hunk
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

// ParseFileDiffs parses `git diff` unified output.
// It handles -U0 and -U3, new/deleted/renamed files, binary patches and
// CRLF line endings.
func ParseFileDiffs(patch []byte) ([]FileDiff, error) {
	lines := splitPatchLines(patch)
	var files []FileDiff
	var cur *FileDiff

	finish := func() {
		if cur == nil {
			return
		}
		if cur.Path == "" {
			cur.Path = cur.OldPath
		}
		if cur.Status == "" {
			cur.Status = "modified"
		}
		if cur.Path != "" {
			files = append(files, *cur)
		}
		cur = nil
	}

	for i := 0; i < len(lines); {
		line := lines[i]

		switch {
		case strings.HasPrefix(line, "diff --git "):
			finish()
			cur = &FileDiff{Status: "modified"}
			if oldPath, newPath, ok := parseGitHeaderPaths(line); ok {
				cur.OldPath = oldPath
				cur.Path = newPath
			}
			i++

		case strings.HasPrefix(line, "new file mode"):
			if cur != nil {
				cur.Status = "added"
				cur.OldPath = ""
			}
			i++

		case strings.HasPrefix(line, "deleted file mode"):
			if cur != nil {
				cur.Status = "deleted"
			}
			i++

		case strings.HasPrefix(line, "rename from "):
			if cur != nil {
				cur.OldPath = cleanPathValue(strings.TrimPrefix(line, "rename from "))
				cur.Status = "renamed"
			}
			i++

		case strings.HasPrefix(line, "rename to "):
			if cur != nil {
				cur.Path = cleanPathValue(strings.TrimPrefix(line, "rename to "))
				cur.Status = "renamed"
			}
			i++

		case strings.HasPrefix(line, "Binary files ") || line == "GIT binary patch":
			if cur != nil {
				cur.IsBinary = true
			}
			i++

		case strings.HasPrefix(line, "--- "):
			if cur != nil {
				if p := cleanPathValue(strings.TrimPrefix(line, "--- ")); p != "" {
					cur.OldPath = p
				}
			}
			i++

		case strings.HasPrefix(line, "+++ "):
			if cur != nil {
				if p := cleanPathValue(strings.TrimPrefix(line, "+++ ")); p != "" {
					cur.Path = p
				}
			}
			i++

		case strings.HasPrefix(line, "@@"):
			if cur == nil {
				return nil, fmt.Errorf("%w: hunk without file header", ErrMalformedDiff)
			}
			h, next, err := parseHunk(lines, i)
			if err != nil {
				return nil, err
			}
			cur.Hunks = append(cur.Hunks, h)
			i = next

		default:
			i++
		}
	}
	finish()

	return files, nil
}

// CommentableLines returns new-side line numbers a review comment may anchor to.
// Removed lines have no new-side number, so they are never commentable.
func (f FileDiff) CommentableLines() []int {
	var out []int
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			if l.Kind == LineAdded || l.Kind == LineContext {
				out = append(out, l.NewNo)
			}
		}
	}
	return out
}

// ByteSize approximates the changed text size, used by file filters.
func (f FileDiff) ByteSize() int {
	n := 0
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			n += len(l.Text)
		}
	}
	return n
}

func parseHunk(lines []string, start int) (Hunk, int, error) {
	m := hunkHeaderRe.FindStringSubmatch(lines[start])
	if m == nil {
		return Hunk{}, start, fmt.Errorf("%w: bad hunk header %q", ErrMalformedDiff, lines[start])
	}

	oldStart, _ := strconv.Atoi(m[1])
	oldLines := 1
	if m[2] != "" {
		oldLines, _ = strconv.Atoi(m[2])
	}
	newStart, _ := strconv.Atoi(m[3])
	newLines := 1
	if m[4] != "" {
		newLines, _ = strconv.Atoi(m[4])
	}

	h := Hunk{
		OldStart: oldStart,
		OldLines: oldLines,
		NewStart: newStart,
		NewLines: newLines,
		Header:   strings.TrimSpace(m[5]),
	}

	oldNo, newNo := oldStart, newStart
	remOld, remNew := oldLines, newLines

	i := start + 1
	for i < len(lines) && (remOld > 0 || remNew > 0) {
		line := lines[i]
		if strings.HasPrefix(line, `\`) { // "\ No newline at end of file"
			i++
			continue
		}
		if strings.HasPrefix(line, "diff --git ") {
			return Hunk{}, i, fmt.Errorf("%w: unexpected end of hunk before %q", ErrMalformedDiff, line)
		}
		if line == "" {
			// Tolerate tools that strip the single-space prefix of empty context lines.
			h.Lines = append(h.Lines, DiffLine{Kind: LineContext, OldNo: oldNo, NewNo: newNo})
			oldNo++
			newNo++
			remOld--
			remNew--
			i++
			continue
		}

		switch line[0] {
		case ' ':
			h.Lines = append(h.Lines, DiffLine{Kind: LineContext, OldNo: oldNo, NewNo: newNo, Text: line[1:]})
			oldNo++
			newNo++
			remOld--
			remNew--
		case '+':
			h.Lines = append(h.Lines, DiffLine{Kind: LineAdded, NewNo: newNo, Text: line[1:]})
			newNo++
			remNew--
		case '-':
			h.Lines = append(h.Lines, DiffLine{Kind: LineRemoved, OldNo: oldNo, Text: line[1:]})
			oldNo++
			remOld--
		default:
			return Hunk{}, i, fmt.Errorf("%w: unexpected line %q in hunk", ErrMalformedDiff, line)
		}
		i++
	}

	if remOld > 0 || remNew > 0 {
		return Hunk{}, i, fmt.Errorf("%w: unexpected EOF in hunk", ErrMalformedDiff)
	}
	return h, i, nil
}

func splitPatchLines(patch []byte) []string {
	s := strings.ReplaceAll(string(patch), "\r\n", "\n")
	return strings.Split(s, "\n")
}

func parseGitHeaderPaths(line string) (oldPath, newPath string, ok bool) {
	rest := strings.TrimPrefix(line, "diff --git ")

	if strings.HasPrefix(rest, `"`) {
		first, tail, ok1 := takeQuoted(rest)
		second, _, ok2 := takeQuoted(strings.TrimSpace(tail))
		if ok1 && ok2 {
			return trimSidePrefix(first, "a/"), trimSidePrefix(second, "b/"), true
		}
	}

	idx := strings.Index(rest, " b/")
	if idx < 0 {
		return "", "", false
	}
	return trimSidePrefix(strings.TrimSpace(rest[:idx]), "a/"),
		trimSidePrefix(strings.TrimSpace(rest[idx+1:]), "b/"), true
}

func takeQuoted(s string) (val, rest string, ok bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", s, false
	}
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			u, err := strconv.Unquote(s[:i+1])
			if err != nil {
				return "", s, false
			}
			return u, s[i+1:], true
		}
	}
	return "", s, false
}

func cleanPathValue(p string) string {
	p = strings.TrimSpace(p)
	if idx := strings.IndexByte(p, '\t'); idx >= 0 {
		p = p[:idx]
	}
	if p == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(p, `"`) {
		if u, _, ok := takeQuoted(p); ok {
			p = u
		}
	}
	p = trimSidePrefix(p, "a/")
	return trimSidePrefix(p, "b/")
}

func trimSidePrefix(p, prefix string) string {
	p = strings.TrimSpace(p)
	if rest, ok := strings.CutPrefix(p, prefix); ok {
		return rest
	}
	return p
}
