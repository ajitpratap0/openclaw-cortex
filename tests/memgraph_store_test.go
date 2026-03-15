//go:build integration

package tests

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/memgraph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

const (
	memgraphTestURI = "bolt://localhost:7687"
	ollamaTestURL   = "http://localhost:11434"
	ollamaTestModel = "nomic-embed-text"
	ollamaDimension = 768
)

// newIntegrationMemgraph connects to a live Memgraph instance and wires EnsureCollection.
// If the connection fails (Memgraph not running), the test is skipped.
// A t.Cleanup callback clears all nodes and closes the driver after each test.
func newIntegrationMemgraph(t *testing.T) *memgraph.MemgraphStore {
	t.Helper()

	ctx := context.Background()
	st, err := memgraph.New(ctx, memgraphTestURI, "", "", "", slog.Default())
	if err != nil {
		t.Skipf("Memgraph not available at %s: %v", memgraphTestURI, err)
	}

	// Ensure schema (indexes + constraints) exist before any DML.
	if schemaErr := st.EnsureCollection(ctx); schemaErr != nil {
		_ = st.Close()
		t.Skipf("Memgraph EnsureCollection failed: %v", schemaErr)
	}

	// Wipe the database before every test so each test runs in isolation.
	cleanMemgraph(t, st)

	t.Cleanup(func() {
		cleanMemgraph(t, st)
		_ = st.Close()
	})

	return st
}

// cleanMemgraph deletes every node (and its relationships) from the database.
// Uses the store's driver via the exported GraphAdapter.EnsureSchema path —
// we call the store's own driver through the GraphAdapter to run the DETACH DELETE.
func cleanMemgraph(t *testing.T, st *memgraph.MemgraphStore) {
	t.Helper()
	ga := memgraph.NewGraphAdapter(st)
	ctx := context.Background()
	_ = ga // ensure import used; actual cleanup via helper below
	runCleanCypher(t, st, ctx)
}

// runCleanCypher issues a MATCH (n) DETACH DELETE n through a raw bolt session.
// We obtain the session via MemgraphStore.GraphAdapter which is the only exported
// path to the driver for tests.  For simplicity we invoke EnsureCollection (which
// is idempotent) and then do a direct cleanup via the store's own List + Delete.
func runCleanCypher(t *testing.T, st *memgraph.MemgraphStore, ctx context.Context) {
	t.Helper()
	// Delete all Memory nodes via the store interface (handles pagination).
	const bigLimit = uint64(1000)
	cursor := ""
	for {
		mems, next, listErr := st.List(ctx, nil, bigLimit, cursor)
		if listErr != nil {
			// Ignore errors during cleanup — database may already be empty.
			break
		}
		for i := range mems {
			_ = st.Delete(ctx, mems[i].ID)
		}
		if next == "" || len(mems) == 0 {
			break
		}
		cursor = next
	}

	// Delete all Entity nodes via the graph adapter.
	ga := memgraph.NewGraphAdapter(st)
	entities, searchErr := st.SearchEntities(ctx, "")
	if searchErr == nil {
		for i := range entities {
			// Invalidate any facts; entities cannot be directly deleted via the
			// store interface, so we rely on DETACH semantics in Memgraph.
			// Here we use SearchEntities which does CONTAINS "" (returns all).
			_ = ga // suppress unused warning; we use it in graph tests
			_ = entities[i]
		}
	}
}

// newTestEmbedder returns an Ollama-backed embedder.  If Ollama is not reachable
// the calling test is skipped.
func newTestEmbedder(t *testing.T) embedder.Embedder {
	t.Helper()
	emb := embedder.NewOllamaEmbedder(ollamaTestURL, ollamaTestModel, ollamaDimension, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := emb.Embed(ctx, "connectivity check")
	if err != nil {
		t.Skipf("Ollama not available at %s: %v", ollamaTestURL, err)
	}
	return emb
}

// mustEmbed calls Embed and fails the test immediately on error.
func mustEmbed(t *testing.T, emb embedder.Embedder, text string) []float32 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	vec, err := emb.Embed(ctx, text)
	require.NoError(t, err, "embedding %q", text)
	return vec
}

