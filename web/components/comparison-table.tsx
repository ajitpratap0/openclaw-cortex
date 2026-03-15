type CellValue = "yes" | "no" | "partial" | "n/a";

interface Feature {
  name: string;
  cortex: CellValue;
  mem0: CellValue;
  zep: CellValue;
  langchain: CellValue;
  rawVector: CellValue;
}

const features: Feature[] = [
  {
    name: "Multi-factor ranking",
    cortex: "yes",
    mem0: "yes",
    zep: "partial",
    langchain: "no",
    rawVector: "no",
  },
  {
    name: "Graph traversal",
    cortex: "yes",
    mem0: "partial",
    zep: "yes",
    langchain: "no",
    rawVector: "no",
  },
  {
    name: "Temporal versioning",
    cortex: "yes",
    mem0: "no",
    zep: "no",
    langchain: "no",
    rawVector: "no",
  },
  {
    name: "Contradiction detection",
    cortex: "yes",
    mem0: "no",
    zep: "no",
    langchain: "no",
    rawVector: "no",
  },
  {
    name: "Self-hosted",
    cortex: "yes",
    mem0: "yes",
    zep: "yes",
    langchain: "yes",
    rawVector: "yes",
  },
  {
    name: "Single container",
    cortex: "yes",
    mem0: "no",
    zep: "no",
    langchain: "n/a",
    rawVector: "yes",
  },
  {
    name: "Claude integration",
    cortex: "yes",
    mem0: "no",
    zep: "no",
    langchain: "partial",
    rawVector: "no",
  },
  {
    name: "Auto-deduplication",
    cortex: "yes",
    mem0: "yes",
    zep: "yes",
    langchain: "no",
    rawVector: "partial",
  },
  {
    name: "Lifecycle management",
    cortex: "yes",
    mem0: "partial",
    zep: "partial",
    langchain: "no",
    rawVector: "no",
  },
  {
    name: "Episodic memory",
    cortex: "yes",
    mem0: "no",
    zep: "partial",
    langchain: "no",
    rawVector: "no",
  },
];

function Cell({ value }: { value: CellValue }) {
  if (value === "yes") {
    return (
      <span className="inline-flex items-center justify-center">
        <svg
          className="w-5 h-5 text-emerald-400"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
          aria-label="Yes"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2.5}
            d="M5 13l4 4L19 7"
          />
        </svg>
        <span className="sr-only">Yes</span>
      </span>
    );
  }

  if (value === "no") {
    return (
      <span className="inline-flex items-center justify-center">
        <svg
          className="w-5 h-5 text-red-400"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
          aria-label="No"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2.5}
            d="M6 18L18 6M6 6l12 12"
          />
        </svg>
        <span className="sr-only">No</span>
      </span>
    );
  }

  if (value === "partial") {
    return (
      <span className="inline-flex items-center justify-center text-amber-400 font-bold text-lg leading-none" aria-label="Partial">
        ~
      </span>
    );
  }

  return (
    <span className="text-zinc-600 text-sm" aria-label="Not applicable">
      —
    </span>
  );
}

const columns = [
  { key: "cortex" as const, label: "OpenClaw Cortex", highlight: true },
  { key: "mem0" as const, label: "mem0", highlight: false },
  { key: "zep" as const, label: "Zep", highlight: false },
  { key: "langchain" as const, label: "LangChain Memory", highlight: false },
  { key: "rawVector" as const, label: "Raw Vector DB", highlight: false },
];

export default function ComparisonTable() {
  return (
    <div className="overflow-x-auto rounded-xl border border-zinc-800">
      <table className="min-w-full">
        <thead>
          <tr className="border-b border-zinc-800">
            {/* Sticky feature column header */}
            <th
              scope="col"
              className="sticky left-0 z-10 bg-zinc-950 px-6 py-4 text-left text-xs font-semibold text-zinc-500 uppercase tracking-wider min-w-[180px]"
            >
              Feature
            </th>
            {columns.map((col) => (
              <th
                key={col.key}
                scope="col"
                className={`px-6 py-4 text-center text-xs font-semibold uppercase tracking-wider whitespace-nowrap ${
                  col.highlight
                    ? "text-indigo-400 bg-indigo-500/5"
                    : "text-zinc-500 bg-zinc-950"
                }`}
              >
                {col.highlight && (
                  <span className="block text-indigo-400 mb-0.5">★</span>
                )}
                {col.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-zinc-800/60">
          {features.map((feature, idx) => (
            <tr
              key={feature.name}
              className={idx % 2 === 0 ? "bg-zinc-950" : "bg-zinc-900/30"}
            >
              {/* Sticky feature name */}
              <td
                className={`sticky left-0 z-10 px-6 py-4 text-sm font-medium text-zinc-200 ${
                  idx % 2 === 0 ? "bg-zinc-950" : "bg-zinc-900/80"
                }`}
              >
                {feature.name}
              </td>
              {columns.map((col) => (
                <td
                  key={col.key}
                  className={`px-6 py-4 text-center ${
                    col.highlight ? "bg-indigo-500/5" : ""
                  }`}
                >
                  <Cell value={feature[col.key]} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
