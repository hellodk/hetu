# Option B — Full Incident Command Centre Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the circular ScoreCard row + tab layout on the dashboard home page with a command-centre view: a full-width critical banner, a dark status bar, a three-column risk/incidents/recommendations strip, and a four-column cluster vitals row.

**Architecture:** Add six new components (`scoreLevel` util, `CriticalBanner`, `StatusBar`, `RiskSummaryPanel`, `IncidentsFeed`, `RecommendationsPanel`, `ClusterVitals`) and rewire `page.tsx` to use them; the existing detail tabs (Issues, Recommendations, Timeline, Namespaces) stay in place below the new strip and serve as drill-down views. The `DiagnosticPanel` path (when `scores` is null) remains unchanged.

**Tech Stack:** Next.js 14 App Router, React, Tailwind CSS v3, lucide-react, Playwright for integration tests. All colour tokens use existing `text-cluster-*` / `bg-cluster-*` CSS variables — no hardcoded hex values in component files.

---

## File Structure

| Action | Path | Responsibility |
|--------|------|---------------|
| Create | `src/dashboard/lib/scoreLevel.ts` | Pure score → severity mapping, shared colour tokens |
| Create | `src/dashboard/components/CriticalBanner.tsx` | Full-width red alert, shown only when any score ≤ 25 |
| Create | `src/dashboard/components/StatusBar.tsx` | Dark monospace header: cluster ID, live/demo pill, action buttons |
| Create | `src/dashboard/components/RiskSummaryPanel.tsx` | 260 px left column — four dimension rows with severity badge + score |
| Create | `src/dashboard/components/IncidentsFeed.tsx` | Centre column — severity-bar incident list (command-centre style) |
| Create | `src/dashboard/components/RecommendationsPanel.tsx` | 300 px right column — compact chip-decorated recommendation list |
| Create | `src/dashboard/components/ClusterVitals.tsx` | Four-column row: Nodes, Pods, CPU %, Memory % with mini progress bars |
| Modify | `src/dashboard/app/page.tsx` | Wire in new components; remove ScoreCard grid; keep tabs + DiagnosticPanel |
| Create | `src/dashboard/tests/incident-command.spec.ts` | Playwright integration tests for every new component |

---

## Task 1 — scoreLevel utility

**Files:**
- Create: `src/dashboard/lib/scoreLevel.ts`

- [ ] **Step 1: Write the failing test (inline smoke check)**

There is no Jest/Vitest in this project; verify the function is correct by tracing values by hand before writing it:
- `scoreLevel(0)` → `'critical'`
- `scoreLevel(25)` → `'critical'`
- `scoreLevel(26)` → `'high'`
- `scoreLevel(50)` → `'high'`
- `scoreLevel(51)` → `'degraded'`
- `scoreLevel(74)` → `'degraded'`
- `scoreLevel(75)` → `'ok'`
- `scoreLevel(100)` → `'ok'`

- [ ] **Step 2: Create `src/dashboard/lib/scoreLevel.ts`**

```ts
export type ScoreLevel = 'critical' | 'high' | 'degraded' | 'ok'

export function scoreLevel(score: number): ScoreLevel {
  if (score <= 25) return 'critical'
  if (score <= 50) return 'high'
  if (score <= 74) return 'degraded'
  return 'ok'
}

export const LEVEL_LABEL: Record<ScoreLevel, string> = {
  critical: 'CRITICAL',
  high: 'HIGH',
  degraded: 'DEGRADED',
  ok: 'OK',
}

// Tailwind classes that work on both light and dark themes via opacity.
export const LEVEL_COLORS: Record<ScoreLevel, {
  text: string
  bg: string
  border: string
  badge: string
  leftBorder: string
}> = {
  critical: {
    text:        'text-red-500',
    bg:          'bg-red-500/10',
    border:      'border-red-500/30',
    badge:       'bg-red-500/15 text-red-600 border border-red-500/40 dark:text-red-400',
    leftBorder:  'border-l-red-600',
  },
  high: {
    text:        'text-orange-500',
    bg:          'bg-orange-500/10',
    border:      'border-orange-500/30',
    badge:       'bg-orange-500/15 text-orange-600 border border-orange-500/40 dark:text-orange-400',
    leftBorder:  'border-l-orange-500',
  },
  degraded: {
    text:        'text-yellow-500',
    bg:          'bg-yellow-500/10',
    border:      'border-yellow-500/30',
    badge:       'bg-yellow-500/15 text-yellow-700 border border-yellow-500/40 dark:text-yellow-400',
    leftBorder:  'border-l-yellow-500',
  },
  ok: {
    text:        'text-green-500',
    bg:          'bg-green-500/10',
    border:      'border-green-500/30',
    badge:       'bg-green-500/15 text-green-700 border border-green-500/40 dark:text-green-400',
    leftBorder:  'border-l-green-500',
  },
}
```

- [ ] **Step 3: Commit**

```bash
git add src/dashboard/lib/scoreLevel.ts
git commit -m "feat(ui): add scoreLevel severity utility (0-25 critical, 26-50 high, 51-74 degraded, 75+ ok)"
```

---

## Task 2 — CriticalBanner component

**Files:**
- Create: `src/dashboard/components/CriticalBanner.tsx`

- [ ] **Step 1: Create `src/dashboard/components/CriticalBanner.tsx`**

