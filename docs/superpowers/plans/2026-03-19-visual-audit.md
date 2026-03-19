# Visual Audit Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce a complete visual audit report for `web/` (marketing + docs) and `apps/admin/` (developer dashboard) covering 6 quality dimensions, plus a prioritized implementation plan for every finding.

**Architecture:** Two parallel audit agents (one per site) read source code using a shared rubric, then findings are merged into a unified report. Tasks 2 and 3 are independent and MUST run in parallel. Task 4 (merge) depends on both completing. The output is a single markdown report committed to the repo.

**Tech Stack:** Static source code analysis (no live browser). Next.js 16 App Router, Tailwind CSS 3.4.17 (`web/`), Tailwind v4 + shadcn/ui (`apps/admin/`), Framer Motion, Geist fonts.

---

## File Structure

| File | Purpose |
|------|---------|
| `docs/superpowers/specs/2026-03-19-visual-audit-design.md` | Spec — read this, do not modify |
| `docs/superpowers/specs/2026-03-19-visual-audit-report.md` | **Output** — create this in Task 4 |

No source files are modified by this plan. This is a read-only audit.

---

## Task 1: Read the Spec and Understand the Rubric

**Files:**
- Read: `docs/superpowers/specs/2026-03-19-visual-audit-design.md`

- [ ] **Step 1: Read the full spec**

  Open and read `docs/superpowers/specs/2026-03-19-visual-audit-design.md` in full.
  Confirm you understand:
  - The 6 audit dimensions and what to check for each
  - The 4 severity levels (Critical / High / Medium / Low) and their definitions
  - The canonical page lists for both sites
  - The static-code limitation (no live rendering; flag unverifiable items)
  - The report structure and implementation plan tier ordering (severity-first)

- [ ] **Step 2: Verify the page inventories exist**

  Confirm:
  - `web/app/` directory exists and contains the expected route folders
  - `web/content/docs/` contains the 10 `.mdx` files: `getting-started.mdx`, `configuration.mdx`, `cli-reference.mdx`, `architecture.mdx`, `api.mdx`, `hooks.mdx`, `mcp.mdx`, `deployment.mdx`, `benchmarks.mdx`, `faq.mdx`
  - `apps/admin/app/` directory exists and contains the expected route folders
  - Both `web/tailwind.config.ts` and `apps/admin/` tailwind config are readable

---

## Task 2: Audit `web/` (Marketing + Docs Site)

**⚡ Run in parallel with Task 3.**

**Files to read (in order):**
- `web/package.json`
- `web/tailwind.config.ts`
- `web/app/globals.css`
- `web/app/layout.tsx`
- `web/app/page.tsx` (Home)
- `web/app/features/page.tsx`
- `web/app/compare/page.tsx`
- `web/app/playground/page.tsx`
- `web/app/blog/page.tsx` + any `[slug]/page.tsx`
- `web/app/docs/[[...slug]]/page.tsx`
- All components imported from the above pages (one level of static imports)
- `web/components/ui/` directory (all files)
- `web/content/docs/*.mdx` — read 2–3 representative docs pages to assess content formatting

- [ ] **Step 1: Read design system baseline**

  Read `web/package.json`, `web/tailwind.config.ts`, `web/app/globals.css`.
  Note:
  - Color tokens defined (are indigo/emerald/zinc used consistently or are there raw hex overrides?)
  - Typography scale (are font-size/weight classes consistent across pages?)
  - Spacing tokens (are custom spacing values added, or is standard Tailwind scale used?)
  - Any custom CSS that could conflict with utility classes

- [ ] **Step 2: Read root layout and shared components**

  Read `web/app/layout.tsx`.
  Note:
  - Confirm `dark` class is hardcoded on `<html>` (spec says `layout.tsx:80`)
  - Font loading strategy — are Inter and JetBrains Mono loaded via `next/font`?
  - Meta/OG tags — are they present?
  - Skip-nav link — is there one?
  - Any global accessibility patterns (focus-visible styles in globals.css?)

