"use client";

import { useState } from "react";
import useSWR from "swr";
import { cortex } from "@/lib/api";
import { EntityTable } from "@/components/entity-table";
import { Input } from "@/components/ui/input";
import {
  Select, SelectContent, SelectItem,
  SelectTrigger, SelectValue,
} from "@/components/ui/select";
import type { SearchEntitiesResponse } from "@/lib/types";

const ENTITY_TYPES = ["", "person", "project", "system", "decision", "concept"];

export default function EntitiesPage() {
  const [query,      setQuery]      = useState("");
  const [typeFilter, setTypeFilter] = useState("");

  const { data, error, isLoading } = useSWR<SearchEntitiesResponse>(
    ["/v1/entities", query, typeFilter],
    () =>
      cortex.searchEntities({
        query: query      || undefined,
        type:  typeFilter || undefined,
        limit: 50,
      })
  );

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold text-zinc-100">Entities</h1>

      <div className="flex flex-wrap items-center gap-2">
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search entities..."
          className="w-56 text-sm"
        />
        <Select value={typeFilter} onValueChange={(v) => setTypeFilter(v ?? "")}>
          <SelectTrigger className="w-36 text-xs">
            <SelectValue placeholder="All types" />
          </SelectTrigger>
          <SelectContent>
            {ENTITY_TYPES.map((t) => (
              <SelectItem key={t} value={t}>
                {t === "" ? "All types" : t}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <span className="ml-auto font-mono text-xs text-zinc-500">
          {data ? `${data.entities.length} entities` : ""}
        </span>
      </div>

      {isLoading && <p className="text-sm text-zinc-500">Loading...</p>}
      {error && (
        <p className="text-sm text-red-400">
          {error instanceof Error ? error.message : "Failed to load entities"}
        </p>
      )}
      {data && <EntityTable entities={data.entities} />}
    </div>
  );
}
