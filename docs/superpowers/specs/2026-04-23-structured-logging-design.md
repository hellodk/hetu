# Structured Logging & Correlation Design

**Date:** 2026-04-23
**Status:** Approved

---

## Problem

The three services have inconsistent, incomplete logging:

- **Analyzer & Collector** use `zerolog` but always run `ConsoleWriter` (human-readable), have no configurable log level, no HTTP request logging, and no correlation IDs.
- **Dashboard** has 7 scattered `console.error` calls and no server-side structured logger. The API proxy route logs nothing.
- No correlation ID threads events across the collector → NATS → analyzer → dashboard chain, making troubleshooting a specific event impossible without grepping raw timestamps.

---

## Goal

Production-ready structured logging across all three services with a single `request_id` that can be grepped in a log aggregator (Loki, Datadog) to trace an event end-to-end.

---

## Scope

- No API or data model changes
- No client-side JS bundle changes (server-side Next.js only)
- Dark/light theme and UI unaffected
- Does not introduce OpenTelemetry tracing (separate project)

---

## Architecture

```
Collector (HTTP in / K8s watch)
  └─ generate request_id → inject into zerolog context
  └─ publish to NATS with X-Request-ID header
        │
        ▼
Analyzer (NATS subscriber / HTTP API)
  └─ extract X-Request-ID from NATS header → inject into zerolog context
  └─ all downstream log calls carry request_id automatically
  └─ HTTP middleware: generate/forward X-Request-ID on responses
        │
        ▼
Dashboard Next.js (API proxy /api/v1/[...path])
  └─ forward X-Request-ID to analyzer, log it with pino
  └─ structured server-side logs: method, path, status, duration_ms, request_id
```

---

## Section 1 — Shared Go logger initialisation (`pkg/logger`)

### New files
- `pkg/logger/logger.go` — `Init(level, format string) zerolog.Logger`
- `pkg/logger/middleware.go` — HTTP middleware

### `Init` behaviour
| `LOG_LEVEL` | Default | Accepted values |
|---|---|---|
| env var | `info` | `debug`, `info`, `warn`, `error` |

| `LOG_FORMAT` | Default | Accepted values |
|---|---|---|
| env var | `json` | `json`, `pretty` |

- `json`: writes newline-delimited JSON to `stderr` (production)
- `pretty`: uses `zerolog.ConsoleWriter` (local dev)
- Both services call `Init` once in `main()`, replacing the current inline setup

### Impact on existing log calls
Zero — all existing `log.Info()`, `log.Error()` etc. continue to work. `Init` sets the global logger via `log.Logger = ...`.

---

## Section 2 — HTTP middleware (`pkg/logger/middleware.go`)

```
RequestLogger(next http.Handler) http.Handler
```

Per-request behaviour:
1. Read `X-Request-ID` header; if absent generate a UUID v4
2. Build a child logger: `log.With().Str("request_id", id).Logger()`
3. Store child logger in `context` via `zerolog`'s `log.Ctx` / `zerolog.Ctx` API
4. Call `next` with the enriched context
5. After response: log one `info` line with `method`, `path`, `status`, `duration_ms`, `request_id`
6. Write `X-Request-ID` to response header

Registered in:
- `analyzer/main.go` — wraps the existing `http.ServeMux`
- `collector/main.go` — wraps the health/metrics `http.ServeMux`

### Using the context logger in handlers
Any handler that wants request-scoped logging:
```go
log.Ctx(r.Context()).Info().Str("incident", id).Msg("RCA started")
```
Falls back to global logger if context has no logger (safe).

---

## Section 3 — NATS correlation

### Collector side (`src/collector/lblogs.go`, `podlogs.go`)
- When publishing a NATS message, attach header:
  ```go
  msg := nats.NewMsg(subject)
  msg.Header.Set("X-Request-ID", requestID)
  msg.Data = payload
  nc.PublishMsg(msg)
  ```
- `requestID` sourced from the zerolog context of the current goroutine; if absent, generate fresh UUID

