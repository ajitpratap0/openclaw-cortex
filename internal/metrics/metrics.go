// Package metrics provides application-level counters using stdlib expvar.
// Counters are automatically exported on the /debug/vars HTTP endpoint
// when net/http/pprof is imported in the main binary.
package metrics

import "expvar"

// Operation counters.
var (
	RecallTotal      = expvar.NewInt("cortex_recall_total")
	CaptureTotal     = expvar.NewInt("cortex_capture_total")
	StoreTotal       = expvar.NewInt("cortex_store_total")
	DedupSkipped     = expvar.NewInt("cortex_dedup_skipped_total")
	LifecycleExpired = expvar.NewInt("cortex_lifecycle_expired_total")
	LifecycleDecayed = expvar.NewInt("cortex_lifecycle_decayed_total")
)

// Inc increments the given counter by 1.
func Inc(counter *expvar.Int) { counter.Add(1) }
