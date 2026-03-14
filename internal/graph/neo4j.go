package graph

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Neo4jClient implements Client using a Neo4j Bolt driver.
type Neo4jClient struct {
	driver   neo4j.DriverWithContext
	database string
	logger   *slog.Logger
}

// NewNeo4jClient creates a new Neo4jClient connected to the given Neo4j instance.
func NewNeo4jClient(ctx context.Context, uri, username, password, database string, logger *slog.Logger) (*Neo4jClient, error) {
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return nil, fmt.Errorf("neo4j new driver: %w", err)
	}

	if verifyErr := driver.VerifyConnectivity(ctx); verifyErr != nil {
		closeErr := driver.Close(ctx)
		if closeErr != nil {
			logger.Error("failed to close driver after connectivity check failure", "error", closeErr)
		}
		return nil, fmt.Errorf("neo4j verify connectivity: %w", verifyErr)
	}

	return &Neo4jClient{
		driver:   driver,
		database: database,
		logger:   logger,
	}, nil
}

func (c *Neo4jClient) sessionConfig() neo4j.SessionConfig {
	return neo4j.SessionConfig{
		DatabaseName: c.database,
	}
}

// closeSession closes a Neo4j session and logs any error.
func (c *Neo4jClient) closeSession(ctx context.Context, session neo4j.SessionWithContext) {
	if closeErr := session.Close(ctx); closeErr != nil {
		c.logger.Warn("neo4j session close failed", "error", closeErr)
	}
}

// EnsureSchema creates range indexes, fulltext indexes, and constraints.
func (c *Neo4jClient) EnsureSchema(ctx context.Context) error {
	session := c.driver.NewSession(ctx, c.sessionConfig())
	defer c.closeSession(ctx, session)

	queries := []string{
		// Uniqueness constraint on Entity name (prevents duplicate nodes for the same entity)
		"CREATE CONSTRAINT entity_name_unique IF NOT EXISTS FOR (e:Entity) REQUIRE e.name IS UNIQUE",

		// Range indexes on Entity
		"CREATE INDEX entity_uuid IF NOT EXISTS FOR (n:Entity) ON (n.uuid)",
		"CREATE INDEX entity_project IF NOT EXISTS FOR (n:Entity) ON (n.project)",

		// Range index on RELATES_TO relationship
		"CREATE INDEX rel_uuid IF NOT EXISTS FOR ()-[r:RELATES_TO]-() ON (r.uuid)",

		// Temporal range indexes on RELATES_TO
		"CREATE INDEX rel_created_at IF NOT EXISTS FOR ()-[r:RELATES_TO]-() ON (r.created_at)",
		"CREATE INDEX rel_expired_at IF NOT EXISTS FOR ()-[r:RELATES_TO]-() ON (r.expired_at)",

		// Fulltext indexes
		"CREATE FULLTEXT INDEX entity_fulltext IF NOT EXISTS FOR (n:Entity) ON EACH [n.name, n.summary]",
		"CREATE FULLTEXT INDEX fact_fulltext IF NOT EXISTS FOR ()-[r:RELATES_TO]-() ON EACH [r.relation_type, r.fact]",
	}

	for i := range queries {
		_, writeErr := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			_, runErr := tx.Run(ctx, queries[i], nil)
			return nil, runErr
		})
		if writeErr != nil {
			return fmt.Errorf("neo4j ensure schema query %d: %w", i, writeErr)
		}
	}

	c.logger.Info("neo4j schema ensured", "index_count", len(queries))
	return nil
}

