# Benchmark Evaluation Harness

This directory contains a benchmark evaluation harness for `openclaw-cortex`
against two canonical long-term memory benchmarks: **LoCoMo** and
**LongMemEval**.

Both benchmarks use **synthetic datasets only** — no external downloads or
internet access required.

---

## Directory Layout

```
eval/
├── cmd/eval/main.go          # CLI entry point
├── locomo/
│   ├── dataset.go            # 10 synthetic LoCoMo QA pairs (3 conversations)
│   └── harness.go            # Ingest + recall + score runner
├── longmemeval/
│   ├── dataset.go            # 10 synthetic LongMemEval QA pairs
│   └── harness.go            # Ingest + recall + score runner
└── runner/
    └── runner.go             # Shared types, CortexClient, scoring functions
```

Tests for the eval harness live in the top-level `tests/` package:
`tests/eval_locomo_test.go`, `tests/eval_longmemeval_test.go`, `tests/eval_runner_test.go`.

---

## Prerequisites

| Dependency | Purpose |
|------------|---------|
| `openclaw-cortex` binary in `PATH` | Retrieval backend |
| Memgraph running on `bolt://localhost:7687` | Vector + graph storage |
| Ollama running on `http://localhost:11434` | Embeddings (`nomic-embed-text`) |

Start local services:

```bash
docker compose up -d
```

---

## How to Build and Run

### Run all benchmarks (default)

```bash
go run ./eval/cmd/eval --benchmark all --k 5
```

### Run a single benchmark

```bash
go run ./eval/cmd/eval --benchmark locomo --k 5
go run ./eval/cmd/eval --benchmark longmemeval --k 5
```

### Save JSON results to a file

```bash
go run ./eval/cmd/eval --benchmark all --output results.json
```

### Use a custom binary path or config

```bash
go run ./eval/cmd/eval \
  --binary /path/to/openclaw-cortex \
  --config ~/.openclaw-cortex/config.yaml \
  --benchmark all \
  --k 10
```

### Run unit tests only (no binary / services needed)

```bash
go test -short -count=1 ./tests/... -v
```

---

## Output Format

### JSON

The JSON output is an array of `BenchmarkSummary` objects:

```json
[
  {
    "name": "LoCoMo",
    "total_questions": 10,
    "exact_match_accuracy": 0.6,
    "avg_f1": 0.623,
    "recall_at_k": 0.8,
    "k": 5,
    "results": [ ... ]
  }
]
```

Each `results` entry is a `BenchmarkResult`:

| Field          | Description |
|----------------|-------------|
| `question_id`  | Synthetic dataset identifier (e.g. `locomo-A1`) |
| `question`     | The evaluation question |
| `ground_truth` | Expected answer substring |
| `retrieved`    | Oracle-selected best candidate — the top-k result with the highest token-F1 vs. `ground_truth` |
| `exact_match`  | Whether `retrieved` contains `ground_truth` (case-insensitive); oracle-selected, not top-ranked |
| `f1_score`     | Token-level F1 between `retrieved` and `ground_truth`; oracle-selected, not top-ranked |
| `recalled_at_k`| Whether any of the top-k memories contained `ground_truth` |

### Markdown Table

After the JSON block the tool prints a summary table:

```
| Benchmark      | Questions | Exact Match | Avg F1  | Recall@5 |
|----------------|-----------|-------------|---------|----------|
| LoCoMo         | 10        |       60.0% | 0.6230  |    80.0% |
| LongMemEval    | 10        |       50.0% | 0.5410  |    70.0% |
```

---

## Interpreting Results

### Metrics

| Metric | Definition |
|--------|-----------|
| **Exact Match** | Fraction of questions where the oracle-selected best candidate (highest token-F1 among top-K) *contains* the ground-truth string (case-insensitive). This is an upper-bound metric — it answers "could the answer be found anywhere in top-K?", not "did the system rank the answer first?". |
| **Avg F1** | Mean token-level F1 between the oracle-selected best candidate and ground truth. Upper-bound metric; same oracle selection as Exact Match. |
| **Recall\@K** | Fraction of questions where *any* of the top-K retrieved memories contains the ground truth. The canonical recall metric. |

### Baseline Results — v0.10.0 (2026-03-23)

Run against the full v0.10.0 binary with live Memgraph + Ollama (`nomic-embed-text`), k=5.

| Benchmark   | Questions | Exact Match | Avg F1 | Recall@5 | Recall failures |
|-------------|-----------|-------------|--------|----------|-----------------|
| LoCoMo      | 10        | **100.0%**  | 0.085  | **100%** | 0               |
| LongMemEval | 10        | **80.0%**   | 0.179  | **80%**  | 2               |

Full per-question results: [`eval/results_v0.10.0.json`](results_v0.10.0.json)

**Why Avg F1 is low despite high Exact Match:**
The oracle-selected candidate is a full conversation turn or fact sentence
(10–30 tokens), while the ground truth is a short keyword (1–3 tokens, e.g.
`"Go"`, `"blue-green"`, `"A100 GPUs"`). Token-F1 precision is low because the
candidate contains far more tokens than the ground truth. Exact Match and
Recall@K are the more meaningful metrics for this harness.

**LongMemEval miss analysis (2 / 10 failed):**

