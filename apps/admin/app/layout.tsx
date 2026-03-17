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
      <body className="bg-zinc-950 text-zinc-100 antialiased">
        <SWRProvider>
          <div className="flex h-screen overflow-hidden">
            <Sidebar />
            <main className="flex-1 overflow-auto p-6">{children}</main>
          </div>
        </SWRProvider>
      </body>
    </html>
  );
}
