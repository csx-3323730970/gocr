package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/csx-3323730970/gocr/internal/llm"
	"github.com/csx-3323730970/gocr/internal/report"
)

const modifyPatch = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,2 +1,3 @@
 package a
+import "os"
 func f() {}
`

func findingsResponse(file string, line int, snippet string) llm.Response {
	return llm.Response{Text: `{"findings":[{"file":"` + file + `","line":` + itoa(line) + `,"severity":"major","message":"defect","snippet":` + quote(snippet) + `}]}`}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func TestReviewPatchAnchorsFindings(t *testing.T) {
	provider := &llm.FakeProvider{Responses: []llm.Response{
		findingsResponse("a.go", 2, `import "os"`),
	}}
	p := New(provider, Options{})

	res, err := p.ReviewPatch(context.Background(), "", []byte(modifyPatch))
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 || res.Bundles != 1 {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("findings = %+v", res.Findings)
	}
	f := res.Findings[0]
	if f.Line != 2 || f.Anchor.Method != "exact" || f.Anchor.Confidence != 1 {
		t.Fatalf("finding = %+v", f)
	}
}

func TestReviewPatchDowngradesUnmatchedSnippet(t *testing.T) {
	provider := &llm.FakeProvider{Responses: []llm.Response{
		findingsResponse("a.go", 2, "completely unrelated content here"),
	}}
	p := New(provider, Options{})

	res, err := p.ReviewPatch(context.Background(), "", []byte(modifyPatch))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("findings = %+v", res.Findings)
	}
	f := res.Findings[0]
	if f.Line != 0 || f.Anchor.Method != "downgraded" {
		t.Fatalf("finding = %+v", f)
	}
}

func TestReviewPatchDropsUnknownFile(t *testing.T) {
	provider := &llm.FakeProvider{Responses: []llm.Response{
		findingsResponse("other.go", 2, "anything"),
	}}
	p := New(provider, Options{})

	res, err := p.ReviewPatch(context.Background(), "", []byte(modifyPatch))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("findings = %+v", res.Findings)
	}
}

func TestReviewPatchFiltersBinaryAndVendor(t *testing.T) {
	provider := &llm.FakeProvider{}
	p := New(provider, Options{})

	vendorPatch := `diff --git a/vendor/x.go b/vendor/x.go
--- a/vendor/x.go
+++ b/vendor/x.go
@@ -1 +1,2 @@
 package x
+var V = 1
`
	res, err := p.ReviewPatch(context.Background(), "", []byte(vendorPatch))
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 0 || res.Bundles != 0 || provider.CallCount() != 0 {
		t.Fatalf("res = %+v, calls = %d", res, provider.CallCount())
	}

	binaryPatch := `diff --git a/logo.png b/logo.png
index 1111111..2222222 100644
Binary files a/logo.png and b/logo.png differ
`
	res, err = p.ReviewPatch(context.Background(), "", []byte(binaryPatch))
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 0 || provider.CallCount() != 0 {
		t.Fatalf("res = %+v", res)
	}
}

func TestReviewPatchDedupes(t *testing.T) {
	response := `{"findings":[
		{"file":"a.go","line":2,"severity":"major","message":"defect","snippet":"import \"os\""},
		{"file":"a.go","line":2,"severity":"major","message":"defect","snippet":"import \"os\""}
	]}`
	provider := &llm.FakeProvider{Responses: []llm.Response{{Text: response}}}
	p := New(provider, Options{})

	res, err := p.ReviewPatch(context.Background(), "", []byte(modifyPatch))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("findings = %+v", res.Findings)
	}
	if res.Findings[0].Severity != report.SeverityMajor {
		t.Fatalf("severity = %v", res.Findings[0].Severity)
	}
}
