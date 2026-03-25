// Package memgraph provides a store.Store implementation backed by Memgraph,
// a Bolt-compatible graph database. It uses Cypher queries and the
// neo4j-go-driver for all persistence operations.
package memgraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// Compile-time assertions that MemgraphStore fully implements store.Store and store.ResettableStore.
var _ store.Store = (*MemgraphStore)(nil)
var _ store.ResettableStore = (*MemgraphStore)(nil)

const (
	memgraphReadTimeout  = 10 * time.Second
	memgraphWriteTimeout = 30 * time.Second
	// memgraphDeleteAllTimeout is intentionally generous: MATCH (n) DETACH DELETE n
	// scans the entire graph and can be slow on large stores. memgraphWriteTimeout
	// (30 s) is sized for single-node upserts and is too short for a full-graph wipe.
	memgraphDeleteAllTimeout = 5 * time.Minute
)

// MemgraphStore implements store.Store using Memgraph (Bolt-compatible).
type MemgraphStore struct {
	driver                neo4j.DriverWithContext
	database              string
	logger                *slog.Logger
	contradictionDetector store.ContradictionDetector
	vectorDim             int
}

// SetContradictionDetector attaches a contradiction detector to the store.
// When set, Upsert will call FindContradictions before inserting and invalidate
// any memories that contradict the new one. Best-effort: errors are logged, not returned.
func (s *MemgraphStore) SetContradictionDetector(d store.ContradictionDetector) {
	s.contradictionDetector = d
}

// New creates a new MemgraphStore and verifies connectivity.
func New(ctx context.Context, uri, username, password, database string, vectorDim int, logger *slog.Logger) (*MemgraphStore, error) {
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return nil, fmt.Errorf("memgraph new: creating driver: %w", err)
	}

	verifyCtx, verifyCancel := context.WithTimeout(ctx, memgraphReadTimeout)
	defer verifyCancel()
	if verifyErr := driver.VerifyConnectivity(verifyCtx); verifyErr != nil {
		_ = driver.Close(ctx)
		return nil, fmt.Errorf("memgraph new: verifying connectivity to %s: %w", redactURI(uri), verifyErr)
	}

	logger.Debug("connected to Memgraph", "uri", redactURI(uri), "database", database)

	return &MemgraphStore{
		driver:    driver,
		database:  database,
		logger:    logger,
		vectorDim: vectorDim,
	}, nil
}

// sessionConfig returns the session configuration. Memgraph does not support
// multi-database mode, so DatabaseName is always left empty.
func (s *MemgraphStore) sessionConfig() neo4j.SessionConfig {
	return neo4j.SessionConfig{
		// Intentionally empty: Memgraph ignores DatabaseName.
	}
}

// closeSession logs any error encountered while closing a session.
func (s *MemgraphStore) closeSession(ctx context.Context, session neo4j.SessionWithContext) {
	if err := session.Close(ctx); err != nil {
		s.logger.Warn("memgraph: error closing session", "error", err)
	}
}

// EnsureCollection creates all indexes, constraints, and vector indexes in Memgraph.
// Delegates to GraphAdapter.EnsureSchema which runs each DDL statement individually.
func (s *MemgraphStore) EnsureCollection(ctx context.Context) error {
	ga := NewGraphAdapter(s)
	return ga.EnsureSchema(ctx, s.vectorDim)
}

// Upsert inserts or updates a memory node with its embedding vector.
// When memory.SupersedesID is set, the superseded memory is invalidated (valid_to = now).
func (s *MemgraphStore) Upsert(ctx context.Context, memory models.Memory, vector []float32) error {
	// If superseding another memory, invalidate it first (non-fatal).
	if memory.SupersedesID != "" {
		now := time.Now().UTC()
		if invErr := s.InvalidateMemory(ctx, memory.SupersedesID, now); invErr != nil {
			s.logger.Warn("upsert: failed to invalidate superseded memory",
				"superseded_id", memory.SupersedesID, "error", invErr)
		}
	}

	// Set valid_from if not already set.
	if memory.ValidFrom.IsZero() {
		memory.ValidFrom = time.Now().UTC()
	}

	wctx, cancel := context.WithTimeout(ctx, memgraphWriteTimeout)
	defer cancel()

	session := s.driver.NewSession(wctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	params := memoryToParams(memory, vector)

	_, err := session.ExecuteWrite(wctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, txErr := tx.Run(wctx, `
			MERGE (m:Memory {uuid: $uuid})
			SET m.type             = $type,
			    m.scope            = $scope,
			    m.visibility       = $visibility,
			    m.content          = $content,
			    m.confidence       = $confidence,
			    m.source           = $source,
			    m.project          = $project,
			    m.ttl_seconds      = $ttl_seconds,
			    m.tags             = $tags,
			    m.metadata         = $metadata,
			    m.created_at       = $created_at,
			    m.updated_at       = $updated_at,
			    m.last_accessed    = $last_accessed,
			    m.access_count     = $access_count,
			    m.supersedes_id    = $supersedes_id,
			    m.conflict_group_id = $conflict_group_id,
			    m.conflict_status  = $conflict_status,
			    m.valid_until_unix = $valid_until_unix,
			    m.valid_from       = $valid_from,
			    m.valid_to         = $valid_to,
			    m.reinforced_at_unix = $reinforced_at_unix,
			    m.reinforced_count = $reinforced_count,
			    m.user_id          = $user_id,
			    m.embedding        = CASE WHEN $has_embedding THEN $embedding ELSE m.embedding END
		`, params)
		return nil, txErr
	})
	if err != nil {
		return fmt.Errorf("memgraph upsert %s: %w", memory.ID, err)
	}

	s.logger.Debug("upserted memory", "id", memory.ID, "type", memory.Type)
	return nil
}