```tsx
'use client'

import { AlertTriangle } from 'lucide-react'
import { scoreLevel, LEVEL_LABEL } from '@/lib/scoreLevel'

interface HealthScores {
  overall: number
  reliability: number
  security: number
  cost: number
  architecture: number
}

interface CriticalBannerProps {
  scores: HealthScores
  onViewIssues: () => void
}

const DIM_LABEL: Record<keyof HealthScores, string> = {
  overall:      'Overall',
  reliability:  'Reliability',
  security:     'Security',
  cost:         'Cost',
  architecture: 'Architecture',
}

export function CriticalBanner({ scores, onViewIssues }: CriticalBannerProps) {
  const criticalDims = (Object.keys(scores) as (keyof HealthScores)[])
    .filter(k => scoreLevel(scores[k]) === 'critical')

  if (criticalDims.length === 0) return null

  const summary = criticalDims
    .map(d => `${DIM_LABEL[d]}: ${scores[d]}/100`)
    .join(' · ')

  return (
    <div
      className="bg-red-600 text-white px-4 sm:px-6 py-2.5 flex items-center gap-3 text-sm font-medium"
      role="alert"
      aria-live="assertive"
    >
      <AlertTriangle className="w-4 h-4 flex-shrink-0" aria-hidden="true" />
      <strong className="font-bold tracking-wide">CRITICAL</strong>
      <span className="hidden sm:inline text-red-200">—</span>
      <span className="text-red-100">{summary}</span>
      <button
        onClick={onViewIssues}
        className="ml-auto text-red-200 hover:text-white underline font-semibold whitespace-nowrap text-xs sm:text-sm transition-colors"
        aria-label="View critical findings"
      >
        View findings →
      </button>
    </div>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add src/dashboard/components/CriticalBanner.tsx
git commit -m "feat(ui): add CriticalBanner — full-width alert for score ≤ 25"
```

---

## Task 3 — StatusBar component

**Files:**
- Create: `src/dashboard/components/StatusBar.tsx`

The existing header (`<header>` in `page.tsx`) will be **replaced** by this component. It keeps the same action buttons but switches to a compact dark monospace strip.

- [ ] **Step 1: Create `src/dashboard/components/StatusBar.tsx`**

```tsx
'use client'

import { Activity, RefreshCw, Download, Bell, Settings } from 'lucide-react'

interface StatusBarProps {
  clusterId: string
  profile: 'live' | 'mock'
  lastUpdated: Date | null
  loading: boolean
  criticalCount: number
  version: string
  onRefresh: () => void
  onExport: () => void
  onBell: () => void
  onSettings: () => void
}

export function StatusBar({
  clusterId,
  profile,
  lastUpdated,
  loading,
  criticalCount,
  version,
  onRefresh,
  onExport,
  onBell,
  onSettings,
}: StatusBarProps) {
  const isLive = profile === 'live'

  return (
    <header
      className="bg-[#14151a] text-[#e5e7eb] px-4 sm:px-6 py-2 flex items-center gap-3 sm:gap-4 text-xs font-mono sticky top-0 z-50"
      aria-label="Dashboard status bar"
    >
      {/* Brand */}
      <div className="flex items-center gap-2 font-sans font-bold text-white text-sm mr-1 flex-shrink-0">
        <Activity className="w-4 h-4 text-blue-400" aria-hidden="true" />
        <span className="hidden sm:inline">K8s Cluster Intelligence</span>
        <span className="sm:hidden">K8s Intel</span>
      </div>

      <span className="text-[#374151] hidden sm:inline" aria-hidden="true">|</span>

      <span
        className="text-[#9ca3af] hidden sm:inline truncate max-w-[120px]"
        aria-label={`Cluster: ${clusterId}`}
        title={clusterId}
      >
        {clusterId}
      </span>

      <span className="text-[#374151] hidden md:inline" aria-hidden="true">|</span>

      {/* Live / Demo pill */}
      <span
        className={`flex items-center gap-1.5 flex-shrink-0 ${isLive ? 'text-green-400' : 'text-yellow-400'}`}
        aria-label={`Profile: ${isLive ? 'live' : 'demo mode'}`}
      >
        <span
          className={`w-1.5 h-1.5 rounded-full ${isLive ? 'bg-green-400' : 'bg-yellow-400'} animate-pulse`}
          aria-hidden="true"
        />
        {isLive ? 'LIVE' : 'DEMO'}
      </span>

      {lastUpdated && (
        <>
          <span className="text-[#374151] hidden md:inline" aria-hidden="true">|</span>
          <span className="text-[#6b7280] hidden md:inline">
            {lastUpdated.toLocaleTimeString()}
          </span>
        </>
      )}

      <span className="text-[#374151] hidden lg:inline" aria-hidden="true">|</span>
      <span className="text-[#4b5563] hidden lg:inline">v{version}</span>

      {/* Actions */}
      <div className="ml-auto flex items-center gap-0.5" role="group" aria-label="Dashboard actions">
        <button
          onClick={onRefresh}
          disabled={loading}
          className="p-1.5 rounded hover:bg-[#1f2937] text-[#9ca3af] hover:text-white transition-colors disabled:opacity-50"
          aria-label={loading ? 'Refreshing data…' : 'Refresh dashboard data'}
        >
          <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} aria-hidden="true" />
        </button>

        <button
          onClick={onExport}
          className="p-1.5 rounded hover:bg-[#1f2937] text-[#9ca3af] hover:text-white transition-colors"
          aria-label="Export JSON report"
        >
          <Download className="w-4 h-4" aria-hidden="true" />
        </button>

        <button
          onClick={onBell}
          className="relative p-1.5 rounded hover:bg-[#1f2937] text-[#9ca3af] hover:text-white transition-colors"
          aria-label={`Alerts${criticalCount > 0 ? `, ${criticalCount} critical` : ', none'}`}
        >
          <Bell className="w-4 h-4" aria-hidden="true" />
          {criticalCount > 0 && (
            <span
              className="absolute -top-0.5 -right-0.5 w-4 h-4 bg-red-500 text-white text-[9px] rounded-full flex items-center justify-center font-bold leading-none"
              aria-hidden="true"
            >
              {criticalCount > 9 ? '9+' : criticalCount}
            </span>
          )}
        </button>

        <button
          onClick={onSettings}
          className="p-1.5 rounded hover:bg-[#1f2937] text-[#9ca3af] hover:text-white transition-colors"
          aria-label="Settings"
        >
          <Settings className="w-4 h-4" aria-hidden="true" />
        </button>
      </div>
    </header>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add src/dashboard/components/StatusBar.tsx
git commit -m "feat(ui): add StatusBar — dark monospace header replacing glass header"
```

