import Link from "next/link";
import Button from "@/components/ui/button";

export default function NotFound() {
  return (
    <div className="flex flex-col items-center justify-center min-h-[60vh] px-4 text-center">
      <p className="text-7xl font-bold text-zinc-800 select-none mb-4">404</p>
      <h1 className="text-2xl font-semibold text-zinc-100 mb-3">
        Page not found
      </h1>
      <p className="text-zinc-400 mb-8 max-w-sm">
        The page you are looking for does not exist or has been moved.
      </p>
      <div className="flex items-center gap-3">
        <Button variant="primary" href="/">
          Go home
        </Button>
        <Button variant="outline" href="/docs/getting-started">
          Read the docs
        </Button>
      </div>
    </div>
  );
}
