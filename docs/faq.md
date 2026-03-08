# FAQ

## Do I need Qdrant?

Yes. Qdrant is the only supported vector store. Run it locally with `docker compose up -d`
(see [Quickstart](quickstart.md)) or use Qdrant Cloud.

## Can I use OpenAI embeddings instead of Ollama?

Yes. Set `embedder.provider: openai` in `~/.openclaw-cortex/config.yaml` and provide
`OPENAI_API_KEY`. The embedding dimension must match your Qdrant collection configuration
(`embedder.openai_dim`, default: 1536 for `text-embedding-3-small`).

## What is the difference between `recall` and `search`?

| Command | Ranking | Updates access metadata | Token budget |
|---------|---------|------------------------|-------------|
| `recall` | Multi-factor (similarity + recency + frequency + type + scope) | Yes | Yes |
| `search` | Raw cosine similarity only | No | No |

Use `recall` for injecting context into Claude. Use `search` for exploration and debugging.

## Does capture work offline?

No. `cortex capture` and the post-turn hook call the Anthropic API (Claude Haiku) for
memory extraction. If the API is unavailable, the hook exits cleanly with `{"stored": false}`
— Claude is never blocked.

## What happens if Qdrant or Ollama is down?

Both hooks exit with code 0 (graceful degradation):
- Pre-turn hook returns `{"context": "", "memory_count": 0, "tokens_used": 0}`
- Post-turn hook returns `{"stored": false}`

Claude continues working without memory assistance until services recover.

## How does the conflict engine work?

On each capture, `ConflictDetector` asks Claude whether the new memory contradicts any
existing similar memories. If yes, both are tagged with a shared `ConflictGroupID` and
`status="active"`. During `cortex consolidate`, the highest-confidence memory in each group
wins and the rest are marked `status="resolved"`.

See [Architecture — Conflict Engine](ARCHITECTURE.md#conflict-engine-v030) for details.

## What is confidence reinforcement?

When a new capture is semantically similar to an existing memory (0.80–0.92 similarity),
instead of storing a near-duplicate, the existing memory's `confidence` is incremented by
0.05 (capped at 1.0) and its `reinforced_count` increases. Frequently-observed facts
naturally converge toward maximum confidence.

## What is threshold-gated re-ranking?

When the top-4 recall scores are tightly clustered (spread ≤ 0.15), the ranking is
ambiguous and Claude is asked to re-rank them intelligently. This fires on ~10–30% of
recalls and is subject to a latency budget (100 ms for hooks, 3 s for CLI). On timeout,
the original ranking is used.

## How large can my collection be?

Qdrant scales to hundreds of millions of vectors. At typical memory sizes (~4–5 KB each),
100k memories use ~500 MB of storage. Search latency remains low (P50 < 20 ms) at this
scale. See [Benchmarks](benchmarks.md) for details.

## How do I migrate from v0.1.0 to v0.3.0?

No data migration is required. The Qdrant collection schema is backward-compatible — new
fields (`ConflictGroupID`, `reinforced_count`, etc.) are optional and default to zero
values for existing memories. Just update the binary.

## Can I run this as a shared service for a team?

v0.3.0 is designed for single-user or small-team use with a shared Qdrant instance.
Per-user namespace isolation is planned for v0.4.0. In the meantime, use the `project`
field to segment memories by team member or project.

## Does it work with Claude Desktop?

Yes, via the MCP server. Run `cortex mcp` and configure it in your Claude Desktop
`claude_desktop_config.json`. See [MCP Server](mcp.md) for setup instructions.

## What is the token budget?

The token budget limits how many tokens the recalled context occupies in Claude's system
prompt. Lower-ranked memories are dropped until the total fits. Default: 2000 tokens.
Configure per-call: `openclaw-cortex recall "query" --budget 4000`.

## Is my data sent anywhere?

- Memory content is sent to Ollama (local, no external call) for embedding
- Memory extraction (`capture`) sends conversation turns to the Anthropic API (Claude Haiku)
- Qdrant is self-hosted — your vectors never leave your infrastructure
- Re-ranking sends candidate memory content to the Anthropic API when triggered