- [ ] **Step 3: Audit Home page (`/`)**

  Read `web/app/page.tsx` and all components it imports.
  Evaluate all 6 dimensions. Record findings in this format:

  ```
  | Page | Dimension | Issue | Severity | Evidence |
  |------|-----------|-------|----------|----------|
  | /    | ...       | ...   | ...      | file:line or description |
  ```

  Dimension checklist for each page:
  1. **Design consistency**: Typography matches scale? Colors from tokens? Consistent spacing? Border-radius consistent with other pages?
  2. **Accessibility**: All images have `alt`? Interactive elements have ARIA labels where needed? Color contrast adequate (check text-zinc-400 on zinc-950 — this is borderline WCAG AA)? Focus indicators visible?
  3. **Responsive layout**: `sm:`/`md:`/`lg:` breakpoints present where needed? Any fixed widths that could overflow on mobile? Flag anything unverifiable from source.
  4. **UX flows**: CTAs clear and hierarchically ordered? Navigation links cover expected destinations? Any dead-ends (pages with no way back)?
  5. **Performance**: `<Image>` from `next/image` used for all images? No `<img>` tags? No render-blocking scripts?
  6. **Brand/visual polish**: Consistent with the declared design language? Visual hierarchy reads naturally?

- [ ] **Step 4: Audit Features page (`/features`)**

  Read `web/app/features/page.tsx` and its imports. Apply the same 6-dimension checklist. Record findings.

- [ ] **Step 5: Audit Compare page (`/compare`)**

  Read `web/app/compare/page.tsx` and its imports. Apply checklist. Record findings.

- [ ] **Step 6: Audit Playground page (`/playground`)**

  Read `web/app/playground/page.tsx` and its imports. Apply checklist. Record findings.
  Pay extra attention to: interactive elements (keyboard accessibility), code display (is `<pre>`/`<code>` semantic?), responsive behavior of interactive panels.

- [ ] **Step 7: Audit Blog pages (`/blog`, `/blog/[slug]`)**

  Read `web/app/blog/page.tsx` and `web/app/blog/[slug]/page.tsx` (or equivalent). Apply checklist. Record findings.
  Check: pagination/listing accessibility, article semantic HTML (`<article>`, `<time>`, headings), reading width on large screens.

- [ ] **Step 8: Audit Docs pages (`/docs/[slug]`)**

  Read `web/app/docs/[[...slug]]/page.tsx`.
  Read 3 representative `.mdx` files from `web/content/docs/`: `getting-started.mdx`, `cli-reference.mdx`, `api.mdx`.
  Apply checklist. Record findings.
  Special attention: code block accessibility, heading hierarchy in MDX, sidebar navigation (keyboard nav?), search (if present), docs-specific UX (prev/next links?).

- [ ] **Step 9: Audit shared UI components**

  Read all files in `web/components/ui/` (button, card, badge, callout, code-block, steps, etc.).
  Check:
  - Button: focus ring present? disabled state handled? ARIA attributes?
  - Card: semantic HTML (is it `<article>` or `<section>` where appropriate)?
  - Code block: `<pre><code>` with `lang` attribute? Copy button accessible?
  - Any component that wraps interactive elements: keyboard support?

- [ ] **Step 10: Compile web/ findings table**

  Produce a complete findings table for `web/`:
  ```
  | Page | Dimension | Issue | Severity | Evidence |
  ```
  Include a per-dimension summary:
  - Design consistency: N Critical, N High, N Medium, N Low
  - Accessibility: ...
  - Responsive layout: ...
  - UX flows: ...
  - Performance: ...
  - Brand/visual polish: ...

---

## Task 3: Audit `apps/admin/` (Developer Dashboard)

**⚡ Run in parallel with Task 2.**

**Files to read (in order):**
- `apps/admin/package.json`
- `apps/admin/tailwind.config.*` (find the config file)
- `apps/admin/app/globals.css`
- `apps/admin/app/layout.tsx`
- `apps/admin/app/page.tsx` (Dashboard)
- `apps/admin/app/memories/page.tsx`
- `apps/admin/app/memories/[id]/page.tsx`
- `apps/admin/app/entities/page.tsx`
- `apps/admin/app/conflicts/page.tsx`
- `apps/admin/app/settings/page.tsx`
- All components imported from the above pages (one level of static imports)
- `apps/admin/components/ui/` directory (shadcn components: badge, button, card, dialog, input, select, table)

