'use client'

import { Modal } from './Modal'

interface SettingsModalProps {
    isOpen: boolean;
    onClose: () => void;
}

export function SettingsModal({ isOpen, onClose }: SettingsModalProps) {
    return (
        <Modal isOpen={isOpen} onClose={onClose} title="Dashboard Settings" size="sm">
            <div className="space-y-4 text-sm">
                <div>
                    <label className="block text-slate-400 mb-1 font-medium">API Endpoint URL</label>
                    <input
                        type="text"
                        className="w-full bg-black/20 border border-cluster-border rounded-lg p-2 text-cluster-text font-mono text-xs focus:ring-blue-500 focus:border-blue-500 outline-none transition-all"
                        defaultValue={process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8081'}
                        readOnly
                    />
                    <p className="text-xs text-slate-500 mt-1">Configured permanently via NEXT_PUBLIC_API_URL</p>
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
                        Cancel
                    </button>
                    <button
                        onClick={onClose}
                        className="btn-primary"
                    >
                        Save Preferences
                    </button>
                </div>
            </div>
        </Modal>
    )
}
