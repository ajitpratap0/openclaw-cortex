package memgraph

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/pkg/vecmath"
	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// ErrMAGENotAvailable is returned by RunCommunityDetection and GetMemoriesForCommunity
// when the Memgraph MAGE library is not installed or the community_detection module
// is not loaded.
var ErrMAGENotAvailable = fmt.Errorf("MAGE community_detection module not available")

// GraphAdapter wraps MemgraphStore and implements graph.Client.
// It is needed because graph.Client.SearchEntities has a different signature than
// store.Store.SearchEntities — Go does not allow two methods with the same name but
// different signatures on the same struct, so we use this thin adapter for the graph
// interface. All other methods delegate directly to the underlying MemgraphStore.
//
// NOTE: Memgraph does not support vector indexes on relationships (RELATES_TO edges).
// Fact embeddings are stored as edge properties and cosine similarity is computed in
// Go after fetching candidate facts. SearchFacts fetches non-expired facts with their
// embeddings for cosine ranking.
// NOTE: capped at fetchLimit rows in arbitrary storage order — not a true full scan.
// Increase fetchLimit (or remove LIMIT) if the graph has many facts and recall quality degrades.
type GraphAdapter struct {
	store         *MemgraphStore
	mu            sync.RWMutex      // protects embeddr
	embeddr       embedder.Embedder // optional; enables semantic fact embedding in UpsertFact
	mageAvailable bool              // true if MAGE community_detection module is loaded
}

// NewGraphAdapter creates a GraphAdapter that delegates to the given MemgraphStore.
func NewGraphAdapter(s *MemgraphStore) *GraphAdapter {
	return &GraphAdapter{store: s}
}

// SetEmbedder attaches an embedder to the GraphAdapter. When set, UpsertFact will
// automatically embed the fact text if the Fact.FactEmbedding field is empty.
// Safe to call concurrently with other GraphAdapter methods.
func (g *GraphAdapter) SetEmbedder(e embedder.Embedder) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.embeddr = e
}

// Compile-time assertion: GraphAdapter must satisfy graph.Client.
var _ graph.Client = (*GraphAdapter)(nil)

// isAlreadyExistsErr returns true if the error message indicates a constraint or
// index already exists (Memgraph does not support IF NOT EXISTS on constraints).
func isAlreadyExistsErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "constraint already") ||
		strings.Contains(msg, "index already")
}

// luceneSpecialChars is a 128-entry boolean lookup table for Lucene special
// characters that Memgraph's text_search.search_all treats as query syntax
// operators.  Initialized once at package load time so SanitizeTextSearchQuery
// is O(q) with no per-call allocation.
//
// Stripped set: + - & | ! ( ) { } [ ] ^ " ~ * ? : \ /
var luceneSpecialChars = func() [128]bool {
	var t [128]bool
	for _, c := range `+-&|!(){}[]^"~*?:\/` {
		if c < 128 {
			t[c] = true
		}
	}
	return t
}()

// SanitizeTextSearchQuery removes characters that Memgraph's text_search.search_all
// treats as Lucene query syntax operators, causing "Unknown exception!" when present
// in a plain natural-language query.  The colon is the primary culprit (interpreted
// as a Lucene field specifier, e.g. "name:foo"), but we strip the full set of
// Lucene special characters to be safe.
//
// Exported so the tests/ package can verify sanitization without requiring a live
// Memgraph instance.
func SanitizeTextSearchQuery(q string) string {
	return strings.Map(func(r rune) rune {
		if r < 128 && luceneSpecialChars[r] {
			return ' '
		}
		return r
	}, q)
}

// BuildMemoryVectorIndexDDL returns the CREATE VECTOR INDEX DDL for the given dimension.
// Exported for testing.
func BuildMemoryVectorIndexDDL(dim int) string {
	return fmt.Sprintf(
		`CREATE VECTOR INDEX memory_embedding ON :Memory(embedding) WITH CONFIG {"dimension": %d, "metric": "cos", "capacity": 10000}`,
		dim,
	)
}

// BuildEntityVectorIndexDDL returns the CREATE VECTOR INDEX DDL for entities.
func BuildEntityVectorIndexDDL(dim int) string {
	return fmt.Sprintf(
		`CREATE VECTOR INDEX entity_name_embedding ON :Entity(name_embedding) WITH CONFIG {"dimension": %d, "metric": "cos", "capacity": 10000}`,
		dim,
	)
}

// BuildSearchEntitiesCypher returns the Cypher query used by SearchEntities for text search.
// Exported so that tests in the tests/ package can verify the query uses the correct
// Memgraph procedure (text_search.search_all) and clause order (WITH before WHERE).
// Do not call from production code; exported only to satisfy the tests/ package
// testing convention (tests are black-box and cannot access unexported identifiers).
//
// Fixes:
//   - Bug 1: Memgraph requires an explicit WITH clause to bridge YIELD and WHERE;
//     WHERE directly after YIELD causes "mismatched input 'WHERE'" parse error.
//   - Bug 2: text_search.search throws "Unknown exception!" on current Memgraph;
//     text_search.search_all is the correct procedure name.
func BuildSearchEntitiesCypher() string {
	return `
		CALL text_search.search_all("entity_text", $query)
		YIELD node, score
		WITH node, score
		WHERE ($project = "" OR node.project = $project)
		RETURN node.uuid AS id, node.name AS name, node.type AS type, score
		LIMIT $limit
	`
}

// EnsureSchema creates indexes, constraints, and vector indexes on Memgraph.
// Memgraph does not support IF NOT EXISTS on constraints, so "already exists" errors
// are caught and logged as warnings.
func (g *GraphAdapter) EnsureSchema(ctx context.Context, vectorDim int) error {
	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	queries := []string{
		// Uniqueness constraints — Memgraph-specific DDL (no IF NOT EXISTS)
		"CREATE CONSTRAINT ON (m:Memory) ASSERT m.uuid IS UNIQUE",
		"CREATE CONSTRAINT ON (e:Entity) ASSERT e.name IS UNIQUE",

		// Vector indexes for semantic search
		BuildMemoryVectorIndexDDL(vectorDim),
		BuildEntityVectorIndexDDL(vectorDim),

		// Property indexes for filtering
		"CREATE INDEX ON :Memory(type)",
		"CREATE INDEX ON :Memory(scope)",
		"CREATE INDEX ON :Memory(project)",
		"CREATE INDEX ON :Memory(source)",
		"CREATE INDEX ON :Memory(uuid)",
		// Temporal versioning indexes
		"CREATE INDEX ON :Memory(valid_from)",
		"CREATE INDEX ON :Memory(valid_to)",
		"CREATE INDEX ON :Entity(uuid)",
		"CREATE INDEX ON :Entity(project)",

		// Text search index for entity fulltext (label-wide)
		"CREATE TEXT INDEX entity_text ON :Entity",
		// Note: Memgraph does not support text indexes on relationships.
		// Fact text search uses property-level CONTAINS matching instead.
	}

	for i := range queries {
		// Memgraph requires auto-commit (implicit) transactions for DDL.
		// session.Run() executes as an auto-commit transaction.
		result, runErr := session.Run(ctx, queries[i], nil)
		if runErr != nil {
			if isAlreadyExistsErr(runErr) {
				g.store.logger.Debug("memgraph schema already exists, skipping", "query_index", i)
				continue
			}
			return fmt.Errorf("memgraph ensure schema query %d: %w", i, runErr)
		}
		// Consume the result to ensure the query completes.
		if result != nil {
			_, _ = result.Consume(ctx)
		}
	}

	g.store.logger.Info("memgraph schema ensured", "query_count", len(queries))

	// Probe for MAGE community_detection availability.
	// Uses a LIMIT 0 query so no data is returned; success means the module is loaded.
	mageProbeResult, mageProbeErr := session.Run(ctx,
		`CALL community_detection.get() YIELD node, community_id RETURN 1 LIMIT 0`,
		nil,
	)
	if mageProbeErr != nil {
		g.store.logger.Debug("memgraph MAGE community_detection not available", "error", mageProbeErr)
		g.mageAvailable = false
	} else {
		if mageProbeResult != nil {
			_, _ = mageProbeResult.Consume(ctx)
		}
		g.mageAvailable = true
		g.store.logger.Info("memgraph MAGE community_detection available")
	}

	return nil
}

