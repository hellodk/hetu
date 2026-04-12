import { NextRequest, NextResponse } from 'next/server'

// Runtime API proxy: /api/v1/{path} → analyzer:8081/api/v1/{path}
// Runs server-side inside the dashboard pod — uses K8s DNS, no CORS.

function getAnalyzerUrl(): string {
  if (process.env.ANALYZER_URL) return process.env.ANALYZER_URL
  const host = process.env.CLUSTER_INTEL_ANALYZER_SERVICE_HOST
  const port = process.env.CLUSTER_INTEL_ANALYZER_SERVICE_PORT_HTTP || process.env.CLUSTER_INTEL_ANALYZER_SERVICE_PORT || '8081'
  if (host) return `http://${host}:${port}`
  return 'http://cluster-intel-analyzer:8081'
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
  const base = getAnalyzerUrl()
  const path = pathSegments.join('/')
  const qs = request.nextUrl.searchParams.toString()
  const url = qs ? `${base}/api/v1/${path}?${qs}` : `${base}/api/v1/${path}`

  try {
    const opts: RequestInit = {
      method: request.method,
      headers: { 'Content-Type': request.headers.get('content-type') || 'application/json' },
    }
    if (request.method !== 'GET' && request.method !== 'HEAD') {
      opts.body = await request.text()
    }

    const resp = await fetch(url, opts)
    const respHeaders = new Headers()
    resp.headers.forEach((v, k) => {
      if (!['transfer-encoding', 'content-encoding'].includes(k.toLowerCase())) {
        respHeaders.set(k, v)
      }
    })

    return new NextResponse(resp.body, { status: resp.status, headers: respHeaders })
  } catch (err: any) {
    return NextResponse.json({ error: `Proxy: ${err.message}`, target: url }, { status: 502 })
  }
}
