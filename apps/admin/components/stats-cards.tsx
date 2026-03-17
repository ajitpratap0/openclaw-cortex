"use client";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { CollectionStats } from "@/lib/types";

interface StatsCardsProps {
  stats: CollectionStats;
}

export function StatsCards({ stats }: StatsCardsProps) {
  const typeEntries  = Object.entries(stats.by_type  ?? {}).sort((a, b) => b[1] - a[1]);
  const scopeEntries = Object.entries(stats.by_scope ?? {}).sort((a, b) => b[1] - a[1]);

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <Card className="border-zinc-800 bg-zinc-900">
        <CardHeader className="pb-2">
          <CardTitle className="text-xs font-medium uppercase tracking-wide text-zinc-500">
            Total Memories
          </CardTitle>
        </CardHeader>
        <CardContent>
          <p className="font-mono text-3xl font-semibold text-zinc-100">
            {stats.total_memories.toLocaleString()}
          </p>
        </CardContent>
      </Card>

      <Card className="border-zinc-800 bg-zinc-900">
        <CardHeader className="pb-2">
          <CardTitle className="text-xs font-medium uppercase tracking-wide text-zinc-500">
            Active Conflicts
          </CardTitle>
        </CardHeader>
        <CardContent>
          <p
            className={`font-mono text-3xl font-semibold ${
              stats.active_conflicts > 0 ? "text-amber-400" : "text-zinc-100"
            }`}
          >
            {stats.active_conflicts}
          </p>
        </CardContent>
      </Card>

      <Card className="border-zinc-800 bg-zinc-900">
        <CardHeader className="pb-2">
          <CardTitle className="text-xs font-medium uppercase tracking-wide text-zinc-500">
            By Type
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-0.5">
          {typeEntries.map(([t, n]) => (
            <div key={t} className="flex justify-between text-xs">
              <span className="font-mono text-zinc-400">{t}</span>
              <span className="font-mono text-zinc-200">{n}</span>
            </div>
          ))}
          {typeEntries.length === 0 && (
            <span className="text-xs text-zinc-600">no data</span>
          )}
        </CardContent>
      </Card>

      <Card className="border-zinc-800 bg-zinc-900">
        <CardHeader className="pb-2">
          <CardTitle className="text-xs font-medium uppercase tracking-wide text-zinc-500">
            By Scope
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-0.5">
          {scopeEntries.map(([s, n]) => (
            <div key={s} className="flex justify-between text-xs">
              <span className="font-mono text-zinc-400">{s}</span>
              <span className="font-mono text-zinc-200">{n}</span>
            </div>
          ))}
          {scopeEntries.length === 0 && (
            <span className="text-xs text-zinc-600">no data</span>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
