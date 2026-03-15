import type { Metadata } from "next";
import ComparisonTable from "@/components/comparison-table";
import Button from "@/components/ui/button";

export const metadata: Metadata = {
  title: "Compare — OpenClaw Cortex",
  description:
    "See how OpenClaw Cortex compares to mem0, Zep, LangChain Memory, and raw vector databases.",
};

export default function ComparePage() {
  return (
    <div className="max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-16">
      {/* Header */}
      <div className="text-center mb-14">
        <h1 className="text-4xl sm:text-5xl font-bold text-zinc-50 mb-4">
          Why OpenClaw Cortex?
        </h1>
        <p className="text-lg text-zinc-400 max-w-2xl mx-auto">
          See how Cortex compares to other AI memory solutions
        </p>
      </div>

      {/* Comparison table */}
      <div className="mb-16">
        <ComparisonTable />

        {/* Legend */}
        <div className="mt-4 flex flex-wrap items-center gap-5 text-xs text-zinc-500 justify-center">
          <span className="flex items-center gap-1.5">
            <svg className="w-4 h-4 text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
            </svg>
            Fully supported
          </span>
          <span className="flex items-center gap-1.5">
            <span className="text-amber-400 font-bold text-base leading-none">~</span>
            Partial / requires configuration
          </span>
          <span className="flex items-center gap-1.5">
            <svg className="w-4 h-4 text-red-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M6 18L18 6M6 6l12 12" />
            </svg>
            Not supported
          </span>
          <span className="flex items-center gap-1.5">
            <span className="text-zinc-600">—</span>
            Not applicable
          </span>
        </div>
      </div>

      {/* Positioning narrative */}
      <div className="max-w-3xl mx-auto space-y-8 mb-16">
        <div>
          <h2 className="text-xl font-semibold text-zinc-100 mb-3">
            Graph-aware recall changes what agents can know
          </h2>
          <p className="text-zinc-400 leading-relaxed">
            Most AI memory systems store facts as isolated text fragments and retrieve them by
            semantic similarity. This works for simple lookups but breaks down when the answer
            requires traversing a chain of relationships: Alice manages project-X, project-X
            depends on library-Y, library-Y was deprecated last month. No semantic query
            captures that chain. OpenClaw Cortex stores entities and relationships as a native
            graph in Memgraph, then uses Reciprocal Rank Fusion to merge graph traversal results
            with vector similarity — surfacing structurally-connected facts that pure embedding
            search would miss.
          </p>
        </div>

        <div>
          <h2 className="text-xl font-semibold text-zinc-100 mb-3">
            Temporal versioning means no silent stale data
          </h2>
          <p className="text-zinc-400 leading-relaxed">
            When a fact changes — a team member moves projects, a preference is updated, a tool
            is deprecated — Cortex preserves the old version with a{" "}
            <code className="text-emerald-400 bg-zinc-800 px-1.5 py-0.5 rounded text-sm font-mono">
              valid_to
            </code>{" "}
            timestamp rather than overwriting it. Superseded memories receive a 0.3× scoring
            penalty, keeping them available for historical queries while ensuring current facts
            dominate. Contradiction detection groups conflicting facts and applies a 0.8×
            penalty, surfacing ambiguity to the agent rather than silently picking a winner.
            No other open-source memory system in this comparison offers this level of temporal
            precision.
          </p>
        </div>

        <div>
          <h2 className="text-xl font-semibold text-zinc-100 mb-3">
            One container, zero infrastructure complexity
          </h2>
          <p className="text-zinc-400 leading-relaxed">
            Cortex v0.7.0 consolidated from Qdrant + Neo4j to a single Memgraph instance —
            vector search and graph traversal in one container speaking the Bolt protocol.
            The result is a memory backend that starts with{" "}
            <code className="text-emerald-400 bg-zinc-800 px-1.5 py-0.5 rounded text-sm font-mono">
              docker compose up -d
            </code>
            , has one health endpoint to monitor, and eliminates the partial-failure modes
            that come from keeping two storage backends in sync. For teams running AI agents
            in production, fewer moving parts is a feature.
          </p>
        </div>
      </div>

      {/* CTA */}
      <div className="text-center">
        <Button variant="primary" size="lg" href="/docs/getting-started">
          Get Started
        </Button>
      </div>
    </div>
  );
}
