interface TableSkeletonProps {
  rows?: number;
}

export function TableSkeleton({ rows = 5 }: TableSkeletonProps) {
  return (
    <div className="space-y-4 p-6">
      <div className="h-8 w-48 animate-pulse rounded bg-zinc-800" />
      <div className="space-y-2">
        {Array.from({ length: rows }).map((_, i) => (
          <div key={i} className="h-12 w-full animate-pulse rounded bg-zinc-800" />
        ))}
      </div>
    </div>
  );
}
