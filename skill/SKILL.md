# Cortex — Semantic Memory Skill

## Description
Cortex provides hybrid layered memory for OpenClaw AI agents. It combines file-based structured memory with vector-based semantic memory for compaction-proof, searchable, classified memory across sessions.

## Capabilities
- **Index**: Scan and embed markdown memory files into vector store
- **Search**: Find memories by semantic similarity with type/scope filters
- **Recall**: Multi-factor ranked retrieval within token budgets
- **Capture**: Extract structured memories from conversation turns using Claude Haiku
- **Store**: Directly store classified memories with metadata
- **Lifecycle**: TTL expiry, session decay, consolidation

## Integration Points

### Pre-Turn Hook
Before each agent turn, recall relevant memories:
```bash
cortex recall "$CURRENT_MESSAGE" --budget 2000 --project "$PROJECT" --context json
```
Inject the output into the system prompt as context.

### Post-Turn Hook
After each agent turn, capture new memories:
```bash
cortex capture --user "$USER_MSG" --assistant "$ASSISTANT_MSG" --session-id "$SESSION_ID"
```

### Periodic Maintenance
Run lifecycle management periodically:
```bash
cortex consolidate
```

## Memory Types
| Type | Description | Priority |
|------|-------------|----------|
| rule | Operating principles, hard constraints | 1.5x |
| procedure | How-to steps, processes | 1.3x |
| fact | Declarative knowledge | 1.0x |
| episode | Specific events with temporal context | 0.8x |
| preference | User preferences, style choices | 0.7x |

## Memory Scopes
| Scope | Description |
|-------|-------------|
| permanent | Persists indefinitely |
| project | Scoped to a specific project |
| session | Expires after session ends |
| ttl | Expires after configured time |

## Configuration
Set via environment variables or `~/.cortex/config.yaml`:
```yaml
qdrant:
  host: localhost
  grpc_port: 6334
ollama:
  base_url: http://localhost:11434
  model: nomic-embed-text
claude:
  model: claude-haiku-4-5-20241022
memory:
  dedup_threshold: 0.92
  chunk_size: 512
```

## Requirements
- Qdrant running (Docker or K8s)
- Ollama with nomic-embed-text model
- Anthropic API key (for capture)
