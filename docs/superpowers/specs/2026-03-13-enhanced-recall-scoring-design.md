# Enhanced Recall Scoring Design

## Goal

Expand the recall ranking formula from 5 factors to 8 weighted components plus 2 multiplicative penalties, incorporating confidence, reinforcement, and tag affinity signals that already exist on memories but are currently ignored by the scorer.

## Background

The current scoring formula is:

```
0.5*similarity + 0.2*recency + 0.1*frequency + 0.1*typeBoost + 0.1*scopeBoost
```

Several fields tracked on every memory never enter ranking:

- **Confidence** (0.0-1.0): set on capture, bumped +0.05 on reinforcement. A 0.95 rule and a 0.5 auto-captured episode score identically if other factors match.
- **ReinforcedCount / ReinforcedAt**: incremented when a near-duplicate is captured (0.80 cosine threshold). A memory reinforced 10 times scores the same as one stored once.
- **Tags**: stored but never compared against the query.
- **SupersedesID**: chains memory history but superseded memories compete equally with their replacements.
- **ConflictStatus**: used only in `FormatWithConflictAnnotations` formatting, not in ranking.

## New Formula

### Weighted Components (sum = 1.0)

```
0.35*similarity + 0.15*recency + 0.10*frequency +
0.10*typeBoost  + 0.08*scopeBoost + 0.10*confidence +
0.07*reinforcement + 0.05*tagAffinity
```

### Multiplicative Penalties (applied after weighted sum)

```
finalScore = weightedSum * supersessionPenalty * conflictPenalty
```

- `supersessionPenalty`: 0.3 if this memory was superseded by another result, 1.0 otherwise
- `conflictPenalty`: 0.8 if `ConflictStatus == "active"`, 1.0 otherwise

Penalties stack: a superseded memory in active conflict scores `weightedSum * 0.24`.

## Component Specifications

### 1. Confidence Score (weight: 0.10)

```
confidenceScore = mem.Confidence  // already [0,1]
```

No transformation needed. The field is set by Claude Haiku during capture (range 0.0-1.0), and `UpdateReinforcement` bumps it by 0.05 (capped at 1.0).

**Legacy memories (Confidence == 0):** Memories captured before confidence tracking may have a zero-value `Confidence`. To avoid silently penalizing them, treat `Confidence == 0` as "unknown" and substitute a neutral value of 0.7 (the median of observed capture confidences). This applies only in the scoring function — the stored value is not mutated.

### 2. Reinforcement Score (weight: 0.07)

```
reinforcementScore = min(1.0, log2(reinforcedCount + 1) / 5.0)
```

Note: `ReinforcedCount` is `int` (not `int64` like `AccessCount`). Convert to `float64` before the log computation.

Saturates at ~32 reinforcements. The `/5.0` divisor is chosen because reinforcement is rarer than access (requires 0.80 cosine match on re-encounter). Compare to frequency which uses `/10.0` and needs ~1024 accesses to saturate.

| ReinforcedCount | Score |
|-----------------|-------|
| 0               | 0.00  |
| 1               | 0.20  |
| 3               | 0.40  |
| 7               | 0.60  |
| 15              | 0.80  |
| 31              | 1.00  |

### 3. Tag Affinity Score (weight: 0.05)

```
tagAffinityScore = matchingTags / len(mem.Tags)  // 0 if mem has no tags
```

Where `matchingTags` is the count of the memory's tags that match at least one query word. The denominator is `len(mem.Tags)` (total tags on the memory), not the number of query words.

**Matching rules:**
- The query is split into words (lowercased, whitespace-delimited).
- Each tag is lowercased.
- A **single-word tag** matches if any query word equals it exactly. No substring matching (e.g., query word "go" does not match tag "golang").
- A **multi-word tag** (e.g., `"ci pipeline"`) matches if *all* words in the tag appear in the query words. Tags are assumed to be short (1-3 words); existing capture code produces single-word tags in practice, but this handles edge cases safely.

**Examples** (query words after lowercasing shown in brackets):

- Query "GoSQLX CI pipeline" `[gosqlx, ci, pipeline]`, tags `["gosqlx", "ci"]` -> 2/2 = 1.0
- Query "GoSQLX CI pipeline" `[gosqlx, ci, pipeline]`, tags `["gosqlx", "ci", "docker"]` -> 2/3 = 0.67
- Query "GoSQLX CI pipeline" `[gosqlx, ci, pipeline]`, tags `["python", "ml"]` -> 0/2 = 0.0
- Query "GoSQLX CI pipeline" `[gosqlx, ci, pipeline]`, tags `[]` -> 0.0 (no tags = no boost)
- Query "GoSQLX CI pipeline" `[gosqlx, ci, pipeline]`, tags `["ci pipeline"]` -> 1/1 = 1.0 (multi-word tag, all words found in query)

