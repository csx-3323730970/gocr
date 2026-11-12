// Package mysql implements store.Store on MySQL 8 (InnoDB).
package mysql

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/csx-3323730970/gocr/internal/report"
	"github.com/csx-3323730970/gocr/internal/store"
)

const defaultLease = 5 * time.Minute

//go:embed migrations/*.sql
var migrationFS embed.FS

var _ store.Store = (*DB)(nil)

type DB struct {
	db    *sql.DB
	lease time.Duration
}

func Open(ctx context.Context, dsn string) (*DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(20)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	return &DB{db: db, lease: defaultLease}, nil
}

func (d *DB) Close() error { return d.db.Close() }

// Migrate applies embedded .sql files in filename order at most once each,
// tracked in schema_migrations.
func (d *DB) Migrate(ctx context.Context) error {
	if _, err := d.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(255) PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		var applied int
		if err := d.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, e.Name()).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}

		raw, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		for _, stmt := range strings.Split(string(raw), ";") {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := d.db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("apply %s: %w", e.Name(), err)
			}
		}
		if _, err := d.db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES (?)`, e.Name()); err != nil {
			return fmt.Errorf("record %s: %w", e.Name(), err)
		}
	}
	return nil
}

// Enqueue inserts a queued job. Duplicate idem_key returns the existing job id.
func (d *DB) Enqueue(ctx context.Context, j store.Job) (int64, error) {
	res, err := d.db.ExecContext(ctx, `INSERT INTO jobs (idem_key, repo, base_sha, head_sha, diff, state)
		VALUES (?, ?, ?, ?, ?, 'queued')
		ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		j.IdemKey, j.Repo, j.BaseSHA, j.HeadSHA, j.Diff)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const jobColumns = `id, idem_key, repo, base_sha, head_sha, COALESCE(diff, ''), state,
	attempts, lease_until, created_at, COALESCE(last_error, '')`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (store.Job, error) {
	var (
		j     store.Job
		lease sql.NullTime
	)
	err := row.Scan(&j.ID, &j.IdemKey, &j.Repo, &j.BaseSHA, &j.HeadSHA, &j.Diff,
		&j.State, &j.Attempts, &lease, &j.CreatedAt, &j.LastError)
	if err != nil {
		return store.Job{}, err
	}
	j.LeaseUntil = lease
	return j, nil
}

func (d *DB) Get(ctx context.Context, id int64) (store.Job, error) {
	j, err := scanJob(d.db.QueryRowContext(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return store.Job{}, store.ErrNotFound
	}
	if err != nil {
		return store.Job{}, err
	}
	return j, nil
}

func (d *DB) GetFindings(ctx context.Context, id int64) ([]report.Finding, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT file, line, severity, rule_name, message,
		COALESCE(snippet, ''), confidence, method, original_line
		FROM findings WHERE job_id = ? ORDER BY file, line, id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]report.Finding, 0)
	for rows.Next() {
		var (
			f        report.Finding
			severity string
		)
		if err := rows.Scan(&f.File, &f.Line, &severity, &f.Rule, &f.Message,
			&f.Snippet, &f.Anchor.Confidence, &f.Anchor.Method, &f.Anchor.Original); err != nil {
			return nil, err
		}
		f.Severity = report.Severity(severity)
		out = append(out, f)
	}
	return out, rows.Err()
}

// Claim atomically leases one queued job with SKIP LOCKED. The transaction only
// covers the claim; LLM calls never run inside it.
func (d *DB) Claim(ctx context.Context) (store.Job, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Job{}, err
	}
	defer tx.Rollback()

	j, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+`
		FROM jobs
		WHERE state = 'queued' AND (lease_until IS NULL OR lease_until < NOW())
		ORDER BY created_at, id
		LIMIT 1
		FOR UPDATE SKIP LOCKED`))
	if errors.Is(err, sql.ErrNoRows) {
		return store.Job{}, store.ErrNoJob
	}
	if err != nil {
		return store.Job{}, err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE jobs
		SET state = 'running', lease_until = DATE_ADD(NOW(), INTERVAL ? SECOND), attempts = attempts + 1
		WHERE id = ?`, int(d.lease.Seconds()), j.ID); err != nil {
		return store.Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.Job{}, err
	}

	j.State = store.StateRunning
	j.Attempts++
	j.LeaseUntil = sql.NullTime{Time: time.Now().Add(d.lease), Valid: true}
	return j, nil
}

func (d *DB) Complete(ctx context.Context, id int64, findings []report.Finding) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE jobs
		SET state = 'succeeded', lease_until = NULL, last_error = NULL
		WHERE id = ? AND state = 'running'`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM findings WHERE job_id = ?`, id); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO findings
		(job_id, file, line, severity, rule_name, message, snippet, confidence, method, original_line)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, f := range findings {
		if _, err := stmt.ExecContext(ctx, id, f.File, f.Line, string(f.Severity), f.Rule,
			f.Message, f.Snippet, f.Anchor.Confidence, f.Anchor.Method, f.Anchor.Original); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) Fail(ctx context.Context, id int64, cause string) error {
	_, err := d.db.ExecContext(ctx, `UPDATE jobs
		SET state = 'failed', last_error = ?, lease_until = NULL
		WHERE id = ?`, truncateString(cause, 2000), id)
	return err
}

// RequeueExpired returns jobs with expired leases to the queue. Jobs that
// already exhausted three attempts are marked failed instead.
func (d *DB) RequeueExpired(ctx context.Context) (int64, error) {
	if _, err := d.db.ExecContext(ctx, `UPDATE jobs
		SET state = 'failed', last_error = 'lease expired after max attempts', lease_until = NULL
		WHERE state = 'running' AND lease_until < NOW() AND attempts >= 3`); err != nil {
		return 0, err
	}
	res, err := d.db.ExecContext(ctx, `UPDATE jobs
		SET state = 'queued', lease_until = NULL
		WHERE state = 'running' AND lease_until < NOW()`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DB) CountQueued(ctx context.Context) (int64, error) {
	var n int64
	if err := d.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs WHERE state = 'queued'`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (d *DB) CacheGet(ctx context.Context, key string) ([]byte, bool, error) {
	var payload []byte
	err := d.db.QueryRowContext(ctx, `SELECT payload FROM llm_cache
		WHERE cache_key = ? AND expires_at > NOW()`, key).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return payload, true, nil
}

// cacheAdapter exposes the cache methods without colliding with
// Store.Get(int64), since Go does not support method overloading.
type cacheAdapter struct{ db *DB }

var _ store.Cache = cacheAdapter{}

func (a cacheAdapter) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return a.db.CacheGet(ctx, key)
}

func (a cacheAdapter) Put(ctx context.Context, key string, payload []byte, tokens int, ttl time.Duration) error {
	return a.db.Put(ctx, key, payload, tokens, ttl)
}

// Cache returns an LLM response cache backed by the llm_cache table.
func (d *DB) Cache() store.Cache { return cacheAdapter{db: d} }

func (d *DB) Put(ctx context.Context, key string, payload []byte, tokens int, ttl time.Duration) error {
	seconds := int(ttl.Seconds())
	if seconds <= 0 {
		seconds = 3600
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO llm_cache (cache_key, payload, tokens, expires_at)
		VALUES (?, ?, ?, DATE_ADD(NOW(), INTERVAL ? SECOND))
		ON DUPLICATE KEY UPDATE payload = VALUES(payload), tokens = VALUES(tokens), expires_at = VALUES(expires_at)`,
		key, payload, tokens, seconds)
	return err
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
