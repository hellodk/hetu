'use client'

import { Modal } from './Modal'
import { PlayCircle, Activity, Save, Download } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

type ThemeChoice = 'graphite' | 'calm-signal' | 'aurora' | 'prism' | 'auto' | 'md-dark' | 'md-light'
const THEME_VALUES: readonly ThemeChoice[] = ['graphite', 'calm-signal', 'aurora', 'prism', 'auto', 'md-dark', 'md-light'] as const
const THEME_LABELS: Record<ThemeChoice, string> = {
    graphite:      'Graphite — editorial light (default)',
    'calm-signal': 'Calm signal — restrained dark',
    aurora:        'Aurora — magical dark',
    prism:         'Prism — white wow',
    auto:          'Auto — follow OS preference',
    'md-dark':     'Material Dark — MD3 dark palette',
    'md-light':    'Material Light — MD3 light palette',
}
const LEGACY_MIGRATION: Record<string, ThemeChoice> = {
    light:  'graphite',
    dark:   'calm-signal',
    system: 'auto',
}

interface SettingsModalProps {
    isOpen: boolean
    onClose: () => void
    // Current profile, sourced from report.status.profile in page.tsx.
    profile?: 'live' | 'mock'
    // Current collector URL (from report.status.collector.endpoint)
    collectorUrl?: string
    // Callback invoked when the operator toggles the profile. The parent
    // handles the POST /api/v1/profile call so this component stays
    // presentational.
    onSwitchProfile?: (profile: 'live' | 'mock') => void
    // Callback invoked when the operator updates COLLECTOR_URL.
    onSetCollectorUrl?: (collectorUrl: string) => Promise<void> | void
}

