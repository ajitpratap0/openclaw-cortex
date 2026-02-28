package tests

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cortexmcp "github.com/ajitpratap0/openclaw-cortex/internal/mcp"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// mcpMockEmbedder returns deterministic fixed-dimension vectors for MCP tests.
// Each text gets a unique vector based on its first byte so that
// equal texts produce identical (hence maximally-similar) vectors.
type mcpMockEmbedder struct{}

func (m *mcpMockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	return mcpTestVector(text), nil
}

func (m *mcpMockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, t := range texts {
		vecs[i] = mcpTestVector(t)
	}
	return vecs, nil
}

func (m *mcpMockEmbedder) Dimension() int { return 768 }

func mcpTestVector(text string) []float32 {
	v := make([]float32, 768)
	seed := float32(1.0)
	if len(text) > 0 {
		seed = float32(text[0])
	}
	for i := range v {
		v[i] = seed * float32(i+1) / float32(768)
	}
	return v
}

// newMCPServer returns a Server backed by a MockStore and mock embedder.
func newMCPServer(t *testing.T) (*cortexmcp.Server, *store.MockStore) {
	t.Helper()
	ms := store.NewMockStore()
	emb := &mcpMockEmbedder{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := cortexmcp.NewServer(ms, emb, logger)
	return srv, ms
}

// makeReq builds a CallToolRequest with the given string/number/bool arguments.
func makeReq(toolName string, args map[string]any) mcpgo.CallToolRequest {
	req := mcpgo.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = args
	return req
}

// textContent extracts the first TextContent string from a CallToolResult.
func textContent(t *testing.T, result *mcpgo.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, result.Content, "expected at least one content item")
	tc, ok := result.Content[0].(mcpgo.TextContent)
	require.True(t, ok, "expected TextContent, got %T", result.Content[0])
	return tc.Text
}

// rememberAndGetID calls the remember handler and returns the stored memory ID.
func rememberAndGetID(t *testing.T, srv *cortexmcp.Server, args map[string]any) string {
	t.Helper()
	ctx := context.Background()
	result, err := srv.HandleRemember(ctx, makeReq("remember", args))
	require.NoError(t, err)
	require.False(t, result.IsError, "remember returned error: %s", textContent(t, result))

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &out))
	id, ok := out["id"].(string)
	require.True(t, ok)
	require.NotEmpty(t, id)
	return id
}

// --- remember tests ---

func TestMCPRemember_StoresMemory(t *testing.T) {
	srv, ms := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleRemember(ctx, makeReq("remember", map[string]any{
		"content": "Always use context.Context as the first parameter",
		"type":    "rule",
		"scope":   "permanent",
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError, "expected no error")

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &out))
	assert.Equal(t, true, out["stored"])
	id, ok := out["id"].(string)
	require.True(t, ok, "id should be a string")
	require.NotEmpty(t, id)

	// Verify the memory is in the store.
	mem, getErr := ms.Get(context.Background(), id)
	require.NoError(t, getErr)
	assert.Equal(t, "Always use context.Context as the first parameter", mem.Content)
	assert.Equal(t, models.MemoryTypeRule, mem.Type)
	assert.Equal(t, models.ScopePermanent, mem.Scope)
}

