'use client'

import { useState } from 'react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import {
  LayoutDashboard, Boxes, Server, Network, Database,
  Shield, Gauge, ChevronDown, ChevronRight, Menu, X, Bug, Globe, Zap, TrendingDown,
  Activity, Settings
} from 'lucide-react'

interface NavSection {
  name: string
  icon: React.ReactNode
  items: { label: string; href: string }[]
}

const sections: NavSection[] = [
  {
    name: 'Workloads',
    icon: <Boxes className="w-4 h-4" />,
    items: [
      { label: 'Pods', href: '/workloads/pods?group=core&version=v1' },
      { label: 'Deployments', href: '/workloads/deployments?group=apps&version=v1' },
      { label: 'StatefulSets', href: '/workloads/statefulsets?group=apps&version=v1' },
      { label: 'DaemonSets', href: '/workloads/daemonsets?group=apps&version=v1' },
      { label: 'ReplicaSets', href: '/workloads/replicasets?group=apps&version=v1' },
      { label: 'Jobs', href: '/workloads/jobs?group=batch&version=v1' },
      { label: 'CronJobs', href: '/workloads/cronjobs?group=batch&version=v1' },
    ],
  },
  {
    name: 'Service & Networking',
    icon: <Network className="w-4 h-4" />,
    items: [
      { label: 'Services', href: '/workloads/services?group=core&version=v1' },
      { label: 'Ingresses', href: '/workloads/ingresses?group=networking.k8s.io&version=v1' },
      { label: 'Endpoints', href: '/workloads/endpoints?group=core&version=v1' },
      { label: 'NetworkPolicies', href: '/workloads/networkpolicies?group=networking.k8s.io&version=v1' },
    ],
  },
  {
    name: 'Config & Storage',
    icon: <Database className="w-4 h-4" />,
    items: [
      { label: 'ConfigMaps', href: '/workloads/configmaps?group=core&version=v1' },
      { label: 'Secrets', href: '/workloads/secrets?group=core&version=v1' },
      { label: 'PVCs', href: '/workloads/persistentvolumeclaims?group=core&version=v1' },
    ],
  },
  {
    name: 'Cluster',
    icon: <Server className="w-4 h-4" />,
    items: [
      { label: 'Nodes', href: '/workloads/nodes?group=core&version=v1' },
      { label: 'Namespaces', href: '/workloads/namespaces?group=core&version=v1' },
      { label: 'Events', href: '/workloads/events?group=core&version=v1' },
    ],
  },
  {
    name: 'RBAC',
    icon: <Shield className="w-4 h-4" />,
    items: [
      { label: 'ServiceAccounts', href: '/workloads/serviceaccounts?group=core&version=v1' },
      { label: 'ClusterRoles', href: '/workloads/clusterroles?group=rbac.authorization.k8s.io&version=v1' },
      { label: 'ClusterRoleBindings', href: '/workloads/clusterrolebindings?group=rbac.authorization.k8s.io&version=v1' },
    ],
  },
  {
    name: 'Autoscaling',
    icon: <Gauge className="w-4 h-4" />,
    items: [
      { label: 'HPAs', href: '/workloads/horizontalpodautoscalers?group=autoscaling&version=v2' },
    ],
  },
]

