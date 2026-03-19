import Button from "@/components/ui/button";

export default function CTASection() {
  return (
    <section className="relative py-32 overflow-hidden">
      {/* Emerald radial glow */}
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[700px] h-[500px] gradient-radial-emerald pointer-events-none blur-3xl opacity-50" />

      <div className="relative z-10 max-w-6xl mx-auto px-6 text-center">
        <h2 className="text-3xl sm:text-4xl md:text-5xl font-bold text-zinc-50 mb-6">
          Ready to give your agent memory?
        </h2>
        <p className="text-zinc-300 text-lg max-w-xl mx-auto mb-10">
          Deploy in minutes with Docker. Works with any LLM via MCP or the CLI.
          Open source under MIT.
        </p>

        <div className="flex items-center justify-center gap-4 flex-wrap">
          <Button variant="primary" size="lg" href="/docs/getting-started">
            Get Started
          </Button>
          <Button
            variant="ghost"
            size="lg"
            href="https://github.com/ajitpratap0/openclaw-cortex"
            target="_blank"
            rel="noopener noreferrer"
          >
            View on GitHub
          </Button>
        </div>
      </div>
    </section>
  );
}