- [ ] **Step 1: Read design system baseline**

  Read `apps/admin/package.json`, tailwind config, `apps/admin/app/globals.css`.
  Note:
  - OKLch CSS variables defined — are they used consistently or are there raw color overrides?
  - Is `@layer base` properly setting up the CSS variable system?
  - Typography scale — is Geist used consistently?
  - shadcn/ui `components.json` — what's the configured style (default/new-york) and baseColor?

- [ ] **Step 2: Read root layout and shared components**

  Read `apps/admin/app/layout.tsx`.
  Note:
  - Font loading — Geist loaded via `next/font/google` or local?
  - Dark mode class application
  - Skip-nav link present?
  - Global focus-visible styles?
  - Sidebar/nav structure — semantic (`<nav>` with `aria-label`?)

- [ ] **Step 3: Audit Dashboard page (`/`)**

  Read `apps/admin/app/page.tsx` and all components it imports.
  Apply the 6-dimension checklist (same as Task 2, Step 3).

  Extra admin-dashboard considerations:
  - Data tables: are they `<table>` elements with proper `<thead>`, `<th scope>`, `<tbody>`? Or divs?
  - Empty states: does `loading.tsx` / `error.tsx` exist at the app or page level?
  - Stats/metrics: are numbers legible at small viewport?
  - Action buttons: do they have descriptive `aria-label` (not just icon buttons with no label)?

- [ ] **Step 4: Audit Memories list page (`/memories`)**

  Read `apps/admin/app/memories/page.tsx` and imports. Apply checklist. Record findings.
  Check: table/list semantics, search/filter accessibility, pagination, empty state.

- [ ] **Step 5: Audit Memory Detail page (`/memories/[id]`)**

  Read `apps/admin/app/memories/[id]/page.tsx` and imports. Apply checklist. Record findings.
  Check: back navigation, data display hierarchy, any edit/delete actions (confirmation dialogs?).

- [ ] **Step 6: Audit Entities page (`/entities`)**

  Read `apps/admin/app/entities/page.tsx` and imports. Apply checklist. Record findings.

- [ ] **Step 7: Audit Conflicts page (`/conflicts`)**

  Read `apps/admin/app/conflicts/page.tsx` and imports. Apply checklist. Record findings.
  Extra: conflict resolution UX — are action buttons clearly differentiated? Is the consequence of each action clear?

- [ ] **Step 8: Audit Settings page (`/settings`)**

  Read `apps/admin/app/settings/page.tsx` and imports. Apply checklist. Record findings.
  Check: form accessibility (labels associated with inputs?), save/reset feedback states, error states.

- [ ] **Step 9: Audit shadcn/ui components**

  Read all files in `apps/admin/components/ui/`.
  Check each component against shadcn defaults:
  - Are components using the OKLch CSS variables (not hardcoded colors)?
  - `Dialog`: focus trap implemented? `aria-labelledby`?
  - `Table`: proper semantic elements?
  - `Input`: `id` linkable to `<label>`?
  - `Select`: keyboard navigable? `aria-expanded`?
  - Any component that deviates from shadcn defaults — note the deviation and assess if intentional

- [ ] **Step 10: Check App Router convention files**

  For each route in `apps/admin/app/`, check if these files exist:
  - `loading.tsx` — skeleton/spinner for suspense boundaries
  - `error.tsx` — error boundary UI
  - `not-found.tsx` — 404 handling

  Check the same for `web/app/`.

  Record which routes are missing these files as UX flow findings.

- [ ] **Step 11: Compile apps/admin/ findings table**

  Produce a complete findings table for `apps/admin/`:
  ```
  | Page | Dimension | Issue | Severity | Evidence |
  ```
  Include per-dimension summary counts (same format as Task 2, Step 10).

---

## Task 4: Merge Findings and Write the Report

**Depends on:** Task 2 and Task 3 both complete.

**Files:**
- Create: `docs/superpowers/specs/2026-03-19-visual-audit-report.md`

- [ ] **Step 1: Identify cross-site issues**

  Compare the `web/` and `apps/admin/` findings tables.
  Look for patterns that appear in both:
  - Same missing App Router convention files
  - Same accessibility gaps (e.g., both missing skip-nav)
  - Same performance pattern (e.g., both using `<img>` instead of `<Image>`)
  - Same UX gap (e.g., no breadcrumbs on detail pages)

  List cross-site issues separately — these may warrant a shared fix.

