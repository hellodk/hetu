'use client'

import React from 'react'
import { Boxes, AlertCircle } from 'lucide-react'

export interface NamespaceStats {
    cpuUsed: number
    memoryUsed: number
    podCount: number
    warnings: number
}

interface NamespacesTableProps {
    namespaces?: Record<string, NamespaceStats>
}

export function NamespacesTable({ namespaces }: NamespacesTableProps) {
    if (!namespaces || Object.keys(namespaces).length === 0) {
        return (
            <div className="bg-cluster-card rounded-xl border border-cluster-border p-12 text-center">
                <Boxes className="w-12 h-12 text-slate-500 mx-auto mb-4" />
                <h3 className="text-lg font-medium text-slate-300">No Namespace Data</h3>
                <p className="text-sm text-slate-500 mt-2">
                    Namespace-level metrics are not available for this cluster.
                </p>
            </div>
        )
    }

    const entries = Object.entries(namespaces).sort((a, b) => b[1].cpuUsed - a[1].cpuUsed)

    return (
        <div className="space-y-4">
            <div className="flex items-center gap-2 mb-2">
                <Boxes className="w-5 h-5 text-indigo-400" />
                <h2 className="text-lg font-semibold text-slate-100">Namespace Drill-Down</h2>
            </div>

            <div className="bg-cluster-card rounded-xl border border-cluster-border overflow-hidden">
                <div className="overflow-x-auto">
                    <table className="w-full text-left text-sm whitespace-nowrap">
                        <thead className="bg-black/20 text-slate-400 text-xs uppercase font-medium">
                            <tr>
                                <th className="px-6 py-4">Namespace</th>
                                <th className="px-6 py-4 text-right">Pods</th>
                                <th className="px-6 py-4 text-right">CPU Used (cores)</th>
                                <th className="px-6 py-4 text-right">Memory Used (Gi)</th>
                                <th className="px-6 py-4 text-right">Active Warnings</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-cluster-border text-slate-200">
                            {entries.map(([name, stats]) => (
                                <tr key={name} className="hover:bg-white/[0.02] transition-colors">
                                    <td className="px-6 py-4 font-medium">{name}</td>
                                    <td className="px-6 py-4 text-right">{stats.podCount}</td>
                                    <td className="px-6 py-4 text-right">{stats.cpuUsed.toFixed(2)}</td>
                                    <td className="px-6 py-4 text-right">{stats.memoryUsed.toFixed(2)}</td>
                                    <td className="px-6 py-4 text-right">
                                        {stats.warnings > 0 ? (
                                            <span className="inline-flex items-center gap-1 text-red-400 font-bold bg-red-400/10 px-2 py-0.5 rounded-full text-xs">
                                                <AlertCircle className="w-3 h-3" />
                                                {stats.warnings}
                                            </span>
                                        ) : (
                                            <span className="text-green-400/70">0</span>
                                        )}
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            </div>
        </div>
    )
}
