const stats = [
  { value: "8-factor", label: "ranking factors" },
  { value: "768-dim", label: "embeddings" },
  { value: "<50ms", label: "hook latency" },
  { value: "1", label: "container" },
];

export default function StatsBar() {
  return (
    <section className="border-y border-zinc-800 py-10">
      <div className="max-w-6xl mx-auto px-6">
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-6 sm:gap-0 sm:divide-x sm:divide-zinc-800">
          {stats.map((stat) => (
            <div
              key={stat.label}
              className="flex flex-col items-center text-center sm:px-6"
            >
              <span className="text-3xl sm:text-4xl font-bold text-zinc-50 tabular-nums">
                {stat.value}
              </span>
              <span className="text-sm text-zinc-400 mt-1">{stat.label}</span>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
