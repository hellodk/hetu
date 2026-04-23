'use client'

import { useState, useEffect } from 'react'
import { Server, Activity, AlertTriangle, CheckCircle } from 'lucide-react'
import clsx from 'clsx'

// API URL: read from runtime config injected by server layout, or fall back to build-time env
const getApiUrl = () =>
    typeof window !== 'undefined'
        ? ((window as any).__CLUSTER_INTEL_API__ || '')
        : (process.env.NEXT_PUBLIC_API_URL || '')

// Matches the analyzer's DNSHealth response shape
interface DNSHealthData {
    requestsPerSecond?: number
    errorRate?: number
    avgLatencyMs?: number
    p99LatencyMs?: number
    cacheHitRate?: number
    nxdomainRate?: number
    servfailRate?: number
    forwardHealthy?: boolean
}

export function CoreDNSHealth() {
    const [data, setData] = useState<DNSHealthData | null>(null)
    const [loading, setLoading] = useState(true)

    useEffect(() => {
        const fetchDNS = async () => {
            try {
                const res = await fetch(`${getApiUrl()}/api/v1/dns/health`)
                if (res.ok) {
                    const json = await res.json()
                    setData(json)
                }
            } catch (err) {
                console.error("Failed to fetch DNS health", err)
            } finally {
                setLoading(false)
            }
        }

        fetchDNS()
        const interval = setInterval(fetchDNS, 30000)
        return () => clearInterval(interval)
    }, [])

    if (loading && !data) {
        return (
            <section className="bg-cluster-card rounded-xl border border-cluster-border p-4 sm:p-6 animate-pulse" aria-labelledby="dns-heading">
                <div className="h-6 w-1/3 bg-cluster-border rounded mb-6"></div>
                <div className="h-16 bg-cluster-border rounded"></div>
            </section>
        )
    }

    // Defensive normalization: API may omit fields, derive status from forwardHealthy + errorRate
    const requestRate = data?.requestsPerSecond ?? 0
    const errorRate = data?.errorRate ?? 0
    const latencyP99 = data?.p99LatencyMs ?? 0
    const cacheHitRate = data?.cacheHitRate ?? 0
    const forwardHealthy = data?.forwardHealthy ?? false
    const derivedStatus = forwardHealthy && errorRate < 1 ? 'Healthy' : (data ? 'Degraded' : 'Unknown')

    const safeData = {
        status: derivedStatus,
        requestRate,
        errorRate,
        latencyP99,
        cacheHitRate,
    }

    const isHealthy = safeData.status.toLowerCase() === 'healthy'
    const StatusIcon = isHealthy ? CheckCircle : AlertTriangle
    const statusColor = isHealthy ? 'text-green-400' : 'text-red-400'   // remapped to sev tokens in light themes
    const statusBg = isHealthy ? 'bg-green-500/10 border-green-500/20' : 'bg-red-500/10 border-red-500/20'

    return (
        <section className="bg-cluster-card rounded-xl border border-cluster-border p-4 sm:p-6" aria-labelledby="dns-heading">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 mb-6">
                <h2 id="dns-heading" className="text-lg font-semibold flex items-center gap-2">
                    <Server className="w-5 h-5 text-indigo-500" aria-hidden="true" />
                    CoreDNS Health
                </h2>

                <div className={clsx('flex items-center gap-1.5 px-3 py-1 rounded-full border', statusBg)}>
                    <StatusIcon className={clsx('w-4 h-4', statusColor)} />
                    <span className={clsx('text-xs font-medium uppercase', statusColor)}>{safeData.status}</span>
                </div>
            </div>

            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                {/* Request Rate */}
                <div className="p-4 rounded-lg bg-cluster-border/30">
                    <p className="text-xs text-cluster-muted mb-1">Request Rate</p>
                    <div className="flex items-baseline gap-1">
                        <span className="text-xl sm:text-2xl font-bold text-cluster-text">
                            {safeData.requestRate.toFixed(1)}
                        </span>
                        <span className="text-xs font-medium text-cluster-muted">req/s</span>
                    </div>
                </div>

                {/* Error Rate */}
                <div className="p-4 rounded-lg bg-cluster-border/30">
                    <p className="text-xs text-cluster-muted mb-1">Error Rate</p>
                    <div className="flex items-baseline gap-1">
                        <span className={clsx('text-xl sm:text-2xl font-bold', safeData.errorRate > 1 ? 'text-red-400' : 'text-cluster-text')}>
                            {safeData.errorRate.toFixed(2)}%
                        </span>
                    </div>
                </div>

                {/* Latency P99 */}
                <div className="p-4 rounded-lg bg-cluster-border/30">
                    <p className="text-xs text-cluster-muted mb-1">P99 Latency</p>
                    <div className="flex items-baseline gap-1">
                        <span className={clsx('text-xl sm:text-2xl font-bold', safeData.latencyP99 > 50 ? 'text-yellow-400' : 'text-cluster-text')}>
                            {safeData.latencyP99.toFixed(1)}
                        </span>
                        <span className="text-xs font-medium text-cluster-muted">ms</span>
                    </div>
                </div>

                {/* Cache Hit Rate */}
                <div className="p-4 rounded-lg bg-cluster-border/30">
                    <p className="text-xs text-cluster-muted mb-1">Cache Hit Rate</p>
                    <div className="flex items-baseline gap-1">
                        <span className="text-xl sm:text-2xl font-bold text-cluster-text">
                            {safeData.cacheHitRate.toFixed(1)}%
                        </span>
                    </div>
                </div>
            </div>
        </section>
    )
}
