# HTTP API Reference

OpenClaw Cortex exposes a REST API for integrating with external tools and LLM pipelines.

## Start the server

```bash
openclaw-cortex serve --port 8080
```

With an auth token:

```bash
openclaw-cortex serve --port 8080 --auth-token my-secret-token
```

## Authentication

When `--auth-token` is set, all endpoints except `GET /healthz` require a `Bearer` token:

```
Authorization: Bearer my-secret-token
```

If no `--auth-token` is set, auth is disabled and all endpoints are open.

## Base URL

```
http://localhost:8080
```

---

## Endpoints

### `GET /healthz`

Health check. No authentication required.

**Response** `200 OK`:

```json
{
  "status": "ok"
}
```

---

### `POST /v1/remember`

Store a memory. Embeds the content and upserts it to the vector store.

**Request body**:

```json
{
  "content": "Always use context propagation when calling external services",
  "type": "rule",
  "scope": "permanent",
  "tags": ["go", "best-practices"],
  "project": "my-project",
  "confidence": 0.95
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `content` | string | yes | — | The text to remember |
| `type` | string | no | `fact` | One of: `rule`, `fact`, `episode`, `procedure`, `preference` |
| `scope` | string | no | `permanent` | One of: `permanent`, `project`, `session`, `ttl` |
| `tags` | []string | no | `[]` | Arbitrary labels |
| `project` | string | no | `""` | Project name (used with `scope=project`) |
| `confidence` | float64 | no | `1.0` | Confidence score 0.0–1.0; values below 0.5 are rejected |

**Response** `200 OK`:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "stored": true
}
```

**Response** `409 Conflict` (duplicate detected):

```json
{
  "id": "",
  "stored": false
}
```

**Error responses**: `400 Bad Request`, `401 Unauthorized`, `500 Internal Server Error`

---

### `POST /v1/recall`

Retrieve memories relevant to a message, ranked by multi-factor scoring and trimmed to a token budget.

**Request body**:

```json
{
  "message": "How should I handle database connection errors?",
  "project": "my-project",
  "budget": 2000
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `message` | string | yes | — | The query to find relevant memories for |
| `project` | string | no | `""` | Filters memories to this project scope |
| `budget` | int | no | `2000` | Maximum tokens in the returned context |

**Response** `200 OK`:

```json
{
  "context": "--- Relevant Memories ---\n[rule] Always wrap database errors with fmt.Errorf...\n[procedure] On connection failure: retry with exponential backoff...\n",
  "memory_count": 2,
  "tokens_used": 89
}
```

| Field | Type | Description |
|-------|------|-------------|
| `context` | string | Formatted memory context, ready to inject into a system prompt |
| `memory_count` | int | Number of memories included |
| `tokens_used` | int | Estimated token count of `context` |

---

### `GET /v1/memories/{id}`

Retrieve a single memory by ID.

**Path parameters**:

| Parameter | Description |
|-----------|-------------|
| `id` | UUID of the memory |

**Response** `200 OK`:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "content": "Always use context propagation when calling external services",
  "type": "rule",
  "scope": "permanent",
  "tags": ["go", "best-practices"],
  "project": "my-project",
  "confidence": 0.95,
  "created_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-15T10:30:00Z",
  "last_accessed": "2024-01-16T14:22:00Z",
  "access_count": 7
}
```

**Error responses**: `400 Bad Request`, `401 Unauthorized`, `404 Not Found`, `500 Internal Server Error`

---

### `DELETE /v1/memories/{id}`

Delete a memory by ID.

**Path parameters**:

| Parameter | Description |
|-----------|-------------|
| `id` | UUID of the memory |

**Response** `200 OK`:

```json
{
  "deleted": true
}
```

**Error responses**: `400 Bad Request`, `401 Unauthorized`, `404 Not Found`, `500 Internal Server Error`

---

### `POST /v1/search`

Search memories by semantic similarity. Unlike `/v1/recall`, this returns raw search results without multi-factor re-ranking and does not update access metadata.

**Request body**:

```json
{
  "message": "error handling patterns",
  "limit": 10,
  "project": "my-project"
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `message` | string | yes | — | The search query |
| `limit` | int | no | `10` | Maximum number of results |
| `project` | string | no | `""` | Filter results to this project |

**Response** `200 OK`:

```json
{
  "results": [
    {
      "memory": {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "content": "Always wrap errors with context using fmt.Errorf",
        "type": "rule",
        "scope": "permanent",
        "confidence": 0.95
      },
      "score": 0.923
    },
    {
      "memory": {
        "id": "660e8400-e29b-41d4-a716-446655440001",
        "content": "Use sentinel errors for known error conditions",
        "type": "rule",
        "scope": "permanent",
        "confidence": 0.88
      },
      "score": 0.871
    }
  ]
}
```

---

### `GET /v1/stats`

Get statistics about the memory store.

**Response** `200 OK`:

```json
{
  "total_memories": 142,
  "by_type": {
    "rule": 28,
    "fact": 67,
    "episode": 31,
    "procedure": 11,
    "preference": 5
  },
  "by_scope": {
    "permanent": 95,
    "project": 22,
    "session": 18,
    "ttl": 7
  }
}
```

---

## Error Format

All error responses use the same format:

```json
{
  "error": "description of what went wrong"
}
```

Common status codes:

| Code | Meaning |
|------|---------|
| `400` | Bad request — missing required fields or invalid values |
| `401` | Unauthorized — missing or invalid Bearer token |
| `404` | Not found — memory ID does not exist |
| `409` | Conflict — duplicate memory detected |
| `500` | Internal server error — Qdrant or Ollama unavailable |

## Request Size Limit

Request bodies are limited to 1 MB.

## Example: cURL

```bash
# Store a memory
curl -X POST http://localhost:8080/v1/remember \
  -H "Authorization: Bearer my-token" \
  -H "Content-Type: application/json" \
  -d '{"content": "Always validate input at the boundary", "type": "rule"}'

# Recall relevant memories
curl -X POST http://localhost:8080/v1/recall \
  -H "Authorization: Bearer my-token" \
  -H "Content-Type: application/json" \
  -d '{"message": "How do I handle user input?", "budget": 1000}'

# Get stats
curl http://localhost:8080/v1/stats \
  -H "Authorization: Bearer my-token"
```
