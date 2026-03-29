// Package async provides a durable work queue backed by an append-only JSONL
// write-ahead log (WAL).  Workers consume items through a buffered Go channel;
// the WAL ensures that items survive process restarts even when the channel has
// not yet been drained.
package async

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// WALState represents the lifecycle state of a WAL entry.
type WALState string

const (
	// WALStatePending indicates the item has been enqueued but not yet picked up.
	WALStatePending WALState = "pending"
	// WALStateProcessing is reserved for crash-recovery semantics: items remain
	// in WALStatePending in the WAL until Complete or Fail is called.  If a
	// process crashes while an item is in-flight, it will be replayed as pending
	// on the next startup.  WALStateProcessing is never written by the current
	// implementation; it is kept here so that WAL files from future versions that
	// do write this state are replayed correctly (treated the same as pending).
	WALStateProcessing WALState = "processing"
	// WALStateDone indicates the item was processed successfully.
	WALStateDone WALState = "done"
	// WALStateFailed indicates the item failed processing.
	WALStateFailed WALState = "failed"
)

// WorkItem is the unit of work passed through the channel to workers.
type WorkItem struct {
	ID         string // uuid
	MemoryID   string // uuid of the stored memory
	Content    string // memory content for LLM processing
	Project    string // project scope
	SessionID  string // originating session
	EnqueuedAt time.Time
	Attempts   int // number of processing attempts so far; persisted across re-enqueues
}