### 4. Supersession Penalty (multiplicative: 0.3)

`SupersedesID` means "this memory supersedes X" — it is set on the *newer* memory pointing to the older one it replaced. The *older* memory itself has no field marking it as superseded.

During `Rank()`, build a set of superseded IDs by scanning all results:

```
supersededIDs = set()
for each result:
    if result.Memory.SupersedesID != "":
        supersededIDs.add(result.Memory.SupersedesID)

for each result:
    if result.Memory.ID in supersededIDs:
        result.SupersessionPenalty = 0.3
    else:
        result.SupersessionPenalty = 1.0
```

This is O(n) with a map — trivial for typical result sets of 10-50 items. The penalty only applies when both the superseding and superseded memory appear in the same result set. If only the old memory appears (the new one wasn't relevant to the query), no penalty is applied — the old memory is the best available.

**Chains:** For A supersedes B supersedes C, the set-based approach handles chains correctly. A adds B to the set, B adds C to the set. In the second pass, B and C are both found in the set and penalized. A (the newest) is not penalized. Each memory is penalized at most once (×0.3) regardless of chain depth.

### 5. Conflict Penalty (multiplicative: 0.8)

```
if mem.ConflictStatus == "active":
    conflictPenalty = 0.8
else:
    conflictPenalty = 1.0
```

Mild demotion for unresolved conflicts. Resolved conflicts (`ConflictStatus == "resolved"` or `""`) are not penalized. The formatting layer (`FormatWithConflictAnnotations`) already annotates active conflicts for the user — this ensures they also rank slightly lower.

## Deferred: Temporal Relevance

Time-of-day/day-of-week awareness is deferred because:

1. It requires external configuration (timezone, work hours schedule, type-to-timeperiod mappings) that no other boost needs.
2. The type-to-timeperiod mapping is subjective and varies per user.
3. It can be layered on later as an optional post-scoring multiplier without changing the weighted sum formula.

The infrastructure built here (configurable weights, expanded RecallResult) makes temporal a clean follow-up.

## API Changes

### Rank() Signature

```go
// Before
func (r *Recaller) Rank(results []models.SearchResult, project string) []models.RecallResult

// After
func (r *Recaller) Rank(results []models.SearchResult, project string, query string) []models.RecallResult
```

One new parameter. All three call sites have the raw query string available:
- `cmd/openclaw-cortex/cmd_recall.go`: `query := args[0]`
- `internal/api/server.go`: `req.Message`
- `internal/hooks/hooks.go`: `input.Message`

### RecallResult Struct

```go
type RecallResult struct {
    Memory              Memory  `json:"memory"`
    SimilarityScore     float64 `json:"similarity_score"`
    RecencyScore        float64 `json:"recency_score"`
    FrequencyScore      float64 `json:"frequency_score"`
    TypeBoost           float64 `json:"type_boost"`
    ScopeBoost          float64 `json:"scope_boost"`
    ConfidenceScore     float64 `json:"confidence_score"`
    ReinforcementScore  float64 `json:"reinforcement_score"`
    TagAffinityScore    float64 `json:"tag_affinity_score"`
    SupersessionPenalty float64 `json:"supersession_penalty"`
    ConflictPenalty     float64 `json:"conflict_penalty"`
    FinalScore          float64 `json:"final_score"`
}
```

New fields are additive to the JSON contract. The TypeScript plugin won't break on extra fields — we update the `RecallResult` interface for completeness.

### Weights Struct

```go
type Weights struct {
    Similarity    float64 `json:"similarity" mapstructure:"similarity"`
    Recency       float64 `json:"recency" mapstructure:"recency"`
    Frequency     float64 `json:"frequency" mapstructure:"frequency"`
    TypeBoost     float64 `json:"type_boost" mapstructure:"type_boost"`
    ScopeBoost    float64 `json:"scope_boost" mapstructure:"scope_boost"`
    Confidence    float64 `json:"confidence" mapstructure:"confidence"`
    Reinforcement float64 `json:"reinforcement" mapstructure:"reinforcement"`
    TagAffinity   float64 `json:"tag_affinity" mapstructure:"tag_affinity"`
}

// Penalty constants (not configurable via weights — these are categorical)
const (
    SupersessionPenaltyFactor = 0.3
    ConflictPenaltyFactor     = 0.8
)
```

Validation: all weights >= 0, sum within [0.99, 1.01].

### Config Integration

Add weight fields inline to `RecallConfig` in `internal/config/config.go`. We do **not** import `internal/recall` — `config` is a leaf package with zero internal imports and we keep it that way. Instead, define the weight fields directly and convert to `recall.Weights` at the call site:

```go
type RecallConfig struct {
    RerankScoreSpreadThreshold float64 `mapstructure:"rerank_score_spread_threshold"`
    RerankLatencyBudgetHooksMs int     `mapstructure:"rerank_latency_budget_hooks_ms"`
    RerankLatencyBudgetCLIMs   int     `mapstructure:"rerank_latency_budget_cli_ms"`
    Weights                    RecallWeightsConfig `mapstructure:"weights"`
}

type RecallWeightsConfig struct {
    Similarity    float64 `mapstructure:"similarity"`
    Recency       float64 `mapstructure:"recency"`
    Frequency     float64 `mapstructure:"frequency"`
    TypeBoost     float64 `mapstructure:"type_boost"`
    ScopeBoost    float64 `mapstructure:"scope_boost"`
    Confidence    float64 `mapstructure:"confidence"`
    Reinforcement float64 `mapstructure:"reinforcement"`
    TagAffinity   float64 `mapstructure:"tag_affinity"`
}
```

At each call site (cmd_recall.go, cmd_serve.go, hooks.go), convert `cfg.Recall.Weights` to `recall.Weights`:

```go
w := recall.Weights{
    Similarity:    cfg.Recall.Weights.Similarity,
    Recency:       cfg.Recall.Weights.Recency,
    // ... etc
}
if !w.IsValid() {
    w = recall.DefaultWeights()
}
recaller := recall.NewRecaller(w, logger)
```

With env var overrides: `OPENCLAW_CORTEX_RECALL_WEIGHTS_SIMILARITY`, etc. If no weights are configured (all zero), the call site falls back to `DefaultWeights()`. Invalid weights (don't sum to 1.0, negative values) log a warning and fall back to defaults — same behavior as today.

Example `~/.cortex/config.yaml`:

```yaml
recall:
  weights:
    similarity: 0.35
    recency: 0.15
    frequency: 0.10
    type_boost: 0.10
    scope_boost: 0.08
    confidence: 0.10
    reinforcement: 0.07
    tag_affinity: 0.05
```

## Plugin Changes

Update `RecallResult` interface in `extensions/openclaw-plugin/index.ts`:

```typescript
interface RecallResult {
  memory: CortexMemory;
  similarity_score: number;
  recency_score: number;
  frequency_score: number;
  type_boost: number;
  scope_boost: number;
  confidence_score: number;
  reinforcement_score: number;
  tag_affinity_score: number;
  supersession_penalty: number;
  conflict_penalty: number;
  final_score: number;
}
```

No behavioral changes needed in the plugin — it already uses `r.final_score` for display. The new fields are informational.

## Files Changed

| File | Change |
|------|--------|
| `internal/recall/recall.go` | Expand `Weights` to 8 fields, add `DefaultWeights()` with new defaults, add `confidenceScore()`, `reinforcementScore()`, `tagAffinityScore()` functions, update `Rank()` to accept query string, add supersession scan + conflict check, update final score computation |
| `internal/models/memory.go` | Add `ConfidenceScore`, `ReinforcementScore`, `TagAffinityScore`, `SupersessionPenalty`, `ConflictPenalty` fields to `RecallResult` |
| `internal/config/config.go` | Add `RecallWeightsConfig` struct and `Weights` field to `RecallConfig`, wire viper defaults and env var overrides |
| `cmd/openclaw-cortex/cmd_recall.go` | Convert `cfg.Recall.Weights` to `recall.Weights`, pass query string to `Rank()` |
| `cmd/openclaw-cortex/cmd_serve.go` | Convert `cfg.Recall.Weights` to `recall.Weights` |
| `internal/api/server.go` | Pass `req.Message` to `Rank()` |
| `internal/hooks/hooks.go` | Pass `input.Message` to `Rank()` |
| `extensions/openclaw-plugin/index.ts` | Add new fields to `RecallResult` interface |
| `tests/recall_scoring_test.go` | Tests for confidence, reinforcement, tag affinity scoring functions; tests for supersession and conflict penalties; tests for new default weights validation |
| `tests/plugin_contract_test.go` | Contract tests for new RecallResult JSON fields |

## Testing Strategy

- **Unit tests** for each new scoring function (confidence, reinforcement, tag affinity) with boundary values (0, 1, saturation points)
- **Unit tests** for supersession scan logic (both memories in results, only old memory, only new memory, chains)
- **Unit tests** for conflict penalty (active, resolved, empty status)
- **Integration test** for full `Rank()` with all 8 components + penalties, verifying final score computation
- **Contract tests** for new RecallResult JSON fields (backwards compatibility — old fields still present, new fields added)
- **Config tests** for weight loading, validation, and fallback to defaults

All tests use `MockStore` and run in `-short` mode (no external services).

## Backwards Compatibility

- JSON output gains new fields but no existing fields are removed or renamed
- Default weights produce different rankings than before (similarity drops from 0.50 to 0.35) — this is intentional and the core purpose of the change
- Config-less installations use `DefaultWeights()` automatically
- The `Rank()` signature change requires updating all 3 call sites (CLI, API, hooks) — all in this repo