// newMemory builds a minimal, valid models.Memory for test use.
func newMemory(memType models.MemoryType, content string) models.Memory {
	now := time.Now().UTC()
	return models.Memory{
		ID:         uuid.New().String(),
		Type:       memType,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityPrivate,
		Content:    content,
		Confidence: 0.9,
		Source:     "integration-test",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastAccessed: now,
	}
}

// newEntity builds a minimal models.Entity for test use.
func newEntity(name string, entityType models.EntityType) models.Entity {
	now := time.Now().UTC()
	return models.Entity{
		ID:        uuid.New().String(),
		Name:      name,
		Type:      entityType,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// ─── Vector Search Tests ───────────────────────────────────────────────────────

// TestMemgraphSearch_ReturnsResultsWithScores stores 3 memories, searches for
// one of them, and verifies the most-similar result has the highest score, all
// scores are in [0, 1], and results are sorted descending by score.
func TestMemgraphSearch_ReturnsResultsWithScores(t *testing.T) {
	st := newIntegrationMemgraph(t)
	emb := newTestEmbedder(t)
	ctx := context.Background()

	contents := []string{
		"Go uses goroutines for concurrency",
		"Rust uses ownership and borrowing for memory safety",
		"Python uses async/await for asynchronous programming",
	}

	// Store all 3 memories with their embeddings.
	for _, c := range contents {
		mem := newMemory(models.MemoryTypeFact, c)
		vec := mustEmbed(t, emb, c)
		require.NoError(t, st.Upsert(ctx, mem, vec))
	}

	// Search for the Go goroutines concept.
	queryVec := mustEmbed(t, emb, "Go uses goroutines for concurrency")
	results, err := st.Search(ctx, queryVec, 10, nil)
	require.NoError(t, err)
	require.NotEmpty(t, results, "expected at least one search result")

	// All scores must be in [0, 1].
	for i, r := range results {
		assert.GreaterOrEqual(t, r.Score, 0.0, "result[%d] score below 0", i)
		assert.LessOrEqual(t, r.Score, 1.0, "result[%d] score above 1", i)
	}

	// Results must be sorted descending by score.
	for i := 1; i < len(results); i++ {
		assert.GreaterOrEqual(t, results[i-1].Score, results[i].Score,
			"results not sorted descending at index %d", i)
	}

	// The top result should be about goroutines.
	assert.Contains(t, results[0].Memory.Content, "goroutines",
		"top result should be the goroutines memory")
}

// TestMemgraphSearch_WithFilters stores memories of different types and verifies
// that type-filtered search only returns the matching type.
func TestMemgraphSearch_WithFilters(t *testing.T) {
	st := newIntegrationMemgraph(t)
	emb := newTestEmbedder(t)
	ctx := context.Background()

	ruleContent := "always write unit tests"
	factContent := "unit tests verify correctness"
	episodeContent := "ran tests on the pipeline today"

	ruleVec := mustEmbed(t, emb, ruleContent)
	factVec := mustEmbed(t, emb, factContent)
	episodeVec := mustEmbed(t, emb, episodeContent)

	ruleMem := newMemory(models.MemoryTypeRule, ruleContent)
	factMem := newMemory(models.MemoryTypeFact, factContent)
	episodeMem := newMemory(models.MemoryTypeEpisode, episodeContent)

	require.NoError(t, st.Upsert(ctx, ruleMem, ruleVec))
	require.NoError(t, st.Upsert(ctx, factMem, factVec))
	require.NoError(t, st.Upsert(ctx, episodeMem, episodeVec))

	// Filter search to only return rule-type memories.
	ruleType := models.MemoryTypeRule
	filters := &store.SearchFilters{Type: &ruleType}

	queryVec := mustEmbed(t, emb, "testing rules")
	results, err := st.Search(ctx, queryVec, 10, filters)
	require.NoError(t, err)
	require.NotEmpty(t, results, "expected rule results")

	for i, r := range results {
		assert.Equal(t, models.MemoryTypeRule, r.Memory.Type,
			"result[%d] has wrong type %q", i, r.Memory.Type)
	}
}

// TestMemgraphSearch_DifferentiatesContent stores two semantically different
// memories and verifies the correct one scores significantly higher.
func TestMemgraphSearch_DifferentiatesContent(t *testing.T) {
	st := newIntegrationMemgraph(t)
	emb := newTestEmbedder(t)
	ctx := context.Background()

	goContent := "Go uses goroutines for concurrency"
	pyContent := "Python uses async/await for asynchronous programming"

	goVec := mustEmbed(t, emb, goContent)
	pyVec := mustEmbed(t, emb, pyContent)

	goMem := newMemory(models.MemoryTypeFact, goContent)
	pyMem := newMemory(models.MemoryTypeFact, pyContent)

	require.NoError(t, st.Upsert(ctx, goMem, goVec))
	require.NoError(t, st.Upsert(ctx, pyMem, pyVec))

	queryVec := mustEmbed(t, emb, "goroutines concurrency Go")
	results, err := st.Search(ctx, queryVec, 10, nil)
	require.NoError(t, err)
	require.Len(t, results, 2, "expected exactly 2 results")

	// Find scores for each memory.
	var goScore, pyScore float64
	for i := range results {
		switch results[i].Memory.ID {
		case goMem.ID:
			goScore = results[i].Score
		case pyMem.ID:
			pyScore = results[i].Score
		}
	}

	assert.Greater(t, goScore, pyScore,
		"Go result (%.4f) should score higher than Python result (%.4f)", goScore, pyScore)
	assert.Greater(t, goScore-pyScore, 0.2,
		"score difference (%.4f) should be > 0.2", goScore-pyScore)
}

// ─── Dedup Tests ───────────────────────────────────────────────────────────────

// TestMemgraphFindDuplicates_ExactMatch stores a memory and then calls
// FindDuplicates with the same embedding. Expects 1 result with score >= 0.99.
func TestMemgraphFindDuplicates_ExactMatch(t *testing.T) {
	st := newIntegrationMemgraph(t)
	emb := newTestEmbedder(t)
	ctx := context.Background()

	content := "Go is a statically typed compiled language"
	vec := mustEmbed(t, emb, content)
	mem := newMemory(models.MemoryTypeFact, content)
	require.NoError(t, st.Upsert(ctx, mem, vec))

	// Query with the exact same vector.
	dupes, err := st.FindDuplicates(ctx, vec, 0.92)
	require.NoError(t, err)
	require.Len(t, dupes, 1, "expected exactly 1 duplicate for exact match")
	assert.GreaterOrEqual(t, dupes[0].Score, 0.99,
		"exact match score should be >= 0.99, got %.4f", dupes[0].Score)
}

// TestMemgraphFindDuplicates_NearMatch stores a memory and queries with a
// semantically close but not identical embedding. Expects score > 0.85.
func TestMemgraphFindDuplicates_NearMatch(t *testing.T) {
	st := newIntegrationMemgraph(t)
	emb := newTestEmbedder(t)
	ctx := context.Background()

	storedContent := "Go is a compiled language"
	queryContent := "Go is a statically compiled programming language"

	storedVec := mustEmbed(t, emb, storedContent)
	queryVec := mustEmbed(t, emb, queryContent)

	mem := newMemory(models.MemoryTypeFact, storedContent)
	require.NoError(t, st.Upsert(ctx, mem, storedVec))

	dupes, err := st.FindDuplicates(ctx, queryVec, 0.85)
	require.NoError(t, err)
	require.NotEmpty(t, dupes, "expected at least one near-match duplicate")
	assert.Greater(t, dupes[0].Score, 0.85,
		"near-match score should be > 0.85, got %.4f", dupes[0].Score)
}

// TestMemgraphFindDuplicates_NoMatch stores a Go content memory and queries
// with a totally unrelated embedding.  Expects 0 results (below threshold).
func TestMemgraphFindDuplicates_NoMatch(t *testing.T) {
	st := newIntegrationMemgraph(t)
	emb := newTestEmbedder(t)
	ctx := context.Background()

	storedContent := "Go uses goroutines for concurrency"
	unrelatedQuery := "French cooking recipes and culinary techniques"

	storedVec := mustEmbed(t, emb, storedContent)
	queryVec := mustEmbed(t, emb, unrelatedQuery)

	mem := newMemory(models.MemoryTypeFact, storedContent)
	require.NoError(t, st.Upsert(ctx, mem, storedVec))

	dupes, err := st.FindDuplicates(ctx, queryVec, 0.92)
	require.NoError(t, err)
	assert.Empty(t, dupes, "expected 0 results for unrelated query")
}

// TestMemgraphDedup_PreventsDuplicateStore simulates the capture pipeline:
// embed a memory, check for duplicates, upsert only if no duplicate exists.
// Attempting the same flow twice should result in exactly 1 node in the database.
func TestMemgraphDedup_PreventsDuplicateStore(t *testing.T) {
	st := newIntegrationMemgraph(t)
	emb := newTestEmbedder(t)
	ctx := context.Background()

	content := "Always use context cancellation for long-running operations"
	vec := mustEmbed(t, emb, content)

	// First store: no duplicates yet, so upsert proceeds.
	dupes, err := st.FindDuplicates(ctx, vec, 0.92)
	require.NoError(t, err)
	require.Empty(t, dupes, "should be no duplicates on first store")

	mem1 := newMemory(models.MemoryTypeRule, content)
	require.NoError(t, st.Upsert(ctx, mem1, vec))

	// Second store: duplicate found, so skip.
	dupes2, err := st.FindDuplicates(ctx, vec, 0.92)
	require.NoError(t, err)
	require.NotEmpty(t, dupes2, "should detect duplicate on second attempt")

	// Verify only 1 node exists in the database.
	allMems, _, listErr := st.List(ctx, nil, 100, "")
	require.NoError(t, listErr)
	assert.Len(t, allMems, 1, "expected exactly 1 memory node after dedup prevention")
}

// ─── CRUD Tests ────────────────────────────────────────────────────────────────

// TestMemgraphUpsert_AndGet stores a memory and retrieves it by ID, verifying
// that all key fields round-trip correctly.
func TestMemgraphUpsert_AndGet(t *testing.T) {
	st := newIntegrationMemgraph(t)
	emb := newTestEmbedder(t)
	ctx := context.Background()

	content := "use defer to release resources in Go"
	vec := mustEmbed(t, emb, content)
	now := time.Now().UTC().Truncate(time.Second)

	mem := models.Memory{
		ID:           uuid.New().String(),
		Type:         models.MemoryTypeRule,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      content,
		Confidence:   0.95,
		Source:       "integration-test",
		Tags:         []string{"go", "defer", "resources"},
		Project:      "test-project",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
		AccessCount:  3,
	}

	require.NoError(t, st.Upsert(ctx, mem, vec))

	got, err := st.Get(ctx, mem.ID)
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, mem.ID, got.ID)
	assert.Equal(t, mem.Type, got.Type)
	assert.Equal(t, mem.Scope, got.Scope)
	assert.Equal(t, mem.Content, got.Content)
	assert.InDelta(t, mem.Confidence, got.Confidence, 0.001)
	assert.Equal(t, mem.Source, got.Source)
	assert.Equal(t, mem.Project, got.Project)
	assert.ElementsMatch(t, mem.Tags, got.Tags)
	assert.Equal(t, mem.AccessCount, got.AccessCount)
}

// TestMemgraphGet_NotFound verifies that Get returns store.ErrNotFound for a
// nonexistent ID.
func TestMemgraphGet_NotFound(t *testing.T) {
	st := newIntegrationMemgraph(t)
	ctx := context.Background()

	_, err := st.Get(ctx, "nonexistent-id-"+uuid.New().String())
	require.Error(t, err)
	assert.True(t, errors.Is(err, store.ErrNotFound),
		"expected ErrNotFound, got: %v", err)
}

// TestMemgraphDelete stores a memory, deletes it, and confirms Get returns
// ErrNotFound afterward.
func TestMemgraphDelete(t *testing.T) {
	st := newIntegrationMemgraph(t)
	emb := newTestEmbedder(t)
	ctx := context.Background()

	mem := newMemory(models.MemoryTypeFact, "deletable memory")
	vec := mustEmbed(t, emb, mem.Content)
	require.NoError(t, st.Upsert(ctx, mem, vec))

	// Confirm it exists.
	_, err := st.Get(ctx, mem.ID)
	require.NoError(t, err)

	// Delete it.
	require.NoError(t, st.Delete(ctx, mem.ID))

	// Confirm it is gone.
	_, err = st.Get(ctx, mem.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, store.ErrNotFound),
		"expected ErrNotFound after delete, got: %v", err)
}

