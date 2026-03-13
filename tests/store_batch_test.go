package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// --- Mock embedder for batch store tests ---

type batchMockEmbedder struct{}

func (e *batchMockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	return batchTestVector(text), nil
}

func (e *batchMockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, t := range texts {
		vecs[i] = batchTestVector(t)
	}
	return vecs, nil
}

func (e *batchMockEmbedder) Dimension() int { return 768 }

// batchTestVector creates a deterministic vector from the text content.
// Identical texts produce identical vectors (cosine similarity = 1.0).
// Different texts produce orthogonal-ish vectors (low cosine similarity).
func batchTestVector(text string) []float32 {
	const dim = 768
	v := make([]float32, dim)
	// Use a simple hash of the full text to seed a unique direction.
	var h uint32
	for _, c := range text {
		h = h*31 + uint32(c)
	}
	// Place energy in a few dimensions determined by the hash.
	for i := 0; i < dim; i++ {
		// Mix the hash with the dimension index for variation.
		mixed := h ^ uint32(i*2654435761)
		// Convert to a float in [-1, 1].
		v[i] = float32(int32(mixed)) / float32(1<<31)
	}
	return v
}

// batchStoreInput mirrors the CLI's JSON input schema.
type batchStoreInput struct {
	Content    string   `json:"content"`
	Type       string   `json:"type"`
	Scope      string   `json:"scope"`
	Tags       []string `json:"tags"`
	Confidence float64  `json:"confidence"`
}

// batchStoreResult mirrors the CLI's JSON output schema.
type batchStoreResult struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	Content string `json:"content,omitempty"`
}

// executeBatch simulates the core logic of the store-batch command:
// validate, embed batch, dedup check, upsert. Returns results array.
func executeBatch(t *testing.T, st *store.MockStore, emb *batchMockEmbedder, inputs []batchStoreInput, project string, dedupThreshold float64) []batchStoreResult {
	t.Helper()
	ctx := context.Background()

	if len(inputs) == 0 {
		return []batchStoreResult{}
	}

	// Validate
	for i := range inputs {
		inp := &inputs[i]
		if inp.Type == "" {
			inp.Type = "fact"
		}
		if inp.Scope == "" {
			inp.Scope = "permanent"
		}
		if inp.Confidence == 0 {
			inp.Confidence = 0.9
		}
		mt := models.MemoryType(inp.Type)
		require.True(t, mt.IsValid(), "invalid type %q at index %d", inp.Type, i)
		ms := models.MemoryScope(inp.Scope)
		require.True(t, ms.IsValid(), "invalid scope %q at index %d", inp.Scope, i)
	}

	// Batch embed
	contents := make([]string, len(inputs))
	for i := range inputs {
		contents[i] = inputs[i].Content
	}
	vectors, err := emb.EmbedBatch(ctx, contents)
	require.NoError(t, err)
	require.Len(t, vectors, len(inputs))

	// Process each
	results := make([]batchStoreResult, len(inputs))
	now := time.Now().UTC()

	for i := range inputs {
		inp := &inputs[i]
		vec := vectors[i]

		dupes, dupErr := st.FindDuplicates(ctx, vec, dedupThreshold)
		if dupErr == nil && len(dupes) > 0 {
			results[i] = batchStoreResult{
				ID:     dupes[0].Memory.ID,
				Status: "duplicate",
			}
			continue
		}

		mem := models.Memory{
			ID:           uuid.New().String(),
			Type:         models.MemoryType(inp.Type),
			Scope:        models.MemoryScope(inp.Scope),
			Visibility:   models.VisibilityShared,
			Content:      inp.Content,
			Confidence:   inp.Confidence,
			Source:       "explicit",
			Tags:         inp.Tags,
			Project:      project,
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		}

		upsertErr := st.Upsert(ctx, mem, vec)
		require.NoError(t, upsertErr)

		results[i] = batchStoreResult{
			ID:     mem.ID,
			Status: "created",
		}
	}

	return results
}

func TestBatchStore_AllCreated(t *testing.T) {
	st := store.NewMockStore()
	emb := &batchMockEmbedder{}

	inputs := []batchStoreInput{
		{Content: "Go uses goroutines for concurrency", Type: "fact", Scope: "permanent", Tags: []string{"go"}},
		{Content: "Always run tests before merging", Type: "rule", Scope: "permanent", Tags: []string{"ci"}},
		{Content: "Deploy using kubectl apply", Type: "procedure", Scope: "project", Tags: []string{"k8s"}},
	}

	results := executeBatch(t, st, emb, inputs, "test-project", 0.95)

	require.Len(t, results, 3)
	for i, r := range results {
		assert.Equal(t, "created", r.Status, "entry %d should be created", i)
		assert.NotEmpty(t, r.ID, "entry %d should have an ID", i)
	}

	// Verify all three are in the store.
	stats, err := st.Stats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(3), stats.TotalMemories)
}

