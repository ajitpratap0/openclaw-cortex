"use client";

import dynamic from "next/dynamic";

const PlaygroundUI = dynamic(
  () => import("@/components/playground-ui"),
  {
    ssr: false,
    loading: () => (
      <div className="flex flex-col items-center justify-center py-20 rounded-xl border border-dashed border-zinc-800 bg-zinc-900/20">
        <div className="w-8 h-8 rounded-full border-2 border-indigo-500 border-t-transparent animate-spin mb-4" />
        <p className="text-zinc-500 text-sm">Loading playground...</p>
      </div>
    ),
  }
);

export default function PlaygroundLoader() {
  return <PlaygroundUI />;
}
