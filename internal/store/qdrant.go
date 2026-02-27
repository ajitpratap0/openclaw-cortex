package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	pb "github.com/qdrant/go-client/qdrant"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

const (
	qdrantDialTimeout  = 10 * time.Second
	qdrantReadTimeout  = 10 * time.Second
	qdrantWriteTimeout = 30 * time.Second
)

func withTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}

// QdrantStore implements Store using Qdrant's gRPC API.
type QdrantStore struct {
	conn       *grpc.ClientConn
	points     pb.PointsClient
	collection pb.CollectionsClient
	collName   string
	dimension  uint64
	logger     *slog.Logger
}

// NewQdrantStore creates a new Qdrant store connection.
func NewQdrantStore(host string, port int, collection string, dimension uint64, useTLS bool, logger *slog.Logger) (*QdrantStore, error) {
	addr := fmt.Sprintf("%s:%d", host, port)

	opts := []grpc.DialOption{}
	if !useTLS {
		logger.Warn("Qdrant connection using insecure credentials (no TLS)")
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("connecting to Qdrant at %s: %w", addr, err)
	}

	// Verify the connection with a timeout by issuing a lightweight RPC.
	dialCtx, dialCancel := context.WithTimeout(context.Background(), qdrantDialTimeout)
	defer dialCancel()
	if _, err := pb.NewCollectionsClient(conn).List(dialCtx, &pb.ListCollectionsRequest{}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("verifying Qdrant connection at %s: %w", addr, err)
	}

	logger.Info("connected to Qdrant", "addr", addr, "collection", collection)

	return &QdrantStore{
		conn:       conn,
		points:     pb.NewPointsClient(conn),
		collection: pb.NewCollectionsClient(conn),
		collName:   collection,
		dimension:  dimension,
		logger:     logger,
	}, nil
}