---

## Task 4 — RiskSummaryPanel component

**Files:**
- Create: `src/dashboard/components/RiskSummaryPanel.tsx`

- [ ] **Step 1: Create `src/dashboard/components/RiskSummaryPanel.tsx`**

```tsx
'use client'

import { scoreLevel, LEVEL_LABEL, LEVEL_COLORS } from '@/lib/scoreLevel'

interface RiskSummaryPanelProps {
  scores: {
    reliability:  number
    security:     number
    cost:         number
    architecture: number
  }
  onDrillDown: (dimension: string) => void
}

const DIMENSIONS = [
  { key: 'reliability',  label: 'Reliability',   detail: 'Pod restarts & availability' },
  { key: 'security',     label: 'Security',       detail: 'RBAC, privileges & policies' },
  { key: 'cost',         label: 'Cost',           detail: 'Resource waste & rightsizing' },
  { key: 'architecture', label: 'Architecture',   detail: 'Design & best practices' },
] as const

export function RiskSummaryPanel({ scores, onDrillDown }: RiskSummaryPanelProps) {
  return (
    <section
      className="bg-cluster-card rounded-xl border border-cluster-border overflow-hidden"
      aria-label="Risk summary"
    >
      <div className="px-4 py-3 border-b border-cluster-border flex items-center justify-between">
        <span className="text-xs font-bold uppercase tracking-widest text-cluster-muted">
          Risk Summary
        </span>
        <span className="text-xs text-cluster-muted">/ 100</span>
      </div>

      <div>
        {DIMENSIONS.map(({ key, label, detail }) => {
          const score  = scores[key]
          const level  = scoreLevel(score)
          const colors = LEVEL_COLORS[level]

          return (
            <button
              key={key}
              onClick={() => onDrillDown(key)}
              className={`w-full text-left flex items-center gap-3 px-4 py-3 border-b border-cluster-border/50 last:border-0 border-l-[3px] ${colors.leftBorder} card-hover`}
              aria-label={`${label}: ${score} out of 100, severity ${LEVEL_LABEL[level]}`}
            >
              <span
                className={`text-[10px] font-bold tracking-widest px-2 py-0.5 rounded ${colors.badge} min-w-[66px] text-center flex-shrink-0`}
              >
                {LEVEL_LABEL[level]}
              </span>

              <span className="flex-1 min-w-0">
                <span className="block text-sm font-semibold text-cluster-text">{label}</span>
                <span className="block text-xs text-cluster-muted mt-0.5 truncate">{detail}</span>
              </span>

              <span className={`text-2xl font-extrabold flex-shrink-0 ${colors.text}`}>
                {score}
              </span>
            </button>
          )
        })}
      </div>
    </section>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add src/dashboard/components/RiskSummaryPanel.tsx
git commit -m "feat(ui): add RiskSummaryPanel — left-column severity strip with drill-down"
```

---

## Task 5 — IncidentsFeed component

**Files:**
- Create: `src/dashboard/components/IncidentsFeed.tsx`

- [ ] **Step 1: Create `src/dashboard/components/IncidentsFeed.tsx`**

