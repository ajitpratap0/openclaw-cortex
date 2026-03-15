"use client";

import { useState } from "react";
import Link from "next/link";
import Image from "next/image";
import Button from "@/components/ui/button";

const navLinks = [
  { label: "Docs", href: "/docs/getting-started" },
  { label: "Features", href: "/#features" },
  { label: "Blog", href: "/blog" },
  { label: "Playground", href: "/playground" },
  { label: "Compare", href: "/compare" },
];

export default function Nav() {
  const [mobileOpen, setMobileOpen] = useState(false);

  return (
    <header className="sticky top-0 z-50 w-full bg-zinc-950/80 backdrop-blur-lg border-b border-zinc-800">
      <nav className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex items-center justify-between h-16">
          {/* Logo */}
          <Link href="/" className="flex items-center gap-3 group">
            <div className="w-7 h-7 rounded-md bg-gradient-to-br from-indigo-500 to-emerald-500 flex-shrink-0 group-hover:opacity-90 transition-opacity" />
            <span className="font-semibold text-zinc-100 text-sm tracking-tight">
              openclaw-cortex
            </span>
          </Link>

          {/* Desktop links */}
          <div className="hidden md:flex items-center gap-1">
            {navLinks.map((link) => (
              <Link
                key={link.href}
                href={link.href}
                className="px-3 py-2 text-sm text-zinc-400 hover:text-zinc-50 rounded-md transition-colors duration-150"
              >
                {link.label}
              </Link>
            ))}
          </div>

          {/* Desktop right side */}
          <div className="hidden md:flex items-center gap-3">
            <Link
              href="https://github.com/ajitpratap0/openclaw-cortex"
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center"
              aria-label="GitHub Stars"
            >
              <Image
                src="https://img.shields.io/github/stars/ajitpratap0/openclaw-cortex?style=social"
                alt="GitHub stars"
                width={100}
                height={20}
                unoptimized
              />
            </Link>
            <Button
              variant="primary"
              size="sm"
              href="/docs/getting-started"
            >
              Get Started
            </Button>
          </div>

          {/* Mobile hamburger */}
          <button
            className="md:hidden inline-flex items-center justify-center w-9 h-9 rounded-md text-zinc-400 hover:text-zinc-50 hover:bg-zinc-800 transition-colors"
            onClick={() => setMobileOpen(!mobileOpen)}
            aria-label="Toggle menu"
            aria-expanded={mobileOpen}
          >
            {mobileOpen ? (
              <svg
                className="w-5 h-5"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M6 18L18 6M6 6l12 12"
                />
              </svg>
            ) : (
              <svg
                className="w-5 h-5"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M4 6h16M4 12h16M4 18h16"
                />
              </svg>
            )}
          </button>
        </div>

        {/* Mobile menu */}
        {mobileOpen && (
          <div className="md:hidden py-4 border-t border-zinc-800">
            <div className="flex flex-col gap-1">
              {navLinks.map((link) => (
                <Link
                  key={link.href}
                  href={link.href}
                  className="px-3 py-2.5 text-sm text-zinc-400 hover:text-zinc-50 hover:bg-zinc-800/50 rounded-md transition-colors"
                  onClick={() => setMobileOpen(false)}
                >
                  {link.label}
                </Link>
              ))}
              <div className="mt-3 pt-3 border-t border-zinc-800 flex flex-col gap-2">
                <Link
                  href="https://github.com/ajitpratap0/openclaw-cortex"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="px-3 py-2.5 text-sm text-zinc-400 hover:text-zinc-50 hover:bg-zinc-800/50 rounded-md transition-colors"
                  onClick={() => setMobileOpen(false)}
                >
                  GitHub
                </Link>
                <Button
                  variant="primary"
                  size="md"
                  href="/docs/getting-started"
                  className="mx-3"
                >
                  Get Started
                </Button>
              </div>
            </div>
          </div>
        )}
      </nav>
    </header>
  );
}
