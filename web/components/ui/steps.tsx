"use client";

import { Children, type ReactNode } from "react";

interface StepsProps {
  children: ReactNode;
}

export default function Steps({ children }: StepsProps) {
  const steps = Children.toArray(children);

  return (
    <ol className="relative my-6 space-y-0 list-none pl-0 ml-0">
      {steps.map((step, index) => (
        <li key={index} className="relative flex gap-4 pb-8 last:pb-0">
          {/* Vertical connecting line */}
          {index < steps.length - 1 && (
            <div
              className="absolute left-4 top-8 w-px bg-zinc-800"
              style={{ height: "calc(100% - 2rem)" }}
            />
          )}
          {/* Step number circle */}
          <div className="relative z-10 flex-shrink-0 w-8 h-8 rounded-full bg-indigo-500/10 border border-indigo-500/30 flex items-center justify-center">
            <span className="text-xs font-bold text-indigo-400">
              {index + 1}
            </span>
          </div>
          {/* Step content */}
          <div className="flex-1 min-w-0 pt-1 pb-4">{step}</div>
        </li>
      ))}
    </ol>
  );
}
