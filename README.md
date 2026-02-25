# Cortex

Hybrid layered memory system for OpenClaw AI agents. Combines file-based structured memory with vector-based semantic memory for compaction-proof, searchable, classified memory.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                    CLI / Hooks                       │
│  index  search  recall  capture  store  consolidate  │
├─────────────┬──────────────┬────────────────────────┤
│   Indexer   │   Capturer   │      Recaller          │
│  (markdown  │  (Claude     │  (multi-factor         │
│   scanner)  │   Haiku)     │   ranking)             │
├─────────────┴──────┬───────┴────────────────────────┤
│     Classifier     │         Lifecycle              │
│  (heuristic +      │  (TTL, decay,                  │
│   LLM typing)      │   consolidation)               │
├────────────────────┴────────────────────────────────┤
│                   Embedder                           │
│            (Ollama / nomic-embed-text)               │
├─────────────────────────────────────────────────────┤
│                     Store                            │
│              (Qdrant gRPC client)                    │
└─────────────────────────────────────────────────────┘
```

## Features

- **Semantic Search**: Vector-based similarity search over all memories
- **Smart Capture**: LLM-powered extraction of structured memories from conversations
- **Classification**: Automatic categorization into rules, facts, episodes, procedures, preferences
- **Multi-Factor Recall**: Ranking by similarity, recency, frequency, type priority, and scope
- **Token Budget**: Fit recalled memories within configurable token limits
- **Deduplication**: Cosine similarity-based duplicate detection before insertion
- **Lifecycle Management**: TTL expiry, session decay, consolidation
- **OpenClaw Integration**: Pre/post-turn hooks for seamless agent integration

## Quick Start

### Prerequisites

- Go 1.25+
- [Task](https://taskfile.dev/) (`brew install go-task`)
- Docker (for Qdrant)
- Ollama with `nomic-embed-text` model
- Anthropic API key (for capture feature)

### Setup

```bash
# Start Qdrant
task docker:up

# Pull the embedding model
ollama pull nomic-embed-text

# Build cortex
task build

# Set API key for capture feature
export ANTHROPIC_API_KEY=sk-ant-...
```

### Usage

```bash
# Index existing memory files
cortex index --path ~/.openclaw/workspace/memory/

# Search memories
cortex search "how to deploy to production"
cortex search "error handling" --type rule --limit 5

# Store a new memory
cortex store "Always run tests before deploying" --type rule --scope permanent --tags ci,deployment

# Recall memories for a conversation turn (with token budget)
cortex recall "How should I structure the database schema?" --budget 2000 --project myapp

# Capture memories from a conversation
cortex capture --user "What's the best way to handle errors in Go?" \
               --assistant "In Go, always check error returns explicitly..."

# View stats
cortex stats

# Run lifecycle management
cortex consolidate
cortex consolidate --dry-run

# Delete a memory
cortex forget <memory-id>

# List memories with filters
cortex list --type rule --scope permanent --limit 20
```

## Configuration

Configuration is loaded from (in order of precedence):
1. Environment variables (prefixed with `CORTEX_`)
2. `~/.cortex/config.yaml`
3. Built-in defaults

### Config File

```yaml
qdrant:
  host: localhost
  grpc_port: 6334
  http_port: 6333
  collection: cortex_memories
  use_tls: false

ollama:
  base_url: http://localhost:11434
  model: nomic-embed-text

claude:
  model: claude-haiku-4-5-20241022

memory:
  memory_dir: ~/.openclaw/workspace/memory/
  chunk_size: 512
  chunk_overlap: 64
  dedup_threshold: 0.92
  default_ttl_hours: 720
  vector_dimension: 768

logging:
  level: info
  format: text
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `ANTHROPIC_API_KEY` | Anthropic API key for capture | — |
| `CORTEX_QDRANT_HOST` | Qdrant hostname | `localhost` |
| `CORTEX_QDRANT_GRPC_PORT` | Qdrant gRPC port | `6334` |
| `CORTEX_OLLAMA_BASE_URL` | Ollama base URL | `http://localhost:11434` |

## Memory Model

### Types

| Type | Description | Recall Priority |
|------|-------------|-----------------|
| `rule` | Operating principles, hard constraints | 1.5x |
| `procedure` | How-to steps, processes, workflows | 1.3x |
| `fact` | Declarative knowledge, definitions | 1.0x |
| `episode` | Specific events with temporal context | 0.8x |
| `preference` | User preferences, style choices | 0.7x |

### Scopes

| Scope | Behavior |
|-------|----------|
| `permanent` | Persists indefinitely |
| `project` | Boosted when project context matches |
| `session` | Auto-expires after 24h without access |
| `ttl` | Expires after configured TTL |

### Recall Scoring

Final score = weighted combination of:
- **Similarity** (0.5): Cosine distance from Qdrant
- **Recency** (0.2): Exponential decay, 7-day half-life
- **Frequency** (0.1): Log-scale access count
- **Type Boost** (0.1): Priority multiplier per type
- **Scope Boost** (0.1): Project-match bonus

## Deployment

### Docker

```bash
# Run Qdrant
docker compose up -d

# Build cortex image
docker build -t cortex:latest .

# Run cortex
docker run --rm cortex:latest search "query"
```

### Kubernetes

```bash
kubectl apply -f k8s/qdrant.yaml
```

Creates a StatefulSet with PVC in the `cortex` namespace.

## Development

```bash
# Run tests
task test

# Run tests with coverage
task test:cover

# Lint
task lint

# Format
task fmt

# Build
task build
```

## Project Structure

```
cortex/
├── cmd/cortex/main.go          # CLI entrypoint (cobra)
├── internal/
│   ├── config/config.go        # Viper-based configuration
│   ├── models/memory.go        # Memory types, scopes, data structures
│   ├── embedder/
│   │   ├── embedder.go         # Embedder interface
│   │   └── ollama.go           # Ollama HTTP API implementation
│   ├── store/
│   │   ├── store.go            # Store interface
│   │   ├── qdrant.go           # Qdrant gRPC implementation
│   │   └── mock_store.go       # In-memory mock for testing
│   ├── indexer/indexer.go      # Markdown file scanner + chunker
│   ├── capture/capture.go     # Claude Haiku memory extraction
│   ├── classifier/classifier.go # Heuristic memory classification
│   ├── recall/recall.go       # Multi-factor ranked recall
│   ├── lifecycle/lifecycle.go # TTL, decay, consolidation
│   └── hooks/hooks.go         # OpenClaw hook integration
├── pkg/tokenizer/tokenizer.go # Token estimation + budgeting
├── tests/                      # Comprehensive test suite
├── skill/SKILL.md              # OpenClaw skill definition
├── k8s/qdrant.yaml            # Kubernetes deployment
├── docker-compose.yml          # Local Qdrant
├── Dockerfile                  # Multi-stage build
├── Taskfile.yml                # Build, test, lint targets (go-task)
└── .golangci.yml              # Linter configuration
```

## Tech Stack

- **Go 1.25+** with structured logging (`slog`)
- **Qdrant** vector database (gRPC via `github.com/qdrant/go-client`)
- **Ollama** for embeddings (`nomic-embed-text`, 768 dimensions)
- **Claude Haiku** for memory extraction (`github.com/anthropics/anthropic-sdk-go`)
- **Cobra** CLI framework
- **Viper** configuration management
- **Testify** for test assertions