- [ ] **Step 2: Assign unified priority**

  Combine all findings into one list. Sort by severity:
  - Critical findings first
  - High severity next
  - Within each severity tier, order by effort estimate (quickest fix first)

  Assign each finding a short fix description and an effort estimate:
  - Quick Win: < 1 hour (e.g., add `alt` attribute, add `aria-label`, add `loading.tsx`)
  - Medium: < 1 day (e.g., refactor nav to use `<nav>` with ARIA, add focus rings globally)
  - Major: > 1 day (e.g., full responsive overhaul of a complex layout, rebuild a data table with proper semantics)

- [ ] **Step 3: Write the report**

  Create `docs/superpowers/specs/2026-03-19-visual-audit-report.md` with this exact structure:

  ```markdown
  # Visual Audit Report
  **Date:** 2026-03-19
  **Sites audited:** web/ (marketing + docs), apps/admin/ (developer dashboard)

  ---

  ## 1. Executive Summary

  | Site | Critical | High | Medium | Low |
  |------|----------|------|--------|-----|
  | web/ | N | N | N | N |
  | apps/admin/ | N | N | N | N |
  | **Total** | N | N | N | N |

  ### Top 3 Priority Fixes
  1. [Most critical finding — 1 sentence]
  2. [Second most critical — 1 sentence]
  3. [Third most critical — 1 sentence]

  ---

  ## 2. Per-Site Findings

  ### web/ (Marketing + Docs)

  #### Design Consistency
  | Page | Issue | Severity | Evidence |
  ...

  #### Accessibility
  ...

  #### Responsive Layout
  ...

  #### UX Flows
  ...

  #### Performance
  ...

  #### Brand/Visual Polish
  ...

  **Dimension summary:**
  | Dimension | Critical | High | Medium | Low |
  ...

  ---

  ### apps/admin/ (Developer Dashboard)
  [Same structure as web/ section]

  ---

  ## 3. Cross-Site Issues
  | Issue | Severity | Sites Affected | Evidence |
  ...

  ---

  ## 4. Implementation Plan

  > Tiers are ordered by severity. Within each tier, fixes are ordered Quick Win → Medium → Major.

  ### Tier 1 — Critical

  #### Fix 1: [Title]
  - **Severity:** Critical
  - **Site(s):** web/ | apps/admin/ | both
  - **Files:** `exact/path/to/file.tsx`
  - **What to change:** [Specific, actionable description — no vague "improve X"]
  - **Why:** [One sentence on user impact]
  - **Effort:** Quick Win / Medium / Major

  [Repeat for each Critical finding]

  ### Tier 2 — High
  [Same format]

  ### Tier 3 — Medium
  [Same format]

  ### Tier 4 — Low
  [Same format]

  ---

  ## 5. Appendix: Page Inventory

  ### web/
  | Page | Design | A11y | Responsive | UX | Perf | Brand | Notes |
  | / | ✅/⚠️/❌ | ... | ... | ... | ... | ... | |
  ...

  ### apps/admin/
  | Page | Design | A11y | Responsive | UX | Perf | Brand | Notes |
  ...

  Legend: ✅ No issues | ⚠️ Medium/Low issues only | ❌ Critical or High issues
  ```

- [ ] **Step 4: Self-check completeness**

  Verify before saving:
  - Every page from both canonical page lists appears in the findings and appendix
  - All 10 docs pages (`getting-started`, `configuration`, `cli-reference`, `architecture`, `api`, `hooks`, `mcp`, `deployment`, `benchmarks`, `faq`) are individually listed in the appendix
  - Every finding in the findings tables has a corresponding fix in the implementation plan
  - No implementation plan fix says anything vague like "improve accessibility" — each must name a specific change and a specific file
  - Every finding flagged as "unverifiable from source" in the responsive dimension is noted as such (not marked pass or fail)

- [ ] **Step 5: Commit the report**

  ```bash
  git add docs/superpowers/specs/2026-03-19-visual-audit-report.md
  git commit -m "docs: add visual audit report for web/ and apps/admin/

  Covers 6 dimensions across both sites: design consistency, accessibility,
  responsive layout, UX flows, performance, and brand/visual polish.
  Includes prioritized implementation plan ordered by severity tier.

  Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
  ```
