import type { Metadata } from "next";
import { GeistSans } from "geist/font/sans";
import { GeistMono } from "geist/font/mono";
import { Sidebar } from "@/components/sidebar";
import { SWRProvider } from "@/components/swr-provider";
import "./globals.css";

export const metadata: Metadata = {
  title: "openclaw-cortex admin",
  description: "Admin interface for the openclaw-cortex memory system",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html
      lang="en"
      className={`dark ${GeistSans.variable} ${GeistMono.variable}`}
    >
      <body className="bg-[--color-surface] text-zinc-100 antialiased">
        <a
          href="#main-content"
          className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:px-4 focus:py-2 focus:bg-white focus:text-black focus:rounded"
        >
          Skip to content
        </a>
        <SWRProvider>
          <div className="flex h-screen overflow-hidden">
            <Sidebar />
            <main id="main-content" className="flex-1 overflow-auto p-6">{children}</main>
          </div>
        </SWRProvider>
      </body>
    </html>
  );
}