export function Navigation() {
  const pathname = usePathname()
  const [openSections, setOpenSections] = useState<Set<string>>(new Set(['Workloads']))
  const [mobileOpen, setMobileOpen] = useState(false)

  const toggle = (name: string) => {
    setOpenSections(prev => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  const isActive = (href: string) => {
    const path = href.split('?')[0]
    return pathname === path || pathname?.startsWith(path + '/')
  }

  const nav = (
    <nav className="flex flex-col h-full">
      {/* Top links */}
      <div className="p-4 border-b border-white/10">
        <Link
          href="/"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors ${
            pathname === '/' ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-white/10'
          }`}
        >
          <LayoutDashboard className="w-4 h-4" />
          Overview
        </Link>
        <Link
          href="/errors"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors mt-1 ${
            pathname?.startsWith('/errors') ? 'bg-red-600/20 text-red-400' : 'text-gray-300 hover:bg-white/10'
          }`}
        >
          <Bug className="w-4 h-4" />
          Errors
        </Link>
        <Link
          href="/lb-logs"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors mt-1 ${
            pathname?.startsWith('/lb-logs') ? 'bg-blue-600/20 text-blue-400' : 'text-gray-300 hover:bg-white/10'
          }`}
        >
          <Globe className="w-4 h-4" />
          LB Logs
        </Link>
        <Link
          href="/incidents"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors mt-1 ${
            pathname?.startsWith('/incidents') ? 'bg-purple-600/20 text-purple-400' : 'text-gray-300 hover:bg-white/10'
          }`}
        >
          <Zap className="w-4 h-4" />
          Incidents & RCA
        </Link>
        <Link
          href="/optimization"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors mt-1 ${
            pathname?.startsWith('/optimization') ? 'bg-green-600/20 text-green-400' : 'text-gray-300 hover:bg-white/10'
          }`}
        >
          <TrendingDown className="w-4 h-4" />
          Optimization
        </Link>
        <Link
          href="/anomalies"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors mt-1 ${
            pathname?.startsWith('/anomalies') ? 'bg-teal-600/20 text-teal-400' : 'text-gray-300 hover:bg-white/10'
          }`}
        >
          <Activity className="w-4 h-4" />
          Anomalies
        </Link>
        <Link
          href="/security"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors mt-1 ${
            pathname?.startsWith('/security') ? 'bg-orange-600/20 text-orange-400' : 'text-gray-300 hover:bg-white/10'
          }`}
        >
          <Shield className="w-4 h-4" />
          Security
        </Link>
        <Link
          href="/settings"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors mt-1 ${
            pathname?.startsWith('/settings') ? 'bg-gray-600/20 text-gray-400' : 'text-gray-300 hover:bg-white/10'
          }`}
        >
          <Settings className="w-4 h-4" />
          Settings
        </Link>
      </div>

      {/* Resource sections */}
      <div className="flex-1 overflow-y-auto p-2">
        {sections.map(section => (
          <div key={section.name} className="mb-1">
            <button
              onClick={() => toggle(section.name)}
              className="flex items-center gap-2 w-full px-3 py-2 text-sm text-gray-400 hover:text-white rounded-md hover:bg-white/5 transition-colors"
            >
              {section.icon}
              <span className="flex-1 text-left">{section.name}</span>
              {openSections.has(section.name) ? (
                <ChevronDown className="w-3 h-3" />
              ) : (
                <ChevronRight className="w-3 h-3" />
              )}
            </button>
            {openSections.has(section.name) && (
              <div className="ml-4 mt-1 space-y-0.5">
                {section.items.map(item => (
                  <Link
                    key={item.href}
                    href={item.href}
                    className={`block px-3 py-1.5 text-sm rounded-md transition-colors ${
                      isActive(item.href)
                        ? 'bg-blue-600/20 text-blue-400'
                        : 'text-gray-400 hover:text-white hover:bg-white/5'
                    }`}
                  >
                    {item.label}
                  </Link>
                ))}
              </div>
            )}
          </div>
        ))}
      </div>
    </nav>
  )

  return (
    <>
      {/* Mobile toggle */}
      <button
        onClick={() => setMobileOpen(!mobileOpen)}
        className="fixed top-4 left-4 z-50 p-2 rounded-md bg-gray-800 text-white lg:hidden"
      >
        {mobileOpen ? <X className="w-5 h-5" /> : <Menu className="w-5 h-5" />}
      </button>

      {/* Sidebar */}
      <aside
        className={`fixed inset-y-0 left-0 z-40 w-56 bg-gray-900 border-r border-white/10 transform transition-transform duration-200 lg:translate-x-0 ${
          mobileOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
      >
        <div className="h-14 flex items-center px-4 border-b border-white/10">
          <span className="text-sm font-semibold text-white">Cluster Intel</span>
        </div>
        {nav}
      </aside>

      {/* Mobile overlay */}
      {mobileOpen && (
        <div
          className="fixed inset-0 z-30 bg-black/50 lg:hidden"
          onClick={() => setMobileOpen(false)}
        />
      )}
    </>
  )
}
