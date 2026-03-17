import type { AppProps } from "next/app";

// Minimal Pages Router _app — only used for the static /404 and /500 error
// pages generated at build time. Does NOT import any App Router components
// to avoid hook errors in the Pages Router SSR context.
export default function App({ Component, pageProps }: AppProps) {
  return <Component {...pageProps} />;
}
