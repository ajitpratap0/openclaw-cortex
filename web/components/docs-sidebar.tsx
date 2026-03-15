"use client";

import { useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { DocSection } from "@/lib/mdx";

interface DocsSidebarProps {
  sections: DocSection[];
}

export default function DocsSidebar({ sections }: DocsSidebarProps) {
  const pathname = usePathname();
  const [open, setOpen] = useState(false);
  const [collapsedSections, setCollapsedSections] = useState<Set<string>>(
    new Set()
  );

  function toggleSection(section: string) {
    setCollapsedSections((prev) => {
      const next = new Set(prev);
      if (next.has(section)) {
        next.delete(section);
      } else {
        next.add(section);
      }
      return next;
    });
  }

  const nav = (
    <nav className="py-6 px-4">
      {sections.map(({ section, docs }) => {
        const isCollapsed = collapsedSections.has(section);
        return (
          <div key={section} className="mb-6">
            <button
              onClick={() => toggleSection(section)}
              className="w-full flex items-center justify-between text-xs font-semibold uppercase tracking-wider text-zinc-400 hover:text-zinc-200 transition-colors mb-2 px-2"
            >
              <span>{section}</span>
              <svg
                className={`w-3 h-3 transition-transform ${isCollapsed ? "-rotate-90" : ""}`}
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                strokeWidth={2}
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M19 9l-7 7-7-7"
                />
              </svg>
            </button>
            {!isCollapsed && (
              <ul className="space-y-1">
                {docs.map((doc) => {
                  const href = `/docs/${doc.slug}`;
                  const isActive = pathname === href;
                  return (
                    <li key={doc.slug}>
                      <Link
                        href={href}
                        onClick={() => setOpen(false)}
                        className={`block text-sm px-3 py-1.5 rounded-r transition-colors border-l-2 ${
                          isActive
                            ? "border-indigo-500 text-indigo-400 bg-indigo-500/5"
                            : "border-transparent text-zinc-400 hover:text-zinc-100 hover:border-zinc-600"
                        }`}
                      >
                        {doc.frontmatter.title}
                      </Link>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        );
      })}
    </nav>
  );

  return (
    <>
      {/* Mobile toggle */}
      <div className="lg:hidden flex items-center px-4 py-3 border-b border-zinc-800">
        <button
          onClick={() => setOpen((v) => !v)}
          className="flex items-center gap-2 text-sm text-zinc-400 hover:text-zinc-100 transition-colors"
          aria-label="Toggle docs navigation"
        >
          <svg
            className="w-5 h-5"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            strokeWidth={2}
          >
            {open ? (
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M6 18L18 6M6 6l12 12"
              />
            ) : (
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M4 6h16M4 12h16M4 18h16"
              />
            )}
          </svg>
          Navigation
        </button>
      </div>

      {/* Mobile drawer */}
      {open && (
        <div className="lg:hidden border-b border-zinc-800 bg-zinc-950">{nav}</div>
      )}

      {/* Desktop sidebar */}
      <aside className="hidden lg:block w-64 shrink-0">
        <div className="sticky top-16 h-[calc(100vh-4rem)] overflow-y-auto border-r border-zinc-800 bg-zinc-950">
          {nav}
        </div>
      </aside>
    </>
  );
}
