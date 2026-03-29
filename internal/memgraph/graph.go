package memgraph

import (
	"context"
	"fmt"
	"log/slog"
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
	store   *MemgraphStore
	mu      sync.RWMutex      // protects embeddr
	embeddr embedder.Embedder // optional; enables semantic fact embedding in UpsertFact
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

// ParseVectorIndexRows converts raw SHOW VECTOR INDEXES row data (a slice of
// maps with at least "index_name" and "property_name" keys) into the
// indexName → propertyName map used by showVectorIndexes.
// Exported so tests/ can exercise the parsing logic without a live session.
func ParseVectorIndexRows(rows []map[string]any) map[string]string {
	indexes := make(map[string]string)
	for _, row := range rows {
		name, _ := row["index_name"].(string)
		prop, _ := row["property_name"].(string)
		if name != "" {
			indexes[name] = prop
		}
	}
	return indexes
}

// showVectorIndexes runs SHOW VECTOR INDEXES and returns a map of
// indexName → propertyName for all existing vector indexes.
func showVectorIndexes(ctx context.Context, session neo4j.SessionWithContext) (map[string]string, error) {
	result, err := session.Run(ctx, "SHOW VECTOR INDEXES", nil)
	if err != nil {
		return nil, fmt.Errorf("show vector indexes: %w", err)
	}

	var rows []map[string]any
	for result.Next(ctx) {
		record := result.Record()
		row := make(map[string]any)
		if nameVal, ok := record.Get("index_name"); ok {
			row["index_name"] = nameVal
		}
		if propVal, ok := record.Get("property_name"); ok {
			row["property_name"] = propVal
		}
		rows = append(rows, row)
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("show vector indexes: consuming results: %w", err)
	}
	return ParseVectorIndexRows(rows), nil
}

// verifyOrRebuildVectorIndex checks whether the named vector index exists and is
// on the expected property. Three outcomes:
//   - Index absent: the DDL is executed to create it.
//   - Index present with correct property: nothing to do, returns nil.
//   - Index present with wrong property: logs a Warn, drops the index, then recreates it.
func verifyOrRebuildVectorIndex(ctx context.Context, session neo4j.SessionWithContext, logger *slog.Logger, indexName, expectedProperty, ddl string) error {
	indexes, err := showVectorIndexes(ctx, session)
	if err != nil {
		// Non-fatal for the "show" step — fall through and attempt creation.
		logger.Warn("verifyOrRebuildVectorIndex: could not inspect existing indexes, will attempt creation",
			"index", indexName, "error", err)
		indexes = make(map[string]string)
	}

	existingProp, exists := indexes[indexName]
	if !exists {
		// Index does not exist — create it.
		result, runErr := session.Run(ctx, ddl, nil)
		if runErr != nil {
			return fmt.Errorf("verifyOrRebuildVectorIndex: create %s: %w", indexName, runErr)
		}
		if result != nil {
			_, _ = result.Consume(ctx)
		}
		return nil
	}

	if existingProp == expectedProperty {
		// Index exists on the correct property — nothing to do.
		return nil
	}

	// Index exists but is on the wrong property — drop and recreate.
	logger.Warn("vector index is on wrong property, dropping and recreating",
		"index", indexName,
		"expected_property", expectedProperty,
		"actual_property", existingProp,
	)

	dropDDL := fmt.Sprintf("DROP VECTOR INDEX %s", indexName)
	dropResult, dropErr := session.Run(ctx, dropDDL, nil)
	if dropErr != nil {
		return fmt.Errorf("verifyOrRebuildVectorIndex: drop %s: %w", indexName, dropErr)
	}
	if dropResult != nil {
		_, _ = dropResult.Consume(ctx)
	}

	recreateResult, recreateErr := session.Run(ctx, ddl, nil)
	if recreateErr != nil {
		return fmt.Errorf("verifyOrRebuildVectorIndex: recreate %s: %w", indexName, recreateErr)
	}
	if recreateResult != nil {
		_, _ = recreateResult.Consume(ctx)
	}

	logger.Info("vector index rebuilt on correct property",
		"index", indexName, "property", expectedProperty)
	return nil
}

// vectorIndexSpec pairs an index name with its expected property for verification.
type vectorIndexSpec struct {
	name     string
	property string
	ddl      string
}

// EnsureSchema creates indexes, constraints, and vector indexes on Memgraph.
// Memgraph does not support IF NOT EXISTS on constraints, so "already exists" errors
// are caught and logged as warnings.
// For vector indexes, EnsureSchema additionally validates that the existing index is
// on the correct property — if not, it drops and recreates the index to prevent
// silent garbage results from mismatched index definitions.
func (g *GraphAdapter) EnsureSchema(ctx context.Context, vectorDim int) error {
	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	memoryVectorDDL := BuildMemoryVectorIndexDDL(vectorDim)
	entityVectorDDL := BuildEntityVectorIndexDDL(vectorDim)

	// Vector indexes require property verification — handled separately below.
	vectorIndexes := []vectorIndexSpec{
		{name: "memory_embedding", property: "embedding", ddl: memoryVectorDDL},
		{name: "entity_name_embedding", property: "name_embedding", ddl: entityVectorDDL},
	}

	// Non-vector DDL: constraints, property indexes, and text indexes.
	// "already exists" errors are silently skipped (Memgraph has no IF NOT EXISTS).
	otherQueries := []string{
		// Uniqueness constraints — Memgraph-specific DDL (no IF NOT EXISTS)
		"CREATE CONSTRAINT ON (m:Memory) ASSERT m.uuid IS UNIQUE",
		"CREATE CONSTRAINT ON (e:Entity) ASSERT e.name IS UNIQUE",

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

	for i := range otherQueries {
		// Memgraph requires auto-commit (implicit) transactions for DDL.
		// session.Run() executes as an auto-commit transaction.
		result, runErr := session.Run(ctx, otherQueries[i], nil)
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

	// Verify (and if needed, rebuild) each vector index on the expected property.
	for i := range vectorIndexes {
		spec := vectorIndexes[i]
		if err := verifyOrRebuildVectorIndex(ctx, session, g.store.logger, spec.name, spec.property, spec.ddl); err != nil {
			return fmt.Errorf("memgraph ensure schema: vector index %s: %w", spec.name, err)
		}
	}

	g.store.logger.Info("memgraph schema ensured",
		"non_vector_query_count", len(otherQueries),
		"vector_index_count", len(vectorIndexes),
	)
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

	cypher := `
		MATCH (s:Entity {uuid: $source_id})
		MATCH (t:Entity {uuid: $target_id})
		MERGE (s)-[r:RELATES_TO {uuid: $uuid}]->(t)
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
	`

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
		cypher = `
			MATCH (s:Entity)-[r:RELATES_TO]->(t:Entity)
			WHERE r.expired_at IS NULL
			RETURN r.uuid AS uuid, r.fact AS fact, s.uuid AS source_entity_id,
			       t.uuid AS target_entity_id, r.source_memory_ids AS source_memory_ids,
			       r.fact_embedding AS fact_embedding
			LIMIT $limit
		`
	} else {
		cypher = `
			MATCH (s:Entity)-[r:RELATES_TO]->(t:Entity)
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

	cypher := `
		MATCH ()-[r:RELATES_TO {uuid: $uuid}]->()
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

	cypher := `
		MATCH (n:Entity)-[r:RELATES_TO]-(m:Entity)
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

	cypher := `
		MATCH (n:Entity {uuid: $entity_id})-[r:RELATES_TO]-(m:Entity)
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

	cypher := `
		MATCH ()-[r:RELATES_TO {uuid: $uuid}]->()
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

	cypher := `
		MATCH ()-[r:RELATES_TO {uuid: $uuid}]->()
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

	cypher := `
		MATCH (s:Entity)-[r:RELATES_TO]->(t:Entity)
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

	// Traverse both outgoing and incoming RELATES_TO edges from seed entities.
	// expired_at IS NULL filters out invalidated (soft-deleted) facts.
	cypher := `
		UNWIND $entity_ids AS eid
		MATCH (e:Entity {uuid: eid})-[r:RELATES_TO]-(neighbor:Entity)
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
