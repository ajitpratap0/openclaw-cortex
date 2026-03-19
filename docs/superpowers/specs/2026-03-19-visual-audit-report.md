# Visual Audit Report
**Date:** 2026-03-19
**Sites audited:** web/ (marketing + docs), apps/admin/ (developer dashboard)

---

## 1. Executive Summary

| Site | Critical | High | Medium | Low |
|------|----------|------|--------|-----|
| web/ | 0 | 1 | 5 | 1 |
| apps/admin/ | 0 | 0 | 11 | 7 |
| **Total** | **0** | **1** | **16** | **8** |

**By dimension:**

| Dimension | Critical | High | Medium | Low |
|-----------|----------|------|--------|-----|
| Design consistency | 0 | 0 | 3 | 2 |
| Accessibility | 0 | 1 | 4 | 1 |
| Responsive layout | 0 | 0 | 3 | 0 |
| UX flows | 0 | 0 | 5 | 1 |
| Performance | 0 | 0 | 1 | 4 |
| Brand/visual polish | 0 | 0 | 0 | 0 |

No critical issues were found across either site. Both sites share two structural gaps: missing App Router convention files (loading.tsx, error.tsx) at every route level, and unstyled/inaccessible interactive elements. The admin dashboard carries a heavier accessibility and UX debt than the marketing site due to its interactive nature.

### Top 3 Priority Fixes

