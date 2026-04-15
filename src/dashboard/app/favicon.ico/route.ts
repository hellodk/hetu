import { NextRequest, NextResponse } from 'next/server'

export const runtime = 'nodejs'

export function GET(_req: NextRequest) {
  // Serve the same SVG as the app icon. Browsers that request /favicon.ico
  // are happy with an image response; this avoids host/redirect quirks in dev.
  const svg = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128">
  <defs>
    <linearGradient id="g" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#3b82f6"/>
      <stop offset="1" stop-color="#8b5cf6"/>
    </linearGradient>
  </defs>
  <rect x="8" y="8" width="112" height="112" rx="28" fill="url(#g)"/>
  <g fill="none" stroke="#ffffff" stroke-width="8" stroke-linecap="round" stroke-linejoin="round">
    <path d="M40 66l16 16 32-36"/>
    <path d="M36 44c8-10 18-16 28-16s20 6 28 16" opacity="0.85"/>
  </g>
</svg>`

  return new NextResponse(svg, {
    headers: {
      'Content-Type': 'image/svg+xml; charset=utf-8',
      'Cache-Control': 'public, max-age=86400',
    },
  })
}

