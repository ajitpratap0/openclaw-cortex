import CodeBlock from "@/components/ui/code-block";

const captureCode = `$ openclaw-cortex capture \\
  --user "We decided to use Memgraph for both \\
           vector search and graph traversal" \\
  --assistant "Solid choice — unified storage \\
               reduces operational overhead" \\
  --project "infra-decisions"

Embedding... done (768-dim)
Dedup check... no duplicates found
Captured [fact]: Memgraph chosen for unified vector + graph storage
  confidence: 0.91  scope: project  tags: [memgraph, storage, vectors]`;

const recallCode = `$ openclaw-cortex recall "database decision" \\
  --project "infra-decisions" \\
  --limit 3

Embedding query... done
Graph traversal... 4 entities seeded

[1] (92%) [fact] scope=project
    Memgraph chosen for unified vector + graph storage
    tags: [memgraph, storage, vectors]

[2] (67%) [rule] scope=permanent
    Always validate schema on startup
    tags: [startup, validation]

[3] (54%) [episode] scope=session
    Evaluated Qdrant as alternative, ruled out
    tags: [qdrant, storage, evaluation]`;

export default function CodeExample() {
  return (
    <section className="py-24">
      <div className="max-w-6xl mx-auto px-6">
        {/* Section heading */}
        <div className="text-center mb-14">
          <p className="text-sm font-semibold text-indigo-400 uppercase tracking-widest mb-3">
            CLI
          </p>
          <h2 className="text-3xl sm:text-4xl font-bold text-zinc-50">
            See It In Action
          </h2>
          <p className="text-zinc-400 mt-4 max-w-xl mx-auto">
            Capture memories from conversations and recall them with intelligent
            ranking — from the terminal or via the MCP plugin.
          </p>
        </div>

        {/* Split code view */}
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          <div>
            <p className="text-xs font-semibold text-zinc-500 uppercase tracking-wider mb-2 ml-1">
              Capture
            </p>
            <CodeBlock
              code={captureCode}
              language="bash"
              filename="terminal"
            />
          </div>
          <div>
            <p className="text-xs font-semibold text-zinc-500 uppercase tracking-wider mb-2 ml-1">
              Recall
            </p>
            <CodeBlock
              code={recallCode}
              language="bash"
              filename="terminal"
            />
          </div>
        </div>
      </div>
    </section>
  );
}
