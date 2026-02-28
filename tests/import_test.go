package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// importTestEmbedder is a minimal embedder that returns a deterministic fixed vector.
type importTestEmbedder struct{}

func (e *importTestEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	const dim = 768
	v := make([]float32, dim)
	for i := range v {
		v[i] = float32(i) * 0.001
	}
	return v, nil
}

func (e *importTestEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v, err := e.Embed(context.Background(), texts[i])
		if err != nil {
			return nil, fmt.Errorf("embed batch: %w", err)
		}
		out[i] = v
	}
	return out, nil
}

func (e *importTestEmbedder) Dimension() int { return 768 }

// Compile-time interface check.
var _ embedder.Embedder = (*importTestEmbedder)(nil)

// runImportJSON replicates the import command logic for JSON format.
// It parses a JSON array of memories, embeds each, and upserts to the store.
// Returns the count of imported and skipped memories.
func runImportJSON(ctx context.Context, data []byte, emb embedder.Embedder, st store.Store) (imported, skipped int, err error) {
	var memories []models.Memory
	if decErr := json.Unmarshal(data, &memories); decErr != nil {
		return 0, 0, fmt.Errorf("decoding JSON: %w", decErr)
	}

	now := time.Now().UTC()
	for i := range memories {
		m := &memories[i]
		if strings.TrimSpace(m.Content) == "" {
			skipped++
			continue
		}
		if m.CreatedAt.IsZero() {
			m.CreatedAt = now
		}
		if m.UpdatedAt.IsZero() {
			m.UpdatedAt = now
		}
		if m.LastAccessed.IsZero() {
			m.LastAccessed = now
		}
		vec, embedErr := emb.Embed(ctx, m.Content)
		if embedErr != nil {
			return imported, skipped, fmt.Errorf("embedding memory %q: %w", m.ID, embedErr)
		}
		if upsertErr := st.Upsert(ctx, *m, vec); upsertErr != nil {
			return imported, skipped, fmt.Errorf("upserting memory %q: %w", m.ID, upsertErr)
		}
		imported++
	}
	return imported, skipped, nil
}

// runImportJSONL replicates the import command logic for JSONL format.
func runImportJSONL(ctx context.Context, data []byte, emb embedder.Embedder, st store.Store) (imported, skipped int, err error) {
	now := time.Now().UTC()
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m models.Memory
		if unmarshalErr := json.Unmarshal([]byte(line), &m); unmarshalErr != nil {
			return imported, skipped, fmt.Errorf("decoding JSONL line: %w", unmarshalErr)
		}
		if strings.TrimSpace(m.Content) == "" {
			skipped++
			continue
		}
		if m.CreatedAt.IsZero() {
			m.CreatedAt = now
		}
		if m.UpdatedAt.IsZero() {
			m.UpdatedAt = now
		}
		if m.LastAccessed.IsZero() {
			m.LastAccessed = now
		}
		vec, embedErr := emb.Embed(ctx, m.Content)
		if embedErr != nil {
			return imported, skipped, fmt.Errorf("embedding memory %q: %w", m.ID, embedErr)
		}
		if upsertErr := st.Upsert(ctx, m, vec); upsertErr != nil {
			return imported, skipped, fmt.Errorf("upserting memory %q: %w", m.ID, upsertErr)
		}
		imported++
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return imported, skipped, fmt.Errorf("reading JSONL: %w", scanErr)
	}
	return imported, skipped, nil
}

// --- tests ---

func TestImport_JSON_ThreeMemories(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()
	emb := &importTestEmbedder{}

	now := time.Now().UTC().Truncate(time.Second)
	memories := []models.Memory{
		{
			ID:           "imp-1",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			Visibility:   models.VisibilityShared,
			Content:      "Go is statically typed",
			Confidence:   0.9,
			Source:       "explicit",
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		},
		{
			ID:           "imp-2",
			Type:         models.MemoryTypeRule,
			Scope:        models.ScopePermanent,
			Visibility:   models.VisibilityShared,
			Content:      "Always handle errors explicitly",
			Confidence:   0.95,
			Source:       "explicit",
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		},
		{
			ID:           "imp-3",
			Type:         models.MemoryTypeProcedure,
			Scope:        models.ScopeProject,
			Visibility:   models.VisibilityShared,
			Content:      "Run go test -race before committing",
			Confidence:   0.85,
			Source:       "explicit",
			Project:      "cortex",
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		},
	}

	// Write to a temp JSON file.
	data, err := json.MarshalIndent(memories, "", "  ")
	require.NoError(t, err)

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "memories.json")
	require.NoError(t, os.WriteFile(tmpFile, data, 0o600))

	// Read back and run the import logic.
	fileData, err := os.ReadFile(tmpFile)
	require.NoError(t, err)

	imported, skipped, err := runImportJSON(ctx, fileData, emb, st)
	require.NoError(t, err)
	assert.Equal(t, 3, imported, "all 3 memories should be imported")
	assert.Equal(t, 0, skipped, "no memories should be skipped")

	// Verify each memory was upserted.
	got1, err := st.Get(ctx, "imp-1")
	require.NoError(t, err)
	assert.Equal(t, "Go is statically typed", got1.Content)
	assert.Equal(t, models.MemoryTypeFact, got1.Type)

	got2, err := st.Get(ctx, "imp-2")
	require.NoError(t, err)
	assert.Equal(t, "Always handle errors explicitly", got2.Content)
	assert.Equal(t, models.MemoryTypeRule, got2.Type)

	got3, err := st.Get(ctx, "imp-3")
	require.NoError(t, err)
	assert.Equal(t, "Run go test -race before committing", got3.Content)
	assert.Equal(t, "cortex", got3.Project)
}

