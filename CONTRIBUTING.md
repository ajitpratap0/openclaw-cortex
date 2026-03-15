# Contributing to OpenClaw Cortex

Thanks for your interest in contributing to OpenClaw Cortex! Here's how to get started.

## Prerequisites

- [Go 1.23+](https://go.dev/dl/)
- [Task](https://taskfile.dev/) (replaces Make)
- [Docker](https://www.docker.com/) (for Memgraph)
- [Ollama](https://ollama.ai/) with `nomic-embed-text` model
- [golangci-lint](https://golangci-lint.run/)

## Setup

```bash
# Clone the repo
git clone https://github.com/ajitpratap0/openclaw-cortex.git
cd openclaw-cortex

# Start Memgraph (graph + vector store)
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
| `task docker:up` | Start Memgraph |
| `task docker:down` | Stop Memgraph |
| `task clean` | Remove build artifacts |

## Project Structure

```
cortex/
├── cmd/openclaw-cortex/          # CLI entrypoint (cobra)
├── internal/            # Private application code
│   ├── config/          # Viper-based configuration
│   ├── models/          # Memory types, data structures
│   ├── embedder/        # Ollama HTTP embedding client
│   ├── memgraph/        # Memgraph Bolt client + mock
│   ├── llm/             # LLMClient interface + Anthropic + Gateway implementations
│   ├── indexer/         # Markdown scanner + chunker
│   ├── capture/         # Claude Haiku memory + entity extraction
│   ├── classifier/      # Heuristic memory classification
│   ├── recall/          # Multi-factor ranked recall + RRF
│   ├── lifecycle/       # TTL, decay, consolidation
│   └── hooks/           # OpenClaw hook integration
├── pkg/tokenizer/       # Token estimation + budgeting
├── skill/               # OpenClaw skill definition
├── Taskfile.yml         # Build, test, lint targets
├── Dockerfile           # Multi-stage production build
└── docker-compose.yml   # Local Memgraph
```

## Testing

- All tests use table-driven patterns with mocked dependencies
- Memgraph tests use `MockMemgraphClient` from `internal/memgraph/mock_client.go` (in-memory replacement — no live Memgraph needed for unit tests)
- Run with race detection: `task test`
- Short tests only (no external services): `go test -short -count=1 ./...`
- Coverage report: `task test:cover` → opens `coverage.html`
- Tests live in `tests/` (black-box package), not co-located with source packages

## Code Style

- Follow standard Go conventions
- Use `slog` for structured logging
- Keep interfaces minimal (defined in consumer packages)
- Always wrap errors: `fmt.Errorf("context: %w", err)` — never bare `err` returns
- Always propagate `context.Context` as the first argument to functions that call Memgraph, Ollama, or Claude
- XML-escape user/assistant content before interpolating into Claude prompts (see `internal/capture/capture.go`)
- golangci-lint config in `.golangci.yml`

## Good First Issues

Issues labeled [`good-first-issue`](https://github.com/ajitpratap0/openclaw-cortex/labels/good-first-issue)
are a great place to start. They are scoped, well-described, and don't require deep
knowledge of the whole codebase.
