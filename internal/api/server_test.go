package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/csx-3323730970/gocr/internal/report"
	"github.com/csx-3323730970/gocr/internal/store"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHealthz(t *testing.T) {
	s := NewServer(":0", nil, testLogger(), 0)

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "{\"status\":\"ok\"}\n" {
		t.Fatalf("body = %q", got)
	}
}

type fakeStore struct {
	mu       sync.Mutex
	jobs     map[int64]store.Job
	findings map[int64][]report.Finding
	idem     map[string]int64
	nextID   int64
	queued   int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		jobs:     map[int64]store.Job{},
		findings: map[int64][]report.Finding{},
		idem:     map[string]int64{},
	}
}

func (f *fakeStore) Enqueue(_ context.Context, j store.Job) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.idem[j.IdemKey]; ok {
		return id, nil
	}
	f.nextID++
	j.ID = f.nextID
	j.State = store.StateQueued
	f.jobs[j.ID] = j
	f.idem[j.IdemKey] = j.ID
	f.queued++
	return j.ID, nil
}

func (f *fakeStore) Get(_ context.Context, id int64) (store.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[id]
	if !ok {
		return store.Job{}, store.ErrNotFound
	}
	return j, nil
}

func (f *fakeStore) GetFindings(_ context.Context, id int64) ([]report.Finding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.findings[id], nil
}

func (f *fakeStore) Claim(context.Context) (store.Job, error) { return store.Job{}, store.ErrNoJob }

func (f *fakeStore) Complete(_ context.Context, id int64, findings []report.Finding) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j := f.jobs[id]
	j.State = store.StateSucceeded
	f.jobs[id] = j
	f.findings[id] = findings
	return nil
}

func (f *fakeStore) Fail(_ context.Context, id int64, cause string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j := f.jobs[id]
	j.State = store.StateFailed
	j.LastError = cause
	f.jobs[id] = j
	return nil
}

func (f *fakeStore) RequeueExpired(context.Context) (int64, error) { return 0, nil }

func (f *fakeStore) CountQueued(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queued, nil
}

func (f *fakeStore) Close() error { return nil }

func doRequest(s *Server, method, path, body string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(rec, req)
	return rec
}

func TestSubmitAndGet(t *testing.T) {
	st := newFakeStore()
	s := NewServer(":0", st, testLogger(), 10)

	rec := doRequest(s, "POST", "/v1/reviews", `{"repo":"acme/api","diff":"diff --git a/a.go b/a.go\n"}`)
	if rec.Code != 202 {
		t.Fatalf("submit status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var submitted submitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &submitted); err != nil {
		t.Fatal(err)
	}
	if submitted.ID != 1 || submitted.State != "queued" {
		t.Fatalf("submitted = %+v", submitted)
	}
	if loc := rec.Header().Get("Location"); loc != "/v1/reviews/1" {
		t.Fatalf("location = %q", loc)
	}

	// Same payload returns the same job id (idempotency key).
	rec = doRequest(s, "POST", "/v1/reviews", `{"repo":"acme/api","diff":"diff --git a/a.go b/a.go\n"}`)
	var submittedAgain submitResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &submittedAgain)
	if submittedAgain.ID != submitted.ID {
		t.Fatalf("second id = %d, want %d", submittedAgain.ID, submitted.ID)
	}

	st.mu.Lock()
	st.findings[1] = []report.Finding{{File: "a.go", Line: 2, Severity: report.SeverityMajor, Message: "x"}}
	st.jobs[1] = store.Job{ID: 1, State: store.StateSucceeded}
	st.mu.Unlock()

	rec = doRequest(s, "GET", "/v1/reviews/1", "")
	if rec.Code != 200 {
		t.Fatalf("get status = %d", rec.Code)
	}
	var got reviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.State != "succeeded" || len(got.Findings) != 1 {
		t.Fatalf("get response = %+v", got)
	}

	// Re-submitting an already finished job reports its real state.
	rec = doRequest(s, "POST", "/v1/reviews", `{"repo":"acme/api","diff":"diff --git a/a.go b/a.go\n"}`)
	var resubmitted submitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resubmitted); err != nil {
		t.Fatal(err)
	}
	if resubmitted.ID != 1 || resubmitted.State != "succeeded" {
		t.Fatalf("resubmitted = %+v", resubmitted)
	}
}

func TestSubmitValidation(t *testing.T) {
	s := NewServer(":0", newFakeStore(), testLogger(), 10)

	cases := []string{
		`{}`,
		`{"repo":"acme/api"}`,
		`{"repo":"","diff":"x"}`,
		`not json`,
	}
	for _, body := range cases {
		rec := doRequest(s, "POST", "/v1/reviews", body)
		if rec.Code != 400 {
			t.Errorf("body %q: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestSubmitQueueFull(t *testing.T) {
	st := newFakeStore()
	st.queued = 11
	s := NewServer(":0", st, testLogger(), 10)

	rec := doRequest(s, "POST", "/v1/reviews", `{"repo":"acme/api","diff":"x"}`)
	if rec.Code != 429 {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "5" {
		t.Fatalf("retry-after = %q", got)
	}
}

func TestGetErrors(t *testing.T) {
	s := NewServer(":0", newFakeStore(), testLogger(), 10)

	if rec := doRequest(s, "GET", "/v1/reviews/abc", ""); rec.Code != 400 {
		t.Fatalf("invalid id status = %d", rec.Code)
	}
	if rec := doRequest(s, "GET", "/v1/reviews/99", ""); rec.Code != 404 {
		t.Fatalf("missing job status = %d", rec.Code)
	}
}
