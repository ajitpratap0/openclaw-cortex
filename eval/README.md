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
| `retrieved`    | Best recalled memory content |
| `exact_match`  | Whether `retrieved` contains `ground_truth` (case-insensitive) |
| `f1_score`     | Token-level F1 between `retrieved` and `ground_truth` |
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
| **Exact Match** | Fraction of questions where the best retrieved memory *contains* the ground-truth answer string (case-insensitive). |
| **Avg F1** | Mean token-level F1 across all questions. Rewards partial overlap, not just exact matches. |
| **Recall\@K** | Fraction of questions where *any* of the top-K retrieved memories contains the ground truth. |

### Competitor Context (from issue #88)

| System | LoCoMo EM | LongMemEval EM |
|--------|-----------|----------------|
| GPT-4 (RAG baseline) | ~58% | ~52% |
| MemGPT | ~61% | ~55% |
| A-MEM | ~63% | ~57% |
| **openclaw-cortex (target)** | **>60%** | **>55%** |

These numbers are from the academic literature and reflect full-scale
benchmark runs. The synthetic datasets here have 10 QA pairs each — they
are representative but not statistically equivalent to the full benchmarks.
Use them for regression detection and qualitative comparison, not
for publication-grade claims.

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
