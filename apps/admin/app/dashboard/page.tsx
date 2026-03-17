"use client";

import Link from "next/link";
import useSWR from "swr";
import { cortex } from "@/lib/api";
import { StatsCards } from "@/components/stats-cards";
import { Badge } from "@/components/ui/badge";
import type { CollectionStats } from "@/lib/types";

export default function DashboardPage() {
  const {
    data: stats,
    error,
    isLoading,
  } = useSWR<CollectionStats>("/v1/stats", () => cortex.stats(), {
    refreshInterval: 30_000,
  });

  if (isLoading) {
    return (
      <div className="space-y-4">
        <h1 className="text-xl font-semibold text-zinc-100">Dashboard</h1>
        <p className="text-sm text-zinc-500">Loading stats...</p>
      </div>
    );
  }

  if (error || !stats) {
    return (
      <div className="space-y-4">
        <h1 className="text-xl font-semibold text-zinc-100">Dashboard</h1>
        <p className="text-sm text-red-400">
          Failed to load stats:{" "}
          {error instanceof Error ? error.message : "unknown error"}. Check the{" "}
          <Link href="/settings" className="underline">
            Settings
          </Link>{" "}
          page.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-zinc-100">Dashboard</h1>
        {stats.active_conflicts > 0 && (
          <Link href="/conflicts">
            <Badge variant="destructive">
              {stats.active_conflicts} active conflict
              {stats.active_conflicts > 1 ? "s" : ""}
            </Badge>
          </Link>
        )}
      </div>

      <StatsCards stats={stats} />

      {stats.top_accessed && stats.top_accessed.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-sm font-medium text-zinc-400">Top Accessed</h2>
          <ul className="space-y-1.5">
            {stats.top_accessed.slice(0, 5).map((m) => (
              <li key={m.id}>
                <Link
                  href={`/memories/${m.id}`}
                  className="flex items-center justify-between rounded-md border border-zinc-800 bg-zinc-900 px-3 py-2 transition-colors hover:bg-zinc-800"
                >
                  <span className="truncate font-mono text-xs text-zinc-300">
                    {m.content}
                  </span>
                  <span className="ml-4 shrink-0 font-mono text-xs text-zinc-500">
                    {m.access_count}x
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
