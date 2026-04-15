'use client'

import { useEffect, useMemo, useState } from 'react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import {
  LayoutDashboard, Boxes, Server, Network, Database,
  Shield, Gauge, ChevronDown, ChevronRight, Menu, X, Bug, Globe, Zap, TrendingDown,
  Activity, Settings, Sun, Moon, Monitor
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
  const [theme, setTheme] = useState<'light' | 'dark' | 'system'>('system')

  const themeIcon = useMemo(() => {
    if (theme === 'light') return <Sun className="w-4 h-4" />
    if (theme === 'dark') return <Moon className="w-4 h-4" />
    return <Monitor className="w-4 h-4" />
  }, [theme])

  useEffect(() => {
    try {
      const stored = (localStorage.getItem('ci_theme') as 'light' | 'dark' | 'system' | null) || 'system'
      setTheme(stored)
    } catch {
      // ignore
    }
  }, [])

  useEffect(() => {
    try {
      localStorage.setItem('ci_theme', theme)
    } catch {
      // ignore
    }
    const prefersDark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches
    const shouldDark = theme === 'dark' || (theme === 'system' && prefersDark)
    document.documentElement.classList.toggle('dark', shouldDark)
  }, [theme])

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
      <div className="p-4 border-b border-cluster-border">
        <div className="flex items-center justify-between gap-2 mb-2">
          <div className="text-xs font-semibold tracking-wide text-cluster-muted uppercase">
            Navigation
          </div>
          <div className="flex items-center gap-2">
            <label className="sr-only" htmlFor="theme">
              Theme
            </label>
            <div className="flex items-center gap-1.5 text-xs text-cluster-muted">
              {themeIcon}
              <select
                id="theme"
                value={theme}
                onChange={(e) => setTheme(e.target.value as 'light' | 'dark' | 'system')}
                className="bg-transparent text-xs text-cluster-muted hover:text-cluster-text focus:outline-none cursor-pointer"
                aria-label="Theme"
              >
                <option value="system">System</option>
                <option value="light">Light</option>
                <option value="dark">Dark</option>
              </select>
            </div>
          </div>
        </div>
        <Link
          href="/"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors ${
            pathname === '/' ? 'bg-blue-600 text-white' : 'text-cluster-muted hover:bg-cluster-border/50 hover:text-cluster-text'
          }`}
        >
          <LayoutDashboard className="w-4 h-4" />
          Overview
        </Link>
        <Link
          href="/errors"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors mt-1 ${
            pathname?.startsWith('/errors') ? 'bg-red-600/15 text-red-600 dark:text-red-300' : 'text-cluster-muted hover:bg-cluster-border/50 hover:text-cluster-text'
          }`}
        >
          <Bug className="w-4 h-4" />
          Errors
        </Link>
        <Link
          href="/lb-logs"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors mt-1 ${
            pathname?.startsWith('/lb-logs') ? 'bg-blue-600/15 text-blue-700 dark:text-blue-300' : 'text-cluster-muted hover:bg-cluster-border/50 hover:text-cluster-text'
          }`}
        >
          <Globe className="w-4 h-4" />
          LB Logs
        </Link>
        <Link
          href="/incidents"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors mt-1 ${
            pathname?.startsWith('/incidents') ? 'bg-purple-600/15 text-purple-700 dark:text-purple-300' : 'text-cluster-muted hover:bg-cluster-border/50 hover:text-cluster-text'
          }`}
        >
          <Zap className="w-4 h-4" />
          Incidents & RCA
        </Link>
        <Link
          href="/optimization"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors mt-1 ${
            pathname?.startsWith('/optimization') ? 'bg-green-600/15 text-green-700 dark:text-green-300' : 'text-cluster-muted hover:bg-cluster-border/50 hover:text-cluster-text'
          }`}
        >
          <TrendingDown className="w-4 h-4" />
          Optimization
        </Link>
        <Link
          href="/anomalies"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors mt-1 ${
            pathname?.startsWith('/anomalies') ? 'bg-teal-600/15 text-teal-700 dark:text-teal-300' : 'text-cluster-muted hover:bg-cluster-border/50 hover:text-cluster-text'
          }`}
        >
          <Activity className="w-4 h-4" />
          Anomalies
        </Link>
        <Link
          href="/security"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors mt-1 ${
            pathname?.startsWith('/security') ? 'bg-orange-600/15 text-orange-700 dark:text-orange-300' : 'text-cluster-muted hover:bg-cluster-border/50 hover:text-cluster-text'
          }`}
        >
          <Shield className="w-4 h-4" />
          Security
        </Link>
        <Link
          href="/settings"
          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors mt-1 ${
            pathname?.startsWith('/settings') ? 'bg-cluster-border/60 text-cluster-text' : 'text-cluster-muted hover:bg-cluster-border/50 hover:text-cluster-text'
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
              className="flex items-center gap-2 w-full px-3 py-2 text-sm text-cluster-muted hover:text-cluster-text rounded-md hover:bg-cluster-border/40 transition-colors"
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
                        ? 'bg-blue-600/15 text-blue-700 dark:text-blue-300'
                        : 'text-cluster-muted hover:text-cluster-text hover:bg-cluster-border/40'
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
        className="fixed top-4 left-4 z-50 p-2 rounded-md bg-cluster-card border border-cluster-border text-cluster-text lg:hidden"
      >
        {mobileOpen ? <X className="w-5 h-5" /> : <Menu className="w-5 h-5" />}
      </button>

      {/* Sidebar */}
      <aside
        className={`fixed inset-y-0 left-0 z-40 w-56 bg-cluster-bg border-r border-cluster-border transform transition-transform duration-200 lg:translate-x-0 ${
          mobileOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
      >
        <div className="h-14 flex items-center px-4 border-b border-cluster-border">
          <span className="text-sm font-semibold text-cluster-text">Cluster Intel</span>
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