// UpsertEntity creates or updates an entity node with a dynamic type label.
func (c *Neo4jClient) UpsertEntity(ctx context.Context, entity models.Entity) error {
	session := c.driver.NewSession(ctx, c.sessionConfig())
	defer c.closeSession(ctx, session)

	typeLabel := entityTypeLabel(entity.Type)

	// MERGE on name so the same entity is never duplicated.
	// ON CREATE: assign a fresh UUID, set immutable fields, and stamp created_at via datetime().
	// ON MATCH: update mutable fields but preserve the original UUID and created_at.
	// updated_at is always refreshed via datetime() so it is never zero.
	cypher := fmt.Sprintf(`
		MERGE (n:Entity {name: $name})
		ON CREATE SET n.uuid = $uuid,
		              n.type = $type,
		              n.project = $project,
		              n.summary = $summary,
		              n.aliases = $aliases,
		              n.memory_ids = $memory_ids,
		              n.name_embedding = $name_embedding,
		              n.community_id = $community_id,
		              n.created_at = datetime(),
		              n.updated_at = datetime()
		ON MATCH SET  n.type = $type,
		              n.project = $project,
		              n.summary = $summary,
		              n.aliases = $aliases,
		              n.memory_ids = $memory_ids,
		              n.name_embedding = $name_embedding,
		              n.community_id = $community_id,
		              n.updated_at = datetime()
		SET n:%s
	`, typeLabel)

	params := map[string]any{
		"uuid":           entity.ID,
		"name":           entity.Name,
		"type":           string(entity.Type),
		"project":        entity.Project,
		"summary":        entity.Summary,
		"aliases":        entity.Aliases,
		"memory_ids":     entity.MemoryIDs,
		"name_embedding": entity.NameEmbedding,
		"community_id":   entity.CommunityID,
	}

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, runErr := tx.Run(ctx, cypher, params)
		return nil, runErr
	})
	if err != nil {
		return fmt.Errorf("neo4j upsert entity %s: %w", entity.ID, err)
	}
	return nil
}

// SearchEntities finds entities by fulltext search, optionally filtered by project.
func (c *Neo4jClient) SearchEntities(ctx context.Context, query string, _ []float32, project string, limit int) ([]EntityResult, error) {
	session := c.driver.NewSession(ctx, c.sessionConfig())
	defer c.closeSession(ctx, session)

	var cypher string
	params := map[string]any{
		"limit": int64(limit),
	}

	if query != "" {
		cypher = `
			CALL db.index.fulltext.queryNodes("entity_fulltext", $query, {limit: $limit})
			YIELD node, score
		`
		params["query"] = query

		if project != "" {
			cypher += `WHERE node.project = $project`
			params["project"] = project
		}
		cypher += `
			RETURN node.uuid AS id, node.name AS name, node.type AS type, score
		`
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

		var results []EntityResult
		for records.Next(ctx) {
			record := records.Record()
			id, _, _ := neo4j.GetRecordValue[string](record, "id")
			name, _, _ := neo4j.GetRecordValue[string](record, "name")
			entityType, _, _ := neo4j.GetRecordValue[string](record, "type")
			score, _, _ := neo4j.GetRecordValue[float64](record, "score")

			results = append(results, EntityResult{
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
		return nil, fmt.Errorf("neo4j search entities: %w", err)
	}

	results, _ := result.([]EntityResult)
	return results, nil
}

// GetEntity retrieves a single entity by ID.
func (c *Neo4jClient) GetEntity(ctx context.Context, id string) (*models.Entity, error) {
	session := c.driver.NewSession(ctx, c.sessionConfig())
	defer c.closeSession(ctx, session)

	cypher := `
		MATCH (n:Entity {uuid: $uuid})
		RETURN n.uuid AS uuid, n.name AS name, n.type AS type, n.project AS project,
		       n.summary AS summary, n.aliases AS aliases, n.memory_ids AS memory_ids,
		       n.name_embedding AS name_embedding, n.community_id AS community_id,
		       n.created_at AS created_at, n.updated_at AS updated_at
	`

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		records, runErr := tx.Run(ctx, cypher, map[string]any{"uuid": id})
		if runErr != nil {
			return nil, runErr
		}

		if !records.Next(ctx) {
			if nextErr := records.Err(); nextErr != nil {
				return nil, nextErr
			}
			return nil, nil
		}

		record := records.Record()
		entity := recordToEntity(record)
		return &entity, nil
	})
	if err != nil {
		return nil, fmt.Errorf("neo4j get entity %s: %w", id, err)
	}

	entity, _ := result.(*models.Entity)
	if entity == nil {
		return nil, fmt.Errorf("neo4j entity %s not found", id)
	}
	return entity, nil
}

// UpsertFact creates or updates a RELATES_TO relationship between two entities.
func (c *Neo4jClient) UpsertFact(ctx context.Context, fact models.Fact) error {
	session := c.driver.NewSession(ctx, c.sessionConfig())
	defer c.closeSession(ctx, session)

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

	// Bi-temporal nullable fields
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
		return fmt.Errorf("neo4j upsert fact %s: %w", fact.ID, err)
	}
	return nil
}

