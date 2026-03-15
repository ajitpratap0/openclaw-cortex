import type { Metadata } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";
import Nav from "@/components/nav";
import Footer from "@/components/footer";
import "./globals.css";

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-inter",
  display: "swap",
});

const jetbrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  variable: "--font-jetbrains-mono",
  display: "swap",
});

export const metadata: Metadata = {
  metadataBase: new URL("https://openclaw-cortex.vercel.app"),
  title: {
    default: "OpenClaw Cortex",
    template: "%s | OpenClaw Cortex",
  },
  description:
    "Hybrid semantic memory system for AI agents. Store and retrieve memories using multi-factor ranking with Memgraph, vector search, and graph traversal.",
  keywords: [
    "AI memory",
    "semantic memory",
    "AI agents",
    "graph database",
    "vector search",
    "Memgraph",
    "MCP",
    "Claude",
  ],
  authors: [{ name: "Ajit Pratap Singh" }],
  openGraph: {
    type: "website",
    locale: "en_US",
    url: "https://openclaw-cortex.dev",
    siteName: "OpenClaw Cortex",
    title: "OpenClaw Cortex",
    description:
      "Hybrid semantic memory system for AI agents. Store and retrieve memories using multi-factor ranking.",
    images: [
      {
        url: "/og-image.png",
        width: 1200,
        height: 630,
        alt: "OpenClaw Cortex",
      },
    ],
  },
  twitter: {
    card: "summary_large_image",
    title: "OpenClaw Cortex",
    description:
      "Hybrid semantic memory system for AI agents. Store and retrieve memories using multi-factor ranking.",
    images: ["/og-image.png"],
  },
  robots: {
    index: true,
    follow: true,
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      className={`${inter.variable} ${jetbrainsMono.variable} dark`}
    >
      <body className="min-h-screen bg-zinc-950 text-zinc-50 antialiased flex flex-col">
        <Nav />
        <main className="flex-1">{children}</main>
        <Footer />
      </body>
    </html>
  );
}
