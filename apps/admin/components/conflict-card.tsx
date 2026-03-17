"use client";

import { Badge }   from "@/components/ui/badge";
import { Button }  from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import type { Memory } from "@/lib/types";

interface ConflictCardProps {
  memory:    Memory;
  onResolve: (id: string) => void;
  onDismiss: (id: string) => void;
}

export function ConflictCard({ memory, onResolve, onDismiss }: ConflictCardProps) {
  return (
    <Card className="border-amber-800/40 bg-zinc-900">
      <CardHeader className="flex flex-row items-start justify-between pb-2">
        <div className="space-y-0.5">
          <Badge variant="destructive">active conflict</Badge>
          {memory.conflict_group_id && (
            <p className="font-mono text-xs text-zinc-600">
              group: {memory.conflict_group_id}
            </p>
          )}
        </div>
        <span className="font-mono text-xs text-zinc-600">
          {new Date(memory.created_at).toLocaleString()}
        </span>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="whitespace-pre-wrap font-mono text-sm text-zinc-200">
          {memory.content}
        </p>
        <div className="flex flex-wrap gap-2 text-xs text-zinc-400">
          <span>type: {memory.type}</span>
          <span>scope: {memory.scope}</span>
          <span>confidence: {memory.confidence.toFixed(2)}</span>
          {memory.project && <span>project: {memory.project}</span>}
        </div>
        <div className="flex items-center gap-2 pt-1">
          <Button
            size="sm"
            variant="outline"
            onClick={() => onResolve(memory.id)}
          >
            Mark Resolved
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => onDismiss(memory.id)}
          >
            Dismiss
          </Button>
          <a
            href={`/memories/${memory.id}`}
            className="ml-auto text-xs text-zinc-500 hover:text-zinc-300"
          >
            view detail
          </a>
        </div>
      </CardContent>
    </Card>
  );
}
