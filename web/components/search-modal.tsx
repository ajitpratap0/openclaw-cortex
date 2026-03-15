"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import type { DocPage } from "@/lib/mdx";

interface SearchModalProps {
  docs: DocPage[];
}

interface SearchResult {
  slug: string;
  title: string;
  section: string;
  description?: string;
}

export default function SearchModal({ docs }: SearchModalProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResult[]>([]);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const router = useRouter();

  // Open on Cmd+K / Ctrl+K
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        setOpen((v) => !v);
      }
      if (e.key === "Escape") {
        setOpen(false);
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  // Focus input when modal opens
  useEffect(() => {
    if (open) {
      setTimeout(() => inputRef.current?.focus(), 50);
      setQuery("");
      setResults([]);
      setSelectedIndex(0);
    }
  }, [open]);

  // Search
  useEffect(() => {
    if (!query.trim()) {
      setResults([]);
      setSelectedIndex(0);
      return;
    }
    const q = query.toLowerCase();
    const matched = docs
      .filter(
        (doc) =>
          doc.frontmatter.title?.toLowerCase().includes(q) ||
          doc.frontmatter.description?.toLowerCase().includes(q) ||
          doc.frontmatter.section?.toLowerCase().includes(q)
      )
      .slice(0, 8)
      .map((doc) => ({
        slug: doc.slug,
        title: doc.frontmatter.title,
        section: doc.frontmatter.section ?? "",
        description: doc.frontmatter.description,
      }));
    setResults(matched);
    setSelectedIndex(0);
  }, [query, docs]);

  function navigate(slug: string) {
    setOpen(false);
    router.push(`/docs/${slug}`);
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelectedIndex((i) => Math.min(i + 1, results.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelectedIndex((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter" && results[selectedIndex]) {
      navigate(results[selectedIndex].slug);
    }
  }

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-[15vh]"
      role="dialog"
      aria-modal="true"
      aria-label="Search documentation"
    >
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-zinc-950/80 backdrop-blur-sm"
        onClick={() => setOpen(false)}
      />

      {/* Modal */}
      <div className="relative z-10 w-full max-w-xl mx-4 bg-zinc-900 border border-zinc-700 rounded-xl shadow-2xl overflow-hidden">
        {/* Search input */}
        <div className="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
          <svg
            className="w-5 h-5 text-zinc-500 shrink-0"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            strokeWidth={2}
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M21 21l-4.35-4.35M17 11A6 6 0 1 1 5 11a6 6 0 0 1 12 0z"
            />
          </svg>
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={onKeyDown}
            placeholder="Search documentation..."
            className="flex-1 bg-transparent text-zinc-100 placeholder-zinc-500 text-sm outline-none"
          />
          <kbd className="text-xs text-zinc-600 border border-zinc-700 rounded px-1.5 py-0.5">
            Esc
          </kbd>
        </div>

        {/* Results */}
        {results.length > 0 ? (
          <ul className="max-h-80 overflow-y-auto py-2">
            {results.map((result, index) => (
              <li key={result.slug}>
                <button
                  onClick={() => navigate(result.slug)}
                  onMouseEnter={() => setSelectedIndex(index)}
                  className={`w-full text-left px-4 py-3 transition-colors ${
                    selectedIndex === index
                      ? "bg-zinc-800"
                      : "hover:bg-zinc-800/50"
                  }`}
                >
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-indigo-400 bg-indigo-500/10 px-1.5 py-0.5 rounded shrink-0">
                      {result.section}
                    </span>
                    <span className="text-sm text-zinc-100 font-medium truncate">
                      {result.title}
                    </span>
                  </div>
                  {result.description && (
                    <p className="text-xs text-zinc-500 mt-0.5 truncate pl-0">
                      {result.description}
                    </p>
                  )}
                </button>
              </li>
            ))}
          </ul>
        ) : query ? (
          <div className="px-4 py-8 text-center text-sm text-zinc-500">
            No results for &ldquo;{query}&rdquo;
          </div>
        ) : (
          <div className="px-4 py-8 text-center text-sm text-zinc-500">
            Type to search documentation
          </div>
        )}

        {/* Footer hint */}
        <div className="border-t border-zinc-800 px-4 py-2 flex items-center gap-4 text-xs text-zinc-600">
          <span>
            <kbd className="border border-zinc-700 rounded px-1 py-0.5">↑</kbd>{" "}
            <kbd className="border border-zinc-700 rounded px-1 py-0.5">↓</kbd>{" "}
            navigate
          </span>
          <span>
            <kbd className="border border-zinc-700 rounded px-1 py-0.5">
              ↵
            </kbd>{" "}
            select
          </span>
        </div>
      </div>
    </div>
  );
}
