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
          className="text-xs text-zinc-500 hover:text-zinc-300"
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
          className="text-xs text-zinc-500 hover:text-zinc-300"
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