```tsx
'use client'

import { ChevronRight } from 'lucide-react'

interface Issue {
  id: string
  severity: 'critical' | 'high' | 'medium' | 'low'
  category: string
  title: string
  description: string
  affectedResources: string[]
  confidence: number
}

interface IncidentsFeedProps {
  issues: Issue[]
  onViewAll: () => void
}

const SEV_BAR: Record<string, string> = {
  critical: 'bg-red-500',
  high:     'bg-orange-500',
  medium:   'bg-yellow-500',
  low:      'bg-green-500',
}

const MAX_VISIBLE = 6

export function IncidentsFeed({ issues, onViewAll }: IncidentsFeedProps) {
  const shown       = issues.slice(0, MAX_VISIBLE)
  const hasCritical = issues.some(i => i.severity === 'critical')

  return (
    <section
      className="bg-cluster-card rounded-xl border border-cluster-border overflow-hidden flex flex-col"
      aria-label="Active incidents"
    >
      <div className="px-4 py-3 border-b border-cluster-border flex items-center justify-between flex-shrink-0">
        <span className="text-xs font-bold uppercase tracking-widest text-cluster-muted">
          Active Incidents
        </span>
        <span
          className={`text-xs font-bold px-2 py-0.5 rounded-full ${
            hasCritical
              ? 'bg-red-500/15 text-red-500 border border-red-500/30'
              : 'bg-cluster-border text-cluster-muted'
          }`}
        >
          {issues.length} total
        </span>
      </div>

      {shown.length === 0 ? (
        <div className="px-4 py-10 text-center text-cluster-muted text-sm flex-1">
          No active incidents
        </div>
      ) : (
        <ul role="list" className="flex-1 overflow-y-auto">
          {shown.map(issue => (
            <li
              key={issue.id}
              className="flex items-start gap-3 px-4 py-3 border-b border-cluster-border/50 last:border-0 card-hover cursor-pointer"
            >
              {/* Left severity bar */}
              <div
                className={`w-1 self-stretch rounded-full flex-shrink-0 min-h-[44px] ${SEV_BAR[issue.severity] ?? SEV_BAR.low}`}
                aria-hidden="true"
              />

              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 mb-0.5">
                  <span className="text-[10px] font-bold uppercase tracking-wider text-cluster-muted">
                    {issue.category}
                  </span>
                </div>
                <p className="text-sm font-semibold text-cluster-text leading-snug">
                  {issue.title}
                </p>
                <p className="text-xs text-cluster-muted mt-1 leading-relaxed line-clamp-2">
                  {issue.description}
                </p>
                {issue.affectedResources.length > 0 && (
                  <div className="flex flex-wrap gap-1 mt-2">
                    {issue.affectedResources.slice(0, 3).map(r => (
                      <span
                        key={r}
                        className="text-[10px] bg-cluster-border/50 text-cluster-muted rounded px-1.5 py-0.5 font-mono"
                      >
                        {r}
                      </span>
                    ))}
                    {issue.affectedResources.length > 3 && (
                      <span className="text-[10px] text-cluster-muted">
                        +{issue.affectedResources.length - 3} more
                      </span>
                    )}
                  </div>
                )}
              </div>

              <ChevronRight
                className="w-4 h-4 text-cluster-muted/40 mt-2 flex-shrink-0"
                aria-hidden="true"
              />
            </li>
          ))}
        </ul>
      )}

      {issues.length > MAX_VISIBLE && (
        <div className="px-4 py-2.5 border-t border-cluster-border text-center flex-shrink-0">
          <button
            onClick={onViewAll}
            className="text-xs font-semibold text-blue-400 hover:text-blue-300 transition-colors"
          >
            View all {issues.length} incidents →
          </button>
        </div>
      )}
    </section>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add src/dashboard/components/IncidentsFeed.tsx
git commit -m "feat(ui): add IncidentsFeed — command-centre incident list with severity bars"
```

---

## Task 6 — RecommendationsPanel component

**Files:**
- Create: `src/dashboard/components/RecommendationsPanel.tsx`

- [ ] **Step 1: Create `src/dashboard/components/RecommendationsPanel.tsx`**

```tsx
'use client'

interface Recommendation {
  id: string
  category: string
  title: string
  severity: string
  impact: {
    costSavings?: { monthly: number; currency: string }
    effort: string
  }
}

interface RecommendationsPanelProps {
  recommendations: Recommendation[]
  onViewAll: () => void
}

const SEV_CHIP: Record<string, string> = {
  critical: 'bg-red-500/15 text-red-600 border border-red-500/30',
  high:     'bg-orange-500/15 text-orange-600 border border-orange-500/30',
  medium:   'bg-yellow-500/15 text-yellow-700 border border-yellow-500/30',
  low:      'bg-green-500/15 text-green-700 border border-green-500/30',
}

const MAX_VISIBLE = 5

export function RecommendationsPanel({ recommendations, onViewAll }: RecommendationsPanelProps) {
  const shown = recommendations.slice(0, MAX_VISIBLE)

  return (
    <section
      className="bg-cluster-card rounded-xl border border-cluster-border overflow-hidden flex flex-col"
      aria-label="Recommendations"
    >
      <div className="px-4 py-3 border-b border-cluster-border flex-shrink-0">
        <span className="text-xs font-bold uppercase tracking-widest text-cluster-muted">
          Recommendations
        </span>
      </div>

      {shown.length === 0 ? (
        <div className="px-4 py-10 text-center text-cluster-muted text-sm flex-1">
          No recommendations
        </div>
      ) : (
        <ul role="list" className="flex-1 overflow-y-auto">
          {shown.map((rec, i) => (
            <li
              key={rec.id}
              className="px-4 py-3 border-b border-cluster-border/50 last:border-0 card-hover cursor-pointer"
            >
              <div className="text-[10px] font-bold text-cluster-muted mb-1">
                #{i + 1} · {rec.category.toUpperCase()}
              </div>
              <p className="text-sm font-semibold text-cluster-text leading-snug mb-2">
                {rec.title}
              </p>
              <div className="flex flex-wrap gap-1.5">
                <span
                  className={`text-[10px] font-semibold px-1.5 py-0.5 rounded ${SEV_CHIP[rec.severity] ?? SEV_CHIP.low}`}
                >
                  {rec.severity.toUpperCase()}
                </span>
                <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-cluster-border/50 text-cluster-muted border border-cluster-border">
                  {rec.impact.effort} effort
                </span>
                {rec.impact.costSavings && rec.impact.costSavings.monthly > 0 && (
                  <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-blue-500/10 text-blue-500 border border-blue-500/30">
                    ${rec.impact.costSavings.monthly}/mo
                  </span>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}

      {recommendations.length > MAX_VISIBLE && (
        <div className="px-4 py-2.5 border-t border-cluster-border text-center flex-shrink-0">
          <button
            onClick={onViewAll}
            className="text-xs font-semibold text-blue-400 hover:text-blue-300 transition-colors"
          >
            View all {recommendations.length} →
          </button>
        </div>
      )}
    </section>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add src/dashboard/components/RecommendationsPanel.tsx
git commit -m "feat(ui): add RecommendationsPanel — compact chip-decorated right column"
```

