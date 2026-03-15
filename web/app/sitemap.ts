import { MetadataRoute } from "next";
import { getAllDocs, getAllBlogPosts } from "@/lib/mdx";

const BASE_URL = "https://openclaw-cortex.vercel.app"; // update when custom domain set

export default function sitemap(): MetadataRoute.Sitemap {
  const now = new Date();

  const staticPages: MetadataRoute.Sitemap = [
    { url: BASE_URL, lastModified: now, changeFrequency: "weekly", priority: 1.0 },
    { url: `${BASE_URL}/features`, lastModified: now, changeFrequency: "monthly", priority: 0.8 },
    { url: `${BASE_URL}/blog`, lastModified: now, changeFrequency: "weekly", priority: 0.8 },
    { url: `${BASE_URL}/playground`, lastModified: now, changeFrequency: "monthly", priority: 0.9 },
    { url: `${BASE_URL}/compare`, lastModified: now, changeFrequency: "monthly", priority: 0.7 },
  ];

  const docs = getAllDocs();
  const docPages: MetadataRoute.Sitemap = docs.map((doc) => ({
    url: `${BASE_URL}/docs/${doc.slug}`,
    lastModified: now,
    changeFrequency: "monthly" as const,
    priority: 0.7,
  }));

  const posts = getAllBlogPosts();
  const blogPages: MetadataRoute.Sitemap = posts.map((post) => ({
    url: `${BASE_URL}/blog/${post.slug}`,
    lastModified: post.frontmatter.date
      ? new Date(post.frontmatter.date)
      : now,
    changeFrequency: "yearly" as const,
    priority: 0.6,
  }));

  return [...staticPages, ...docPages, ...blogPages];
}
