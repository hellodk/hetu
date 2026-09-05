'use client'

import { useState, useEffect, useMemo, useCallback } from 'react'
import Link from 'next/link'
import { apiFetch } from '@/lib/api'
import { RefreshCw, Loader2 } from 'lucide-react'

// ─── types from the analyzer summaries (mirrors the /issues aggregation) ────────

interface HealthSummaryData {
  warningEvents: number; criticalEvents: number
  pendingPods: number; unhealthyPods: number
}
interface ErrorSummary { byReason?: Record<string, number> }
interface SecuritySummaryData { bySeverity?: { critical?: number; high?: number; medium?: number; low?: number } }

type Sev = 'crit' | 'high' | 'warn'

interface Cell {
  id: string; label: string; sev: Sev; count: number
  section: string; href: string; why: string
}

const SEV = {
  crit: { label: 'Critical', weight: 60 },
  high: { label: 'High',     weight: 7  },
  warn: { label: 'Warning',  weight: 1  },
} as const
const SEV_ORDER: Record<Sev, number> = { crit: 3, high: 2, warn: 1 }

// Literal token classes (kept literal so Tailwind's JIT emits them).
const CELL_BG:  Record<Sev, string> = { crit: 'bg-sev-crit', high: 'bg-sev-high', warn: 'bg-sev-warn' }
const LEDGER_BG = CELL_BG
const ROW_BADGE: Record<Sev, string> = {
  crit: 'bg-sev-crit/10 text-sev-crit border-sev-crit/40',
  high: 'bg-sev-high/10 text-sev-high border-sev-high/40',
  warn: 'bg-sev-warn/10 text-sev-warn border-sev-warn/40',
}

// ─── squarified treemap (worst-aspect-ratio rows) ───────────────────────────────

interface Laid extends Cell { x: number; y: number; w: number; h: number }

function worstRatio(row: { value: number }[], side: number): number {
  let sum = 0, max = -Infinity, min = Infinity
  for (const n of row) { sum += n.value; if (n.value > max) max = n.value; if (n.value < min) min = n.value }
  return Math.max((side * side * max) / (sum * sum), (sum * sum) / (side * side * min))
}

function squarify(items: { cell: Cell; value: number }[], W: number, H: number): Laid[] {
  const total = items.reduce((s, i) => s + i.value, 0) || 1
  const scale = (W * H) / total
  const nodes = items.map(i => ({ cell: i.cell, area: i.value * scale })).sort((a, b) => b.area - a.area)
  const out: Laid[] = []
  let area = { x: 0, y: 0, w: W, h: H }
  const rest = nodes.slice()
  while (rest.length) {
    let row: { cell: Cell; area: number }[] = []
    let best = Infinity
    const side = Math.min(area.w, area.h)
    while (rest.length) {
      const test = row.concat([rest[0]])
      const wr = worstRatio(test.map(n => ({ value: n.area })), side)
      if (row.length === 0 || wr <= best) { row = test; best = wr; rest.shift() } else break
    }
    const rowArea = row.reduce((s, n) => s + n.area, 0)
    if (area.w <= area.h) {
      const rh = rowArea / area.w; let ox = area.x
      for (const n of row) { const cw = n.area / rh; out.push({ ...n.cell, x: ox, y: area.y, w: cw, h: rh }); ox += cw }
      area = { x: area.x, y: area.y + rh, w: area.w, h: area.h - rh }
    } else {
      const rw = rowArea / area.h; let oy = area.y
      for (const n of row) { const ch = n.area / rw; out.push({ ...n.cell, x: area.x, y: oy, w: rw, h: ch }); oy += ch }
      area = { x: area.x + rw, y: area.y, w: area.w, h: area.h }
    }
  }
  return out
}

// ─── page ───────────────────────────────────────────────────────────────────────