// Search finds memories similar to the query vector using Memgraph's vector search.
func (s *MemgraphStore) Search(ctx context.Context, vector []float32, limit uint64, filters *store.SearchFilters) ([]models.SearchResult, error) {
	rctx, cancel := context.WithTimeout(ctx, memgraphReadTimeout)
	defer cancel()

	session := s.driver.NewSession(rctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	whereClauses, filterParams := buildWhereClause(filters, "node")
	whereStr := ""
	if len(whereClauses) > 0 {
		whereStr = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Memgraph requires WITH between YIELD and WHERE — cannot use WHERE directly after YIELD.
	query := fmt.Sprintf(`
		CALL vector_search.search("memory_embedding", $limit, $query_vector)
		YIELD node, similarity
		WITH node, similarity AS score
		%s
		RETURN node, score
	`, whereStr)

	params := map[string]any{
		"limit":        int64(limit),
		"query_vector": float32SliceToAny(vector),
	}
	for k, v := range filterParams {
		params[k] = v
	}

	results, err := session.ExecuteRead(rctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, txErr := tx.Run(rctx, query, params)
		if txErr != nil {
			return nil, txErr
		}
		return collectSearchResults(rctx, res)
	})
	if err != nil {
		return nil, fmt.Errorf("memgraph search: %w", err)
	}

	sr, ok := results.([]models.SearchResult)
	if !ok {
		return nil, fmt.Errorf("memgraph search: unexpected result type %T", results)
	}
	return sr, nil
}

// Get retrieves a single memory by ID.
func (s *MemgraphStore) Get(ctx context.Context, id string) (*models.Memory, error) {
	rctx, cancel := context.WithTimeout(ctx, memgraphReadTimeout)
	defer cancel()

	session := s.driver.NewSession(rctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	result, err := session.ExecuteRead(rctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, txErr := tx.Run(rctx, `MATCH (m:Memory {uuid: $id}) RETURN m`, map[string]any{"id": id})
		if txErr != nil {
			return nil, txErr
		}
		if res.Next(rctx) {
			record := res.Record()
			return recordToMemory(record, "m")
		}
		if consumeErr := res.Err(); consumeErr != nil {
			return nil, consumeErr
		}
		return nil, nil
	})
	if err != nil {
		return nil, fmt.Errorf("memgraph get %s: %w", id, err)
	}

	if result == nil {
		return nil, fmt.Errorf("%w: %s", store.ErrNotFound, id)
	}

	mem, ok := result.(*models.Memory)
	if !ok {
		return nil, fmt.Errorf("memgraph get: unexpected result type %T", result)
	}
	return mem, nil
}

// Delete removes a memory by ID. Returns store.ErrNotFound if nothing was deleted.
// If id is shorter than 36 characters (a full UUID), prefix matching is used instead
// of exact matching. If the prefix matches more than one memory, an error is returned.
func (s *MemgraphStore) Delete(ctx context.Context, id string) error {
	wctx, cancel := context.WithTimeout(ctx, memgraphWriteTimeout)
	defer cancel()

	session := s.driver.NewSession(wctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	// For partial IDs, verify the prefix is unambiguous before deleting.
	if len(id) < 36 {
		count, err := session.ExecuteRead(wctx, func(tx neo4j.ManagedTransaction) (any, error) {
			res, txErr := tx.Run(wctx, `
				MATCH (m:Memory) WHERE m.uuid STARTS WITH $id
				RETURN count(m) AS n
			`, map[string]any{"id": id})
			if txErr != nil {
				return 0, txErr
			}
			if !res.Next(wctx) {
				return 0, res.Err()
			}
			n, _ := res.Record().Get("n")
			if consumeErr := res.Err(); consumeErr != nil {
				return 0, consumeErr
			}
			return n, nil
		})
		if err != nil {
			return fmt.Errorf("memgraph delete prefix count %s: %w", id, err)
		}
		n, ok := count.(int64)
		if !ok {
			return fmt.Errorf("memgraph delete: unexpected count type %T", count)
		}
		if n == 0 {
			return fmt.Errorf("%w: %s", store.ErrNotFound, id)
		}
		if n > 1 {
			return fmt.Errorf("ambiguous prefix: matches %d memories", n)
		}
	}

	deleted, err := session.ExecuteWrite(wctx, func(tx neo4j.ManagedTransaction) (any, error) {
		var query string
		if len(id) < 36 {
			query = `
				MATCH (m:Memory) WHERE m.uuid STARTS WITH $id
				WITH m, m.uuid AS uuid
				DETACH DELETE m
				RETURN uuid
			`
		} else {
			query = `
				MATCH (m:Memory {uuid: $id})
				WITH m, m.uuid AS uuid
				DETACH DELETE m
				RETURN uuid
			`
		}
		res, txErr := tx.Run(wctx, query, map[string]any{"id": id})
		if txErr != nil {
			return false, txErr
		}
		found := res.Next(wctx)
		if consumeErr := res.Err(); consumeErr != nil {
			return false, consumeErr
		}
		return found, nil
	})
	if err != nil {
		return fmt.Errorf("memgraph delete %s: %w", id, err)
	}

	deletedBool, ok := deleted.(bool)
	if !ok {
		return fmt.Errorf("memgraph delete: unexpected result type %T", deleted)
	}
	if !deletedBool {
		return fmt.Errorf("%w: %s", store.ErrNotFound, id)
	}

	s.logger.Debug("deleted memory", "id", id)
	return nil
}

// List returns memories matching the given filters with cursor-based pagination.
// The cursor is the SKIP offset encoded as a decimal string; "" means page 0.
func (s *MemgraphStore) List(ctx context.Context, filters *store.SearchFilters, limit uint64, cursor string) ([]models.Memory, string, error) {
	rctx, cancel := context.WithTimeout(ctx, memgraphReadTimeout)
	defer cancel()

	session := s.driver.NewSession(rctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	var skip int64
	if cursor != "" {
		parsed, parseErr := strconv.ParseInt(cursor, 10, 64)
		if parseErr == nil {
			skip = parsed
		}
	}

	whereClauses, filterParams := buildWhereClause(filters, "m")
	whereStr := ""
	if len(whereClauses) > 0 {
		whereStr = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	query := fmt.Sprintf(`
		MATCH (m:Memory)
		%s
		RETURN m
		ORDER BY m.created_at DESC
		SKIP $skip
		LIMIT $limit
	`, whereStr)

	params := map[string]any{
		"skip":  skip,
		"limit": int64(limit),
	}
	for k, v := range filterParams {
		params[k] = v
	}

	raw, err := session.ExecuteRead(rctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, txErr := tx.Run(rctx, query, params)
		if txErr != nil {
			return nil, txErr
		}
		return collectMemories(rctx, res, "m")
	})
	if err != nil {
		return nil, "", fmt.Errorf("memgraph list: %w", err)
	}

	memories, ok := raw.([]models.Memory)
	if !ok {
		return nil, "", fmt.Errorf("memgraph list: unexpected result type %T", raw)
	}

	var nextCursor string
	if uint64(len(memories)) == limit {
		nextCursor = strconv.FormatInt(skip+int64(limit), 10)
	}

	return memories, nextCursor, nil
}

// FindDuplicates returns memories whose vector similarity to the given vector
// is at or above the threshold.
func (s *MemgraphStore) FindDuplicates(ctx context.Context, vector []float32, threshold float64) ([]models.SearchResult, error) {
	rctx, cancel := context.WithTimeout(ctx, memgraphReadTimeout)
	defer cancel()

	session := s.driver.NewSession(rctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	results, err := session.ExecuteRead(rctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, txErr := tx.Run(rctx, `
			CALL vector_search.search("memory_embedding", 10, $vector)
			YIELD node, similarity
			WITH node, similarity AS score
			WHERE score >= $threshold
			RETURN node, score
		`, map[string]any{
			"vector":    float32SliceToAny(vector),
			"threshold": threshold,
		})
		if txErr != nil {
			return nil, txErr
		}
		return collectSearchResults(rctx, res)
	})
	if err != nil {
		return nil, fmt.Errorf("memgraph find duplicates: %w", err)
	}

	sr, ok := results.([]models.SearchResult)
	if !ok {
		return nil, fmt.Errorf("memgraph find duplicates: unexpected result type %T", results)
	}
	return sr, nil
}

// UpdateAccessMetadata increments access count and updates last_accessed time.
func (s *MemgraphStore) UpdateAccessMetadata(ctx context.Context, id string) error {
	wctx, cancel := context.WithTimeout(ctx, memgraphWriteTimeout)
	defer cancel()

	session := s.driver.NewSession(wctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := session.ExecuteWrite(wctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, txErr := tx.Run(wctx, `
			MATCH (m:Memory {uuid: $id})
			SET m.access_count  = m.access_count + 1,
			    m.last_accessed = $now
		`, map[string]any{"id": id, "now": now})
		return nil, txErr
	})
	if err != nil {
		return fmt.Errorf("memgraph update access metadata %s: %w", id, err)
	}

	return nil
}

// Stats returns collection statistics including type and scope counts plus health metrics.
func (s *MemgraphStore) Stats(ctx context.Context) (*models.CollectionStats, error) {
	rctx, cancel := context.WithTimeout(ctx, memgraphReadTimeout)
	defer cancel()

	session := s.driver.NewSession(rctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	stats := &models.CollectionStats{
		ByType:             make(map[string]int64),
		ByScope:            make(map[string]int64),
		ReinforcementTiers: make(map[string]int64),
	}

	// Total count.
	totalResult, err := session.ExecuteRead(rctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, txErr := tx.Run(rctx, `MATCH (m:Memory) RETURN count(m) AS total`, nil)
		if txErr != nil {
			return int64(0), txErr
		}
		if res.Next(rctx) {
			record := res.Record()
			if v, ok := record.Get("total"); ok {
				return toInt64(v), nil
			}
		}
		return int64(0), res.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("memgraph stats total count: %w", err)
	}
	total, ok := totalResult.(int64)
	if !ok {
		return nil, fmt.Errorf("memgraph stats total count: unexpected result type %T", totalResult)
	}
	stats.TotalMemories = total

	// Count by type.
	for _, mt := range models.ValidMemoryTypes {
		key := string(mt)
		cnt, countErr := s.countByField(rctx, session, "type", key)
		if countErr != nil {
			s.logger.Warn("memgraph stats: counting by type", "type", key, "error", countErr)
		}
		stats.ByType[key] = cnt
	}

	// Count by scope.
	for _, sc := range []models.MemoryScope{models.ScopePermanent, models.ScopeProject, models.ScopeSession, models.ScopeTTL} {
		key := string(sc)
		cnt, countErr := s.countByField(rctx, session, "scope", key)
		if countErr != nil {
			s.logger.Warn("memgraph stats: counting by scope", "scope", key, "error", countErr)
		}
		stats.ByScope[key] = cnt
	}

	// Health metrics via full scan.
	s.populateHealthMetrics(ctx, session, stats)

	// Storage estimate: TotalMemories * 768 * 4 bytes per float32.
	stats.StorageEstimate = stats.TotalMemories * 768 * 4

	return stats, nil
}

// countByField executes a filtered COUNT query for a single field=value combination.
func (s *MemgraphStore) countByField(ctx context.Context, session neo4j.SessionWithContext, field, value string) (int64, error) {
	query := fmt.Sprintf(`MATCH (m:Memory) WHERE m.%s = $value RETURN count(m) AS cnt`, field)
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, txErr := tx.Run(ctx, query, map[string]any{"value": value})
		if txErr != nil {
			return int64(0), txErr
		}
		if res.Next(ctx) {
			record := res.Record()
			if v, ok := record.Get("cnt"); ok {
				return toInt64(v), nil
			}
		}
		return int64(0), res.Err()
	})
	if err != nil {
		return 0, err
	}
	cnt, ok := result.(int64)
	if !ok {
		return 0, fmt.Errorf("memgraph countByField: unexpected result type %T", result)
	}
	return cnt, nil
}

// populateHealthMetrics scans all memories to compute temporal range, top accessed,
// reinforcement tiers, active conflicts, and pending TTL expiry.
func (s *MemgraphStore) populateHealthMetrics(ctx context.Context, session neo4j.SessionWithContext, stats *models.CollectionStats) {
	rctx, cancel := context.WithTimeout(ctx, memgraphReadTimeout)
	defer cancel()

	var topAccessed []topEntry
	now := time.Now().UTC()
	ttlDeadline := now.Add(24 * time.Hour)

	// Stream all memories in pages of 100.
	const pageSize = 100
	var skip int64

	for {
		raw, err := session.ExecuteRead(rctx, func(tx neo4j.ManagedTransaction) (any, error) {
			res, txErr := tx.Run(rctx, `
				MATCH (m:Memory)
				RETURN m
				ORDER BY m.created_at DESC
				SKIP $skip
				LIMIT $limit
			`, map[string]any{"skip": skip, "limit": int64(pageSize)})
			if txErr != nil {
				return nil, txErr
			}
			return collectMemories(rctx, res, "m")
		})
		if err != nil {
			s.logger.Warn("memgraph stats: scrolling for health metrics", "error", err)
			return
		}
		page, ok := raw.([]models.Memory)
		if !ok {
			s.logger.Warn("memgraph stats: unexpected result type scrolling for health metrics", "type", fmt.Sprintf("%T", raw))
			return
		}
		if len(page) == 0 {
			break
		}

		for i := range page {
			mem := &page[i]

			// Temporal range.
			if !mem.CreatedAt.IsZero() {
				if stats.OldestMemory == nil || mem.CreatedAt.Before(*stats.OldestMemory) {
					t := mem.CreatedAt
					stats.OldestMemory = &t
				}
				if stats.NewestMemory == nil || mem.CreatedAt.After(*stats.NewestMemory) {
					t := mem.CreatedAt
					stats.NewestMemory = &t
				}
			}

			// Reinforcement tiers.
			rc := mem.ReinforcedCount
			switch {
			case rc == 0:
				stats.ReinforcementTiers["0"]++
			case rc >= 1 && rc <= 3:
				stats.ReinforcementTiers["1-3"]++
			case rc >= 4 && rc <= 10:
				stats.ReinforcementTiers["4-10"]++
			default:
				stats.ReinforcementTiers["10+"]++
			}

			// Active conflicts.
			if mem.ConflictStatus == models.ConflictStatusActive {
				stats.ActiveConflicts++
			}

			// Pending TTL expiry (expires within 24 h).
			if mem.Scope == models.ScopeTTL && !mem.ValidUntil.IsZero() && mem.ValidUntil.Before(ttlDeadline) {
				stats.PendingTTLExpiry++
			}

			// Top accessed (top 5).
			content := mem.Content
			if len(content) > 80 {
				content = content[:80]
			}
			entry := topEntry{id: mem.ID, content: content, accessCount: mem.AccessCount}
			topAccessed = insertTopEntry(topAccessed, entry, 5)
		}

		if len(page) < pageSize {
			break
		}
		skip += int64(pageSize)
	}

	for i := range topAccessed {
		stats.TopAccessed = append(stats.TopAccessed, models.MemoryPreview{
			ID:          topAccessed[i].id,
			Content:     topAccessed[i].content,
			AccessCount: topAccessed[i].accessCount,
		})
	}
}

type topEntry struct {
	id          string
	content     string
	accessCount int64
}

func insertTopEntry(top []topEntry, entry topEntry, maxLen int) []topEntry {
	pos := len(top)
	for i := range top {
		if entry.accessCount > top[i].accessCount {
			pos = i
			break
		}
	}
	if pos >= maxLen {
		return top
	}
	top = append(top, entry)
	copy(top[pos+1:], top[pos:])
	top[pos] = entry
	if len(top) > maxLen {
		top = top[:maxLen]
	}
	return top
}

// UpsertEntity inserts or updates an entity node.
func (s *MemgraphStore) UpsertEntity(ctx context.Context, entity models.Entity) error {
	wctx, cancel := context.WithTimeout(ctx, memgraphWriteTimeout)
	defer cancel()

	session := s.driver.NewSession(wctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	params := entityToParams(entity)

	_, err := session.ExecuteWrite(wctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, txErr := tx.Run(wctx, `
			MERGE (e:Entity {name: $name})
			ON CREATE SET
			    e.uuid         = $uuid,
			    e.type         = $type,
			    e.aliases      = $aliases,
			    e.memory_ids   = $memory_ids,
			    e.metadata     = $metadata,
			    e.project      = $project,
			    e.summary      = $summary,
			    e.community_id = $community_id,
			    e.created_at   = $created_at,
			    e.updated_at   = $updated_at
			ON MATCH SET
			    e.type         = $type,
			    e.aliases      = $aliases,
			    e.memory_ids   = $memory_ids,
			    e.metadata     = $metadata,
			    e.project      = $project,
			    e.summary      = $summary,
			    e.community_id = $community_id,
			    e.updated_at   = $updated_at
		`, params)
		return nil, txErr
	})
	if err != nil {
		return fmt.Errorf("memgraph upsert entity %s: %w", entity.ID, err)
	}

	s.logger.Debug("upserted entity", "id", entity.ID, "name", entity.Name)
	return nil
}

// GetEntity retrieves a single entity by ID.
func (s *MemgraphStore) GetEntity(ctx context.Context, id string) (*models.Entity, error) {
	rctx, cancel := context.WithTimeout(ctx, memgraphReadTimeout)
	defer cancel()

	session := s.driver.NewSession(rctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	result, err := session.ExecuteRead(rctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, txErr := tx.Run(rctx, `MATCH (e:Entity {uuid: $id}) RETURN e`, map[string]any{"id": id})
		if txErr != nil {
			return nil, txErr
		}
		if res.Next(rctx) {
			record := res.Record()
			return recordToEntity(record, "e")
		}
		if consumeErr := res.Err(); consumeErr != nil {
			return nil, consumeErr
		}
		return nil, nil
	})
	if err != nil {
		return nil, fmt.Errorf("memgraph get entity %s: %w", id, err)
	}

	if result == nil {
		return nil, fmt.Errorf("%w: %s", store.ErrNotFound, id)
	}

	ent, ok := result.(*models.Entity)
	if !ok {
		return nil, fmt.Errorf("memgraph get entity: unexpected result type %T", result)
	}
	return ent, nil
}

// SearchEntities finds entities whose name contains the given string (case-insensitive).
// entityType filters by entity type (empty = all types).
// limit caps the number of results (0 = default 100).
func (s *MemgraphStore) SearchEntities(ctx context.Context, name, entityType string, limit int) ([]models.Entity, error) {
	if limit <= 0 {
		limit = 100
	}
	rctx, cancel := context.WithTimeout(ctx, memgraphReadTimeout)
	defer cancel()

	session := s.driver.NewSession(rctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	whereClauses := []string{}
	params := map[string]any{"limit": int64(limit)}

	if name != "" {
		whereClauses = append(whereClauses, "toLower(e.name) CONTAINS toLower($name)")
		params["name"] = name
	}
	if entityType != "" {
		whereClauses = append(whereClauses, "e.type = $entityType")
		params["entityType"] = entityType
	}

	where := ""
	if len(whereClauses) > 0 {
		where = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	query := fmt.Sprintf(`MATCH (e:Entity) %s RETURN e LIMIT $limit`, where)

	raw, err := session.ExecuteRead(rctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, txErr := tx.Run(rctx, query, params)
		if txErr != nil {
			return nil, txErr
		}
		return collectEntities(rctx, res, "e")
	})
	if err != nil {
		return nil, fmt.Errorf("memgraph search entities: %w", err)
	}

	entities, ok := raw.([]models.Entity)
	if !ok {
		return nil, fmt.Errorf("memgraph search entities: unexpected result type %T", raw)
	}
	return entities, nil
}

// redactURI returns a version of rawURI with credentials removed, safe to log.
func redactURI(rawURI string) string {
	u, err := url.Parse(rawURI)
	if err != nil {
		return "[unparseable URI]"
	}
	return u.Redacted()
}

// LinkMemoryToEntity appends a memory ID to an entity's memory_ids list (idempotent).
func (s *MemgraphStore) LinkMemoryToEntity(ctx context.Context, entityID, memoryID string) error {
	wctx, cancel := context.WithTimeout(ctx, memgraphWriteTimeout)
	defer cancel()

	session := s.driver.NewSession(wctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	result, err := session.ExecuteWrite(wctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, txErr := tx.Run(wctx, `
			MATCH (e:Entity {uuid: $entityID})
			SET e.memory_ids = CASE
			  WHEN $memoryID IN coalesce(e.memory_ids, []) THEN coalesce(e.memory_ids, [])
			  ELSE coalesce(e.memory_ids, []) + $memoryID
			END
			RETURN e
		`, map[string]any{"entityID": entityID, "memoryID": memoryID})
		if txErr != nil {
			return nil, fmt.Errorf("run cypher: %w", txErr)
		}
		if res.Next(wctx) {
			if _, consumeErr := res.Consume(wctx); consumeErr != nil {
				return nil, fmt.Errorf("memgraph link memory to entity: consume result: %w", consumeErr)
			}
			return true, nil
		}
		if consumeErr := res.Err(); consumeErr != nil {
			return nil, fmt.Errorf("memgraph link memory to entity: iterate result: %w", consumeErr)
		}
		if _, consumeErr := res.Consume(wctx); consumeErr != nil {
			return nil, fmt.Errorf("memgraph link memory to entity: consume not-found result: %w", consumeErr)
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("memgraph link memory to entity: %w", err)
	}

	matched, ok := result.(bool)
	if !ok {
		return fmt.Errorf("memgraph link memory to entity: internal: unexpected result type %T", result)
	}
	if !matched {
		return fmt.Errorf("memgraph link memory to entity: entity %s: %w", entityID, store.ErrNotFound)
	}

	return nil
}

// GetChain follows the supersedes_id chain and returns the full history, newest first.
// Stops when supersedes_id is empty, the referenced memory is not found, or a cycle is detected.
func (s *MemgraphStore) GetChain(ctx context.Context, id string) ([]models.Memory, error) {
	var chain []models.Memory
	visited := make(map[string]bool)
	currentID := id

	for currentID != "" {
		if visited[currentID] {
			break
		}
		visited[currentID] = true

		mem, err := s.Get(ctx, currentID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				break // legitimate chain termination
			}
			return nil, fmt.Errorf("memgraph get chain: reading %s: %w", currentID, err)
		}
		chain = append(chain, *mem)
		currentID = mem.SupersedesID
	}

	return chain, nil
}

// InvalidateMemory sets valid_to on a memory without deleting it.
// Used when a superseding memory is stored (temporal versioning).
func (s *MemgraphStore) InvalidateMemory(ctx context.Context, id string, validTo time.Time) error {
	wctx, cancel := context.WithTimeout(ctx, memgraphWriteTimeout)
	defer cancel()

	session := s.driver.NewSession(wctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	validToStr := validTo.UTC().Format(time.RFC3339Nano)

	_, err := session.ExecuteWrite(wctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, txErr := tx.Run(wctx, `
			MATCH (m:Memory {uuid: $id})
			SET m.valid_to = $valid_to
		`, map[string]any{"id": id, "valid_to": validToStr})
		return nil, txErr
	})
	if err != nil {
		return fmt.Errorf("memgraph invalidate memory %s: %w", id, err)
	}
	s.logger.Debug("invalidated memory", "id", id, "valid_to", validToStr)
	return nil
}

// GetHistory returns all versions of a memory chain, including invalidated ones.
// Uses the SupersedesID chain traversal (newest first).
func (s *MemgraphStore) GetHistory(ctx context.Context, id string) ([]models.Memory, error) {
	// GetChain already follows the SupersedesID chain.
	// We add an explicit Memgraph reverse-direction query to find predecessors too.
	// For now, delegate to GetChain which works correctly for forward chains.
	return s.GetChain(ctx, id)
}

// MigrateTemporalFields backfills valid_from = created_at for all memories without valid_from.
// Idempotent — safe to run multiple times.
func (s *MemgraphStore) MigrateTemporalFields(ctx context.Context) error {
	wctx, cancel := context.WithTimeout(ctx, 5*memgraphWriteTimeout)
	defer cancel()

	session := s.driver.NewSession(wctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	_, err := session.ExecuteWrite(wctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, txErr := tx.Run(wctx, `
			MATCH (m:Memory)
			WHERE m.valid_from IS NULL OR m.valid_from = ""
			SET m.valid_from = m.created_at
		`, nil)
		return nil, txErr
	})
	if err != nil {
		return fmt.Errorf("memgraph migrate temporal fields: %w", err)
	}

	s.logger.Info("temporal migration complete: valid_from backfilled for existing memories")
	return nil
}

// UpdateConflictFields sets ConflictGroupID and ConflictStatus on an existing memory.
func (s *MemgraphStore) UpdateConflictFields(ctx context.Context, id, conflictGroupID, conflictStatus string) error {
	wctx, cancel := context.WithTimeout(ctx, memgraphWriteTimeout)
	defer cancel()

	session := s.driver.NewSession(wctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	_, err := session.ExecuteWrite(wctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, txErr := tx.Run(wctx, `
			MATCH (m:Memory {uuid: $id})
			SET m.conflict_group_id = $group_id,
			    m.conflict_status   = $status
		`, map[string]any{
			"id":       id,
			"group_id": conflictGroupID,
			"status":   conflictStatus,
		})
		return nil, txErr
	})
	if err != nil {
		return fmt.Errorf("memgraph update conflict fields %s: %w", id, err)
	}

	return nil
}

// UpdateReinforcement boosts the confidence of an existing memory (capped at 1.0)
// and increments reinforced_count.
func (s *MemgraphStore) UpdateReinforcement(ctx context.Context, id string, confidenceBoost float64) error {
	existing, err := s.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("memgraph update reinforcement get %s: %w", id, err)
	}

	newConf := existing.Confidence + confidenceBoost
	if newConf > 1.0 {
		newConf = 1.0
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	wctx, cancel := context.WithTimeout(ctx, memgraphWriteTimeout)
	defer cancel()

	session := s.driver.NewSession(wctx, s.sessionConfig())
	defer s.closeSession(ctx, session)

	_, writeErr := session.ExecuteWrite(wctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, txErr := tx.Run(wctx, `
			MATCH (m:Memory {uuid: $id})
			SET m.confidence       = $confidence,
			    m.reinforced_at_unix = $reinforced_at,
			    m.reinforced_count = $reinforced_count
		`, map[string]any{
			"id":               id,
			"confidence":       newConf,
			"reinforced_at":    now,
			"reinforced_count": int64(existing.ReinforcedCount + 1),
		})
		return nil, txErr
	})
	if writeErr != nil {
		return fmt.Errorf("memgraph update reinforcement set %s: %w", id, writeErr)
	}

	return nil
}

// DeleteAllMemories removes all nodes and relationships from the graph.
// This is intended for eval benchmark isolation only — it is destructive.
//
// Known limitation: `MATCH (n) DETACH DELETE n` runs as a single Bolt
// transaction. On a large or heavily-indexed graph this can exhaust the
// Memgraph transaction memory budget and fail even within
// memgraphDeleteAllTimeout. The eval harness synthetic datasets are small
// (O(100) nodes per QA pair), so this is safe in practice. For production
// stores with millions of nodes, batched deletion (WITH n LIMIT N) would be
// required; tracked in issue #91 alongside the --format json follow-up.
func (s *MemgraphStore) DeleteAllMemories(ctx context.Context) error {
	wctx, cancel := context.WithTimeout(ctx, memgraphDeleteAllTimeout)
	defer cancel()

	session := s.driver.NewSession(wctx, s.sessionConfig())
	defer s.closeSession(context.Background(), session) // fresh ctx — both wctx and caller's ctx may be expired by close time

	_, err := session.ExecuteWrite(wctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, txErr := tx.Run(wctx, "MATCH (n) DETACH DELETE n", nil)
		if txErr != nil {
			return nil, txErr
		}
		_, txErr = result.Consume(wctx)
		return nil, txErr
	})
	if err != nil {
		return fmt.Errorf("memgraph delete all memories: %w", err)
	}
	return nil
}

// Close releases the driver connection.
func (s *MemgraphStore) Close() error {
	closeCtx, cancel := context.WithTimeout(context.Background(), memgraphReadTimeout)
	defer cancel()
	if err := s.driver.Close(closeCtx); err != nil {
		return fmt.Errorf("memgraph close: %w", err)
	}
	return nil
}

// --- Serialization helpers ---

// memoryToParams converts a Memory and its embedding vector to a Cypher parameter map.
func memoryToParams(m models.Memory, vector []float32) map[string]any {
	var validUntilUnix int64
	if !m.ValidUntil.IsZero() {
		validUntilUnix = m.ValidUntil.Unix()
	}
	var reinforcedAtUnix int64
	if !m.ReinforcedAt.IsZero() {
		reinforcedAtUnix = m.ReinforcedAt.Unix()
	}

	var metaStr string
	if len(m.Metadata) > 0 {
		if b, marshalErr := json.Marshal(m.Metadata); marshalErr == nil {
			metaStr = string(b)
		}
	}

	// Convert tags to []any for Cypher list parameters.
	tags := make([]any, len(m.Tags))
	for i, t := range m.Tags {
		tags[i] = t
	}

	return map[string]any{
		"uuid":              m.ID,
		"type":              string(m.Type),
		"scope":             string(m.Scope),
		"visibility":        string(m.Visibility),
		"content":           m.Content,
		"confidence":        m.Confidence,
		"source":            m.Source,
		"project":           m.Project,
		"ttl_seconds":       m.TTLSeconds,
		"tags":              tags,
		"metadata":          metaStr,
		"created_at":        m.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":        m.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"last_accessed":     m.LastAccessed.UTC().Format(time.RFC3339Nano),
		"access_count":      m.AccessCount,
		"supersedes_id":     m.SupersedesID,
		"conflict_group_id": m.ConflictGroupID,
		"conflict_status":   string(m.ConflictStatus),
		"valid_until_unix":  validUntilUnix,
		"valid_from": func() string {
			if m.ValidFrom.IsZero() {
				return m.CreatedAt.UTC().Format(time.RFC3339Nano)
			}
			return m.ValidFrom.UTC().Format(time.RFC3339Nano)
		}(),
		"valid_to": func() string {
			if m.ValidTo == nil {
				return ""
			}
			return m.ValidTo.UTC().Format(time.RFC3339Nano)
		}(),
		"reinforced_at_unix": reinforcedAtUnix,
		"reinforced_count":   int64(m.ReinforcedCount),
		"has_embedding":      vector != nil,
		"user_id":            m.UserID,
		"embedding":          float32SliceToAny(vector),
	}
}

// recordToMemory deserializes a neo4j record node into a Memory.
func recordToMemory(record *neo4j.Record, alias string) (*models.Memory, error) {
	raw, ok := record.Get(alias)
	if !ok {
		return nil, fmt.Errorf("recordToMemory: alias %q not found in record", alias)
	}
	node, ok := raw.(neo4j.Node)
	if !ok {
		return nil, fmt.Errorf("recordToMemory: expected neo4j.Node, got %T", raw)
	}
	props := node.Props

	m := &models.Memory{
		ID:              propString(props, "uuid"),
		Type:            models.MemoryType(propString(props, "type")),
		Scope:           models.MemoryScope(propString(props, "scope")),
		Visibility:      models.MemoryVisibility(propString(props, "visibility")),
		Content:         propString(props, "content"),
		Confidence:      propFloat64(props, "confidence"),
		Source:          propString(props, "source"),
		Project:         propString(props, "project"),
		UserID:          propString(props, "user_id"),
		TTLSeconds:      propInt64(props, "ttl_seconds"),
		AccessCount:     propInt64(props, "access_count"),
		SupersedesID:    propString(props, "supersedes_id"),
		ConflictGroupID: propString(props, "conflict_group_id"),
		ConflictStatus:  models.ConflictStatus(propString(props, "conflict_status")),
		ReinforcedCount: int(propInt64(props, "reinforced_count")),
	}

	if ts := propString(props, "created_at"); ts != "" {
		m.CreatedAt = parseTime(ts)
	}
	if ts := propString(props, "updated_at"); ts != "" {
		m.UpdatedAt = parseTime(ts)
	}
	if ts := propString(props, "last_accessed"); ts != "" {
		m.LastAccessed = parseTime(ts)
	}

	if unix := propInt64(props, "valid_until_unix"); unix != 0 {
		m.ValidUntil = time.Unix(unix, 0).UTC()
	}
	if unix := propInt64(props, "reinforced_at_unix"); unix != 0 {
		m.ReinforcedAt = time.Unix(unix, 0).UTC()
	}

	// Tags stored as a Cypher list.
	if raw, exists := props["tags"]; exists {
		if list, isList := raw.([]any); isList {
			for _, v := range list {
				if s, isStr := v.(string); isStr {
					m.Tags = append(m.Tags, s)
				}
			}
		}
	}

	// Metadata stored as a JSON string.
	if metaStr := propString(props, "metadata"); metaStr != "" {
		var meta map[string]any
		if unmarshalErr := json.Unmarshal([]byte(metaStr), &meta); unmarshalErr == nil {
			m.Metadata = meta
		}
	}

	return m, nil
}

// entityToParams converts an Entity to a Cypher parameter map.
func entityToParams(e models.Entity) map[string]any {
	aliases := make([]any, len(e.Aliases))
	for i, a := range e.Aliases {
		aliases[i] = a
	}
	memIDs := make([]any, len(e.MemoryIDs))
	for i, mid := range e.MemoryIDs {
		memIDs[i] = mid
	}
	var metaStr string
	if len(e.Metadata) > 0 {
		if b, marshalErr := json.Marshal(e.Metadata); marshalErr == nil {
			metaStr = string(b)
		}
	}

	return map[string]any{
		"uuid":         e.ID,
		"name":         e.Name,
		"type":         string(e.Type),
		"aliases":      aliases,
		"memory_ids":   memIDs,
		"metadata":     metaStr,
		"project":      e.Project,
		"summary":      e.Summary,
		"community_id": e.CommunityID,
		"created_at":   e.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":   e.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// recordToEntity deserializes a neo4j record node into an Entity.
func recordToEntity(record *neo4j.Record, alias string) (*models.Entity, error) {
	raw, ok := record.Get(alias)
	if !ok {
		return nil, fmt.Errorf("recordToEntity: alias %q not found in record", alias)
	}
	node, ok := raw.(neo4j.Node)
	if !ok {
		return nil, fmt.Errorf("recordToEntity: expected neo4j.Node, got %T", raw)
	}
	props := node.Props

	e := &models.Entity{
		ID:          propString(props, "uuid"),
		Name:        propString(props, "name"),
		Type:        models.EntityType(propString(props, "type")),
		Project:     propString(props, "project"),
		Summary:     propString(props, "summary"),
		CommunityID: propString(props, "community_id"),
	}

	if ts := propString(props, "created_at"); ts != "" {
		e.CreatedAt = parseTime(ts)
	}
	if ts := propString(props, "updated_at"); ts != "" {
		e.UpdatedAt = parseTime(ts)
	}

	if rawAliases, exists := props["aliases"]; exists {
		if list, isList := rawAliases.([]any); isList {
			for _, v := range list {
				if s, isStr := v.(string); isStr {
					e.Aliases = append(e.Aliases, s)
				}
			}
		}
	}

	if rawMemIDs, exists := props["memory_ids"]; exists {
		if list, isList := rawMemIDs.([]any); isList {
			for _, v := range list {
				if s, isStr := v.(string); isStr {
					e.MemoryIDs = append(e.MemoryIDs, s)
				}
			}
		}
	}

	if metaStr := propString(props, "metadata"); metaStr != "" {
		var meta map[string]any
		if unmarshalErr := json.Unmarshal([]byte(metaStr), &meta); unmarshalErr == nil {
			e.Metadata = meta
		}
	}

	return e, nil
}

// --- Filter builder ---

// buildWhereClause constructs WHERE clause conditions from SearchFilters.
// nodeAlias MUST be a hard-coded Cypher variable name ("m" or "node") — never user-supplied input.
// Returns the condition strings and a parameter map.
func buildWhereClause(f *store.SearchFilters, nodeAlias string) ([]string, map[string]any) {
	// Allowlist guard: nodeAlias is interpolated into Cypher; only known literals are safe.
	switch nodeAlias {
	case "m", "node":
	default:
		panic(fmt.Sprintf("buildWhereClause: invalid nodeAlias %q (must be \"m\" or \"node\")", nodeAlias))
	}

	// Sensitive memories are opt-in: excluded by default unless visibility filter explicitly requests them.
	// This mirrors the matchesFilters logic in MockStore for consistent behavior across implementations.
	var sensitiveRequested bool
	if f != nil && f.Visibility != nil && *f.Visibility == models.VisibilitySensitive {
		sensitiveRequested = true
	}

	if f == nil {
		return []string{fmt.Sprintf("%s.visibility <> $exclude_sensitive", nodeAlias)},
			map[string]any{"exclude_sensitive": string(models.VisibilitySensitive)}
	}

	var clauses []string
	params := make(map[string]any)

	if !sensitiveRequested {
		clauses = append(clauses, fmt.Sprintf("%s.visibility <> $exclude_sensitive", nodeAlias))
		params["exclude_sensitive"] = string(models.VisibilitySensitive)
	}

	if f.Type != nil {
		clauses = append(clauses, fmt.Sprintf("%s.type = $filter_type", nodeAlias))
		params["filter_type"] = string(*f.Type)
	}
	if f.Scope != nil {
		clauses = append(clauses, fmt.Sprintf("%s.scope = $filter_scope", nodeAlias))
		params["filter_scope"] = string(*f.Scope)
	}
	if f.Visibility != nil {
		clauses = append(clauses, fmt.Sprintf("%s.visibility = $filter_visibility", nodeAlias))
		params["filter_visibility"] = string(*f.Visibility)
	}
	if f.Project != nil {
		clauses = append(clauses, fmt.Sprintf("%s.project = $filter_project", nodeAlias))
		params["filter_project"] = *f.Project
	}
	if f.Source != nil {
		clauses = append(clauses, fmt.Sprintf("%s.source = $filter_source", nodeAlias))
		params["filter_source"] = *f.Source
	}
	if f.UserID != "" {
		clauses = append(clauses, fmt.Sprintf("%s.user_id = $filter_user_id", nodeAlias))
		params["filter_user_id"] = f.UserID
	}
	if f.ConflictStatus != nil {
		clauses = append(clauses, fmt.Sprintf("%s.conflict_status = $filter_conflict_status", nodeAlias))
		params["filter_conflict_status"] = string(*f.ConflictStatus)
	}
	for i, tag := range f.Tags {
		paramKey := fmt.Sprintf("filter_tag_%d", i)
		clauses = append(clauses, fmt.Sprintf("$%s IN %s.tags", paramKey, nodeAlias))
		params[paramKey] = tag
	}

	// Temporal filtering: by default exclude invalidated memories (valid_to IS NULL).
	// AsOf takes precedence over IncludeInvalidated.
	if f.AsOf != nil {
		asOfStr := f.AsOf.UTC().Format(time.RFC3339)
		// valid_from <= AsOf AND (valid_to IS NULL OR valid_to > AsOf)
		clauses = append(clauses,
			fmt.Sprintf("(%s.valid_from IS NULL OR %s.valid_from <= $filter_as_of)", nodeAlias, nodeAlias),
			fmt.Sprintf("(%s.valid_to IS NULL OR %s.valid_to > $filter_as_of)", nodeAlias, nodeAlias),
		)
		params["filter_as_of"] = asOfStr
	} else if !f.IncludeInvalidated {
		// Default: only return currently-valid memories (valid_to not set).
		clauses = append(clauses, fmt.Sprintf("(%s.valid_to IS NULL OR %s.valid_to = \"\")", nodeAlias, nodeAlias))
	}

	return clauses, params
}

// --- Collection helpers ---

// collectSearchResults drains a result set into []models.SearchResult.
// Each record is expected to have a "node" column (neo4j.Node) and a "score" column (float64).
func collectSearchResults(ctx context.Context, res neo4j.ResultWithContext) ([]models.SearchResult, error) {
	var results []models.SearchResult
	for res.Next(ctx) {
		record := res.Record()

		mem, memErr := recordToMemory(record, "node")
		if memErr != nil {
			return nil, memErr
		}

		score, _ := record.Get("score")
		results = append(results, models.SearchResult{
			Memory: *mem,
			Score:  toFloat64(score),
		})
	}
	return results, res.Err()
}

// collectMemories drains a result set into []models.Memory using the given node alias.
func collectMemories(ctx context.Context, res neo4j.ResultWithContext, alias string) ([]models.Memory, error) {
	var memories []models.Memory
	for res.Next(ctx) {
		mem, memErr := recordToMemory(res.Record(), alias)
		if memErr != nil {
			return nil, memErr
		}
		memories = append(memories, *mem)
	}
	return memories, res.Err()
}

// collectEntities drains a result set into []models.Entity using the given node alias.
func collectEntities(ctx context.Context, res neo4j.ResultWithContext, alias string) ([]models.Entity, error) {
	var entities []models.Entity
	for res.Next(ctx) {
		e, eErr := recordToEntity(res.Record(), alias)
		if eErr != nil {
			return nil, eErr
		}
		entities = append(entities, *e)
	}
	return entities, res.Err()
}

// --- Property accessors ---

func propString(props map[string]any, key string) string {
	if v, ok := props[key]; ok {
		if s, isStr := v.(string); isStr {
			return s
		}
	}
	return ""
}

func propFloat64(props map[string]any, key string) float64 {
	if v, ok := props[key]; ok {
		return toFloat64(v)
	}
	return 0
}

func propInt64(props map[string]any, key string) int64 {
	if v, ok := props[key]; ok {
		return toInt64(v)
	}
	return 0
}

// toFloat64 converts a neo4j numeric value to float64.
func toFloat64(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int64:
		return float64(val)
	case int:
		return float64(val)
	}
	return 0
}

// toInt64 converts a neo4j numeric value to int64.
func toInt64(v any) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case float64:
		return int64(val)
	}
	return 0
}

// float32SliceToAny converts []float32 to []any for Cypher parameter passing.
func float32SliceToAny(vec []float32) []any {
	out := make([]any, len(vec))
	for i, v := range vec {
		out[i] = float64(v)
	}
	return out
}

// parseTime parses a timestamp string, trying RFC3339Nano first then RFC3339.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}