func TestImport_JSONL_ThreeMemories(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()
	emb := &importTestEmbedder{}

	now := time.Now().UTC().Truncate(time.Second)
	memories := []models.Memory{
		{
			ID:           "jl-1",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			Visibility:   models.VisibilityShared,
			Content:      "JSONL memory one",
			Confidence:   0.9,
			Source:       "explicit",
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		},
		{
			ID:           "jl-2",
			Type:         models.MemoryTypeEpisode,
			Scope:        models.ScopeSession,
			Visibility:   models.VisibilityShared,
			Content:      "JSONL memory two",
			Confidence:   0.75,
			Source:       "explicit",
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		},
		{
			ID:           "jl-3",
			Type:         models.MemoryTypePreference,
			Scope:        models.ScopePermanent,
			Visibility:   models.VisibilityShared,
			Content:      "JSONL memory three",
			Confidence:   0.8,
			Source:       "explicit",
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		},
	}

	// Build JSONL content.
	var sb strings.Builder
	for i := range memories {
		line, marshalErr := json.Marshal(memories[i])
		require.NoError(t, marshalErr)
		sb.Write(line)
		sb.WriteByte('\n')
	}

	imported, skipped, err := runImportJSONL(ctx, []byte(sb.String()), emb, st)
	require.NoError(t, err)
	assert.Equal(t, 3, imported, "all 3 JSONL memories should be imported")
	assert.Equal(t, 0, skipped, "no memories should be skipped")

	// Verify all three are in the store.
	for _, m := range memories {
		got, getErr := st.Get(ctx, m.ID)
		require.NoError(t, getErr, "memory %s should exist", m.ID)
		assert.Equal(t, m.Content, got.Content)
	}
}

func TestImport_SkipsEmptyContent(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()
	emb := &importTestEmbedder{}

	now := time.Now().UTC()
	memories := []models.Memory{
		{
			ID:         "skip-1",
			Type:       models.MemoryTypeFact,
			Scope:      models.ScopePermanent,
			Visibility: models.VisibilityShared,
			Content:    "",
			Confidence: 0.9,
			Source:     "explicit",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		{
			ID:         "skip-2",
			Type:       models.MemoryTypeFact,
			Scope:      models.ScopePermanent,
			Visibility: models.VisibilityShared,
			Content:    "  ",
			Confidence: 0.9,
			Source:     "explicit",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		{
			ID:         "keep-1",
			Type:       models.MemoryTypeFact,
			Scope:      models.ScopePermanent,
			Visibility: models.VisibilityShared,
			Content:    "This one has content",
			Confidence: 0.9,
			Source:     "explicit",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	data, err := json.Marshal(memories)
	require.NoError(t, err)

	imported, skipped, err := runImportJSON(ctx, data, emb, st)
	require.NoError(t, err)
	assert.Equal(t, 1, imported, "only the non-empty memory should be imported")
	assert.Equal(t, 2, skipped, "two empty memories should be skipped")

	_, err = st.Get(ctx, "keep-1")
	require.NoError(t, err, "non-empty memory should exist in store")

	_, err = st.Get(ctx, "skip-1")
	assert.Error(t, err, "empty-content memory should not be in store")
}

func TestImport_BackfillsZeroTimestamps(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()
	emb := &importTestEmbedder{}

	// Memory with zero timestamps — import should back-fill them.
	memories := []models.Memory{
		{
			ID:         "ts-1",
			Type:       models.MemoryTypeFact,
			Scope:      models.ScopePermanent,
			Visibility: models.VisibilityShared,
			Content:    "memory with no timestamps",
			Confidence: 0.9,
			Source:     "explicit",
			// CreatedAt, UpdatedAt, LastAccessed all zero
		},
	}

	data, err := json.Marshal(memories)
	require.NoError(t, err)

	before := time.Now().UTC()
	imported, skipped, err := runImportJSON(ctx, data, emb, st)
	after := time.Now().UTC()

	require.NoError(t, err)
	assert.Equal(t, 1, imported)
	assert.Equal(t, 0, skipped)

	got, err := st.Get(ctx, "ts-1")
	require.NoError(t, err)
	assert.False(t, got.CreatedAt.IsZero(), "CreatedAt should be back-filled")
	assert.True(t, !got.CreatedAt.Before(before) && !got.CreatedAt.After(after),
		"CreatedAt should be within the test time window")
}