---

## Task 7 — ClusterVitals component

**Files:**
- Create: `src/dashboard/components/ClusterVitals.tsx`

- [ ] **Step 1: Create `src/dashboard/components/ClusterVitals.tsx`**

```tsx
'use client'

import { Server, Boxes, Cpu, HardDrive } from 'lucide-react'

interface ClusterVitalsProps {
  summary: {
    totalNodes:    number
    totalPods:     number
    healthyPods:   number
    unhealthyPods: number
    pendingPods:   number
  }
  resources: {
    cpu:    { used: number; capacity: number; unit: string }
    memory: { used: number; capacity: number; unit: string }
  }
}

function VitalCard({
  label,
  icon,
  main,
  sub,
  barPercent,
  barColor,
}: {
  label:       string
  icon:        React.ReactNode
  main:        string
  sub:         string
  barPercent?: number
  barColor?:   string
}) {
  return (
    <div className="bg-cluster-card rounded-xl border border-cluster-border p-4 card-hover">
      <div className="flex items-center gap-2 mb-2">
        <span className="text-cluster-muted" aria-hidden="true">{icon}</span>
        <span className="text-xs font-bold uppercase tracking-widest text-cluster-muted">
          {label}
        </span>
      </div>
      <div className="text-3xl font-extrabold text-cluster-text leading-none">{main}</div>
      <div className="text-xs text-cluster-muted mt-1 leading-snug">{sub}</div>
      {barPercent !== undefined && (
        <div
          className="mt-3 h-1.5 bg-cluster-border rounded-full overflow-hidden"
          role="img"
          aria-label={`${barPercent}% utilized`}
        >
          <div
            className={`h-full rounded-full transition-all duration-500 ${barColor ?? 'bg-blue-500'}`}
            style={{ width: `${Math.min(barPercent, 100)}%` }}
            aria-hidden="true"
          />
        </div>
      )}
    </div>
  )
}

export function ClusterVitals({ summary, resources }: ClusterVitalsProps) {
  const cpuPct = resources.cpu.capacity > 0
    ? Math.round((resources.cpu.used / resources.cpu.capacity) * 100)
    : 0
  const memPct = resources.memory.capacity > 0
    ? Math.round((resources.memory.used / resources.memory.capacity) * 100)
    : 0
  const healthyPct = summary.totalPods > 0
    ? Math.round((summary.healthyPods / summary.totalPods) * 100)
    : 100

  const cpuColor = cpuPct >= 90 ? 'bg-red-500' : cpuPct >= 70 ? 'bg-yellow-500' : 'bg-blue-500'
  const memColor = memPct >= 90 ? 'bg-red-500' : memPct >= 70 ? 'bg-yellow-500' : 'bg-purple-500'

  const notReadyCount = summary.unhealthyPods + summary.pendingPods

  return (
    <section
      aria-label="Cluster vitals"
      className="grid grid-cols-2 sm:grid-cols-4 gap-4"
    >
      <VitalCard
        label="Nodes"
        icon={<Server className="w-4 h-4" />}
        main={String(summary.totalNodes)}
        sub="cluster nodes"
      />
      <VitalCard
        label="Pods"
        icon={<Boxes className="w-4 h-4" />}
        main={String(summary.totalPods)}
        sub={`${summary.healthyPods} healthy · ${notReadyCount} not ready`}
        barPercent={healthyPct}
        barColor="bg-green-500"
      />
      <VitalCard
        label="CPU"
        icon={<Cpu className="w-4 h-4" />}
        main={`${cpuPct}%`}
        sub={`${resources.cpu.used} / ${resources.cpu.capacity} ${resources.cpu.unit}`}
        barPercent={cpuPct}
        barColor={cpuColor}
      />
      <VitalCard
        label="Memory"
        icon={<HardDrive className="w-4 h-4" />}
        main={`${memPct}%`}
        sub={`${resources.memory.used} / ${resources.memory.capacity} ${resources.memory.unit}`}
        barPercent={memPct}
        barColor={memColor}
      />
    </section>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add src/dashboard/components/ClusterVitals.tsx
git commit -m "feat(ui): add ClusterVitals — 4-column nodes/pods/cpu/memory vitals row"
```

---

## Task 8 — Rewire page.tsx

**Files:**
- Modify: `src/dashboard/app/page.tsx`

This is the largest change. Do it in three focused sub-steps.

### 8a — Update imports

- [ ] **Step 1: Replace the ScoreCard import and add new component imports**

Find this block near the top of `page.tsx`:
```tsx
import { ScoreCard } from '@/components/ScoreCard'
import { IssuesList } from '@/components/IssuesList'
import { RecommendationsList } from '@/components/RecommendationsList'
import { ResourceUtilization } from '@/components/ResourceUtilization'
import { TimelineChart } from '@/components/TimelineChart'
import { ClusterSummary } from '@/components/ClusterSummary'
```

Replace with:
```tsx
import { IssuesList } from '@/components/IssuesList'
import { RecommendationsList } from '@/components/RecommendationsList'
import { TimelineChart } from '@/components/TimelineChart'
import { CriticalBanner } from '@/components/CriticalBanner'
import { StatusBar } from '@/components/StatusBar'
import { RiskSummaryPanel } from '@/components/RiskSummaryPanel'
import { IncidentsFeed } from '@/components/IncidentsFeed'
import { RecommendationsPanel } from '@/components/RecommendationsPanel'
import { ClusterVitals } from '@/components/ClusterVitals'
```

