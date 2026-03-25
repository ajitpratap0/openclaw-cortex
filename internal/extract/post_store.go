// Package extract provides shared entity and fact extraction logic used by
// both the store and capture commands. Extraction is always graceful —
// failures are logged as warnings and the caller receives a zero Result.
package extract

import (
	"context"
	"log/slog"
	"strings"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	graphpkg "github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/llm"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// EntityFactStore is the subset of store.Store required for entity and link writes.
type EntityFactStore interface {
	// UpsertEntity creates or updates an entity node.
	UpsertEntity(ctx context.Context, entity models.Entity) error
	// LinkMemoryToEntity adds a memory ID to an entity's memory list.
	LinkMemoryToEntity(ctx context.Context, entityID, memoryID string) error
}

// StoredMemory holds the ID and content of a memory that has already been
// persisted to the store and is ready for entity/fact extraction.
type StoredMemory struct {
	ID      string
	Content string
}

// Deps bundles the external dependencies needed by Run.
type Deps struct {
	// LLMClient is the LLM used for entity and fact extraction.
	// If nil, Run returns a zero Result immediately.
	LLMClient llm.LLMClient
	// Model is the model name to pass to the LLM client.
	Model string
	// Store handles entity upserts and memory-to-entity links.
	Store EntityFactStore
	// GraphClient handles fact upserts and memory-to-fact links.
	GraphClient graphpkg.Client
	// Logger is used for warnings on non-fatal errors.
	Logger *slog.Logger
}

// Result summarizes what was extracted from the given memories.
type Result struct {
	// EntitiesExtracted is the number of distinct entities successfully upserted.
	EntitiesExtracted int
	// FactsExtracted is the number of facts successfully upserted.
	FactsExtracted int
}

// Run extracts entities and facts from memories and writes them to the store
// and graph client. Errors from individual extraction or write operations are
// logged as warnings and do not abort the loop — Run always returns a Result.
//
// Callers MUST ensure that deps.Store and deps.GraphClient are non-nil when
// deps.LLMClient is non-nil.
func Run(ctx context.Context, deps Deps, memories []StoredMemory) Result {
	if deps.LLMClient == nil {
		return Result{}
	}
	if deps.Store == nil || deps.GraphClient == nil {
		if deps.Logger != nil {
			deps.Logger.Warn("extract.Run: Store and GraphClient must be non-nil when LLMClient is set; skipping")
		}
		return Result{}
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	extractor := capture.NewEntityExtractor(deps.LLMClient, deps.Model, logger)

	// entityNameToID maps lowercased entity names to their UUIDs so that the
	// fact extractor (which returns names) can resolve them to UUIDs.
	entityNameToID := make(map[string]string)
	var allEntityNames []string
	var entitiesExtracted int

	for i := range memories {
		entities, extractErr := extractor.Extract(ctx, memories[i].Content)
		if extractErr != nil {
			logger.Warn("entity extraction failed, skipping", "error", extractErr)
			continue
		}
		for j := range entities {
			if upsertErr := deps.Store.UpsertEntity(ctx, entities[j]); upsertErr != nil {
				logger.Warn("upsert entity to store failed",
					"entity", entities[j].Name, "error", upsertErr)
				continue
			}
			if linkErr := deps.Store.LinkMemoryToEntity(ctx, entities[j].ID, memories[i].ID); linkErr != nil {
				logger.Warn("link entity to memory failed",
					"entity", entities[j].Name, "error", linkErr)
				continue
			}
			allEntityNames = append(allEntityNames, entities[j].Name)
			entityNameToID[strings.ToLower(entities[j].Name)] = entities[j].ID
			entitiesExtracted++
		}
	}

	// Skip fact extraction if no entities were found.
	if len(allEntityNames) == 0 {
		return Result{EntitiesExtracted: entitiesExtracted}
	}

	factExtractor := graphpkg.NewFactExtractor(deps.LLMClient, deps.Model, logger)
	var factsExtracted int

	for i := range memories {
		facts, factErr := factExtractor.Extract(ctx, memories[i].Content, allEntityNames)
		if factErr != nil {
			logger.Warn("fact extraction failed, skipping", "error", factErr)
			continue
		}
		for j := range facts {
			// FactExtractor returns entity names in SourceEntityID/TargetEntityID;
			// resolve to UUIDs before upserting.
			srcID, srcOK := entityNameToID[strings.ToLower(facts[j].SourceEntityID)]
			tgtID, tgtOK := entityNameToID[strings.ToLower(facts[j].TargetEntityID)]
			if !srcOK || !tgtOK {
				logger.Warn("fact references unknown entity, skipping",
					"source", facts[j].SourceEntityID, "target", facts[j].TargetEntityID,
					"source_resolved", srcOK, "target_resolved", tgtOK)
				continue
			}
			facts[j].SourceEntityID = srcID
			facts[j].TargetEntityID = tgtID

			if upsertErr := deps.GraphClient.UpsertFact(ctx, facts[j]); upsertErr != nil {
				logger.Warn("upsert fact failed",
					"fact_id", facts[j].ID, "error", upsertErr)
				continue
			}
			if linkErr := deps.GraphClient.AppendMemoryToFact(ctx, facts[j].ID, memories[i].ID); linkErr != nil {
				logger.Warn("link fact to memory failed",
					"fact_id", facts[j].ID, "error", linkErr)
			}
			factsExtracted++
		}
	}

	return Result{
		EntitiesExtracted: entitiesExtracted,
		FactsExtracted:    factsExtracted,
	}
}