// UpsertEntity creates or updates an Entity node. Delegates to MemgraphStore.
func (g *GraphAdapter) UpsertEntity(ctx context.Context, entity models.Entity) error {
	return g.store.UpsertEntity(ctx, entity)
}

// SearchEntities finds entities by text search, optionally filtered by project.
// This implements graph.Client.SearchEntities (different signature from store.Store.SearchEntities).
func (g *GraphAdapter) SearchEntities(ctx context.Context, query string, _ []float32, project string, limit int) ([]graph.EntityResult, error) {
	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	var cypher string
	params := map[string]any{
		"limit": int64(limit),
	}

	if query != "" {
		cypher = BuildSearchEntitiesCypher()
		params["query"] = SanitizeTextSearchQuery(query)
		params["project"] = project
	} else {
		cypher = `MATCH (node:Entity)`
		if project != "" {
			cypher += ` WHERE node.project = $project`
			params["project"] = project
		}
		cypher += `
			RETURN node.uuid AS id, node.name AS name, node.type AS type, 1.0 AS score
			LIMIT $limit
		`
	}

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		records, runErr := tx.Run(ctx, cypher, params)
		if runErr != nil {
			return nil, runErr
		}

		var results []graph.EntityResult
		for records.Next(ctx) {
			record := records.Record()
			id, _, _ := neo4j.GetRecordValue[string](record, "id")
			name, _, _ := neo4j.GetRecordValue[string](record, "name")
			entityType, _, _ := neo4j.GetRecordValue[string](record, "type")
			score, _, _ := neo4j.GetRecordValue[float64](record, "score")

			results = append(results, graph.EntityResult{
				ID:    id,
				Name:  name,
				Type:  entityType,
				Score: score,
			})
		}
		if collectErr := records.Err(); collectErr != nil {
			return nil, collectErr
		}
		return results, nil
	})
	if err != nil {
		return nil, fmt.Errorf("memgraph search entities: %w", err)
	}

	results, _ := result.([]graph.EntityResult)
	return results, nil
}

// GetEntity retrieves a single entity by ID. Delegates to MemgraphStore.
func (g *GraphAdapter) GetEntity(ctx context.Context, id string) (*models.Entity, error) {
	return g.store.GetEntity(ctx, id)
}

// validateAndNormalizeRelType sanitizes a relation type string before use as a Cypher label.
// Only types in the canonical enum are accepted; all others fall back to "RELATES_TO".
// This is critical for injection safety — the normalized value is concatenated into Cypher.
// The output is ONLY from the ValidRelationshipTypes whitelist; user input is never interpolated directly.
func validateAndNormalizeRelType(relType string) string {
	normalized := strings.ToUpper(strings.TrimSpace(relType))
	if models.ValidRelationshipTypes[normalized] {
		return normalized
	}
	return string(models.RelTypeRelatesTo)
}

// UpsertFact creates or updates a RELATES_TO relationship between two entities.
// If the fact has no embedding and an embedder is configured, the fact text is
// embedded before storage, enabling cosine similarity ranking in SearchFacts.
func (g *GraphAdapter) UpsertFact(ctx context.Context, fact models.Fact) error {
	// Embed fact text when no embedding is present and an embedder is available.
	g.mu.RLock()
	emb := g.embeddr
	g.mu.RUnlock()
	if len(fact.FactEmbedding) == 0 && emb != nil {
		vec, embedErr := emb.Embed(ctx, fact.Fact)
		if embedErr != nil {
			// Non-fatal: log and proceed without embedding.
			g.store.logger.Warn("upsert fact: embedding failed, storing without vector",
				"fact_id", fact.ID, "error", embedErr)
		} else {
			fact.FactEmbedding = vec
		}
	}

	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	// Use the sanitized/validated relationship type as the Cypher label.
	// validateAndNormalizeRelType ensures only whitelisted labels are used — never raw user input.
	relLabel := validateAndNormalizeRelType(fact.RelationType)
	cypher := fmt.Sprintf(`
		MATCH (s:Entity {uuid: $source_id})
		MATCH (t:Entity {uuid: $target_id})
		MERGE (s)-[r:%s {uuid: $uuid}]->(t)
		SET r.relation_type = $relation_type,
		    r.fact = $fact,
		    r.fact_embedding = $fact_embedding,
		    r.created_at = $created_at,
		    r.expired_at = $expired_at,
		    r.valid_at = $valid_at,
		    r.invalid_at = $invalid_at,
		    r.source_memory_ids = $source_memory_ids,
		    r.episodes = $episodes,
		    r.confidence = $confidence
	`, relLabel)

	params := map[string]any{
		"uuid":              fact.ID,
		"source_id":         fact.SourceEntityID,
		"target_id":         fact.TargetEntityID,
		"relation_type":     fact.RelationType,
		"fact":              fact.Fact,
		"fact_embedding":    fact.FactEmbedding,
		"created_at":        fact.CreatedAt.UTC().Format(time.RFC3339Nano),
		"source_memory_ids": fact.SourceMemoryIDs,
		"episodes":          fact.Episodes,
		"confidence":        fact.Confidence,
	}

	// Bi-temporal nullable fields — store as RFC3339Nano strings or nil
	if fact.ExpiredAt != nil {
		params["expired_at"] = fact.ExpiredAt.UTC().Format(time.RFC3339Nano)
	} else {
		params["expired_at"] = nil
	}
	if fact.ValidAt != nil {
		params["valid_at"] = fact.ValidAt.UTC().Format(time.RFC3339Nano)
	} else {
		params["valid_at"] = nil
	}
	if fact.InvalidAt != nil {
		params["invalid_at"] = fact.InvalidAt.UTC().Format(time.RFC3339Nano)
	} else {
		params["invalid_at"] = nil
	}

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, runErr := tx.Run(ctx, cypher, params)
		return nil, runErr
	})
	if err != nil {
		return fmt.Errorf("memgraph upsert fact %s: %w", fact.ID, err)
	}
	return nil
}

