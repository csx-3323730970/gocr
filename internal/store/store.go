// Package store defines the persistence contract for review jobs.
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/csx-3323730970/gocr/internal/report"
)

type JobState string

const (
	StateQueued    JobState = "queued"
	StateRunning   JobState = "running"
	StateSucceeded JobState = "succeeded"
	StateFailed    JobState = "failed"
	StateCanceled  JobState = "canceled"
)

var (
	ErrNotFound = errors.New("store: job not found")
	ErrNoJob    = errors.New("store: no queued job")
)

type Job struct {
	ID         int64
	IdemKey    string
	Repo       string
	BaseSHA    string
	HeadSHA    string
	Diff       string
	State      JobState
	Attempts   int
	LeaseUntil sql.NullTime
	CreatedAt  time.Time
	LastError  string
}

// Store persists jobs and findings. The MySQL implementation is the only one
// used in production; integration tests hit a real MySQL instance (ADR-0003).
type Store interface {
	Enqueue(ctx context.Context, j Job) (id int64, err error)
	Get(ctx context.Context, id int64) (Job, error)
	GetFindings(ctx context.Context, id int64) ([]report.Finding, error)
	Claim(ctx context.Context) (Job, error)
	Complete(ctx context.Context, id int64, findings []report.Finding) error
	Fail(ctx context.Context, id int64, cause string) error
	RequeueExpired(ctx context.Context) (int64, error)
	CountQueued(ctx context.Context) (int64, error)
	Close() error
}

// Cache is a content-addressed LLM response cache.
type Cache interface {
	Get(ctx context.Context, key string) (payload []byte, ok bool, err error)
	Put(ctx context.Context, key string, payload []byte, tokens int, ttl time.Duration) error
}
