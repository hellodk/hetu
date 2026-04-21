'use client'

import { Server, Boxes, Cpu, HardDrive } from 'lucide-react'

interface ClusterVitalsProps {
  summary: {
    totalNodes:    number
    totalPods:     number
    healthyPods:   number
    unhealthyPods: number
    pendingPods:   number
  }
  resources: {
    cpu:    { used: number; capacity: number; unit: string }
    memory: { used: number; capacity: number; unit: string }
  }
}

function VitalCard({
  label,
  icon,
  main,
  sub,
  barPercent,
  barColor,
}: {
  label:       string
  icon:        React.ReactNode
  main:        string
  sub:         string
  barPercent?: number
  barColor?:   string
}) {
  return (
    <div className="bg-cluster-card rounded-xl border border-cluster-border p-4 card-hover">
      <div className="flex items-center gap-2 mb-2">
        <span className="text-cluster-muted" aria-hidden="true">{icon}</span>
        <span className="text-xs font-bold uppercase tracking-widest text-cluster-muted">
          {label}
        </span>
      </div>
      <div className="text-3xl font-extrabold text-cluster-text leading-none">{main}</div>
      <div className="text-xs text-cluster-muted mt-1 leading-snug">{sub}</div>
      {barPercent !== undefined && (
        <div
          className="mt-3 h-1.5 bg-cluster-border rounded-full overflow-hidden"
          role="img"
          aria-label={`${barPercent}% utilized`}
        >
          <div
            className={`h-full rounded-full transition-all duration-500 ${barColor ?? 'bg-blue-500'}`}
            style={{ width: `${Math.min(barPercent, 100)}%` }}
            aria-hidden="true"
          />
        </div>
      )}
    </div>
  )
}

export function ClusterVitals({ summary, resources }: ClusterVitalsProps) {
  const cpuPct = resources.cpu.capacity > 0
    ? Math.round((resources.cpu.used / resources.cpu.capacity) * 100)
    : 0
  const memPct = resources.memory.capacity > 0
    ? Math.round((resources.memory.used / resources.memory.capacity) * 100)
    : 0
  const healthyPct = summary.totalPods > 0
    ? Math.round((summary.healthyPods / summary.totalPods) * 100)
    : 100

  const cpuColor = cpuPct >= 90 ? 'bg-red-500' : cpuPct >= 70 ? 'bg-yellow-500' : 'bg-blue-500'
  const memColor = memPct >= 90 ? 'bg-red-500' : memPct >= 70 ? 'bg-yellow-500' : 'bg-purple-500'

  const notReadyCount = summary.unhealthyPods + summary.pendingPods

  return (
    <section
      aria-label="Cluster vitals"
      className="grid grid-cols-2 sm:grid-cols-4 gap-4"
    >
      <VitalCard
        label="Nodes"
        icon={<Server className="w-4 h-4" />}
        main={String(summary.totalNodes)}
        sub="cluster nodes"
      />
      <VitalCard
        label="Pods"
        icon={<Boxes className="w-4 h-4" />}
        main={String(summary.totalPods)}
        sub={`${summary.healthyPods} healthy · ${notReadyCount} not ready`}
        barPercent={healthyPct}
        barColor="bg-green-500"
      />
      <VitalCard
        label="CPU"
        icon={<Cpu className="w-4 h-4" />}
        main={`${cpuPct}%`}
        sub={`${resources.cpu.used} / ${resources.cpu.capacity} ${resources.cpu.unit}`}
        barPercent={cpuPct}
        barColor={cpuColor}
      />
      <VitalCard
        label="Memory"
        icon={<HardDrive className="w-4 h-4" />}
        main={`${memPct}%`}
        sub={`${resources.memory.used} / ${resources.memory.capacity} ${resources.memory.unit}`}
        barPercent={memPct}
        barColor={memColor}
      />
    </section>
  )
}
