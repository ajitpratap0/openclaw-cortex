"use client";

import { useState, useEffect, useCallback } from "react";
import {
  rankMemories,
  entities,
  relationships,
  type ScoredResult,
  type PlaygroundMemory,
} from "@/lib/playground-data";

const SUGGESTED_QUERIES = [
  "database decision",
  "coding rules",
  "deploy procedure",
  "error handling",
];

const TYPE_COLORS: Record<PlaygroundMemory["type"], string> = {
  rule: "bg-indigo-500/10 text-indigo-300 border border-indigo-500/20",
  fact: "bg-emerald-500/10 text-emerald-300 border border-emerald-500/20",
  episode: "bg-amber-500/10 text-amber-300 border border-amber-500/20",
  procedure: "bg-violet-500/10 text-violet-300 border border-violet-500/20",
  preference: "bg-rose-500/10 text-rose-300 border border-rose-500/20",
};

const SCOPE_COLORS: Record<PlaygroundMemory["scope"], string> = {
  permanent: "bg-zinc-700/60 text-zinc-300 border border-zinc-600",
  project: "bg-sky-500/10 text-sky-300 border border-sky-500/20",
  session: "bg-orange-500/10 text-orange-300 border border-orange-500/20",
};

const FACTOR_COLORS: Record<string, string> = {
  similarity: "bg-indigo-500",
  recency: "bg-emerald-500",
  frequency: "bg-amber-500",
  typeBoost: "bg-violet-500",
  scopeBoost: "bg-sky-500",
  confidence: "bg-teal-500",
  reinforcement: "bg-rose-500",
  tagAffinity: "bg-orange-500",
};

const FACTOR_LABELS: Record<string, string> = {
  similarity: "Similarity",
  recency: "Recency",
  frequency: "Freq",
  typeBoost: "Type",
  scopeBoost: "Scope",
  confidence: "Conf",
  reinforcement: "Reinf",
  tagAffinity: "Tags",
};

const FACTOR_WEIGHTS: Record<string, number> = {
  similarity: 0.35,
  recency: 0.15,
  frequency: 0.10,
  typeBoost: 0.10,
  scopeBoost: 0.08,
  confidence: 0.10,
  reinforcement: 0.07,
  tagAffinity: 0.05,
};

const FACTOR_ORDER = [
  "similarity",
  "recency",
  "frequency",
  "typeBoost",
  "scopeBoost",
  "confidence",
  "reinforcement",
  "tagAffinity",
];

