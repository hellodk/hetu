// All API calls go through the Next.js proxy route handler.
// Browser calls /api/v1/... → Next.js route handler → analyzer:8081/api/v1/...
// No CORS needed — browser only talks to the dashboard origin.

export function getApiUrl(): string {
  // Empty string = relative URL = same origin = goes through Next.js
  return ''
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
