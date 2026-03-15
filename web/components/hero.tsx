"use client";

import { motion } from "framer-motion";
import Image from "next/image";
import Button from "@/components/ui/button";

const terminalLines = [
  {
    type: "command",
    text: '$ openclaw-cortex capture --user "We use Memgraph for vectors and graph" --assistant "Good choice for unified storage"',
  },
  {
    type: "output",
    text: "Captured [fact]: Memgraph chosen for unified vector + graph storage",
  },
  { type: "blank", text: "" },
  { type: "command", text: '$ openclaw-cortex recall "database decision"' },
  {
    type: "result",
    text: "[1] (92%) [fact] Memgraph chosen for unified vector + graph storage",
  },
  {
    type: "result",
    text: "[2] (67%) [rule] Always validate schema on startup",
  },
];

export default function Hero() {
  return (
    <section className="relative flex flex-col items-center justify-center min-h-[92vh] px-4 text-center overflow-hidden">
      {/* Background grid */}
      <div className="absolute inset-0 bg-grid opacity-40 pointer-events-none" />

      {/* Radial indigo glow */}
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[800px] h-[600px] gradient-radial-indigo pointer-events-none blur-3xl opacity-60" />

      <div className="relative z-10 w-full max-w-4xl mx-auto">
        {/* Headline */}
        <motion.div
          initial={{ opacity: 0, y: 32 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6, ease: "easeOut" }}
        >
          {/* Logo */}
          <div className="flex justify-center mb-8">
            <Image
              src="/logo/logo-256.png"
              alt="OpenClaw Cortex"
              width={96}
              height={96}
              priority
              className="drop-shadow-[0_0_24px_rgba(45,212,191,0.4)]"
            />
          </div>

          <h1 className="text-5xl sm:text-6xl md:text-7xl font-bold text-zinc-50 leading-tight mb-6 tracking-tight">
            Memory that
            <br />
            <span className="bg-gradient-to-r from-indigo-400 to-indigo-600 bg-clip-text text-transparent">
              understands context
            </span>
          </h1>

          <p className="text-lg sm:text-xl text-zinc-400 max-w-2xl mx-auto mb-8 leading-relaxed">
            Graph-aware semantic memory for AI agents. One database. Zero token
            waste.
          </p>

          <div className="flex items-center justify-center gap-4 flex-wrap mb-14">
            <Button variant="primary" size="lg" href="/docs/getting-started">
              Quick Start
            </Button>
            <Button
              variant="ghost"
              size="lg"
              href="https://github.com/ajitpratap0/openclaw-cortex"
              target="_blank"
              rel="noopener noreferrer"
            >
              View on GitHub
            </Button>
          </div>
        </motion.div>

        {/* Terminal mockup */}
        <motion.div
          initial={{ opacity: 0, y: 40 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.7, ease: "easeOut", delay: 0.25 }}
          className="w-full max-w-3xl mx-auto"
        >
          <div className="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden shadow-2xl">
            {/* Terminal title bar */}
            <div className="flex items-center gap-2 px-4 py-3 border-b border-zinc-800 bg-zinc-950/60">
              <span className="w-3 h-3 rounded-full bg-red-500/70" />
              <span className="w-3 h-3 rounded-full bg-yellow-500/70" />
              <span className="w-3 h-3 rounded-full bg-green-500/70" />
              <span className="ml-2 text-xs text-zinc-500 font-mono">
                openclaw-cortex — terminal
              </span>
            </div>

            {/* Terminal content */}
            <div className="p-5 text-left">
              <pre className="font-mono text-sm leading-relaxed overflow-x-auto">
                {terminalLines.map((line, i) => {
                  if (line.type === "blank") {
                    return <span key={i} className="block h-4" />;
                  }
                  if (line.type === "command") {
                    return (
                      <span key={i} className="block">
                        <span className="text-indigo-400">{line.text}</span>
                      </span>
                    );
                  }
                  if (line.type === "output") {
                    return (
                      <span key={i} className="block mt-1">
                        <span className="text-emerald-400">{line.text}</span>
                      </span>
                    );
                  }
                  if (line.type === "result") {
                    return (
                      <span key={i} className="block mt-0.5">
                        <span className="text-zinc-300">{line.text}</span>
                      </span>
                    );
                  }
                  return null;
                })}
              </pre>
            </div>
          </div>
        </motion.div>
      </div>
    </section>
  );
}
