// Package job runs the claim -> process -> persist loop.
package job

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/csx-3323730970/gocr/internal/obs"
	"github.com/csx-3323730970/gocr/internal/report"
	"github.com/csx-3323730970/gocr/internal/store"
)

const (
	workerIdleSleep      = 750 * time.Millisecond
	leaseRequeueInterval = time.Minute
	jobTimeout           = 10 * time.Minute
)

type Processor interface {
	Process(ctx context.Context, j store.Job) ([]report.Finding, error)
}

type Worker struct {
	store store.Store
	proc  Processor
	log   *slog.Logger
}

func NewWorker(st store.Store, proc Processor, log *slog.Logger) *Worker {
	return &Worker{store: st, proc: proc, log: log}
}

// Run claims and processes jobs until ctx is canceled.
func (w *Worker) Run(ctx context.Context) {
	log := w.log
	if log == nil {
		log = slog.Default()
	}

	w.requeueExpired(ctx, log)

	idle := time.NewTicker(workerIdleSleep)
	defer idle.Stop()
	maintenance := time.NewTicker(leaseRequeueInterval)
	defer maintenance.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		j, err := w.store.Claim(ctx)
		if errors.Is(err, store.ErrNoJob) {
			select {
			case <-ctx.Done():
				return
			case <-idle.C:
			case <-maintenance.C:
				w.requeueExpired(ctx, log)
			}
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn("claim job", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-idle.C:
			}
			continue
		}

		w.process(ctx, log, j)
	}
}

func (w *Worker) process(ctx context.Context, log *slog.Logger, j store.Job) {
	obs.JobsInflight.Inc()
	defer obs.JobsInflight.Dec()
	start := time.Now()
	defer func() {
		obs.JobDurationSeconds.Observe(time.Since(start).Seconds())
	}()

	jobCtx, cancel := context.WithTimeout(ctx, jobTimeout)
	defer cancel()

	findings, err := w.proc.Process(jobCtx, j)
	if err != nil {
		if ctx.Err() != nil {
			// Shutting down: leave the lease to expire so another worker retries.
			return
		}
		log.Warn("job failed", "job_id", j.ID, "err", err)
		if failErr := w.store.Fail(ctx, j.ID, err.Error()); failErr != nil {
			log.Error("mark failed", "job_id", j.ID, "err", failErr)
		}
		return
	}

	if err := w.store.Complete(ctx, j.ID, findings); err != nil {
		log.Error("complete job", "job_id", j.ID, "err", err)
	}
}

func (w *Worker) requeueExpired(ctx context.Context, log *slog.Logger) {
	n, err := w.store.RequeueExpired(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Warn("requeue expired jobs", "err", err)
		}
		return
	}
	if n > 0 {
		log.Info("requeued expired jobs", "count", n)
	}
}