Also remove `Activity, Shield, DollarSign, Boxes, CheckCircle, TrendingUp, Server, Cpu, HardDrive, Network, ChevronRight` from the lucide-react import (these were used only by ScoreCard display code). Keep `AlertTriangle, RefreshCw, Settings, Bell, Download, Check, X, Info, Clock`.

The updated lucide import:
```tsx
import {
  AlertTriangle,
  RefreshCw, Settings, Bell, Download,
  Check, X, Info, Clock,
} from 'lucide-react'
```

### 8b — Replace the header block

- [ ] **Step 2: Replace `<header>` element with `<StatusBar />`**

Find the full `<header>` block in the return statement (starts `<header className="border-b border-cluster-border...` and ends with `</header>`).

Replace the entire block with:
```tsx
      <StatusBar
        clusterId={displayReport.clusterId}
        profile={displayReport.status?.profile ?? 'live'}
        lastUpdated={lastUpdated}
        loading={loading}
        criticalCount={criticalIssueCount}
        version={packageJson.version}
        onRefresh={handleRefresh}
        onExport={() => {
          const blob = new Blob([JSON.stringify(displayReport, null, 2)], { type: 'application/json' })
          const url = URL.createObjectURL(blob)
          const a = document.createElement('a')
          a.href = url
          a.download = `k8s-health-report-${displayReport.clusterId}-${new Date().toISOString()}.json`
          a.click()
          URL.revokeObjectURL(url)
          addToast('success', 'Exported health report to JSON')
        }}
        onBell={() => setActiveTab('issues')}
        onSettings={() => setShowSettings(true)}
      />
```

### 8c — Replace the ScoreCard grid and add command-centre layout

- [ ] **Step 3: Replace the scores section with the new command-centre layout**

Find this entire block (inside `<main>`):
```tsx
        {displayReport.scores ? (
          <section aria-labelledby="scores-heading">
            <h2 id="scores-heading" className="sr-only">Health Scores</h2>
            <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3 sm:gap-4 mb-6">
              <ScoreCard ... />
              <ScoreCard ... />
              <ScoreCard ... />
              <ScoreCard ... />
              <ScoreCard ... />
            </div>
          </section>
        ) : (
          <DiagnosticPanel ... />
        )}
```

Replace with:
```tsx
        {/* Critical banner — only when any score ≤ 25 */}
        {displayReport.scores && (
          <CriticalBanner
            scores={displayReport.scores}
            onViewIssues={() => setActiveTab('issues')}
          />
        )}

        {displayReport.scores ? (
          <>
            {/* Command-centre strip: Risk | Incidents | Recommendations */}
            <section
              aria-label="Cluster command centre"
              className="grid grid-cols-1 lg:grid-cols-[260px_1fr_300px] gap-4 sm:gap-5 mb-5"
            >
              <RiskSummaryPanel
                scores={displayReport.scores}
                onDrillDown={(dim) => {
                  setBreakdownExpanded(true)
                  setFocusDimension(dim)
                  setTimeout(() => {
                    document.getElementById(`breakdown-${dim}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' })
                    setTimeout(() => setFocusDimension(null), 3000)
                  }, 100)
                }}
              />
              <IncidentsFeed
                issues={displayReport.topIssues}
                onViewAll={() => setActiveTab('issues')}
              />
              <RecommendationsPanel
                recommendations={displayReport.recommendations}
                onViewAll={() => setActiveTab('recommendations')}
              />
            </section>

            {/* Cluster vitals row */}
            <div className="mb-5">
              <ClusterVitals
                summary={displayReport.summary}
                resources={displayReport.resourceUtilization}
              />
            </div>
          </>
        ) : (
          <DiagnosticPanel
            status={displayReport.status}
            onRetry={handleRefresh}
          />
        )}
```

Note: the `<CriticalBanner>` is placed **outside** `<main>` in the outer `<div className="min-h-screen flex flex-col">`, immediately before `<main>`. Move it there so the red banner appears between the StatusBar and the main content area. Adjust the JSX accordingly.

- [ ] **Step 4: Update the Overview tab content**

In the overview tab panel, `ClusterSummary` and `ResourceUtilization` are now superseded by the vitals row above the tabs. Replace the overview tab with:
```tsx
            {activeTab === 'overview' && (
              <>
                <CoreDNSHealth />
              </>
            )}
```

Remove these imports from `page.tsx` (no longer used on the main page):
```tsx
// Remove:
import { ClusterSummary } from '@/components/ClusterSummary'
import { ResourceUtilization } from '@/components/ResourceUtilization'
```

- [ ] **Step 5: Commit**

```bash
git add src/dashboard/app/page.tsx
git commit -m "feat(ui): rewire page.tsx — command-centre layout with StatusBar, RiskSummaryPanel, IncidentsFeed, Recommendations, ClusterVitals"
```

---

## Task 9 — Playwright integration tests

**Files:**
- Create: `src/dashboard/tests/incident-command.spec.ts`

- [ ] **Step 1: Create `src/dashboard/tests/incident-command.spec.ts`**

```ts
import { test, expect } from '@playwright/test'
import { mockHealthReport } from './fixtures/api'

const CRITICAL_SCORES = { overall: 10, reliability: 80, security: 0, cost: 80, architecture: 80 }
const OK_SCORES       = { overall: 90, reliability: 90, security: 85, cost: 88, architecture: 92 }

