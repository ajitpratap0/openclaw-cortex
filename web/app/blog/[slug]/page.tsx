import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { compileMDX } from "next-mdx-remote/rsc";
import { getAllBlogPosts, getBlogPost } from "@/lib/mdx";
import { components } from "@/components/mdx-components";
import Badge from "@/components/ui/badge";

interface PageProps {
  params: Promise<{ slug: string }>;
}

const tagVariantMap: Record<string, "indigo" | "emerald" | "amber" | "default"> = {
  release: "emerald",
  graph: "indigo",
  entities: "indigo",
  memgraph: "amber",
  architecture: "amber",
  temporal: "indigo",
  recall: "emerald",
};

function tagVariant(tag: string): "indigo" | "emerald" | "amber" | "default" {
  return tagVariantMap[tag] ?? "default";
}

function formatDate(dateStr: string): string {
  const date = new Date(dateStr);
  return date.toLocaleDateString("en-US", {
    year: "numeric",
    month: "long",
    day: "numeric",
  });
}

export async function generateStaticParams() {
  const posts = getAllBlogPosts();
  return posts.map((post) => ({ slug: post.slug }));
}

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { slug } = await params;
  try {
    const post = getBlogPost(slug);
    const excerpt = String(post.frontmatter["excerpt"] ?? post.frontmatter.description ?? "");
    return {
      title: post.frontmatter.title,
      description: excerpt,
    };
  } catch {
    return { title: "Post Not Found" };
  }
}

export default async function BlogPostPage({ params }: PageProps) {
  const { slug } = await params;

  let post;
  try {
    post = getBlogPost(slug);
  } catch {
    notFound();
  }

  const { content } = await compileMDX({
    source: post.content,
    components,
    options: { parseFrontmatter: false },
  });

  const postUrl = `https://openclaw-cortex.dev/blog/${slug}`;
  const twitterShareUrl = `https://twitter.com/intent/tweet?text=${encodeURIComponent(post.frontmatter.title)}&url=${encodeURIComponent(postUrl)}`;
  const linkedInShareUrl = `https://www.linkedin.com/sharing/share-offsite/?url=${encodeURIComponent(postUrl)}`;

  const author = String(post.frontmatter["author"] ?? "Ajit Pratap Singh");
  const readingTime = String(post.frontmatter["readingTime"] ?? "");

  return (
    <div className="max-w-3xl mx-auto px-4 sm:px-6 lg:px-8 py-16">
      {/* Back link */}
      <Link
        href="/blog"
        className="inline-flex items-center gap-1.5 text-sm text-zinc-500 hover:text-zinc-300 transition-colors mb-10"
      >
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
        </svg>
        Back to Blog
      </Link>

      {/* Header */}
      <header className="mb-10">
        {/* Tags */}
        {post.frontmatter.tags && post.frontmatter.tags.length > 0 && (
          <div className="flex flex-wrap gap-1.5 mb-4">
            {post.frontmatter.tags.map((tag) => (
              <Badge key={tag} variant={tagVariant(tag)}>
                {tag}
              </Badge>
            ))}
          </div>
        )}

        <h1 className="text-3xl sm:text-4xl font-bold text-zinc-50 leading-tight mb-4">
          {post.frontmatter.title}
        </h1>

        {/* Meta row */}
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-zinc-500">
          <span>{formatDate(post.frontmatter.date)}</span>
          <span className="text-zinc-700">·</span>
          <span>{author}</span>
          {readingTime && (
            <>
              <span className="text-zinc-700">·</span>
              <span>{readingTime} read</span>
            </>
          )}
        </div>
      </header>

      {/* MDX content */}
      <article className="prose prose-zinc prose-invert max-w-none prose-headings:font-semibold prose-a:text-indigo-400 prose-code:text-emerald-400 prose-pre:bg-zinc-900 prose-blockquote:border-indigo-500 prose-li:text-zinc-300 prose-p:text-zinc-300">
        {content}
      </article>

      {/* Share links */}
      <footer className="mt-16 pt-8 border-t border-zinc-800">
        <p className="text-sm text-zinc-500 mb-4">Found this useful? Share it:</p>
        <div className="flex items-center gap-3">
          <a
            href={twitterShareUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-zinc-900 border border-zinc-700 text-sm text-zinc-300 hover:text-zinc-50 hover:border-zinc-500 transition-colors"
          >
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
              <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z" />
            </svg>
            Share on X
          </a>
          <a
            href={linkedInShareUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-zinc-900 border border-zinc-700 text-sm text-zinc-300 hover:text-zinc-50 hover:border-zinc-500 transition-colors"
          >
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
              <path d="M20.447 20.452h-3.554v-5.569c0-1.328-.027-3.037-1.852-3.037-1.853 0-2.136 1.445-2.136 2.939v5.667H9.351V9h3.414v1.561h.046c.477-.9 1.637-1.85 3.37-1.85 3.601 0 4.267 2.37 4.267 5.455v6.286zM5.337 7.433a2.062 2.062 0 01-2.063-2.065 2.064 2.064 0 112.063 2.065zm1.782 13.019H3.555V9h3.564v11.452zM22.225 0H1.771C.792 0 0 .774 0 1.729v20.542C0 23.227.792 24 1.771 24h20.451C23.2 24 24 23.227 24 22.271V1.729C24 .774 23.2 0 22.222 0h.003z" />
            </svg>
            Share on LinkedIn
          </a>
        </div>

        <div className="mt-8">
          <Link
            href="/blog"
            className="inline-flex items-center gap-1.5 text-sm text-zinc-500 hover:text-zinc-300 transition-colors"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
            </svg>
            All posts
          </Link>
        </div>
      </footer>
    </div>
  );
}