// SearchFacts performs a search over RELATES_TO relationships.
//
// When embedding is non-nil, facts are fetched (with a larger candidate window),
// their stored fact_embedding is compared against the query embedding via cosine
// similarity, results are ranked by that score, and the top `limit` are returned.
// This is a property-based cosine fallback because Memgraph does not support vector
// indexes on relationships.
//
// When embedding is nil, a text-search fallback using CONTAINS matching is used.
func (g *GraphAdapter) SearchFacts(ctx context.Context, query string, embedding []float32, limit int) ([]graph.FactResult, error) {
	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	// When embedding is provided, fetch a broader candidate set and re-rank by cosine.
	// When nil, fall back to text CONTAINS matching.
	// All production call sites pass a positive limit (e.g. RecallByGraph always passes 50),
	// so this guard catches programming errors rather than expected use.
	if limit <= 0 {
		return nil, fmt.Errorf("SearchFacts: limit must be > 0, got %d", limit)
	}
	useEmbedding := len(embedding) > 0
	fetchLimit := int64(limit)
	if useEmbedding {
		// Fetch up to 10× the requested limit as candidates for cosine re-ranking.
		// Cap at math.MaxInt64 to guard against integer overflow on pathological inputs.
		//
		// Important: this is NOT a correct top-K algorithm for graphs with more facts
		// than fetchLimit. Cosine scores are computed only over the facts returned by
		// Memgraph in storage order; facts beyond fetchLimit are never scored and can
		// never appear in results regardless of their relevance. The saturation warning
		// below fires when this limit is reached. For large graphs, increase
		// candidateFactor or add a native vector index that supports exact top-K.
		const candidateFactor = 10
		if fetchLimit <= math.MaxInt64/candidateFactor {
			fetchLimit *= candidateFactor
		} else {
			fetchLimit = math.MaxInt64
		}
		fetchLimit = max(fetchLimit, 200)
	}

	var cypher string
	params := map[string]any{"limit": fetchLimit}

	if useEmbedding {
		// Fetch all non-expired facts with their embeddings for cosine ranking.
		// [r] matches any relationship type — facts may use typed labels (e.g. WORKS_AT).
		cypher = `
			MATCH (s:Entity)-[r]->(t:Entity)
			WHERE r.expired_at IS NULL
			RETURN r.uuid AS uuid, r.fact AS fact, s.uuid AS source_entity_id,
			       t.uuid AS target_entity_id, r.source_memory_ids AS source_memory_ids,
			       r.fact_embedding AS fact_embedding
			LIMIT $limit
		`
	} else {
		// [r] matches any relationship type — facts may use typed labels (e.g. WORKS_AT).
		cypher = `
			MATCH (s:Entity)-[r]->(t:Entity)
			WHERE r.expired_at IS NULL
			  AND (toLower(r.fact) CONTAINS toLower($query)
			       OR toLower(r.relation_type) CONTAINS toLower($query))
			RETURN r.uuid AS uuid, r.fact AS fact, s.uuid AS source_entity_id,
			       t.uuid AS target_entity_id, r.source_memory_ids AS source_memory_ids,
			       null AS fact_embedding
			LIMIT $limit
		`
		params["query"] = query
	}

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		records, runErr := tx.Run(ctx, cypher, params)
		if runErr != nil {
			return nil, runErr
		}

		var results []graph.FactResult
		for records.Next(ctx) {
			record := records.Record()
			id, _, _ := neo4j.GetRecordValue[string](record, "uuid")
			factText, _, _ := neo4j.GetRecordValue[string](record, "fact")
			sourceID, _, _ := neo4j.GetRecordValue[string](record, "source_entity_id")
			targetID, _, _ := neo4j.GetRecordValue[string](record, "target_entity_id")

			memoryIDs := getStringSlice(record, "source_memory_ids")

			// Compute cosine similarity here, inside the closure, so the stored
			// embedding is not carried in the public FactResult type.
			// Facts without a stored embedding receive score 0 and sort to the bottom;
			// they remain discoverable — run a backfill via UpsertFact to fix them.
			score := 1.0
			if useEmbedding {
				factEmb := getFloat32Slice(record, "fact_embedding")
				if len(factEmb) > 0 {
					score = float64(vecmath.CosineSimilarity(embedding, factEmb))
				} else {
					score = 0
				}
			}

			results = append(results, graph.FactResult{
				ID:              id,
				Fact:            factText,
				SourceEntityID:  sourceID,
				TargetEntityID:  targetID,
				SourceMemoryIDs: memoryIDs,
				Score:           score,
			})
		}
		if collectErr := records.Err(); collectErr != nil {
			return nil, collectErr
		}
		return results, nil
	})
	if err != nil {
		return nil, fmt.Errorf("memgraph search facts: %w", err)
	}

	results, _ := result.([]graph.FactResult)

	// Warn when the candidate window is saturated — results may exclude highly-relevant
	// facts that appear later in storage order. Increase the multiplier or implement a
	// full scan to improve recall quality when this fires frequently.
	// Note: LIMIT prevents Memgraph from returning more than fetchLimit rows, so
	// len(results) > fetchLimit is impossible; == is the only true saturation condition.
	// Edge case: when fetchLimit was capped at math.MaxInt64 (the overflow-guard branch),
	// len(results) can never equal math.MaxInt64 (a slice that large would exhaust memory),
	// so the saturation warning is effectively disabled for that path. This is intentional:
	// overflow inputs are pathological and the guard is never reached in practice.
	if useEmbedding && int64(len(results)) == fetchLimit {
		g.store.logger.Warn("search facts: candidate window saturated; results may be incomplete",
			"limit", limit, "fetch_limit", fetchLimit, "candidates_fetched", len(results))
	}

	if useEmbedding && len(results) > 0 {
		sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
		if len(results) > limit {
			results = results[:limit]
		}
	}

	return results, nil
}

// InvalidateFact sets ExpiredAt and InvalidAt on a RELATES_TO relationship.
func (g *GraphAdapter) InvalidateFact(ctx context.Context, id string, expiredAt, invalidAt time.Time) error {
	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	// [r] matches any relationship type — facts may use typed labels (e.g. WORKS_AT).
	cypher := `
		MATCH ()-[r {uuid: $uuid}]->()
		SET r.expired_at = $expired_at, r.invalid_at = $invalid_at
	`

	params := map[string]any{
		"uuid":       id,
		"expired_at": expiredAt.UTC().Format(time.RFC3339Nano),
		"invalid_at": invalidAt.UTC().Format(time.RFC3339Nano),
	}

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, runErr := tx.Run(ctx, cypher, params)
		return nil, runErr
	})
	if err != nil {
		return fmt.Errorf("memgraph invalidate fact %s: %w", id, err)
	}
	return nil
}

