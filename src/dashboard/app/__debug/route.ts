import { NextRequest, NextResponse } from 'next/server'

export const runtime = 'nodejs'
export const dynamic = 'force-dynamic'

type DebugPayload = {
  sessionId: string
  runId: string
  hypothesisId: string
  location: string
  message: string
  data?: unknown
  timestamp: number
}

export async function POST(req: NextRequest) {
  let payloads: DebugPayload[] = []
  try {
    const body = await req.json()
    payloads = Array.isArray(body) ? body : [body]
  } catch {
    payloads = []
  }

  await Promise.all(
    payloads.map((p) =>
      fetch('http://127.0.0.1:7416/ingest/73245bf2-0491-4973-825c-e9acc13eda3f', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Debug-Session-Id': 'ae20cc',
        },
        body: JSON.stringify(p),
      }).catch(() => {}),
    ),
  )

  return NextResponse.json({ ok: true, received: payloads.length })
}

