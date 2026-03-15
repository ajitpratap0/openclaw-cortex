import type { Config } from "tailwindcss";
import typography from "@tailwindcss/typography";

const config: Config = {
  content: [
    "./pages/**/*.{js,ts,jsx,tsx,mdx}",
    "./components/**/*.{js,ts,jsx,tsx,mdx}",
    "./app/**/*.{js,ts,jsx,tsx,mdx}",
    "./content/**/*.{md,mdx}",
  ],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        background: "#09090b",
        surface: "#18181b",
        primary: "#6366f1",
        secondary: "#10b981",
        "zinc-950": "#09090b",
        "zinc-900": "#18181b",
        "indigo-500": "#6366f1",
        "emerald-500": "#10b981",
      },
      fontFamily: {
        sans: ["var(--font-inter)", "Inter", "system-ui", "sans-serif"],
        mono: [
          "var(--font-jetbrains-mono)",
          "JetBrains Mono",
          "Fira Code",
          "monospace",
        ],
      },
      typography: {
        invert: {
          css: {
            "--tw-prose-body": "rgb(228 228 231)",
            "--tw-prose-headings": "rgb(250 250 250)",
            "--tw-prose-links": "rgb(99 102 241)",
            "--tw-prose-code": "rgb(167 243 208)",
            "--tw-prose-pre-bg": "rgb(24 24 27)",
            "--tw-prose-pre-code": "rgb(228 228 231)",
          },
        },
        DEFAULT: {
          css: {
            maxWidth: "none",
            color: "rgb(228 228 231)",
            a: {
              color: "rgb(99 102 241)",
              "&:hover": {
                color: "rgb(129 140 248)",
              },
            },
            "h1, h2, h3, h4": {
              color: "rgb(250 250 250)",
              fontWeight: "600",
            },
            code: {
              color: "rgb(167 243 208)",
              backgroundColor: "rgb(39 39 42)",
              padding: "0.125rem 0.375rem",
              borderRadius: "0.25rem",
              fontWeight: "400",
              "&::before": { content: '""' },
              "&::after": { content: '""' },
            },
            pre: {
              backgroundColor: "rgb(24 24 27)",
              border: "1px solid rgb(63 63 70)",
            },
            blockquote: {
              borderLeftColor: "rgb(99 102 241)",
              color: "rgb(161 161 170)",
            },
            hr: {
              borderColor: "rgb(63 63 70)",
            },
            "thead th": {
              color: "rgb(250 250 250)",
            },
            "tbody tr": {
              borderBottomColor: "rgb(63 63 70)",
            },
          },
        },
      },
    },
  },
  plugins: [typography],
};

export default config;
