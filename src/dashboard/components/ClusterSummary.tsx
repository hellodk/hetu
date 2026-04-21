'use client'

import { Server, Boxes, Layers, CheckCircle, XCircle, Clock, AlertTriangle } from 'lucide-react'
import clsx from 'clsx'

interface ClusterSummaryData {
  totalNodes: number
  totalPods: number
  totalNamespaces: number
  healthyPods: number
  unhealthyPods: number
  pendingPods: number
  warningEvents: number
  criticalEvents: number
}

interface ClusterSummaryProps {
  summary: ClusterSummaryData
}

export function ClusterSummary({ summary }: ClusterSummaryProps) {
  const healthPercentage = summary.totalPods > 0 
    ? Math.round((summary.healthyPods / summary.totalPods) * 100) 
    : 100

  const healthyPercent = summary.totalPods > 0 ? (summary.healthyPods / summary.totalPods) * 100 : 0
  const pendingPercent = summary.totalPods > 0 ? (summary.pendingPods / summary.totalPods) * 100 : 0
  const unhealthyPercent = summary.totalPods > 0 ? (summary.unhealthyPods / summary.totalPods) * 100 : 0

  return (
    <section className="bg-cluster-card rounded-xl border border-cluster-border p-4 sm:p-6" aria-labelledby="cluster-heading">
      <h2 id="cluster-heading" className="text-lg font-semibold mb-4 flex items-center gap-2">
        <Server className="w-5 h-5 text-blue-500" aria-hidden="true" />
        Cluster Overview
      </h2>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-3 sm:gap-4">
        {/* Nodes */}
        <div className="bg-blue-500/10 rounded-lg p-3 sm:p-4 border border-blue-500/20 card-hover">
          <div className="flex items-center gap-2 mb-2">
            <Server className="w-4 h-4 text-blue-400" aria-hidden="true" />
            <span className="text-sm text-slate-400">Nodes</span>
          </div>
          <p className="text-xl sm:text-2xl font-bold text-blue-400" aria-label={`${summary.totalNodes} nodes`}>
            {summary.totalNodes}
          </p>
        </div>

        {/* Namespaces */}
        <div className="bg-purple-500/10 rounded-lg p-3 sm:p-4 border border-purple-500/20 card-hover">
          <div className="flex items-center gap-2 mb-2">
            <Layers className="w-4 h-4 text-purple-400" aria-hidden="true" />
            <span className="text-sm text-slate-400">Namespaces</span>
          </div>
          <p className="text-xl sm:text-2xl font-bold text-purple-400" aria-label={`${summary.totalNamespaces} namespaces`}>
            {summary.totalNamespaces}
          </p>
        </div>

        {/* Total Pods */}
        <div className="bg-emerald-500/10 rounded-lg p-3 sm:p-4 border border-emerald-500/20 card-hover">
          <div className="flex items-center gap-2 mb-2">
            <Boxes className="w-4 h-4 text-emerald-400" aria-hidden="true" />
            <span className="text-sm text-slate-400">Total Pods</span>
          </div>
          <p className="text-xl sm:text-2xl font-bold text-emerald-400" aria-label={`${summary.totalPods} total pods`}>
            {summary.totalPods}
          </p>
        </div>

        {/* Pod Health */}
        <div className={clsx(
          'rounded-lg p-3 sm:p-4 border card-hover',
          healthPercentage >= 95 
            ? 'bg-green-500/10 border-green-500/20' 
            : healthPercentage >= 80 
              ? 'bg-yellow-500/10 border-yellow-500/20'
              : 'bg-red-500/10 border-red-500/20'
        )}>
          <div className="flex items-center gap-2 mb-2">
            <CheckCircle className={clsx(
              'w-4 h-4',
              healthPercentage >= 95 ? 'text-green-400' : healthPercentage >= 80 ? 'text-yellow-400' : 'text-red-400'
            )} aria-hidden="true" />
            <span className="text-sm text-slate-400">Pod Health</span>
          </div>
          <p className={clsx(
            'text-xl sm:text-2xl font-bold',
            healthPercentage >= 95 ? 'text-green-400' : healthPercentage >= 80 ? 'text-yellow-400' : 'text-red-400'
          )} aria-label={`${healthPercentage}% pods healthy`}>
            {healthPercentage}%
          </p>
        </div>
      </div>

      {/* Pod Status Breakdown */}
      <div className="mt-6">
        <h3 className="text-sm font-medium text-slate-400 mb-3">Pod Status Distribution</h3>
        
        {/* Progress bar with accessibility */}
        <div 
          className="progress-bar"
          role="img"
          aria-label={`Pod distribution: ${summary.healthyPods} healthy, ${summary.pendingPods} pending, ${summary.unhealthyPods} unhealthy`}
        >
          <div 
            className="progress-bar-fill bg-green-500"
            style={{ width: `${healthyPercent}%` }}
            aria-hidden="true"
          />
          <div 
            className="progress-bar-fill bg-yellow-500"
            style={{ width: `${pendingPercent}%`, left: `${healthyPercent}%` }}
            aria-hidden="true"
          />
          <div 
            className="progress-bar-fill bg-red-500"
            style={{ width: `${unhealthyPercent}%`, left: `${healthyPercent + pendingPercent}%` }}
            aria-hidden="true"
          />
        </div>

        {/* Legend */}
        <div className="flex flex-wrap items-center gap-4 sm:gap-6 mt-3">
          <div className="flex items-center gap-2">
            <div className="w-3 h-3 bg-green-500 rounded-full" aria-hidden="true" />
            <span className="text-sm text-slate-400">Healthy</span>
            <span className="text-sm font-medium text-green-400">{summary.healthyPods}</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="w-3 h-3 bg-yellow-500 rounded-full" aria-hidden="true" />
            <span className="text-sm text-slate-400">Pending</span>
            <span className="text-sm font-medium text-yellow-400">{summary.pendingPods}</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="w-3 h-3 bg-red-500 rounded-full" aria-hidden="true" />
            <span className="text-sm text-slate-400">Unhealthy</span>
            <span className="text-sm font-medium text-red-400">{summary.unhealthyPods}</span>
          </div>
        </div>
      </div>

      {/* Event Summary */}
      <div className="mt-6 pt-6 border-t border-cluster-border">
        <h3 className="text-sm font-medium text-slate-400 mb-3">Recent Events</h3>
        <div className="flex flex-wrap items-center gap-4 sm:gap-6">
          <div className="flex items-center gap-2">
            <AlertTriangle className="w-4 h-4 text-red-400" aria-hidden="true" />
            <span className="text-sm text-slate-400">Critical</span>
            <span className={clsx(
              'text-sm font-medium',
              summary.criticalEvents > 0 ? 'text-red-400' : 'text-slate-500'
            )} aria-label={`${summary.criticalEvents} critical events`}>
              {summary.criticalEvents}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <AlertTriangle className="w-4 h-4 text-yellow-400" aria-hidden="true" />
            <span className="text-sm text-slate-400">Warning</span>
            <span className={clsx(
              'text-sm font-medium',
              summary.warningEvents > 0 ? 'text-yellow-400' : 'text-slate-500'
            )} aria-label={`${summary.warningEvents} warning events`}>
              {summary.warningEvents}
            </span>
          </div>
        </div>
      </div>
    </section>
  )
}