// GetFactsBetween returns all active facts between two entities (bidirectional).
func (g *GraphAdapter) GetFactsBetween(ctx context.Context, sourceID, targetID string) ([]models.Fact, error) {
	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	// [r] matches any relationship type — facts may use typed labels (e.g. WORKS_AT).
	cypher := `
		MATCH (n:Entity)-[r]-(m:Entity)
		WHERE r.expired_at IS NULL
		  AND ((n.uuid = $source_id AND m.uuid = $target_id)
		    OR (n.uuid = $target_id AND m.uuid = $source_id))
		WITH r, startNode(r) AS s, endNode(r) AS t
		RETURN r.uuid AS uuid, r.relation_type AS relation_type, r.fact AS fact,
		       r.fact_embedding AS fact_embedding,
		       r.created_at AS created_at, r.expired_at AS expired_at,
		       r.valid_at AS valid_at, r.invalid_at AS invalid_at,
		       r.source_memory_ids AS source_memory_ids, r.episodes AS episodes,
		       r.confidence AS confidence,
		       s.uuid AS source_entity_id, t.uuid AS target_entity_id
	`

	params := map[string]any{
		"source_id": sourceID,
		"target_id": targetID,
	}

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		return collectFacts(ctx, tx, cypher, params)
	})
	if err != nil {
		return nil, fmt.Errorf("memgraph get facts between %s and %s: %w", sourceID, targetID, err)
	}

	facts, _ := result.([]models.Fact)
	return facts, nil
}

// GetFactsForEntity returns all active facts involving an entity (as source or target).
func (g *GraphAdapter) GetFactsForEntity(ctx context.Context, entityID string) ([]models.Fact, error) {
	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	// [r] matches any relationship type — facts may use typed labels (e.g. WORKS_AT).
	cypher := `
		MATCH (n:Entity {uuid: $entity_id})-[r]-(m:Entity)
		WHERE r.expired_at IS NULL
		WITH r, startNode(r) AS s, endNode(r) AS t
		RETURN r.uuid AS uuid, r.relation_type AS relation_type, r.fact AS fact,
		       r.fact_embedding AS fact_embedding,
		       r.created_at AS created_at, r.expired_at AS expired_at,
		       r.valid_at AS valid_at, r.invalid_at AS invalid_at,
		       r.source_memory_ids AS source_memory_ids, r.episodes AS episodes,
		       r.confidence AS confidence,
		       s.uuid AS source_entity_id, t.uuid AS target_entity_id
	`

	params := map[string]any{
		"entity_id": entityID,
	}

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		return collectFacts(ctx, tx, cypher, params)
	})
	if err != nil {
		return nil, fmt.Errorf("memgraph get facts for entity %s: %w", entityID, err)
	}

	facts, _ := result.([]models.Fact)
	return facts, nil
}

// AppendEpisode adds an episode/session ID to a fact's episodes list.
func (g *GraphAdapter) AppendEpisode(ctx context.Context, factID, episodeID string) error {
	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	// [r] matches any relationship type — facts may use typed labels (e.g. WORKS_AT).
	cypher := `
		MATCH ()-[r {uuid: $uuid}]->()
		SET r.episodes = CASE
			WHEN r.episodes IS NULL THEN [$episode_id]
			WHEN NOT $episode_id IN r.episodes THEN r.episodes + $episode_id
			ELSE r.episodes
		END
	`

	params := map[string]any{
		"uuid":       factID,
		"episode_id": episodeID,
	}

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, runErr := tx.Run(ctx, cypher, params)
		return nil, runErr
	})
	if err != nil {
		return fmt.Errorf("memgraph append episode to fact %s: %w", factID, err)
	}
	return nil
}

// AppendMemoryToFact adds a memory ID to a fact's source_memory_ids list.
func (g *GraphAdapter) AppendMemoryToFact(ctx context.Context, factID, memoryID string) error {
	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	// [r] matches any relationship type — facts may use typed labels (e.g. WORKS_AT).
	cypher := `
		MATCH ()-[r {uuid: $uuid}]->()
		SET r.source_memory_ids = CASE
			WHEN r.source_memory_ids IS NULL THEN [$memory_id]
			WHEN NOT $memory_id IN r.source_memory_ids THEN r.source_memory_ids + $memory_id
			ELSE r.source_memory_ids
		END
	`

	params := map[string]any{
		"uuid":      factID,
		"memory_id": memoryID,
	}

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, runErr := tx.Run(ctx, cypher, params)
		return nil, runErr
	})
	if err != nil {
		return fmt.Errorf("memgraph append memory to fact %s: %w", factID, err)
	}
	return nil
}

// GetMemoryFacts returns all facts derived from a given memory ID.
func (g *GraphAdapter) GetMemoryFacts(ctx context.Context, memoryID string) ([]models.Fact, error) {
	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	// [r] matches any relationship type — facts may use typed labels (e.g. WORKS_AT).
	cypher := `
		MATCH (s:Entity)-[r]->(t:Entity)
		WHERE $memory_id IN r.source_memory_ids
		RETURN r.uuid AS uuid, r.relation_type AS relation_type, r.fact AS fact,
		       r.fact_embedding AS fact_embedding,
		       r.created_at AS created_at, r.expired_at AS expired_at,
		       r.valid_at AS valid_at, r.invalid_at AS invalid_at,
		       r.source_memory_ids AS source_memory_ids, r.episodes AS episodes,
		       r.confidence AS confidence,
		       s.uuid AS source_entity_id, t.uuid AS target_entity_id
	`

	params := map[string]any{
		"memory_id": memoryID,
	}

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		return collectFacts(ctx, tx, cypher, params)
	})
	if err != nil {
		return nil, fmt.Errorf("memgraph get memory facts for %s: %w", memoryID, err)
	}

	facts, _ := result.([]models.Fact)
	return facts, nil
}

// defaultGraphDepth is the default traversal depth for graph-aware recall.
const defaultGraphDepth = 2

// RecallByGraph returns memory IDs relevant to a query via entity-aware graph traversal.
//
// It delegates to RecallByGraphWithDepth with the default depth (2-hop).
// The function respects the context deadline — callers should wrap ctx with a tight
// timeout (≤ 120 ms) to stay within the 200 ms total recall latency budget.
func (g *GraphAdapter) RecallByGraph(ctx context.Context, query string, embedding []float32, limit int) ([]string, error) {
	return g.RecallByGraphWithDepth(ctx, query, embedding, limit, defaultGraphDepth)
}