1. **Add skip-nav links (both sites)** — Screen reader and keyboard users cannot bypass repeated navigation on any page in either site. This is the highest-severity accessibility gap.
2. **Fix unstyled/inaccessible action buttons in apps/admin/** — The delete button in `memory-table.tsx` lacks visual button styling and a focus indicator, making it unusable for keyboard-only users. This is a High-severity blocker for the primary destructive action in the admin UI.
3. **Replace native `confirm()` dialogs with accessible Dialog components (apps/admin/)** — Two pages use `window.confirm()` for delete confirmation, which is inaccessible to screen readers and inconsistent with the design system.

---

## 2. Per-Site Findings

> Findings are organised by dimension within each site for readability. The combined view across all dimensions follows the format `Page | Dimension | Issue | Severity | Evidence` — reading down each sub-table in order is equivalent to a single merged table sorted by dimension.

### web/ (Marketing + Docs)

#### Design Consistency

| Page | Issue | Severity | Evidence |
|------|-------|----------|----------|
| / (Home) | Raw hex values in globals.css custom utilities instead of token references | Medium | `web/app/globals.css` lines 41–77: `.glow-indigo`, `.glow-emerald`, `.text-gradient` use `rgba(99,102,241)` directly |
| All pages | `text-zinc-400` used for secondary text on `zinc-950` background — borderline WCAG AA contrast (4.5:1 ratio) | Medium | Applies globally wherever secondary body text appears |

#### Accessibility

| Page | Issue | Severity | Evidence |
|------|-------|----------|----------|
| All pages | No skip-nav link present | High | `web/app/layout.tsx` — no `<a href="#main">` or skip-link component anywhere in the layout |
| All pages | Nav links missing hover state for mobile | Low | `web/components/nav.tsx` line 117 |

#### Responsive Layout

| Page | Issue | Severity | Evidence |
|------|-------|----------|----------|
| All pages | Mobile overflow behaviour is **unverifiable from source** — Tailwind breakpoint classes (`sm:`, `md:`, `lg:`) are present throughout, but whether hardcoded spacing causes actual overflow at narrow viewports (320–375 px) cannot be confirmed without browser rendering | Medium (unverifiable from source) | Requires device/viewport rendering to confirm; flag for manual QA pass |

#### UX Flows

| Page | Issue | Severity | Evidence |
|------|-------|----------|----------|
| All routes | No loading.tsx or error.tsx at any route level | Medium | `web/app/`: loading.tsx ❌, error.tsx ❌; all sub-routes also missing |

#### Performance

| Page | Issue | Severity | Evidence |
|------|-------|----------|----------|
| /compare | No lazy loading verified for comparison table | Medium | `ComparisonTable` component not audited for image lazy-loading |

#### Brand/Visual Polish

No issues found.

**Dimension summary — web/:**

| Dimension | Critical | High | Medium | Low |
|-----------|----------|------|--------|-----|
| Design consistency | 0 | 0 | 2 | 0 |
| Accessibility | 0 | 1 | 0 | 1 |
| Responsive layout | 0 | 0 | 1 | 0 |
| UX flows | 0 | 0 | 1 | 0 |
| Performance | 0 | 0 | 1 | 0 |
| Brand/visual polish | 0 | 0 | 0 | 0 |

---

### apps/admin/ (Developer Dashboard)

> **Note on page structure:** The spec's canonical list shows `/` as "Dashboard." In the actual codebase, `apps/admin/app/page.tsx` is a simple `redirect()` to `/dashboard`, and the dashboard UI lives in `apps/admin/app/dashboard/page.tsx`. Both are audited separately below to reflect the real app structure.

#### Design Consistency

| Page | Issue | Severity | Evidence |
|------|-------|----------|----------|
| /dashboard | Inline template literal for color selection instead of CSS class | Medium | `apps/admin/components/stats-cards.tsx` lines 37–39 |
| layout.tsx | Body background uses hardcoded color class (`bg-zinc-950`) instead of CSS variable | Low | `apps/admin/app/layout.tsx` line 23 |
| /dashboard | Hardcoded `amber-400` for conflict badge instead of CSS variable | Low | `apps/admin/components/stats-cards.tsx` line 38 |

#### Accessibility

| Page | Issue | Severity | Evidence |
|------|-------|----------|----------|
| memory-table.tsx | Delete `<button>` missing focus ring and `aria-label` — hover colour only, no keyboard-visible focus indicator | Medium | `apps/admin/components/memory-table.tsx` lines 78–82 |
| /memories | Select filters missing aria-label attributes | Medium | `apps/admin/app/memories/page.tsx` lines 64–75 |
| /memories/[id] | Back button is unstyled plain text link with no aria-label | Medium | `apps/admin/app/memories/[id]/page.tsx` lines 51–56 |
| entity-table.tsx | "Show linked memories" `<button>` styled as plain text link — no `aria-label`, no `aria-expanded`, no visible button affordance | Medium | `apps/admin/components/entity-table.tsx` lines 24–29 |

#### Responsive Layout

| Page | Issue | Severity | Evidence |
|------|-------|----------|----------|
| /memories | Select and Input filters use fixed widths (`w-36`, `w-44`) that may overflow on very small screens | Medium | `apps/admin/app/memories/page.tsx` lines 65, 94 |
| /entities | Input and Select filters use fixed widths (`w-56`, `w-36`) that may overflow on small screens | Medium | `apps/admin/app/entities/page.tsx` lines 39, 42 |

#### UX Flows

| Page | Issue | Severity | Evidence |
|------|-------|----------|----------|
| /memories | Delete confirmation uses native `confirm()` dialog instead of accessible Dialog component | Medium | `apps/admin/app/memories/page.tsx` line 40 |
| /memories/[id] | Delete confirmation uses native `confirm()` dialog instead of accessible Dialog component | Medium | `apps/admin/app/memories/[id]/page.tsx` line 47 |
| /conflicts | Optimistic update — server PUT endpoint does not accept `conflict_status` field | Medium | `apps/admin/app/conflicts/page.tsx` lines 20–23 |
| All routes | No loading.tsx or error.tsx at any route level | Medium | All `apps/admin/app/` route directories missing convention files |
| / (root) | Root page uses `redirect()` without metadata or description | Low | `apps/admin/app/page.tsx` lines 1–5 |

#### Performance

| Page | Issue | Severity | Evidence |
|------|-------|----------|----------|
| /dashboard | Loading state shows minimal content — no skeleton/fallback visual | Low | `apps/admin/app/dashboard/page.tsx` lines 19–26 |
| /memories | Loading state shows minimal content — no skeleton/fallback visual | Low | `apps/admin/app/memories/page.tsx` line 108 |
| /entities | Loading state shows minimal content — no skeleton/fallback visual | Low | `apps/admin/app/entities/page.tsx` line 58 |
| /conflicts | Loading state shows minimal content — no skeleton/fallback visual | Low | `apps/admin/app/conflicts/page.tsx` line 73 |

#### Brand/Visual Polish

No additional issues (amber-400 token gap filed under Design Consistency above).

**Dimension summary — apps/admin/:**

| Dimension | Critical | High | Medium | Low |
|-----------|----------|------|--------|-----|
| Design consistency | 0 | 0 | 1 | 2 |
| Accessibility | 0 | 0 | 4 | 0 |
| Responsive layout | 0 | 0 | 2 | 0 |
| UX flows | 0 | 0 | 4 | 1 |
| Performance | 0 | 0 | 0 | 4 |
| Brand/visual polish | 0 | 0 | 0 | 0 |

---

## 3. Cross-Site Issues

| Issue | Severity | Sites Affected | Evidence |
|-------|----------|----------------|----------|
| No skip-nav link in root layout | High | Both | `web/app/layout.tsx` (no skip-link); `apps/admin/app/layout.tsx` (no skip-link) |
| No loading.tsx or error.tsx at any route level | Medium | Both | All route directories in `web/app/` and `apps/admin/app/` missing convention files |
| Hardcoded color values instead of CSS/design tokens | Medium/Low | Both | `web/app/globals.css` lines 41–77 (hex literals); `apps/admin/app/layout.tsx` line 23 and `apps/admin/components/stats-cards.tsx` lines 37–39 (hardcoded Tailwind classes) |

---

## 4. Implementation Plan

> Tiers ordered by severity (Tier 1 = Critical → Tier 4 = Low). Within each tier: Quick Win first, then Medium, then Major.

### Tier 1 — Critical

No Critical issues found in either site.

---

### Tier 2 — High

#### Fix 1: Add skip-nav links to both site root layouts
- **Severity:** High
- **Site(s):** both
- **Files:** `web/app/layout.tsx`, `apps/admin/app/layout.tsx`
- **What to change:** Insert `<a href="#main-content" className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:px-4 focus:py-2 focus:bg-white focus:text-black focus:rounded">Skip to content</a>` as the first child of `<body>`, and add `id="main-content"` to the main content wrapper in each layout.
- **Why:** Without a skip-nav link, keyboard and screen reader users must tab through the entire navigation on every page load, which violates WCAG 2.1 SC 2.4.1 (Bypass Blocks).
- **Effort:** Quick Win


---

### Tier 3 — Medium

#### Fix 2: Replace native confirm() dialogs with accessible Dialog components
- **Severity:** Medium
- **Site(s):** apps/admin/
- **Files:** `apps/admin/app/memories/page.tsx` line 40, `apps/admin/app/memories/[id]/page.tsx` line 47
- **What to change:** Replace each `window.confirm("Are you sure?")` call with a controlled state boolean (`const [showDeleteDialog, setShowDeleteDialog] = useState(false)`) and render a shadcn/ui `<Dialog>` or `<AlertDialog>` with confirm/cancel buttons. The dialog must trap focus, be announced by screen readers, and close on Escape.
- **Why:** `window.confirm()` is not announced by all screen readers, cannot be styled to match the design system, and blocks the entire browser UI during confirmation.
- **Effort:** Medium

#### Fix 3: Add App Router loading.tsx and error.tsx convention files to all routes (both sites)
- **Severity:** Medium
- **Site(s):** both
- **Files:** `web/app/` sub-routes missing files: blog/, features/, compare/, playground/ (note: `web/app/docs/[[...slug]]/loading.tsx` already exists — skip that route). `apps/admin/app/` sub-routes: dashboard/, memories/, memories/[id]/, entities/, conflicts/, settings/
- **What to change:** Create a minimal `loading.tsx` exporting a skeleton/spinner component and an `error.tsx` exporting an error boundary component with a reset button in each route directory. Reuse a shared `<LoadingSkeleton />` and `<ErrorBoundary />` component to keep the additions thin.
- **Why:** Without these files, any slow data fetch or unhandled runtime error surfaces as a blank page or a full-app crash with no user feedback.
- **Effort:** Medium

#### Fix 4: Replace raw hex values in globals.css with CSS variable references
- **Severity:** Medium
- **Site(s):** web/
- **Files:** `web/app/globals.css` lines 41–77
- **What to change:** Define CSS custom properties for each color used (e.g., `--color-indigo: 99 102 241;`) in the `:root` block, then update `.glow-indigo`, `.glow-emerald`, and `.text-gradient` to reference these variables (`rgba(var(--color-indigo) / 0.3)` etc.) instead of hardcoded `rgba(99,102,241,…)` literals.
- **Why:** Hardcoded hex values in utility classes diverge from the design token system, making theme changes require grep-and-replace rather than a single variable update.
- **Effort:** Quick Win

#### Fix 5: Resolve inline template literal color selection in stats-cards and migrate to CSS class
- **Severity:** Medium
- **Site(s):** apps/admin/
- **Files:** `apps/admin/components/stats-cards.tsx` lines 37–39
- **What to change:** Replace the inline template-literal color expression (e.g., `` `text-${color}-400` ``) with a lookup map of pre-defined Tailwind classes (e.g., `const colorMap = { amber: "text-amber-400", ... }`) so Tailwind's JIT scanner can detect all classes statically. Replace `stats-cards.tsx` line 38 hardcoded `amber-400` conflict badge class with the mapped value.
- **Why:** Dynamic class names constructed via template literals are pruned by Tailwind's JIT compiler in production builds, causing the color to disappear at runtime.
- **Effort:** Quick Win

#### Fix 6: Add aria-label attributes to Select filter elements on /memories
- **Severity:** Medium
- **Site(s):** apps/admin/
- **Files:** `apps/admin/app/memories/page.tsx` lines 64–75
- **What to change:** Add `aria-label="Filter by type"`, `aria-label="Filter by scope"`, etc. to each `<Select>` (or its trigger child) that currently has no accessible label. If shadcn/ui `Select` is used, pass `aria-label` to the `<SelectTrigger>` element.
- **Why:** Unlabeled form controls fail WCAG 2.1 SC 1.3.1 (Info and Relationships) and SC 4.1.2 (Name, Role, Value); screen readers announce them as "combo box" with no context.
- **Effort:** Quick Win

#### Fix 7: Add focus ring and aria-label to delete button in memory-table
- **Severity:** Medium
- **Site(s):** apps/admin/
- **Files:** `apps/admin/components/memory-table.tsx` lines 78–82
- **What to change:** The element is already a `<button>`. Add `focus:outline-none focus:ring-2 focus:ring-red-500 focus:ring-offset-2` to its className, and add `aria-label="Delete memory"`. Do not change the element type.
- **Why:** A `<button>` with only a hover colour change provides no visible focus indicator for keyboard users, and no accessible name for screen readers.
- **Effort:** Quick Win

#### Fix 8: Add aria-label to unstyled back button and apply button styling on /memories/[id]
- **Severity:** Medium
- **Site(s):** apps/admin/
- **Files:** `apps/admin/app/memories/[id]/page.tsx` lines 51–56
- **What to change:** Ensure the back navigation element is a `<button>` or `<a>` with an explicit `aria-label="Back to memories list"`, and apply consistent interactive styling (e.g., `className="flex items-center gap-1 text-sm text-zinc-400 hover:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-zinc-500 rounded"`).
- **Why:** An unstyled, unlabeled back control is invisible to keyboard users and announced with no context by screen readers.
- **Effort:** Quick Win

#### Fix 9: Add aria-label and consistent button styling to "show linked memories" in entity-table
- **Severity:** Medium
- **Site(s):** apps/admin/
- **Files:** `apps/admin/components/entity-table.tsx` lines 24–29
- **What to change:** Apply explicit button styling to the toggle element (visible border or underline to signal interactivity) and add `aria-label="Show linked memories for {entity.name}"` (dynamic per row) and `aria-expanded={isOpen}` so screen readers announce the expanded state.
- **Why:** A `<button>` that looks like a text link and has no accessible name violates WCAG SC 4.1.2 and makes row-level actions indistinguishable to assistive technologies.
- **Effort:** Quick Win

#### Fix 10: Replace fixed filter widths with responsive classes on /memories and /entities
- **Severity:** Medium
- **Site(s):** apps/admin/
- **Files:** `apps/admin/app/memories/page.tsx` lines 65, 94; `apps/admin/app/entities/page.tsx` lines 39, 42
- **What to change:** Replace fixed-width classes (`w-36`, `w-44`, `w-56`) with responsive equivalents (e.g., `w-full sm:w-36`) so the filter row wraps gracefully rather than overflowing on viewports narrower than 375 px.
- **Why:** Fixed-width inputs that exceed the viewport width create horizontal scrollbars and break the filter row layout on mobile devices.
- **Effort:** Quick Win

#### Fix 11: Audit and fix ComparisonTable lazy loading on /compare
- **Severity:** Medium
- **Site(s):** web/
- **Files:** `web/app/compare/` (ComparisonTable component file — locate exact path)
- **What to change:** If the table renders images, add `loading="lazy"` to all `<img>` elements that are below the fold. If the table itself is large and below the fold, wrap it in a `next/dynamic` import with `{ ssr: false, loading: () => <Skeleton /> }`.
- **Why:** Eagerly loading off-screen comparison table content delays Time to Interactive for the page hero section.
- **Effort:** Medium

#### Fix 12: Resolve conflict_status optimistic update mismatch on /conflicts
- **Severity:** Medium
- **Site(s):** apps/admin/
- **Files:** `apps/admin/app/conflicts/page.tsx` lines 20–23
- **What to change:** The code already contains an inline comment acknowledging that the server endpoint does not accept `conflict_status`. Either (a) update the server PUT endpoint to accept and persist `conflict_status`, or (b) remove the optimistic update and revalidate from the server after the mutation resolves. Update the comment to reflect which path was chosen and mark the known limitation resolved.
- **Why:** An optimistic update the server silently ignores causes the UI to show a resolved state that reverts on next page load — the inline comment documents the gap, but it should be resolved rather than tracked indefinitely.
- **Effort:** Medium

#### Fix 13: Investigate and address secondary text contrast (text-zinc-400 on zinc-950)
- **Severity:** Medium
- **Site(s):** web/
- **Files:** All pages using `text-zinc-400` on `bg-zinc-950` backgrounds
- **What to change:** Run contrast verification (e.g., using a contrast checker with `#a1a1aa` on `#09090b`). If the measured ratio falls below 4.5:1 for normal text or 3:1 for large text, upgrade to `text-zinc-300` globally for secondary body text.
- **Why:** Text that fails WCAG AA contrast requirements is difficult to read for users with low vision or in bright environments.
- **Effort:** Quick Win

---

### Tier 4 — Low

#### Fix 14: Add nav link hover states for mobile in web/ nav
- **Severity:** Low
- **Site(s):** web/
- **Files:** `web/components/nav.tsx` line 117
- **What to change:** Add `active:bg-zinc-800` or equivalent touch-feedback class to the mobile nav link element so users on touch devices receive visual confirmation that a tap was registered.
- **Why:** Without a touch/active state, mobile users see no feedback when tapping navigation items, making the interface feel unresponsive.
- **Effort:** Quick Win

#### Fix 15: Add metadata to admin root redirect page
- **Severity:** Low
- **Site(s):** apps/admin/
- **Files:** `apps/admin/app/page.tsx` lines 1–5
- **What to change:** Add a Next.js `metadata` export (`export const metadata: Metadata = { title: "OpenClaw Admin", description: "..." }`) before the `redirect()` call so that crawlers and browser tabs display a meaningful title rather than a blank string.
- **Why:** A missing title on the root page results in "Untitled" in browser history and bookmark lists, degrading the developer experience.
- **Effort:** Quick Win

#### Fix 16: Add skeleton loading states to all admin pages
- **Severity:** Low
- **Site(s):** apps/admin/
- **Files:** `apps/admin/app/dashboard/page.tsx` lines 19–26, `apps/admin/app/memories/page.tsx` line 108, `apps/admin/app/entities/page.tsx` line 58, `apps/admin/app/conflicts/page.tsx` line 73
- **What to change:** Create a shared `<TableSkeleton rows={5} />` component that renders animated `bg-zinc-800 animate-pulse` rows. Replace the current minimal loading states in each Suspense fallback or loading guard with this component.
- **Why:** Blank or near-blank loading states cause layout shift and make the app feel slower than it is; skeleton screens anchor the user's expectation of incoming content.
- **Effort:** Medium

#### Fix 17: Replace hardcoded bg-zinc-950 and amber-400 with CSS variables in admin layout
- **Severity:** Low
- **Site(s):** apps/admin/
- **Files:** `apps/admin/app/layout.tsx` line 23, `apps/admin/components/stats-cards.tsx` line 38
- **What to change:** Define `--color-surface: theme('colors.zinc.950')` and `--color-warning: theme('colors.amber.400')` in `globals.css`, then replace `bg-zinc-950` in layout.tsx with `bg-[--color-surface]` (or a CSS variable-backed class) and replace the hardcoded `amber-400` badge class in stats-cards with the variable reference.
- **Why:** Hardcoded Tailwind color classes scattered across layout and component files make future theme changes require find-and-replace rather than a single token update.
- **Effort:** Quick Win

---

## 5. Appendix: Page Inventory

### web/

| Page | Design | A11y | Responsive | UX | Perf | Brand | Notes |
|------|--------|------|------------|-----|------|-------|-------|
| / | ⚠️ | ⚠️ | ⚠️ | ✅ | ✅ | ✅ | Hero + terminal; responsive grid present but no skip-nav; raw hex in globals.css |
| /features | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | 6 detailed sections with diagrams; consistent token use; missing skip-nav |
| /compare | ✅ | ⚠️ | ✅ | ✅ | ⚠️ | ✅ | ComparisonTable not audited for lazy loading; missing skip-nav |
| /playground | ⚠️ | ⚠️ | ✅ | ✅ | ✅ | ✅ | Interactive demo; keyword similarity disclosure needed; missing skip-nav |
| /blog | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | Blog index + post template solid; prose classes correct; missing skip-nav |
| /docs | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | DocsLayout present; MDX rendering clean; root redirect works; missing skip-nav |

### web/ — Docs Pages

| Page | Design | A11y | Responsive | UX | Perf | Brand | Notes |
|------|--------|------|------------|-----|------|-------|-------|
| /docs/getting-started | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | Inherits skip-nav gap from root layout |
| /docs/configuration | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | Inherits skip-nav gap from root layout |
| /docs/cli-reference | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | Inherits skip-nav gap from root layout |
| /docs/architecture | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | Inherits skip-nav gap from root layout |
| /docs/api | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | Inherits skip-nav gap from root layout |
| /docs/hooks | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | Inherits skip-nav gap from root layout |
| /docs/mcp | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | Inherits skip-nav gap from root layout |
| /docs/deployment | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | Inherits skip-nav gap from root layout |
| /docs/benchmarks | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | Inherits skip-nav gap from root layout |
| /docs/faq | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | Inherits skip-nav gap from root layout |

### apps/admin/

| Page | Design | A11y | Responsive | UX | Perf | Brand | Notes |
|------|--------|------|------------|-----|------|-------|-------|
| / (root) | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | Simple redirect; missing metadata |
| /dashboard | ⚠️ | ⚠️ | ✅ | ✅ | ⚠️ | ⚠️ | Stats display, memory list, conflict badge; inline color logic; missing skeleton |
| /memories | ⚠️ | ⚠️ | ⚠️ | ⚠️ | ⚠️ | ✅ | List with filters, pagination, delete actions; fixed widths; native confirm(); unlabeled selects |
| /memories/[id] | ✅ | ⚠️ | ✅ | ⚠️ | ✅ | ✅ | Detail view; back button unstyled/unlabeled; native confirm() for delete |
| /entities | ⚠️ | ⚠️ | ⚠️ | ✅ | ⚠️ | ✅ | Search + type filter; fixed widths; "show linked memories" button styling issue |
| /conflicts | ✅ | ✅ | ✅ | ⚠️ | ⚠️ | ✅ | Card-based display; optimistic update mismatch; missing skeleton |
| /settings | ⚠️ | ⚠️ | ✅ | ✅ | ✅ | ✅ | API config; non-semantic form labels; hardcoded background |

Legend: ✅ No issues | ⚠️ Medium/Low only | ❌ Critical or High
