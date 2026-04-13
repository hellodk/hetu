// All REST API calls go through the Next.js proxy route handler.
// Browser calls /api/v1/... → Next.js route handler → analyzer/api/v1/...
// No CORS needed for REST — the browser only talks to the dashboard origin.
//
// WebSocket connections CANNOT be proxied by Next.js route handlers, so they
// connect directly to the analyzer. The analyzer URL is injected at runtime
// by the server layout into window.__CLUSTER_INTEL_API__.

export function getApiUrl(): string {
  // For REST calls, use relative URLs (proxied by Next.js)
  return ''
}

// getWsUrl returns the WebSocket base URL for direct analyzer connections.
// WebSocket can't go through the Next.js proxy, so we need the direct URL.
export function getWsUrl(): string {
  if (typeof window === 'undefined') return ''
  const api = (window as any).__CLUSTER_INTEL_API__ || ''
  if (api) {
    return api.replace(/^http/, 'ws')
  }
  // Fallback: same host, default analyzer port
  return `ws://${window.location.hostname}:18081`
}

export async function apiFetch<T>(path: string): Promise<T> {
  const res = await fetch(path)
  if (!res.ok) {
    throw new Error(`API ${path}: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function apiFetchText(path: string): Promise<string> {
  const res = await fetch(path)
  if (!res.ok) {
    throw new Error(`API ${path}: ${res.status} ${res.statusText}`)
  }
  return res.text()
}
