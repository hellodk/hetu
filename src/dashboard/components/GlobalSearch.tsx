'use client'

import { useState, useEffect, useCallback, useRef } from 'react'
import { useRouter } from 'next/navigation'
import {
  Search, X, Clock, ArrowRight, Loader2, ChevronRight,
  AlertTriangle, Zap, Shield, Activity, Boxes, Network,
  Server, Database, LayoutDashboard, TrendingDown, Settings,
  BarChart2, Bug, Globe, Skull, RefreshCw, CircleDot, Cpu
} from 'lucide-react'
import { getApiUrl } from '@/lib/api'

// ─── types ────────────────────────────────────────────────────────────────────

interface SearchResult {
  id: string
  category: 'error' | 'incident' | 'namespace' | 'pod' | 'deployment' | 'service' | 'ingress' | 'node'
  title: string
  subtitle: string
  href: string
  status?: string   // Running, Pending, Failed, etc.
  badge?: string
  badgeSeverity?: string
}

interface ErrorGroup { id: number; title: string; namespace: string; service: string; reason: string; count: number; level: string }
interface Incident { id: string; title: string; severity: string; status: string }
interface K8sItem {
  name: string; namespace?: string; status?: { phase?: string; state?: string; ready?: boolean }
}

// ─── constants ────────────────────────────────────────────────────────────────

const CATEGORY_META = {
  error:      { label: 'Error Groups',  color: 'text-red-500',    dot: 'bg-red-500'    },
  incident:   { label: 'Incidents',     color: 'text-yellow-500', dot: 'bg-yellow-500' },
  namespace:  { label: 'Namespaces',    color: 'text-cyan-500',   dot: 'bg-cyan-500'   },
  pod:        { label: 'Pods',          color: 'text-blue-500',   dot: 'bg-blue-500'   },
  deployment: { label: 'Deployments',   color: 'text-green-500',  dot: 'bg-green-500'  },
  service:    { label: 'Services',      color: 'text-purple-500', dot: 'bg-purple-500' },
  ingress:    { label: 'Ingresses',     color: 'text-orange-500', dot: 'bg-orange-500' },
  node:       { label: 'Nodes',         color: 'text-teal-500',   dot: 'bg-teal-500'   },
}

const STATUS_DOT: Record<string, string> = {
  Running:            'bg-green-500',
  Succeeded:          'bg-green-400',
  Pending:            'bg-amber-500',
  Failed:             'bg-red-500',
  CrashLoopBackOff:   'bg-red-500',
  OOMKilled:          'bg-red-600',
  Evicted:            'bg-red-400',
  Terminating:        'bg-slate-400',
  Unknown:            'bg-slate-400',
}

const QUICK_LINKS = [
  { title: 'Overview',          href: '/',            icon: LayoutDashboard, sub: 'Cluster health scores' },
  { title: 'Issues Dashboard',  href: '/issues',      icon: AlertTriangle,   sub: 'Pod & cluster issues'  },
  { title: 'Error Groups',      href: '/errors',      icon: Bug,             sub: 'Application errors'    },
  { title: 'Incidents & RCA',   href: '/incidents',   icon: Zap,             sub: 'Active incidents'      },
  { title: 'LB Logs',           href: '/lb-logs',     icon: Globe,           sub: 'Load-balancer access'  },
  { title: 'Security',          href: '/security',    icon: Shield,          sub: 'CIS benchmarks & CVEs' },
  { title: 'Optimization',      href: '/optimization',icon: TrendingDown,    sub: 'Cost & right-sizing'   },
  { title: 'Anomalies',         href: '/anomalies',   icon: Activity,        sub: 'Detected anomalies'    },
  { title: 'Executive Summary', href: '/management',  icon: BarChart2,       sub: 'VP/CTO view'           },
  { title: 'Settings',          href: '/settings',    icon: Settings,        sub: 'LLM config & theme'    },
]