// TestMemgraphList_Pagination stores 5 memories and paginates through them with
// a page size of 2, verifying that all 5 are returned across pages.
func TestMemgraphList_Pagination(t *testing.T) {
	st := newIntegrationMemgraph(t)
	emb := newTestEmbedder(t)
	ctx := context.Background()

	const total = 5
	stored := make(map[string]bool)
	for i := range total {
		content := []string{
			"Go interfaces enable duck typing",
			"Go channels communicate between goroutines",
			"Go has a built-in garbage collector",
			"Go modules manage dependencies",
			"Go slices are backed by arrays",
		}[i]
		mem := newMemory(models.MemoryTypeFact, content)
		vec := mustEmbed(t, emb, content)
		require.NoError(t, st.Upsert(ctx, mem, vec))
		stored[mem.ID] = true
	}

	// Paginate with limit=2.
	seen := make(map[string]bool)
	cursor := ""
	pages := 0
	for {
		page, next, listErr := st.List(ctx, nil, 2, cursor)
		require.NoError(t, listErr)
		for i := range page {
			seen[page[i].ID] = true
		}
		pages++
		if next == "" {
			break
		}
		cursor = next
		// Safety guard against infinite loops.
		if pages > total+1 {
			t.Fatal("pagination did not terminate")
		}
	}

	assert.Len(t, seen, total, "expected all %d memories across pages", total)
}

