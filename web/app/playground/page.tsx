import type { Metadata } from "next";
import PlaygroundLoader from "@/components/playground-loader";

export const metadata: Metadata = {
  title: "Interactive Playground",
  description:
    "See how the 8-factor recall ranking works on sample memories. A live demo using the same scoring algorithm as the real openclaw-cortex engine.",
  openGraph: {
    title: "Try the Recall Engine | OpenClaw Cortex",
    description:
      "Interactive demo of 8-factor memory ranking. See similarity, recency, frequency, and more applied in real time.",
  },
};

export default function PlaygroundPage() {
  return (
    <div className="min-h-screen">
      {/* Header */}
      <div className="border-b border-zinc-800 bg-zinc-950/80 backdrop-blur-sm">
        <div className="max-w-4xl mx-auto px-6 py-12">
          <div className="flex items-start gap-4">
            <div className="flex-shrink-0 w-10 h-10 rounded-xl bg-indigo-500/10 border border-indigo-500/20 flex items-center justify-center mt-0.5">
              <svg
                className="w-5 h-5 text-indigo-400"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={1.5}
                  d="M9 3H5a2 2 0 00-2 2v4m6-6h10a2 2 0 012 2v4M9 3v18m0 0h10a2 2 0 002-2V9M9 21H5a2 2 0 01-2-2V9m0 0h18"
                />
              </svg>
            </div>
            <div>
              <p className="text-sm font-semibold text-indigo-400 uppercase tracking-widest mb-2">
                Interactive Demo
              </p>
              <h1 className="text-3xl sm:text-4xl font-bold text-zinc-50 mb-3">
                Try the Recall Engine
              </h1>
              <p className="text-zinc-400 max-w-2xl leading-relaxed">
                See how 8-factor ranking works on sample memories. This is a
                simulated demo using the same scoring algorithm as the real
                engine — running entirely in your browser, no server required.
              </p>
            </div>
          </div>

          {/* Scoring formula callout */}
          <div className="mt-6 rounded-xl border border-zinc-800 bg-zinc-900/60 p-4">
            <p className="text-xs font-semibold text-zinc-500 uppercase tracking-widest mb-2">
              Scoring Formula
            </p>
            <code className="text-xs text-indigo-300 font-mono leading-relaxed break-all">
              0.35×similarity + 0.15×recency + 0.10×frequency + 0.10×typeBoost
              + 0.08×scopeBoost + 0.10×confidence + 0.07×reinforcement +
              0.05×tagAffinity
            </code>
            <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-zinc-600">
              <span>Superseded memories receive a ×0.3 penalty</span>
              <span>Active conflicts receive a ×0.8 penalty</span>
              <span>Recency uses 7-day exponential half-life</span>
            </div>
          </div>
        </div>
      </div>

      {/* Playground content */}
      <div className="max-w-4xl mx-auto px-6 py-10">
        <PlaygroundLoader />
      </div>

      {/* Footer note */}
      <div className="max-w-4xl mx-auto px-6 pb-12">
        <div className="rounded-xl border border-zinc-800/60 bg-zinc-900/30 p-4">
          <div className="flex items-start gap-3">
            <div className="flex-shrink-0 w-5 h-5 rounded-full bg-amber-500/10 border border-amber-500/20 flex items-center justify-center mt-0.5">
              <span className="text-[10px] text-amber-400 font-bold">!</span>
            </div>
            <p className="text-xs text-zinc-500 leading-relaxed">
              <span className="text-zinc-400 font-medium">Note:</span> Similarity
              scoring here uses keyword overlap, not real cosine similarity on
              768-dimensional vectors. In the actual engine, semantic similarity
              is computed via{" "}
              <code className="text-emerald-400 font-mono text-[11px]">
                nomic-embed-text
              </code>{" "}
              embeddings and Memgraph vector search.
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
