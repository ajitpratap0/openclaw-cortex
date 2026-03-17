"use client";

import Link from "next/link";
import { Badge } from "@/components/ui/badge";
import {
  Table, TableBody, TableCell,
  TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import type { Memory } from "@/lib/types";

const typeBadgeVariant: Record<
  string,
  "default" | "secondary" | "outline" | "destructive"
> = {
  rule:       "default",
  fact:       "secondary",
  procedure:  "outline",
  episode:    "outline",
  preference: "outline",
};

interface MemoryTableProps {
  memories: Memory[];
  onDelete?: (id: string) => void;
}

export function MemoryTable({ memories, onDelete }: MemoryTableProps) {
  if (memories.length === 0) {
    return <p className="text-sm text-zinc-500">No memories found.</p>;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow className="border-zinc-800 hover:bg-transparent">
          <TableHead className="text-zinc-400">Content</TableHead>
          <TableHead className="w-24 text-zinc-400">Type</TableHead>
          <TableHead className="w-24 text-zinc-400">Scope</TableHead>
          <TableHead className="w-20 text-zinc-400">Confidence</TableHead>
          <TableHead className="w-32 text-zinc-400">Created</TableHead>
          <TableHead className="w-16 text-zinc-400">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {memories.map((m) => (
          <TableRow key={m.id} className="border-zinc-800 hover:bg-zinc-800/50">
            <TableCell className="max-w-md">
              <Link
                href={`/memories/${m.id}`}
                className="block truncate font-mono text-xs text-zinc-200 hover:text-white"
              >
                {m.content}
              </Link>
              {m.conflict_status === "active" && (
                <span className="ml-1 text-xs text-amber-400">conflict</span>
              )}
            </TableCell>
            <TableCell>
              <Badge variant={typeBadgeVariant[m.type] ?? "outline"}>
                {m.type}
              </Badge>
            </TableCell>
            <TableCell>
              <span className="font-mono text-xs text-zinc-400">{m.scope}</span>
            </TableCell>
            <TableCell>
              <span className="font-mono text-xs text-zinc-300">
                {m.confidence.toFixed(2)}
              </span>
            </TableCell>
            <TableCell>
              <span className="font-mono text-xs text-zinc-500">
                {new Date(m.created_at).toLocaleDateString()}
              </span>
            </TableCell>
            <TableCell>
              {onDelete && (
                <button
                  onClick={() => onDelete(m.id)}
                  className="text-xs text-zinc-600 transition-colors hover:text-red-400"
                >
                  delete
                </button>
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