// Pre-wired operational shortcuts — shown in the empty state, navigating directly
// to the right filtered workloads or issues view.
const SMART_SEARCHES = [
  { label: 'Evicted pods',     icon: Skull,       href: '/workloads/pods?search=Evicted&group=core&version=v1',       color: 'text-red-500'    },
  { label: 'OOM killed',       icon: Skull,       href: '/issues?open=oom',                                            color: 'text-red-500'    },
  { label: 'CrashLoopBackOff', icon: RefreshCw,   href: '/issues?open=crashloop',                                      color: 'text-orange-500' },
  { label: 'Pending pods',     icon: CircleDot,   href: '/workloads/pods?search=Pending&group=core&version=v1',        color: 'text-amber-500'  },
  { label: 'All nodes',        icon: Cpu,         href: '/workloads/nodes?group=core&version=v1',                      color: 'text-teal-500'   },
  { label: 'Warning events',   icon: AlertTriangle,href: '/workloads/events?group=core&version=v1',                    color: 'text-amber-500'  },
]

const SEVERITY_BADGE: Record<string, string> = {
  critical: 'bg-red-500/15 text-red-400',
  high:     'bg-orange-500/15 text-orange-400',
  medium:   'bg-yellow-500/15 text-yellow-400',
  low:      'bg-blue-500/15 text-blue-400',
  fatal:    'bg-red-600/15 text-red-400',
  error:    'bg-red-500/15 text-red-400',
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// Char-by-char fuzzy: returns score > 0 only when all needle chars appear
// in order in haystack. Consecutive runs score higher.
function fuzzyScore(haystack: string, needle: string): number {
  const h = haystack.toLowerCase(); const n = needle.toLowerCase()
  let hi = 0; let ni = 0; let score = 0; let run = 0
  while (hi < h.length && ni < n.length) {
    if (h[hi] === n[ni]) { score += 1 + run * 2; run++; ni++ } else run = 0
    hi++
  }
  return ni === n.length ? score : 0
}

// Parse prefix-typed searches: "pod:nginx" → { type: 'pod', query: 'nginx' }
type ResourcePrefix = 'pod' | 'ing' | 'svc' | 'dep' | 'node' | 'ns' | null
function parsePrefix(q: string): { type: ResourcePrefix; query: string } {
  const m = q.match(/^(pod|ing|ingress|svc|service|dep|deploy|node|ns|namespace):(.*)/)
  if (!m) return { type: null, query: q }
  const aliases: Record<string, ResourcePrefix> = {
    pod: 'pod', ing: 'ing', ingress: 'ing', svc: 'svc', service: 'svc',
    dep: 'dep', deploy: 'dep', node: 'node', ns: 'ns', namespace: 'ns',
  }
  return { type: aliases[m[1]] ?? null, query: m[2].trim() }
}

// Build an href for a K8s resource
function k8sHref(kind: string, ns: string | undefined, name: string): string {
  const bases: Record<string, { path: string; group: string; version: string }> = {
    pod:       { path: 'pods',       group: 'core',                  version: 'v1'  },
    deployment:{ path: 'deployments',group: 'apps',                  version: 'v1'  },
    service:   { path: 'services',   group: 'core',                  version: 'v1'  },
    ingress:   { path: 'ingresses',  group: 'networking.k8s.io',     version: 'v1'  },
    node:      { path: 'nodes',      group: 'core',                  version: 'v1'  },
  }
  const b = bases[kind]
  if (!b) return '/'
  if (kind === 'node') return `/workloads/${b.path}/cluster/${name}?group=${b.group}&version=${b.version}`
  return `/workloads/${b.path}/${ns || 'default'}/${name}?group=${b.group}&version=${b.version}`
}

function statusDot(status: string | undefined) {
  if (!status) return 'bg-slate-400'
  return STATUS_DOT[status] ?? (status === 'Running' ? 'bg-green-500' : 'bg-slate-400')
}

// ─── component ────────────────────────────────────────────────────────────────

export function GlobalSearch() {
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [loading, setLoading] = useState(false)
  const [selectedIdx, setSelectedIdx] = useState(0)
  const [recentSearches, setRecentSearches] = useState<string[]>([])
  // Cached namespace list to avoid re-fetching on every keypress
  const [cachedNamespaces, setCachedNamespaces] = useState<string[]>([])
  const inputRef = useRef<HTMLInputElement>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => {
    try {
      const s = localStorage.getItem('ci_recent_searches')
      if (s) setRecentSearches(JSON.parse(s))
    } catch { /* ignore */ }
  }, [])

  // Pre-fetch namespace list once when component mounts (so search is instant)
  useEffect(() => {
    fetch(`${getApiUrl()}/api/v1/k8s/namespaces`)
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        const list: string[] = Array.isArray(data) ? data : (data?.namespaces ?? [])
        setCachedNamespaces(list)
      })
      .catch(() => {})
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') { e.preventDefault(); setOpen(o => !o) }
      if (e.key === 'Escape') setOpen(false)
    }
    const onEvent = () => setOpen(true)
    window.addEventListener('keydown', onKey)
    window.addEventListener('open-global-search', onEvent)
    return () => { window.removeEventListener('keydown', onKey); window.removeEventListener('open-global-search', onEvent) }
  }, [])

  useEffect(() => {
    if (open) { setTimeout(() => inputRef.current?.focus(), 30); setQuery(''); setResults([]); setSelectedIdx(0) }
  }, [open])

  const doSearch = useCallback(async (q: string) => {
    if (!q.trim()) { setResults([]); setLoading(false); return }
    abortRef.current?.abort()
    abortRef.current = new AbortController()
    const { signal } = abortRef.current
    setLoading(true)

    const { type: prefixType, query: cleanQ } = parsePrefix(q)

    // Which K8s resource types to fetch — scoped by prefix if given
    type K8sTarget = { kind: string; path: string; group: string; version: string; cat: SearchResult['category'] }
    const allK8sTargets: K8sTarget[] = [
      { kind: 'pod',       path: 'pods',        group: 'core',              version: 'v1', cat: 'pod'        },
      { kind: 'deployment',path: 'deployments', group: 'apps',              version: 'v1', cat: 'deployment' },
      { kind: 'service',   path: 'services',    group: 'core',              version: 'v1', cat: 'service'    },
      { kind: 'ingress',   path: 'ingresses',   group: 'networking.k8s.io', version: 'v1', cat: 'ingress'    },
      { kind: 'node',      path: 'nodes',       group: 'core',              version: 'v1', cat: 'node'       },
    ]

    const prefixMap: Record<NonNullable<ResourcePrefix>, string> = {
      pod: 'pod', ing: 'ingress', svc: 'service', dep: 'deployment', node: 'node', ns: 'pod',
    }
    const k8sTargets = prefixType
      ? allK8sTargets.filter(t => t.kind === (prefixMap[prefixType] ?? t.kind))
      : allK8sTargets

    // Pick a namespace: use cached list (prefer "default"), fallback to "default"
    const ns = cachedNamespaces.includes('default')
      ? 'default'
      : (cachedNamespaces[0] ?? 'default')

    const k8sFetches = k8sTargets.map(t => {
      const url = t.kind === 'node'
        ? `${getApiUrl()}/api/v1/k8s/cluster/${t.group}/${t.version}/${t.path}?search=${encodeURIComponent(cleanQ)}&limit=4`
        : `${getApiUrl()}/api/v1/k8s/ns/${ns}/${t.group}/${t.version}/${t.path}?search=${encodeURIComponent(cleanQ)}&limit=4`
      return fetch(url, { signal })
        .then(r => r.ok ? r.json() : null)
        .then(data => ({ target: t, items: (data?.items ?? data ?? []) as K8sItem[] }))
        .catch(() => ({ target: t, items: [] as K8sItem[] }))
    })

    try {
      const [errorsRes, incidentsRes, ...k8sResults] = await Promise.all([
        fetch(`${getApiUrl()}/api/v1/errors/groups?search=${encodeURIComponent(cleanQ)}&limit=5&status=open`, { signal })
          .then(r => r.ok ? r.json() : null).catch(() => null),
        fetch(`${getApiUrl()}/api/v1/incidents?limit=100`, { signal })
          .then(r => r.ok ? r.json() : null).catch(() => null),
        ...k8sFetches,
      ])

      if (signal.aborted) return
      const out: SearchResult[] = []

      // K8s resources (pods, deployments, services, ingresses, nodes)
      for (const { target, items } of k8sResults) {
        const scored = (items as K8sItem[])
          .map(item => ({ item, score: fuzzyScore(item.name, cleanQ) }))
          .filter(x => x.score > 0 || cleanQ.length < 2)
          .sort((a, b) => b.score - a.score)
          .slice(0, 3)
        for (const { item } of scored) {
          const phase = item.status?.phase ?? item.status?.state ?? (item.status?.ready ? 'Ready' : undefined)
          out.push({
            id: `${target.kind}-${item.namespace}-${item.name}`,
            category: target.cat,
            title: item.name,
            subtitle: item.namespace ? `${item.namespace} · ${target.kind}` : target.kind,
            href: k8sHref(target.kind, item.namespace, item.name),
            status: phase,
          })
        }
      }

      // Error groups (backend search)
      if (errorsRes?.groups) {
        for (const g of (errorsRes.groups as ErrorGroup[]).slice(0, 4)) {
          out.push({
            id: `err-${g.id}`,
            category: 'error',
            title: g.title,
            subtitle: `${g.namespace} · ${g.service} · ${g.count} occurrences`,
            href: `/errors?search=${encodeURIComponent(cleanQ)}`,
            badge: g.level,
            badgeSeverity: g.level,
          })
        }
      }

      // Incidents (client-side fuzzy)
      if (incidentsRes) {
        const list: Incident[] = Array.isArray(incidentsRes) ? incidentsRes : (incidentsRes?.incidents ?? [])
        const scored = list
          .map(i => ({ i, score: fuzzyScore(i.title || '', cleanQ) + fuzzyScore(i.severity || '', cleanQ) }))
          .filter(x => x.score > 0)
          .sort((a, b) => b.score - a.score)
          .slice(0, 3)
        for (const { i } of scored) {
          out.push({
            id: `inc-${i.id}`, category: 'incident',
            title: i.title, subtitle: `${i.severity} · ${i.status}`,
            href: `/incidents/${i.id}`, badge: i.severity, badgeSeverity: i.severity,
          })
        }
      }

      // Namespace fuzzy filter (cached — instant)
      if (!prefixType || prefixType === 'ns') {
        const nsMatches = cachedNamespaces
          .map(ns => ({ ns, score: fuzzyScore(ns, cleanQ) }))
          .filter(x => x.score > 0)
          .sort((a, b) => b.score - a.score)
          .slice(0, 2)
        for (const { ns } of nsMatches) {
          out.push({
            id: `ns-${ns}`, category: 'namespace',
            title: ns, subtitle: 'Namespace — view pods',
            href: `/workloads/pods?group=core&version=v1&namespace=${encodeURIComponent(ns)}`,
          })
        }
      }

      setResults(out)
      setSelectedIdx(0)
    } catch (err: unknown) {
      if ((err as Error)?.name !== 'AbortError') console.error('Search error:', err)
    } finally {
      if (!signal.aborted) setLoading(false)
    }
  }, [cachedNamespaces])

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const q = e.target.value
    setQuery(q)
    setSelectedIdx(0)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    if (!q.trim()) { setResults([]); setLoading(false); return }
    setLoading(true)
    debounceRef.current = setTimeout(() => doSearch(q), 300)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setSelectedIdx(i => Math.min(i + 1, results.length - 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setSelectedIdx(i => Math.max(i - 1, 0)) }
    else if (e.key === 'Enter') { e.preventDefault(); if (results[selectedIdx]) go(results[selectedIdx].href) }
  }

  const go = (href: string) => {
    if (query.trim()) {
      const updated = [query, ...recentSearches.filter(s => s !== query)].slice(0, 6)
      setRecentSearches(updated)
      try { localStorage.setItem('ci_recent_searches', JSON.stringify(updated)) } catch { /* ignore */ }
    }
    setOpen(false)
    router.push(href)
  }

  const grouped = results.reduce<Record<string, SearchResult[]>>((acc, r) => {
    if (!acc[r.category]) acc[r.category] = []
    acc[r.category].push(r)
    return acc
  }, {})

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-[200] flex items-start justify-center pt-[10vh] px-4"
      onMouseDown={() => setOpen(false)}
    >
      <div className="absolute inset-0 bg-black/65 backdrop-blur-sm" />

      <div
        className="relative w-full max-w-[680px] rounded-2xl overflow-hidden shadow-2xl border border-cluster-border bg-cluster-card"
        style={{ boxShadow: '0 32px 64px rgba(0,0,0,0.4)' }}
        onMouseDown={e => e.stopPropagation()}
      >
        {/* Input */}
        <div className="flex items-center gap-3 px-4 h-14 border-b border-cluster-border">
          {loading
            ? <Loader2 className="w-5 h-5 text-cluster-muted animate-spin flex-shrink-0" />
            : <Search className="w-5 h-5 text-cluster-muted flex-shrink-0" />}
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={handleChange}
            onKeyDown={handleKeyDown}
            placeholder="Search pods, ingresses, services, events, errors… or type pod: ing: svc:"
            className="flex-1 bg-transparent text-cluster-text placeholder:text-cluster-muted/50 text-[14px] outline-none"
          />
          {query
            ? <button onClick={() => { setQuery(''); setResults([]); inputRef.current?.focus() }}
                className="text-cluster-muted hover:text-cluster-text transition-colors flex-shrink-0">
                <X className="w-4 h-4" />
              </button>
            : <kbd className="hidden sm:inline text-cluster-muted/60 text-xs font-mono px-1.5 py-0.5 rounded border border-cluster-border">Esc</kbd>
          }
        </div>

        {/* Body */}
        <div className="max-h-[58vh] overflow-y-auto overscroll-contain">
          {!query ? (
            <div className="py-2 px-2">
              {/* Smart operational searches */}
              <div className="px-3 pt-2 pb-1.5 text-[11px] font-semibold text-cluster-muted uppercase tracking-widest">
                Quick Operations
              </div>
              <div className="grid grid-cols-2 gap-1 mb-2">
                {SMART_SEARCHES.map((s, i) => (
                  <button
                    key={i}
                    onClick={() => { setOpen(false); router.push(s.href) }}
                    className="flex items-center gap-2 px-3 py-2 rounded-lg text-sm text-cluster-muted hover:bg-cluster-border/40 hover:text-cluster-text transition-colors text-left"
                  >
                    <s.icon className={`w-3.5 h-3.5 flex-shrink-0 ${s.color}`} />
                    <span className="truncate">{s.label}</span>
                  </button>
                ))}
              </div>

              {recentSearches.length > 0 && (
                <div className="mb-1">
                  <div className="mx-3 my-2 border-t border-cluster-border/60" />
                  <div className="px-3 pb-1.5 text-[11px] font-semibold text-cluster-muted uppercase tracking-widest">Recent</div>
                  {recentSearches.map((s, i) => (
                    <button
                      key={i}
                      onClick={() => { setQuery(s); doSearch(s) }}
                      className="flex items-center gap-2.5 w-full px-3 py-2 text-sm text-cluster-muted hover:bg-cluster-border/40 hover:text-cluster-text rounded-lg transition-colors"
                    >
                      <Clock className="w-3.5 h-3.5 flex-shrink-0" />
                      <span className="truncate">{s}</span>
                    </button>
                  ))}
                </div>
              )}

              <div className="mx-3 my-2 border-t border-cluster-border/60" />
              <div className="px-3 pb-1.5 text-[11px] font-semibold text-cluster-muted uppercase tracking-widest">Pages</div>
              {QUICK_LINKS.map((link, i) => (
                <button
                  key={link.href}
                  onClick={() => go(link.href)}
                  className={`flex items-center gap-3 w-full px-3 py-2.5 rounded-lg text-sm transition-colors ${
                    i === selectedIdx ? 'bg-blue-600/15 text-cluster-text' : 'text-cluster-muted hover:bg-cluster-border/40 hover:text-cluster-text'
                  }`}
                >
                  <link.icon className="w-4 h-4 flex-shrink-0" />
                  <span className="flex-1 text-left font-medium">{link.title}</span>
                  <span className="text-xs text-cluster-muted/60">{link.sub}</span>
                </button>
              ))}
            </div>
          ) : results.length === 0 && !loading ? (
            <div className="py-12 text-center">
              <p className="text-sm text-cluster-muted">No results for <span className="text-cluster-text font-medium">"{query}"</span></p>
              <p className="text-xs text-cluster-muted/60 mt-1">
                Try <code className="font-mono">pod:nginx</code> · <code className="font-mono">ing:app</code> · <code className="font-mono">svc:frontend</code>
              </p>
            </div>
          ) : (
            <div className="py-2 px-2">
              {(Object.entries(grouped) as [keyof typeof CATEGORY_META, SearchResult[]][]).map(([cat, items]) => {
                const meta = CATEGORY_META[cat]
                return (
                  <div key={cat} className="mb-1">
                    <div className="px-3 pt-2 pb-1 text-[11px] font-semibold text-cluster-muted uppercase tracking-widest">{meta.label}</div>
                    {items.map(item => {
                      const idx = results.indexOf(item)
                      return (
                        <button
                          key={item.id}
                          onClick={() => go(item.href)}
                          className={`flex items-center gap-3 w-full px-3 py-2.5 rounded-lg text-sm transition-colors ${
                            idx === selectedIdx ? 'bg-blue-600/15 text-cluster-text' : 'hover:bg-cluster-border/40'
                          }`}
                        >
                          {/* Status dot (for K8s resources) or category dot */}
                          <span className={`w-2 h-2 rounded-full flex-shrink-0 ${
                            item.status ? statusDot(item.status) : meta.dot
                          }`} />
                          <div className="flex-1 min-w-0 text-left">
                            <p className="font-mono text-[13px] font-medium text-cluster-text truncate">{item.title}</p>
                            <p className="text-xs text-cluster-muted/80 truncate mt-0.5">{item.subtitle}</p>
                          </div>
                          {item.status && (
                            <span className="text-[10px] text-cluster-muted flex-shrink-0">{item.status}</span>
                          )}
                          {item.badge && !item.status && (
                            <span className={`text-[10px] font-semibold px-1.5 py-0.5 rounded-full uppercase flex-shrink-0 ${SEVERITY_BADGE[item.badgeSeverity || ''] || 'bg-cluster-border text-cluster-muted'}`}>
                              {item.badge}
                            </span>
                          )}
                          <ChevronRight className="w-3.5 h-3.5 text-cluster-muted/40 flex-shrink-0" />
                        </button>
                      )
                    })}
                  </div>
                )
              })}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="border-t border-cluster-border bg-cluster-bg/30 px-4 py-2 flex items-center gap-4">
          <button
            onClick={() => go(`/workloads/pods?search=${encodeURIComponent(query)}&group=core&version=v1`)}
            className="flex items-center gap-1.5 text-xs text-cluster-muted hover:text-cluster-text transition-colors"
          >
            <Boxes className="w-3.5 h-3.5" />
            {query ? <>Search all pods for &ldquo;{query}&rdquo;</> : 'Browse pods'}
            <ArrowRight className="w-3 h-3" />
          </button>
          <button
            onClick={() => go(`/workloads/ingresses?search=${encodeURIComponent(query)}&group=networking.k8s.io&version=v1`)}
            className="flex items-center gap-1.5 text-xs text-cluster-muted hover:text-cluster-text transition-colors"
          >
            <Network className="w-3.5 h-3.5" />
            {query ? <>Ingresses</> : 'Browse ingresses'}
            <ArrowRight className="w-3 h-3" />
          </button>
          <div className="ml-auto flex items-center gap-3 text-[11px] text-cluster-muted/50 font-mono">
            <span>↑↓</span><span>↵ open</span><span>esc</span>
          </div>
        </div>
      </div>
    </div>
  )
}
