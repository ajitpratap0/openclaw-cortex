package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// seedMemory upserts a memory with a fixed vector into the mock store and
// returns the ID.
func seedMemory(t *testing.T, st *store.MockStore, mem models.Memory) string {
	t.Helper()
	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.1
	}
	require.NoError(t, st.Upsert(context.Background(), mem, vec))
	return mem.ID
}

// --- PUT /v1/memories/{id} ---

// TestAPI_UpdateMemory_Content verifies that the content of a memory can be
// updated via PUT and the change is persisted in the store.
func TestAPI_UpdateMemory_Content(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	id := seedMemory(t, st, models.Memory{
		ID:           "update-content-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "original content",
		Confidence:   0.8,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	})

	body := jsonBody(t, map[string]any{
		"content": "updated content",
	})
	resp := doRequest(t, http.MethodPut, ts.URL+"/v1/memories/"+id, body, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got models.Memory
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, id, got.ID)
	assert.Equal(t, "updated content", got.Content)

	// Verify the store was actually updated.
	stored, err := st.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "updated content", stored.Content)
}

// TestAPI_UpdateMemory_TypeAndScope verifies that type and scope can be patched.
func TestAPI_UpdateMemory_TypeAndScope(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	id := seedMemory(t, st, models.Memory{
		ID:           "update-typescope-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeSession,
		Visibility:   models.VisibilityPrivate,
		Content:      "some content",
		Confidence:   0.7,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	})

	body := jsonBody(t, map[string]any{
		"type":  "rule",
		"scope": "permanent",
	})
	resp := doRequest(t, http.MethodPut, ts.URL+"/v1/memories/"+id, body, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	stored, err := st.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, models.MemoryTypeRule, stored.Type)
	assert.Equal(t, models.ScopePermanent, stored.Scope)
}

// TestAPI_UpdateMemory_Tags verifies that tags can be replaced.
func TestAPI_UpdateMemory_Tags(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	id := seedMemory(t, st, models.Memory{
		ID:           "update-tags-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "tagged memory",
		Confidence:   0.9,
		Tags:         []string{"old-tag"},
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	})

	body := jsonBody(t, map[string]any{
		"tags": []string{"new-tag-a", "new-tag-b"},
	})
	resp := doRequest(t, http.MethodPut, ts.URL+"/v1/memories/"+id, body, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	stored, err := st.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, []string{"new-tag-a", "new-tag-b"}, stored.Tags)
}

