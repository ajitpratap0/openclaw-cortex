# OpenClaw Cortex

**Persistent, semantically searchable memory for AI agents.**

OpenClaw Cortex is a hybrid memory system that gives AI agents long-term memory. It combines vector-based semantic search (Qdrant) with structured memory classification so agents can recall relevant context across conversations, sessions, and projects — without hitting token limits.

## Key Features

- **Semantic recall**: Vector similarity search powered by Ollama (`nomic-embed-text`, 768 dimensions)
- **Smart capture**: Claude Haiku extracts structured memories from conversation turns automatically
- **Multi-factor ranking**: Combines similarity, recency, frequency, memory type, and project scope into a single score
- **Token-aware output**: Recalled memories are trimmed to fit your token budget
- **Deduplication**: Cosine similarity dedup prevents storing near-identical memories
- **Lifecycle management**: TTL expiry, session decay, and consolidation keep the memory store clean
- **Claude Code integration**: Pre/post-turn hooks inject memories and capture new ones automatically
- **HTTP API + MCP server**: Integrate with any LLM stack or use directly from Claude Desktop

## Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/ajitpratap0/openclaw-cortex/main/scripts/install.sh | bash
```

Or build from source:

```bash
git clone https://github.com/ajitpratap0/openclaw-cortex
cd openclaw-cortex
go build -o bin/openclaw-cortex ./cmd/openclaw-cortex
```

## 3-Command Start

```bash
docker compose up -d              # start Qdrant vector store
ollama pull nomic-embed-text      # pull the embedding model
cortex capture "Always prefer explicit error handling over panics" --type rule
```

## Documentation Sections

| Section | Description |
|---------|-------------|
| [Quickstart](quickstart.md) | End-to-end setup in 5 minutes |
| [Architecture](architecture.md) | How the system works internally |
| [Claude Code Hooks](hooks.md) | Automatic memory injection for Claude Code |
| [HTTP API](api.md) | REST API reference with request/response schemas |
| [MCP Server](mcp.md) | Model Context Protocol integration for Claude Desktop |
| [Benchmarks](benchmarks.md) | Latency characteristics and throughput estimates |

## Requirements

| Dependency | Version | Purpose |
|------------|---------|---------|
| Go | 1.23+ | Build from source |
| Qdrant | 1.12+ | Vector storage (via Docker or hosted) |
| Ollama | any | Local embeddings (`nomic-embed-text`) |
| Anthropic API key | — | Memory extraction via Claude Haiku (capture only) |

## License

MIT — see [LICENSE](https://github.com/ajitpratap0/openclaw-cortex/blob/main/LICENSE).
