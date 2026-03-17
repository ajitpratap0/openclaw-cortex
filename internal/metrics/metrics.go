// Package metrics exposes Prometheus metrics for openclaw-cortex.
// All metrics are registered with the default Prometheus registry.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// MemoriesStoredTotal counts memories written to the store, by source (api, mcp, hook, cli).
	MemoriesStoredTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cortex_memories_stored_total",
		Help: "Total number of memories stored, by source.",
	}, []string{"source"})

	// RecallsTotal counts recall operations.
	RecallsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cortex_recalls_total",
		Help: "Total number of recall operations.",
	})

	// LLMCallsTotal counts LLM completions, by model name.
	LLMCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cortex_llm_calls_total",
		Help: "Total number of LLM completion calls, by model.",
	}, []string{"model"})

	// LLMErrorsTotal counts LLM completion errors, by model name.
	LLMErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cortex_llm_errors_total",
		Help: "Total number of LLM completion errors, by model.",
	}, []string{"model"})

	// RecallLatencyMs is a histogram of recall operation latency in milliseconds.
	RecallLatencyMs = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "cortex_recall_latency_ms",
		Help:    "Recall operation latency in milliseconds.",
		Buckets: []float64{10, 50, 100, 250, 500, 1000, 2500},
	})

	// EmbedLatencyMs is a histogram of embedding operation latency.
	EmbedLatencyMs = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "cortex_embed_latency_ms",
		Help:    "Embedding generation latency in milliseconds.",
		Buckets: []float64{5, 20, 50, 100, 250, 500},
	})

	// LLMLatencyMs is a histogram of LLM completion latency, by model.
	LLMLatencyMs = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cortex_llm_latency_ms",
		Help:    "LLM completion latency in milliseconds, by model.",
		Buckets: []float64{100, 250, 500, 1000, 2500, 5000},
	}, []string{"model"})

	// MemoryCount is a gauge of the total number of memories in the store.
	MemoryCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cortex_memory_count",
		Help: "Current total number of memories in the store.",
	})

	// DedupSkippedTotal counts memories skipped due to deduplication.
	DedupSkippedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cortex_dedup_skipped_total",
		Help: "Total number of memories skipped due to deduplication.",
	})

	// LifecycleExpiredTotal counts memories expired by the lifecycle manager.
	LifecycleExpiredTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cortex_lifecycle_expired_total",
		Help: "Total number of memories expired by the lifecycle manager.",
	})

	// LifecycleDecayedTotal counts memories decayed by the lifecycle manager.
	LifecycleDecayedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cortex_lifecycle_decayed_total",
		Help: "Total number of memories decayed by the lifecycle manager.",
	})

	// LifecycleRetiredTotal counts memories retired by the lifecycle manager.
	LifecycleRetiredTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cortex_lifecycle_retired_total",
		Help: "Total number of memories retired by the lifecycle manager.",
	})
)