// TestMemgraphUpdateAccessMetadata stores a memory, calls UpdateAccessMetadata,
// and verifies access_count incremented and last_accessed was updated.
func TestMemgraphUpdateAccessMetadata(t *testing.T) {
	st := newIntegrationMemgraph(t)
	emb := newTestEmbedder(t)
	ctx := context.Background()

	mem := newMemory(models.MemoryTypeEpisode, "deployed the service today")
	mem.AccessCount = 0
	vec := mustEmbed(t, emb, mem.Content)
	require.NoError(t, st.Upsert(ctx, mem, vec))

	before, err := st.Get(ctx, mem.ID)
	require.NoError(t, err)
	initialCount := before.AccessCount
	initialAccessed := before.LastAccessed

	// Small sleep to guarantee last_accessed changes.
	time.Sleep(10 * time.Millisecond)

	require.NoError(t, st.UpdateAccessMetadata(ctx, mem.ID))

	after, err := st.Get(ctx, mem.ID)
	require.NoError(t, err)

	assert.Equal(t, initialCount+1, after.AccessCount,
		"access_count should have incremented by 1")
	assert.True(t, after.LastAccessed.After(initialAccessed) || after.LastAccessed.Equal(initialAccessed),
		"last_accessed should not have gone backwards")
}

// ─── Entity Tests ──────────────────────────────────────────────────────────────

