import Button from "@/components/ui/button";
import Badge from "@/components/ui/badge";

export default function HomePage() {
  return (
    <div className="relative flex flex-col items-center justify-center min-h-[80vh] px-4 text-center overflow-hidden">
      {/* Background grid */}
      <div className="absolute inset-0 bg-grid opacity-40 pointer-events-none" />
      {/* Radial glow */}
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[600px] h-[400px] gradient-radial-indigo pointer-events-none" />

      <div className="relative z-10 max-w-3xl mx-auto">
        <Badge variant="indigo" className="mb-6">
          Open Source · MIT License
        </Badge>

        <h1 className="text-4xl sm:text-5xl md:text-6xl font-bold text-zinc-50 leading-tight mb-6">
          Hybrid Semantic Memory
          <br />
          <span className="text-gradient">for AI Agents</span>
        </h1>

        <p className="text-lg text-zinc-400 max-w-xl mx-auto mb-8 leading-relaxed">
          OpenClaw Cortex stores and retrieves memories using multi-factor
          ranking with Memgraph graph traversal, vector search, and an 8-factor
          scoring algorithm.
        </p>

        <div className="flex items-center justify-center gap-4 flex-wrap">
          <Button variant="primary" size="lg" href="/docs/getting-started">
            Get Started
          </Button>
          <Button
            variant="outline"
            size="lg"
            href="https://github.com/ajitpratap0/openclaw-cortex"
            target="_blank"
            rel="noopener noreferrer"
          >
            View on GitHub
          </Button>
        </div>
      </div>
    </div>
  );
}
