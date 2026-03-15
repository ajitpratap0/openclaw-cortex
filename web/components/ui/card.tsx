import { type ReactNode } from "react";

interface CardProps {
  children: ReactNode;
  className?: string;
  hover?: boolean;
}

export default function Card({ children, className = "", hover = false }: CardProps) {
  const hoverClasses = hover
    ? "hover:border-indigo-500/50 hover:glow-indigo-sm cursor-pointer"
    : "";

  return (
    <div
      className={`bg-zinc-900 border border-zinc-800 rounded-xl p-6 transition-all duration-200 ${hoverClasses} ${className}`}
    >
      {children}
    </div>
  );
}