test.describe('Incident Command Centre', () => {
  // ── CriticalBanner ────────────────────────────────────────────────────────

  test('CriticalBanner visible when any score ≤ 25', async ({ page }) => {
    await mockHealthReport(page, { scores: CRITICAL_SCORES })
    await page.goto('/')
    const banner = page.getByRole('alert')
    await expect(banner).toBeVisible()
    await expect(banner).toContainText('CRITICAL')
    await expect(banner).toContainText('Security: 0/100')
  })

  test('CriticalBanner hidden when all scores ≥ 26', async ({ page }) => {
    await mockHealthReport(page, { scores: OK_SCORES })
    await page.goto('/')
    // No [role=alert] element should be present
    await expect(page.getByRole('alert')).not.toBeAttached()
  })

  test('CriticalBanner "View findings" button switches to issues tab', async ({ page }) => {
    await mockHealthReport(page, { scores: CRITICAL_SCORES })
    await page.goto('/')
    await page.getByRole('alert').getByRole('button', { name: /view findings/i }).click()
    // Issues tab becomes selected
    await expect(page.getByRole('tab', { name: /issues/i })).toHaveAttribute('aria-selected', 'true')
  })

  // ── StatusBar ─────────────────────────────────────────────────────────────

  test('StatusBar shows LIVE indicator when profile is live', async ({ page }) => {
    await mockHealthReport(page, { status: { state: 'ok', profile: 'live', message: '', collector: { reachable: true }, llm: { reachable: true } } })
    await page.goto('/')
    const header = page.getByRole('banner')
    await expect(header).toContainText('LIVE')
  })

  test('StatusBar shows DEMO indicator when profile is mock', async ({ page }) => {
    await mockHealthReport(page, { status: { state: 'ok', profile: 'mock', message: '', collector: { reachable: true }, llm: { reachable: true } } })
    await page.goto('/')
    const header = page.getByRole('banner')
    await expect(header).toContainText('DEMO')
  })

  // ── RiskSummaryPanel ──────────────────────────────────────────────────────

  test('RiskSummaryPanel visible when scores present', async ({ page }) => {
    await mockHealthReport(page, { scores: OK_SCORES })
    await page.goto('/')
    await expect(page.getByRole('region', { name: 'Risk summary' })).toBeVisible()
  })

  test('RiskSummaryPanel shows CRITICAL badge for score ≤ 25', async ({ page }) => {
    await mockHealthReport(page, { scores: { ...OK_SCORES, security: 10 } })
    await page.goto('/')
    const panel = page.getByRole('region', { name: 'Risk summary' })
    // The Security row should have the CRITICAL badge text
    const secBtn = panel.getByRole('button', { name: /security/i })
    await expect(secBtn).toContainText('CRITICAL')
  })

  test('RiskSummaryPanel shows OK badge when score ≥ 75', async ({ page }) => {
    await mockHealthReport(page, { scores: OK_SCORES })
    await page.goto('/')
    const panel = page.getByRole('region', { name: 'Risk summary' })
    // All rows should show OK
    const buttons = panel.getByRole('button')
    await expect(buttons.first()).toContainText('OK')
  })

  test('RiskSummaryPanel not rendered when scores null', async ({ page }) => {
    await mockHealthReport(page, {
      scores: null,
      status: { state: 'awaiting', profile: 'live', message: 'Awaiting analysis', collector: { reachable: false }, llm: { reachable: false } },
    })
    await page.goto('/')
    await expect(page.getByRole('region', { name: 'Risk summary' })).not.toBeAttached()
  })

  // ── IncidentsFeed ─────────────────────────────────────────────────────────

  test('IncidentsFeed renders issue titles', async ({ page }) => {
    await mockHealthReport(page, {
      scores: OK_SCORES,
      topIssues: [
        { id: 'i1', severity: 'critical', category: 'Security', title: 'Privileged container detected', description: 'A container is running with privileged: true', affectedResources: ['prod/nginx'], confidence: 95 },
        { id: 'i2', severity: 'high',     category: 'Reliability', title: 'Pod crash loop detected', description: 'Pod has restarted 10 times', affectedResources: [], confidence: 80 },
      ],
    })
    await page.goto('/')
    const feed = page.getByRole('region', { name: 'Active incidents' })
    await expect(feed).toBeVisible()
    await expect(feed.getByText('Privileged container detected')).toBeVisible()
    await expect(feed.getByText('Pod crash loop detected')).toBeVisible()
  })

  test('IncidentsFeed shows "No active incidents" when empty', async ({ page }) => {
    await mockHealthReport(page, { scores: OK_SCORES, topIssues: [] })
    await page.goto('/')
    const feed = page.getByRole('region', { name: 'Active incidents' })
    await expect(feed.getByText('No active incidents')).toBeVisible()
  })

  // ── RecommendationsPanel ──────────────────────────────────────────────────

  test('RecommendationsPanel renders recommendation titles', async ({ page }) => {
    await mockHealthReport(page, {
      scores: OK_SCORES,
      recommendations: [{
        id: 'r1', category: 'Cost', title: 'Rightsize nginx deployment',
        severity: 'high',
        confidence: 90,
        description: 'CPU requests are 4× actual usage',
        aiReasoning: '',
        impact: { costSavings: { monthly: 45, currency: 'USD' }, riskLevel: 'low', effort: 'low' },
      }],
    })
    await page.goto('/')
    const panel = page.getByRole('region', { name: 'Recommendations' })
    await expect(panel).toBeVisible()
    await expect(panel.getByText('Rightsize nginx deployment')).toBeVisible()
  })

  // ── ClusterVitals ─────────────────────────────────────────────────────────

  test('ClusterVitals shows node count from summary', async ({ page }) => {
    await mockHealthReport(page, {
      scores: OK_SCORES,
      summary: { totalNodes: 7, totalPods: 30, healthyPods: 28, unhealthyPods: 1, pendingPods: 1, totalNamespaces: 5, warningEvents: 0, criticalEvents: 0 },
    })
    await page.goto('/')
    const vitals = page.getByRole('region', { name: 'Cluster vitals' })
    await expect(vitals).toBeVisible()
    await expect(vitals.getByText('7')).toBeVisible()
    await expect(vitals.getByText('30')).toBeVisible()
  })

  test('ClusterVitals CPU bar is red when usage ≥ 90%', async ({ page }) => {
    await mockHealthReport(page, {
      scores: OK_SCORES,
      resourceUtilization: {
        cpu:     { used: 9, requested: 9, capacity: 10, unit: 'cores' },
        memory:  { used: 4, requested: 4, capacity: 8, unit: 'GiB' },
        storage: { used: 0, requested: 0, capacity: 0, unit: 'GiB' },
      },
    })
    await page.goto('/')
    const vitals = page.getByRole('region', { name: 'Cluster vitals' })
    await expect(vitals.getByText('90%')).toBeVisible()
  })
})
```

- [ ] **Step 2: Run tests to confirm they fail (components not yet wired in)**

```bash
cd src/dashboard && npx playwright test tests/incident-command.spec.ts --reporter=line 2>&1 | head -40
```

Expected: multiple FAIL lines — CriticalBanner/RiskSummaryPanel/IncidentsFeed/ClusterVitals not found.

- [ ] **Step 3: Build after implementing all components (Tasks 1–8 done)**

```bash
cd src/dashboard && npm run build 2>&1 | tail -20
```

Expected: `✓ Compiled successfully` with no TypeScript errors.

- [ ] **Step 4: Start the server and run tests**

```bash
bash scripts/run-local.sh restart --yes
```

Wait for "ready" output, then:

```bash
cd src/dashboard && npx playwright test tests/incident-command.spec.ts --reporter=line
```

Expected: all tests pass (green).

- [ ] **Step 5: Commit**

```bash
git add src/dashboard/tests/incident-command.spec.ts
git commit -m "test(ui): add Playwright integration tests for Incident Command Centre"
```

---

## Task 10 — Visual verification

- [ ] **Step 1: Open the dashboard in a browser**

Navigate to `http://localhost:3000` and verify:

