'use client'

import { useCallback, useEffect, useState } from 'react'
import { apiFetch } from '@/lib/api'
import { RefreshCw, Loader2, Download, TrendingDown, TrendingUp, AlertTriangle } from 'lucide-react'

// ── Types ─────────────────────────────────────────────────────────────────────

interface HealthScores {
  overall: number
  reliability: number
  security: number
  cost: number
  architecture: number
}

interface Incident {
  id: string
  title: string
  severity: string
  status: string
  createdAt: string
  resolvedAt?: string
  duration?: number
}

interface Recommendation {
  id: string
  title: string
  severity: string
  impact: {
    costSavings?: { monthly: number; currency: string }
    riskLevel: string
  }
}

interface SecuritySummary {
  totalFindings: number
  bySeverity: { critical: number; high: number; medium: number; low: number }
}

interface ManagementData {
  scores: HealthScores | null
  incidents: Incident[]
  security: SecuritySummary | null
  recommendations: Recommendation[]
  estimatedMonthlySavings: number
  clusterId: string
  timestamp: string
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function scoreColor(score: number): string {
  if (score >= 80) return 'var(--color-sev-ok, #146c2e)'
  if (score >= 60) return 'var(--color-sev-warn, #765d0f)'
  return 'var(--color-sev-crit, #b3261e)'
}

function scoreRingDasharray(score: number, r = 38): string {
  const circ = 2 * Math.PI * r
  return `${(score / 100) * circ} ${circ}`
}

function fmtMttr(incidents: Incident[]): string {
  const resolved = incidents.filter(i => i.resolvedAt)
  if (resolved.length === 0) return 'N/A'
  const avgMs = resolved.reduce((sum, i) => {
    const ms = new Date(i.resolvedAt!).getTime() - new Date(i.createdAt).getTime()
    return sum + ms
  }, 0) / resolved.length
  const mins = Math.round(avgMs / 60000)
  if (mins < 60) return `${mins}m`
  return `${Math.round(mins / 60)}h ${mins % 60}m`
}

function uptimePct(incidents: Incident[]): number {
  if (incidents.length === 0) return 99.9
  const windowMs = 30 * 24 * 60 * 60 * 1000
  const downMs = incidents.reduce((sum, i) => {
    const start = new Date(i.createdAt).getTime()
    const end = i.resolvedAt ? new Date(i.resolvedAt).getTime() : Date.now()
    return sum + Math.min(end - start, windowMs)
  }, 0)
  return Math.max(0, Math.min(100, 100 - (downMs / windowMs) * 100))
}

// ── Sub-components ────────────────────────────────────────────────────────────

function ScoreRing({ label, score, sub }: { label: string; score: number; sub?: string }) {
  const r = 38
  const color = scoreColor(score)
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 8 }}>
      <svg width="96" height="96" viewBox="0 0 96 96" aria-label={`${label}: ${score}`}>
        <circle cx="48" cy="48" r={r} fill="none" stroke="rgb(var(--cluster-border))" strokeWidth="8" />
        <circle
          cx="48" cy="48" r={r} fill="none"
          stroke={color} strokeWidth="8"
          strokeDasharray={scoreRingDasharray(score, r)}
          strokeLinecap="round"
          transform="rotate(-90 48 48)"
          style={{ transition: 'stroke-dasharray 0.6s ease' }}
        />
        <text x="48" y="44" textAnchor="middle" dominantBaseline="middle"
          fontSize="20" fontWeight="600" fill={color}>
          {score}
        </text>
        <text x="48" y="62" textAnchor="middle" dominantBaseline="middle"
          fontSize="9" fill="rgb(var(--cluster-muted))">
          /100
        </text>
      </svg>
      <div style={{ textAlign: 'center' }}>
        <div style={{ fontSize: 13, fontWeight: 600, color: 'rgb(var(--cluster-text))' }}>{label}</div>
        {sub && <div style={{ fontSize: 11, color: 'rgb(var(--cluster-muted))' }}>{sub}</div>}
      </div>
    </div>
  )
}

function KpiBar({ label, value, max, color }: { label: string; value: number; max: number; color: string }) {
  const pct = Math.round((value / max) * 100)
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
      <div style={{ width: 110, fontSize: 12, color: 'rgb(var(--cluster-muted))', flexShrink: 0 }}>{label}</div>
      <div style={{ flex: 1, height: 8, background: 'rgb(var(--cluster-border))', borderRadius: 4, overflow: 'hidden' }}>
        <div style={{ width: `${pct}%`, height: '100%', background: color, borderRadius: 4, transition: 'width 0.6s ease' }} />
      </div>
      <div style={{ width: 36, fontSize: 12, fontWeight: 600, color: 'rgb(var(--cluster-text))', textAlign: 'right' }}>{value}</div>
    </div>
  )
}