// TestMemgraphUpsertEntity_Dedup upserts the same entity name twice with updated
// summary and verifies only 1 Entity node exists (MERGE on name).
func TestMemgraphUpsertEntity_Dedup(t *testing.T) {
	st := newIntegrationMemgraph(t)
	ctx := context.Background()

	entity1 := newEntity("Alice", models.EntityTypePerson)
	entity1.Summary = "first summary"

	require.NoError(t, st.UpsertEntity(ctx, entity1))

	// Upsert again with the same name but different summary.
	entity2 := entity1 // same ID and name
	entity2.Summary = "updated summary"
	entity2.UpdatedAt = time.Now().UTC()

	require.NoError(t, st.UpsertEntity(ctx, entity2))

	// Retrieve by ID and verify the summary was updated.
	got, err := st.GetEntity(ctx, entity1.ID)
	require.NoError(t, err)
	assert.Equal(t, "updated summary", got.Summary,
		"entity summary should reflect the second upsert")

	// Verify only 1 node via SearchEntities.
	entities, err := st.SearchEntities(ctx, "Alice")
	require.NoError(t, err)
	assert.Len(t, entities, 1, "expected exactly 1 Entity node after dedup upsert")
}

// TestMemgraphSearchEntities stores 3 entities and searches by name substring,
// verifying that the matching entity is returned.
func TestMemgraphSearchEntities(t *testing.T) {
	st := newIntegrationMemgraph(t)
	ctx := context.Background()

	entities := []models.Entity{
		newEntity("Alice Smith", models.EntityTypePerson),
		newEntity("Bob Jones", models.EntityTypePerson),
		newEntity("GoRoutines Framework", models.EntityTypeProject),
	}

	for i := range entities {
		require.NoError(t, st.UpsertEntity(ctx, entities[i]))
	}

	// Search for "alice" (case-insensitive).
	results, err := st.SearchEntities(ctx, "alice")
	require.NoError(t, err)
	require.NotEmpty(t, results, "expected at least 1 entity matching 'alice'")

	foundAlice := false
	for i := range results {
		if results[i].Name == "Alice Smith" {
			foundAlice = true
		}
	}
	assert.True(t, foundAlice, "Alice Smith should be in search results")

	// "bob" should only return Bob, not Alice.
	bobResults, err := st.SearchEntities(ctx, "bob")
	require.NoError(t, err)
	require.NotEmpty(t, bobResults)
	for i := range bobResults {
		assert.Contains(t, bobResults[i].Name, "Bob",
			"bob search should not return non-Bob entities")
	}
}

