package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/api"
	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/hooks"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// newPropTestServer creates a test HTTP server using a shared MockStore for propagation tests.
func newPropTestServer(t *testing.T, st *store.MockStore) *httptest.Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rec := recall.NewRecaller(recall.DefaultWeights(), logger)
	emb := &apiTestEmbedder{}
	srv := api.NewServer(st, rec, emb, logger, "")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// TestAPI_UserID_Remember verifies that POST /v1/remember with X-User-ID stores UserID on the memory.
func TestAPI_UserID_Remember(t *testing.T) {
	st := store.NewMockStore()
	ts := newPropTestServer(t, st)

	body, _ := json.Marshal(map[string]any{
		"content": "Alice prefers dark mode",
		"type":    "fact",
	})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		ts.URL+"/v1/remember", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "user-test")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/remember: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var remResp struct {
		ID     string `json:"id"`
		Stored bool   `json:"stored"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&remResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !remResp.Stored {
		t.Fatal("expected stored=true")
	}

	// Verify directly from store that UserID was persisted.
	mem, getErr := st.Get(context.Background(), remResp.ID)
	if getErr != nil {
		t.Fatalf("store.Get: %v", getErr)
	}
	if mem.UserID != "user-test" {
		t.Errorf("UserID: got %q, want %q", mem.UserID, "user-test")
	}
}

// TestAPI_UserID_List verifies that GET /v1/memories with X-User-ID returns only that user's memories.
func TestAPI_UserID_List(t *testing.T) {
	st := store.NewMockStore()

	now := time.Now().UTC()
	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.1
	}

	alice := models.Memory{
		ID:         "list-alice-1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityPrivate,
		Content:    "Alice content",
		Confidence: 0.9,
		UserID:     "alice",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	bob := models.Memory{
		ID:         "list-bob-1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityPrivate,
		Content:    "Bob content",
		Confidence: 0.9,
		UserID:     "bob",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := st.Upsert(context.Background(), alice, vec); err != nil {
		t.Fatalf("Upsert alice: %v", err)
	}
	if err := st.Upsert(context.Background(), bob, vec); err != nil {
		t.Fatalf("Upsert bob: %v", err)
	}

	ts := newPropTestServer(t, st)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		ts.URL+"/v1/memories", http.NoBody)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("X-User-ID", "alice")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/memories: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var listResp struct {
		Memories []models.Memory `json:"memories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	for _, m := range listResp.Memories {
		if m.UserID != "alice" {
			t.Errorf("expected only alice's memories, got memory with UserID=%q (id=%s)", m.UserID, m.ID)
		}
	}

	found := false
	for _, m := range listResp.Memories {
		if m.ID == "list-alice-1" {
			found = true
		}
	}
	if !found {
		t.Error("alice's memory not found in filtered list")
	}
}

// TestHooks_UserID_PreTurn verifies that PreTurnHook.Execute uses UserID in search filter.
func TestHooks_UserID_PreTurn(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	st := store.NewMockStore()

	now := time.Now().UTC()
	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.5
	}

	hookMem := models.Memory{
		ID:         "hook-pre-mem-1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityPrivate,
		Content:    "hook user memory",
		Confidence: 0.9,
		UserID:     "hook-user",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	otherMem := models.Memory{
		ID:         "hook-pre-mem-2",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityPrivate,
		Content:    "other user memory",
		Confidence: 0.9,
		UserID:     "other-user",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := st.Upsert(ctx, hookMem, vec); err != nil {
		t.Fatalf("Upsert hookMem: %v", err)
	}
	if err := st.Upsert(ctx, otherMem, vec); err != nil {
		t.Fatalf("Upsert otherMem: %v", err)
	}

	emb := &apiTestEmbedder{}
	recaller := recall.NewRecaller(recall.DefaultWeights(), logger)
	hook := hooks.NewPreTurnHook(emb, st, recaller, logger)

	out, execErr := hook.Execute(ctx, hooks.PreTurnInput{
		Message:     "hook user memory",
		UserID:      "hook-user",
		TokenBudget: 2000,
	})
	if execErr != nil {
		t.Fatalf("Execute: %v", execErr)
	}

	// All returned memories must belong to hook-user.
	for _, r := range out.Memories {
		if r.Memory.UserID != "hook-user" {
			t.Errorf("pre-turn returned memory with UserID=%q, want hook-user", r.Memory.UserID)
		}
	}

	found := false
	for _, r := range out.Memories {
		if r.Memory.ID == "hook-pre-mem-1" {
			found = true
		}
	}
	if !found {
		t.Error("hook-user memory not found in pre-turn output")
	}
}

// propagationStubCapturer satisfies capture.Capturer for PostTurnHook tests.
type propagationStubCapturer struct{}

func (s *propagationStubCapturer) Extract(_ context.Context, _, _ string) ([]models.CapturedMemory, error) {
	return []models.CapturedMemory{
		{Content: "Paris is the capital of France", Confidence: 0.9, Type: models.MemoryTypeFact},
	}, nil
}

func (s *propagationStubCapturer) ExtractWithContext(ctx context.Context, user, assistant string, _ []capture.ConversationTurn) ([]models.CapturedMemory, error) {
	return s.Extract(ctx, user, assistant)
}

// propagationStubClassifier satisfies classifier.Classifier for PostTurnHook tests.
type propagationStubClassifier struct{}

func (s *propagationStubClassifier) Classify(_ string) models.MemoryType {
	return models.MemoryTypeFact
}

// TestHooks_UserID_PostTurn verifies that PostTurnHook.Execute sets UserID on stored memories.
func TestHooks_UserID_PostTurn(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	st := store.NewMockStore()
	emb := &apiTestEmbedder{}

	cap := &propagationStubCapturer{}
	cls := &propagationStubClassifier{}
	hook := hooks.NewPostTurnHook(cap, cls, emb, st, logger, 0.95)

	execErr := hook.Execute(ctx, hooks.PostTurnInput{
		UserMessage:      "what is the capital of France?",
		AssistantMessage: "Paris",
		UserID:           "post-user",
	})
	if execErr != nil {
		t.Fatalf("Execute: %v", execErr)
	}

	// Verify stored memories have UserID == "post-user".
	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.1
	}
	filters := &store.SearchFilters{UserID: "post-user"}
	results, searchErr := st.Search(ctx, vec, 10, filters)
	if searchErr != nil {
		t.Fatalf("Search: %v", searchErr)
	}
	if len(results) == 0 {
		t.Fatal("no memories stored for post-user")
	}
	for _, r := range results {
		if r.Memory.UserID != "post-user" {
			t.Errorf("stored memory UserID=%q, want post-user", r.Memory.UserID)
		}
	}
}