export default function TriagePage() {
  const [health, setHealth] = useState<HealthSummaryData | null>(null)
  const [errors, setErrors] = useState<ErrorSummary | null>(null)
  const [security, setSecurity] = useState<SecuritySummaryData | null>(null)
  const [loading, setLoading] = useState(true)
  const [weight, setWeight] = useState<'count' | 'impact'>('count')
  const [filter, setFilter] = useState<'all' | Sev>('all')
  const [selected, setSelected] = useState<string | null>(null)
  const [size, setSize] = useState({ w: 0, h: 452 })

  // Callback ref: the map is mounted behind a loading spinner, so a plain ref +
  // mount-only effect would attach the observer before the node exists. This
  // re-attaches whenever the map element actually mounts.
  const [mapEl, setMapEl] = useState<HTMLDivElement | null>(null)

  const fetchAll = useCallback(async () => {
    setLoading(true)
    const [h, e, s] = await Promise.allSettled([
      apiFetch<{ summary: HealthSummaryData }>('/api/v1/health'),
      apiFetch<ErrorSummary>('/api/v1/errors/summary'),
      apiFetch<SecuritySummaryData>('/api/v1/security/summary'),
    ])
    if (h.status === 'fulfilled') setHealth(h.value.summary)
    if (e.status === 'fulfilled') setErrors(e.value)
    if (s.status === 'fulfilled') setSecurity(s.value)
    setLoading(false)
  }, [])

  useEffect(() => { fetchAll() }, [fetchAll])

  useEffect(() => {
    if (!mapEl) return
    const ro = new ResizeObserver(entries => {
      const cr = entries[0].contentRect
      setSize({ w: cr.width, h: cr.height })
    })
    ro.observe(mapEl)
    return () => ro.disconnect()
  }, [mapEl])

  const cells: Cell[] = useMemo(() => {
    const r = errors?.byReason ?? {}
    const raw: Cell[] = [
      { id: 'oom',       label: 'OOMKilled',        sev: 'crit', count: (r.OOMKilled ?? 0) + (r.oom ?? 0),      section: 'Pods',     href: '/errors?search=OOMKilled',                 why: 'Containers killed for memory' },
      { id: 'crash',     label: 'CrashLoopBackOff', sev: 'crit', count: r.CrashLoopBackOff ?? 0,                section: 'Pods',     href: '/errors?search=CrashLoopBackOff',          why: 'Restart loops' },
      { id: 'critevt',   label: 'Critical events',  sev: 'crit', count: health?.criticalEvents ?? 0,            section: 'Events',   href: '/workloads/events?group=core&version=v1',  why: 'Critical cluster events' },
      { id: 'cvecrit',   label: 'Critical CVEs',    sev: 'crit', count: security?.bySeverity?.critical ?? 0,    section: 'Security', href: '/security',                                why: 'Critical vulnerabilities' },
      { id: 'imgpull',   label: 'ImagePullBackOff', sev: 'high', count: r.ImagePullBackOff ?? 0,                section: 'Pods',     href: '/errors?search=ImagePullBackOff',          why: 'Image pull failures' },
      { id: 'dns',       label: 'DNS errors',       sev: 'high', count: r.DNSConfigForming ?? 0,                section: 'Errors',   href: '/errors',                                  why: 'Name resolution failures' },
      { id: 'timeout',   label: 'Timeouts',         sev: 'high', count: r.timeout ?? 0,                         section: 'Errors',   href: '/errors',                                  why: 'Upstream timeouts' },
      { id: 'http5xx',   label: 'HTTP 5xx',         sev: 'high', count: r['http.5xx'] ?? 0,                     section: 'Errors',   href: '/errors',                                  why: 'Server errors' },
      { id: 'panic',     label: 'Panics',           sev: 'high', count: (r.panic ?? 0) + (r.exception ?? 0),    section: 'Errors',   href: '/errors',                                  why: 'Panics & exceptions' },
      { id: 'cvehigh',   label: 'High CVEs',        sev: 'high', count: security?.bySeverity?.high ?? 0,        section: 'Security', href: '/security',                                why: 'High vulnerabilities' },
      { id: 'pending',   label: 'Pending pods',     sev: 'warn', count: health?.pendingPods ?? 0,               section: 'Pods',     href: '/workloads/pods?group=core&version=v1',    why: 'Unscheduled pods' },
      { id: 'unhealthy', label: 'Unhealthy pods',   sev: 'warn', count: health?.unhealthyPods ?? 0,             section: 'Pods',     href: '/workloads/pods?group=core&version=v1',    why: 'Failing readiness probes' },
      { id: 'warnevt',   label: 'Warning events',   sev: 'warn', count: health?.warningEvents ?? 0,             section: 'Events',   href: '/workloads/events?group=core&version=v1',  why: 'Warning cluster events' },
    ]
    return raw.filter(c => c.count > 0)
  }, [health, errors, security])

  const totals = useMemo(() => {
    const t: Record<Sev, number> = { crit: 0, high: 0, warn: 0 }
    let grand = 0
    for (const c of cells) { t[c.sev] += c.count; grand += c.count }
    return { ...t, grand }
  }, [cells])

  const laid = useMemo(() => {
    if (!cells.length || size.w < 2) return [] as Laid[]
    const items = cells.map(c => ({ cell: c, value: weight === 'count' ? c.count : c.count * SEV[c.sev].weight }))
    return squarify(items, size.w, size.h)
  }, [cells, weight, size])

  const spine = useMemo(
    () => cells
      .filter(c => filter === 'all' || c.sev === filter)
      .sort((a, b) => (SEV_ORDER[b.sev] - SEV_ORDER[a.sev]) || (b.count - a.count)),
    [cells, filter],
  )

  const select = (id: string) => {
    setSelected(id)
    if (filter !== 'all') { const c = cells.find(x => x.id === id); if (c && c.sev !== filter) setFilter('all') }
    requestAnimationFrame(() => {
      document.getElementById(`spine-${id}`)?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    })
  }

  return (
    <div className="min-h-screen bg-cluster-bg text-cluster-text" style={{ fontFamily: 'var(--font-body)' }}>
      <div className="max-w-6xl mx-auto px-4 sm:px-6 py-8">
        {/* header */}
        <div className="flex items-start justify-between gap-4 flex-wrap mb-5">
          <div>
            <p className="text-[11px] font-semibold tracking-[0.18em] uppercase text-cluster-muted" style={{ fontFamily: 'var(--font-mono)' }}>Triage board</p>
            <h1 className="mt-1.5 text-3xl font-semibold" style={{ fontFamily: 'var(--font-display)' }}>Find the fire, not the smoke.</h1>
            <p className="mt-2 text-sm text-cluster-muted max-w-2xl">
              {totals.grand.toLocaleString()} open issues · <span className="text-sev-crit font-semibold">{totals.crit.toLocaleString()} critical</span>.
              The density map shows where risk concentrates; the spine says what to touch first.
            </p>
          </div>
          <button onClick={fetchAll}
            className="flex items-center gap-2 px-3 py-2 text-sm rounded-md border border-cluster-border text-cluster-muted hover:text-cluster-text hover:bg-cluster-border/40 transition-colors">
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} /> Refresh
          </button>
        </div>

        {/* severity ledger */}
        <div className="mb-5">
          <div className="flex h-3.5 rounded-md overflow-hidden border border-cluster-border bg-cluster-card" role="img" aria-label="Issues by severity">
            {(['crit', 'high', 'warn'] as Sev[]).map(s => totals[s] > 0 && (
              <span key={s} className={LEDGER_BG[s]} style={{ width: `${(totals[s] / (totals.grand || 1)) * 100}%` }} title={`${SEV[s].label}: ${totals[s]}`} />
            ))}
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1 mt-2" style={{ fontFamily: 'var(--font-mono)' }}>
            {(['crit', 'high', 'warn'] as Sev[]).map(s => (
              <span key={s} className="text-[11.5px] text-cluster-muted flex items-center gap-1.5">
                <i className={`inline-block w-2.5 h-2.5 rounded-sm ${LEDGER_BG[s]}`} />
                {SEV[s].label} <b className="text-cluster-text">{totals[s].toLocaleString()}</b>
              </span>
            ))}
          </div>
        </div>

        {/* weight toggle */}
        <div className="flex items-center gap-3 flex-wrap mb-4">
          <div className="inline-flex rounded-lg border border-cluster-border bg-cluster-card p-0.5" role="group" aria-label="Weight the map by">
            {(['count', 'impact'] as const).map(w => (
              <button key={w} onClick={() => setWeight(w)} aria-pressed={weight === w}
                className={`px-3.5 py-1.5 text-sm font-semibold rounded-md transition-colors ${weight === w ? 'bg-[rgb(var(--accent))] text-white' : 'text-cluster-muted hover:text-cluster-text'}`}>
                Weight: {w === 'count' ? 'Count' : 'Impact'}
              </button>
            ))}
          </div>
          <p className="text-[13px] text-cluster-muted max-w-md">
            {weight === 'count'
              ? <><b className="text-cluster-text">Count</b> — area is how many. Warnings dominate; criticals are the small bright cells.</>
              : <><b className="text-cluster-text">Impact</b> — area is severity-weighted. The few criticals own the map.</>}
          </p>
        </div>

        {loading && !cells.length ? (
          <div className="flex items-center justify-center gap-2 py-24 text-cluster-muted">
            <Loader2 className="w-5 h-5 animate-spin" /> Loading the issue space…
          </div>
        ) : !cells.length ? (
          <div className="rounded-xl border border-cluster-border bg-cluster-card py-24 text-center text-cluster-muted">
            No open issues. The cluster is clear.
          </div>
        ) : (
          <div className="grid grid-cols-1 lg:grid-cols-[1.4fr_1fr] gap-4 items-start">
            {/* density map */}
            <section className="rounded-xl border border-cluster-border bg-cluster-card overflow-hidden" data-testid="triage-map-panel">
              <div className="flex items-center justify-between px-4 py-3 border-b border-cluster-border">
                <h2 className="text-sm font-semibold">The issue space</h2>
                <span className="text-[11.5px] text-cluster-muted" style={{ fontFamily: 'var(--font-mono)' }}>area = volume · click to scope</span>
              </div>
              <div ref={setMapEl} className="relative w-full" style={{ height: 452 }} data-testid="triage-map">
                {laid.map(c => {
                  const dim = filter !== 'all' && c.sev !== filter
                  const small = c.w < 66 || c.h < 40
                  return (
                    <button
                      key={c.id}
                      data-testid={`cell-${c.id}`}
                      onClick={() => select(c.id)}
                      aria-label={`${SEV[c.sev].label}: ${c.label}, ${c.count}`}
                      className={`absolute rounded-md overflow-hidden text-white text-left p-2 flex flex-col justify-between transition-all duration-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-cluster-text ${CELL_BG[c.sev]} ${dim ? 'opacity-15' : ''} ${selected === c.id ? 'ring-2 ring-cluster-text z-10' : ''}`}
                      style={{ left: c.x, top: c.y, width: Math.max(c.w - 3, 0), height: Math.max(c.h - 3, 0) }}
                    >
                      {!small && (
                        <>
                          <span className="text-[11px] font-semibold leading-tight drop-shadow" style={{ fontFamily: 'var(--font-mono)' }}>{c.label}</span>
                          <span className="text-[15px] font-bold drop-shadow self-start" style={{ fontFamily: 'var(--font-mono)' }}>{c.count.toLocaleString()}</span>
                        </>
                      )}
                    </button>
                  )
                })}
              </div>
            </section>

            {/* priority spine */}
            <section className="rounded-xl border border-cluster-border bg-cluster-card overflow-hidden" data-testid="triage-spine">
              <div className="flex items-center justify-between px-4 py-3 border-b border-cluster-border gap-2">
                <h2 className="text-sm font-semibold">Priority spine</h2>
                <div className="flex gap-1.5">
                  {(['all', 'crit', 'high', 'warn'] as const).map(f => (
                    <button key={f} onClick={() => setFilter(f)} aria-pressed={filter === f}
                      className={`text-[11px] font-semibold px-2.5 py-1 rounded-full border transition-colors ${filter === f ? 'border-cluster-border bg-cluster-bg text-cluster-text' : 'border-cluster-border/60 text-cluster-muted hover:text-cluster-text'}`}
                      style={{ fontFamily: 'var(--font-mono)' }}>
                      {f === 'all' ? 'All' : SEV[f].label}
                    </button>
                  ))}
                </div>
              </div>
              <div className="max-h-[452px] overflow-y-auto">
                {spine.length === 0 ? (
                  <p className="py-10 text-center text-sm text-cluster-muted">Nothing at this severity.</p>
                ) : spine.map(c => (
                  <div key={c.id} id={`spine-${c.id}`}
                    className={`grid grid-cols-[auto_1fr_auto] gap-3 items-center px-4 py-3 border-b border-cluster-border last:border-b-0 transition-colors ${selected === c.id ? 'bg-cluster-border/25' : 'hover:bg-cluster-border/15'}`}>
                    <span className={`text-[9.5px] font-bold uppercase tracking-wide px-2 py-0.5 rounded border ${ROW_BADGE[c.sev]}`} style={{ fontFamily: 'var(--font-mono)' }}>{SEV[c.sev].label}</span>
                    <div className="min-w-0">
                      <div className="text-[13.5px] font-semibold flex items-baseline gap-2 flex-wrap">
                        {c.label}
                        <span className="text-[11px] text-cluster-muted font-medium" style={{ fontFamily: 'var(--font-mono)' }}>{c.section}</span>
                      </div>
                      <div className="text-[12.5px] text-cluster-muted mt-0.5">
                        <b className="text-cluster-text" style={{ fontFamily: 'var(--font-mono)' }}>{c.count.toLocaleString()}</b> — {c.why}
                      </div>
                    </div>
                    <Link href={c.href} className="text-[12px] font-semibold text-[rgb(var(--accent))] border border-cluster-border rounded-md px-2.5 py-1.5 whitespace-nowrap hover:bg-cluster-border/30 transition-colors">
                      → {c.section}
                    </Link>
                  </div>
                ))}
              </div>
            </section>
          </div>
        )}
      </div>
    </div>
  )
}