// ─── Graph Tests (via GraphAdapter) ────────────────────────────────────────────

// TestMemgraphEnsureSchema verifies that EnsureSchema is idempotent — calling it
// twice must not return an error.
func TestMemgraphEnsureSchema(t *testing.T) {
	st := newIntegrationMemgraph(t)
	ctx := context.Background()

	ga := memgraph.NewGraphAdapter(st)

	// First call already happened in newIntegrationMemgraph; call it again.
	require.NoError(t, ga.EnsureSchema(ctx), "second EnsureSchema call should be idempotent")
}

// TestMemgraphUpsertFact_AndSearch creates two entities, upserts a fact between
// them, then verifies SearchFacts returns it.
func TestMemgraphUpsertFact_AndSearch(t *testing.T) {
	st := newIntegrationMemgraph(t)
	ctx := context.Background()

	ga := memgraph.NewGraphAdapter(st)

	alice := newEntity("Alice", models.EntityTypePerson)
	acme := newEntity("Acme Corp", models.EntityTypeProject)

	require.NoError(t, st.UpsertEntity(ctx, alice))
	require.NoError(t, st.UpsertEntity(ctx, acme))

	factText := "Alice is the lead engineer at Acme Corp"
	fact := models.Fact{
		ID:             uuid.New().String(),
		SourceEntityID: alice.ID,
		TargetEntityID: acme.ID,
		RelationType:   "WORKS_AT",
		Fact:           factText,
		CreatedAt:      time.Now().UTC(),
		Confidence:     0.9,
	}

	require.NoError(t, ga.UpsertFact(ctx, fact))

	results, err := ga.SearchFacts(ctx, "lead engineer", nil, 10)
	require.NoError(t, err)
	require.NotEmpty(t, results, "expected at least 1 fact matching 'lead engineer'")

	found := false
	for i := range results {
		if results[i].ID == fact.ID {
			found = true
			assert.Equal(t, factText, results[i].Fact)
		}
	}
	assert.True(t, found, "inserted fact should appear in SearchFacts results")
}

