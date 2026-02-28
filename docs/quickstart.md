# Quickstart

This guide gets you from zero to a working memory system in about 5 minutes.

## Prerequisites

- Docker (for Qdrant)
- [Ollama](https://ollama.com/) installed and running
- An Anthropic API key (for `capture`; not required for `store`/`recall`/`search`)

## Step 1: Install the binary

```bash
curl -fsSL https://raw.githubusercontent.com/ajitpratap0/openclaw-cortex/main/scripts/install.sh | bash
```

Verify:

```bash
openclaw-cortex --version
```

Or build from source if you prefer:

```bash
git clone https://github.com/ajitpratap0/openclaw-cortex
cd openclaw-cortex
go build -o bin/openclaw-cortex ./cmd/openclaw-cortex
export PATH="$PWD/bin:$PATH"
```

## Step 2: Start Qdrant

```bash
# Using the provided docker-compose.yml
docker compose up -d
```

Qdrant will be available at:
- HTTP: `http://localhost:6333`
- gRPC: `localhost:6334` (used by openclaw-cortex)

## Step 3: Pull the embedding model

```bash
ollama pull nomic-embed-text
```

## Step 4: Store your first memory

```bash
openclaw-cortex store "Always run tests before merging to main" \
  --type rule \
  --scope permanent \
  --tags ci,testing
```

## Step 5: Recall memories

```bash
openclaw-cortex recall "What are the testing requirements?"
```

You should see the memory from Step 4 returned with a relevance score.

## Step 6: Capture from a conversation

For automatic memory extraction from conversations, set your API key:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

Then capture:

```bash
openclaw-cortex capture \
  --user "How should I handle errors in Go?" \
  --assistant "Always return errors explicitly. Use fmt.Errorf with %w to wrap them for unwrapping. Never use panic for expected error conditions."
```

This sends the conversation turn to Claude Haiku, which extracts structured memories and stores them automatically.

## Step 7: Wire up Claude Code hooks

To get automatic memory injection in every Claude Code conversation, add the hook configuration to `.claude/settings.json` in your project:

```json
{
  "hooks": {
    "PreTurn": [{
      "hooks": [{
        "type": "command",
        "command": "echo '{\"message\": \"{{HUMAN_TURN}}\", \"project\": \"my-project\", \"token_budget\": 2000}' | openclaw-cortex hook pre"
      }]
    }],
    "PostTurn": [{
      "hooks": [{
        "type": "command",
        "command": "echo '{\"user_message\": \"{{HUMAN_TURN}}\", \"assistant_message\": \"{{ASSISTANT_TURN}}\", \"session_id\": \"{{SESSION_ID}}\", \"project\": \"my-project\"}' | openclaw-cortex hook post"
      }]
    }]
  }
}
```

See [Claude Code Hooks](hooks.md) for full details and options.

## Verify everything works

```bash
# Check stats
openclaw-cortex stats

# Search memories
openclaw-cortex search "error handling"

# List recent memories
openclaw-cortex list --limit 10
```

## Configuration

The default configuration works out of the box if Qdrant and Ollama are running locally. To customize, create `~/.openclaw-cortex/config.yaml`:

```yaml
qdrant:
  host: localhost
  grpc_port: 6334

ollama:
  base_url: http://localhost:11434
  model: nomic-embed-text

memory:
  dedup_threshold: 0.92
  default_ttl_hours: 720
```

Or use environment variables:

```bash
export OPENCLAW_CORTEX_QDRANT_HOST=my-qdrant-host
export OPENCLAW_CORTEX_OLLAMA_BASE_URL=http://my-ollama:11434
```

## Next Steps

- [Architecture](architecture.md) — understand how recall scoring works
- [Claude Code Hooks](hooks.md) — automatic memory for every conversation
- [HTTP API](api.md) — integrate with other tools
- [MCP Server](mcp.md) — use from Claude Desktop
