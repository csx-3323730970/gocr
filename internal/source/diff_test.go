package source

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseFileDiffsBasic(t *testing.T) {
	patch := `diff --git a/foo.go b/foo.go
index 1111111..2222222 100644
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package foo

+import "fmt"
 func bar() {}
`
	files, err := ParseFileDiffs([]byte(patch))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	f := files[0]
	if f.Path != "foo.go" || f.OldPath != "foo.go" || f.Status != "modified" {
		t.Fatalf("unexpected file meta: %+v", f)
	}
	if len(f.Hunks) != 1 {
		t.Fatalf("hunks = %d, want 1", len(f.Hunks))
	}
	h := f.Hunks[0]
	if h.OldStart != 1 || h.OldLines != 3 || h.NewStart != 1 || h.NewLines != 4 {
		t.Fatalf("unexpected hunk range: %+v", h)
	}
	want := []DiffLine{
		{Kind: LineContext, OldNo: 1, NewNo: 1, Text: "package foo"},
		{Kind: LineContext, OldNo: 2, NewNo: 2, Text: ""},
		{Kind: LineAdded, NewNo: 3, Text: `import "fmt"`},
		{Kind: LineContext, OldNo: 3, NewNo: 4, Text: "func bar() {}"},
	}
	if !reflect.DeepEqual(h.Lines, want) {
		t.Fatalf("lines = %#v, want %#v", h.Lines, want)
	}
}

func TestParseU0(t *testing.T) {
	patch := `diff --git a/x.go b/x.go
--- a/x.go
+++ b/x.go
@@ -10,0 +11,2 @@
+line one
+line two
`
	files, err := ParseFileDiffs([]byte(patch))
	if err != nil {
		t.Fatal(err)
	}
	h := files[0].Hunks[0]
	if h.OldLines != 0 || h.NewLines != 2 {
		t.Fatalf("unexpected range: %+v", h)
	}
	if len(h.Lines) != 2 || h.Lines[0].NewNo != 11 || h.Lines[1].NewNo != 12 {
		t.Fatalf("lines = %+v", h.Lines)
	}
}

func TestParseNewFile(t *testing.T) {
	patch := "diff --git a/new.go b/new.go\n" +
		"new file mode 100644\n" +
		"index 0000000..e69de29\n" +
		"--- /dev/null\n" +
		"+++ b/new.go\n" +
		"@@ -0,0 +1,2 @@\n" +
		"+package new\n" +
		"+\n"
	files, err := ParseFileDiffs([]byte(patch))
	if err != nil {
		t.Fatal(err)
	}
	f := files[0]
	if f.Status != "added" || f.Path != "new.go" || f.OldPath != "" {
		t.Fatalf("unexpected file: %+v", f)
	}
	if len(f.Hunks[0].Lines) != 2 {
		t.Fatalf("lines = %+v", f.Hunks[0].Lines)
	}
}

func TestParseDeletedFile(t *testing.T) {
	patch := `diff --git a/old.go b/old.go
deleted file mode 100644
index e69de29..0000000
--- a/old.go
+++ /dev/null
@@ -1,1 +0,0 @@
-package old
`
	files, err := ParseFileDiffs([]byte(patch))
	if err != nil {
		t.Fatal(err)
	}
	f := files[0]
	if f.Status != "deleted" || f.Path != "old.go" {
		t.Fatalf("unexpected file: %+v", f)
	}
	if got := f.CommentableLines(); len(got) != 0 {
		t.Fatalf("commentable lines = %v, want none", got)
	}
}

func TestParseRename(t *testing.T) {
	patch := `diff --git a/old_name.go b/new_name.go
similarity index 100%
rename from old_name.go
rename to new_name.go
`
	files, err := ParseFileDiffs([]byte(patch))
	if err != nil {
		t.Fatal(err)
	}
	f := files[0]
	if f.Status != "renamed" || f.OldPath != "old_name.go" || f.Path != "new_name.go" {
		t.Fatalf("unexpected file: %+v", f)
	}
}

func TestParseBinary(t *testing.T) {
	patch := `diff --git a/logo.png b/logo.png
index 1111111..2222222 100644
Binary files a/logo.png and b/logo.png differ
`
	files, err := ParseFileDiffs([]byte(patch))
	if err != nil {
		t.Fatal(err)
	}
	if !files[0].IsBinary || files[0].Path != "logo.png" {
		t.Fatalf("unexpected file: %+v", files[0])
	}
}

func TestParseCRLF(t *testing.T) {
	patch := strings.ReplaceAll(`diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,1 +1,2 @@
 package a
+var x = 1
`, "\n", "\r\n")

	files, err := ParseFileDiffs([]byte(patch))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range files[0].Hunks[0].Lines {
		if strings.Contains(l.Text, "\r") {
			t.Fatalf("line contains CR: %q", l.Text)
		}
	}
}

func TestParseMultipleHunks(t *testing.T) {
	patch := `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,2 +1,2 @@
 a
-b
+c
@@ -10,2 +10,3 @@ func x()
 d
+e
 f
`
	files, err := ParseFileDiffs([]byte(patch))
	if err != nil {
		t.Fatal(err)
	}
	hunks := files[0].Hunks
	if len(hunks) != 2 {
		t.Fatalf("hunks = %d, want 2", len(hunks))
	}
	if hunks[1].Header != "func x()" {
		t.Fatalf("header = %q", hunks[1].Header)
	}
	want := []int{1, 2, 10, 11, 12}
	if got := files[0].CommentableLines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("commentable = %v, want %v", got, want)
	}
}

func TestParseQuotedPath(t *testing.T) {
	patch := `diff --git "a/dir with space/f.go" "b/dir with space/f.go"
new file mode 100644
--- /dev/null
+++ "b/dir with space/f.go"
@@ -0,0 +1,1 @@
+package f
`
	files, err := ParseFileDiffs([]byte(patch))
	if err != nil {
		t.Fatal(err)
	}
	if files[0].Path != "dir with space/f.go" {
		t.Fatalf("path = %q", files[0].Path)
	}
}

func TestParseMalformed(t *testing.T) {
	badHeader := []byte("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ oops @@\n+x\n")
	if _, err := ParseFileDiffs(badHeader); !errors.Is(err, ErrMalformedDiff) {
		t.Fatalf("bad header err = %v", err)
	}

	truncated := []byte("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,5 +1,5 @@\n+x\n")
	if _, err := ParseFileDiffs(truncated); !errors.Is(err, ErrMalformedDiff) {
		t.Fatalf("truncated err = %v", err)
	}
}

func TestFileDiffCommentableLines(t *testing.T) {
	fd := FileDiff{
		Path: "a.go",
		Hunks: []Hunk{
			{
				Lines: []DiffLine{
					{Kind: LineContext, OldNo: 10, NewNo: 10},
					{Kind: LineAdded, NewNo: 11},
					{Kind: LineRemoved, OldNo: 11},
					{Kind: LineAdded, NewNo: 12},
				},
			},
		},
	}

	got := fd.CommentableLines()
	want := []int{10, 11, 12}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CommentableLines() = %v, want %v", got, want)
	}
}

func FuzzParseFileDiffs(f *testing.F) {
	f.Add([]byte("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-a\n+b\n"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, patch []byte) {
		_, _ = ParseFileDiffs(patch)
	})
}
