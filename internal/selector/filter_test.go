package selector

import "testing"

func TestKeep(t *testing.T) {
	f := Filter{
		IgnoreGlobs:  append([]string{}, DefaultIgnoreGlobs...),
		MaxFileBytes: 100,
	}

	cases := []struct {
		path string
		size int64
		want bool
	}{
		{"internal/app.go", 10, true},
		{"vendor/github.com/x/y.go", 10, false},
		{"web/node_modules/x.js", 10, false},
		{"dist/bundle.js", 10, false},
		{"big.go", 101, false},
		{"go.sum", 5, false},
	}
	for _, c := range cases {
		if got := f.Keep(c.path, c.size); got != c.want {
			t.Errorf("Keep(%q,%d) = %v, want %v", c.path, c.size, got, c.want)
		}
	}
}

func TestKeepAllowlist(t *testing.T) {
	f := Filter{Allowlist: []string{"src/**"}}
	if !f.Keep("src/a.go", 1) {
		t.Fatal("src/a.go should be kept")
	}
	if f.Keep("cmd/a.go", 1) {
		t.Fatal("cmd/a.go should be filtered")
	}
}
