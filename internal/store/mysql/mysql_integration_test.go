//go:build integration

package mysql

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/csx-3323730970/gocr/internal/report"
	"github.com/csx-3323730970/gocr/internal/store"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dsn := os.Getenv("GOCR_TEST_DSN")
	if dsn == "" {
		t.Skip("GOCR_TEST_DSN not set")
	}

	ctx := context.Background()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	// GOCR_TEST_DSN must point at a disposable database.
	for _, q := range []string{"DELETE FROM findings", "DELETE FROM jobs", "DELETE FROM llm_cache"} {
		if _, err := db.db.ExecContext(ctx, q); err != nil {
			t.Fatalf("cleanup %q: %v", q, err)
		}
	}
	return db
}

func testKey(t *testing.T) string {
	t.Helper()
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())))
	return hex.EncodeToString(sum[:])
}

func TestEnqueueClaimComplete(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	j := store.Job{
		IdemKey: testKey(t),
		Repo:    "test/repo",
		BaseSHA: "base",
		HeadSHA: "head",
		Diff:    "diff --git a/a.go b/a.go\n",
	}
	id1, err := db.Enqueue(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := db.Enqueue(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("idempotent enqueue: %d vs %d", id1, id2)
	}

	got, err := db.Get(ctx, id1)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != store.StateQueued || got.Diff != j.Diff {
		t.Fatalf("job = %+v", got)
	}

	if n, err := db.CountQueued(ctx); err != nil || n != 1 {
		t.Fatalf("queued = %d, err = %v", n, err)
	}

	claimed, err := db.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != id1 || claimed.State != store.StateRunning || claimed.Attempts != 1 {
		t.Fatalf("claimed = %+v", claimed)
	}

	findings := []report.Finding{{
		File:     "a.go",
		Line:     2,
		Severity: report.SeverityMajor,
		Rule:     "ignored-error",
		Message:  "err ignored",
		Snippet:  "x, _ := f()",
		Anchor:   report.Anchor{Confidence: 0.9, Method: "fuzzy", Original: 3},
	}}
	if err := db.Complete(ctx, id1, findings); err != nil {
		t.Fatal(err)
	}

	got, err = db.Get(ctx, id1)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != store.StateSucceeded {
		t.Fatalf("state = %s", got.State)
	}

	stored, err := db.GetFindings(ctx, id1)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored findings = %+v", stored)
	}
	if stored[0].Anchor.Method != "fuzzy" || stored[0].Snippet != "x, _ = f()" && stored[0].Snippet != "x, _ := f()" {
		t.Fatalf("stored finding = %+v", stored[0])
	}

	if _, err := db.Claim(ctx); !errors.Is(err, store.ErrNoJob) {
		t.Fatalf("empty claim err = %v", err)
	}
}

func TestRequeueExpiredLease(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	j := store.Job{IdemKey: testKey(t), Repo: "test/repo", Diff: "diff\n"}
	id, err := db.Enqueue(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Claim(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx,
		`UPDATE jobs SET lease_until = DATE_SUB(NOW(), INTERVAL 1 MINUTE) WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}

	n, err := db.RequeueExpired(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("requeued = %d, want 1", n)
	}
	got, err := db.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != store.StateQueued {
		t.Fatalf("state = %s", got.State)
	}
}

func TestCache(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	key := testKey(t)
	payload := []byte(`{"text":"cached"}`)

	if err := db.Put(ctx, key, payload, 7, time.Minute); err != nil {
		t.Fatal(err)
	}
	cache := db.Cache()
	got, ok, err := cache.Get(ctx, key)
	if err != nil || !ok || string(got) != string(payload) {
		t.Fatalf("cache get: %s, ok=%v, err=%v", got, ok, err)
	}

	if _, err := db.db.ExecContext(ctx,
		`UPDATE llm_cache SET expires_at = DATE_SUB(NOW(), INTERVAL 1 MINUTE) WHERE cache_key = ?`, key); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cache.Get(ctx, key); err != nil || ok {
		t.Fatalf("expired cache: ok=%v, err=%v", ok, err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}
