import { NextRequest, NextResponse } from 'next/server'
import logger from '@/lib/logger'

export async function POST(req: NextRequest) {
  try {
    const { message, stack, componentStack } = await req.json()
    logger.error({ source: 'client', message, stack, componentStack }, 'client-side render error')
    return NextResponse.json({ ok: true })
  } catch {
    return NextResponse.json({ ok: false }, { status: 400 })
  }
}
