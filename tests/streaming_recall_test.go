package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/api"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// newStreamTestServer creates a test HTTP server for streaming recall tests.
func newStreamTestServer(t *testing.T, st *store.MockStore) *httptest.Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rec := recall.NewRecaller(recall.DefaultWeights(), logger)
	emb := &apiTestEmbedder{}
	srv := api.NewServer(st, rec, emb, logger, "")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// TestRecallStream_EventsOrdered verifies that GET /v1/recall/stream streams events in order
// and terminates with a [DONE] sentinel.
func TestRecallStream_EventsOrdered(t *testing.T) {
	st := store.NewMockStore()
	now := time.Now().UTC()

	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.5
	}

	memories := []models.Memory{
		{
			ID:         "stream-mem-1",
			Type:       models.MemoryTypeFact,
			Scope:      models.ScopePermanent,
			Visibility: models.VisibilityPrivate,
			Content:    "streaming test memory one",
			Confidence: 0.9,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		{
			ID:         "stream-mem-2",
			Type:       models.MemoryTypeFact,
			Scope:      models.ScopePermanent,
			Visibility: models.VisibilityPrivate,
			Content:    "streaming test memory two",
			Confidence: 0.85,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		{
			ID:         "stream-mem-3",
			Type:       models.MemoryTypeFact,
			Scope:      models.ScopePermanent,
			Visibility: models.VisibilityPrivate,
			Content:    "streaming test memory three",
			Confidence: 0.8,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	for i := range memories {
		if err := st.Upsert(context.Background(), memories[i], vec); err != nil {
			t.Fatalf("Upsert memory %d: %v", i, err)
		}
	}

	ts := newStreamTestServer(t, st)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		ts.URL+"/v1/recall/stream?q=streaming+test", http.NoBody)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/recall/stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}

	// Parse SSE events from response body.
	type streamEvent struct {
		Index  int           `json:"index"`
		Memory models.Memory `json:"memory"`
		Score  float64       `json:"score"`
	}

	var events []streamEvent
	var doneReceived bool

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			doneReceived = true
			break
		}
		var evt streamEvent
		if decErr := json.Unmarshal([]byte(payload), &evt); decErr != nil {
			t.Fatalf("unmarshal event %q: %v", payload, decErr)
		}
		events = append(events, evt)
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("scanner error: %v", scanErr)
	}

	if !doneReceived {
		t.Error("expected [DONE] sentinel but did not receive it")
	}

	if len(events) == 0 {
		t.Fatal("expected at least one streaming event, got none")
	}

	// Verify events are received in order (Index 0, 1, 2, ...).
	for i := range events {
		if events[i].Index != i {
			t.Errorf("event[%d].Index = %d, want %d", i, events[i].Index, i)
		}
	}
}

// TestRecallStream_MissingQuery verifies that GET /v1/recall/stream without q param returns 400.
func TestRecallStream_MissingQuery(t *testing.T) {
	st := store.NewMockStore()
	ts := newStreamTestServer(t, st)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		ts.URL+"/v1/recall/stream", http.NoBody)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/recall/stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}
