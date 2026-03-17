"use client";

import { useCallback } from "react";
import useSWR from "swr";
import { cortex } from "@/lib/api";
import { ConflictCard } from "@/components/conflict-card";
import type { ListMemoriesResponse, Memory } from "@/lib/types";

export default function ConflictsPage() {
  const { data, error, isLoading, mutate } = useSWR<ListMemoriesResponse>(
    "/v1/memories?limit=1000",
    () => cortex.listMemories({ limit: 1000 })
  );

  const conflicts: Memory[] = (data?.memories ?? []).filter(
    (m) => m.conflict_status === "active"
  );

  // "Mark Resolved" optimistically updates the cache and triggers revalidation.
  // Note: PUT /v1/memories/{id} does not currently accept conflict_status in the
  // request body (server's updateRequest struct omits it). The optimistic update
  // makes the UI responsive; a server-side extension is tracked in Known
  // Limitations.
  const handleResolve = useCallback(
    async (id: string) => {
      await mutate(
        (prev) =>
          prev
            ? {
                ...prev,
                memories: prev.memories.map((m) =>
                  m.id === id
                    ? { ...m, conflict_status: "resolved" as const }
                    : m
                ),
              }
            : prev,
        { revalidate: true }
      );
    },
    [mutate]
  );

  // "Dismiss" hides the card locally without revalidating.
  const handleDismiss = useCallback(
    async (id: string) => {
      await mutate(
        (prev) =>
          prev
            ? {
                ...prev,
                memories: prev.memories.filter((m) => m.id !== id),
              }
            : prev,
        { revalidate: false }
      );
    },
    [mutate]
  );

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-zinc-100">Conflicts</h1>
        {!isLoading && (
          <span className="font-mono text-sm text-zinc-500">
            {conflicts.length} active conflict
            {conflicts.length !== 1 ? "s" : ""}
          </span>
        )}
      </div>

      {isLoading && <p className="text-sm text-zinc-500">Loading...</p>}
      {error && (
        <p className="text-sm text-red-400">
          {error instanceof Error ? error.message : "Failed to load memories"}
        </p>
      )}
      {!isLoading && !error && conflicts.length === 0 && (
        <p className="text-sm text-zinc-500">No active conflicts.</p>
      )}

      <div className="space-y-3">
        {conflicts.map((m) => (
          <ConflictCard
            key={m.id}
            memory={m}
            onResolve={handleResolve}
            onDismiss={handleDismiss}
          />
        ))}
      </div>
    </div>
  );
}