func TestMCPRemember_WithProjectAndConfidence(t *testing.T) {
	srv, ms := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleRemember(ctx, makeReq("remember", map[string]any{
		"content":    "Use postgres for this project",
		"type":       "fact",
		"scope":      "project",
		"project":    "myproject",
		"confidence": 0.9,
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &out))
	id, ok := out["id"].(string)
	require.True(t, ok, "id should be a string")

	mem, getErr := ms.Get(context.Background(), id)
	require.NoError(t, getErr)
	assert.Equal(t, "myproject", mem.Project)
	assert.Equal(t, models.ScopeProject, mem.Scope)
	assert.InDelta(t, 0.9, mem.Confidence, 0.001)
}

func TestMCPRemember_EmptyContentReturnsError(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleRemember(ctx, makeReq("remember", map[string]any{
		"content": "   ",
	}))
	require.NoError(t, err)
	assert.True(t, result.IsError, "expected an error for empty content")
}

func TestMCPRemember_InvalidTypeReturnsError(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleRemember(ctx, makeReq("remember", map[string]any{
		"content": "some content",
		"type":    "invalid-type",
	}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestMCPRemember_InvalidScopeReturnsError(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleRemember(ctx, makeReq("remember", map[string]any{
		"content": "some content",
		"scope":   "nope",
	}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestMCPRemember_InvalidConfidenceReturnsError(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleRemember(ctx, makeReq("remember", map[string]any{
		"content":    "some content",
		"confidence": 1.5,
	}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

// --- recall tests ---

func TestMCPRecall_ReturnsStoredMemories(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	rememberAndGetID(t, srv, map[string]any{
		"content": "Go tests use the testing package",
		"type":    "fact",
	})
	rememberAndGetID(t, srv, map[string]any{
		"content": "Always wrap errors with fmt.Errorf",
		"type":    "rule",
	})

	result, err := srv.HandleRecall(ctx, makeReq("recall", map[string]any{
		"message": "How do I write tests in Go?",
		"budget":  500,
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &out))
	memCount, ok := out["memory_count"].(float64)
	require.True(t, ok, "memory_count should be a float64")
	assert.GreaterOrEqual(t, int(memCount), 1)
}

func TestMCPRecall_EmptyMessageReturnsError(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleRecall(ctx, makeReq("recall", map[string]any{
		"message": "",
	}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestMCPRecall_EmptyStoreReturnsZeroCount(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleRecall(ctx, makeReq("recall", map[string]any{
		"message": "anything",
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &out))
	memCount, ok := out["memory_count"].(float64)
	require.True(t, ok, "memory_count should be a float64")
	assert.Equal(t, float64(0), memCount)
}

func TestMCPRecall_WithProjectContext(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	rememberAndGetID(t, srv, map[string]any{
		"content": "project cortex uses gRPC",
		"scope":   "project",
		"project": "cortex",
	})

	result, err := srv.HandleRecall(ctx, makeReq("recall", map[string]any{
		"message": "protocol",
		"project": "cortex",
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &out))
	memCount, ok := out["memory_count"].(float64)
	require.True(t, ok, "memory_count should be a float64")
	assert.GreaterOrEqual(t, memCount, float64(1))
}

// --- forget tests ---

func TestMCPForget_DeletesMemory(t *testing.T) {
	srv, ms := newMCPServer(t)
	ctx := context.Background()

	id := rememberAndGetID(t, srv, map[string]any{
		"content": "temporary fact",
	})

	result, err := srv.HandleForget(ctx, makeReq("forget", map[string]any{
		"id": id,
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &out))
	assert.Equal(t, true, out["deleted"])

	// Confirm it is gone from the store.
	_, getErr := ms.Get(context.Background(), id)
	assert.ErrorIs(t, getErr, store.ErrNotFound)
}

func TestMCPForget_NonexistentIDReturnsError(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleForget(ctx, makeReq("forget", map[string]any{
		"id": "does-not-exist",
	}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestMCPForget_EmptyIDReturnsError(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleForget(ctx, makeReq("forget", map[string]any{
		"id": "",
	}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

// --- search tests ---

func TestMCPSearch_ReturnsResults(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	rememberAndGetID(t, srv, map[string]any{
		"content": "Qdrant is a vector database",
	})

	result, err := srv.HandleSearch(ctx, makeReq("search", map[string]any{
		"message": "vector database",
		"limit":   5,
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &out))
	results, ok := out["results"].([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(results), 1)
}

func TestMCPSearch_EmptyMessageReturnsError(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleSearch(ctx, makeReq("search", map[string]any{
		"message": "",
	}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestMCPSearch_WithProjectFilter(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	rememberAndGetID(t, srv, map[string]any{
		"content": "project alpha uses Go",
		"scope":   "project",
		"project": "alpha",
	})
	rememberAndGetID(t, srv, map[string]any{
		"content": "project beta uses Python",
		"scope":   "project",
		"project": "beta",
	})

	result, err := srv.HandleSearch(ctx, makeReq("search", map[string]any{
		"message": "programming language",
		"project": "alpha",
		"limit":   10,
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &out))
	results, ok := out["results"].([]any)
	require.True(t, ok, "results should be a []any")
	// Only alpha-project memories should be returned.
	for _, r := range results {
		entry, ok := r.(map[string]any)
		require.True(t, ok, "result entry should be a map[string]any")
		mem, ok := entry["memory"].(map[string]any)
		require.True(t, ok, "memory field should be a map[string]any")
		assert.Equal(t, "alpha", mem["project"])
	}
}

func TestMCPSearch_EmptyStoreReturnsEmptyResults(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleSearch(ctx, makeReq("search", map[string]any{
		"message": "anything",
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &out))
	// results key should be present and nil/empty
	if out["results"] != nil {
		results, ok := out["results"].([]any)
		require.True(t, ok, "results should be a []any")
		assert.Empty(t, results)
	}
}

// --- stats tests ---

func TestMCPStats_ReturnsCollectionStats(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	rememberAndGetID(t, srv, map[string]any{
		"content": "first fact",
		"type":    "fact",
	})
	rememberAndGetID(t, srv, map[string]any{
		"content": "important rule",
		"type":    "rule",
	})

	result, err := srv.HandleStats(ctx, makeReq("stats", map[string]any{}))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var stats models.CollectionStats
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &stats))
	assert.Equal(t, int64(2), stats.TotalMemories)
	assert.Equal(t, int64(1), stats.ByType["fact"])
	assert.Equal(t, int64(1), stats.ByType["rule"])
}

func TestMCPStats_EmptyStore(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleStats(ctx, makeReq("stats", map[string]any{}))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var stats models.CollectionStats
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &stats))
	assert.Equal(t, int64(0), stats.TotalMemories)
}
