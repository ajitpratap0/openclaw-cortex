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
	LifecycleRetired = expvar.NewInt("cortex_lifecycle_retired_total")
)

// Async pipeline counters.
var (
	// AsyncInFlight tracks the number of work items currently being processed
	// by the async worker pool.  It is incremented on dequeue and decremented
	// when processing finishes (success or permanent failure).
	AsyncInFlight = expvar.NewInt("cortex_async_in_flight")

	// AsyncProcessedTotal counts work items that completed successfully.
	AsyncProcessedTotal = expvar.NewInt("cortex_async_processed_total")

	// AsyncFailedTotal counts work items that exhausted their retry budget and
	// were permanently marked as failed.
	AsyncFailedTotal = expvar.NewInt("cortex_async_failed_total")
)

// Inc increments the given counter by 1.
func Inc(counter *expvar.Int) { counter.Add(1) }
