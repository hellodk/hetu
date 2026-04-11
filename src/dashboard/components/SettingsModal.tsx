'use client'

import { Modal } from './Modal'
import { PlayCircle, Activity } from 'lucide-react'

interface SettingsModalProps {
    isOpen: boolean
    onClose: () => void
    // Current profile, sourced from report.status.profile in page.tsx.
    profile?: 'live' | 'mock'
    // Callback invoked when the operator toggles the profile. The parent
    // handles the POST /api/v1/profile call so this component stays
    // presentational.
    onSwitchProfile?: (profile: 'live' | 'mock') => void
}

export function SettingsModal({ isOpen, onClose, profile = 'live', onSwitchProfile }: SettingsModalProps) {
    const isMock = profile === 'mock'
    return (
        <Modal isOpen={isOpen} onClose={onClose} title="Dashboard Settings" size="sm">
            <div className="space-y-5 text-sm">
                {/* Profile section — lets the operator switch between live
                    cluster analysis and synthetic demo data. Critical for
                    running product demos without a real cluster. */}
                <div>
                    <label className="block text-slate-300 mb-2 font-medium">Data Profile</label>
                    <div className="grid grid-cols-2 gap-2">
                        <button
                            type="button"
                            onClick={() => onSwitchProfile?.('live')}
                            aria-pressed={!isMock}
                            className={`flex items-center justify-center gap-2 p-3 rounded-lg border transition-all ${
                                !isMock
                                    ? 'bg-emerald-500/20 border-emerald-500/50 text-emerald-200'
                                    : 'bg-black/20 border-cluster-border text-slate-400 hover:bg-white/5'
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
                                    ? 'bg-amber-500/20 border-amber-500/50 text-amber-200'
                                    : 'bg-black/20 border-cluster-border text-slate-400 hover:bg-white/5'
                            }`}
                        >
                            <PlayCircle className="w-4 h-4" aria-hidden="true" />
                            <span className="font-medium">Demo</span>
                        </button>
                    </div>
                    <p className="text-xs text-slate-500 mt-2">
                        {isMock
                            ? 'Demo mode is active: synthetic data is being shown. No real cluster is analyzed.'
                            : 'Live mode: real cluster telemetry is analyzed. Switch to demo to present without a real cluster.'}
                    </p>
                    <p className="text-xs text-slate-600 mt-1">
                        Note: runtime profile changes are not persisted. Restarting the analyzer reverts to the PROFILE env var default.
                    </p>
                </div>

                <div>
                    <label className="block text-slate-400 mb-1 font-medium">API Endpoint URL</label>
                    <input
                        type="text"
                        className="w-full bg-black/20 border border-cluster-border rounded-lg p-2 text-cluster-text font-mono text-xs focus:ring-blue-500 focus:border-blue-500 outline-none transition-all"
                        defaultValue={typeof window !== 'undefined' ? ((window as any).__CLUSTER_INTEL_API__ || '(relative)') : '(server)'}
                        readOnly
                    />
                    <p className="text-xs text-slate-500 mt-1">Injected at runtime by the Next.js server layout</p>
                </div>

                <div>
                    <label className="block text-slate-400 mb-1 font-medium">Data Refresh Mode</label>
                    <select className="w-full bg-black/20 border border-cluster-border rounded-lg p-2 text-cluster-text focus:ring-blue-500 focus:border-blue-500 outline-none transition-all cursor-pointer appearance-none">
                        <option value="stream">Live Stream (SSE) - Active</option>
                        <option value="30s">30 seconds Polling</option>
                        <option value="60s">1 minute Polling</option>
                        <option value="manual">Manual Refresh Only</option>
                    </select>
                </div>

                <div>
                    <label className="block text-slate-400 mb-1 font-medium">Theme Preference</label>
                    <select className="w-full bg-black/20 border border-cluster-border rounded-lg p-2 text-cluster-text focus:ring-blue-500 focus:border-blue-500 outline-none transition-all cursor-pointer appearance-none">
                        <option value="dark">Dark Mode (Default)</option>
                        <option value="light">Light Mode (Coming Soon)</option>
                    </select>
                </div>

                <div className="pt-5 flex justify-end gap-3 border-t border-cluster-border mt-6">
                    <button
                        onClick={onClose}
                        className="px-4 py-2 rounded-lg text-slate-300 hover:text-white hover:bg-white/5 transition-colors font-medium"
                    >
                        Close
                    </button>
                </div>
            </div>
        </Modal>
    )
}
