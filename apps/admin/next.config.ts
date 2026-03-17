import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Standalone output makes it easy to run as a single artifact if needed.
  output: "standalone",
  // All data pages use Client Components + SWR; no SSR needed for this
  // local developer tool.
};

export default nextConfig;