export function SettingsModal({
    isOpen,
    onClose,
    profile = 'live',
    collectorUrl = '',
    onSwitchProfile,
    onSetCollectorUrl,
}: SettingsModalProps) {
    const isMock = profile === 'mock'
    const initialCollector = useMemo(() => collectorUrl || '', [collectorUrl])
    const [collectorInput, setCollectorInput] = useState(initialCollector)
    const [savingCollector, setSavingCollector] = useState(false)
    const [overrideYaml, setOverrideYaml] = useState<string>('')
    const [overrideYamlLoaded, setOverrideYamlLoaded] = useState(false)
    const [savingOverride, setSavingOverride] = useState(false)
    const [overrideLocation, setOverrideLocation] = useState<string>('')
    const [overrideLoadError, setOverrideLoadError] = useState<string>('')
    const [themePref, setThemePref] = useState<ThemeChoice>('graphite')

    useEffect(() => {
        if (typeof window === 'undefined') return
        try {
            const raw = localStorage.getItem('ci_theme')
            let next: ThemeChoice = 'graphite'
            if (raw) {
                if (LEGACY_MIGRATION[raw]) next = LEGACY_MIGRATION[raw]
                else if ((THEME_VALUES as readonly string[]).includes(raw)) next = raw as ThemeChoice
            }
            setThemePref(next)
        } catch {
            // ignore
        }
    }, [isOpen])

    useEffect(() => {
        if (typeof window === 'undefined') return
        const prefersDark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches
        const resolved: Exclude<ThemeChoice, 'auto'> =
            themePref === 'auto' ? (prefersDark ? 'calm-signal' : 'graphite') : themePref
        document.documentElement.setAttribute('data-theme', resolved)
        const LIGHT = ['graphite', 'prism', 'md-light']
        document.documentElement.classList.toggle('dark', !LIGHT.includes(resolved))
    }, [themePref])

    useEffect(() => {
        if (!isOpen) return
        setCollectorInput(initialCollector)
    }, [isOpen, initialCollector])

    useEffect(() => {
        if (!isOpen) return
        // Load current runtime override YAML so operators can GitOps it if needed.
        const apiBase = typeof window !== 'undefined' ? ((window as any).__CLUSTER_INTEL_API__ || '') : ''
        setOverrideLoadError('')
        fetch(`${apiBase}/api/v1/config`)
            .then(r => r.ok ? r.json() : Promise.reject(new Error(`HTTP ${r.status}`)))
            .then(data => {
                const yaml = data?.runtimeOverrideYaml?.yaml ?? ''
                setOverrideYaml(String(yaml ?? ''))
                setOverrideYamlLoaded(true)
                setOverrideLocation(String(data?.store?.location ?? ''))
            })
            .catch(err => {
                setOverrideLoadError(err instanceof Error ? err.message : 'Failed to load config')
                setOverrideYamlLoaded(true)
            })
    }, [isOpen])

    const collectorDirty = collectorInput.trim() !== initialCollector.trim()
    return (
        <Modal isOpen={isOpen} onClose={onClose} title="Dashboard Settings" size="sm">
            <div className="space-y-5 text-sm">
                {/* Profile section — lets the operator switch between live
                    cluster analysis and synthetic demo data. Critical for
                    running product demos without a real cluster. */}
                <div>
                    <label className="block text-cluster-text mb-2 font-medium">Data Profile</label>
                    <div className="grid grid-cols-2 gap-2">
                        <button
                            type="button"
                            onClick={() => onSwitchProfile?.('live')}
                            aria-pressed={!isMock}
                            className={`flex items-center justify-center gap-2 p-3 rounded-lg border transition-all ${
                                !isMock
                                    ? 'bg-emerald-500/15 border-emerald-500/40 text-emerald-800 dark:text-emerald-200'
                                    : 'bg-cluster-card border-cluster-border text-cluster-muted hover:bg-cluster-border/30'
                            }`}
                        >
                            <Activity className="w-4 h-4" aria-hidden="true" />
                            <span className="font-medium">Live</span>
                        </button>
                        <button
                            type="button"
                            onClick={() => onSwitchProfile?.('mock')}
                            aria-pressed={isMock}
                            className={`flex items-center justify-center gap-2 p-3 rounded-lg border transition-all ${
                                isMock
                                    ? 'bg-amber-500/15 border-amber-500/40 text-amber-800 dark:text-amber-200'
                                    : 'bg-cluster-card border-cluster-border text-cluster-muted hover:bg-cluster-border/30'
                            }`}
                        >
                            <PlayCircle className="w-4 h-4" aria-hidden="true" />
                            <span className="font-medium">Demo</span>
                        </button>
                    </div>
                    <p className="text-xs text-cluster-muted mt-2">
                        {isMock
                            ? 'Demo mode is active: synthetic data is being shown. No real cluster is analyzed.'
                            : 'Live mode: real cluster telemetry is analyzed. Switch to demo to present without a real cluster.'}
                    </p>
                    <p className="text-xs text-cluster-muted/80 mt-1">
                        Note: profile changes are not persisted. Restarting the analyzer always starts in Live mode; enable Demo again here if needed.
                    </p>
                </div>

                <div>
                    <label className="block text-cluster-text mb-1 font-medium">Collector URL (Live mode)</label>
                    <input
                        type="text"
                        className="w-full bg-cluster-card border border-cluster-border rounded-lg p-2 text-cluster-text font-mono text-xs focus:ring-blue-500 focus:border-blue-500 outline-none transition-all"
                        value={collectorInput}
                        onChange={(e) => setCollectorInput(e.target.value)}
                        placeholder="http://collector:8080"
                        spellCheck={false}
                        inputMode="url"
                    />
                    <div className="mt-2 flex items-center justify-between gap-3">
                        <p className="text-xs text-cluster-muted">
                            Required in Live mode. If empty, telemetry cannot be fetched.
                        </p>
                        <button
                            type="button"
                            className={`inline-flex items-center gap-2 px-3 py-1.5 rounded-md text-xs font-semibold border transition-colors ${collectorDirty
                                ? 'bg-blue-600/15 border-blue-500/40 text-blue-800 dark:text-blue-200 hover:bg-blue-600/20'
                                : 'bg-cluster-card border-cluster-border text-cluster-muted/70 cursor-not-allowed'
                                }`}
                            disabled={!collectorDirty || savingCollector}
                            onClick={async () => {
                                if (!onSetCollectorUrl) return
                                setSavingCollector(true)
                                try {
                                    await onSetCollectorUrl(collectorInput.trim())
                                } finally {
                                    setSavingCollector(false)
                                }
                            }}
                        >
                            <Save className={`w-3.5 h-3.5 ${savingCollector ? 'animate-pulse' : ''}`} aria-hidden="true" />
                            Save
                        </button>
                    </div>
                </div>

                <div>
                    <label className="block text-cluster-text mb-1 font-medium">Runtime configuration overrides (persisted)</label>
                    <p className="text-xs text-cluster-muted mb-2">
                        Saved as a runtime override layer. Intended for quick recovery; commit to GitOps for permanence.
                        {overrideLocation ? ` Stored in: ${overrideLocation}` : ''}
                    </p>
                    {overrideLoadError && (
                        <p className="text-xs text-amber-600 dark:text-amber-300 mb-2">
                            Could not load current overrides: {overrideLoadError}
                        </p>
                    )}
                    <textarea
                        className="w-full h-40 bg-cluster-card border border-cluster-border rounded-lg p-2 text-cluster-text font-mono text-xs focus:ring-blue-500 focus:border-blue-500 outline-none transition-all"
                        value={overrideYaml}
                        onChange={(e) => setOverrideYaml(e.target.value)}
                        placeholder={'# Example:\n# analyzer:\n#   collectorUrl: http://collector:8080\n# llm:\n#   provider: openai\n#   endpoint: https://api.openai.com/v1\n'}
                        spellCheck={false}
                        disabled={!overrideYamlLoaded}
                    />
                    <div className="mt-2 flex items-center justify-end gap-2">
                        <button
                            type="button"
                            className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md text-xs font-semibold border bg-cluster-card border-cluster-border text-cluster-text hover:bg-cluster-border/40 transition-colors"
                            onClick={() => {
                                const blob = new Blob([overrideYaml], { type: 'text/yaml' })
                                const url = URL.createObjectURL(blob)
                                const a = document.createElement('a')
                                a.href = url
                                a.download = 'cluster-intel-runtime.yaml'
                                a.click()
                                URL.revokeObjectURL(url)
                            }}
                            disabled={!overrideYamlLoaded}
                        >
                            <Download className="w-3.5 h-3.5" aria-hidden="true" />
                            Download YAML
                        </button>
                        <button
                            type="button"
                            className={`inline-flex items-center gap-2 px-3 py-1.5 rounded-md text-xs font-semibold border transition-colors ${savingOverride
                                ? 'bg-blue-600/15 border-blue-500/40 text-blue-800 dark:text-blue-200'
                                : 'bg-blue-600/15 border-blue-500/40 text-blue-800 dark:text-blue-200 hover:bg-blue-600/20'
                                }`}
                            disabled={!overrideYamlLoaded || savingOverride}
                            onClick={async () => {
                                const apiBase = typeof window !== 'undefined' ? ((window as any).__CLUSTER_INTEL_API__ || '') : ''
                                setSavingOverride(true)
                                setOverrideLoadError('')
                                try {
                                    const resp = await fetch(`${apiBase}/api/v1/config`, {
                                        method: 'PUT',
                                        headers: { 'Content-Type': 'application/json' },
                                        body: JSON.stringify({ overrideYaml }),
                                    })
                                    if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
                                } catch (err) {
                                    setOverrideLoadError(err instanceof Error ? err.message : 'Failed to save config')
                                } finally {
                                    setSavingOverride(false)
                                }
                            }}
                        >
                            <Save className={`w-3.5 h-3.5 ${savingOverride ? 'animate-pulse' : ''}`} aria-hidden="true" />
                            Save overrides
                        </button>
                    </div>
                </div>

                <div>
                    <label className="block text-cluster-text mb-1 font-medium">API Endpoint URL</label>
                    <input
                        type="text"
                        className="w-full bg-cluster-card border border-cluster-border rounded-lg p-2 text-cluster-text font-mono text-xs focus:ring-blue-500 focus:border-blue-500 outline-none transition-all"
                        defaultValue={typeof window !== 'undefined' ? ((window as any).__CLUSTER_INTEL_API__ || '(relative)') : '(server)'}
                        readOnly
                    />
                    <p className="text-xs text-cluster-muted mt-1">Injected at runtime by the Next.js server layout</p>
                </div>

                <div>
                    <label className="block text-cluster-text mb-1 font-medium">Data Refresh Mode</label>
                    <select className="w-full bg-cluster-card border border-cluster-border rounded-lg p-2 text-cluster-text focus:ring-blue-500 focus:border-blue-500 outline-none transition-all cursor-pointer appearance-none">
                        <option value="stream">Live Stream (SSE) - Active</option>
                        <option value="30s">30 seconds Polling</option>
                        <option value="60s">1 minute Polling</option>
                        <option value="manual">Manual Refresh Only</option>
                    </select>
                </div>

                <div>
                    <label className="block text-slate-400 mb-1 font-medium">Theme</label>
                    <select
                        value={themePref}
                        onChange={(e) => {
                            const val = e.target.value as ThemeChoice
                            try { localStorage.setItem('ci_theme', val) } catch { /* ignore */ }
                            setThemePref(val)
                        }}
                        className="w-full bg-cluster-card border border-cluster-border rounded-lg p-2 text-cluster-text focus:ring-blue-500 focus:border-blue-500 outline-none transition-all cursor-pointer appearance-none"
                    >
                        {THEME_VALUES.map(v => (
                            <option key={v} value={v}>{THEME_LABELS[v]}</option>
                        ))}
                    </select>
                    <p className="text-xs text-cluster-muted mt-1">
                        Applies instantly and is saved in this browser.
                    </p>
                </div>

                <div className="pt-5 flex justify-end gap-3 border-t border-cluster-border mt-6">
                    <button
                        onClick={onClose}
                        className="px-4 py-2 rounded-lg text-cluster-muted hover:text-cluster-text hover:bg-cluster-border/40 transition-colors font-medium"
                    >
                        Close
                    </button>
                </div>
            </div>
        </Modal>
    )
}