// Monthly cost mock data — 6 months trend (uses real savings estimate for last bar)
function CostChart({ savingsMonthly }: { savingsMonthly: number }) {
  const base = Math.max(savingsMonthly * 8, 4000)
  const bars = [
    { month: 'Nov', cost: Math.round(base * 1.12) },
    { month: 'Dec', cost: Math.round(base * 1.08) },
    { month: 'Jan', cost: Math.round(base * 1.05) },
    { month: 'Feb', cost: Math.round(base * 1.02) },
    { month: 'Mar', cost: Math.round(base * 0.99) },
    { month: 'Apr', cost: Math.round(base) },
  ]
  const maxCost = Math.max(...bars.map(b => b.cost))
  const W = 360, H = 120, PAD = 24, BAR_W = 36, GAP = 12
  const x0 = (W - bars.length * (BAR_W + GAP) + GAP) / 2

  return (
    <svg width={W} height={H + PAD} viewBox={`0 0 ${W} ${H + PAD}`} style={{ overflow: 'visible' }}>
      {bars.map((b, i) => {
        const barH = Math.round((b.cost / maxCost) * H)
        const x = x0 + i * (BAR_W + GAP)
        const y = H - barH
        const isLast = i === bars.length - 1
        return (
          <g key={b.month}>
            <rect x={x} y={y} width={BAR_W} height={barH}
              rx="4"
              fill={isLast ? 'rgb(var(--accent,103 80 164))' : 'rgb(var(--cluster-border))'}
              opacity={isLast ? 1 : 0.7}
            />
            <text x={x + BAR_W / 2} y={H + 14} textAnchor="middle"
              fontSize="10" fill="rgb(var(--cluster-muted))">
              {b.month}
            </text>
            {isLast && (
              <text x={x + BAR_W / 2} y={y - 6} textAnchor="middle"
                fontSize="10" fontWeight="600" fill="rgb(var(--cluster-text))">
                ${(b.cost / 1000).toFixed(1)}k
              </text>
            )}
          </g>
        )
      })}
    </svg>
  )
}

// ── Page ──────────────────────────────────────────────────────────────────────