// RecallByGraphWithDepth implements configurable-depth graph-aware recall.
//
// Algorithm (Phase 4 – Graph-Aware Recall):
//  1. Entity discovery: find Entity nodes whose name matches query terms via text search.
//  2. 1-hop traversal: for each discovered entity, follow RELATES_TO edges (both directions)
//     and collect source_memory_ids from adjacent fact relationships.
//  3. 2-hop traversal (depth >= 2): for each neighbor entity found in step 2,
//     repeat the 1-hop traversal to collect transitively connected memory IDs.
//
// Memories found at 1-hop score 1.0; 2-hop memories score 0.5.
// Returns memory IDs sorted by graph distance score (descending).
func (g *GraphAdapter) RecallByGraphWithDepth(ctx context.Context, query string, embedding []float32, limit int, depth int) ([]string, error) {
	if depth < 1 {
		depth = 1
	}

	// Step 1: find entities matching the query (text search, up to 10 candidates).
	entityCandidates, err := g.SearchEntities(ctx, query, embedding, "", 10)
	if err != nil {
		// Degrade gracefully: fall back to fact-text search if entity search fails.
		g.store.logger.Warn("graph recall: entity search failed, falling back to fact text search", "error", err)
		return g.recallByFactSearch(ctx, query, embedding, limit)
	}

	if len(entityCandidates) == 0 {
		// No entity hits — fall back to fact text search.
		return g.recallByFactSearch(ctx, query, embedding, limit)
	}

	// Collect entity IDs from candidates.
	entityIDs := make([]string, 0, len(entityCandidates))
	for i := range entityCandidates {
		entityIDs = append(entityIDs, entityCandidates[i].ID)
	}

	type scoredID struct {
		id    string
		score float64
	}
	seen := make(map[string]float64)

	// Step 2: 1-hop traversal — Entity → RELATES_TO → fact → source_memory_ids.
	hop1MemIDs, hop1NeighbourIDs, err := g.traverseEntityFacts(ctx, entityIDs)
	if err != nil {
		g.store.logger.Warn("graph recall: 1-hop traversal failed", "error", err)
		return g.recallByFactSearch(ctx, query, embedding, limit)
	}

	// Score 1-hop memories at 1.0 (closest to query entities).
	for _, mid := range hop1MemIDs {
		if _, exists := seen[mid]; !exists {
			seen[mid] = 1.0
		}
	}

	// Step 3: 2-hop traversal — neighbor entities → their facts → memory IDs.
	if depth >= 2 && len(hop1NeighbourIDs) > 0 {
		// Exclude already-visited entity IDs to avoid cycles.
		visitedEntityIDs := make(map[string]struct{}, len(entityIDs))
		for _, eid := range entityIDs {
			visitedEntityIDs[eid] = struct{}{}
		}
		var hop2EntityIDs []string
		for _, nid := range hop1NeighbourIDs {
			if _, visited := visitedEntityIDs[nid]; !visited {
				hop2EntityIDs = append(hop2EntityIDs, nid)
				visitedEntityIDs[nid] = struct{}{}
			}
		}

		if len(hop2EntityIDs) > 0 {
			hop2MemIDs, _, hop2Err := g.traverseEntityFacts(ctx, hop2EntityIDs)
			if hop2Err != nil {
				// Non-fatal: 2-hop failure just means we return 1-hop results only.
				g.store.logger.Warn("graph recall: 2-hop traversal failed", "error", hop2Err)
			} else {
				// Score 2-hop memories at 0.5 (half weight of direct neighbors).
				for _, mid := range hop2MemIDs {
					if _, exists := seen[mid]; !exists {
						seen[mid] = 0.5
					}
				}
			}
		}
	}

	// Sort by graph distance score (descending), then apply limit.
	ranked := make([]scoredID, 0, len(seen))
	for id, score := range seen {
		ranked = append(ranked, scoredID{id: id, score: score})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	result := make([]string, 0, len(ranked))
	for _, s := range ranked {
		if limit > 0 && len(result) >= limit {
			break
		}
		result = append(result, s.id)
	}
	return result, nil
}

// traverseEntityFacts fetches all active RELATES_TO facts for the given entity IDs
// in a single Cypher query and returns:
//   - the union of all source_memory_ids from those facts
//   - the IDs of all neighbor entities (for 2-hop traversal)
func (g *GraphAdapter) traverseEntityFacts(ctx context.Context, entityIDs []string) (memoryIDs []string, neighborEntityIDs []string, err error) {
	if len(entityIDs) == 0 {
		return nil, nil, nil
	}

	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	// Traverse both outgoing and incoming edges from seed entities.
	// [r] matches any relationship type — facts may use typed labels (e.g. WORKS_AT).
	// expired_at IS NULL filters out invalidated (soft-deleted) facts.
	cypher := `
		UNWIND $entity_ids AS eid
		MATCH (e:Entity {uuid: eid})-[r]-(neighbor:Entity)
		WHERE r.expired_at IS NULL
		RETURN r.source_memory_ids AS memory_ids,
		       neighbor.uuid      AS neighbor_id
	`

	type row struct {
		memoryIDs  []string
		neighborID string
	}

	result, runErr := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		records, txErr := tx.Run(ctx, cypher, map[string]any{
			"entity_ids": entityIDs,
		})
		if txErr != nil {
			return nil, txErr
		}

		var rows []row
		for records.Next(ctx) {
			rec := records.Record()
			rawMIDs, _, _ := neo4j.GetRecordValue[[]any](rec, "memory_ids")
			nid, _, _ := neo4j.GetRecordValue[string](rec, "neighbor_id")

			var mids []string
			for _, v := range rawMIDs {
				if s, ok := v.(string); ok {
					mids = append(mids, s)
				}
			}
			rows = append(rows, row{memoryIDs: mids, neighborID: nid})
		}
		if recErr := records.Err(); recErr != nil {
			return nil, recErr
		}
		return rows, nil
	})
	if runErr != nil {
		return nil, nil, fmt.Errorf("graph recall traversal: %w", runErr)
	}

	rows, _ := result.([]row)
	seenMem := make(map[string]struct{})
	seenNeighbour := make(map[string]struct{})

	for _, r := range rows {
		for _, mid := range r.memoryIDs {
			if _, ok := seenMem[mid]; !ok {
				seenMem[mid] = struct{}{}
				memoryIDs = append(memoryIDs, mid)
			}
		}
		if r.neighborID != "" {
			if _, ok := seenNeighbour[r.neighborID]; !ok {
				seenNeighbour[r.neighborID] = struct{}{}
				neighborEntityIDs = append(neighborEntityIDs, r.neighborID)
			}
		}
	}
	return memoryIDs, neighborEntityIDs, nil
}

// recallByFactSearch is the fallback when entity search yields no results.
// It performs fact text search and collects source_memory_ids (original behavior).
func (g *GraphAdapter) recallByFactSearch(ctx context.Context, query string, embedding []float32, limit int) ([]string, error) {
	facts, err := g.SearchFacts(ctx, query, embedding, limit)
	if err != nil {
		return nil, fmt.Errorf("memgraph recall by graph (fact fallback): %w", err)
	}

	seen := make(map[string]bool)
	var memoryIDs []string
	for i := range facts {
		for j := range facts[i].SourceMemoryIDs {
			mid := facts[i].SourceMemoryIDs[j]
			if !seen[mid] {
				seen[mid] = true
				memoryIDs = append(memoryIDs, mid)
			}
		}
	}
	return memoryIDs, nil
}