| QA ID  | Question | Ground Truth | Retrieved | Root cause |
|--------|----------|--------------|-----------|------------|
| lme-T1 | Diana's current job title? | `Senior Software Engineer` | `Software Engineer` (Jan 2024 entry) | Temporal supersession — the v1 memory (Jan 2024) was recalled instead of the superseding entry carrying the promotion; the updated title fact was not stored with a later `valid_from` |
| lme-T4 | Laura's latest ML model accuracy? | `89%` | `82% accuracy` (v1.0 entry) | Knowledge-update — the newer model version (89%) should have invalidated the old entry (82%); temporal ordering was not reflected in recall ranking |

Both failures are knowledge-update / temporal-supersession cases — exactly the
class of retrieval the [ROADMAP v0.11.0 items](../ROADMAP.md) target.

### Competitor Context (from issue #88)

| System | LoCoMo EM | LongMemEval EM |
|--------|-----------|----------------|
| GPT-4 (RAG baseline) | ~58% | ~52% |
| MemGPT | ~61% | ~55% |
| A-MEM | ~63% | ~57% |
| **openclaw-cortex v0.10.0 (synthetic, per-pair isolation)** | **100%** | **80%** |

> **Comparison caveat:** Published numbers accumulate full conversation history;
> this harness resets between pairs. Per-pair isolation removes cross-session
> distractors, so single-hop within-pair scores can be *higher* than published
> numbers; multi-session cross-turn questions would score lower. The synthetic
> dataset (10 pairs) is representative but not statistically equivalent.
> See *Isolation Design* below.

These numbers are from the academic literature and reflect full-scale
benchmark runs. The synthetic datasets here have 10 QA pairs each — they
are representative but not statistically equivalent to the full benchmarks.
Use them for regression detection and qualitative comparison, not
for publication-grade claims.

### Isolation Design and Comparison Caveats

**The harness resets the memory store before each QA pair.** This means
every question is evaluated against a freshly-empty store containing only
the facts/turns for that single pair. This differs from the published
LoCoMo and LongMemEval protocols, which accumulate conversation history
across all turns before running evaluation:

- **LoCoMo** is designed to stress *multi-session* recall — the model is
  expected to answer questions by drawing on a long, accumulated
  conversation history. Resetting between pairs removes cross-pair context,
  so questions that require facts from earlier conversations will always
  score zero here. The published LoCoMo numbers assume full history is
  available.
- **LongMemEval** similarly expects the full fact set to be in the store
  simultaneously.

**Why the harness resets anyway:** The reset ensures deterministic,
non-contaminating isolation between QA pairs — a required property for
CI/regression use. Each run produces identical scores regardless of
ordering or prior state. The trade-off is that scores reflect
*single-pair retrieval capability* rather than *long-horizon accumulation*.

**Consequence:** Scores from this harness will generally be lower than
published benchmarks for cross-turn questions. Do not compare raw numbers
directly against the academic literature. Use the scores as a stable
regression baseline — if scores drop between commits, retrieval quality
degraded; if they hold steady, the change is neutral.

---

## How to Reproduce

```bash
# 1. Build the binary
go build -o bin/openclaw-cortex ./cmd/openclaw-cortex

# 2. Start services
docker compose up -d

# 3. Run benchmarks
go run ./eval/cmd/eval --binary ./bin/openclaw-cortex --benchmark all --k 5 --output eval_results.json

# 4. Inspect per-question breakdown
cat eval_results.json | jq '.[] | {name, exact_match_accuracy, avg_f1, recall_at_k}'

# 5. Run unit tests (no services needed)
go test -short -count=1 ./tests/... -v
```

---

## Dataset Design

### LoCoMo (10 QA pairs, 3 conversations)

Conversation A — Alice (software engineer): programming language preferences,
job history.

Conversation B — Bob (infra lead): Kubernetes adoption, deployment strategies,
prior tooling.

Conversation C — Carol (ML engineer): framework choices, hardware, career path.

QA categories: `single-hop`, `multi-hop`, `temporal`.

### LongMemEval (10 QA pairs)

Covers knowledge that changes over time (job titles, databases, protocols) and
chained facts requiring two-step reasoning.

QA categories: `temporal`, `multi-hop`, `knowledge-update`.

Each `knowledge-update` pair has at least one fact with a `ValidTo` field
(a superseded memory) and a newer replacement fact.

---

## Adding New QA Pairs

1. Add entries to `locomo/dataset.go` or `longmemeval/dataset.go`.
2. Ensure each entry has a unique `ID`, non-empty `Question` and
   `GroundTruth`, and at least one `Conversation` turn / `Fact`.
3. Run the unit tests: `go test -short ./tests/...` — the size/structure
   assertions will catch common mistakes.

---

## Troubleshooting

**Binary hangs / per-call timeout fires**

Each `Reset`, `Store`, and `Recall` subprocess has a 30 s deadline
(`runner.defaultCallTimeout`). When the deadline fires, `cmd.Run()` returns an
error wrapping the killed-process signal; the harness counts it as a
`recallFailure` (or aborts, for `Reset`/`Store`). The failure log will include
`"context deadline exceeded"` to make the cause diagnosable.

If the binary consistently hangs in CI (e.g. Memgraph is slow to respond),
tune the per-call deadline via `CortexClient.CallTimeout`:

```go
client := runner.NewCortexClient(binaryPath, configPath)
client.CallTimeout = 2 * time.Minute // override 30 s default
```

The global `--timeout` flag bounds the entire benchmark run; `CallTimeout`
bounds each individual subprocess call.

**Scores lower than expected / `recall_failures > 0`**

Check that Memgraph and Ollama are running (`openclaw-cortex health`). A
non-zero `recall_failures` in the JSON output means some QA pairs scored zero
due to binary/connectivity errors and the aggregate metrics are deflated.
