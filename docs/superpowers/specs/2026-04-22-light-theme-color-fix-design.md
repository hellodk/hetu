# Light-Theme Color Fix — Design Spec

**Date:** 2026-04-22  
**Branch:** deep-analysis-improvement-plan  
**Status:** Approved

---

## Problem

Three pages — Errors, Incidents list, and Incident detail — were written before the multi-theme system existed. They use hardcoded dark-mode Tailwind classes (`text-white`, `bg-gray-800/50`, `text-red-300`, `bg-red-900/30`, etc.) that render unreadably on the three light themes (Graphite, Prism, md-light).

`globals.css` already has a remediation layer but has three critical gaps:
- `<a>`/`<Link>` elements are not covered (only `div`, `section`, `aside`, `thead`, `tr`)
- Dark-tinted semantic badge colors (`bg-red-900/30 text-red-300`, etc.) are not remapped
- Recharts SVG tick colors are hardcoded hex (`#9ca3af`) — CSS selectors cannot reach SVG attributes

Additionally, the Errors page defines its own `SEVERITY_STYLE` system in JS that duplicates the per-theme `.severity-*` CSS classes already established in `globals.css`.

## Light Theme Audit

Three light themes exist and are genuinely distinct — keep all three:

| Theme | Background | Accent | Font | Signature |
|---|---|---|---|---|
| Graphite | Warm cream `#F5F3EE` | Deep blue `#2732A8` | Fraunces serif | Paper texture |
| Prism | Pure white `#FFFFFF` | Purple `#8B5CF6` | Fraunces + orbs | Gradient orbs animation |
| md-light | White-lavender `#FFFBFE` | MD3 purple `#6750A4` | Roboto | 16px radius, shadows |

## Approach: Hybrid CSS + TSX

CSS handles structural patterns (substring-selector matches on containers/borders/text).  
TSX fixes handle what CSS cannot: JS-generated class strings and SVG attributes.

### File 1 — `src/dashboard/app/globals.css`

**Extend the existing light-theme remediation block:**

1. Add `a[class*="bg-gray-8"]` and `a[class*="bg-gray-9"]` to the container background overrides (covers `<Link>` incident cards).

2. Add overrides for dark-tinted semantic badge tints on light themes:
   - `[class*="bg-red-9"]`, `[class*="bg-amber-9"]`, `[class*="bg-orange-9"]`, `[class*="bg-green-9"]`, `[class*="bg-yellow-9"]` → map to `rgb(var(--cluster-card))` (remove the dark tint).
   - `[class*="text-red-3"]`, `[class*="text-amber-3"]`, `[class*="text-orange-3"]`, `[class*="text-green-3"]`, `[class*="text-yellow-3"]` → map to their corresponding `--sev-*` token (pre-calibrated per theme for WCAG AA).

3. Add `[class*="border-red-7"]`, `[class*="border-amber-7"]`, `[class*="border-green-7"]`, `[class*="border-blue-7"]` → `rgb(var(--cluster-border))` on light themes (avoids invisible dark-tinted borders).

### File 2 — `src/dashboard/app/errors/page.tsx`

1. **`SEVERITY_STYLE` object** — replace JS-side bg/text class strings with the pre-existing semantic CSS class names (`.severity-critical`, `.severity-high`, `.severity-medium`, `.severity-low`, `.severity-info`) which already carry per-theme overrides in globals.css.

2. **`statusColor()`** — replace dark-only strings with theme-neutral equivalents using the `.severity-*` pattern or cluster tokens:
   - `open` → `.severity-critical` style (red)
   - `resolved` → `.severity-low` style (green/blue per theme)
   - default → `.severity-info` style (muted)

3. **`SummaryCard` `accentMap`/`textMap`** — replace `bg-blue-900/10 text-blue-300` etc. with cluster-token variants that are theme-calibrated.

4. **`SpikeBadge`** — replace `text-amber-300` with `text-cluster-text` or use `--sev-warn` token.

5. **Recharts `<XAxis>` and `<YAxis>` tick fill** — change `fill: '#9ca3af'` to a CSS variable resolved at runtime: wrap with `getComputedStyle(document.documentElement).getPropertyValue('--cluster-muted')` or pass a ref-resolved color.

6. **`h1 className="text-white"`** → `text-cluster-text`.

7. **`ReasonTooltip`** — replace `bg-gray-900 text-white text-gray-400` with cluster tokens.

8. **Pagination buttons** — replace `bg-gray-800 border-gray-700 text-gray-300` with cluster tokens.

### File 3 — `src/dashboard/app/incidents/page.tsx`

1. **`h1 className="text-white"`** → `text-cluster-text`.

2. **`statusBadge()`** — replace dark-only strings with token-based equivalents:
   - `open` → red using `--sev-crit` token
   - `investigating` → blue using `--sev-info` token
   - `resolved` → green using `--sev-ok` token
   - `dismissed` → muted using cluster tokens

3. **Empty-state `text-gray-500`** — already partially covered by globals.css; verify it applies.

### File 4 — `src/dashboard/app/incidents/[id]/page.tsx`

1. **Severity badge inline ternary** (`bg-red-900/30 text-red-300` etc.) → use `.severity-*` classes.

2. **`h1` and `h2` `text-white`** → `text-cluster-text`.

3. **`p className="text-sm text-gray-400"`** for summary → `text-cluster-muted`.

4. **`markdownComponents`**:
   - `li`: `text-gray-300` → `text-cluster-text`
   - `strong`: `text-white` → `text-cluster-text`
   - `h3`: `text-white` → `text-cluster-text`

5. **Chat bubble colors**:
   - User bubble: `text-blue-100` → `text-cluster-text` (blue-100 is near-invisible on light bg)
   - AI bubble: `text-gray-200` → `text-cluster-text`

6. **`MarkdownCode`** inline variant: `text-green-300` → use `--sev-ok` or `text-cluster-text` on light themes (covered by globals.css `text-gray-3` rule if we add green).

7. **`border-t border-gray-700`** divider above chat input → `border-cluster-border`.

8. **Signal timeline items** — `text-white`, `text-gray-300`, `text-gray-400`, `text-gray-500` → cluster tokens.

## Scope

- No API or data changes
- No layout changes
- Dark themes (`calm-signal`, `aurora`, `md-dark`) are untouched — all new globals.css rules scoped to light themes only
- All changes are backwards-compatible

## Testing

1. Switch to each light theme (Graphite, Prism, md-light) in Settings
2. Navigate to Errors page — verify header, chips, badges, chart, table, cards all readable
3. Navigate to Incidents list — verify incident cards, status badges, heading readable
4. Click an incident — verify heading, severity badge, signal timeline, RCA, chat all readable
5. Switch back to each dark theme — verify no regressions
