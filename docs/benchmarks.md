# Benchmarks

This page describes the performance characteristics of OpenClaw Cortex under typical workloads. These are estimates based on the architecture rather than formal benchmarks — actual numbers depend on hardware, network conditions, and data volume.

## Recall Latency

Recall involves two external service calls: Ollama for embedding and Qdrant for vector search. Both are required on the critical path.

| Component | P50 estimate | P99 estimate | Notes |
|-----------|-------------|-------------|-------|
| Ollama embedding (nomic-embed-text) | 15–30 ms | 80–150 ms | Local CPU; GPU would be 2–5x faster |
| Qdrant gRPC search (top-50) | 2–5 ms | 15–30 ms | Local Docker; scales with collection size |
| Multi-factor re-ranking (in-process) | <1 ms | <1 ms | Pure Go, no I/O |
| Token budget trimming | <1 ms | <1 ms | Pure Go, no I/O |
| **Total recall** | **20–40 ms** | **100–200 ms** | |

At 10k memories, Qdrant search latency increases modestly. At 100k memories, expect P50 to reach 10–20 ms for the vector search component. Qdrant is designed for millions of vectors.

### Factors that increase recall latency

- Slow hardware running Ollama (no GPU, older CPU)
- Network round-trip to remote Qdrant instance
- Large collections (>100k memories) without HNSW index tuning
- High token budgets that require processing many candidates

## Capture Latency

Capture involves an Anthropic API call, which dominates the latency.

| Component | P50 estimate | P99 estimate | Notes |
|-----------|-------------|-------------|-------|
| Claude Haiku extraction | 400–800 ms | 1.5–3 s | Anthropic API; varies with input length |
| Embedding extracted memories | 15–30 ms each | 80–150 ms each | One call per extracted memory |
| Dedup check (Qdrant) | 2–5 ms per memory | 15 ms per memory | FindDuplicates is a vector search |
| Upsert (Qdrant) | 1–3 ms per memory | 10 ms per memory | gRPC write |
| **Total capture (2–3 memories extracted)** | **500 ms – 1 s** | **2–4 s** | |

Capture runs in the post-turn hook so it does not block the user from seeing Claude's response.

## Embedding Throughput

For bulk indexing (`cortex index`), the bottleneck is Ollama embedding throughput.

| Scenario | Throughput |
|----------|-----------|
| Ollama on CPU (4-core) | ~20–40 chunks/second |
| Ollama on CPU (8-core) | ~40–80 chunks/second |
| Ollama on GPU (consumer) | ~200–500 chunks/second |

A memory directory with 100 markdown files and ~2000 chunks would take:
- CPU (4-core): ~50–100 seconds
- GPU: ~5–10 seconds

## Dedup Performance

Deduplication uses cosine similarity at a threshold of 0.92.

| Collection size | FindDuplicates latency (P50) |
|-----------------|------------------------------|
| 1k memories | <2 ms |
| 10k memories | 3–5 ms |
| 100k memories | 8–15 ms |

False positive rate at 0.92 threshold is low for distinct memories. Near-paraphrases of the same concept will typically exceed the threshold and be correctly deduplicated.

## Memory Store Size

Approximate storage per memory:

| Component | Size |
|-----------|------|
| Vector (768 float32 dimensions) | 3 KB |
| Payload (metadata JSON) | 0.5–2 KB |
| **Total per memory** | **~4–5 KB** |

| Collection size | Approximate storage |
|-----------------|---------------------|
| 1k memories | ~5 MB |
| 10k memories | ~50 MB |
| 100k memories | ~500 MB |
| 1M memories | ~5 GB |

Qdrant stores vectors in memory by default for fast access. For large collections, configure Qdrant's `on_disk` vector storage to reduce RAM requirements.

## What Affects Throughput

**Increases throughput**:
- Running Ollama with GPU acceleration
- Co-locating all services (Qdrant + Ollama + cortex on same machine)
- Batching index operations rather than calling `store` one at a time
- Increasing Qdrant HNSW `m` and `ef_construction` for better index quality at scale

**Decreases throughput**:
- Remote Qdrant or Ollama over WAN
- Very long memory content (>1000 tokens per memory)
- High `dedup_threshold` causing more candidate comparisons
- Enabling `--summarize` in `cortex index` (adds one Haiku call per section)

## Hook Overhead

The pre-turn hook adds ~20–40 ms (P50) to each conversation turn when all services are local. This is imperceptible in interactive use.

The post-turn hook runs asynchronously in the hook pipeline and does not block Claude's response delivery to the user.

## Reliability

Both hooks exit with code 0 even on failure, so service downtime does not break Claude. When Qdrant or Ollama is unavailable:

- Pre-turn hook: returns empty context in <1 ms (immediate fallback)
- Post-turn hook: logs a warning and returns `{"stored": false}` in <1 ms

Normal hook operation (all services healthy) never times out before 30 seconds, at which point the hook returns a graceful fallback.