// TestAPI_UpdateMemory_Confidence verifies that confidence can be changed.
func TestAPI_UpdateMemory_Confidence(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	id := seedMemory(t, st, models.Memory{
		ID:           "update-conf-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "confidence test",
		Confidence:   0.5,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	})

	body := jsonBody(t, map[string]any{
		"confidence": 0.95,
	})
	resp := doRequest(t, http.MethodPut, ts.URL+"/v1/memories/"+id, body, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	stored, err := st.Get(context.Background(), id)
	require.NoError(t, err)
	assert.InDelta(t, 0.95, stored.Confidence, 0.001)
}

// TestAPI_UpdateMemory_Project verifies that project can be changed.
func TestAPI_UpdateMemory_Project(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	id := seedMemory(t, st, models.Memory{
		ID:           "update-project-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeProject,
		Visibility:   models.VisibilityPrivate,
		Content:      "project memory",
		Confidence:   0.8,
		Project:      "old-project",
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	})

	body := jsonBody(t, map[string]any{
		"project": "new-project",
	})
	resp := doRequest(t, http.MethodPut, ts.URL+"/v1/memories/"+id, body, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	stored, err := st.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "new-project", stored.Project)
}

// TestAPI_UpdateMemory_NotFound verifies 404 for non-existent memory.
func TestAPI_UpdateMemory_NotFound(t *testing.T) {
	ts, _ := newTestServer(t, "")

	body := jsonBody(t, map[string]any{"content": "updated"})
	resp := doRequest(t, http.MethodPut, ts.URL+"/v1/memories/does-not-exist", body, "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestAPI_UpdateMemory_InvalidType verifies 400 for invalid memory type.
func TestAPI_UpdateMemory_InvalidType(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	id := seedMemory(t, st, models.Memory{
		ID:           "update-badtype-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "some content",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	})

	body := jsonBody(t, map[string]any{"type": "bogus-type"})
	resp := doRequest(t, http.MethodPut, ts.URL+"/v1/memories/"+id, body, "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestAPI_UpdateMemory_InvalidScope verifies 400 for invalid scope.
func TestAPI_UpdateMemory_InvalidScope(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	id := seedMemory(t, st, models.Memory{
		ID:           "update-badscope-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "some content",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	})

	body := jsonBody(t, map[string]any{"scope": "bad-scope"})
	resp := doRequest(t, http.MethodPut, ts.URL+"/v1/memories/"+id, body, "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestAPI_UpdateMemory_InvalidBody verifies 400 for malformed JSON.
func TestAPI_UpdateMemory_InvalidBody(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	id := seedMemory(t, st, models.Memory{
		ID:           "update-badjson-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "some content",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	})

	resp := doRequest(t, http.MethodPut, ts.URL+"/v1/memories/"+id,
		bytes.NewBufferString("not-json"), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// --- GET /v1/memories ---

// TestAPI_ListMemories verifies that all memories are returned when no filter.
func TestAPI_ListMemories(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	for i := range 3 {
		seedMemory(t, st, models.Memory{
			ID:           fmt.Sprintf("list-all-%03d", i),
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			Visibility:   models.VisibilityPrivate,
			Content:      fmt.Sprintf("list memory %d", i),
			Confidence:   0.9,
			Source:       "test",
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		})
	}

	resp := doRequest(t, http.MethodGet, ts.URL+"/v1/memories", nil, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	memories, ok := result["memories"].([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(memories), 3)
}

// TestAPI_ListMemories_TypeFilter verifies filtering by type returns only
// matching memories.
func TestAPI_ListMemories_TypeFilter(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	types := []models.MemoryType{models.MemoryTypeRule, models.MemoryTypeFact, models.MemoryTypeEpisode}
	for i, mt := range types {
		seedMemory(t, st, models.Memory{
			ID:           fmt.Sprintf("list-type-%03d", i),
			Type:         mt,
			Scope:        models.ScopePermanent,
			Visibility:   models.VisibilityPrivate,
			Content:      fmt.Sprintf("type filter memory %d", i),
			Confidence:   0.9,
			Source:       "test",
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		})
	}

	resp := doRequest(t, http.MethodGet, ts.URL+"/v1/memories?type=rule", nil, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	memories, ok := result["memories"].([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(memories), 1)

	// Verify all returned memories have type=rule.
	for _, m := range memories {
		mem, ok := m.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "rule", mem["type"])
	}
}

// TestAPI_ListMemories_ScopeFilter verifies filtering by scope.
func TestAPI_ListMemories_ScopeFilter(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	seedMemory(t, st, models.Memory{
		ID:           "list-scope-perm-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "permanent memory",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	})
	seedMemory(t, st, models.Memory{
		ID:           "list-scope-sess-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeSession,
		Visibility:   models.VisibilityPrivate,
		Content:      "session memory",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	})

	resp := doRequest(t, http.MethodGet, ts.URL+"/v1/memories?scope=session", nil, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	memories, ok := result["memories"].([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(memories), 1)

	for _, m := range memories {
		mem, ok := m.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "session", mem["scope"])
	}
}

// TestAPI_ListMemories_ProjectFilter verifies filtering by project.
func TestAPI_ListMemories_ProjectFilter(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	seedMemory(t, st, models.Memory{
		ID:           "list-proj-alpha-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeProject,
		Visibility:   models.VisibilityPrivate,
		Content:      "alpha project memory",
		Confidence:   0.9,
		Project:      "alpha",
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	})
	seedMemory(t, st, models.Memory{
		ID:           "list-proj-beta-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeProject,
		Visibility:   models.VisibilityPrivate,
		Content:      "beta project memory",
		Confidence:   0.9,
		Project:      "beta",
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	})

	resp := doRequest(t, http.MethodGet, ts.URL+"/v1/memories?project=alpha", nil, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	memories, ok := result["memories"].([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(memories), 1)

	for _, m := range memories {
		mem, ok := m.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "alpha", mem["project"])
	}
}

// TestAPI_ListMemories_Limit verifies the limit query param.
func TestAPI_ListMemories_Limit(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	for i := range 5 {
		seedMemory(t, st, models.Memory{
			ID:           fmt.Sprintf("list-limit-%03d", i),
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			Visibility:   models.VisibilityPrivate,
			Content:      fmt.Sprintf("limit test memory %d", i),
			Confidence:   0.9,
			Source:       "test",
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		})
	}

	resp := doRequest(t, http.MethodGet, ts.URL+"/v1/memories?limit=2", nil, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	memories, ok := result["memories"].([]any)
	require.True(t, ok)
	assert.LessOrEqual(t, len(memories), 2)

	// If there are more, next_cursor should be set.
	nextCursor, _ := result["next_cursor"].(string)
	assert.NotEmpty(t, nextCursor)
}

// TestAPI_ListMemories_EmptyResult verifies that an empty result is a JSON
// array, not null.
func TestAPI_ListMemories_EmptyResult(t *testing.T) {
	ts, _ := newTestServer(t, "")

	resp := doRequest(t, http.MethodGet, ts.URL+"/v1/memories?type=rule", nil, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	memories, ok := result["memories"].([]any)
	require.True(t, ok)
	assert.Empty(t, memories)
}

// --- POST /v1/search with type/scope/tags filters ---

// TestAPI_Search_TypeFilter verifies that POST /v1/search respects the type
// filter field.
func TestAPI_Search_TypeFilter(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	// Store one rule and one fact.
	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.1
	}
	require.NoError(t, st.Upsert(context.Background(), models.Memory{
		ID:           "search-type-rule-001",
		Type:         models.MemoryTypeRule,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "always use context propagation",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}, vec))
	require.NoError(t, st.Upsert(context.Background(), models.Memory{
		ID:           "search-type-fact-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "Go uses goroutines for concurrency",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}, vec))

	body := jsonBody(t, map[string]any{
		"message": "context",
		"limit":   10,
		"type":    "rule",
	})
	resp := doRequest(t, http.MethodPost, ts.URL+"/v1/search", body, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	results, ok := result["results"].([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(results), 1)

	for _, r := range results {
		entry, ok := r.(map[string]any)
		require.True(t, ok)
		mem, ok := entry["memory"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "rule", mem["type"])
	}
}

// TestAPI_Search_ScopeFilter verifies that POST /v1/search respects the scope
// filter field.
func TestAPI_Search_ScopeFilter(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.1
	}
	require.NoError(t, st.Upsert(context.Background(), models.Memory{
		ID:           "search-scope-perm-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "permanent knowledge",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}, vec))
	require.NoError(t, st.Upsert(context.Background(), models.Memory{
		ID:           "search-scope-sess-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeSession,
		Visibility:   models.VisibilityPrivate,
		Content:      "session knowledge",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}, vec))

	body := jsonBody(t, map[string]any{
		"message": "knowledge",
		"limit":   10,
		"scope":   "permanent",
	})
	resp := doRequest(t, http.MethodPost, ts.URL+"/v1/search", body, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	results, ok := result["results"].([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(results), 1)

	for _, r := range results {
		entry, ok := r.(map[string]any)
		require.True(t, ok)
		mem, ok := entry["memory"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "permanent", mem["scope"])
	}
}

// TestAPI_Search_TagsFilter verifies that POST /v1/search filters by tags.
func TestAPI_Search_TagsFilter(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.1
	}
	require.NoError(t, st.Upsert(context.Background(), models.Memory{
		ID:           "search-tags-golang-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "golang memory",
		Tags:         []string{"golang", "programming"},
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}, vec))
	require.NoError(t, st.Upsert(context.Background(), models.Memory{
		ID:           "search-tags-rust-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "rust memory",
		Tags:         []string{"rust", "systems"},
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}, vec))

	body := jsonBody(t, map[string]any{
		"message": "programming",
		"limit":   10,
		"tags":    []string{"golang"},
	})
	resp := doRequest(t, http.MethodPost, ts.URL+"/v1/search", body, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	results, ok := result["results"].([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(results), 1)

	for _, r := range results {
		entry, ok := r.(map[string]any)
		require.True(t, ok)
		mem, ok := entry["memory"].(map[string]any)
		require.True(t, ok)
		rawTags, ok := mem["tags"].([]any)
		require.True(t, ok)
		tagStrs := make([]string, len(rawTags))
		for i, tag := range rawTags {
			tagStrs[i], _ = tag.(string)
		}
		assert.Contains(t, tagStrs, "golang")
	}
}

// TestAPI_Search_ProjectFilter verifies that POST /v1/search filters by project.
func TestAPI_Search_ProjectFilter(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.1
	}
	require.NoError(t, st.Upsert(context.Background(), models.Memory{
		ID:           "search-proj-alpha-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeProject,
		Visibility:   models.VisibilityPrivate,
		Content:      "alpha project fact",
		Project:      "alpha",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}, vec))
	require.NoError(t, st.Upsert(context.Background(), models.Memory{
		ID:           "search-proj-beta-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeProject,
		Visibility:   models.VisibilityPrivate,
		Content:      "beta project fact",
		Project:      "beta",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}, vec))

	body := jsonBody(t, map[string]any{
		"message": "project",
		"limit":   10,
		"project": "alpha",
	})
	resp := doRequest(t, http.MethodPost, ts.URL+"/v1/search", body, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	results, ok := result["results"].([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(results), 1)

	for _, r := range results {
		entry, ok := r.(map[string]any)
		require.True(t, ok)
		mem, ok := entry["memory"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "alpha", mem["project"])
	}
}

// --- CLI update command ---

// TestCLI_Update_ContentFlag verifies that the update command stores new
// content when --content is provided (using MockStore directly).
func TestCLI_Update_ContentFlag(t *testing.T) {
	st := store.NewMockStore()

	now := time.Now().UTC()
	require.NoError(t, st.Upsert(context.Background(), models.Memory{
		ID:           "cli-update-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "before update",
		Confidence:   0.8,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}, make([]float32, 768)))

	// Simulate what the CLI update command does: Get → patch → Upsert.
	mem, err := st.Get(context.Background(), "cli-update-001")
	require.NoError(t, err)

	mem.Content = "after update"
	mem.UpdatedAt = time.Now().UTC()

	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.2 // simulated new embedding
	}
	require.NoError(t, st.Upsert(context.Background(), *mem, vec))

	stored, err := st.Get(context.Background(), "cli-update-001")
	require.NoError(t, err)
	assert.Equal(t, "after update", stored.Content)
}

// TestCLI_Update_TypeFlag verifies type patching without content change.
func TestCLI_Update_TypeFlag(t *testing.T) {
	st := store.NewMockStore()

	now := time.Now().UTC()
	require.NoError(t, st.Upsert(context.Background(), models.Memory{
		ID:           "cli-update-type-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "type update test",
		Confidence:   0.8,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}, make([]float32, 768)))

	mem, err := st.Get(context.Background(), "cli-update-type-001")
	require.NoError(t, err)

	mem.Type = models.MemoryTypeRule
	mem.UpdatedAt = time.Now().UTC()
	require.NoError(t, st.Upsert(context.Background(), *mem, make([]float32, 768)))

	stored, err := st.Get(context.Background(), "cli-update-type-001")
	require.NoError(t, err)
	assert.Equal(t, models.MemoryTypeRule, stored.Type)
}

// TestCLI_Update_NotFound verifies that Get returns an error for missing IDs.
func TestCLI_Update_NotFound(t *testing.T) {
	st := store.NewMockStore()

	_, err := st.Get(context.Background(), "nonexistent-id")
	assert.Error(t, err)
}
