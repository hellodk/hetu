'use client'

import { Cpu, HardDrive, Database, Network } from 'lucide-react'
import clsx from 'clsx'

interface ResourceBarProps {
  label: string
  icon: typeof Cpu
  used: number
  requested: number
  capacity: number
  unit: string
  color: string
}

function ResourceBar({ label, icon: Icon, used, requested, capacity, unit, color }: ResourceBarProps) {
  const usedPercent = (used / capacity) * 100
  const requestedPercent = (requested / capacity) * 100
  const efficiency = requested > 0 ? (used / requested) * 100 : 0

  const getUtilizationColor = (percent: number) => {
    if (percent >= 90) return 'text-red-400'
    if (percent >= 75) return 'text-yellow-400'
    return 'text-green-400'
  }

  return (
    <div className="space-y-2">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <Icon className={clsx('w-4 h-4', color)} aria-hidden="true" />
          <span className="text-sm font-medium">{label}</span>
        </div>
        <div className="flex flex-wrap items-center gap-3 sm:gap-4 text-xs sm:text-sm">
          <span className="text-slate-400">
            Used: <span className={getUtilizationColor(usedPercent)}>{used.toFixed(1)} {unit}</span>
          </span>
          <span className="text-slate-400">
            Requested: <span className="text-blue-400">{requested.toFixed(1)} {unit}</span>
          </span>
          <span className="text-slate-400 hidden sm:inline">
            Capacity: <span className="text-cluster-text">{capacity.toFixed(1)} {unit}</span>
          </span>
        </div>
      </div>

      {/* Multi-layer progress bar with accessibility */}
      <div
        className="relative h-4 bg-cluster-border rounded-full overflow-hidden"
        role="progressbar"
        aria-valuenow={usedPercent}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label={`${label} usage: ${usedPercent.toFixed(0)}% of capacity, ${efficiency.toFixed(0)}% efficiency`}
      >
        {/* Capacity indicator (background) */}
        <div className="absolute inset-0 bg-cluster-border" aria-hidden="true" />

        {/* Requested indicator */}
        <div
          className="absolute inset-y-0 left-0 bg-blue-500/30 border-r border-blue-500"
          style={{ width: `${Math.min(requestedPercent, 100)}%` }}
          aria-hidden="true"
        />

        {/* Used indicator */}
        <div
          className={clsx(
            'absolute inset-y-0 left-0 rounded-full transition-all duration-500',
            usedPercent >= 90 ? 'bg-red-500' : usedPercent >= 75 ? 'bg-yellow-500' : 'bg-green-500'
          )}
          style={{ width: `${Math.min(usedPercent, 100)}%` }}
          aria-hidden="true"
        />

        {/* Percentage labels */}
        <div className="absolute inset-0 flex items-center justify-between px-2" aria-hidden="true">
          <span className="text-xs font-medium text-white drop-shadow">
            {usedPercent.toFixed(0)}%
          </span>
        </div>
      </div>

      {/* Efficiency indicator */}
      <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-slate-400">
        <span>
          Efficiency:
          <span className={clsx(
            'ml-1 font-medium',
            efficiency > 70 ? 'text-green-400' : 'text-yellow-400'
          )}>
            {efficiency.toFixed(0)}%
          </span>
          <span className="ml-1">of requested</span>
        </span>
        <span>
          Headroom:
          <span className="ml-1 font-medium text-blue-400">
            {(capacity - used).toFixed(1)} {unit}
          </span>
        </span>
      </div>
    </div>
  )
}

interface ResourceData {
  used: number
  requested: number
  capacity: number
  unit: string
}

export interface ResourceUtilizationProps {
  resources?: {
    cpu: ResourceData
    memory: ResourceData
    storage: ResourceData
  }
}

export function ResourceUtilization({ resources }: ResourceUtilizationProps) {
  if (!resources) {
    return (
      <section className="bg-cluster-card rounded-xl border border-cluster-border p-4 sm:p-6 animate-pulse" aria-labelledby="resource-heading">
        <div className="h-6 w-1/3 bg-cluster-border rounded mb-6"></div>
        <div className="space-y-6">
          <div className="h-8 bg-cluster-border rounded"></div>
          <div className="h-8 bg-cluster-border rounded"></div>
          <div className="h-8 bg-cluster-border rounded"></div>
        </div>
      </section>
    )
  }

  const cpuSavings = ((resources.cpu.requested - resources.cpu.used) * 30 * 24 * 0.05).toFixed(0)
  const memorySavings = ((resources.memory.requested - resources.memory.used) * 30 * 24 * 0.01).toFixed(0)
  const overallEfficiency = ((resources.cpu.used / resources.cpu.requested +
    resources.memory.used / resources.memory.requested) / 2 * 100).toFixed(0)

  return (
    <section className="bg-cluster-card rounded-xl border border-cluster-border p-4 sm:p-6" aria-labelledby="resource-heading">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 mb-6">
        <h2 id="resource-heading" className="text-lg font-semibold flex items-center gap-2">
          <Database className="w-5 h-5 text-purple-500" aria-hidden="true" />
          Resource Utilization
        </h2>
        <span className="text-xs text-slate-400 flex items-center gap-1">
          <span className="w-2 h-2 bg-green-500 rounded-full animate-pulse" aria-hidden="true" />
          Real-time metrics
        </span>
      </div>

      <div className="space-y-5 sm:space-y-6">
        <ResourceBar
          label="CPU"
          icon={Cpu}
          used={resources.cpu.used}
          requested={resources.cpu.requested}
          capacity={resources.cpu.capacity}
          unit={resources.cpu.unit}
          color="text-blue-400"
        />

        <ResourceBar
          label="Memory"
          icon={HardDrive}
          used={resources.memory.used}
          requested={resources.memory.requested}
          capacity={resources.memory.capacity}
          unit={resources.memory.unit}
          color="text-purple-400"
        />

        <ResourceBar
          label="Storage"
          icon={Database}
          used={resources.storage.used}
          requested={resources.storage.requested}
          capacity={resources.storage.capacity}
          unit={resources.storage.unit}
          color="text-emerald-400"
        />
      </div>

      {/* Summary Cards */}
      <div className="grid grid-cols-3 gap-2 sm:gap-4 mt-6 pt-6 border-t border-cluster-border">
        <div className="text-center">
          <p className="text-lg sm:text-2xl font-bold text-yellow-400" aria-label={`$${cpuSavings} potential CPU savings per month`}>
            ${cpuSavings}
          </p>
          <p className="text-[10px] sm:text-xs text-slate-400 mt-1">CPU savings/mo</p>
        </div>
        <div className="text-center">
          <p className="text-lg sm:text-2xl font-bold text-yellow-400" aria-label={`$${memorySavings} potential memory savings per month`}>
            ${memorySavings}
          </p>
          <p className="text-[10px] sm:text-xs text-slate-400 mt-1">Memory savings/mo</p>
        </div>
        <div className="text-center">
          <p className="text-lg sm:text-2xl font-bold text-green-400" aria-label={`${overallEfficiency}% overall efficiency`}>
            {overallEfficiency}%
          </p>
          <p className="text-[10px] sm:text-xs text-slate-400 mt-1">Overall efficiency</p>
        </div>
      </div>
    </section>
  )
}
