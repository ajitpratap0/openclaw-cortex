"use client";

import { useState, useEffect } from "react";

interface NavSection {
  id: string;
  label: string;
}

interface FeaturesNavProps {
  sections: NavSection[];
}

export default function FeaturesNav({ sections }: FeaturesNavProps) {
  const [activeId, setActiveId] = useState<string>("");

  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) {
            setActiveId(entry.target.id);
          }
        }
      },
      { rootMargin: "-20% 0px -60% 0px" }
    );

    for (const section of sections) {
      const el = document.getElementById(section.id);
      if (el) observer.observe(el);
    }

    return () => observer.disconnect();
  }, [sections]);

  return (
    <nav className="sticky top-24 space-y-1">
      <p className="text-xs font-semibold text-zinc-500 uppercase tracking-widest mb-3 px-3">
        On this page
      </p>
      {sections.map((section) => (
        <a
          key={section.id}
          href={`#${section.id}`}
          className={`block px-3 py-1.5 rounded-lg text-sm transition-all duration-150 ${
            activeId === section.id
              ? "bg-indigo-500/10 text-indigo-300 font-medium"
              : "text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800/50"
          }`}
        >
          {section.label}
        </a>
      ))}
    </nav>
  );
}