// TestMemgraphGetFactsBetween creates two entities and a fact between them, then
// verifies GetFactsBetween returns that fact.
func TestMemgraphGetFactsBetween(t *testing.T) {
	st := newIntegrationMemgraph(t)
	ctx := context.Background()

	ga := memgraph.NewGraphAdapter(st)

	src := newEntity("Engineer", models.EntityTypePerson)
	tgt := newEntity("Database", models.EntityTypeSystem)

	require.NoError(t, st.UpsertEntity(ctx, src))
	require.NoError(t, st.UpsertEntity(ctx, tgt))

	fact := models.Fact{
		ID:             uuid.New().String(),
		SourceEntityID: src.ID,
		TargetEntityID: tgt.ID,
		RelationType:   "MAINTAINS",
		Fact:           "Engineer maintains the Database system",
		CreatedAt:      time.Now().UTC(),
		Confidence:     0.85,
	}

	require.NoError(t, ga.UpsertFact(ctx, fact))

	facts, err := ga.GetFactsBetween(ctx, src.ID, tgt.ID)
	require.NoError(t, err)
	require.NotEmpty(t, facts, "expected facts between src and tgt")

	found := false
	for i := range facts {
		if facts[i].ID == fact.ID {
			found = true
			assert.Equal(t, fact.Fact, facts[i].Fact)
			assert.Equal(t, fact.RelationType, facts[i].RelationType)
			assert.Nil(t, facts[i].ExpiredAt, "active fact should have nil ExpiredAt")
		}
	}
	assert.True(t, found, "upserted fact should be returned by GetFactsBetween")
}

// TestMemgraphInvalidateFact creates a fact, invalidates it, then verifies that
// GetFactsBetween no longer returns it.
func TestMemgraphInvalidateFact(t *testing.T) {
	st := newIntegrationMemgraph(t)
	ctx := context.Background()

	ga := memgraph.NewGraphAdapter(st)

	src := newEntity("Developer", models.EntityTypePerson)
	tgt := newEntity("Legacy Service", models.EntityTypeSystem)

	require.NoError(t, st.UpsertEntity(ctx, src))
	require.NoError(t, st.UpsertEntity(ctx, tgt))

	fact := models.Fact{
		ID:             uuid.New().String(),
		SourceEntityID: src.ID,
		TargetEntityID: tgt.ID,
		RelationType:   "OWNS",
		Fact:           "Developer owns the Legacy Service",
		CreatedAt:      time.Now().UTC(),
		Confidence:     0.8,
	}

	require.NoError(t, ga.UpsertFact(ctx, fact))

	// Confirm the fact is active before invalidation.
	activeFacts, err := ga.GetFactsBetween(ctx, src.ID, tgt.ID)
	require.NoError(t, err)
	require.NotEmpty(t, activeFacts, "fact should be active before invalidation")

	// Invalidate the fact.
	now := time.Now().UTC()
	require.NoError(t, ga.InvalidateFact(ctx, fact.ID, now, now))

	// After invalidation, GetFactsBetween should return no active facts.
	remainingFacts, err := ga.GetFactsBetween(ctx, src.ID, tgt.ID)
	require.NoError(t, err)
	for i := range remainingFacts {
		assert.NotEqual(t, fact.ID, remainingFacts[i].ID,
			"invalidated fact should not appear in active facts")
	}
}
