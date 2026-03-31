import Link from "next/link";
import Image from "next/image";

const footerLinks = {
  Product: [
    { label: "Features", href: "/#features" },
    { label: "Playground", href: "/playground" },
    { label: "Compare", href: "/compare" },
    { label: "Changelog", href: "https://github.com/ajitpratap0/openclaw-cortex/releases", external: true },
  ],
  Documentation: [
    { label: "Getting Started", href: "/docs/getting-started" },
    { label: "Configuration", href: "/docs/configuration" },
    { label: "API Reference", href: "/docs/api" },
    { label: "CLI Reference", href: "/docs/cli" },
  ],
  Community: [
    {
      label: "GitHub",
      href: "https://github.com/ajitpratap0/openclaw-cortex",
      external: true,
    },
    {
      label: "Discussions",
      href: "https://github.com/ajitpratap0/openclaw-cortex/discussions",
      external: true,
    },
    {
      label: "Issues",
      href: "https://github.com/ajitpratap0/openclaw-cortex/issues",
      external: true,
    },
    { label: "Blog", href: "/blog" },
  ],
  Legal: [
    {
      label: "MIT License",
      href: "https://github.com/ajitpratap0/openclaw-cortex/blob/main/LICENSE",
      external: true,
    },
    { label: "Privacy", href: "https://github.com/ajitpratap0/openclaw-cortex/blob/main/PRIVACY.md", external: true },
    { label: "Terms", href: "https://github.com/ajitpratap0/openclaw-cortex/blob/main/TERMS.md", external: true },
  ],
};

export default function Footer() {
  const currentYear = new Date().getFullYear();

  return (
    <footer className="border-t border-zinc-800 bg-zinc-950">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
        {/* Top section: logo + columns */}
        <div className="grid grid-cols-2 md:grid-cols-5 gap-8 mb-10">
          {/* Brand column */}
          <div className="col-span-2 md:col-span-1">
            <Link href="/" className="flex items-center gap-2 mb-3">
              <Image
                src="/logo/logo-navbar.png"
                alt="OpenClaw Cortex"
                width={40}
                height={40}
                className="flex-shrink-0"
              />
              <span className="font-semibold text-zinc-100 text-sm tracking-tight">
                openclaw-cortex
              </span>
            </Link>
            <p className="text-xs text-zinc-500 leading-relaxed">
              Hybrid semantic memory system for AI agents.
            </p>
          </div>

          {/* Link columns */}
          {Object.entries(footerLinks).map(([section, links]) => (
            <div key={section}>
              <h3 className="text-xs font-semibold text-zinc-300 uppercase tracking-wider mb-3">
                {section}
              </h3>
              <ul className="space-y-2">
                {links.map((link) => (
                  <li key={link.href}>
                    {"external" in link && link.external ? (
                      <a
                        href={link.href}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-sm text-zinc-500 hover:text-zinc-300 transition-colors"
                      >
                        {link.label}
                      </a>
                    ) : (
                      <Link
                        href={link.href}
                        className="text-sm text-zinc-500 hover:text-zinc-300 transition-colors"
                      >
                        {link.label}
                      </Link>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        {/* Bottom bar */}
        <div className="pt-8 border-t border-zinc-800 flex flex-col sm:flex-row items-center justify-between gap-3">
          <p className="text-xs text-zinc-500">
            Built by{" "}
            <a
              href="https://github.com/ajitpratap0"
              target="_blank"
              rel="noopener noreferrer"
              className="text-zinc-400 hover:text-zinc-200 transition-colors"
            >
              Ajit Pratap Singh
            </a>
          </p>
          <div className="flex items-center gap-4 text-xs text-zinc-500">
            <a
              href="https://github.com/ajitpratap0/openclaw-cortex/blob/main/LICENSE"
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-zinc-300 transition-colors"
            >
              MIT License
            </a>
            <span>&copy; {currentYear} openclaw-cortex</span>
          </div>
        </div>
      </div>
    </footer>
  );
}
