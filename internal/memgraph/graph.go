package memgraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// GraphAdapter wraps MemgraphStore and implements graph.Client.
// It is needed because graph.Client.SearchEntities has a different signature than
// store.Store.SearchEntities — Go does not allow two methods with the same name but
// different signatures on the same struct, so we use this thin adapter for the graph
// interface. All other methods delegate directly to the underlying MemgraphStore.
type GraphAdapter struct {
	store *MemgraphStore
}

// NewGraphAdapter creates a GraphAdapter that delegates to the given MemgraphStore.
func NewGraphAdapter(s *MemgraphStore) *GraphAdapter {
	return &GraphAdapter{store: s}
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

// EnsureSchema creates indexes, constraints, and vector indexes on Memgraph.
// Memgraph does not support IF NOT EXISTS on constraints, so "already exists" errors
// are caught and logged as warnings.
func (g *GraphAdapter) EnsureSchema(ctx context.Context) error {
	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	queries := []string{
		// Uniqueness constraints — Memgraph-specific DDL (no IF NOT EXISTS)
		"CREATE CONSTRAINT ON (m:Memory) ASSERT m.uuid IS UNIQUE",
		"CREATE CONSTRAINT ON (e:Entity) ASSERT e.name IS UNIQUE",

		// Vector indexes for semantic search
		`CREATE VECTOR INDEX memory_embedding ON :Memory(embedding) WITH CONFIG {"dimension": 768, "metric": "cosine", "capacity": 10000}`,
		`CREATE VECTOR INDEX entity_name_embedding ON :Entity(name_embedding) WITH CONFIG {"dimension": 768, "metric": "cosine", "capacity": 10000}`,

		// Property indexes for filtering
		"CREATE INDEX ON :Memory(type)",
		"CREATE INDEX ON :Memory(scope)",
		"CREATE INDEX ON :Memory(project)",
		"CREATE INDEX ON :Memory(source)",
		"CREATE INDEX ON :Memory(uuid)",
		"CREATE INDEX ON :Entity(uuid)",
		"CREATE INDEX ON :Entity(project)",

		// Text search indexes (label-wide, no field specification needed)
		"CREATE TEXT INDEX entity_text ON :Entity",
		"CREATE TEXT INDEX fact_text ON :RELATES_TO",
	}

	for i := range queries {
		_, writeErr := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			_, runErr := tx.Run(ctx, queries[i], nil)
			return nil, runErr
		})
		if writeErr != nil {
			if isAlreadyExistsErr(writeErr) {
				g.store.logger.Warn("memgraph schema already exists, skipping", "query_index", i, "query", queries[i])
				continue
			}
			return fmt.Errorf("memgraph ensure schema query %d: %w", i, writeErr)
		}
	}

	g.store.logger.Info("memgraph schema ensured", "query_count", len(queries))
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
		cypher = `
			CALL text_search.search("entity_text", $query)
			YIELD node, score
			WHERE ($project = "" OR node.project = $project)
			RETURN node.uuid AS id, node.name AS name, node.type AS type, score
			LIMIT $limit
		`
		params["query"] = query
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
func (g *GraphAdapter) UpsertFact(ctx context.Context, fact models.Fact) error {
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

// SearchFacts performs a text search over RELATES_TO relationships using Memgraph's
// text_search.search_edges procedure.
func (g *GraphAdapter) SearchFacts(ctx context.Context, query string, _ []float32, limit int) ([]graph.FactResult, error) {
	session := g.store.driver.NewSession(ctx, g.store.sessionConfig())
	defer g.store.closeSession(ctx, session)

	cypher := `
		CALL text_search.search_edges("fact_text", $query)
		YIELD edge, score
		WITH edge AS r, score, startNode(edge) AS s, endNode(edge) AS t
		WHERE r.expired_at IS NULL
		RETURN r.uuid AS uuid, r.fact AS fact, s.uuid AS source_entity_id,
		       t.uuid AS target_entity_id, r.source_memory_ids AS source_memory_ids, score
		LIMIT $limit
	`

	params := map[string]any{
		"query": query,
		"limit": int64(limit),
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
			score, _, _ := neo4j.GetRecordValue[float64](record, "score")

			memoryIDs := getStringSlice(record, "source_memory_ids")

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

// RecallByGraph returns memory IDs relevant to a query via fact text search and extraction.
func (g *GraphAdapter) RecallByGraph(ctx context.Context, query string, embedding []float32, limit int) ([]string, error) {
	facts, err := g.SearchFacts(ctx, query, embedding, limit)
	if err != nil {
		return nil, fmt.Errorf("memgraph recall by graph: %w", err)
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
