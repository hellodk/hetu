import { NextRequest, NextResponse } from 'next/server'
import { randomUUID } from 'crypto'
import logger from '@/lib/logger'

// Runtime API proxy: /api/v1/{path} → analyzer:8081/api/v1/{path}
// Runs server-side inside the dashboard pod — uses K8s DNS, no CORS.

function getAnalyzerUrl(): string {
  // Primary: ANALYZER_URL (explicit override)
  if (process.env.ANALYZER_URL) return process.env.ANALYZER_URL
  // Convenience: accept NEXT_PUBLIC_ANALYZER_URL too. The NEXT_PUBLIC_ prefix
  // is Next.js convention for client-accessible vars, but for a tiny dev
  // project it's common for people to set either name. Accepting both here
  // avoids a silent 502 when the proxy falls back to the K8s DNS default.
  if (process.env.NEXT_PUBLIC_ANALYZER_URL) return process.env.NEXT_PUBLIC_ANALYZER_URL
  // K8s service discovery env vars (injected by the scheduler when the
  // dashboard pod and the cluster-intel-analyzer Service are in the same ns)
  const host = process.env.HETU_ANALYZER_SERVICE_HOST
  const port = process.env.HETU_ANALYZER_SERVICE_PORT_HTTP || process.env.HETU_ANALYZER_SERVICE_PORT || '8081'
  if (host) return `http://${host}:${port}`
  // Last resort: K8s DNS by service name (works inside the cluster,
  // fails locally — which is why the explicit env vars exist).
  return 'http://hetu-analyzer:8081'
}

// Next.js 15+ changed route handler params to be async (Promise-based).
type RouteContext = { params: Promise<{ path: string[] }> }

export async function GET(req: NextRequest, ctx: RouteContext) {
  return proxy(req, (await ctx.params).path)
}
export async function POST(req: NextRequest, ctx: RouteContext) {
  return proxy(req, (await ctx.params).path)
}
export async function PUT(req: NextRequest, ctx: RouteContext) {
  return proxy(req, (await ctx.params).path)
}
export async function PATCH(req: NextRequest, ctx: RouteContext) {
  return proxy(req, (await ctx.params).path)
}
export async function DELETE(req: NextRequest, ctx: RouteContext) {
  return proxy(req, (await ctx.params).path)
}

async function proxy(request: NextRequest, pathSegments: string[]) {
  const requestId = request.headers.get('x-request-id') ?? randomUUID()
  const base = getAnalyzerUrl()
  const path = pathSegments.join('/')
  const qs = request.nextUrl.searchParams.toString()
  const url = qs ? `${base}/api/v1/${path}?${qs}` : `${base}/api/v1/${path}`
  const start = Date.now()

  try {
    const opts: RequestInit = {
      method: request.method,
      headers: {
        'Content-Type': request.headers.get('content-type') || 'application/json',
        'X-Request-ID': requestId,
      },
    }
    if (request.method !== 'GET' && request.method !== 'HEAD') {
      opts.body = await request.text()
    }

    const resp = await fetch(url, opts)
    const duration = Date.now() - start

    logger.info({ method: request.method, path: `/api/v1/${path}`, status: resp.status, duration_ms: duration, request_id: requestId }, 'proxy request')

    const respHeaders = new Headers()
    resp.headers.forEach((v, k) => {
      if (!['transfer-encoding', 'content-encoding'].includes(k.toLowerCase())) {
        respHeaders.set(k, v)
      }
    })
    respHeaders.set('X-Request-ID', requestId)

    return new NextResponse(resp.body, { status: resp.status, headers: respHeaders })
  } catch (err: any) {
    const duration = Date.now() - start
    const message = err?.message ?? String(err) ?? 'unknown error'
    logger.error({ err: message, target: url, duration_ms: duration, request_id: requestId }, 'proxy error')
    return NextResponse.json({ error: `Proxy: ${message}`, target: url }, { status: 502 })
  }
}