// Healthy returns true if the Memgraph database is reachable.
// CreateEpisode stores an Episode node in Memgraph.
func (g *GraphAdapter) CreateEpisode(ctx context.Context, episode models.Episode) error {
	session := g.store.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer func() { _ = session.Close(ctx) }()

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
MERGE (e:Episode {uuid: $uuid})
SET e.session_id = $session_id,
    e.user_msg   = $user_msg,
    e.assistant_msg = $assistant_msg,
    e.captured_at   = $captured_at,
    e.memory_ids    = $memory_ids,
    e.fact_ids      = $fact_ids
`
		params := map[string]any{
			"uuid":          episode.UUID,
			"session_id":    episode.SessionID,
			"user_msg":      episode.UserMsg,
			"assistant_msg": episode.AssistantMsg,
			"captured_at":   episode.CapturedAt.Unix(),
			"memory_ids":    episode.MemoryIDs,
			"fact_ids":      episode.FactIDs,
		}
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// GetEpisodesForMemory returns all Episode nodes whose memory_ids contain the given memoryID.
func (g *GraphAdapter) GetEpisodesForMemory(ctx context.Context, memoryID string) ([]models.Episode, error) {
	session := g.store.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `MATCH (e:Episode) WHERE $memoryID IN e.memory_ids RETURN e`
		records, err := tx.Run(ctx, cypher, map[string]any{"memoryID": memoryID})
		if err != nil {
			return nil, err
		}
		var episodes []models.Episode
		for records.Next(ctx) {
			rec := records.Record()
			node, ok := rec.Values[0].(neo4j.Node)
			if !ok {
				continue
			}
			ep := models.Episode{
				UUID:      getString(node.Props, "uuid"),
				SessionID: getString(node.Props, "session_id"),
			}
			if ids, ok := node.Props["memory_ids"].([]any); ok {
				for _, id := range ids {
					if s, ok := id.(string); ok {
						ep.MemoryIDs = append(ep.MemoryIDs, s)
					}
				}
			}
			if ids, ok := node.Props["fact_ids"].([]any); ok {
				for _, id := range ids {
					if s, ok := id.(string); ok {
						ep.FactIDs = append(ep.FactIDs, s)
					}
				}
			}
			episodes = append(episodes, ep)
		}
		return episodes, records.Err()
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	eps, ok := result.([]models.Episode)
	if !ok {
		return nil, nil
	}
	return eps, nil
}

func (g *GraphAdapter) Healthy(ctx context.Context) bool {
	err := g.store.driver.VerifyConnectivity(ctx)
	if err != nil {
		g.store.logger.Warn("memgraph health check failed", "error", err)
		return false
	}
	return true
}

// Close releases the driver resources. Delegates to MemgraphStore.
func (g *GraphAdapter) Close() error {
	return g.store.Close()
}

// collectFacts runs a Cypher query and collects the results into a slice of models.Fact.
func collectFacts(ctx context.Context, tx neo4j.ManagedTransaction, cypher string, params map[string]any) ([]models.Fact, error) {
	records, err := tx.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}

	var facts []models.Fact
	for records.Next(ctx) {
		record := records.Record()
		fact := recordToFact(record)
		facts = append(facts, fact)
	}
	if collectErr := records.Err(); collectErr != nil {
		return nil, collectErr
	}
	return facts, nil
}

// recordToFact converts a Bolt record to a models.Fact.
func recordToFact(record *neo4j.Record) models.Fact {
	id, _, _ := neo4j.GetRecordValue[string](record, "uuid")
	relationType, _, _ := neo4j.GetRecordValue[string](record, "relation_type")
	factText, _, _ := neo4j.GetRecordValue[string](record, "fact")
	sourceID, _, _ := neo4j.GetRecordValue[string](record, "source_entity_id")
	targetID, _, _ := neo4j.GetRecordValue[string](record, "target_entity_id")
	confidence, _, _ := neo4j.GetRecordValue[float64](record, "confidence")
	createdAtStr, _, _ := neo4j.GetRecordValue[string](record, "created_at")
	expiredAtStr, _, _ := neo4j.GetRecordValue[string](record, "expired_at")
	validAtStr, _, _ := neo4j.GetRecordValue[string](record, "valid_at")
	invalidAtStr, _, _ := neo4j.GetRecordValue[string](record, "invalid_at")

	sourceMemoryIDs := getStringSlice(record, "source_memory_ids")
	episodes := getStringSlice(record, "episodes")
	factEmbedding := getFloat32Slice(record, "fact_embedding")

	createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)

	fact := models.Fact{
		ID:              id,
		SourceEntityID:  sourceID,
		TargetEntityID:  targetID,
		RelationType:    relationType,
		Fact:            factText,
		FactEmbedding:   factEmbedding,
		CreatedAt:       createdAt,
		SourceMemoryIDs: sourceMemoryIDs,
		Episodes:        episodes,
		Confidence:      confidence,
	}

	if expiredAtStr != "" {
		t, parseErr := time.Parse(time.RFC3339Nano, expiredAtStr)
		if parseErr == nil {
			fact.ExpiredAt = &t
		}
	}
	if validAtStr != "" {
		t, parseErr := time.Parse(time.RFC3339Nano, validAtStr)
		if parseErr == nil {
			fact.ValidAt = &t
		}
	}
	if invalidAtStr != "" {
		t, parseErr := time.Parse(time.RFC3339Nano, invalidAtStr)
		if parseErr == nil {
			fact.InvalidAt = &t
		}
	}

	return fact
}

// getString extracts a string from a map, returning "" if missing or wrong type.
func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getStringSlice extracts a []string from a Bolt record field.
func getStringSlice(record *neo4j.Record, key string) []string {
	val, ok, _ := neo4j.GetRecordValue[[]any](record, key)
	if !ok {
		return nil
	}

	result := make([]string, 0, len(val))
	for i := range val {
		s, sOK := val[i].(string)
		if sOK {
			result = append(result, s)
		}
	}
	return result
}

// getFloat32Slice extracts a []float32 from a Bolt record field.
func getFloat32Slice(record *neo4j.Record, key string) []float32 {
	val, ok, _ := neo4j.GetRecordValue[[]any](record, key)
	if !ok {
		return nil
	}

	result := make([]float32, 0, len(val))
	for i := range val {
		switch v := val[i].(type) {
		case float64:
			result = append(result, float32(v))
		case float32:
			result = append(result, v)
		}
	}
	return result
}

// maxSubgraphDepth is the maximum allowed depth for GetSubgraph variable-length path queries.
// Memgraph variable-length path bounds must be literals, not parameters.
const maxSubgraphDepth = 3

// defaultSubgraphLimit is the default LIMIT for GetSubgraph result rows.
const defaultSubgraphLimit = 200

// GetSubgraph returns the neighborhood of nodes and edges reachable from
// entityID within depth hops (capped at maxSubgraphDepth).
//
// The depth bound must be a literal integer in the Cypher variable-length path syntax;
// Memgraph does not support parameterized path bounds. We substitute the integer
// directly via fmt.Sprintf — the value is clamped to [1, maxSubgraphDepth] before
// formatting, so no user input ever reaches the Cypher string.
func (g *GraphAdapter) GetSubgraph(ctx context.Context, entityID string, depth int) (graph.SubgraphResult, error) {
	if depth < 1 {
		depth = 1
	}
	if depth > maxSubgraphDepth {
		depth = maxSubgraphDepth
	}

	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	// depth is an integer literal clamped to [1, maxSubgraphDepth] — safe to inline.
	// [r] matches any relationship type in the path (facts may use typed labels).
	cypher := fmt.Sprintf(`
		MATCH path = (start:Entity {uuid: $seed_id})-[*1..%d]-(connected:Entity)
		WHERE ALL(r IN relationships(path) WHERE r.expired_at IS NULL OR r.expired_at = "")
		RETURN DISTINCT
		    startNode(last(relationships(path))).uuid AS source_id,
		    endNode(last(relationships(path))).uuid   AS target_id,
		    connected.uuid AS connected_id,
		    connected.name AS connected_name,
		    connected.type AS connected_type,
		    length(path)   AS dist,
		    last(relationships(path)).uuid AS fact_id,
		    type(last(relationships(path)))  AS relation_label,
		    last(relationships(path)).fact   AS fact
		ORDER BY dist
		LIMIT $limit
	`, depth)

	params := map[string]any{
		"seed_id": entityID,
		"limit":   int64(defaultSubgraphLimit),
	}

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		records, runErr := tx.Run(ctx, cypher, params)
		if runErr != nil {
			return nil, runErr
		}

		res := graph.SubgraphResult{SeedEntityID: entityID}
		seenNodes := make(map[string]struct{})
		seenEdges := make(map[string]struct{})

		for records.Next(ctx) {
			rec := records.Record()

			connID, _, _ := neo4j.GetRecordValue[string](rec, "connected_id")
			connName, _, _ := neo4j.GetRecordValue[string](rec, "connected_name")
			connType, _, _ := neo4j.GetRecordValue[string](rec, "connected_type")
			dist, _, _ := neo4j.GetRecordValue[int64](rec, "dist")
			factID, _, _ := neo4j.GetRecordValue[string](rec, "fact_id")
			sourceID, _, _ := neo4j.GetRecordValue[string](rec, "source_id")
			targetID, _, _ := neo4j.GetRecordValue[string](rec, "target_id")
			relLabel, _, _ := neo4j.GetRecordValue[string](rec, "relation_label")
			factText, _, _ := neo4j.GetRecordValue[string](rec, "fact")

			if connID != "" {
				if _, seen := seenNodes[connID]; !seen {
					seenNodes[connID] = struct{}{}
					res.Nodes = append(res.Nodes, graph.SubgraphNode{
						EntityID:   connID,
						EntityName: connName,
						EntityType: connType,
						Distance:   int(dist),
					})
				}
			}

			if factID != "" {
				if _, seen := seenEdges[factID]; !seen {
					seenEdges[factID] = struct{}{}
					res.Edges = append(res.Edges, graph.SubgraphEdge{
						FactID:         factID,
						SourceEntityID: sourceID,
						TargetEntityID: targetID,
						RelationType:   relLabel,
						Fact:           factText,
					})
				}
			}
		}
		if collectErr := records.Err(); collectErr != nil {
			return nil, collectErr
		}
		return res, nil
	})
	if err != nil {
		return graph.SubgraphResult{SeedEntityID: entityID}, fmt.Errorf("memgraph get subgraph %s: %w", entityID, err)
	}

	res, _ := result.(graph.SubgraphResult)
	return res, nil
}

// GetCommunitiesForEntity returns the community IDs that the given entity belongs to.
// The community_id property is written by RunCommunityDetection.
// Returns an empty slice (not an error) when the entity has no community_id property.
func (g *GraphAdapter) GetCommunitiesForEntity(ctx context.Context, entityID string) ([]string, error) {
	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
			MATCH (e:Entity {uuid: $entity_id})
			WHERE e.community_id IS NOT NULL
			RETURN e.community_id AS community_id
		`
		records, runErr := tx.Run(ctx, cypher, map[string]any{"entity_id": entityID})
		if runErr != nil {
			return nil, runErr
		}
		var ids []string
		for records.Next(ctx) {
			rec := records.Record()
			cid, _, _ := neo4j.GetRecordValue[int64](rec, "community_id")
			ids = append(ids, fmt.Sprintf("%d", cid))
		}
		if recErr := records.Err(); recErr != nil {
			return nil, recErr
		}
		return ids, nil
	})
	if err != nil {
		return nil, fmt.Errorf("memgraph get communities for entity %s: %w", entityID, err)
	}
	ids, _ := result.([]string)
	return ids, nil
}

