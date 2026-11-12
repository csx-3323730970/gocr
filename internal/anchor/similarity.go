package anchor

import "strings"

// Normalize strips line comments and collapses whitespace before comparing.
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "//"); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.Index(s, "#"); idx >= 0 {
		s = s[:idx]
	}
	return strings.Join(strings.Fields(s), " ")
}

// Similarity returns the normalized Levenshtein ratio in [0,1].
func Similarity(a, b string) float64 {
	a = Normalize(a)
	b = Normalize(b)
	if a == b {
		if a == "" {
			return 1
		}
		return 1
	}
	if a == "" || b == "" {
		return 0
	}
	ra, rb := []rune(a), []rune(b)
	maxLen := len(ra)
	if len(rb) > maxLen {
		maxLen = len(rb)
	}
	return 1 - float64(levenshtein(ra, rb))/float64(maxLen)
}

func levenshtein(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
