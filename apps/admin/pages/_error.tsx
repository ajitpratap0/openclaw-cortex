// Minimal Pages Router error override. Returns null to avoid rendering any
// component that uses React hooks, which fail in the Pages Router SSR context
// when the App Router root layout is loaded.
export default function Error() {
  return null;
}
Error.getInitialProps = () => ({});
