# MCP Server

OpenClaw Cortex implements the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/), allowing it to be used as a memory provider directly from Claude Desktop or any MCP-compatible client.

## Start the MCP server

```bash
openclaw-cortex mcp
```

The server communicates over stdio (stdin/stdout), which is the standard MCP transport. It does not bind to a network port.

## Configure in Claude Desktop

Add the following to your Claude Desktop configuration file:

**macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
**Linux**: `~/.config/claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "openclaw-cortex": {
      "command": "openclaw-cortex",
      "args": ["mcp"],
      "env": {
        "ANTHROPIC_API_KEY": "sk-ant-...",
        "OPENCLAW_CORTEX_QDRANT_HOST": "localhost"
      }
    }
  }
}
```

After adding this configuration, restart Claude Desktop. You should see "openclaw-cortex" listed as an available MCP server.

## Available Tools

### `remember`

Store a memory in the vector store.

**Parameters**:

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `content` | string | yes | The text content to remember |
| `type` | string | no | Memory type: `rule`, `fact`, `episode`, `procedure`, or `preference` (default: `fact`) |
| `scope` | string | no | Memory scope: `permanent`, `project`, `session`, or `ttl` (default: `permanent`) |
| `project` | string | no | Project name for project-scoped memories |
| `confidence` | number | no | Confidence score 0.0–1.0 (default: `1.0`) |

**Example**:

```
Remember that we always use snake_case for database column names in this project.
```

Claude will call `remember` with:
```json
{
  "content": "Always use snake_case for database column names",
  "type": "rule",
  "scope": "project",
  "project": "my-project"
}
```

**Response**:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "stored": true
}
```

---

### `recall`

Retrieve memories relevant to a message using semantic search and multi-factor ranking.

**Parameters**:

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `message` | string | yes | The query to recall memories for |
| `project` | string | no | Project context for scope boosting |
| `budget` | number | no | Token budget for returned context (default: `2000`) |

**Example**:

```
What do we know about naming conventions in this codebase?
```

**Response**:
```json
{
  "context": "--- Relevant Memories ---\n[rule] Always use snake_case for database columns...\n[preference] Prefer camelCase for Go variable names...\n",
  "memory_count": 2,
  "tokens_used": 67
}
```

---

### `forget`

Delete a memory by ID.

**Parameters**:

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `id` | string | yes | The UUID of the memory to delete |

**Response**:
```json
{
  "deleted": true
}
```

---

### `search`

Semantic search over memories. Returns raw results with similarity scores, without multi-factor re-ranking.

**Parameters**:

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `message` | string | yes | The query to search for |
| `limit` | number | no | Maximum number of results (default: `10`) |
| `project` | string | no | Filter results to this project |

**Response**:
```json
{
  "results": [
    {
      "memory": {
        "id": "550e8400-...",
        "content": "Always use snake_case for database columns",
        "type": "rule",
        "scope": "project"
      },
      "score": 0.934
    }
  ]
}
```

---

### `stats`

Get statistics about the memory collection.

**Parameters**: None

**Response**:
```json
{
  "total_memories": 87,
  "by_type": {
    "rule": 14,
    "fact": 42,
    "episode": 20,
    "procedure": 8,
    "preference": 3
  },
  "by_scope": {
    "permanent": 55,
    "project": 18,
    "session": 10,
    "ttl": 4
  }
}
```

## Usage Patterns

### Automatic context injection

Ask Claude to recall memories before working on a task:

```
Before we start, recall what you know about this project's architecture and coding standards.
```

### Saving important decisions

```
Remember that we decided to use PostgreSQL instead of MySQL because of better JSON support and the jsonb type.
```

### Searching past work

```
Search for any memories about authentication implementation.
```

## Requirements

Qdrant and Ollama must be running before starting the MCP server. The `remember` tool does not require `ANTHROPIC_API_KEY` — only the CLI `capture` command uses the Anthropic API.

```bash
docker compose up -d           # start Qdrant
ollama pull nomic-embed-text   # ensure embedding model is available
openclaw-cortex mcp            # start MCP server
```

## Troubleshooting

**"MCP server not appearing in Claude Desktop"**: Restart Claude Desktop after editing the config file. Check that `openclaw-cortex` is in your `PATH`.

**"Error: connection refused"**: Ensure Qdrant is running (`docker compose up -d`) and Ollama is running (`ollama serve`).

**"Tool call failed"**: Check that `OPENCLAW_CORTEX_QDRANT_HOST` and `OPENCLAW_CORTEX_OLLAMA_BASE_URL` point to the right hosts if you are not running services locally.
