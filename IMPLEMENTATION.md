# OpenClaw Cortex — Implementation Plan (Go)

> **Historical document.** This file captures the original design decisions made when the project was started. The current implementation has evolved significantly. For accurate architecture information, see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Overview
Hybrid layered memory system for OpenClaw agents. Combines file-based structured memory (existing) with vector-based and graph-based semantic memory (new) for compaction-proof, searchable, classified memory.

## Tech Stack (current as of v0.8.0)
- **Language:** Go 1.23+
- **Database:** Memgraph (Docker/K8s deployment) — single backend for both vector search and knowledge graph
- **Bolt driver:** `neo4j/neo4j-go-driver/v5` — Memgraph is Bolt-compatible
- **Embeddings:** Ollama HTTP API (`/api/embeddings` with `nomic-embed-text` model, 768 dimensions)
- **Extraction LLM:** Anthropic Claude Haiku via `llm.LLMClient` interface (supports Anthropic API or OpenClaw gateway)
- **CLI:** `cobra` for CLI commands
- **Config:** `viper` for config management
- **Integration:** OpenClaw skill + CLI binary

> **Note:** The original design used Qdrant (gRPC) as the vector store. Qdrant was replaced by Memgraph in v0.8.0 to consolidate vector and graph storage into a single service. All `store/` package references below now correspond to `internal/memgraph/`.

## Project Structure (current)
```
cortex/
├── README.md
├── IMPLEMENTATION.md           # This file (historical)
├── CONTRIBUTING.md
├── docs/
│   └── ARCHITECTURE.md         # Current architecture reference
├── go.mod
├── go.sum
├── Taskfile.yml
├── Dockerfile
├── docker-compose.yml          # Local Memgraph (memgraph/memgraph:latest)
├── cmd/
│   └── openclaw-cortex/
│       └── main.go             # CLI entrypoint
├── internal/
│   ├── config/
│   │   └── config.go           # Viper-based config (env vars + ~/.openclaw-cortex/config.yaml)
│   ├── models/
│   │   └── memory.go           # Memory types, scopes, data structures
│   ├── embedder/
│   │   ├── embedder.go         # Embedder interface
│   │   └── ollama.go           # Ollama HTTP API embedding provider
│   ├── memgraph/
│   │   ├── client.go           # Memgraph Bolt client — CRUD, vector search, graph traversal
│   │   ├── mock_client.go      # MockMemgraphClient for tests
│   │   └── fact_extractor.go   # Claude Haiku S-P-O triple extraction
│   ├── llm/
│   │   └── client.go           # LLMClient interface + AnthropicClient + GatewayClient
│   ├── indexer/
│   │   └── indexer.go          # File indexer — scan markdown, chunk, embed, store
│   ├── capture/
│   │   ├── capture.go          # LLM-based extraction — Claude Haiku extracts memories
│   │   ├── entity_extractor.go # Named entity extraction
│   │   └── conflict_detector.go # Contradiction detection
│   ├── classifier/
│   │   └── classifier.go       # Classify memories (rule/fact/episode/procedure/preference)
│   ├── recall/
│   │   ├── recall.go           # Multi-factor recall + RRF merge
│   │   └── reasoner.go         # Claude re-ranker (threshold-gated)
│   ├── lifecycle/
│   │   └── lifecycle.go        # TTL expiry, decay, consolidation, conflict resolution
│   └── hooks/
│       └── hooks.go            # OpenClaw hook integration
├── pkg/
│   └── tokenizer/
│       └── tokenizer.go        # Token counting + budget management
├── tests/
│   ├── store_test.go
│   ├── indexer_test.go
│   ├── capture_test.go
│   ├── recall_test.go
│   ├── classifier_test.go
│   └── lifecycle_test.go
└── skill/
    └── SKILL.md                # OpenClaw skill definition
```

## Memory Model

```go
type MemoryType string
const (
    MemoryTypeRule       MemoryType = "rule"        // Operating principles, hard constraints
    MemoryTypeFact       MemoryType = "fact"        // Declarative knowledge
    MemoryTypeEpisode    MemoryType = "episode"     // Specific events with temporal context
    MemoryTypeProcedure  MemoryType = "procedure"   // How to do things
    MemoryTypePreference MemoryType = "preference"  // User preferences
)

type MemoryScope string
const (
    ScopePermanent MemoryScope = "permanent"
    ScopeProject   MemoryScope = "project"
    ScopeSession   MemoryScope = "session"
    ScopeTTL       MemoryScope = "ttl"
)

type Memory struct {
    ID             string           `json:"id"`
    Type           MemoryType       `json:"type"`
    Scope          MemoryScope      `json:"scope"`
    Content        string           `json:"content"`
    Confidence     float64          `json:"confidence"`
    Source         string           `json:"source"`
    Tags           []string         `json:"tags"`
    Project        string           `json:"project,omitempty"`
    TTLSeconds     int64            `json:"ttl_seconds,omitempty"`
    CreatedAt      time.Time        `json:"created_at"`
    UpdatedAt      time.Time        `json:"updated_at"`
    LastAccessed   time.Time        `json:"last_accessed"`
    AccessCount    int64            `json:"access_count"`
    SupersedesID   string           `json:"supersedes_id,omitempty"`
    ValidFrom      time.Time        `json:"valid_from"`
    ValidTo        *time.Time       `json:"valid_to,omitempty"`
    Metadata       map[string]any   `json:"metadata,omitempty"`
}
```

## Implementation Phases (completed)

### Phase 1: Foundation
- Go module, Memgraph docker-compose, config management, memory data models, Ollama embedder, Memgraph Bolt client, file indexer, CLI scaffold

### Phase 2: Smart Capture
- Claude Haiku extraction, memory classifier, dedup/merge, `cortex capture` CLI

### Phase 3: Smart Recall
- Multi-factor recall scoring, token budget management, `cortex recall` CLI

### Phase 4: Integration & Advanced
- OpenClaw skill, lifecycle management, stats command, hook integration, Docker, Taskfile

### Phase 5: Intelligence (v0.3.0)
- Threshold-gated Claude re-ranking, session pre-warm cache, conflict engine, confidence reinforcement

### Phase 6: Memgraph Migration (v0.8.0)
- Replaced Qdrant with Memgraph; added entity extraction, fact extraction (S-P-O triples), graph-aware recall with RRF, temporal versioning, episodic extraction, LLM gateway abstraction

## Key Dependencies
- `github.com/neo4j/neo4j-go-driver/v5` — Memgraph Bolt client
- `github.com/anthropics/anthropic-sdk-go` — Claude API
- `github.com/spf13/cobra` — CLI framework
- `github.com/spf13/viper` — Config management
- `github.com/google/uuid` — UUID generation
- `github.com/stretchr/testify` — Test assertions

## CLI Interface
```bash
openclaw-cortex index [--path ~/.openclaw/workspace/memory/] [--watch]
openclaw-cortex search "query" [--type rule|fact|episode] [--limit 10] [--project name]
openclaw-cortex recall "current message" [--budget 2000] [--include-history]
openclaw-cortex capture --user "msg" --assistant "response" [--session-id X]
openclaw-cortex store "memory text" --type fact --scope permanent [--tags tag1,tag2]
openclaw-cortex forget <memory-id>
openclaw-cortex list [--type rule] [--scope permanent] [--limit 50]
openclaw-cortex stats
openclaw-cortex consolidate [--dry-run]
openclaw-cortex health
openclaw-cortex hook pre
openclaw-cortex hook post
openclaw-cortex hook install [--global] [--project name]
openclaw-cortex serve
openclaw-cortex mcp
```
