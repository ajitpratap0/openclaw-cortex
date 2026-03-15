export interface PlaygroundMemory {
  id: string;
  content: string;
  type: "rule" | "fact" | "episode" | "procedure" | "preference";
  scope: "permanent" | "project" | "session";
  tags: string[];
  confidence: number;
  createdAt: string; // ISO date
  accessCount: number;
  reinforcedCount: number;
  supersedesId?: string;
  conflictStatus?: "active" | "resolved";
}

export interface ScoreBreakdown {
  similarity: number;
  recency: number;
  frequency: number;
  typeBoost: number;
  scopeBoost: number;
  confidence: number;
  reinforcement: number;
  tagAffinity: number;
}

export interface ScoredResult {
  memory: PlaygroundMemory;
  finalScore: number;
  breakdown: ScoreBreakdown;
  weightedContributions: ScoreBreakdown;
  penaltyApplied: string | null;
}

const NOW = new Date("2026-03-16T12:00:00Z");

function daysAgo(days: number): string {
  const d = new Date(NOW);
  d.setDate(d.getDate() - days);
  return d.toISOString();
}

function hoursAgo(hours: number): string {
  const d = new Date(NOW);
  d.setHours(d.getHours() - hours);
  return d.toISOString();
}

export const sampleMemories: PlaygroundMemory[] = [
  {
    id: "m1",
    content:
      "Never push directly to main — all changes must go through PRs with CI passing",
    type: "rule",
    scope: "permanent",
    tags: ["git", "workflow", "ci", "main", "pr", "branch"],
    confidence: 0.98,
    createdAt: daysAgo(7),
    accessCount: 18,
    reinforcedCount: 4,
  },
  {
    id: "m2",
    content:
      "OpenClaw Cortex uses Memgraph for both vector search and graph traversal",
    type: "fact",
    scope: "permanent",
    tags: ["memgraph", "vector", "graph", "database", "storage"],
    confidence: 0.95,
    createdAt: daysAgo(6),
    accessCount: 12,
    reinforcedCount: 2,
  },
  {
    id: "m3",
    content:
      "The recall engine uses 8-factor scoring with configurable weights",
    type: "fact",
    scope: "permanent",
    tags: ["recall", "scoring", "weights", "engine", "algorithm"],
    confidence: 0.92,
    createdAt: daysAgo(5),
    accessCount: 9,
    reinforcedCount: 1,
  },
  {
    id: "m4",
    content:
      "Migrated from Qdrant + Neo4j to single Memgraph instance on 2026-03-15",
    type: "episode",
    scope: "permanent",
    tags: ["migration", "memgraph", "qdrant", "neo4j", "database"],
    confidence: 0.88,
    createdAt: hoursAgo(36),
    accessCount: 5,
    reinforcedCount: 0,
    supersedesId: "m-old-qdrant",
  },
  {
    id: "m5",
    content:
      "To deploy: docker compose up -d, then run openclaw-cortex health to verify",
    type: "procedure",
    scope: "permanent",
    tags: ["deploy", "docker", "health", "procedure", "startup"],
    confidence: 0.97,
    createdAt: daysAgo(4),
    accessCount: 14,
    reinforcedCount: 3,
  },
  {
    id: "m6",
    content:
      "Always wrap errors with fmt.Errorf and %w for proper error chains",
    type: "rule",
    scope: "permanent",
    tags: ["error", "go", "fmt", "errorf", "coding", "convention"],
    confidence: 0.99,
    createdAt: daysAgo(6),
    accessCount: 20,
    reinforcedCount: 5,
  },
  {
    id: "m7",
    content:
      "Embeddings use nomic-embed-text via Ollama, producing 768-dim vectors",
    type: "fact",
    scope: "permanent",
    tags: ["embeddings", "ollama", "nomic", "vector", "dimensions"],
    confidence: 0.93,
    createdAt: daysAgo(3),
    accessCount: 7,
    reinforcedCount: 1,
  },
  {
    id: "m8",
    content:
      "Prefer direct communication, no fluff, bias toward action",
    type: "preference",
    scope: "permanent",
    tags: ["communication", "style", "preference", "action", "direct"],
    confidence: 0.85,
    createdAt: daysAgo(5),
    accessCount: 3,
    reinforcedCount: 0,
  },
  {
    id: "m9",
    content:
      "Debugged dedup failure: Memgraph needs WITH between YIELD and WHERE",
    type: "episode",
    scope: "project",
    tags: ["memgraph", "debug", "cypher", "yield", "where", "dedup"],
    confidence: 0.9,
    createdAt: hoursAgo(12),
    accessCount: 2,
    reinforcedCount: 0,
    conflictStatus: "resolved",
  },
  {
    id: "m10",
    content:
      "To add a new CLI command: create cmd_name.go, add to root command in main.go",
    type: "procedure",
    scope: "project",
    tags: ["cli", "command", "go", "main", "procedure", "development"],
    confidence: 0.94,
    createdAt: daysAgo(2),
    accessCount: 6,
    reinforcedCount: 1,
  },
];

const weights = {
  similarity: 0.35,
  recency: 0.15,
  frequency: 0.10,
  typeBoost: 0.10,
  scopeBoost: 0.08,
  confidence: 0.10,
  reinforcement: 0.07,
  tagAffinity: 0.05,
};

const typeMultipliers: Record<PlaygroundMemory["type"], number> = {
  rule: 1.5,
  procedure: 1.3,
  fact: 1.0,
  episode: 0.8,
  preference: 0.7,
};

