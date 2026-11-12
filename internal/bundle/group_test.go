package bundle

import (
	"reflect"
	"testing"

	"github.com/csx-3323730970/gocr/internal/source"
)

func TestGroup(t *testing.T) {
	files := []source.FileDiff{
		{Path: "cmd/main.go"},
		{Path: "internal/b.go"},
		{Path: "internal/a.go"},
		{Path: "README.md"},
	}

	bundles := Group(files, 5)
	want := []Bundle{
		{Paths: []string{"README.md"}},
		{Paths: []string{"cmd/main.go"}},
		{Paths: []string{"internal/a.go", "internal/b.go"}},
	}
	if !reflect.DeepEqual(bundles, want) {
		t.Fatalf("bundles = %#v, want %#v", bundles, want)
	}

	split := Group(files, 1)
	if len(split) != len(files) {
		t.Fatalf("split bundles = %d, want %d", len(split), len(files))
	}
}