// RunCommunityDetection calls MAGE's community_detection algorithm and writes
// the community_id property back to each Entity node.
// Returns ErrMAGENotAvailable when MAGE is not installed.
// This method is intended for periodic offline use; it is not called automatically.
func (g *GraphAdapter) RunCommunityDetection(ctx context.Context) error {
	if !g.mageAvailable {
		return ErrMAGENotAvailable
	}

	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	// Step 1: run community detection and collect (node_id, community_id) pairs.
	type pair struct {
		nodeID      string
		communityID int64
	}

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		records, runErr := tx.Run(ctx,
			`CALL community_detection.get() YIELD node, community_id
			 WHERE node:Entity
			 RETURN node.uuid AS uuid, community_id`,
			nil,
		)
		if runErr != nil {
			return nil, runErr
		}
		var pairs []pair
		for records.Next(ctx) {
			rec := records.Record()
			uuid, _, _ := neo4j.GetRecordValue[string](rec, "uuid")
			cid, _, _ := neo4j.GetRecordValue[int64](rec, "community_id")
			if uuid != "" {
				pairs = append(pairs, pair{nodeID: uuid, communityID: cid})
			}
		}
		if recErr := records.Err(); recErr != nil {
			return nil, recErr
		}
		return pairs, nil
	})
	if err != nil {
		return fmt.Errorf("memgraph run community detection (read): %w", err)
	}

	pairs, _ := result.([]pair)
	if len(pairs) == 0 {
		g.store.logger.Info("community detection: no Entity nodes found, nothing to update")
		return nil
	}

	// Step 2: write community_id back to Entity nodes in batches of 100.
	const batchSize = 100
	total := len(pairs)
	for start := 0; start < total; start += batchSize {
		end := start + batchSize
		if end > total {
			end = total
		}
		batch := pairs[start:end]

		rows := make([]map[string]any, len(batch))
		for i := range batch {
			rows[i] = map[string]any{
				"uuid":         batch[i].nodeID,
				"community_id": batch[i].communityID,
			}
		}

		_, writeErr := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			_, runErr := tx.Run(ctx,
				`UNWIND $rows AS row
				 MATCH (e:Entity {uuid: row.uuid})
				 SET e.community_id = row.community_id`,
				map[string]any{"rows": rows},
			)
			return nil, runErr
		})
		if writeErr != nil {
			return fmt.Errorf("memgraph run community detection (write batch %d-%d): %w", start, end, writeErr)
		}

		g.store.logger.Info("community detection: batch written",
			"start", start, "end", end, "total", total)
	}

	g.store.logger.Info("community detection complete", "entities_updated", total)
	return nil
}

