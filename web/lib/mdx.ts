import fs from "fs";
import path from "path";
import matter from "gray-matter";

const contentDir = path.join(process.cwd(), "content");

export interface DocFrontmatter {
  title: string;
  description?: string;
  order?: number;
  section?: string;
  [key: string]: unknown;
}

export interface BlogFrontmatter {
  title: string;
  description?: string;
  date: string;
  author?: string;
  tags?: string[];
  [key: string]: unknown;
}

export interface DocPage {
  slug: string;
  frontmatter: DocFrontmatter;
  content: string;
}

export interface BlogPost {
  slug: string;
  frontmatter: BlogFrontmatter;
  content: string;
}

export interface DocSection {
  section: string;
  docs: DocPage[];
}

function readMdxFile(filePath: string): { frontmatter: Record<string, unknown>; content: string } {
  const raw = fs.readFileSync(filePath, "utf-8");
  const { data, content } = matter(raw);
  return { frontmatter: data, content };
}

export function getDocBySlug(slug: string): DocPage {
  const filePath = path.join(contentDir, "docs", `${slug}.mdx`);
  const { frontmatter, content } = readMdxFile(filePath);
  return {
    slug,
    frontmatter: frontmatter as DocFrontmatter,
    content,
  };
}

export function getAllDocs(): DocPage[] {
  const docsDir = path.join(contentDir, "docs");

  if (!fs.existsSync(docsDir)) {
    return [];
  }

  const files = fs.readdirSync(docsDir).filter((f) => f.endsWith(".mdx"));

  const docs = files.map((file) => {
    const slug = file.replace(/\.mdx$/, "");
    return getDocBySlug(slug);
  });

  return docs.sort((a, b) => {
    const orderA = a.frontmatter.order ?? 999;
    const orderB = b.frontmatter.order ?? 999;
    return orderA - orderB;
  });
}

export function getDocSections(): DocSection[] {
  const docs = getAllDocs();
  const sectionMap = new Map<string, DocPage[]>();

  for (const doc of docs) {
    const section = doc.frontmatter.section ?? "Other";
    const existing = sectionMap.get(section);
    if (existing) {
      existing.push(doc);
    } else {
      sectionMap.set(section, [doc]);
    }
  }

  return Array.from(sectionMap.entries()).map(([section, pages]) => ({
    section,
    docs: pages,
  }));
}

export function getBlogPost(slug: string): BlogPost {
  const filePath = path.join(contentDir, "blog", `${slug}.mdx`);
  const { frontmatter, content } = readMdxFile(filePath);
  return {
    slug,
    frontmatter: frontmatter as BlogFrontmatter,
    content,
  };
}

export function getAllBlogPosts(): BlogPost[] {
  const blogDir = path.join(contentDir, "blog");

  if (!fs.existsSync(blogDir)) {
    return [];
  }

  const files = fs.readdirSync(blogDir).filter((f) => f.endsWith(".mdx"));

  const posts = files.map((file) => {
    const slug = file.replace(/\.mdx$/, "");
    return getBlogPost(slug);
  });

  return posts.sort((a, b) => {
    const dateA = new Date(a.frontmatter.date).getTime();
    const dateB = new Date(b.frontmatter.date).getTime();
    return dateB - dateA;
  });
}
