export default function DocsLoading() {
  return (
    <div className="flex min-h-[calc(100vh-4rem)]">
      {/* Sidebar skeleton */}
      <aside className="hidden lg:block w-64 shrink-0 border-r border-zinc-800 py-6 px-4">
        <div className="space-y-6">
          {[1, 2, 3].map((s) => (
            <div key={s}>
              <div className="h-3 w-24 bg-zinc-800 rounded animate-pulse mb-3" />
              <div className="space-y-2">
                {[1, 2, 3].map((i) => (
                  <div
                    key={i}
                    className="h-7 bg-zinc-800/60 rounded animate-pulse"
                    style={{ width: `${60 + i * 10}%` }}
                  />
                ))}
              </div>
            </div>
          ))}
        </div>
      </aside>

      {/* Main content skeleton */}
      <main className="flex-1 min-w-0 px-6 py-8 lg:px-10 lg:py-10 max-w-3xl">
        {/* Breadcrumbs */}
        <div className="flex gap-2 mb-6">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-4 w-16 bg-zinc-800 rounded animate-pulse" />
          ))}
        </div>

        {/* Title */}
        <div className="h-9 w-2/3 bg-zinc-800 rounded animate-pulse mb-3" />
        {/* Subtitle */}
        <div className="h-5 w-1/2 bg-zinc-800/70 rounded animate-pulse mb-10" />

        {/* Content lines */}
        <div className="space-y-3">
          {[100, 90, 75, 95, 60, 85, 70, 100, 80, 65, 90, 55].map(
            (w, i) => (
              <div
                key={i}
                className="h-4 bg-zinc-800/50 rounded animate-pulse"
                style={{ width: `${w}%` }}
              />
            )
          )}
        </div>

        {/* Code block placeholder */}
        <div className="mt-8 h-32 bg-zinc-800/40 rounded-lg animate-pulse" />

        {/* More content lines */}
        <div className="space-y-3 mt-8">
          {[85, 70, 95, 60, 80].map((w, i) => (
            <div
              key={i}
              className="h-4 bg-zinc-800/50 rounded animate-pulse"
              style={{ width: `${w}%` }}
            />
          ))}
        </div>
      </main>

      {/* TOC skeleton */}
      <div className="hidden lg:block w-56 shrink-0 py-10 pr-6">
        <div className="h-3 w-20 bg-zinc-800 rounded animate-pulse mb-4" />
        <div className="space-y-2">
          {[1, 2, 3, 4, 5].map((i) => (
            <div
              key={i}
              className="h-4 bg-zinc-800/60 rounded animate-pulse"
              style={{ width: `${50 + i * 8}%` }}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