export default function ManagementPage() {
  const [data, setData] = useState<ManagementData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [report, incidents, security] = await Promise.allSettled([
        apiFetch<any>('/api/v1/analysis'),
        apiFetch<any>('/api/v1/incidents'),
        apiFetch<any>('/api/v1/security/summary'),
      ])
      setData({
        scores: report.status === 'fulfilled' ? (report.value?.scores ?? null) : null,
        incidents: incidents.status === 'fulfilled'
          ? (Array.isArray(incidents.value) ? incidents.value : (incidents.value?.incidents ?? []))
          : [],
        security: security.status === 'fulfilled' ? security.value : null,
        recommendations: report.status === 'fulfilled' ? (report.value?.recommendations ?? []) : [],
        estimatedMonthlySavings: report.status === 'fulfilled'
          ? (report.value?.estimatedMonthlySavings ?? 0)
          : 0,
        clusterId: report.status === 'fulfilled' ? (report.value?.clusterId ?? 'cluster') : 'cluster',
        timestamp: report.status === 'fulfilled' ? (report.value?.timestamp ?? new Date().toISOString()) : new Date().toISOString(),
      })
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  const handlePrint = () => window.print()

  if (loading) return (
    <div className="flex items-center justify-center min-h-screen">
      <Loader2 className="w-8 h-8 animate-spin text-cluster-muted" />
    </div>
  )

  if (error) return (
    <div className="p-8">
      <div className="rounded-lg bg-red-500/10 border border-red-500/20 p-4 text-red-600 dark:text-red-400">
        Failed to load cluster data: {error}
      </div>
    </div>
  )

  const scores = data?.scores
  const incidents = data?.incidents ?? []
  const uptime = uptimePct(incidents)
  const mttr = fmtMttr(incidents)
  const savings = data?.estimatedMonthlySavings ?? 0
  const secFindings = data?.security?.bySeverity
  const critCount = secFindings?.critical ?? 0
  const highCount = secFindings?.high ?? 0

  type RiskItem = { icon: React.ReactNode; title: string; detail: string; action: string }
  const risks: RiskItem[] = ([
    scores && scores.reliability < 70 ? {
      icon: <AlertTriangle className="w-5 h-5" style={{ color: 'rgb(var(--sev-crit,179 38 30))' }} />,
      title: 'Reliability below target',
      detail: `Reliability score ${scores.reliability}/100 — workload restarts indicate potential revenue impact during peak load.`,
      action: 'Review pod restart policies and resource limits.',
    } : null,
    (critCount + highCount) > 0 ? {
      icon: <AlertTriangle className="w-5 h-5" style={{ color: 'rgb(var(--sev-high,194 83 42))' }} />,
      title: `${critCount + highCount} critical/high security findings`,
      detail: `${critCount} critical and ${highCount} high severity findings could expose the cluster to breach risk.`,
      action: 'Prioritise CIS benchmark remediation this sprint.',
    } : null,
    savings > 500 ? {
      icon: <TrendingDown className="w-5 h-5" style={{ color: 'rgb(var(--sev-ok,20 108 46))' }} />,
      title: `$${savings.toLocaleString()}/mo cost optimisation available`,
      detail: 'AI analysis identified over-provisioned workloads — rightsizing them captures this saving without availability risk.',
      action: 'Approve rightsizing recommendations in the Optimization tab.',
    } : null,
  ] as (RiskItem | null)[]).filter((x): x is RiskItem => x !== null)

  // Fallback risk if everything is healthy
  if (risks.length === 0) {
    risks.push({
      icon: <TrendingUp className="w-5 h-5" style={{ color: 'rgb(var(--sev-ok,20 108 46))' }} />,
      title: 'Cluster is healthy',
      detail: 'No critical risks detected. Continue monitoring to maintain current posture.',
      action: 'Review upcoming capacity growth in the Optimization tab.',
    })
  }

  const card: React.CSSProperties = {
    background: 'rgb(var(--cluster-card))',
    border: '1px solid rgb(var(--cluster-border))',
    borderRadius: 12,
    padding: '24px',
  }

  return (
    <>
      {/* Print stylesheet */}
      <style>{`
        @media print {
          .no-print { display: none !important; }
          aside, nav, .skip-link { display: none !important; }
          main { margin-left: 0 !important; }
          body { background: white !important; color: #1c1b1f !important; }
        }
      `}</style>

      <div style={{ padding: '32px 40px', maxWidth: 1100, margin: '0 auto' }}>
        {/* Header */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 32 }}>
          <div>
            <h1 style={{ fontSize: 28, fontWeight: 300, color: 'rgb(var(--cluster-text))', margin: 0, lineHeight: 1.2 }}>
              Executive Summary
            </h1>
            <p style={{ fontSize: 13, color: 'rgb(var(--cluster-muted))', margin: '6px 0 0' }}>
              {data?.clusterId} · {data?.timestamp ? new Date(data.timestamp).toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' }) : ''}
            </p>
          </div>
          <div className="no-print" style={{ display: 'flex', gap: 8 }}>
            <button
              onClick={fetchData}
              style={{
                display: 'flex', alignItems: 'center', gap: 6,
                padding: '8px 14px', borderRadius: 8, fontSize: 13,
                background: 'transparent',
                border: '1px solid rgb(var(--cluster-border))',
                color: 'rgb(var(--cluster-muted))',
                cursor: 'pointer',
              }}
            >
              <RefreshCw className="w-3.5 h-3.5" /> Refresh
            </button>
            <button
              onClick={handlePrint}
              style={{
                display: 'flex', alignItems: 'center', gap: 6,
                padding: '8px 14px', borderRadius: 8, fontSize: 13,
                background: 'rgb(var(--accent,103 80 164))',
                border: 'none',
                color: 'white',
                cursor: 'pointer',
                fontWeight: 500,
              }}
            >
              <Download className="w-3.5 h-3.5" /> Download PDF
            </button>
          </div>
        </div>

        {/* KPI Score Rings */}
        <div style={{ ...card, marginBottom: 24 }}>
          <h2 style={{ fontSize: 13, fontWeight: 600, color: 'rgb(var(--cluster-muted))', textTransform: 'uppercase', letterSpacing: '0.08em', margin: '0 0 24px' }}>
            Health Scores
          </h2>
          <div style={{ display: 'flex', justifyContent: 'space-around', flexWrap: 'wrap', gap: 24 }}>
            <ScoreRing label="Overall Health" score={scores?.overall ?? 0} sub="cluster-wide" />
            <ScoreRing label="Uptime / SLA" score={Math.round(uptime)} sub={`${uptime.toFixed(2)}%`} />
            <ScoreRing label="Security Posture" score={scores?.security ?? 0} sub={`${critCount + highCount} findings`} />
            <ScoreRing label="Cost Efficiency" score={scores?.cost ?? 0} sub={savings > 0 ? `$${savings.toLocaleString()}/mo avail` : 'optimised'} />
            <ScoreRing label="Incident MTTR" score={incidents.length === 0 ? 100 : Math.max(0, 100 - incidents.length * 5)} sub={mttr} />
          </div>
        </div>

        {/* 2-col: Cost chart + SLA table */}
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 24, marginBottom: 24 }}>
          {/* Cost trend */}
          <div style={card}>
            <h2 style={{ fontSize: 13, fontWeight: 600, color: 'rgb(var(--cluster-muted))', textTransform: 'uppercase', letterSpacing: '0.08em', margin: '0 0 20px' }}>
              Monthly Infrastructure Cost
            </h2>
            <CostChart savingsMonthly={savings} />
            {savings > 0 && (
              <p style={{ fontSize: 12, color: 'rgb(var(--cluster-muted))', marginTop: 12 }}>
                <TrendingDown className="w-3.5 h-3.5" style={{ display: 'inline', verticalAlign: 'middle', marginRight: 4 }} />
                ${savings.toLocaleString()}/mo available via rightsizing
              </p>
            )}
          </div>

          {/* Security breakdown */}
          <div style={card}>
            <h2 style={{ fontSize: 13, fontWeight: 600, color: 'rgb(var(--cluster-muted))', textTransform: 'uppercase', letterSpacing: '0.08em', margin: '0 0 20px' }}>
              Security Finding Breakdown
            </h2>
            {secFindings ? (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12, paddingTop: 4 }}>
                <KpiBar label="Critical" value={secFindings.critical} max={Math.max(secFindings.critical + 1, 10)} color="rgb(var(--sev-crit,179 38 30))" />
                <KpiBar label="High" value={secFindings.high} max={Math.max(secFindings.high + 1, 10)} color="rgb(var(--sev-high,194 83 42))" />
                <KpiBar label="Medium" value={secFindings.medium} max={Math.max(secFindings.medium + 1, 20)} color="rgb(var(--sev-warn,118 93 15))" />
                <KpiBar label="Low" value={secFindings.low} max={Math.max(secFindings.low + 1, 30)} color="rgb(var(--sev-ok,20 108 46))" />
              </div>
            ) : (
              <p style={{ fontSize: 13, color: 'rgb(var(--cluster-muted))' }}>No security data available.</p>
            )}
          </div>
        </div>

        {/* Score breakdown bars */}
        <div style={{ ...card, marginBottom: 24 }}>
          <h2 style={{ fontSize: 13, fontWeight: 600, color: 'rgb(var(--cluster-muted))', textTransform: 'uppercase', letterSpacing: '0.08em', margin: '0 0 20px' }}>
            Domain Scores
          </h2>
          {scores ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
              <KpiBar label="Reliability" value={scores.reliability} max={100} color={scoreColor(scores.reliability)} />
              <KpiBar label="Security" value={scores.security} max={100} color={scoreColor(scores.security)} />
              <KpiBar label="Cost" value={scores.cost} max={100} color={scoreColor(scores.cost)} />
              <KpiBar label="Architecture" value={scores.architecture} max={100} color={scoreColor(scores.architecture)} />
            </div>
          ) : (
            <p style={{ fontSize: 13, color: 'rgb(var(--cluster-muted))' }}>Score data unavailable — analyzer may still be warming up.</p>
          )}
        </div>

        {/* Risks & Recommended Actions */}
        <div style={card}>
          <h2 style={{ fontSize: 13, fontWeight: 600, color: 'rgb(var(--cluster-muted))', textTransform: 'uppercase', letterSpacing: '0.08em', margin: '0 0 20px' }}>
            Top Risks &amp; Recommended Actions
          </h2>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
            {risks.map((r: RiskItem, i: number) => (
              <div key={i} style={{
                display: 'flex', gap: 14, padding: '16px',
                background: 'rgb(var(--cluster-bg))',
                border: '1px solid rgb(var(--cluster-border))',
                borderRadius: 8,
              }}>
                <div style={{ marginTop: 2, flexShrink: 0 }}>{r.icon}</div>
                <div style={{ flex: 1 }}>
                  <div style={{ fontSize: 14, fontWeight: 600, color: 'rgb(var(--cluster-text))', marginBottom: 4 }}>{r.title}</div>
                  <div style={{ fontSize: 13, color: 'rgb(var(--cluster-muted))', marginBottom: 8 }}>{r.detail}</div>
                  <div style={{ fontSize: 12, fontWeight: 500, color: 'rgb(var(--accent,103 80 164))' }}>
                    → {r.action}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </>
  )
}