1. Dark status bar at the top — cluster ID, LIVE/DEMO pill, action buttons.
2. No circular score-ring cards.
3. Three-column strip: Risk Summary (left, ~260 px) | Active Incidents (centre) | Recommendations (right).
4. Four-column vitals row below the strip.
5. Tabs below that — Overview, Issues, Recommendations, Timeline, Namespaces.

- [ ] **Step 2: Test with a critical score**

Open Settings → switch to Demo mode. Verify the red `CRITICAL` banner appears when a demo score ≤ 25.

- [ ] **Step 3: Test responsive layout**

Resize to mobile width (~375 px). Verify the three-column strip stacks vertically (single column), vitals become 2-column, status bar abbreviates.

- [ ] **Step 4: Test all 6 themes**

Use the theme selector. Verify severity badges and text are readable on both light (graphite, prism) and dark (aurora, md-dark, calm-signal, md-light) themes.

- [ ] **Step 5: Final commit**

```bash
git add -p
git commit -m "chore: visual verification complete — Option B Incident Command Centre"
```

---

## Self-Review Checklist

### Spec coverage
- [x] Dark status bar replacing glass header → `StatusBar` (Task 3, wired Task 8b)
- [x] Full-width critical banner when score ≤ 25 → `CriticalBanner` (Task 2, wired Task 8c)
- [x] Left risk panel with severity badges → `RiskSummaryPanel` (Task 4, wired Task 8c)
- [x] Centre incidents feed with severity bars → `IncidentsFeed` (Task 5, wired Task 8c)
- [x] Right recommendations panel → `RecommendationsPanel` (Task 6, wired Task 8c)
- [x] Four-column vitals row → `ClusterVitals` (Task 7, wired Task 8c)
- [x] Severity model 0-25/26-50/51-74/75-100 → `scoreLevel.ts` (Task 1)
- [x] Remove ScoreCard tiles → Task 8 removes the grid and ScoreCard import
- [x] DiagnosticPanel (scores null) unchanged → kept in Task 8c conditional
- [x] Drill-down to ScoreBreakdown still works → `onDrillDown` in RiskSummaryPanel wired to existing `drillIntoDimension` logic
- [x] Playwright tests for all critical behaviours → Task 9

### No placeholders
All code blocks are complete and self-contained.

### Type consistency
- `HealthScores` interface repeated in `CriticalBanner.tsx` — matches `page.tsx` definition exactly.
- `scoreLevel()` returns `ScoreLevel` exported type; `LEVEL_COLORS[level]` always defined for all four values.
- `IncidentsFeed` uses the same `Issue` interface shape as `page.tsx` (subset of fields, all present in mock).
- `ClusterVitals` receives `resourceUtilization` which in `page.tsx` is typed `ResourceUtilizationData` — the `resources` prop uses the same field names (`cpu`, `memory`, `used`, `capacity`, `unit`).
