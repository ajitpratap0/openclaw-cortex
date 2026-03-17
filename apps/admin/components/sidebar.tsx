"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  LayoutDashboard,
  Brain,
  GitMerge,
  Network,
  Settings,
} from "lucide-react";
import { cn } from "@/lib/utils";

const navItems = [
  { href: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
  { href: "/memories",  label: "Memories",  icon: Brain },
  { href: "/entities",  label: "Entities",  icon: Network },
  { href: "/conflicts", label: "Conflicts", icon: GitMerge },
  { href: "/settings",  label: "Settings",  icon: Settings },
];

export function Sidebar() {
  const pathname = usePathname();

  return (
    <aside className="flex h-screen w-56 flex-col border-r border-zinc-800 bg-zinc-950 px-3 py-4">
      <div className="mb-6 px-2">
        <span className="font-mono text-sm font-semibold text-zinc-100">
          openclaw-cortex
        </span>
        <span className="ml-2 font-mono text-xs text-zinc-500">admin</span>
      </div>
      <nav className="flex flex-1 flex-col gap-1">
        {navItems.map(({ href, label, icon: Icon }) => (
          <Link
            key={href}
            href={href}
            className={cn(
              "flex items-center gap-2.5 rounded-md px-2 py-2 text-sm transition-colors",
              pathname.startsWith(href)
                ? "bg-zinc-800 text-zinc-100"
                : "text-zinc-400 hover:bg-zinc-800/60 hover:text-zinc-100"
            )}
          >
            <Icon className="h-4 w-4 shrink-0" />
            {label}
          </Link>
        ))}
      </nav>
    </aside>
  );
}
