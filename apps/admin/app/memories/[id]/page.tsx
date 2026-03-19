"use client";

import { use } from "react";
import { useRouter } from "next/navigation";
import useSWR from "swr";
import { cortex } from "@/lib/api";
import { MemoryDetail } from "@/components/memory-detail";
import type { Memory } from "@/lib/types";

export default function MemoryDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const router = useRouter();

  const {
    data: memory,
    error,
    isLoading,
    mutate,
  } = useSWR<Memory>(
    `/v1/memories/${id}`,
    () => cortex.getMemory(id)
  );

  if (isLoading) {
    return <p className="text-sm text-zinc-500">Loading memory...</p>;
  }

  if (error || !memory) {
    return (
      <div className="space-y-2">
        <button
          onClick={() => router.back()}
          aria-label="Back to memories list"
          className="flex items-center gap-1 text-sm text-zinc-400 hover:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-zinc-500 rounded transition-colors"
        >
          Back
        </button>
        <p className="text-sm text-red-400">
          {error instanceof Error ? error.message : "Memory not found"}
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <button
          onClick={() => router.back()}
          aria-label="Back to memories list"
          className="flex items-center gap-1 text-sm text-zinc-400 hover:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-zinc-500 rounded transition-colors"
        >
          Back
        </button>
        <h1 className="font-mono text-sm text-zinc-400">{memory.id}</h1>
      </div>
      <MemoryDetail
        memory={memory}
        onUpdate={() => { void mutate(); }}
        onDelete={() => router.push("/memories")}
      />
    </div>
  );
}