function ScoreBar({
  contributions,
}: {
  contributions: Record<string, number>;
}) {
  const total = FACTOR_ORDER.reduce(
    (sum, k) => sum + (contributions[k] ?? 0),
    0
  );

  return (
    <div className="space-y-1.5">
      {/* Stacked bar */}
      <div className="flex h-2.5 w-full rounded-full overflow-hidden bg-zinc-800 gap-px">
        {FACTOR_ORDER.map((key) => {
          const value = contributions[key] ?? 0;
          const pct = total > 0 ? (value / total) * 100 : 0;
          if (pct < 0.5) return null;
          return (
            <div
              key={key}
              className={`h-full ${FACTOR_COLORS[key]} transition-all duration-500`}
              style={{ width: `${pct}%` }}
              title={`${FACTOR_LABELS[key]}: ${(value * 100).toFixed(1)}%`}
            />
          );
        })}
      </div>
      {/* Factor labels */}
      <div className="flex flex-wrap gap-x-3 gap-y-1">
        {FACTOR_ORDER.map((key) => {
          const raw = contributions[key] ?? 0;
          const weight = FACTOR_WEIGHTS[key];
          const normalized = weight > 0 ? raw / weight : 0;
          return (
            <div key={key} className="flex items-center gap-1">
              <div
                className={`w-2 h-2 rounded-sm flex-shrink-0 ${FACTOR_COLORS[key]}`}
              />
              <span className="text-[10px] text-zinc-500">
                {FACTOR_LABELS[key]}
              </span>
              <span className="text-[10px] text-zinc-400 font-mono">
                {(normalized * 100).toFixed(0)}%
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function MemoryCard({
  result,
  rank,
}: {
  result: ScoredResult;
  rank: number;
}) {
  const { memory, finalScore, weightedContributions, penaltyApplied } = result;
  const scorePercent = (finalScore * 100).toFixed(1);
  const scoreColor =
    finalScore >= 0.6
      ? "text-emerald-400"
      : finalScore >= 0.35
      ? "text-amber-400"
      : "text-zinc-500";

  return (
    <div className="group relative rounded-xl border border-zinc-800 bg-zinc-900/60 p-4 hover:border-zinc-700 hover:bg-zinc-900 transition-all duration-200">
      {/* Header row */}
      <div className="flex items-start gap-3 mb-3">
        {/* Rank */}
        <div className="flex-shrink-0 w-7 h-7 rounded-full bg-zinc-800 border border-zinc-700 flex items-center justify-center">
          <span className="text-xs font-bold text-zinc-400">#{rank}</span>
        </div>

        {/* Content + badges */}
        <div className="flex-1 min-w-0">
          <div className="flex flex-wrap items-center gap-1.5 mb-2">
            <span
              className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${TYPE_COLORS[memory.type]}`}
            >
              {memory.type}
            </span>
            <span
              className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${SCOPE_COLORS[memory.scope]}`}
            >
              {memory.scope}
            </span>
            {penaltyApplied && (
              <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-red-500/10 text-red-400 border border-red-500/20">
                {penaltyApplied}
              </span>
            )}
          </div>
          <p className="text-sm text-zinc-200 leading-relaxed">
            {memory.content}
          </p>
        </div>

        {/* Score */}
        <div className="flex-shrink-0 text-right">
          <div className={`text-lg font-bold tabular-nums ${scoreColor}`}>
            {scorePercent}%
          </div>
          <div className="text-[10px] text-zinc-600 uppercase tracking-wider">
            score
          </div>
        </div>
      </div>

      {/* Score breakdown bar */}
      <div className="pl-10">
        <ScoreBar contributions={weightedContributions as unknown as Record<string, number>} />
      </div>

      {/* Tags */}
      {memory.tags.length > 0 && (
        <div className="pl-10 mt-2 flex flex-wrap gap-1">
          {memory.tags.map((tag) => (
            <span
              key={tag}
              className="text-[10px] px-1.5 py-0.5 rounded bg-zinc-800 text-zinc-500 font-mono"
            >
              {tag}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

// Entity positions for the graph overlay (relative, percentage-based)
const ENTITY_POSITIONS: Record<string, { x: number; y: number }> = {
  e3: { x: 50, y: 10 }, // OpenClaw Cortex — top center
  e1: { x: 25, y: 60 }, // Memgraph — bottom left
  e2: { x: 75, y: 60 }, // Qdrant — bottom right (replaced)
  e4: { x: 10, y: 35 }, // Ollama — left
  e5: { x: 88, y: 35 }, // Claude Haiku — right
};

const ENTITY_TYPE_STYLES: Record<string, string> = {
  system: "border-zinc-600 bg-zinc-800 text-zinc-300",
  project: "border-indigo-500 bg-indigo-500/10 text-indigo-300",
};

const REL_TYPE_COLORS: Record<string, string> = {
  USES: "#6366f1",
  REPLACED: "#f43f5e",
};

function GraphOverlay() {
  // Render SVG lines between entity boxes, then absolute-positioned boxes
  return (
    <div className="relative w-full rounded-xl border border-zinc-800 bg-zinc-900/40 overflow-hidden mt-6">
      <div className="px-4 pt-4 pb-2 border-b border-zinc-800">
        <p className="text-xs font-semibold text-zinc-400 uppercase tracking-widest">
          Entity Graph
        </p>
      </div>
      <div className="relative" style={{ height: 240 }}>
        {/* SVG lines */}
        <svg
          className="absolute inset-0 w-full h-full pointer-events-none"
          viewBox="0 0 100 100"
          preserveAspectRatio="none"
        >
          {relationships.map((rel, i) => {
            const src = ENTITY_POSITIONS[rel.source];
            const tgt = ENTITY_POSITIONS[rel.target];
            if (!src || !tgt) return null;
            const color = REL_TYPE_COLORS[rel.type] ?? "#52525b";
            const dashed = rel.type === "REPLACED";
            return (
              <g key={i}>
                <line
                  x1={src.x}
                  y1={src.y + 4}
                  x2={tgt.x}
                  y2={tgt.y - 4}
                  stroke={color}
                  strokeWidth="0.6"
                  strokeDasharray={dashed ? "2 1.5" : undefined}
                  strokeOpacity="0.7"
                />
                {/* Midpoint label */}
                <text
                  x={(src.x + tgt.x) / 2}
                  y={(src.y + tgt.y) / 2 - 1}
                  textAnchor="middle"
                  fontSize="2.2"
                  fill={color}
                  fillOpacity="0.8"
                >
                  {rel.type}
                </text>
              </g>
            );
          })}
        </svg>

        {/* Entity nodes */}
        {entities.map((entity) => {
          const pos = ENTITY_POSITIONS[entity.id];
          if (!pos) return null;
          const style = ENTITY_TYPE_STYLES[entity.type];
          return (
            <div
              key={entity.id}
              className={`absolute transform -translate-x-1/2 -translate-y-1/2 rounded-lg border px-3 py-1.5 text-xs font-medium whitespace-nowrap ${style}`}
              style={{
                left: `${pos.x}%`,
                top: `${pos.y + 8}%`,
              }}
            >
              {entity.name}
            </div>
          );
        })}
      </div>

      {/* Legend */}
      <div className="px-4 py-3 border-t border-zinc-800 flex flex-wrap gap-x-4 gap-y-1">
        {Object.entries(REL_TYPE_COLORS).map(([type, color]) => (
          <div key={type} className="flex items-center gap-1.5">
            <div
              className="w-5 h-px"
              style={{
                backgroundColor: color,
                borderTop:
                  type === "REPLACED"
                    ? `1px dashed ${color}`
                    : `1px solid ${color}`,
              }}
            />
            <span className="text-[10px] text-zinc-500">{type}</span>
          </div>
        ))}
        <div className="flex items-center gap-1.5">
          <div className="w-3 h-3 rounded border border-indigo-500 bg-indigo-500/10" />
          <span className="text-[10px] text-zinc-500">project</span>
        </div>
        <div className="flex items-center gap-1.5">
          <div className="w-3 h-3 rounded border border-zinc-600 bg-zinc-800" />
          <span className="text-[10px] text-zinc-500">system</span>
        </div>
      </div>
    </div>
  );
}

function useDebounce<T>(value: T, delay: number): T {
  const [debouncedValue, setDebouncedValue] = useState<T>(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedValue(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debouncedValue;
}

export default function PlaygroundUI() {
  const [query, setQuery] = useState("");
  const [showGraph, setShowGraph] = useState(false);
  const debouncedQuery = useDebounce(query, 300);
  const [results, setResults] = useState<ScoredResult[]>([]);

  const computeResults = useCallback((q: string) => {
    setResults(rankMemories(q));
  }, []);

  useEffect(() => {
    if (debouncedQuery.trim()) {
      computeResults(debouncedQuery);
    } else {
      setResults([]);
    }
  }, [debouncedQuery, computeResults]);

  return (
    <div className="space-y-6">
      {/* Search input */}
      <div className="space-y-3">
        <div className="relative">
          <div className="absolute inset-y-0 left-4 flex items-center pointer-events-none">
            <svg
              className="w-5 h-5 text-zinc-500"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={1.5}
                d="M21 21l-4.35-4.35M17 11A6 6 0 1 1 5 11a6 6 0 0 1 12 0z"
              />
            </svg>
          </div>
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder='Try: database decision, coding rules, deployment...'
            className="w-full pl-12 pr-4 py-3.5 rounded-xl bg-zinc-900 border border-zinc-700 text-zinc-100 placeholder-zinc-500 text-base focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent transition-all"
          />
          {query && (
            <button
              onClick={() => setQuery("")}
              className="absolute inset-y-0 right-4 flex items-center text-zinc-500 hover:text-zinc-300 transition-colors"
            >
              <svg
                className="w-4 h-4"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M6 18L18 6M6 6l12 12"
                />
              </svg>
            </button>
          )}
        </div>

        {/* Suggested queries */}
        <div className="flex flex-wrap gap-2 items-center">
          <span className="text-xs text-zinc-600">Try:</span>
          {SUGGESTED_QUERIES.map((sq) => (
            <button
              key={sq}
              onClick={() => setQuery(sq)}
              className={`px-3 py-1 rounded-full text-xs font-medium border transition-all duration-150 ${
                query === sq
                  ? "bg-indigo-500/20 text-indigo-300 border-indigo-500/40"
                  : "bg-zinc-800/60 text-zinc-400 border-zinc-700 hover:bg-zinc-800 hover:text-zinc-300 hover:border-zinc-600"
              }`}
            >
              {sq}
            </button>
          ))}
        </div>
      </div>

      {/* Graph toggle */}
      <div className="flex items-center justify-between">
        <div className="text-sm text-zinc-500">
          {results.length > 0 ? (
            <span>
              <span className="font-semibold text-zinc-300">
                {results.length}
              </span>{" "}
              memories ranked
            </span>
          ) : (
            <span className="text-zinc-600">
              {query.trim()
                ? "No results"
                : "Type a query to see ranked results"}
            </span>
          )}
        </div>
        <label className="flex items-center gap-2 cursor-pointer select-none group">
          <span className="text-xs text-zinc-500 group-hover:text-zinc-400 transition-colors">
            Show graph connections
          </span>
          <button
            role="switch"
            aria-checked={showGraph}
            onClick={() => setShowGraph((prev) => !prev)}
            className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors duration-200 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 focus:ring-offset-zinc-950 ${
              showGraph ? "bg-indigo-500" : "bg-zinc-700"
            }`}
          >
            <span
              className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow-sm transition-transform duration-200 ${
                showGraph ? "translate-x-4.5" : "translate-x-1"
              }`}
            />
          </button>
        </label>
      </div>

      {/* Results */}
      {results.length > 0 && (
        <div className="space-y-3">
          {results.map((result, i) => (
            <MemoryCard key={result.memory.id} result={result} rank={i + 1} />
          ))}
        </div>
      )}

      {/* Empty state */}
      {!query.trim() && (
        <div className="flex flex-col items-center justify-center py-16 rounded-xl border border-dashed border-zinc-800 bg-zinc-900/20">
          <div className="w-12 h-12 rounded-full bg-zinc-800 border border-zinc-700 flex items-center justify-center mb-4">
            <svg
              className="w-6 h-6 text-zinc-500"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={1.5}
                d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z"
              />
            </svg>
          </div>
          <p className="text-zinc-300 text-sm font-medium">
            Type a query to see ranked results
          </p>
          <p className="text-zinc-600 text-xs mt-1">
            Scoring runs live in your browser using the same 8-factor algorithm
          </p>
        </div>
      )}

      {/* Graph overlay */}
      {showGraph && <GraphOverlay />}

      {/* Score legend */}
      {results.length > 0 && (
        <div className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-4">
          <p className="text-xs font-semibold text-zinc-500 uppercase tracking-widest mb-3">
            Score Factor Legend
          </p>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
            {FACTOR_ORDER.map((key) => (
              <div key={key} className="flex items-center gap-2">
                <div
                  className={`w-3 h-3 rounded-sm flex-shrink-0 ${FACTOR_COLORS[key]}`}
                />
                <div>
                  <div className="text-xs text-zinc-300 font-medium">
                    {FACTOR_LABELS[key]}
                  </div>
                  <div className="text-[10px] text-zinc-600">
                    w={FACTOR_WEIGHTS[key]}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
