"use client";

import Link from "next/link";
import { useState } from "react";
import useSWR from "swr";
import { cortex } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import {
  Table, TableBody, TableCell,
  TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import type { Entity } from "@/lib/types";

function LinkedMemories({ entityId, entityName }: { entityId: string; entityName: string }) {
  const [open, setOpen] = useState(false);

  const { data, isLoading } = useSWR<Entity>(
    open ? `/v1/entities/${entityId}` : null,
    () => cortex.getEntity(entityId)
  );

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        aria-label={`Show linked memories for ${entityName}`}
        aria-expanded={false}
        className="text-xs text-zinc-600 hover:text-zinc-300 focus:outline-none focus:ring-2 focus:ring-zinc-500 rounded underline underline-offset-2"
      >
        show linked memories
      </button>
    );
  }

  if (isLoading) {
    return <span className="text-xs text-zinc-500">loading...</span>;
  }

  const memoryIds: string[] = data?.memory_ids ?? [];

  return (
    <ul className="mt-1 space-y-0.5 text-xs">
      {memoryIds.length === 0 && (
        <li className="text-zinc-600">no linked memories</li>
      )}
      {memoryIds.map((mid) => (
        <li key={mid}>
          <Link
            href={`/memories/${mid}`}
            className="font-mono text-zinc-400 underline hover:text-zinc-100"
          >
            {mid}
          </Link>
        </li>
      ))}
    </ul>
  );
}

interface EntityTableProps {
  entities: Entity[];
}

export function EntityTable({ entities }: EntityTableProps) {
  if (entities.length === 0) {
    return <p className="text-sm text-zinc-500">No entities found.</p>;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow className="border-zinc-800 hover:bg-transparent">
          <TableHead className="text-zinc-400">Name</TableHead>
          <TableHead className="w-28 text-zinc-400">Type</TableHead>
          <TableHead className="w-32 text-zinc-400">Project</TableHead>
          <TableHead className="text-zinc-400">Linked Memories</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {entities.map((e) => (
          <TableRow key={e.id} className="border-zinc-800 hover:bg-zinc-800/50">
            <TableCell>
              <span className="font-mono text-sm text-zinc-200">{e.name}</span>
              {e.summary && (
                <p className="mt-0.5 text-xs text-zinc-500">{e.summary}</p>
              )}
            </TableCell>
            <TableCell>
              <Badge variant="outline">{e.type}</Badge>
            </TableCell>
            <TableCell>
              <span className="font-mono text-xs text-zinc-400">
                {e.project ?? "—"}
              </span>
            </TableCell>
            <TableCell>
              <LinkedMemories entityId={e.id} entityName={e.name} />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