const scopeMultipliers: Record<PlaygroundMemory["scope"], number> = {
  permanent: 1.0,
  project: 1.5,
  session: 0.8,
};

function computeSimilarity(memory: PlaygroundMemory, query: string): number {
  if (!query.trim()) return 0;
  const queryWords = query
    .toLowerCase()
    .split(/\s+/)
    .filter((w) => w.length > 1);
  if (queryWords.length === 0) return 0;
  const contentLower = memory.content.toLowerCase();
  let matches = 0;
  for (const word of queryWords) {
    if (contentLower.includes(word)) matches++;
  }
  return Math.min(matches / queryWords.length, 1.0);
}

function computeRecency(createdAt: string): number {
  const hoursElapsed =
    (NOW.getTime() - new Date(createdAt).getTime()) / (1000 * 60 * 60);
  const halfLifeHours = 168; // 7 days
  return Math.exp((-Math.LN2 * hoursElapsed) / halfLifeHours);
}

function computeFrequency(accessCount: number): number {
  return Math.min(Math.log2(accessCount + 1) / 10, 1.0);
}

function computeTypeBoost(type: PlaygroundMemory["type"]): number {
  return typeMultipliers[type] / 1.5;
}

function computeScopeBoost(scope: PlaygroundMemory["scope"]): number {
  return scopeMultipliers[scope] / 1.5;
}

function computeConfidence(confidence: number): number {
  return confidence < 0.01 ? 0.7 : confidence;
}

function computeReinforcement(reinforcedCount: number): number {
  return Math.min(Math.log2(reinforcedCount + 1) / 5, 1.0);
}

function computeTagAffinity(memory: PlaygroundMemory, query: string): number {
  if (!query.trim() || memory.tags.length === 0) return 0;
  const queryWords = query
    .toLowerCase()
    .split(/\s+/)
    .filter((w) => w.length > 1);
  if (queryWords.length === 0) return 0;
  let matchedTags = 0;
  for (const tag of memory.tags) {
    if (queryWords.some((w) => tag.includes(w) || w.includes(tag))) {
      matchedTags++;
    }
  }
  return Math.min(matchedTags / memory.tags.length, 1.0);
}

export function scoreMemory(
  memory: PlaygroundMemory,
  query: string
): ScoredResult {
  const breakdown: ScoreBreakdown = {
    similarity: computeSimilarity(memory, query),
    recency: computeRecency(memory.createdAt),
    frequency: computeFrequency(memory.accessCount),
    typeBoost: computeTypeBoost(memory.type),
    scopeBoost: computeScopeBoost(memory.scope),
    confidence: computeConfidence(memory.confidence),
    reinforcement: computeReinforcement(memory.reinforcedCount),
    tagAffinity: computeTagAffinity(memory, query),
  };

  const weightedContributions: ScoreBreakdown = {
    similarity: breakdown.similarity * weights.similarity,
    recency: breakdown.recency * weights.recency,
    frequency: breakdown.frequency * weights.frequency,
    typeBoost: breakdown.typeBoost * weights.typeBoost,
    scopeBoost: breakdown.scopeBoost * weights.scopeBoost,
    confidence: breakdown.confidence * weights.confidence,
    reinforcement: breakdown.reinforcement * weights.reinforcement,
    tagAffinity: breakdown.tagAffinity * weights.tagAffinity,
  };

  let rawScore =
    weightedContributions.similarity +
    weightedContributions.recency +
    weightedContributions.frequency +
    weightedContributions.typeBoost +
    weightedContributions.scopeBoost +
    weightedContributions.confidence +
    weightedContributions.reinforcement +
    weightedContributions.tagAffinity;

  let penaltyApplied: string | null = null;
  if (memory.supersedesId) {
    rawScore *= 0.3;
    penaltyApplied = "superseded ×0.3";
  } else if (memory.conflictStatus === "active") {
    rawScore *= 0.8;
    penaltyApplied = "conflict ×0.8";
  }

  return {
    memory,
    finalScore: Math.min(rawScore, 1.0),
    breakdown,
    weightedContributions,
    penaltyApplied,
  };
}

export function rankMemories(query: string): ScoredResult[] {
  const scored = sampleMemories.map((m) => scoreMemory(m, query));
  return scored.sort((a, b) => b.finalScore - a.finalScore);
}

export interface Entity {
  id: string;
  name: string;
  type: "system" | "project";
}

export interface Relationship {
  source: string;
  target: string;
  type: string;
  fact: string;
}

export const entities: Entity[] = [
  { id: "e1", name: "Memgraph", type: "system" },
  { id: "e2", name: "Qdrant", type: "system" },
  { id: "e3", name: "OpenClaw Cortex", type: "project" },
  { id: "e4", name: "Ollama", type: "system" },
  { id: "e5", name: "Claude Haiku", type: "system" },
];

export const relationships: Relationship[] = [
  {
    source: "e3",
    target: "e1",
    type: "USES",
    fact: "Cortex uses Memgraph for storage",
  },
  {
    source: "e3",
    target: "e4",
    type: "USES",
    fact: "Cortex uses Ollama for embeddings",
  },
  {
    source: "e3",
    target: "e5",
    type: "USES",
    fact: "Cortex uses Claude for extraction",
  },
  {
    source: "e1",
    target: "e2",
    type: "REPLACED",
    fact: "Memgraph replaced Qdrant",
  },
];
