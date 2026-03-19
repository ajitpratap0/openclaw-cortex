# Visual Audit Design Spec
**Date:** 2026-03-19
**Scope:** Full visual audit of both web/ (marketing + docs) and apps/admin/ (developer dashboard)
**Output:** Findings report + implementation plan

---

## Goal

Produce a comprehensive visual audit of both sites in this repository across six quality dimensions — design consistency, accessibility, responsive layout, UX flows, performance, and brand/visual polish — with a prioritized implementation plan for every finding.

---

## Approach: Parallel Site-First Audit

Two agents run simultaneously — one per site — each auditing all six dimensions using a shared severity rubric. Results merge into a unified report with a single prioritized implementation plan.

**Why this approach:** Fastest path to full coverage. Each agent can focus deeply on one site's component tree and page structure. The shared rubric ensures findings are comparable and can be ranked together after merging.

**Limitation:** Agents audit source code statically (no live browser rendering). Responsive layout checks are limited to what can be inferred from Tailwind breakpoint classes and JSX structure. Items that require rendering (e.g., actual overflow at 320px, tap target pixel sizes) must be flagged as "unverifiable from source" rather than skipped or guessed.

---

## Sites in Scope

### Site A — `web/` (Marketing + Docs)
- **Stack:** Next.js 16.1.7 App Router, Tailwind CSS 3.4.17 (custom, no shadcn), Framer Motion, Inter + JetBrains Mono
- **Theme:** Always dark — `dark` class hardcoded on `<html>` in `web/app/layout.tsx:80`; indigo (#6366f1) + emerald (#10b981) accents, zinc-950 background
- **Pages (canonical list):**
  - `/` — Home
  - `/features` — Features
  - `/compare` — Compare
  - `/playground` — Playground
  - `/blog` — Blog index + individual posts
  - `/docs/[slug]` — 10 doc pages sourced from `web/content/docs/`: `getting-started`, `configuration`, `cli-reference`, `architecture`, `api`, `hooks`, `mcp`, `deployment`, `benchmarks`, `faq`
- **Dev server:** `npm run dev` → localhost:3000

### Site B — `apps/admin/` (Developer Dashboard)
- **Stack:** Next.js 16.1.7, shadcn/ui + Tailwind v4, Geist sans + mono
- **Theme:** Always dark, zinc monochrome, OKLch CSS variables
- **Pages:**
  - `/` — Dashboard
  - `/memories` — Memories list
  - `/memories/[id]` — Memory detail
  - `/entities` — Entities list
  - `/conflicts` — Conflicts list
  - `/settings` — Settings
- **Dev server:** `pnpm dev` → localhost:3001

---

## Audit Dimensions

Each agent evaluates all six dimensions for their site:

| # | Dimension | What to check |
|---|-----------|---------------|
| 1 | **Design consistency** | Typography scale, color usage, spacing tokens, component patterns, border-radius consistency across pages |
| 2 | **Accessibility** | Color contrast ratios (WCAG AA minimum), keyboard navigation, focus indicators, ARIA labels, semantic HTML, skip-nav |
| 3 | **Responsive layout** | Audit Tailwind breakpoint classes (`sm:`, `md:`, `lg:`) and JSX structure for mobile/tablet/desktop intent. Flag items unverifiable from source (actual rendered overflow, tap target pixel sizes) rather than skipping or guessing. |
| 4 | **UX flows** | Navigation clarity, CTA hierarchy; check for Next.js App Router convention files (`loading.tsx`, `error.tsx`, `not-found.tsx`) as primary indicators of empty/loading/error state coverage; flag pages missing these files |
| 5 | **Performance** | next/image usage for images, next/font for fonts, render-blocking assets, dynamic import usage, bundle concerns visible from source |
| 6 | **Brand/visual polish** | Visual hierarchy, whitespace, "feels finished" gut-check, consistency with the site's own declared design language |

---

## Shared Severity Rubric

| Severity | Criteria |
|----------|----------|
| **Critical** | Blocks usability or fails basic accessibility (invisible text, no focus state, broken layout at any viewport) |
| **High** | Significant degradation in UX or brand trust; user likely notices |
| **Medium** | Noticeable inconsistency or polish gap; user may notice |
| **Low** | Minor improvement opportunity; most users won't notice |

---

## Agent Methodology

Each agent will:
1. Read the site's `package.json`, tailwind config, and global CSS to understand the design system
2. Walk through every page in the canonical page list for their site (see Sites in Scope above)
3. Read shared layout files (`layout.tsx`, `page.tsx` at each route level)
4. Read component files referenced via static imports from pages; for dynamic imports or barrel re-exports, read one level deep and note if the full tree is unreachable
5. For each page, evaluate all 6 dimensions and record findings in the standard format
6. Produce a per-site findings table

**Finding format:**
```
| Page | Dimension | Issue | Severity | Evidence (file:line or description) |
```

**Scoring:** Report finding counts per severity level per dimension (no numeric weighting formula). Example: "Accessibility: 1 Critical, 2 High, 3 Medium." Do not compute percentage scores — finding counts are sufficient for prioritization.

---

## Report Structure

Saved to: `docs/superpowers/specs/2026-03-19-visual-audit-report.md`

```
1. Executive Summary
   - Finding counts per site (Critical / High / Medium / Low)
   - Top 3 priority fixes across both sites

2. Per-Site Findings
   web/ → findings table, organized by dimension
   apps/admin/ → findings table, organized by dimension

3. Cross-Site Issues
   Patterns appearing in both sites

4. Implementation Plan
   Tier 1 — Critical severity (any effort): fix these first
   Tier 2 — High severity (any effort): fix these next
   Tier 3 — Medium severity
   Tier 4 — Low severity / systemic improvements

   Tier assignment is by severity. Within each tier, fixes are ordered by
   estimated effort (quick wins first). Each fix: what to change, which file(s), why it matters.

5. Appendix
   Full page inventory with pass/fail per dimension
```

**Note on path:** Report is co-located with the spec in `docs/superpowers/specs/` for discoverability alongside planning documents.

---

## Execution Plan

1. **Dispatch parallel agents** — Agent A audits `web/`, Agent B audits `apps/admin/`, both using this spec as their rubric
2. **Merge findings** — Combine both agents' output into the unified report structure above
3. **Assign unified priority** — Rank all findings cross-site by severity (Critical first), then by effort within each tier
4. **Write implementation plan** — Sequence fixes per the tier structure defined above
5. **Save report** — Commit to `docs/superpowers/specs/2026-03-19-visual-audit-report.md`

---

## Success Criteria

- Every page in the canonical page lists above has been evaluated against all 6 dimensions
- Every finding has a severity, a file reference (or "unverifiable from source" flag), and a concrete fix description
- Implementation plan is sequenced by severity tier, then effort within each tier — no vague "improve UX" items
- All 10 docs pages (`web/content/docs/*.mdx`) are individually assessed, not treated as a single entry
