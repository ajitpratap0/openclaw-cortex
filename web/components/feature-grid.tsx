import Card from "@/components/ui/card";

const features = [
  {
    icon: "🔍",
    title: "Semantic Recall",
    description:
      "Vector similarity search across 768-dim embeddings with cosine distance.",
  },
  {
    icon: "🕸️",
    title: "Graph Traversal",
    description:
      "Entity-seeded walks with Reciprocal Rank Fusion merge for richer context.",
  },
  {
    icon: "🧠",
    title: "Smart Capture",
    description:
      "Claude Haiku extracts structured memories from conversations automatically.",
  },
  {
    icon: "⏳",
    title: "Temporal Versioning",
    description:
      "Memories evolve over time with full version history and decay scoring.",
  },
  {
    icon: "⚡",
    title: "Contradiction Detection",
    description:
      "Conflicting facts are flagged with shared conflict groups and penalized.",
  },
  {
    icon: "📐",
    title: "Token-Aware Output",
    description:
      "Recalled context is trimmed to fit your token budget — zero waste.",
  },
];

export default function FeatureGrid() {
  return (
    <section className="py-24">
      <div className="max-w-6xl mx-auto px-6">
        {/* Section heading */}
        <div className="text-center mb-14">
          <p className="text-sm font-semibold text-indigo-400 uppercase tracking-widest mb-3">
            Capabilities
          </p>
          <h2 className="text-3xl sm:text-4xl font-bold text-zinc-50">
            Features
          </h2>
          <p className="text-zinc-400 mt-4 max-w-xl mx-auto">
            Everything you need to give your AI agent persistent, intelligent
            memory.
          </p>
        </div>

        {/* 3×2 grid */}
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-6">
          {features.map((feature) => (
            <Card key={feature.title} hover>
              <div className="text-3xl mb-4">{feature.icon}</div>
              <h3 className="text-base font-semibold text-zinc-50 mb-2">
                {feature.title}
              </h3>
              <p className="text-sm text-zinc-400 leading-relaxed">
                {feature.description}
              </p>
            </Card>
          ))}
        </div>
      </div>
    </section>
  );
}
