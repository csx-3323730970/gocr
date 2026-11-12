package obs

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	JobsInflight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gocr_jobs_inflight",
		Help: "Review jobs currently being processed.",
	})

	JobDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "gocr_job_duration_seconds",
		Help:    "End-to-end review job duration.",
		Buckets: prometheus.DefBuckets,
	})

	LLMRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gocr_llm_requests_total",
		Help: "LLM requests by provider and result (ok/retry/fatal).",
	}, []string{"provider", "result"})

	LLMTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gocr_llm_tokens_total",
		Help: "LLM tokens by kind (input/output).",
	}, []string{"kind"})

	CacheHitsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gocr_llm_cache_hits_total",
		Help: "Content-addressed LLM cache hits.",
	})

	AnchorMethodTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gocr_anchor_method_total",
		Help: "Findings by anchor method (exact/fuzzy/downgraded/dropped).",
	}, []string{"method"})
)
