// All REST API calls go through the Next.js proxy route handler.
// Browser calls /api/v1/... → Next.js route handler → analyzer/api/v1/...
// No CORS needed for REST — the browser only talks to the dashboard origin.
//
// WebSocket connections CANNOT be proxied by Next.js route handlers, so they
// connect directly to the analyzer. The analyzer URL is injected at runtime
// by the server layout into window.__HETU_API__.

// BASE_PATH is the Next.js basePath the app is served under (e.g. "/hetu").
// It is inlined at build time from next.config.js `env`. All browser REST calls
// must be prefixed with it: Next.js does NOT rewrite raw `fetch('/api/..')`
// paths, so an unprefixed call hits the origin root and 404s (the API route
// handler lives under `${BASE_PATH}/api/v1/*`).
export const BASE_PATH = process.env.NEXT_PUBLIC_BASE_PATH ?? ''

export function getApiUrl(): string {
  // REST calls are served by the Next.js proxy route handler mounted under the
  // basePath, so return the basePath (relative to the current origin). Callers
  // build `${getApiUrl()}/api/v1/...`.
  return BASE_PATH
}

// getWsUrl returns the WebSocket base URL for direct analyzer connections.
// WebSocket can't go through the Next.js proxy, so we need the direct URL.
export function getWsUrl(): string {
  if (typeof window === 'undefined') return ''
  const api = (window as any).__HETU_API__ || ''
  if (api) {
    return api.replace(/^http/, 'ws')
  }
  // Fallback: same host, default analyzer port
  return `ws://${window.location.hostname}:18081`
}

export async function apiFetch<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE_PATH}${path}`)
  if (!res.ok) {
    throw new Error(`API ${path}: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function apiFetchText(path: string): Promise<string> {
  const res = await fetch(`${BASE_PATH}${path}`)
  if (!res.ok) {
    throw new Error(`API ${path}: ${res.status} ${res.statusText}`)
  }
  return res.text()
}
