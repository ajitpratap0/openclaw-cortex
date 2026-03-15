import { redirect, notFound } from "next/navigation";
import { compileMDX } from "next-mdx-remote/rsc";
import { components } from "@/components/mdx-components";
import DocsLayout from "@/components/docs-layout";
import { getAllDocs, getDocBySlug, getDocSections } from "@/lib/mdx";
import type { TocHeading } from "@/components/toc";
import type { Metadata } from "next";

interface PageProps {
  params: Promise<{ slug?: string[] }>;
}

function extractHeadings(content: string): TocHeading[] {
  const headings: TocHeading[] = [];
  const lines = content.split("\n");

  function slugify(text: string): string {
    return text
      .toLowerCase()
      .replace(/[^a-z0-9\s-]/g, "")
      .replace(/\s+/g, "-")
      .replace(/-+/g, "-")
      .trim();
  }

  for (const line of lines) {
    const h2 = line.match(/^##\s+(.+)/);
    const h3 = line.match(/^###\s+(.+)/);
    if (h2) {
      const text = h2[1].trim();
      headings.push({ id: slugify(text), text, level: 2 });
    } else if (h3) {
      const text = h3[1].trim();
      headings.push({ id: slugify(text), text, level: 3 });
    }
  }

  return headings;
}

export async function generateStaticParams() {
  const docs = getAllDocs();
  return docs.map((doc) => ({
    slug: [doc.slug],
  }));
}

export async function generateMetadata({
  params,
}: PageProps): Promise<Metadata> {
  const { slug: slugParts } = await params;
  if (!slugParts || slugParts.length === 0) {
    return { title: "Documentation" };
  }
  const slug = slugParts.join("/");
  try {
    const doc = getDocBySlug(slug);
    return {
      title: doc.frontmatter.title,
      description: doc.frontmatter.description,
    };
  } catch {
    return { title: "Documentation" };
  }
}

export default async function DocsPage({ params }: PageProps) {
  const { slug: slugParts } = await params;

  // Default redirect
  if (!slugParts || slugParts.length === 0) {
    redirect("/docs/getting-started");
  }

  const slug = slugParts.join("/");

  let doc;
  try {
    doc = getDocBySlug(slug);
  } catch {
    notFound();
  }

  const allDocs = getAllDocs();
  const sections = getDocSections();
  const headings = extractHeadings(doc.content);

  const { content } = await compileMDX({
    source: doc.content,
    components,
    options: {
      parseFrontmatter: false,
    },
  });

  return (
    <DocsLayout
      frontmatter={doc.frontmatter}
      slug={slug}
      allDocs={allDocs}
      sections={sections}
      headings={headings}
    >
      {content}
    </DocsLayout>
  );
}
