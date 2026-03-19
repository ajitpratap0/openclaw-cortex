"use client";

import { motion } from "framer-motion";
import Image from "next/image";

const containerVariants = {
  hidden: {},
  visible: {
    transition: {
      staggerChildren: 0.12,
    },
  },
};

const itemVariants = {
  hidden: { opacity: 0, y: 20 },
  visible: { opacity: 1, y: 0, transition: { duration: 0.5, ease: "easeOut" } },
};

function DiagramBox({
  children,
  className = "",
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={`rounded-xl border px-5 py-3 text-center font-medium text-sm ${className}`}
    >
      {children}
    </div>
  );
}

function ArrowRight() {
  return (
    <div className="hidden lg:flex items-center gap-0 mx-4">
      <div className="w-14 h-px bg-indigo-500/50" />
      <div
        className="w-0 h-0"
        style={{
          borderTop: "5px solid transparent",
          borderBottom: "5px solid transparent",
          borderLeft: "8px solid rgba(99,102,241,0.5)",
        }}
      />
    </div>
  );
}

function ArrowDown() {
  return (
    <div className="lg:hidden flex flex-col items-center my-1">
      <div className="h-5 w-px bg-indigo-500/50" />
      <div
        className="w-0 h-0"
        style={{
          borderLeft: "5px solid transparent",
          borderRight: "5px solid transparent",
          borderTop: "8px solid rgba(99,102,241,0.5)",
        }}
      />
    </div>
  );
}

export default function ArchitectureDiagram() {
  return (
    <section className="py-24">
      <div className="max-w-6xl mx-auto px-6">
        {/* Section heading */}
        <div className="text-center mb-14">
          <p className="text-sm font-semibold text-indigo-400 uppercase tracking-widest mb-3">
            How It Works
          </p>
          <h2 className="text-3xl sm:text-4xl font-bold text-zinc-50">
            Architecture
          </h2>
          <p className="text-zinc-300 mt-4 max-w-xl mx-auto">
            A clean layered design: your agent talks to the cortex, the cortex
            talks to the infrastructure.
          </p>
        </div>

        {/* Diagram card */}
        <motion.div
          variants={containerVariants}
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true, margin: "-80px" }}
          className="bg-zinc-900 border border-zinc-800 rounded-2xl p-8 lg:p-12"
        >
          <div className="flex flex-col lg:flex-row items-center justify-center">
            {/* Left: Your AI Agent */}
            <motion.div
              variants={itemVariants}
              className="flex flex-col items-center lg:flex-1"
            >
              <DiagramBox className="border-zinc-600 bg-zinc-800 text-zinc-100 w-48">
                <div className="text-base font-semibold">Your AI Agent</div>
                <div className="text-xs text-zinc-400 mt-0.5">
                  Claude · GPT-4 · any LLM
                </div>
              </DiagramBox>
            </motion.div>

            <motion.div variants={itemVariants}>
              <ArrowRight />
              <ArrowDown />
            </motion.div>

            {/* Center: openclaw-cortex */}
            <motion.div variants={itemVariants} className="flex-shrink-0">
              <DiagramBox className="border-indigo-500 bg-indigo-500/10 text-indigo-300 w-52 glow-indigo-sm">
                <div className="flex justify-center mb-1.5">
                  <Image
                    src="/logo/logo-128.png"
                    alt="OpenClaw Cortex"
                    width={32}
                    height={32}
                  />
                </div>
                <div className="text-base font-bold text-indigo-200">
                  openclaw-cortex
                </div>
                <div className="text-xs text-indigo-400 mt-0.5">
                  recall · capture · score
                </div>
              </DiagramBox>
            </motion.div>

            <motion.div variants={itemVariants}>
              <ArrowRight />
              <ArrowDown />
            </motion.div>

            {/* Right: services column */}
            <motion.div
              variants={itemVariants}
              className="flex flex-col gap-3 lg:flex-1"
            >
              <DiagramBox className="border-zinc-700 bg-zinc-800/60 text-zinc-200 w-48">
                <div className="font-semibold">Memgraph</div>
                <div className="text-xs text-zinc-400">vector + graph</div>
              </DiagramBox>
              <DiagramBox className="border-zinc-700 bg-zinc-800/60 text-zinc-200 w-48">
                <div className="font-semibold">Ollama</div>
                <div className="text-xs text-zinc-400">embeddings</div>
              </DiagramBox>
              <DiagramBox className="border-zinc-700 bg-zinc-800/60 text-zinc-200 w-48">
                <div className="font-semibold">Claude / Gateway</div>
                <div className="text-xs text-zinc-400">extraction</div>
              </DiagramBox>
            </motion.div>
          </div>
        </motion.div>

        <p className="text-center text-xs text-zinc-600 mt-6">
          All services are hot-swappable — swap Ollama for any OpenAI-compatible
          embedder, or Claude for any LLM via the gateway.
        </p>
      </div>
    </section>
  );
}
