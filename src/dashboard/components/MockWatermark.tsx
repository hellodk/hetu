'use client'

export function MockWatermark() {
  return (
    <div
      className="pointer-events-none fixed inset-0 z-[60] overflow-hidden"
      aria-hidden="true"
    >
      {/* Repeating tiled watermark (hard to miss in screenshots) */}
      <div className="absolute inset-[-25%] opacity-[0.20]">
        <div className="h-full w-full rotate-[-24deg]">
          <div
            className="grid gap-x-14 gap-y-10"
            style={{ gridTemplateColumns: 'repeat(4, minmax(0, 1fr))' }}
          >
            {Array.from({ length: 36 }).map((_, i) => (
              <div
                // eslint-disable-next-line react/no-array-index-key
                key={i}
                className="select-none text-center text-[32px] sm:text-[44px] md:text-[56px] font-black tracking-[0.22em] text-amber-200 drop-shadow-[0_0_18px_rgba(245,158,11,0.28)]"
              >
                DEMO
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Corner label (always visible even on bright charts) */}
      <div className="absolute top-4 left-4 opacity-[0.75]">
        <div className="select-none rounded-md border border-amber-500/40 bg-amber-500/15 px-3 py-1.5 text-xs font-bold tracking-[0.22em] text-amber-200">
          MOCK DATA
        </div>
      </div>
    </div>
  )
}

