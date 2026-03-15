"use client";

import { useEffect, useRef, useState } from "react";

export interface TocHeading {
  id: string;
  text: string;
  level: 2 | 3;
}

interface TocProps {
  headings: TocHeading[];
}

export default function Toc({ headings }: TocProps) {
  const [activeId, setActiveId] = useState<string>("");
  const observerRef = useRef<IntersectionObserver | null>(null);

  useEffect(() => {
    if (headings.length === 0) return;

    const elements = headings
      .map(({ id }) => document.getElementById(id))
      .filter(Boolean) as HTMLElement[];

    observerRef.current?.disconnect();

    observerRef.current = new IntersectionObserver(
      (entries) => {
        // Find the topmost visible heading
        const visible = entries
          .filter((e) => e.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
        if (visible.length > 0) {
          setActiveId(visible[0].target.id);
        }
      },
      {
        rootMargin: "0px 0px -60% 0px",
        threshold: 0,
      }
    );

    for (const el of elements) {
      observerRef.current.observe(el);
    }

    return () => {
      observerRef.current?.disconnect();
    };
  }, [headings]);

  if (headings.length === 0) return null;

  return (
    <aside className="hidden lg:block w-48 shrink-0">
      <div className="sticky top-20">
        <p className="text-xs font-semibold uppercase tracking-wider text-zinc-500 mb-3 px-2">
          On this page
        </p>
        <ul className="space-y-1">
          {headings.map(({ id, text, level }) => (
            <li key={id}>
              <a
                href={`#${id}`}
                className={`block text-sm py-1 transition-colors ${
                  level === 3 ? "pl-4" : "pl-2"
                } ${
                  activeId === id
                    ? "text-indigo-400"
                    : "text-zinc-500 hover:text-zinc-200"
                }`}
              >
                {text}
              </a>
            </li>
          ))}
        </ul>
      </div>
    </aside>
  );
}
