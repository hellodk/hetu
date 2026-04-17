'use client'

import { useEffect, useRef, useState } from 'react'
import { apiFetch, getApiUrl } from '@/lib/api'
import {
  RefreshCw, Loader2, CheckCircle, XCircle,
  Server, Brain, Save, RotateCcw, Key, Search, Palette, ChevronDown
} from 'lucide-react'

interface LLMConfig {
  provider: string
  endpoint: string
  model: string
  maxTokens: number
  temperature: number
  dailyTokenBudget: number
  apiKeySet: boolean
  explainOptimizations: boolean
}

interface ProviderInfo {
  id: string
  name: string
  defaultEndpoint: string
  defaultModel: string
  requiresApiKey: boolean
  description: string
}

interface Capabilities {
  exec: boolean
  writeActions: boolean
}

type ThemeChoice = 'graphite' | 'calm-signal' | 'aurora' | 'prism' | 'auto' | 'md-dark' | 'md-light'

const THEMES: { id: ThemeChoice; label: string; bg: string; accent: string; dark: boolean }[] = [
  { id: 'graphite',    label: 'Graphite',       bg: '#F5F3EE', accent: '#2732A8', dark: false },
  { id: 'calm-signal', label: 'Calm Signal',    bg: '#07080B', accent: '#7C6CFF', dark: true  },
  { id: 'aurora',      label: 'Aurora',         bg: '#06060E', accent: '#5CF2E1', dark: true  },
  { id: 'prism',       label: 'Prism',          bg: '#FFFFFF', accent: '#8B5CF6', dark: false },
  { id: 'md-dark',     label: 'Material Dark',  bg: '#1C1B1F', accent: '#D0BCFF', dark: true  },
  { id: 'md-light',    label: 'Material Light', bg: '#FFFBFE', accent: '#6750A4', dark: false },
  { id: 'auto',        label: 'System',         bg: 'linear-gradient(135deg, #F5F3EE 50%, #07080B 50%)', accent: '#888', dark: false },
]