// WALEntry is a single line in the append-only JSONL WAL file.
type WALEntry struct {
	ID         string    `json:"id"`
	MemoryID   string    `json:"memory_id"`
	Content    string    `json:"content"`
	Project    string    `json:"project"`
	SessionID  string    `json:"session_id"`
	EnqueuedAt time.Time `json:"enqueued_at"`
	Attempts   int       `json:"attempts,omitempty"` // persisted so retries survive re-enqueue
	State      WALState  `json:"state"`
	Error      string    `json:"error,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Enqueuer is the interface used by cmd/ packages to avoid importing the full Queue.
type Enqueuer interface {
	Enqueue(item WorkItem) error
}

// QueueStatus holds observable queue metrics.
type QueueStatus struct {
	ChannelDepth int
	TotalPending int64
	TotalFailed  int64
}

// Queue is the core work queue: append-only JSONL WAL + buffered Go channel.
type Queue struct {
	mu           sync.Mutex
	walPath      string
	ch           chan WorkItem
	compactEvery int
	enqueueCount int64 // total successful Enqueue calls since last compact
	logger       *slog.Logger
}

// NewQueue opens (or creates) the WAL file at walPath, replays pending and
// processing entries back into the channel, and then compacts the WAL so only
// pending items remain on disk.  capacity is the buffered-channel size;
// compactEvery controls how many successful Enqueue calls trigger an automatic
// background compact (0 disables automatic compaction).
func NewQueue(walPath string, capacity int, compactEvery int) (*Queue, error) {
	q := &Queue{
		walPath:      walPath,
		ch:           make(chan WorkItem, capacity),
		compactEvery: compactEvery,
		logger:       slog.Default(),
	}

	if err := q.replay(); err != nil {
		return nil, fmt.Errorf("async.NewQueue replay: %w", err)
	}

	// Compact after replay so the WAL only contains pending items.
	if err := q.Compact(); err != nil {
		return nil, fmt.Errorf("async.NewQueue compact: %w", err)
	}

	return q, nil
}

// NewQueueReadOnly opens the WAL at walPath for read-only inspection.  Unlike
// NewQueue it does NOT replay items into a channel and does NOT compact the
// WAL, so it is safe to use for status checks (e.g. "worker status") without
// mutating the queue state.  The returned Queue can only be used with Status();
// calls to Enqueue, Complete, Fail, Compact, or Close are no-ops or may panic.
func NewQueueReadOnly(walPath string) (*Queue, error) {
	q := &Queue{
		walPath: walPath,
		ch:      make(chan WorkItem), // never written; just keeps the type consistent
		logger:  slog.Default(),
	}
	// Create the WAL file if it doesn't exist yet (first-run case).
	f, err := os.OpenFile(walPath, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("async.NewQueueReadOnly open: %w", err)
	}
	_ = f.Close()
	return q, nil
}

// replay reads all WAL lines, keeps the last entry per ID (by max UpdatedAt),
// and re-enqueues items in pending or processing state.
func (q *Queue) replay() error {
	f, err := os.OpenFile(q.walPath, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open wal for replay: %w", err)
	}
	defer func() { _ = f.Close() }()

	byID := map[string]WALEntry{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry WALEntry
		if jsonErr := json.Unmarshal(line, &entry); jsonErr != nil {
			// Skip malformed lines; log but do not abort.
			q.logger.Warn("async: skipping malformed WAL line", "err", jsonErr)
			continue
		}
		existing, ok := byID[entry.ID]
		if !ok || entry.UpdatedAt.After(existing.UpdatedAt) {
			byID[entry.ID] = entry
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return fmt.Errorf("scanning wal: %w", scanErr)
	}

	// Re-enqueue surviving pending/processing entries (non-blocking).
	for i := range byID {
		entry := byID[i]
		if entry.State != WALStatePending && entry.State != WALStateProcessing {
			continue
		}
		item := WorkItem{
			ID:         entry.ID,
			MemoryID:   entry.MemoryID,
			Content:    entry.Content,
			Project:    entry.Project,
			SessionID:  entry.SessionID,
			EnqueuedAt: entry.EnqueuedAt,
			Attempts:   entry.Attempts,
		}
		select {
		case q.ch <- item:
		default:
			q.logger.Warn("async: channel full during replay, item stays in WAL",
				"id", entry.ID)
		}
	}
	return nil
}

// appendWALEntry appends a single WALEntry as a JSON line to the WAL file,
// then fsyncs to ensure durability.
func (q *Queue) appendWALEntry(entry WALEntry) error {
	f, err := os.OpenFile(q.walPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open wal for append: %w", err)
	}
	defer func() { _ = f.Close() }()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal wal entry: %w", err)
	}
	data = append(data, '\n')

	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("write wal entry: %w", err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("fsync wal: %w", err)
	}
	return nil
}

// Enqueue durably records the work item in the WAL (with fsync) and then
// attempts a non-blocking send on the channel.  If the channel is full the item
// remains safely in the WAL and will be replayed on the next startup.
// Enqueue satisfies the Enqueuer interface.
func (q *Queue) Enqueue(item WorkItem) error {
	if item.ID == "" {
		item.ID = uuid.New().String()
	}
	if item.EnqueuedAt.IsZero() {
		item.EnqueuedAt = time.Now().UTC()
	}

	entry := WALEntry{
		ID:         item.ID,
		MemoryID:   item.MemoryID,
		Content:    item.Content,
		Project:    item.Project,
		SessionID:  item.SessionID,
		EnqueuedAt: item.EnqueuedAt,
		Attempts:   item.Attempts,
		State:      WALStatePending,
		UpdatedAt:  time.Now().UTC(),
	}

	q.mu.Lock()
	err := q.appendWALEntry(entry)
	q.mu.Unlock()
	if err != nil {
		return fmt.Errorf("async.Enqueue: %w", err)
	}

	// Non-blocking channel send — the WAL is the source of truth.
	select {
	case q.ch <- item:
	default:
		q.logger.Warn("async: channel full, item durable in WAL only", "id", item.ID)
	}

	count := atomic.AddInt64(&q.enqueueCount, 1)
	if q.compactEvery > 0 && count%int64(q.compactEvery) == 0 {
		if compactErr := q.Compact(); compactErr != nil {
			q.logger.Warn("async: background compact failed", "err", compactErr)
		}
	}

	return nil
}

// C returns the receive-only channel for workers to consume work items.
func (q *Queue) C() <-chan WorkItem {
	return q.ch
}

// Close closes the underlying work channel, signaling workers that no more
// items will be sent.  Callers must ensure no concurrent Enqueue calls are
// made after Close.
func (q *Queue) Close() {
	close(q.ch)
}

// Complete appends a done tombstone for the given item ID.
func (q *Queue) Complete(id string) {
	entry := WALEntry{
		ID:        id,
		State:     WALStateDone,
		UpdatedAt: time.Now().UTC(),
	}
	q.mu.Lock()
	err := q.appendWALEntry(entry)
	q.mu.Unlock()
	if err != nil {
		q.logger.Warn("async: failed to write done tombstone", "id", id, "err", err)
	}
}

// Fail appends a failed tombstone for the given item ID, recording the error.
func (q *Queue) Fail(id string, failErr error) {
	errStr := ""
	if failErr != nil {
		errStr = failErr.Error()
	}
	entry := WALEntry{
		ID:        id,
		State:     WALStateFailed,
		Error:     errStr,
		UpdatedAt: time.Now().UTC(),
	}
	q.mu.Lock()
	err := q.appendWALEntry(entry)
	q.mu.Unlock()
	if err != nil {
		q.logger.Warn("async: failed to write failed tombstone", "id", id, "err", err)
	}
}

// Status returns observable metrics about the queue.
//
// The WAL scan is performed without holding the queue mutex so that
// concurrent Enqueue/Complete/Fail calls are not blocked while status
// is being computed.  A partial last line (from a concurrent write) is
// silently skipped by the JSON parser, which is acceptable for a
// best-effort status report.
func (q *Queue) Status() QueueStatus {
	var pending, failed int64

	f, err := os.OpenFile(q.walPath, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		q.logger.Warn("async: Status could not open WAL", "err", err)
		return QueueStatus{ChannelDepth: len(q.ch)}
	}
	defer func() { _ = f.Close() }()

	byID := map[string]WALEntry{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry WALEntry
		if jsonErr := json.Unmarshal(line, &entry); jsonErr != nil {
			continue
		}
		existing, ok := byID[entry.ID]
		if !ok || entry.UpdatedAt.After(existing.UpdatedAt) {
			byID[entry.ID] = entry
		}
	}

	for i := range byID {
		entry := byID[i]
		switch entry.State {
		case WALStatePending, WALStateProcessing:
			pending++
		case WALStateFailed:
			failed++
		}
	}

	return QueueStatus{
		ChannelDepth: len(q.ch),
		TotalPending: pending,
		TotalFailed:  failed,
	}
}

// Compact rewrites the WAL keeping only entries whose last-seen state is
// pending (done and failed entries are discarded).  This keeps the WAL file
// from growing unboundedly.
func (q *Queue) Compact() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	f, err := os.OpenFile(q.walPath, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("compact open wal: %w", err)
	}

	byID := map[string]WALEntry{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry WALEntry
		if jsonErr := json.Unmarshal(line, &entry); jsonErr != nil {
			q.logger.Warn("async: compact skipping malformed line", "err", jsonErr)
			continue
		}
		existing, ok := byID[entry.ID]
		if !ok || entry.UpdatedAt.After(existing.UpdatedAt) {
			byID[entry.ID] = entry
		}
	}
	scanErr := scanner.Err()
	_ = f.Close()
	if scanErr != nil {
		return fmt.Errorf("compact scan: %w", scanErr)
	}

	// Write surviving pending and failed entries to a temp file then rename.
	// Failed entries are preserved so Status().TotalFailed remains accurate
	// across compaction cycles.  Done entries are discarded to bound WAL growth.
	tmpPath := q.walPath + ".compact.tmp"
	tmp, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("compact create tmp: %w", err)
	}

	w := bufio.NewWriter(tmp)
	for i := range byID {
		entry := byID[i]
		if entry.State != WALStatePending && entry.State != WALStateProcessing && entry.State != WALStateFailed {
			continue
		}
		data, marshalErr := json.Marshal(entry)
		if marshalErr != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("compact marshal: %w", marshalErr)
		}
		if _, writeErr := w.Write(append(data, '\n')); writeErr != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("compact write: %w", writeErr)
		}
	}

	if flushErr := w.Flush(); flushErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("compact flush: %w", flushErr)
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("compact sync: %w", syncErr)
	}
	_ = tmp.Close()

	if renameErr := os.Rename(tmpPath, q.walPath); renameErr != nil {
		return fmt.Errorf("compact rename: %w", renameErr)
	}

	// Fsync the parent directory so the rename is durable across crashes.
	dir, dirErr := os.Open(filepath.Dir(q.walPath))
	if dirErr != nil {
		return fmt.Errorf("compact open dir for fsync: %w", dirErr)
	}
	if syncErr := dir.Sync(); syncErr != nil {
		_ = dir.Close()
		return fmt.Errorf("compact dir fsync: %w", syncErr)
	}
	_ = dir.Close()

	return nil
}
