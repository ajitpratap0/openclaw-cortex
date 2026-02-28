# Claude Code Hooks

OpenClaw Cortex integrates with Claude Code via pre/post-turn hooks. The pre-turn hook injects relevant memories into the conversation context before each turn; the post-turn hook captures new memories after each turn.

## How It Works

```
User message received
        |
        v
[cortex hook pre]  <-- reads stdin JSON, writes stdout JSON
        |           -- embeds the message, searches Qdrant
        |           -- ranks with multi-factor scoring
        |           -- returns formatted context string
        v
Context injected into Claude's system prompt
        |
        v
Claude generates response
        |
        v
[cortex hook post] <-- reads stdin JSON, writes stdout JSON
        |           -- sends turn to Claude Haiku for extraction
        |           -- deduplicates against existing memories
        |           -- stores new memories in Qdrant
        v
Response delivered to user
```

Both hooks exit with code 0 even on error. If Qdrant or Ollama is unavailable, the hooks return empty output so Claude is never blocked. This is **graceful degradation**.

## Configuration

Add the hooks to `.claude/settings.json` in your project directory:

```json
{
  "hooks": {
    "PreToolUse": [],
    "PostToolUse": [],
    "PreTurn": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "echo '{\"message\": \"{{HUMAN_TURN}}\", \"project\": \"my-project\", \"token_budget\": 2000}' | openclaw-cortex hook pre"
          }
        ]
      }
    ],
    "PostTurn": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "echo '{\"user_message\": \"{{HUMAN_TURN}}\", \"assistant_message\": \"{{ASSISTANT_TURN}}\", \"session_id\": \"{{SESSION_ID}}\", \"project\": \"my-project\"}' | openclaw-cortex hook post"
          }
        ]
      }
    ]
  }
}
```

Replace `my-project` with your project name. Project-scoped memories are boosted in recall when the project name matches.

## Hook Input/Output Formats

### Pre-Turn Hook

**Input** (stdin JSON):

```json
{
  "message": "How should I handle database errors?",
  "project": "my-project",
  "token_budget": 2000
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `message` | string | required | The current user message |
| `project` | string | `""` | Project name for scope boosting and filtering |
| `token_budget` | int | `2000` | Maximum tokens to use for injected memories |

**Output** (stdout JSON):

```json
{
  "context": "--- Relevant Memories ---\n[rule] Always wrap database errors...\n",
  "memory_count": 3,
  "tokens_used": 142
}
```

The `context` string is injected into Claude's system prompt. When `memory_count` is 0, `context` is an empty string.

### Post-Turn Hook

**Input** (stdin JSON):

```json
{
  "user_message": "How should I structure this?",
  "assistant_message": "Use a layered architecture with clear interface boundaries...",
  "session_id": "session-abc123",
  "project": "my-project"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `user_message` | string | The user's message |
| `assistant_message` | string | Claude's response |
| `session_id` | string | Session identifier (used for session-scoped memory expiry) |
| `project` | string | Project name |

**Output** (stdout JSON):

```json
{
  "stored": true
}
```

`stored: false` means either no memories were extracted, dedup filtered them all, or an error occurred (graceful degradation).

## Quick Install

```bash
openclaw-cortex hook install
```

This command writes the hook configuration to `.claude/settings.json` in the current directory. It will create the file if it does not exist, or merge the hooks into an existing configuration.

## Environment Variables

The post-turn hook requires `ANTHROPIC_API_KEY` for memory extraction. If the key is not set, the hook exits cleanly with `{"stored": false}` and logs a warning.

```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

## Graceful Degradation

Both hooks are designed to never block Claude:

- If Qdrant is down: pre-hook returns `{"context": "", "memory_count": 0, "tokens_used": 0}`
- If Ollama is down: same empty response
- If `ANTHROPIC_API_KEY` is missing: post-hook skips capture, returns `{"stored": false}`
- If JSON decode fails: hook logs the error and returns the zero-value response
- All hooks exit with code 0 regardless of error

This means the system degrades gracefully — Claude still works, just without memory assistance until services recover.

## Security

User and assistant message content is XML-escaped before being interpolated into Claude Haiku prompts. This prevents prompt injection attacks where a user might include sequences like `</user><system>` in their messages.

Do not pass raw user input directly to Claude without going through the hook interface, which applies this escaping automatically.

## Filtering by Project

When `project` is specified in the hook input, memories are filtered to return only memories from that project (plus global memories). This prevents cross-project memory leakage — a memory from project A will not appear in project B's context.

```json
{
  "message": "Deploy checklist?",
  "project": "ecommerce-api",
  "token_budget": 2000
}
```

## Adjusting the Token Budget

The default token budget is 2000 tokens. For models with larger context windows or when you want more memory context, increase it:

```json
{
  "token_budget": 4000
}
```

The budget is enforced by trimming lower-ranked memories until the total fits. Higher-scored memories are always kept.