export default function SettingsPage() {
  const [llmConfig, setLlmConfig] = useState<LLMConfig | null>(null)
  const [providers, setProviders] = useState<ProviderInfo[]>([])
  const [capabilities, setCapabilities] = useState<Capabilities | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [apiKey, setApiKey] = useState('')
  const [theme, setThemeState] = useState<ThemeChoice>('graphite')
  const [themeDropdownOpen, setThemeDropdownOpen] = useState(false)
  const themeDropdownRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const stored = localStorage.getItem('ci_theme') as ThemeChoice | null
    if (stored && THEMES.some(t => t.id === stored)) setThemeState(stored)
  }, [])

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (themeDropdownRef.current && !themeDropdownRef.current.contains(e.target as Node)) {
        setThemeDropdownOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const applyTheme = (choice: ThemeChoice) => {
    setThemeState(choice)
    try { localStorage.setItem('ci_theme', choice) } catch { /* ignore */ }
    // Resolve 'auto' same way Navigation.tsx does
    const resolved = choice === 'auto'
      ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'calm-signal' : 'graphite')
      : choice
    document.documentElement.setAttribute('data-theme', resolved)
    document.documentElement.classList.toggle('dark', !['graphite', 'prism', 'md-light'].includes(resolved))
  }

  // Editable form state (separate from server state)
  const [form, setForm] = useState<LLMConfig | null>(null)

  // Model discovery
  const [discoveredModels, setDiscoveredModels] = useState<{ id: string; name: string; size?: string; description?: string }[]>([])
  const [discovering, setDiscovering] = useState(false)
  const [discoverError, setDiscoverError] = useState<string | null>(null)

  useEffect(() => {
    Promise.all([
      apiFetch<LLMConfig>('/api/v1/llm/config').catch(() => null),
      apiFetch<{ providers: ProviderInfo[] }>('/api/v1/llm/providers').catch(() => ({ providers: [] })),
      apiFetch<Capabilities>('/api/v1/k8s/capabilities').catch(() => null),
    ]).then(([cfg, prov, caps]) => {
      setLlmConfig(cfg)
      setForm(cfg)
      setProviders(prov.providers)
      setCapabilities(caps)
      setLoading(false)
    })
  }, [])

  const onProviderChange = (providerId: string) => {
    const provider = providers.find(p => p.id === providerId)
    if (provider && form) {
      setForm({
        ...form,
        provider: providerId,
        endpoint: provider.defaultEndpoint,
        model: provider.defaultModel,
      })
      setDiscoveredModels([])
      setDiscoverError(null)
    }
  }

  const discoverModels = async (endpoint?: string, provider?: string) => {
    if (!form) return
    const ep = endpoint || form.endpoint
    const prov = provider || form.provider
    if (!ep) return
    setDiscovering(true)
    setDiscoverError(null)
    try {
      const res = await fetch(`${getApiUrl()}/api/v1/llm/discover-models`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ provider: prov, endpoint: ep, apiKey: apiKey || undefined }),
      })
      const data = await res.json()
      if (data.error) {
        setDiscoverError(data.error)
        setDiscoveredModels([])
      } else {
        setDiscoveredModels(data.models || [])
        setDiscoverError(null)
        // Auto-select first model if current model is not in the list
        if (data.models?.length > 0) {
          const currentInList = data.models.some((m: any) => m.id === form.model)
          if (!currentInList) {
            setForm(prev => prev ? { ...prev, model: data.models[0].id } : null)
          }
        }
      }
    } catch (e: any) {
      setDiscoverError(e.message)
      setDiscoveredModels([])
    } finally {
      setDiscovering(false)
    }
  }

  const saveConfig = async () => {
    if (!form) return
    setSaving(true)
    setError(null)
    setSaved(false)
    try {
      const body: any = { ...form }
      if (apiKey) body.apiKey = apiKey
      const res = await fetch(`${getApiUrl()}/api/v1/llm/config`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) throw new Error(await res.text())
      const updated = await res.json()
      setLlmConfig(updated)
      setForm(updated)
      setApiKey('')
      setSaved(true)
      setTimeout(() => setSaved(false), 3000)
    } catch (e: any) {
      setError(e.message)
    } finally {
      setSaving(false)
    }
  }

  const resetForm = () => {
    setForm(llmConfig)
    setApiKey('')
    setError(null)
  }

  const hasChanges = form && llmConfig && (
    form.provider !== llmConfig.provider ||
    form.endpoint !== llmConfig.endpoint ||
    form.model !== llmConfig.model ||
    form.maxTokens !== llmConfig.maxTokens ||
    form.temperature !== llmConfig.temperature ||
    form.dailyTokenBudget !== llmConfig.dailyTokenBudget ||
    apiKey !== ''
  )

  const selectedProvider = providers.find(p => p.id === form?.provider)

  if (loading) {
    return <div className="flex justify-center items-center min-h-[50vh]"><Loader2 className="w-6 h-6 animate-spin text-blue-400" /></div>
  }

  return (
    <div className="p-6 max-w-4xl">
      <h1 className="text-2xl font-bold text-cluster-text mb-6">Settings</h1>

      {/* LLM Configuration */}
      <div className="bg-cluster-card border border-cluster-border rounded-lg p-6 mb-6">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-cluster-text flex items-center gap-2">
            <Brain className="w-5 h-5 text-purple-400" />
            LLM Configuration
          </h2>
          {saved && (
            <span className="flex items-center gap-1.5 text-sm text-green-400">
              <CheckCircle className="w-4 h-4" /> Saved
            </span>
          )}
        </div>

        {error && (
          <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded text-sm text-red-300">{error}</div>
        )}

        {form && (
          <div className="space-y-4">
            {/* Provider selector */}
            <div>
              <label className="block text-sm text-cluster-muted mb-1.5">Provider</label>
              <select
                value={form.provider}
                onChange={e => onProviderChange(e.target.value)}
                className="w-full bg-cluster-bg border border-cluster-border rounded-lg px-3 py-2.5 text-cluster-text text-sm"
              >
                {providers.map(p => (
                  <option key={p.id} value={p.id}>{p.name}</option>
                ))}
              </select>
              {selectedProvider && (
                <p className="mt-1 text-xs text-cluster-muted/80">{selectedProvider.description}</p>
              )}
            </div>

            {/* Endpoint + Discover */}
            <div>
              <label className="block text-sm text-cluster-muted mb-1.5">Endpoint URL</label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={form.endpoint}
                  onChange={e => { setForm({ ...form, endpoint: e.target.value }); setDiscoveredModels([]); }}
                  className="flex-1 bg-cluster-bg border border-cluster-border rounded-lg px-3 py-2.5 text-cluster-text text-sm font-mono"
                  placeholder="https://api.example.com"
                />
                <button
                  onClick={() => discoverModels()}
                  disabled={discovering || !form.endpoint}
                  className="flex items-center gap-1.5 px-3 py-2.5 text-sm bg-purple-600 hover:bg-purple-500 text-white rounded-lg disabled:opacity-40 transition-colors whitespace-nowrap"
                >
                  {discovering ? <Loader2 className="w-4 h-4 animate-spin" /> : <Search className="w-4 h-4" />}
                  {discovering ? 'Probing...' : 'Fetch Models'}
                </button>
              </div>
              {discoverError && (
                <p className="mt-1.5 text-xs text-red-400">{discoverError}</p>
              )}
            </div>

            {/* Model — dropdown if models discovered, text input otherwise */}
            <div>
              <label className="block text-sm text-cluster-muted mb-1.5">
                Model
                {discoveredModels.length > 0 && (
                  <span className="ml-2 text-xs text-green-500">({discoveredModels.length} models found)</span>
                )}
              </label>
              {discoveredModels.length > 0 ? (
                <select
                  value={form.model}
                  onChange={e => setForm({ ...form, model: e.target.value })}
                  className="w-full bg-cluster-bg border border-cluster-border rounded-lg px-3 py-2.5 text-cluster-text text-sm font-mono"
                >
                  {discoveredModels.map(m => (
                    <option key={m.id} value={m.id}>
                      {m.name}{m.size ? ` (${m.size})` : ''}{m.description ? ` — ${m.description}` : ''}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  type="text"
                  value={form.model}
                  onChange={e => setForm({ ...form, model: e.target.value })}
                  className="w-full bg-cluster-bg border border-cluster-border rounded-lg px-3 py-2.5 text-cluster-text text-sm font-mono"
                  placeholder="model-name — or click Fetch Models to discover"
                />
              )}
            </div>

            {/* API Key */}
            <div>
              <label className="block text-sm text-cluster-muted mb-1.5 flex items-center gap-1.5">
                <Key className="w-3.5 h-3.5" />
                API Key
                {selectedProvider && !selectedProvider.requiresApiKey && (
                  <span className="text-xs text-cluster-muted/70">(optional for {selectedProvider.name})</span>
                )}
              </label>
              <div className="relative">
                <input
                  type="password"
                  value={apiKey}
                  onChange={e => setApiKey(e.target.value)}
                  className="w-full bg-cluster-bg border border-cluster-border rounded-lg px-3 py-2.5 text-cluster-text text-sm font-mono"
                  placeholder={form.apiKeySet ? '••••••••••••••• (already set)' : 'Enter API key'}
                />
                {form.apiKeySet && !apiKey && (
                  <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-green-500">configured</span>
                )}
              </div>
            </div>

            {/* Advanced settings row */}
            <div className="grid grid-cols-3 gap-4">
              <div>
                <label className="block text-sm text-cluster-muted mb-1.5">Max Tokens</label>
                <input
                  type="number"
                  value={form.maxTokens}
                  onChange={e => setForm({ ...form, maxTokens: parseInt(e.target.value) || 0 })}
                  className="w-full bg-cluster-bg border border-cluster-border rounded-lg px-3 py-2.5 text-cluster-text text-sm"
                  min={256}
                  max={128000}
                />
              </div>
              <div>
                <label className="block text-sm text-cluster-muted mb-1.5">Temperature</label>
                <input
                  type="number"
                  value={form.temperature}
                  onChange={e => setForm({ ...form, temperature: parseFloat(e.target.value) || 0 })}
                  className="w-full bg-cluster-bg border border-cluster-border rounded-lg px-3 py-2.5 text-cluster-text text-sm"
                  min={0}
                  max={2}
                  step={0.1}
                />
              </div>
              <div>
                <label className="block text-sm text-cluster-muted mb-1.5">Daily Token Budget</label>
                <input
                  type="number"
                  value={form.dailyTokenBudget}
                  onChange={e => setForm({ ...form, dailyTokenBudget: parseInt(e.target.value) || 0 })}
                  className="w-full bg-cluster-bg border border-cluster-border rounded-lg px-3 py-2.5 text-cluster-text text-sm"
                  min={0}
                  step={100000}
                />
                <p className="mt-1 text-xs text-cluster-muted/70">0 = unlimited</p>
              </div>
            </div>

            {/* Save / Reset buttons */}
            <div className="flex items-center gap-3 pt-2">
              <button
                onClick={saveConfig}
                disabled={saving || !hasChanges}
                className="flex items-center gap-1.5 px-4 py-2 text-sm bg-blue-600 hover:bg-blue-500 text-white rounded-lg disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
              >
                {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
                {saving ? 'Saving...' : 'Save Changes'}
              </button>
              {hasChanges && (
                <button
                  onClick={resetForm}
                  className="flex items-center gap-1.5 px-3 py-2 text-sm text-cluster-muted hover:text-cluster-text transition-colors"
                >
                  <RotateCcw className="w-4 h-4" />
                  Reset
                </button>
              )}
              <span className="flex-1" />
              <span className="text-xs text-cluster-muted/70">
                Runtime config — persists until pod restart. Update Helm values for permanent changes.
              </span>
            </div>
          </div>
        )}

        {!form && (
          <div className="text-center py-6 text-cluster-muted">
            LLM configuration not available. Check analyzer connection.
          </div>
        )}
      </div>

      {/* Theme */}
      <div className="bg-cluster-card border border-cluster-border rounded-lg p-6 mb-6">
        <h2 className="text-lg font-semibold text-cluster-text flex items-center gap-2 mb-4">
          <Palette className="w-5 h-5 text-purple-400" />
          Appearance
        </h2>
        <div ref={themeDropdownRef} className="relative max-w-xs">
          {/* Trigger */}
          <button
            type="button"
            onClick={() => setThemeDropdownOpen(v => !v)}
            className="w-full flex items-center gap-3 px-3 py-2.5 bg-cluster-bg border border-cluster-border rounded-lg text-sm text-cluster-text hover:border-cluster-text/40 transition-colors"
            aria-haspopup="listbox"
            aria-expanded={themeDropdownOpen}
          >
            {(() => {
              const t = THEMES.find(t => t.id === theme) ?? THEMES[0]
              return (
                <>
                  <span
                    className="w-5 h-5 rounded-full border border-black/10 shrink-0 flex items-center justify-center"
                    style={{ background: t.bg }}
                  >
                    <span className="w-2.5 h-2.5 rounded-full" style={{ background: t.accent }} />
                  </span>
                  <span className="flex-1 text-left">{t.label}</span>
                </>
              )
            })()}
            <ChevronDown className={`w-4 h-4 text-cluster-muted transition-transform duration-150 ${themeDropdownOpen ? 'rotate-180' : ''}`} aria-hidden="true" />
          </button>

          {/* Options list */}
          {themeDropdownOpen && (
            <ul
              role="listbox"
              aria-label="Theme"
              className="absolute top-full left-0 right-0 mt-1 bg-cluster-card border border-cluster-border rounded-lg shadow-lg z-20 overflow-hidden"
            >
              {THEMES.map(t => (
                <li key={t.id} role="option" aria-selected={theme === t.id}>
                  <button
                    type="button"
                    onClick={() => { applyTheme(t.id as ThemeChoice); setThemeDropdownOpen(false) }}
                    className={`w-full flex items-center gap-3 px-3 py-2.5 text-sm transition-colors hover:bg-cluster-border/30 ${
                      theme === t.id ? 'text-cluster-text font-medium' : 'text-cluster-muted'
                    }`}
                  >
                    <span
                      className="w-5 h-5 rounded-full border border-black/10 shrink-0 flex items-center justify-center"
                      style={{ background: t.bg }}
                    >
                      <span className="w-2.5 h-2.5 rounded-full" style={{ background: t.accent }} />
                    </span>
                    <span className="flex-1 text-left">{t.label}</span>
                    {theme === t.id && <CheckCircle className="w-3.5 h-3.5 text-blue-400 shrink-0" aria-hidden="true" />}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
        <p className="text-xs text-cluster-muted mt-3">
          Saved to browser — no server restart needed.
        </p>
      </div>

      {/* Cluster Capabilities */}
      <div className="bg-cluster-card border border-cluster-border rounded-lg p-6">
        <h2 className="text-lg font-semibold text-cluster-text flex items-center gap-2 mb-4">
          <Server className="w-5 h-5 text-blue-400" />
          Cluster Capabilities
        </h2>
        <div className="grid grid-cols-2 gap-4">
          <div className="flex items-center gap-3 p-3 bg-cluster-bg/60 border border-cluster-border rounded-lg">
            {capabilities?.exec ? <CheckCircle className="w-5 h-5 text-green-400" /> : <XCircle className="w-5 h-5 text-gray-600" />}
            <div>
              <div className="text-sm text-cluster-text">Pod Exec</div>
              <div className="text-xs text-cluster-muted">{capabilities?.exec ? 'Enabled' : 'Disabled'}</div>
            </div>
          </div>
          <div className="flex items-center gap-3 p-3 bg-cluster-bg/60 border border-cluster-border rounded-lg">
            {capabilities?.writeActions ? <CheckCircle className="w-5 h-5 text-green-400" /> : <XCircle className="w-5 h-5 text-gray-600" />}
            <div>
              <div className="text-sm text-cluster-text">Write Actions</div>
              <div className="text-xs text-cluster-muted">{capabilities?.writeActions ? 'Enabled (scale/restart/delete)' : 'Disabled'}</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
