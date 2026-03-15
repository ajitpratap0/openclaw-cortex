"use client";

import { useState } from "react";

interface CodeBlockProps {
  code: string;
  language?: string;
  filename?: string;
  showLineNumbers?: boolean;
}

export default function CodeBlock({
  code,
  language = "text",
  filename,
  showLineNumbers = false,
}: CodeBlockProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard API not available
    }
  };

  const lines = code.split("\n");

  return (
    <div className="relative group rounded-xl overflow-hidden border border-zinc-800 bg-zinc-900 my-4">
      {filename && (
        <div className="flex items-center gap-2 px-4 py-2 border-b border-zinc-800 bg-zinc-950/50">
          <div className="flex gap-1.5">
            <span className="w-3 h-3 rounded-full bg-red-500/70" />
            <span className="w-3 h-3 rounded-full bg-yellow-500/70" />
            <span className="w-3 h-3 rounded-full bg-green-500/70" />
          </div>
          <span className="text-xs text-zinc-400 font-mono ml-1">{filename}</span>
        </div>
      )}
      {!filename && language && language !== "text" && (
        <div className="flex items-center justify-end px-4 py-1.5 border-b border-zinc-800 bg-zinc-950/30">
          <span className="text-xs text-zinc-500 font-mono uppercase tracking-wider">
            {language}
          </span>
        </div>
      )}
      <div className="relative overflow-x-auto">
        <pre className="p-4 text-sm font-mono leading-relaxed overflow-x-auto">
          {showLineNumbers ? (
            <code className="block">
              {lines.map((line, i) => (
                <span key={i} className="table-row">
                  <span className="table-cell pr-4 text-zinc-600 select-none text-right w-8">
                    {i + 1}
                  </span>
                  <span className="table-cell text-zinc-300">{line}</span>
                </span>
              ))}
            </code>
          ) : (
            <code className="text-zinc-300">{code}</code>
          )}
        </pre>
        <button
          onClick={handleCopy}
          className="absolute top-3 right-3 opacity-0 group-hover:opacity-100 transition-opacity duration-150 bg-zinc-800 hover:bg-zinc-700 border border-zinc-700 hover:border-zinc-600 rounded-md px-2.5 py-1.5 text-xs text-zinc-400 hover:text-zinc-200 font-medium"
          aria-label="Copy code"
        >
          {copied ? "Copied!" : "Copy"}
        </button>
      </div>
    </div>
  );
}
