# Contributing to Cortex

Thanks for your interest in contributing to Cortex! Here's how to get started.

## Prerequisites

- [Go 1.25+](https://go.dev/dl/)
- [Task](https://taskfile.dev/) (replaces Make)
- [Docker](https://www.docker.com/) (for Qdrant)
- [Ollama](https://ollama.ai/) with `nomic-embed-text` model
- [golangci-lint](https://golangci-lint.run/)

## Setup

```bash
# Clone the repo
git clone https://github.com/ajitpratap0/cortex.git
cd cortex

# Start Qdrant
task docker:up

# Pull embedding model
ollama pull nomic-embed-text

# Build
task build

# Run tests
task test
```

## Development Workflow

1. Create a feature branch from `main`
2. Make your changes
3. Run the full check suite:
   ```bash
   task fmt        # Format code
   task lint       # Run linter
   task test       # Run tests with race detector
   ```
4. Commit with conventional commit messages:
   - `feat:` new features
   - `fix:` bug fixes
   - `refactor:` code restructuring
   - `docs:` documentation changes
   - `test:` test additions/changes
   - `chore:` maintenance tasks
5. Open a PR against `main`

## Available Tasks

Run `task` to see all available commands:

| Task | Description |
|------|-------------|
| `task build` | Build the binary |
| `task test` | Run all tests with race detection |
| `task test:short` | Run short tests only |
| `task test:cover` | Generate coverage report |
| `task lint` | Run golangci-lint |
| `task fmt` | Format code (gofmt + goimports) |
| `task docker:up` | Start Qdrant |
| `task docker:down` | Stop Qdrant |
| `task clean` | Remove build artifacts |

## Project Structure

```
cortex/
├── cmd/cortex/          # CLI entrypoint (cobra)
├── internal/            # Private application code
│   ├── config/          # Viper-based configuration
│   ├── models/          # Memory types, data structures
│   ├── embedder/        # Ollama HTTP embedding client
│   ├── store/           # Qdrant gRPC store + mock
│   ├── indexer/         # Markdown scanner + chunker
│   ├── capture/         # Claude Haiku memory extraction
│   ├── classifier/      # Heuristic memory classification
│   ├── recall/          # Multi-factor ranked recall
│   ├── lifecycle/       # TTL, decay, consolidation
│   └── hooks/           # OpenClaw hook integration
├── pkg/tokenizer/       # Token estimation + budgeting
├── skill/               # OpenClaw skill definition
├── k8s/                 # Kubernetes manifests
├── Taskfile.yml         # Build, test, lint targets
├── Dockerfile           # Multi-stage production build
└── docker-compose.yml   # Local Qdrant
```

## Testing

- All tests use table-driven patterns with mocked dependencies
- Store tests use `mock_store.go` (in-memory Qdrant replacement)
- Run with race detection: `task test`
- Coverage report: `task test:cover` → opens `coverage.html`

## Code Style

- Follow standard Go conventions
- Use `slog` for structured logging
- Keep interfaces minimal (defined in consumer packages)
- golangci-lint config in `.golangci.yml`
