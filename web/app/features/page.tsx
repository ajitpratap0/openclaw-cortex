import type { Metadata } from "next";
import FeaturesNav from "./features-nav";

export const metadata: Metadata = {
  title: "Features",
  description:
    "Deep dive into OpenClaw Cortex features: 9-factor recall scoring, graph traversal, temporal versioning, contradiction detection, episodic memory, and smart capture.",
};

const sections = [
  { id: "recall-scoring", label: "Recall Scoring" },
  { id: "graph-aware-recall", label: "Graph-Aware Recall" },
  { id: "temporal-versioning", label: "Temporal Versioning" },
  { id: "contradiction-detection", label: "Contradiction Detection" },
  { id: "episodic-extraction", label: "Episodic Extraction" },
  { id: "smart-capture", label: "Smart Capture" },
];

export default function FeaturesPage() {
  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-16">
      {/* Page header */}
      <div className="mb-16 max-w-2xl">
        <h1 className="text-4xl sm:text-5xl font-bold text-zinc-50 mb-4">Features</h1>
        <p className="text-lg text-zinc-300 leading-relaxed">
          A deep dive into the technical capabilities that make OpenClaw Cortex the most
          sophisticated open-source memory system for AI agents.
        </p>
      </div>

      <div className="flex gap-16">
        {/* Sticky section nav — hidden on mobile */}
        <aside className="hidden lg:block w-52 flex-shrink-0">
          <FeaturesNav sections={sections} />
        </aside>

        {/* Content */}
        <div className="flex-1 min-w-0 space-y-24">
          {/* ── 1. Recall Scoring ── */}
          <section id="recall-scoring" className="scroll-mt-24">
            <h2 className="text-2xl font-bold text-zinc-50 mb-3">Recall Scoring</h2>
            <p className="text-zinc-300 leading-relaxed mb-8">
              Every retrieved memory receives a composite score from nine independent factors.
              The weighted sum balances immediate semantic relevance with long-term signals like
              access frequency, memory type, and graph proximity — ensuring the most useful facts
              surface first, not just the most similar ones.
            </p>

            {/* Formula */}
            <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-6 mb-8 overflow-x-auto">
              <p className="text-sm text-zinc-500 mb-3 font-mono uppercase tracking-wide">Final score formula</p>
              <code className="text-emerald-300 font-mono text-sm leading-loose whitespace-nowrap">
                score = 0.45 × similarity<br />
                {"       "}+ 0.08 × recency<br />
                {"       "}+ 0.05 × frequency<br />
                {"       "}+ 0.10 × typeBoost<br />
                {"       "}+ 0.08 × scopeBoost<br />
                {"       "}+ 0.07 × confidence<br />
                {"       "}+ 0.07 × reinforcement<br />
                {"       "}+ 0.05 × tagAffinity<br />
                {"       "}+ 0.05 × graphProximity
              </code>
            </div>

            {/* Weight bar chart */}
            <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-6">
              <p className="text-sm text-zinc-500 mb-5 font-mono uppercase tracking-wide">Factor weights</p>
              <div className="space-y-3">
                {[
                  { label: "Similarity", weight: 45, color: "bg-indigo-500" },
                  { label: "Type boost", weight: 10, color: "bg-violet-500" },
                  { label: "Recency", weight: 8, color: "bg-emerald-500" },
                  { label: "Scope boost", weight: 8, color: "bg-teal-500" },
                  { label: "Confidence", weight: 7, color: "bg-amber-500" },
                  { label: "Reinforcement", weight: 7, color: "bg-rose-500" },
                  { label: "Frequency", weight: 5, color: "bg-sky-500" },
                  { label: "Tag affinity", weight: 5, color: "bg-orange-500" },
                  { label: "Graph proximity", weight: 5, color: "bg-fuchsia-500" },
                ].map((item) => (
                  <div key={item.label} className="flex items-center gap-3">
                    <span className="text-xs text-zinc-400 w-24 flex-shrink-0">{item.label}</span>
                    <div className="flex-1 bg-zinc-800 rounded-full h-2">
                      <div
                        className={`${item.color} h-2 rounded-full transition-all`}
                        style={{ width: `${item.weight * 2.86}%` }}
                      />
                    </div>
                    <span className="text-xs text-zinc-500 w-8 text-right">{item.weight}%</span>
                  </div>
                ))}
              </div>
              <div className="mt-5 pt-4 border-t border-zinc-800 text-xs text-zinc-600">
                Multiplicative penalties applied after sum: superseded ×0.3 · active conflict ×0.8
              </div>
            </div>
          </section>

          {/* ── 2. Graph-Aware Recall ── */}
          <section id="graph-aware-recall" className="scroll-mt-24">
            <h2 className="text-2xl font-bold text-zinc-50 mb-3">Graph-Aware Recall</h2>
            <p className="text-zinc-300 leading-relaxed mb-3">
              Vector similarity search finds memories that look like the query. Graph traversal
              finds memories that are connected to entities mentioned in the query. Reciprocal Rank
              Fusion combines both ranked lists into a single result without requiring careful weight
              tuning — making graph-aware recall robust across diverse query types.
            </p>
            <p className="text-zinc-300 leading-relaxed mb-8">
              RRF score for each result:{" "}
              <code className="text-emerald-400 bg-zinc-800 px-1.5 py-0.5 rounded text-sm font-mono">
                Σ 1 / (60 + rank_i)
              </code>{" "}
              summed across all contributing ranked lists (vector search, entity traversal, episodic context).
            </p>

            {/* Graph traversal diagram */}
            <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-8">
              <p className="text-sm text-zinc-500 mb-6 font-mono uppercase tracking-wide">Entity graph traversal</p>
              <div className="flex flex-col items-center gap-0">
                {/* Query node */}
                <div className="bg-indigo-500/15 border border-indigo-500/40 rounded-lg px-4 py-2 text-sm text-indigo-300 font-mono">
                  query: &quot;Alice&apos;s project status&quot;
                </div>
                <div className="w-px h-5 bg-zinc-700" />
                {/* Arrow label */}
                <span className="text-xs text-zinc-600 -mt-1 mb-1">entity extraction</span>
                <div className="w-px h-4 bg-zinc-700" />
                {/* Entity node */}
                <div className="bg-emerald-500/15 border border-emerald-500/40 rounded-lg px-4 py-2 text-sm text-emerald-300 font-mono">
                  Entity: Alice (Person)
                </div>
                {/* Branching */}
                <div className="flex gap-12 mt-0">
                  <div className="flex flex-col items-center">
                    <div className="w-px h-5 bg-zinc-700" />
                    <span className="text-xs text-zinc-600">manages</span>
                    <div className="w-px h-4 bg-zinc-700" />
                    <div className="bg-amber-500/15 border border-amber-500/40 rounded-lg px-3 py-2 text-xs text-amber-300 font-mono text-center">
                      Entity:<br />project-X
                    </div>
                    <div className="w-px h-5 bg-zinc-700" />
                    <span className="text-xs text-zinc-600">linked memories</span>
                    <div className="w-px h-4 bg-zinc-700" />
                    <div className="bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-xs text-zinc-400 font-mono text-center">
                      Memory:<br />project-X scope
                    </div>
                  </div>
                  <div className="flex flex-col items-center">
                    <div className="w-px h-5 bg-zinc-700" />
                    <span className="text-xs text-zinc-600">preferences</span>
                    <div className="w-px h-4 bg-zinc-700" />
                    <div className="bg-violet-500/15 border border-violet-500/40 rounded-lg px-3 py-2 text-xs text-violet-300 font-mono text-center">
                      Memory:<br />Alice prefers TypeScript
                    </div>
                  </div>
                </div>
                <div className="mt-6 text-xs text-zinc-600 text-center">
                  All retrieved nodes fed into RRF fusion with vector search results
                </div>
              </div>
            </div>
          </section>

          {/* ── 3. Temporal Versioning ── */}
          <section id="temporal-versioning" className="scroll-mt-24">
            <h2 className="text-2xl font-bold text-zinc-50 mb-3">Temporal Versioning</h2>
            <p className="text-zinc-300 leading-relaxed mb-3">
              Every memory carries <code className="text-emerald-400 bg-zinc-800 px-1.5 py-0.5 rounded text-sm font-mono">valid_from</code> and{" "}
              <code className="text-emerald-400 bg-zinc-800 px-1.5 py-0.5 rounded text-sm font-mono">valid_to</code> timestamps.
              When a fact is superseded by newer information, the old version is preserved with{" "}
              <code className="text-emerald-400 bg-zinc-800 px-1.5 py-0.5 rounded text-sm font-mono">valid_to</code> set rather than deleted,
              maintaining full history for audit and debugging.
            </p>
            <p className="text-zinc-300 leading-relaxed mb-8">
              Superseded memories receive a 0.3× recall score penalty, keeping them accessible for
              historical queries while ensuring current facts dominate. The bi-temporal model tracks
              both when a fact was true in the world (valid time) and when Cortex learned about it
              (transaction time).
            </p>

            {/* Timeline diagram */}
            <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-8">
              <p className="text-sm text-zinc-500 mb-6 font-mono uppercase tracking-wide">Version timeline</p>
              <div className="space-y-0">
                {/* Timeline entries */}
                {[
                  {
                    version: "v1",
                    label: "Alice uses JavaScript",
                    status: "superseded",
                    validFrom: "2026-01-01",
                    validTo: "2026-02-15",
                    color: "border-zinc-600 bg-zinc-800/50",
                    textColor: "text-zinc-500",
                    badge: "superseded · ×0.3 penalty",
                    badgeColor: "text-zinc-600 bg-zinc-900 border border-zinc-700",
                  },
                  {
                    version: "v2",
                    label: "Alice uses TypeScript",
                    status: "superseded",
                    validFrom: "2026-02-15",
                    validTo: "2026-03-10",
                    color: "border-amber-700/50 bg-amber-900/10",
                    textColor: "text-amber-400/70",
                    badge: "superseded · ×0.3 penalty",
                    badgeColor: "text-amber-700 bg-amber-900/20 border border-amber-700/30",
                  },
                  {
                    version: "v3",
                    label: "Alice uses TypeScript + Go",
                    status: "current",
                    validFrom: "2026-03-10",
                    validTo: null,
                    color: "border-emerald-500/40 bg-emerald-500/5",
                    textColor: "text-emerald-300",
                    badge: "current · full score",
                    badgeColor: "text-emerald-400 bg-emerald-500/10 border border-emerald-500/20",
                  },
                ].map((entry, i) => (
                  <div key={entry.version} className="flex items-stretch gap-4">
                    {/* Timeline spine */}
                    <div className="flex flex-col items-center w-8 flex-shrink-0">
                      <div className={`w-3 h-3 rounded-full border-2 mt-5 flex-shrink-0 ${
                        entry.status === "current" ? "border-emerald-400 bg-emerald-400/20" : "border-zinc-600 bg-zinc-800"
                      }`} />
                      {i < 2 && <div className="flex-1 w-px bg-zinc-700 mt-1" />}
                    </div>
                    {/* Card */}
                    <div className={`flex-1 border rounded-lg p-4 mb-3 ${entry.color}`}>
                      <div className="flex items-start justify-between gap-3 flex-wrap">
                        <div>
                          <span className="text-xs text-zinc-600 font-mono mr-2">{entry.version}</span>
                          <span className={`text-sm font-medium ${entry.textColor}`}>{entry.label}</span>
                        </div>
                        <span className={`text-xs px-2 py-0.5 rounded-full font-medium ${entry.badgeColor}`}>
                          {entry.badge}
                        </span>
                      </div>
                      <div className="mt-2 text-xs text-zinc-600 font-mono">
                        valid_from: {entry.validFrom} · valid_to: {entry.validTo ?? "null (current)"}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </section>

          {/* ── 4. Contradiction Detection ── */}
          <section id="contradiction-detection" className="scroll-mt-24">
            <h2 className="text-2xl font-bold text-zinc-50 mb-3">Contradiction Detection</h2>
            <p className="text-zinc-300 leading-relaxed mb-3">
              When a new memory contradicts an existing one, both are placed in a conflict group
              rather than silently overwriting the older fact. Conflicting memories receive a
              0.8× recall penalty, and the conflict is surfaced in recall metadata so agents can
              prompt for clarification rather than guessing.
            </p>
            <p className="text-zinc-300 leading-relaxed mb-8">
              Conflicts resolve through three paths: a superseding memory closes the group,
              an explicit API resolution, or TTL lifecycle expiry. Contradiction detection runs
              at capture time against recent memories in the same scope and project.
            </p>

            {/* Conflict diagram */}
            <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-8">
              <p className="text-sm text-zinc-500 mb-6 font-mono uppercase tracking-wide">Conflict group formation</p>
              <div className="flex flex-col items-center gap-4">
                {/* Two conflicting memories */}
                <div className="flex gap-4 w-full flex-col sm:flex-row">
                  <div className="flex-1 bg-red-500/5 border border-red-500/30 rounded-lg p-4">
                    <p className="text-xs text-red-400 font-mono mb-1">Memory A</p>
                    <p className="text-sm text-zinc-300">&quot;Alice prefers dark mode&quot;</p>
                    <p className="text-xs text-zinc-600 mt-2 font-mono">captured: 2026-03-10</p>
                  </div>
                  <div className="flex-1 bg-red-500/5 border border-red-500/30 rounded-lg p-4">
                    <p className="text-xs text-red-400 font-mono mb-1">Memory B (new)</p>
                    <p className="text-sm text-zinc-300">&quot;Alice switched to light mode&quot;</p>
                    <p className="text-xs text-zinc-600 mt-2 font-mono">captured: 2026-03-15</p>
                  </div>
                </div>

                {/* Arrow down */}
                <div className="flex flex-col items-center">
                  <div className="w-px h-4 bg-zinc-700" />
                  <span className="text-xs text-zinc-600">Claude Haiku detects contradiction</span>
                  <div className="w-px h-4 bg-zinc-700" />
                  <svg className="w-4 h-4 text-zinc-600" fill="currentColor" viewBox="0 0 24 24">
                    <path d="M12 16l-6-6h12z" />
                  </svg>
                </div>

                {/* Conflict group */}
                <div className="w-full bg-amber-500/5 border border-amber-500/30 rounded-lg p-4">
                  <p className="text-xs text-amber-400 font-mono mb-2">Conflict Group</p>
                  <div className="flex flex-wrap gap-2 text-xs text-zinc-500">
                    <span className="bg-zinc-800 rounded px-2 py-1 font-mono">members: [A, B]</span>
                    <span className="bg-zinc-800 rounded px-2 py-1 font-mono">score_multiplier: ×0.8</span>
                    <span className="bg-zinc-800 rounded px-2 py-1 font-mono">status: active</span>
                  </div>
                  <p className="text-xs text-zinc-600 mt-3">
                    Both memories returned in recall with conflict metadata — agent can request resolution
                  </p>
                </div>
              </div>
            </div>
          </section>

          {/* ── 5. Episodic Extraction ── */}
          <section id="episodic-extraction" className="scroll-mt-24">
            <h2 className="text-2xl font-bold text-zinc-50 mb-3">Episodic Extraction</h2>
            <p className="text-zinc-300 leading-relaxed mb-3">
              Related memories captured in the same session are grouped into Episode nodes in the
              graph. An episode represents a coherent narrative arc — a debugging session, an
              onboarding conversation, a decision-making thread — with a synthesized summary and
              links to all participating memories and entities.
            </p>
            <p className="text-zinc-300 leading-relaxed mb-8">
              Episodic context feeds into RRF fusion during recall, allowing queries about past
              events to surface the full narrative context rather than isolated fragments.
              Episodes are created automatically at session boundaries.
            </p>

            {/* Episode diagram */}
            <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-8">
              <p className="text-sm text-zinc-500 mb-6 font-mono uppercase tracking-wide">Episode graph structure</p>
              <div className="flex flex-col items-center gap-0">
                {/* Episode node */}
                <div className="bg-violet-500/10 border border-violet-500/40 rounded-lg px-6 py-3 text-center">
                  <p className="text-xs text-violet-400 font-mono">Episode</p>
                  <p className="text-sm text-zinc-200 mt-0.5">&quot;Resolved deployment issue with Docker networking&quot;</p>
                  <p className="text-xs text-zinc-600 mt-1 font-mono">2026-03-15 · 4 memories · 2 entities</p>
                </div>

                {/* Connections */}
                <div className="flex gap-6 mt-0 w-full justify-center">
                  {[
                    { label: "Memory", text: "Docker compose restart\nfixes port conflict", color: "border-zinc-700 bg-zinc-800/40" },
                    { label: "Memory", text: "Port 5432 was in\nuse by local Postgres", color: "border-zinc-700 bg-zinc-800/40" },
                    { label: "Entity", text: "Docker\n(Tool)", color: "border-emerald-700/40 bg-emerald-900/10" },
                  ].map((node, i) => (
                    <div key={i} className="flex flex-col items-center">
                      <div className="w-px h-5 bg-zinc-700" />
                      <div className={`border rounded-lg p-3 text-center max-w-[120px] ${node.color}`}>
                        <p className="text-xs text-zinc-500 font-mono">{node.label}</p>
                        <p className="text-xs text-zinc-400 mt-0.5 whitespace-pre-line leading-tight">{node.text}</p>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          </section>

          {/* ── 6. Smart Capture ── */}
          <section id="smart-capture" className="scroll-mt-24">
            <h2 className="text-2xl font-bold text-zinc-50 mb-3">Smart Capture</h2>
            <p className="text-zinc-300 leading-relaxed mb-3">
              The capture pipeline transforms raw conversation turns into structured memories,
              entities, and facts in a single LLM round-trip using Claude Haiku. Every step
              — memory extraction, entity resolution, fact extraction, deduplication — runs
              automatically with no configuration required.
            </p>
            <p className="text-zinc-300 leading-relaxed mb-8">
              User and assistant content is XML-escaped before prompt interpolation to prevent
              injection attacks. Memories below 0.5 confidence are filtered at capture time.
              The heuristic classifier assigns memory types (rule, fact, episode, procedure,
              preference) without an additional LLM call.
            </p>

            {/* Pipeline diagram */}
            <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-8">
              <p className="text-sm text-zinc-500 mb-6 font-mono uppercase tracking-wide">Capture pipeline</p>
              <div className="flex flex-col gap-0 items-center">
                {[
                  {
                    step: "1",
                    label: "User message",
                    detail: "XML-escaped, sent to Claude Haiku",
                    color: "bg-zinc-800 border-zinc-700",
                    textColor: "text-zinc-300",
                  },
                  {
                    step: "2",
                    label: "Memory extraction",
                    detail: "Claude Haiku → JSON array of memories + confidence scores",
                    color: "bg-indigo-500/10 border-indigo-500/30",
                    textColor: "text-indigo-300",
                  },
                  {
                    step: "3",
                    label: "Entity & fact extraction",
                    detail: "Named entities + relationship triples extracted in parallel",
                    color: "bg-emerald-500/10 border-emerald-500/30",
                    textColor: "text-emerald-300",
                  },
                  {
                    step: "4",
                    label: "Heuristic classification",
                    detail: "Keyword scoring assigns type: rule / fact / episode / procedure / preference",
                    color: "bg-amber-500/10 border-amber-500/30",
                    textColor: "text-amber-300",
                  },
                  {
                    step: "5",
                    label: "Deduplication",
                    detail: "Cosine similarity check against existing embeddings (threshold: 0.92)",
                    color: "bg-sky-500/10 border-sky-500/30",
                    textColor: "text-sky-300",
                  },
                  {
                    step: "6",
                    label: "Graph upsert",
                    detail: "Memories, entities, and facts written to Memgraph in single transaction",
                    color: "bg-violet-500/10 border-violet-500/30",
                    textColor: "text-violet-300",
                  },
                ].map((item, i, arr) => (
                  <div key={item.step} className="flex flex-col items-center w-full max-w-lg">
                    <div className={`w-full border rounded-lg p-4 ${item.color}`}>
                      <div className="flex items-center gap-3">
                        <span className="w-6 h-6 rounded-full bg-zinc-800 border border-zinc-700 flex items-center justify-center text-xs font-mono text-zinc-400 flex-shrink-0">
                          {item.step}
                        </span>
                        <div>
                          <p className={`text-sm font-medium ${item.textColor}`}>{item.label}</p>
                          <p className="text-xs text-zinc-500 mt-0.5">{item.detail}</p>
                        </div>
                      </div>
                    </div>
                    {i < arr.length - 1 && (
                      <div className="flex flex-col items-center">
                        <div className="w-px h-3 bg-zinc-700" />
                        <svg className="w-3 h-3 text-zinc-600" fill="currentColor" viewBox="0 0 24 24">
                          <path d="M12 16l-6-6h12z" />
                        </svg>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          </section>
        </div>
      </div>
    </div>
  );
}
