import type { Metadata } from "next";
import Link from "next/link";
import { getAllBlogPosts } from "@/lib/mdx";
import Card from "@/components/ui/card";
import Badge from "@/components/ui/badge";

export const metadata: Metadata = {
  title: "Blog — OpenClaw Cortex",
  description: "Release notes and technical deep dives from the OpenClaw Cortex team.",
};

function formatDate(dateStr: string): string {
  const date = new Date(dateStr);
  return date.toLocaleDateString("en-US", {
    year: "numeric",
    month: "long",
    day: "numeric",
  });
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

export default function BlogPage() {
  const posts = getAllBlogPosts();

  return (
    <div className="max-w-5xl mx-auto px-4 sm:px-6 lg:px-8 py-16">
      {/* Header */}
      <div className="mb-12">
        <h1 className="text-4xl font-bold text-zinc-50 mb-3">Blog</h1>
        <p className="text-zinc-300 text-lg">Release notes and technical deep dives</p>
      </div>

      {/* Post grid */}
      {posts.length === 0 ? (
        <p className="text-zinc-500">No posts yet.</p>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          {posts.map((post) => {
            const fm = post.frontmatter;
            const title = String(fm.title ?? "");
            const tags = Array.isArray(fm.tags) ? (fm.tags as string[]) : [];
            const excerpt = fm.description != null
              ? String(fm.description)
              : fm["excerpt"] != null
              ? String(fm["excerpt"])
              : null;
            return (
              <Link key={post.slug} href={`/blog/${post.slug}`} className="block group">
                <Card hover className="h-full flex flex-col">
                  {tags.length > 0 && (
                    <div className="flex flex-wrap gap-1.5 mb-3">
                      {tags.map((tag) => (
                        <Badge key={tag} variant={tagVariant(tag)}>
                          {tag}
                        </Badge>
                      ))}
                    </div>
                  )}

                  <h2 className="text-lg font-semibold text-zinc-100 group-hover:text-indigo-400 transition-colors mb-2 leading-snug">
                    {title}
                  </h2>

                  {excerpt && (
                    <p className="text-sm text-zinc-300 leading-relaxed mb-4 flex-1">
                      {excerpt}
                    </p>
                  )}

                  <p className="text-xs text-zinc-500 mt-auto">
                    {formatDate(fm.date)}
                  </p>
                </Card>
              </Link>
            );
          })}
        </div>
      )}
    </div>
  );
}
