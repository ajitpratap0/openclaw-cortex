import type { MDXComponents } from "mdx/types";
import Link from "next/link";
import CodeBlock from "@/components/ui/code-block";
import Callout from "@/components/ui/callout";
import Steps from "@/components/ui/steps";
import Badge from "@/components/ui/badge";
import Card from "@/components/ui/card";

function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9\s-]/g, "")
    .replace(/\s+/g, "-")
    .replace(/-+/g, "-")
    .trim();
}

function AnchorLink({ id }: { id: string }) {
  return (
    <a
      href={`#${id}`}
      className="ml-2 opacity-0 group-hover:opacity-100 text-zinc-500 hover:text-indigo-400 transition-opacity no-underline"
      aria-label={`Link to ${id}`}
    >
      #
    </a>
  );
}

interface HeadingProps {
  children?: React.ReactNode;
  [key: string]: unknown;
}

function makeHeading(Tag: "h2" | "h3" | "h4", className: string) {
  return function Heading({ children, ...props }: HeadingProps) {
    const text =
      typeof children === "string"
        ? children
        : Array.isArray(children)
          ? children
              .map((c) => (typeof c === "string" ? c : ""))
              .join("")
          : "";
    const id = slugify(text);
    return (
      <Tag id={id} className={`group ${className}`} {...props}>
        {children}
        <AnchorLink id={id} />
      </Tag>
    );
  };
}

interface CodeProps {
  children?: string;
  className?: string;
  [key: string]: unknown;
}

function Code({ children, className, ...props }: CodeProps) {
  const language = className?.replace("language-", "") ?? "text";
  const code = typeof children === "string" ? children.trimEnd() : "";

  // Inline code (no newlines, no explicit language class)
  if (!className) {
    return (
      <code
        className="text-emerald-400 bg-zinc-800 px-1.5 py-0.5 rounded text-sm font-mono"
        {...props}
      >
        {children}
      </code>
    );
  }

  return <CodeBlock code={code} language={language} />;
}

interface PreProps {
  children?: React.ReactNode;
  [key: string]: unknown;
}

// When MDX renders a fenced code block, it wraps <code> in <pre>.
// We intercept <pre> and pass through directly since CodeBlock handles its own wrapper.
function Pre({ children }: PreProps) {
  return <>{children}</>;
}

interface AProps {
  href?: string;
  children?: React.ReactNode;
  [key: string]: unknown;
}

function A({ href, children, ...props }: AProps) {
  const isExternal = href?.startsWith("http");

  if (isExternal) {
    return (
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className="text-indigo-400 hover:text-indigo-300 underline underline-offset-2 transition-colors"
        {...props}
      >
        {children}
      </a>
    );
  }

  return (
    <Link
      href={href ?? "#"}
      className="text-indigo-400 hover:text-indigo-300 underline underline-offset-2 transition-colors"
      {...props}
    >
      {children}
    </Link>
  );
}

interface TableProps {
  children?: React.ReactNode;
}

function Table({ children }: TableProps) {
  return (
    <div className="overflow-x-auto my-6 rounded-lg border border-zinc-800">
      <table className="min-w-full divide-y divide-zinc-800">{children}</table>
    </div>
  );
}

export const components: MDXComponents = {
  h2: makeHeading(
    "h2",
    "text-xl font-semibold text-zinc-100 mt-10 mb-4 scroll-mt-20"
  ),
  h3: makeHeading(
    "h3",
    "text-lg font-semibold text-zinc-100 mt-8 mb-3 scroll-mt-20"
  ),
  h4: makeHeading(
    "h4",
    "text-base font-semibold text-zinc-200 mt-6 mb-2 scroll-mt-20"
  ),
  code: Code,
  pre: Pre,
  a: A,
  table: Table,
  // Expose custom components for use in MDX files
  Callout,
  Steps,
  Badge,
  Card,
  CodeBlock,
};