func TestBatchStore_WithDuplicate(t *testing.T) {
	st := store.NewMockStore()
	emb := &batchMockEmbedder{}

	// Pre-store a memory with the same content vector.
	existingMem := models.Memory{
		ID:           "existing-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityShared,
		Content:      "Go uses goroutines for concurrency",
		Confidence:   0.9,
		Source:       "explicit",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		LastAccessed: time.Now().UTC(),
	}
	vec := batchTestVector(existingMem.Content)
	require.NoError(t, st.Upsert(context.Background(), existingMem, vec))

	inputs := []batchStoreInput{
		{Content: "Go uses goroutines for concurrency", Type: "fact"}, // duplicate
		{Content: "Always run tests before merging", Type: "rule"},    // new
		{Content: "Go uses goroutines for concurrency", Type: "fact"}, // also duplicate
	}

	results := executeBatch(t, st, emb, inputs, "", 0.95)

	require.Len(t, results, 3)
	assert.Equal(t, "duplicate", results[0].Status)
	assert.Equal(t, "existing-001", results[0].ID)
	assert.Equal(t, "created", results[1].Status)
	assert.Equal(t, "duplicate", results[2].Status)
}

func TestBatchStore_InvalidType(t *testing.T) {
	mt := models.MemoryType("invalid_type")
	assert.False(t, mt.IsValid(), "invalid_type should not be valid")

	// Verify all valid types are accepted.
	for _, vt := range models.ValidMemoryTypes {
		assert.True(t, vt.IsValid(), "type %q should be valid", vt)
	}
}

func TestBatchStore_EmptyBatch(t *testing.T) {
	st := store.NewMockStore()
	emb := &batchMockEmbedder{}

	results := executeBatch(t, st, emb, []batchStoreInput{}, "", 0.95)
	assert.Empty(t, results)
}

func TestBatchStore_JSONRoundTrip(t *testing.T) {
	// Test that the input/output JSON schemas marshal/unmarshal correctly.
	inputs := []batchStoreInput{
		{Content: "memory one", Type: "fact", Scope: "permanent", Tags: []string{"a", "b"}, Confidence: 0.85},
		{Content: "memory two", Type: "rule", Scope: "project", Tags: nil, Confidence: 0.9},
	}

	data, err := json.Marshal(inputs)
	require.NoError(t, err)

	var decoded []batchStoreInput
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Len(t, decoded, 2)
	assert.Equal(t, "memory one", decoded[0].Content)
	assert.Equal(t, "fact", decoded[0].Type)
	assert.Equal(t, []string{"a", "b"}, decoded[0].Tags)
	assert.Equal(t, 0.85, decoded[0].Confidence)
	assert.Equal(t, "memory two", decoded[1].Content)
	assert.Equal(t, "rule", decoded[1].Type)

	// Test output round-trip.
	outputs := []batchStoreResult{
		{ID: "abc-123", Status: "created", Content: "memory one"},
		{ID: "existing-1", Status: "duplicate"},
		{ID: "", Status: "error", Error: "some error"},
	}

	outData, err := json.Marshal(outputs)
	require.NoError(t, err)

	var decodedOut []batchStoreResult
	require.NoError(t, json.Unmarshal(outData, &decodedOut))
	require.Len(t, decodedOut, 3)
	assert.Equal(t, "created", decodedOut[0].Status)
	assert.Equal(t, "abc-123", decodedOut[0].ID)
	assert.Equal(t, "duplicate", decodedOut[1].Status)
	assert.Equal(t, "error", decodedOut[2].Status)
	assert.Equal(t, "some error", decodedOut[2].Error)
}

func TestBatchStore_DefaultValues(t *testing.T) {
	st := store.NewMockStore()
	emb := &batchMockEmbedder{}

	// Provide only content — defaults should be applied.
	inputs := []batchStoreInput{
		{Content: "minimal memory"},
	}

	results := executeBatch(t, st, emb, inputs, "my-project", 0.95)

	require.Len(t, results, 1)
	assert.Equal(t, "created", results[0].Status)

	// Verify the stored memory has defaults.
	mem, err := st.Get(context.Background(), results[0].ID)
	require.NoError(t, err)
	assert.Equal(t, models.MemoryTypeFact, mem.Type)
	assert.Equal(t, models.ScopePermanent, mem.Scope)
	assert.Equal(t, 0.9, mem.Confidence)
	assert.Equal(t, "my-project", mem.Project)
}