// GetMemoriesForCommunity returns the memory IDs associated with all entities
// in the given community (identified by community_id property).
// Returns ErrMAGENotAvailable when MAGE is not installed.
func (g *GraphAdapter) GetMemoriesForCommunity(ctx context.Context, communityID string) ([]string, error) {
	if !g.mageAvailable {
		return nil, ErrMAGENotAvailable
	}

	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
			MATCH (e:Entity {community_id: $cid})
			RETURN e.memory_ids AS memory_ids
		`
		records, runErr := tx.Run(ctx, cypher, map[string]any{"cid": communityID})
		if runErr != nil {
			return nil, runErr
		}

		seen := make(map[string]struct{})
		var memIDs []string
		for records.Next(ctx) {
			rec := records.Record()
			ids := getStringSlice(rec, "memory_ids")
			for _, id := range ids {
				if _, exists := seen[id]; !exists {
					seen[id] = struct{}{}
					memIDs = append(memIDs, id)
				}
			}
		}
		if recErr := records.Err(); recErr != nil {
			return nil, recErr
		}
		return memIDs, nil
	})
	if err != nil {
		return nil, fmt.Errorf("memgraph get memories for community %s: %w", communityID, err)
	}
	ids, _ := result.([]string)
	return ids, nil
}

// MigrateRelTypesToLabels re-creates all legacy RELATES_TO edges with their correct
// typed label (derived from the relation_type property), then deletes the old edges.
// This is a one-time migration; it is NOT called automatically.
// Run via a dedicated CLI command after upgrading to Phase C.
//
// Strategy per edge:
//  1. Read the relation_type property from the existing RELATES_TO edge.
//  2. Derive the typed label via validateAndNormalizeRelType.
//  3. If the label is still RELATES_TO (either truly a fallback, or already migrated),
//     skip — no work needed.
//  4. Otherwise, create a new typed edge with all the same properties, then delete
//     the old RELATES_TO edge.
//
// Processes edges in batches of 100 for memory efficiency. Progress is logged.
func (g *GraphAdapter) MigrateRelTypesToLabels(ctx context.Context) error {
	const batchSize = 100

	// Step 1: collect all RELATES_TO edges that need migration.
	type edgeRow struct {
		uuid           string
		sourceID       string
		targetID       string
		relationType   string
		fact           string
		factEmbedding  []float32
		createdAt      string
		expiredAt      string
		validAt        string
		invalidAt      string
		sourceMemoryIDs []string
		episodes       []string
		confidence     float64
	}

	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		records, runErr := tx.Run(ctx, `
			MATCH (s:Entity)-[r:RELATES_TO]->(t:Entity)
			RETURN r.uuid AS uuid, s.uuid AS source_id, t.uuid AS target_id,
			       r.relation_type AS relation_type, r.fact AS fact,
			       r.fact_embedding AS fact_embedding,
			       r.created_at AS created_at, r.expired_at AS expired_at,
			       r.valid_at AS valid_at, r.invalid_at AS invalid_at,
			       r.source_memory_ids AS source_memory_ids,
			       r.episodes AS episodes, r.confidence AS confidence
		`, nil)
		if runErr != nil {
			return nil, runErr
		}

		var rows []edgeRow
		for records.Next(ctx) {
			rec := records.Record()
			uuid, _, _ := neo4j.GetRecordValue[string](rec, "uuid")
			sourceID, _, _ := neo4j.GetRecordValue[string](rec, "source_id")
			targetID, _, _ := neo4j.GetRecordValue[string](rec, "target_id")
			relType, _, _ := neo4j.GetRecordValue[string](rec, "relation_type")
			factText, _, _ := neo4j.GetRecordValue[string](rec, "fact")
			createdAt, _, _ := neo4j.GetRecordValue[string](rec, "created_at")
			expiredAt, _, _ := neo4j.GetRecordValue[string](rec, "expired_at")
			validAt, _, _ := neo4j.GetRecordValue[string](rec, "valid_at")
			invalidAt, _, _ := neo4j.GetRecordValue[string](rec, "invalid_at")
			confidence, _, _ := neo4j.GetRecordValue[float64](rec, "confidence")

			rows = append(rows, edgeRow{
				uuid:            uuid,
				sourceID:        sourceID,
				targetID:        targetID,
				relationType:    relType,
				fact:            factText,
				factEmbedding:   getFloat32Slice(rec, "fact_embedding"),
				createdAt:       createdAt,
				expiredAt:       expiredAt,
				validAt:         validAt,
				invalidAt:       invalidAt,
				sourceMemoryIDs: getStringSlice(rec, "source_memory_ids"),
				episodes:        getStringSlice(rec, "episodes"),
				confidence:      confidence,
			})
		}
		if recErr := records.Err(); recErr != nil {
			return nil, recErr
		}
		return rows, nil
	})
	if err != nil {
		return fmt.Errorf("migrate rel types: read RELATES_TO edges: %w", err)
	}

	rows, _ := result.([]edgeRow)
	total := len(rows)
	g.store.logger.Info("migrate rel types: found RELATES_TO edges", "count", total)

	migrated := 0
	skipped := 0

	for start := 0; start < total; start += batchSize {
		end := start + batchSize
		if end > total {
			end = total
		}
		batch := rows[start:end]

		for i := range batch {
			row := batch[i]
			newLabel := validateAndNormalizeRelType(row.relationType)
			if newLabel == string(models.RelTypeRelatesTo) {
				// Edge is already RELATES_TO (either a genuine fallback or already migrated).
				skipped++
				continue
			}

			// Create a new typed edge and delete the old RELATES_TO edge in one write tx.
			_, writeErr := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
				// Build typed-label MERGE Cypher — newLabel is from the whitelist only.
				createCypher := fmt.Sprintf(`
					MATCH (s:Entity {uuid: $source_id})
					MATCH (t:Entity {uuid: $target_id})
					MERGE (s)-[rNew:%s {uuid: $uuid}]->(t)
					SET rNew.relation_type     = $relation_type,
					    rNew.fact              = $fact,
					    rNew.fact_embedding    = $fact_embedding,
					    rNew.created_at        = $created_at,
					    rNew.expired_at        = $expired_at,
					    rNew.valid_at          = $valid_at,
					    rNew.invalid_at        = $invalid_at,
					    rNew.source_memory_ids = $source_memory_ids,
					    rNew.episodes          = $episodes,
					    rNew.confidence        = $confidence
					WITH s, t
					MATCH (s)-[rOld:RELATES_TO {uuid: $uuid}]->(t)
					DELETE rOld
				`, newLabel)

				params := map[string]any{
					"uuid":              row.uuid,
					"source_id":         row.sourceID,
					"target_id":         row.targetID,
					"relation_type":     row.relationType,
					"fact":              row.fact,
					"fact_embedding":    row.factEmbedding,
					"created_at":        row.createdAt,
					"expired_at":        row.expiredAt,
					"valid_at":          row.validAt,
					"invalid_at":        row.invalidAt,
					"source_memory_ids": row.sourceMemoryIDs,
					"episodes":          row.episodes,
					"confidence":        row.confidence,
				}

				_, runErr := tx.Run(ctx, createCypher, params)
				return nil, runErr
			})
			if writeErr != nil {
				return fmt.Errorf("migrate rel types: write edge %s (%s→%s): %w",
					row.uuid, newLabel, string(models.RelTypeRelatesTo), writeErr)
			}
			migrated++
		}

		g.store.logger.Info("migrate rel types: batch complete",
			"start", start, "end", end, "migrated_so_far", migrated, "skipped_so_far", skipped)
	}

	g.store.logger.Info("migrate rel types complete",
		"total", total, "migrated", migrated, "skipped", skipped)
	return nil
}