// SearchFacts performs a basic BM25 fulltext search over RELATES_TO relationships.
// The full hybrid search with RRF will be added in a later task (search.go).
func (c *Neo4jClient) SearchFacts(ctx context.Context, query string, _ []float32, limit int) ([]FactResult, error) {
	session := c.driver.NewSession(ctx, c.sessionConfig())
	defer c.closeSession(ctx, session)

	cypher := `
		CALL db.index.fulltext.queryRelationships("fact_fulltext", $query, {limit: $limit})
		YIELD relationship, score
		WITH relationship AS r, score
		MATCH (s:Entity)-[r]->(t:Entity)
		WHERE r.expired_at IS NULL
		RETURN r.uuid AS uuid, r.fact AS fact, s.uuid AS source_entity_id,
		       t.uuid AS target_entity_id, r.source_memory_ids AS source_memory_ids, score
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

		var results []FactResult
		for records.Next(ctx) {
			record := records.Record()
			id, _, _ := neo4j.GetRecordValue[string](record, "uuid")
			fact, _, _ := neo4j.GetRecordValue[string](record, "fact")
			sourceID, _, _ := neo4j.GetRecordValue[string](record, "source_entity_id")
			targetID, _, _ := neo4j.GetRecordValue[string](record, "target_entity_id")
			score, _, _ := neo4j.GetRecordValue[float64](record, "score")

			memoryIDs := getStringSlice(record, "source_memory_ids")

			results = append(results, FactResult{
				ID:              id,
				Fact:            fact,
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
		return nil, fmt.Errorf("neo4j search facts: %w", err)
	}

	results, _ := result.([]FactResult)
	return results, nil
}

// InvalidateFact sets ExpiredAt and InvalidAt on a RELATES_TO relationship.
func (c *Neo4jClient) InvalidateFact(ctx context.Context, id string, expiredAt, invalidAt time.Time) error {
	session := c.driver.NewSession(ctx, c.sessionConfig())
	defer c.closeSession(ctx, session)

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
		return fmt.Errorf("neo4j invalidate fact %s: %w", id, err)
	}
	return nil
}

// GetFactsBetween returns all active facts between two entities.
func (c *Neo4jClient) GetFactsBetween(ctx context.Context, sourceID, targetID string) ([]models.Fact, error) {
	session := c.driver.NewSession(ctx, c.sessionConfig())
	defer c.closeSession(ctx, session)

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
		return nil, fmt.Errorf("neo4j get facts between %s and %s: %w", sourceID, targetID, err)
	}

	facts, _ := result.([]models.Fact)
	return facts, nil
}

// GetFactsForEntity returns all active facts involving an entity (as source or target).
func (c *Neo4jClient) GetFactsForEntity(ctx context.Context, entityID string) ([]models.Fact, error) {
	session := c.driver.NewSession(ctx, c.sessionConfig())
	defer c.closeSession(ctx, session)

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
		return nil, fmt.Errorf("neo4j get facts for entity %s: %w", entityID, err)
	}

	facts, _ := result.([]models.Fact)
	return facts, nil
}

// AppendEpisode adds an episode/session ID to a fact's episodes list.
func (c *Neo4jClient) AppendEpisode(ctx context.Context, factID, episodeID string) error {
	session := c.driver.NewSession(ctx, c.sessionConfig())
	defer c.closeSession(ctx, session)

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
		return fmt.Errorf("neo4j append episode to fact %s: %w", factID, err)
	}
	return nil
}

// AppendMemoryToFact adds a memory ID to a fact's source_memory_ids list.
func (c *Neo4jClient) AppendMemoryToFact(ctx context.Context, factID, memoryID string) error {
	session := c.driver.NewSession(ctx, c.sessionConfig())
	defer c.closeSession(ctx, session)

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
		return fmt.Errorf("neo4j append memory to fact %s: %w", factID, err)
	}
	return nil
}

// GetMemoryFacts returns all facts derived from a given memory ID.
func (c *Neo4jClient) GetMemoryFacts(ctx context.Context, memoryID string) ([]models.Fact, error) {
	session := c.driver.NewSession(ctx, c.sessionConfig())
	defer c.closeSession(ctx, session)

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
		return nil, fmt.Errorf("neo4j get memory facts for %s: %w", memoryID, err)
	}

	facts, _ := result.([]models.Fact)
	return facts, nil
}

// RecallByGraph returns memory IDs relevant to a query via fact search and extraction.
func (c *Neo4jClient) RecallByGraph(ctx context.Context, query string, embedding []float32, limit int) ([]string, error) {
	facts, err := c.SearchFacts(ctx, query, embedding, limit)
	if err != nil {
		return nil, fmt.Errorf("neo4j recall by graph: %w", err)
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

// Healthy returns true if the Neo4j database is reachable.
func (c *Neo4jClient) Healthy(ctx context.Context) bool {
	err := c.driver.VerifyConnectivity(ctx)
	if err != nil {
		c.logger.Warn("neo4j health check failed", "error", err)
		return false
	}
	return true
}

// Close releases the Neo4j driver resources.
func (c *Neo4jClient) Close() error {
	ctx := context.Background()
	if err := c.driver.Close(ctx); err != nil {
		return fmt.Errorf("neo4j close: %w", err)
	}
	return nil
}

// entityTypeLabel returns the Neo4j label for the entity type.
// The label is title-cased to match Neo4j naming conventions.
// Unknown types fall back to "Entity" to prevent Cypher injection.
func entityTypeLabel(t models.EntityType) string {
	if !t.IsValid() {
		return "Entity"
	}
	s := string(t)
	return strings.ToUpper(s[:1]) + s[1:]
}

// collectFacts runs a Cypher query and collects the results into a slice of Fact.
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

// recordToEntity converts a Neo4j record to a models.Entity.
func recordToEntity(record *neo4j.Record) models.Entity {
	id, _, _ := neo4j.GetRecordValue[string](record, "uuid")
	name, _, _ := neo4j.GetRecordValue[string](record, "name")
	entityType, _, _ := neo4j.GetRecordValue[string](record, "type")
	project, _, _ := neo4j.GetRecordValue[string](record, "project")
	summary, _, _ := neo4j.GetRecordValue[string](record, "summary")
	communityID, _, _ := neo4j.GetRecordValue[string](record, "community_id")
	createdAtStr, _, _ := neo4j.GetRecordValue[string](record, "created_at")
	updatedAtStr, _, _ := neo4j.GetRecordValue[string](record, "updated_at")

	aliases := getStringSlice(record, "aliases")
	memoryIDs := getStringSlice(record, "memory_ids")
	nameEmbedding := getFloat32Slice(record, "name_embedding")

	createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)
	updatedAt, _ := time.Parse(time.RFC3339Nano, updatedAtStr)

	return models.Entity{
		ID:            id,
		Name:          name,
		Type:          models.EntityType(entityType),
		Project:       project,
		Summary:       summary,
		Aliases:       aliases,
		MemoryIDs:     memoryIDs,
		NameEmbedding: nameEmbedding,
		CommunityID:   communityID,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}
}

// recordToFact converts a Neo4j record to a models.Fact.
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

// getStringSlice extracts a []string from a Neo4j record field.
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

// getFloat32Slice extracts a []float32 from a Neo4j record field.
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

// Compile-time interface assertion.
var _ Client = (*Neo4jClient)(nil)
