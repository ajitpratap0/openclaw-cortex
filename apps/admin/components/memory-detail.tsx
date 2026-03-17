"use client";

import { useState } from "react";
import { cortex } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input }  from "@/components/ui/input";
import { Badge }  from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { Memory } from "@/lib/types";

interface MemoryDetailProps {
  memory:   Memory;
  onUpdate: () => void;
  onDelete: () => void;
}

export function MemoryDetail({ memory, onUpdate, onDelete }: MemoryDetailProps) {
  const [confidence, setConfidence] = useState(String(memory.confidence));
  const [tags,       setTags]       = useState((memory.tags ?? []).join(", "));
  const [saving,     setSaving]     = useState(false);
  const [error,      setError]      = useState("");

  async function handleSave() {
    setSaving(true);
    setError("");
    try {
      const parsedConf = parseFloat(confidence);
      if (isNaN(parsedConf) || parsedConf < 0 || parsedConf > 1) {
        throw new Error("Confidence must be between 0.0 and 1.0");
      }
      await cortex.updateMemory(memory.id, {
        confidence: parsedConf,
        tags: tags
          .split(",")
          .map((t) => t.trim())
          .filter(Boolean),
      });
      onUpdate();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }

  async function handleDelete() {
    if (!confirm("Permanently delete this memory?")) return;
    try {
      await cortex.deleteMemory(memory.id);
      onDelete();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  const metaRows: [string, string][] = [
    ["ID",            memory.id],
    ["Type",          memory.type],
    ["Scope",         memory.scope],
    ["Source",        memory.source],
    ["Project",       memory.project      ?? "—"],
    ["User",          memory.user_id      ?? "—"],
    ["Created",       new Date(memory.created_at).toLocaleString()],
    ["Last accessed", new Date(memory.last_accessed).toLocaleString()],
    ["Access count",  String(memory.access_count)],
    ["Reinforced",    String(memory.reinforced_count ?? 0) + "x"],
  ];

  return (
    <div className="space-y-4">
      <Card className="border-zinc-800 bg-zinc-900">
        <CardHeader>
          <CardTitle className="text-sm text-zinc-300">Content</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="whitespace-pre-wrap font-mono text-sm text-zinc-200">
            {memory.content}
          </p>
        </CardContent>
      </Card>

      <div className="grid gap-4 sm:grid-cols-2">
        <Card className="border-zinc-800 bg-zinc-900">
          <CardHeader>
            <CardTitle className="text-sm text-zinc-300">Metadata</CardTitle>
          </CardHeader>
          <CardContent className="space-y-1.5 text-xs">
            {metaRows.map(([label, value]) => (
              <div key={label} className="flex justify-between">
                <span className="text-zinc-500">{label}</span>
                <span className="font-mono text-zinc-300">{value}</span>
              </div>
            ))}
            {memory.conflict_status === "active" && (
              <div className="pt-1">
                <Badge variant="destructive">active conflict</Badge>
              </div>
            )}
          </CardContent>
        </Card>

        <Card className="border-zinc-800 bg-zinc-900">
          <CardHeader>
            <CardTitle className="text-sm text-zinc-300">Edit</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="space-y-1">
              <label htmlFor="confidence" className="text-xs text-zinc-400">
                Confidence (0.0 – 1.0)
              </label>
              <Input
                id="confidence"
                value={confidence}
                onChange={(e) => setConfidence(e.target.value)}
                className="font-mono text-sm"
              />
            </div>
            <div className="space-y-1">
              <label htmlFor="tags" className="text-xs text-zinc-400">
                Tags (comma-separated)
              </label>
              <Input
                id="tags"
                value={tags}
                onChange={(e) => setTags(e.target.value)}
                className="font-mono text-sm"
                placeholder="tag1, tag2"
              />
            </div>
            {error && <p className="text-xs text-red-400">{error}</p>}
            <div className="flex gap-2">
              <Button size="sm" onClick={handleSave} disabled={saving}>
                {saving ? "Saving..." : "Save"}
              </Button>
              <Button size="sm" variant="destructive" onClick={handleDelete}>
                Delete
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
