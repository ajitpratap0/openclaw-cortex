import Link from "next/link";
import DocsSidebar from "@/components/docs-sidebar";
import Toc, { type TocHeading } from "@/components/toc";
import SearchModal from "@/components/search-modal";
import type { DocFrontmatter, DocPage, DocSection } from "@/lib/mdx";

interface DocsLayoutProps {
  children: React.ReactNode;
  frontmatter: DocFrontmatter;
  slug: string;
  allDocs: DocPage[];
  sections: DocSection[];
  headings: TocHeading[];
}

export default function DocsLayout({
  children,
  frontmatter,
  slug,
  allDocs,
  sections,
  headings,
}: DocsLayoutProps) {
  // Build flat ordered list for prev/next navigation
  const flatDocs = sections.flatMap((s) => s.docs);
  const currentIndex = flatDocs.findIndex((d) => d.slug === slug);
  const prevDoc = currentIndex > 0 ? flatDocs[currentIndex - 1] : null;
  const nextDoc =
    currentIndex < flatDocs.length - 1 ? flatDocs[currentIndex + 1] : null;

  // Breadcrumb: Home > Docs > Section > Page
  const section = frontmatter.section;

  const githubEditUrl = `https://github.com/ajitpratap0/openclaw-cortex/edit/main/web/content/docs/${slug}.mdx`;

  return (
    <div className="flex min-h-[calc(100vh-4rem)]">
      {/* Left sidebar */}
      <DocsSidebar sections={sections} />

      {/* Main content */}
      <main className="flex-1 min-w-0 px-6 py-8 lg:px-10 lg:py-10 max-w-3xl mx-auto lg:mx-0">
        {/* Breadcrumbs + Edit link */}
        <div className="flex items-center justify-between mb-6 flex-wrap gap-2">
          <nav
            className="flex items-center gap-1.5 text-sm text-zinc-500"
            aria-label="Breadcrumb"
          >
            <Link href="/" className="hover:text-zinc-300 transition-colors">
              Home
            </Link>
            <span>/</span>
            <Link
              href="/docs/getting-started"
              className="hover:text-zinc-300 transition-colors"
            >
              Docs
            </Link>
            {section && (
              <>
                <span>/</span>
                <span className="text-zinc-400">{section}</span>
              </>
            )}
            <span>/</span>
            <span className="text-zinc-200">{frontmatter.title}</span>
          </nav>

          <a
            href={githubEditUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-1.5 text-xs text-zinc-500 hover:text-zinc-300 transition-colors"
          >
            <svg
              className="w-3.5 h-3.5"
              viewBox="0 0 24 24"
              fill="currentColor"
            >
              <path d="M12 0C5.374 0 0 5.373 0 12c0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23A11.509 11.509 0 0 1 12 5.803c1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576C20.566 21.797 24 17.3 24 12c0-6.627-5.373-12-12-12z" />
            </svg>
            Edit on GitHub
          </a>
        </div>

        {/* Search trigger hint */}
        <div className="mb-8">
          <SearchModal docs={allDocs} />
        </div>

        {/* Page content */}
        <article className="prose prose-invert prose-zinc max-w-none prose-headings:scroll-mt-20 prose-a:no-underline">
          <h1 className="text-3xl font-bold text-zinc-50 mb-2">
            {frontmatter.title}
          </h1>
          {frontmatter.description && (
            <p className="text-lg text-zinc-400 mb-8 mt-0">
              {frontmatter.description}
            </p>
          )}
          {children}
        </article>

        {/* Prev / Next navigation */}
        {(prevDoc || nextDoc) && (
          <nav
            className="mt-12 pt-8 border-t border-zinc-800 flex items-center justify-between gap-4 flex-wrap"
            aria-label="Pagination"
          >
            {prevDoc ? (
              <Link
                href={`/docs/${prevDoc.slug}`}
                className="group flex items-center gap-2 text-sm text-zinc-400 hover:text-zinc-100 transition-colors"
              >
                <svg
                  className="w-4 h-4 group-hover:-translate-x-0.5 transition-transform"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                  strokeWidth={2}
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    d="M15 19l-7-7 7-7"
                  />
                </svg>
                <span>
                  <span className="block text-xs text-zinc-600">Previous</span>
                  {prevDoc.frontmatter.title}
                </span>
              </Link>
            ) : (
              <div />
            )}

            {nextDoc ? (
              <Link
                href={`/docs/${nextDoc.slug}`}
                className="group flex items-center gap-2 text-sm text-zinc-400 hover:text-zinc-100 transition-colors text-right"
              >
                <span>
                  <span className="block text-xs text-zinc-600">Next</span>
                  {nextDoc.frontmatter.title}
                </span>
                <svg
                  className="w-4 h-4 group-hover:translate-x-0.5 transition-transform"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                  strokeWidth={2}
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    d="M9 5l7 7-7 7"
                  />
                </svg>
              </Link>
            ) : (
              <div />
            )}
          </nav>
        )}
      </main>

      {/* Right TOC */}
      <div className="hidden lg:block w-56 shrink-0 py-10 pr-6">
        <Toc headings={headings} />
      </div>
    </div>
  );
}