### Analyzer side (NATS subscription handlers)
- On message receipt, extract header:
  ```go
  id := msg.Header.Get("X-Request-ID")
  if id == "" { id = uuid.NewString() }
  ctx := log.With().Str("request_id", id).Logger().WithContext(context.Background())
  ```
- Pass `ctx` into all downstream processing functions (RCA, vectorstore, correlator, etc.)
- Downstream functions use `log.Ctx(ctx)` — no signature changes required unless the function already takes a `context.Context` (most do)

---

## Section 4 — Dashboard server-side logging (pino)

### Dependencies
```json
"pino": "^9",
"pino-pretty": "^13"  // devDependency only
```

### `src/dashboard/lib/logger.ts`
```typescript
import pino from 'pino'

const logger = pino({
  level: process.env.LOG_LEVEL ?? 'info',
  ...(process.env.NODE_ENV !== 'production' && {
    transport: { target: 'pino-pretty' },
  }),
})

export default logger
```

### API proxy (`app/api/v1/[...path]/route.ts`)
- At request start: read `X-Request-ID` from incoming headers; if absent generate UUID
- Forward `X-Request-ID` to the upstream analyzer fetch
- Log one line on completion:
  ```
  { level: 'info', method, path, status, duration_ms, request_id }
  ```
- On proxy error (currently silent `catch`): log `error` with `err.message`, `target`, `request_id`

### `console.error` calls — scope note
All 6 files with `console.error` are `"use client"` components — they run in the browser and cannot import a Node.js logger. These are left as `console.error` (still visible in DevTools and acceptable for an internal tool). The only server-side change is the proxy route and error endpoint below.

### Client error boundary (`app/api/log-client-error/route.ts`)
- `POST` endpoint accepting `{ message, stack, componentStack }`
- Logs via pino at `error` level with `source: 'client'`
- `app/error.tsx` already has `console.error` — also updated to POST here so React render crashes are captured server-side alongside the browser console output

---

## New environment variables

| Service | Variable | Default | Description |
|---|---|---|---|
| Analyzer | `LOG_LEVEL` | `info` | `debug\|info\|warn\|error` |
| Analyzer | `LOG_FORMAT` | `json` | `json\|pretty` |
| Collector | `LOG_LEVEL` | `info` | `debug\|info\|warn\|error` |
| Collector | `LOG_FORMAT` | `json` | `json\|pretty` |
| Dashboard | `LOG_LEVEL` | `info` | pino level |

All three added to `values-dev.yaml` (pretty/debug) and `deploy/helm/cluster-intel/values.yaml` (json/info).

---

## Files changed

| Action | Path |
|---|---|
| **Create** | `pkg/logger/logger.go` |
| **Create** | `pkg/logger/middleware.go` |
| **Modify** | `src/analyzer/main.go` |
| **Modify** | `src/collector/main.go` |
| **Modify** | `src/collector/lblogs.go` |
| **Modify** | `src/collector/podlogs.go` |
| **Modify** | `pkg/bus/nats.go` |
| **Create** | `src/dashboard/lib/logger.ts` |
| **Modify** | `src/dashboard/app/api/v1/[...path]/route.ts` |
| **Create** | `src/dashboard/app/api/log-client-error/route.ts` |
| **Modify** | `src/dashboard/app/error.tsx` |
| **Modify** | `src/dashboard/package.json` |
| **Modify** | `values-dev.yaml` |
| **Modify** | `deploy/helm/cluster-intel/values.yaml` |

---

## Testing

1. Start dev server with `LOG_FORMAT=pretty LOG_LEVEL=debug` — verify pretty output in terminal
2. Set `LOG_FORMAT=json` — verify newline-delimited JSON on stderr
3. Make an HTTP request to the analyzer — verify one log line with `request_id`, `method`, `path`, `status`, `duration_ms`
4. Trigger a pod log event — verify same `request_id` appears in collector publish log and analyzer receive log
5. Open dashboard → make a proxied API call — verify `X-Request-ID` header forwarded and logged by pino
6. Trigger an SSE error — verify structured `logger.error` output instead of `console.error`