func (q *QdrantStore) EnsureCollection(ctx context.Context) error {
	// Check if collection exists
	rctx, rcancel := withTimeout(ctx, qdrantReadTimeout)
	defer rcancel()
	resp, err := q.collection.List(rctx, &pb.ListCollectionsRequest{})
	if err != nil {
		return fmt.Errorf("listing collections: %w", err)
	}

	for _, c := range resp.GetCollections() {
		if c.GetName() == q.collName {
			q.logger.Info("collection already exists", "name", q.collName)
			return nil
		}
	}

	// Create collection
	wctx, wcancel := withTimeout(ctx, qdrantWriteTimeout)
	defer wcancel()
	_, err = q.collection.Create(wctx, &pb.CreateCollection{
		CollectionName: q.collName,
		VectorsConfig: &pb.VectorsConfig{
			Config: &pb.VectorsConfig_Params{
				Params: &pb.VectorParams{
					Size:     q.dimension,
					Distance: pb.Distance_Cosine,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("creating collection %s: %w", q.collName, err)
	}

	q.logger.Info("created collection", "name", q.collName, "dimension", q.dimension)

	// Create payload indexes for common filter fields
	indexFields := []string{"type", "scope", "visibility", "project", "source"}
	for _, field := range indexFields {
		ictx, icancel := withTimeout(ctx, qdrantWriteTimeout)
		defer icancel()
		_, err := q.points.CreateFieldIndex(ictx, &pb.CreateFieldIndexCollection{
			CollectionName: q.collName,
			FieldName:      field,
			FieldType:      pb.FieldType_FieldTypeKeyword.Enum(),
		})
		if err != nil {
			q.logger.Warn("creating field index", "field", field, "error", err)
		}
	}

	return nil
}

func (q *QdrantStore) Upsert(ctx context.Context, memory models.Memory, vector []float32) error {
	ctx, cancel := withTimeout(ctx, qdrantWriteTimeout)
	defer cancel()
	payload := memoryToPayload(memory)

	_, err := q.points.Upsert(ctx, &pb.UpsertPoints{
		CollectionName: q.collName,
		Points: []*pb.PointStruct{
			{
				Id: &pb.PointId{
					PointIdOptions: &pb.PointId_Uuid{Uuid: memory.ID},
				},
				Vectors: &pb.Vectors{
					VectorsOptions: &pb.Vectors_Vector{
						Vector: &pb.Vector{Data: vector},
					},
				},
				Payload: payload,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("upserting point %s: %w", memory.ID, err)
	}

	q.logger.Debug("upserted memory", "id", memory.ID, "type", memory.Type)
	return nil
}

func (q *QdrantStore) Search(ctx context.Context, vector []float32, limit uint64, filters *SearchFilters) ([]models.SearchResult, error) {
	ctx, cancel := withTimeout(ctx, qdrantReadTimeout)
	defer cancel()
	req := &pb.SearchPoints{
		CollectionName: q.collName,
		Vector:         vector,
		Limit:          limit,
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
	}

	if filters != nil {
		req.Filter = buildFilter(filters)
	}

	resp, err := q.points.Search(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("searching: %w", err)
	}

	results := make([]models.SearchResult, 0, len(resp.GetResult()))
	for _, point := range resp.GetResult() {
		mem, err := payloadToMemory(point.GetId().GetUuid(), point.GetPayload())
		if err != nil {
			q.logger.Warn("parsing search result", "error", err)
			continue
		}
		results = append(results, models.SearchResult{
			Memory: *mem,
			Score:  float64(point.GetScore()),
		})
	}

	return results, nil
}

func (q *QdrantStore) Get(ctx context.Context, id string) (*models.Memory, error) {
	ctx, cancel := withTimeout(ctx, qdrantReadTimeout)
	defer cancel()
	resp, err := q.points.Get(ctx, &pb.GetPoints{
		CollectionName: q.collName,
		Ids: []*pb.PointId{
			{PointIdOptions: &pb.PointId_Uuid{Uuid: id}},
		},
		WithPayload: &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
	})
	if err != nil {
		return nil, fmt.Errorf("getting point %s: %w", id, err)
	}

	if len(resp.GetResult()) == 0 {
		return nil, fmt.Errorf("memory %s not found", id)
	}

	point := resp.GetResult()[0]
	return payloadToMemory(point.GetId().GetUuid(), point.GetPayload())
}

func (q *QdrantStore) Delete(ctx context.Context, id string) error {
	ctx, cancel := withTimeout(ctx, qdrantWriteTimeout)
	defer cancel()
	_, err := q.points.Delete(ctx, &pb.DeletePoints{
		CollectionName: q.collName,
		Points: &pb.PointsSelector{
			PointsSelectorOneOf: &pb.PointsSelector_Points{
				Points: &pb.PointsIdsList{
					Ids: []*pb.PointId{
						{PointIdOptions: &pb.PointId_Uuid{Uuid: id}},
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("deleting point %s: %w", id, err)
	}

	q.logger.Debug("deleted memory", "id", id)
	return nil
}

func (q *QdrantStore) List(ctx context.Context, filters *SearchFilters, limit uint64, cursor string) ([]models.Memory, string, error) {
	ctx, cancel := withTimeout(ctx, qdrantReadTimeout)
	defer cancel()
	var filter *pb.Filter
	if filters != nil {
		filter = buildFilter(filters)
	}

	limit32 := uint32(limit)
	req := &pb.ScrollPoints{
		CollectionName: q.collName,
		Filter:         filter,
		Limit:          &limit32,
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
	}
	if cursor != "" {
		req.Offset = &pb.PointId{PointIdOptions: &pb.PointId_Uuid{Uuid: cursor}}
	}

	resp, err := q.points.Scroll(ctx, req)
	if err != nil {
		return nil, "", fmt.Errorf("scrolling points: %w", err)
	}

	memories := make([]models.Memory, 0, len(resp.GetResult()))
	for _, point := range resp.GetResult() {
		mem, err := payloadToMemory(point.GetId().GetUuid(), point.GetPayload())
		if err != nil {
			q.logger.Warn("parsing list result", "error", err)
			continue
		}
		memories = append(memories, *mem)
	}

	var nextCursor string
	if npo := resp.GetNextPageOffset(); npo != nil {
		nextCursor = npo.GetUuid()
	}

	return memories, nextCursor, nil
}

func (q *QdrantStore) FindDuplicates(ctx context.Context, vector []float32, threshold float64) ([]models.SearchResult, error) {
	ctx, cancel := withTimeout(ctx, qdrantReadTimeout)
	defer cancel()
	resp, err := q.points.Search(ctx, &pb.SearchPoints{
		CollectionName: q.collName,
		Vector:         vector,
		Limit:          5,
		ScoreThreshold: float32Ptr(float32(threshold)),
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
	})
	if err != nil {
		return nil, fmt.Errorf("searching duplicates: %w", err)
	}

	results := make([]models.SearchResult, 0, len(resp.GetResult()))
	for _, point := range resp.GetResult() {
		mem, err := payloadToMemory(point.GetId().GetUuid(), point.GetPayload())
		if err != nil {
			continue
		}
		results = append(results, models.SearchResult{
			Memory: *mem,
			Score:  float64(point.GetScore()),
		})
	}

	return results, nil
}

// UpdateAccessMetadata sets the last_accessed timestamp directly via SetPayload,
// avoiding a read-modify-write race on access_count.
func (q *QdrantStore) UpdateAccessMetadata(ctx context.Context, id string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	wctx, wcancel := withTimeout(ctx, qdrantWriteTimeout)
	defer wcancel()
	_, err := q.points.SetPayload(wctx, &pb.SetPayloadPoints{
		CollectionName: q.collName,
		PointsSelector: &pb.PointsSelector{
			PointsSelectorOneOf: &pb.PointsSelector_Points{
				Points: &pb.PointsIdsList{
					Ids: []*pb.PointId{
						{PointIdOptions: &pb.PointId_Uuid{Uuid: id}},
					},
				},
			},
		},
		Payload: map[string]*pb.Value{
			"last_accessed": {Kind: &pb.Value_StringValue{StringValue: now}},
		},
	})
	if err != nil {
		return fmt.Errorf("update access metadata: setting payload for %s: %w", id, err)
	}

	return nil
}

// Stats returns collection statistics. Type and scope counts are fetched concurrently.
func (q *QdrantStore) Stats(ctx context.Context) (*models.CollectionStats, error) {
	rctx, rcancel := withTimeout(ctx, qdrantReadTimeout)
	defer rcancel()
	info, err := q.collection.Get(rctx, &pb.GetCollectionInfoRequest{
		CollectionName: q.collName,
	})
	if err != nil {
		return nil, fmt.Errorf("getting collection info: %w", err)
	}

	stats := &models.CollectionStats{
		TotalMemories: int64(info.GetResult().GetPointsCount()),
		ByType:        make(map[string]int64),
		ByScope:       make(map[string]int64),
	}

	type countResult struct {
		key   string
		field string
		count int64
	}

	// Build all count tasks (types + scopes).
	type task struct {
		field string
		key   string
	}
	var tasks []task
	for _, mt := range models.ValidMemoryTypes {
		tasks = append(tasks, task{field: "type", key: string(mt)})
	}
	for _, sc := range []models.MemoryScope{models.ScopePermanent, models.ScopeProject, models.ScopeSession, models.ScopeTTL} {
		tasks = append(tasks, task{field: "scope", key: string(sc)})
	}

	results := make([]countResult, len(tasks))
	g, gctx := errgroup.WithContext(ctx)

	for i, t := range tasks {
		i, t := i, t
		g.Go(func() error {
			cctx, ccancel := withTimeout(gctx, qdrantReadTimeout)
			defer ccancel()
			countResp, err := q.points.Count(cctx, &pb.CountPoints{
				CollectionName: q.collName,
				Filter: &pb.Filter{
					Must: []*pb.Condition{
						{
							ConditionOneOf: &pb.Condition_Field{
								Field: &pb.FieldCondition{
									Key: t.field,
									Match: &pb.Match{
										MatchValue: &pb.Match_Keyword{Keyword: t.key},
									},
								},
							},
						},
					},
				},
				Exact: boolPtr(true),
			})
			if err != nil {
				// Non-fatal: log and continue with 0 count.
				q.logger.Warn("counting by field", "field", t.field, "key", t.key, "error", err)
				results[i] = countResult{field: t.field, key: t.key, count: 0}
				return nil
			}
			results[i] = countResult{field: t.field, key: t.key, count: int64(countResp.GetResult().GetCount())}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("counting stats: %w", err)
	}

	for _, r := range results {
		switch r.field {
		case "type":
			stats.ByType[r.key] = r.count
		case "scope":
			stats.ByScope[r.key] = r.count
		}
	}

	return stats, nil
}

func (q *QdrantStore) Close() error {
	if q.conn != nil {
		return q.conn.Close()
	}
	return nil
}

// --- Helper functions ---

func memoryToPayload(m models.Memory) map[string]*pb.Value {
	payload := map[string]*pb.Value{
		"type":       {Kind: &pb.Value_StringValue{StringValue: string(m.Type)}},
		"scope":      {Kind: &pb.Value_StringValue{StringValue: string(m.Scope)}},
		"visibility": {Kind: &pb.Value_StringValue{StringValue: string(m.Visibility)}},
		"content":    {Kind: &pb.Value_StringValue{StringValue: m.Content}},
		"confidence": {Kind: &pb.Value_DoubleValue{DoubleValue: m.Confidence}},
		"source":     {Kind: &pb.Value_StringValue{StringValue: m.Source}},
		"project":    {Kind: &pb.Value_StringValue{StringValue: m.Project}},
		"ttl_seconds": {Kind: &pb.Value_IntegerValue{IntegerValue: m.TTLSeconds}},
		"created_at":    {Kind: &pb.Value_StringValue{StringValue: m.CreatedAt.Format(time.RFC3339)}},
		"updated_at":    {Kind: &pb.Value_StringValue{StringValue: m.UpdatedAt.Format(time.RFC3339)}},
		"last_accessed": {Kind: &pb.Value_StringValue{StringValue: m.LastAccessed.Format(time.RFC3339)}},
		"access_count":  {Kind: &pb.Value_IntegerValue{IntegerValue: m.AccessCount}},
	}

	// Tags as list
	if len(m.Tags) > 0 {
		tagValues := make([]*pb.Value, len(m.Tags))
		for i, tag := range m.Tags {
			tagValues[i] = &pb.Value{Kind: &pb.Value_StringValue{StringValue: tag}}
		}
		payload["tags"] = &pb.Value{Kind: &pb.Value_ListValue{ListValue: &pb.ListValue{Values: tagValues}}}
	}

	// Metadata as JSON string
	if len(m.Metadata) > 0 {
		metaBytes, err := json.Marshal(m.Metadata)
		if err == nil {
			payload["metadata"] = &pb.Value{Kind: &pb.Value_StringValue{StringValue: string(metaBytes)}}
		}
	}

	return payload
}

func payloadToMemory(id string, payload map[string]*pb.Value) (*models.Memory, error) {
	m := &models.Memory{
		ID:         id,
		Type:       models.MemoryType(getStringValue(payload, "type")),
		Scope:      models.MemoryScope(getStringValue(payload, "scope")),
		Visibility: models.MemoryVisibility(getStringValue(payload, "visibility")),
		Content:    getStringValue(payload, "content"),
		Confidence: getDoubleValue(payload, "confidence"),
		Source:     getStringValue(payload, "source"),
		Project:    getStringValue(payload, "project"),
		TTLSeconds: getIntValue(payload, "ttl_seconds"),
		AccessCount: getIntValue(payload, "access_count"),
	}

	// Parse timestamps
	if ts := getStringValue(payload, "created_at"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			m.CreatedAt = t
		}
	}
	if ts := getStringValue(payload, "updated_at"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			m.UpdatedAt = t
		}
	}
	if ts := getStringValue(payload, "last_accessed"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			m.LastAccessed = t
		}
	}

	// Parse tags
	if tagVal, ok := payload["tags"]; ok {
		if lv := tagVal.GetListValue(); lv != nil {
			for _, v := range lv.GetValues() {
				m.Tags = append(m.Tags, v.GetStringValue())
			}
		}
	}

	// Parse metadata
	if metaStr := getStringValue(payload, "metadata"); metaStr != "" {
		var meta map[string]any
		if err := json.Unmarshal([]byte(metaStr), &meta); err == nil {
			m.Metadata = meta
		}
	}

	return m, nil
}

func buildFilter(f *SearchFilters) *pb.Filter {
	var conditions []*pb.Condition

	if f.Type != nil {
		conditions = append(conditions, &pb.Condition{
			ConditionOneOf: &pb.Condition_Field{
				Field: &pb.FieldCondition{
					Key:   "type",
					Match: &pb.Match{MatchValue: &pb.Match_Keyword{Keyword: string(*f.Type)}},
				},
			},
		})
	}

	if f.Scope != nil {
		conditions = append(conditions, &pb.Condition{
			ConditionOneOf: &pb.Condition_Field{
				Field: &pb.FieldCondition{
					Key:   "scope",
					Match: &pb.Match{MatchValue: &pb.Match_Keyword{Keyword: string(*f.Scope)}},
				},
			},
		})
	}

	if f.Visibility != nil {
		conditions = append(conditions, &pb.Condition{
			ConditionOneOf: &pb.Condition_Field{
				Field: &pb.FieldCondition{
					Key:   "visibility",
					Match: &pb.Match{MatchValue: &pb.Match_Keyword{Keyword: string(*f.Visibility)}},
				},
			},
		})
	}

	if f.Project != nil {
		conditions = append(conditions, &pb.Condition{
			ConditionOneOf: &pb.Condition_Field{
				Field: &pb.FieldCondition{
					Key:   "project",
					Match: &pb.Match{MatchValue: &pb.Match_Keyword{Keyword: *f.Project}},
				},
			},
		})
	}

	if f.Source != nil {
		conditions = append(conditions, &pb.Condition{
			ConditionOneOf: &pb.Condition_Field{
				Field: &pb.FieldCondition{
					Key:   "source",
					Match: &pb.Match{MatchValue: &pb.Match_Keyword{Keyword: *f.Source}},
				},
			},
		})
	}

	for _, tag := range f.Tags {
		conditions = append(conditions, &pb.Condition{
			ConditionOneOf: &pb.Condition_Field{
				Field: &pb.FieldCondition{
					Key:   "tags",
					Match: &pb.Match{MatchValue: &pb.Match_Keyword{Keyword: tag}},
				},
			},
		})
	}

	if len(conditions) == 0 {
		return nil
	}

	return &pb.Filter{Must: conditions}
}

func getStringValue(payload map[string]*pb.Value, key string) string {
	if v, ok := payload[key]; ok {
		return v.GetStringValue()
	}
	return ""
}

func getDoubleValue(payload map[string]*pb.Value, key string) float64 {
	if v, ok := payload[key]; ok {
		return v.GetDoubleValue()
	}
	return 0
}

func getIntValue(payload map[string]*pb.Value, key string) int64 {
	if v, ok := payload[key]; ok {
		return v.GetIntegerValue()
	}
	return 0
}

func float32Ptr(v float32) *float32 { return &v }
func boolPtr(v bool) *bool          { return &v }
