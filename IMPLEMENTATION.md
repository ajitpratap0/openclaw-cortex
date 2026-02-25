# Cortex — Implementation Plan (Go)

## Overview
Hybrid layered memory system for OpenClaw agents. Combines file-based structured memory (existing) with vector-based semantic memory (new) for compaction-proof, searchable, classified memory.

## Tech Stack
- **Language:** Go 1.25+
- **Vector DB:** Qdrant (Docker/K8s deployment)
- **Embeddings:** Ollama HTTP API (`/api/embeddings` with `nomic-embed-text` model)
- **Extraction LLM:** Anthropic Claude Haiku via Go SDK (`github.com/anthropics/anthropic-sdk-go`)
- **Storage:** Qdrant (vectors) + filesystem (markdown files, git-backed)
- **CLI:** `cobra` for CLI commands
- **Config:** `viper` for config management
- **Integration:** OpenClaw skill + CLI binary

## Project Structure
```
cortex/
├── README.md
├── IMPLEMENTATION.md
├── go.mod
├── go.sum
├── Taskfile.yml
├── Dockerfile
├── docker-compose.yml          # Local Qdrant
├── k8s/
│   └── qdrant.yaml             # K8s Qdrant deployment
├── cmd/
│   └── cortex/
│       └── main.go             # CLI entrypoint
├── internal/
│   ├── config/
│   │   └── config.go           # Viper-based config (env vars + ~/.cortex/config.yaml)
│   ├── models/
│   │   └── memory.go           # Memory types, scopes, visibility, data structures
│   ├── embedder/
│   │   ├── embedder.go         # Embedder interface
│   │   └── ollama.go           # Ollama HTTP API embedding provider
│   ├── store/
│   │   └── qdrant.go           # Qdrant client — CRUD, search, filter, dedup
│   ├── indexer/
│   │   └── indexer.go          # File indexer — scan markdown, chunk, embed, store
│   ├── capture/
│   │   └── capture.go          # LLM-based extraction — Claude Haiku extracts memories
│   ├── classifier/
│   │   └── classifier.go       # Classify memories (rule/fact/episode/procedure/preference)
│   ├── recall/
│   │   └── recall.go           # Multi-factor recall — similarity + recency + frequency + type priority
│   ├── lifecycle/
│   │   └── lifecycle.go        # TTL expiry, decay, consolidation, archival
│   └── hooks/
│       └── hooks.go            # OpenClaw hook integration design
├── pkg/
│   └── tokenizer/
│       └── tokenizer.go        # Simple token counting for budget management
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

type MemoryVisibility string
const (
    VisibilityPrivate   MemoryVisibility = "private"
    VisibilityShared    MemoryVisibility = "shared"
    VisibilitySensitive MemoryVisibility = "sensitive"
)

type Memory struct {
    ID           string           `json:"id"`
    Type         MemoryType       `json:"type"`
    Scope        MemoryScope      `json:"scope"`
    Visibility   MemoryVisibility `json:"visibility"`
    Content      string           `json:"content"`
    Confidence   float64          `json:"confidence"`
    Source       string           `json:"source"`        // "explicit" | "inferred" | "reinforced" | "file:<path>"
    Tags         []string         `json:"tags"`
    Project      string           `json:"project,omitempty"`
    TTLSeconds   int64            `json:"ttl_seconds,omitempty"`
    CreatedAt    time.Time        `json:"created_at"`
    UpdatedAt    time.Time        `json:"updated_at"`
    LastAccessed time.Time        `json:"last_accessed"`
    AccessCount  int64            `json:"access_count"`
    Metadata     map[string]any   `json:"metadata,omitempty"`
}
```

## Phase 1: Foundation
1. `go mod init github.com/ajitpratap0/cortex`
2. Deploy infrastructure (docker-compose.yml for Qdrant, k8s/qdrant.yaml)
3. Config management (internal/config/) — viper with env vars + config file
4. Memory data models (internal/models/)
5. Ollama embedder (internal/embedder/) — HTTP client for /api/embeddings
6. Qdrant store (internal/store/) — create collection, upsert, search, delete, filter
7. File indexer (internal/indexer/) — scan markdown dir, chunk by headers, embed, store
8. CLI scaffold with cobra (cmd/cortex/) — `cortex index`, `cortex search`
9. Unit tests for all

## Phase 2: Smart Capture
1. Anthropic Claude Haiku extraction (internal/capture/) — extract structured memories from conversations
2. Memory classifier (internal/classifier/) — classify by type using heuristics + LLM
3. Dedup/merge in store — cosine similarity check before insert, update if similar exists
4. CLI: `cortex capture --user "..." --assistant "..."`
5. Tests

## Phase 3: Smart Recall
1. Multi-factor recall (internal/recall/):
   - Semantic similarity (Qdrant distance score)
   - Recency decay (exponential decay on age)
   - Access frequency boost (log scale)
   - Type priority (rules=1.5, procedures=1.3, facts=1.0, episodes=0.8, preferences=0.7)
   - Scope match (project memories boosted when project context provided)
2. Token budget management (pkg/tokenizer/) — estimate tokens, truncate to budget
3. CLI: `cortex recall "current message" --budget 2000`
4. Tests

## Phase 4: Integration & Advanced
1. OpenClaw skill (skill/SKILL.md)
2. Lifecycle management (internal/lifecycle/) — TTL expiry, decay, consolidation
3. Stats command (cortex stats — collection size, type distribution, oldest/newest)
4. Hook integration design (internal/hooks/) — pre-turn and post-turn patterns
5. Comprehensive README with architecture diagram, setup, usage
6. Dockerfile for the cortex binary
7. Taskfile.yml: build, test, lint, docker:up, docker:down, index, search

## CLI Interface
```bash
cortex index [--path ~/.openclaw/workspace/memory/] [--watch]
cortex search "query" [--type rule|fact|episode] [--limit 10] [--project name]
cortex recall "current message" [--budget 2000] [--context json]
cortex capture --user "msg" --assistant "response" [--session-id X]
cortex store "memory text" --type fact --scope permanent [--tags tag1,tag2]
cortex forget <memory-id>
cortex list [--type rule] [--scope permanent] [--limit 50]
cortex stats
cortex consolidate [--dry-run]
```

## Key Dependencies
- `github.com/qdrant/go-client` — Qdrant gRPC client
- `github.com/anthropics/anthropic-sdk-go` — Claude API
- `github.com/spf13/cobra` — CLI framework
- `github.com/spf13/viper` — Config management
- `github.com/google/uuid` — UUID generation
- `google.golang.org/grpc` — gRPC for Qdrant
- `github.com/stretchr/testify` — Test assertions

## Testing Strategy
- Unit tests with mocked Qdrant (interface-based, inject mock store)
- Integration tests requiring Qdrant (skip in CI without docker, run locally)
- Table-driven tests for classifier, recall scoring, lifecycle rules
- End-to-end: index → capture → recall pipeline test

## Success Criteria
1. `cortex search` returns relevant memories from existing 200KB+ memory files
2. `cortex capture` correctly extracts and classifies facts from conversation text
3. `cortex recall` returns well-ranked memories within a token budget
4. All tests pass, `golangci-lint` clean
5. Single binary, deployable via `go install`
