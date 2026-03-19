import { redirect } from "next/navigation";
import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "OpenClaw Admin",
  description: "Developer dashboard for managing memories, entities, and conflicts",
};

export default function RootPage() {
  redirect("/dashboard");
}
