"use client";

import { useState, useCallback } from "react";
import useSWR from "swr";
import { cortex } from "@/lib/api";
import { MemoryTable } from "@/components/memory-table";
import { Button } from "@/components/ui/button";
import { Input }  from "@/components/ui/input";
import {
  Select, SelectContent, SelectItem,
  SelectTrigger, SelectValue,
} from "@/components/ui/select";
import type { ListMemoriesResponse } from "@/lib/types";

const MEMORY_TYPES  = ["", "rule", "fact", "episode", "procedure", "preference"];
const MEMORY_SCOPES = ["", "permanent", "project", "session", "ttl"];

export default function MemoriesPage() {
  const [typeFilter,    setTypeFilter]    = useState("");
  const [scopeFilter,   setScopeFilter]   = useState("");
  const [projectFilter, setProjectFilter] = useState("");
  const [cursor,        setCursor]        = useState("");

  const swrKey = ["/v1/memories", typeFilter, scopeFilter, projectFilter, cursor];

  const { data, error, isLoading, mutate } = useSWR<ListMemoriesResponse>(
    swrKey,
    () =>
      cortex.listMemories({
        type:    typeFilter    || undefined,
        scope:   scopeFilter   || undefined,
        project: projectFilter || undefined,
        limit:   50,
        cursor:  cursor        || undefined,
      })
  );

  const handleDelete = useCallback(
    async (id: string) => {
      if (!confirm(`Delete memory ${id}?`)) return;
      try {
        await cortex.deleteMemory(id);
        await mutate();
      } catch (e) {
        alert(e instanceof Error ? e.message : String(e));
      }
    },
    [mutate]
  );

  function handleReset() {
    setTypeFilter("");
    setScopeFilter("");
    setProjectFilter("");
    setCursor("");
  }

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold text-zinc-100">Memories</h1>

      {/* Filters */}
      <div className="flex flex-wrap items-center gap-2">
        <Select value={typeFilter} onValueChange={(v) => setTypeFilter(v ?? "")}>
          <SelectTrigger className="w-36 text-xs">
            <SelectValue placeholder="All types" />
          </SelectTrigger>
          <SelectContent>
            {MEMORY_TYPES.map((t) => (
              <SelectItem key={t} value={t}>
                {t === "" ? "All types" : t}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={scopeFilter} onValueChange={(v) => setScopeFilter(v ?? "")}>
          <SelectTrigger className="w-36 text-xs">
            <SelectValue placeholder="All scopes" />
          </SelectTrigger>
          <SelectContent>
            {MEMORY_SCOPES.map((s) => (
              <SelectItem key={s} value={s}>
                {s === "" ? "All scopes" : s}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Input
          value={projectFilter}
          onChange={(e) => setProjectFilter(e.target.value)}
          placeholder="Filter by project"
          className="w-44 text-xs"
        />

        {(typeFilter || scopeFilter || projectFilter || cursor) && (
          <Button variant="ghost" size="sm" onClick={handleReset}>
            Clear
          </Button>
        )}

        <span className="ml-auto font-mono text-xs text-zinc-500">
          {data ? `${data.memories.length} shown` : ""}
        </span>
      </div>

      {isLoading && <p className="text-sm text-zinc-500">Loading...</p>}
      {error && (
        <p className="text-sm text-red-400">
          {error instanceof Error ? error.message : "Failed to load memories"}
        </p>
      )}
      {data && <MemoryTable memories={data.memories} onDelete={handleDelete} />}

      {/* Pagination */}
      <div className="flex items-center gap-3">
        {cursor && (
          <Button variant="outline" size="sm" onClick={() => setCursor("")}>
            First page
          </Button>
        )}
        {data?.next_cursor && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => setCursor(data.next_cursor)}
          >
            Next page
          </Button>
        )}
      </div>
    </div>
  );
}
