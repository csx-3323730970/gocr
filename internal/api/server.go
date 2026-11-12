// Package api exposes the review service over HTTP.
package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/csx-3323730970/gocr/internal/report"
	"github.com/csx-3323730970/gocr/internal/store"
)

const maxRequestBody = 10 << 20

type Server struct {
	addr     string
	store    store.Store
	log      *slog.Logger
	http     *http.Server
	maxQueue int
}

func NewServer(addr string, st store.Store, log *slog.Logger, maxQueue int) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{addr: addr, store: st, log: log, maxQueue: maxQueue}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("POST /v1/reviews", s.handleSubmit)
	mux.HandleFunc("GET /v1/reviews/{id}", s.handleGet)

	s.http = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

func (s *Server) ListenAndServe() error {
	s.log.Info("http server listening", "addr", s.addr)
	if err := s.http.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type submitRequest struct {
	Repo string `json:"repo"`
	Diff string `json:"diff"`
	Base string `json:"base,omitempty"`
	Head string `json:"head,omitempty"`
}

type submitResponse struct {
	ID    int64  `json:"id"`
	State string `json:"state"`
}

// handleSubmit accepts a review job and returns 202 with a Location header.
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store unavailable"})
		return
	}

	var req submitRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	req.Repo = strings.TrimSpace(req.Repo)
	if req.Repo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is required"})
		return
	}
	if strings.TrimSpace(req.Diff) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "diff is required"})
		return
	}

	if s.maxQueue > 0 {
		queued, err := s.store.CountQueued(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "queue check failed"})
			return
		}
		if queued >= int64(s.maxQueue) {
			w.Header().Set("Retry-After", "5")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "queue is full"})
			return
		}
	}

	sum := sha256.Sum256([]byte(req.Repo + "\n" + req.Diff))
	id, err := s.store.Enqueue(r.Context(), store.Job{
		IdemKey: hex.EncodeToString(sum[:]),
		Repo:    req.Repo,
		BaseSHA: req.Base,
		HeadSHA: req.Head,
		Diff:    req.Diff,
	})
	if err != nil {
		s.log.Error("enqueue job", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "enqueue failed"})
		return
	}

	state := string(store.StateQueued)
	if job, err := s.store.Get(r.Context(), id); err == nil {
		state = string(job.State)
	}

	w.Header().Set("Location", fmt.Sprintf("/v1/reviews/%d", id))
	writeJSON(w, http.StatusAccepted, submitResponse{ID: id, State: state})
}

type reviewResponse struct {
	ID       int64            `json:"id"`
	State    string           `json:"state"`
	Error    string           `json:"error,omitempty"`
	Findings []report.Finding `json:"findings"`
}

// handleGet returns job state and findings.
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store unavailable"})
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid job id"})
		return
	}

	job, err := s.store.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	if err != nil {
		s.log.Error("get job", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "get job failed"})
		return
	}

	findings, err := s.store.GetFindings(r.Context(), id)
	if err != nil {
		s.log.Error("get findings", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "get findings failed"})
		return
	}

	writeJSON(w, http.StatusOK, reviewResponse{
		ID:       job.ID,
		State:    string(job.State),
		Error:    job.LastError,
		Findings: findings,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
