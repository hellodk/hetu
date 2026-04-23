# Structured Logging & Correlation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add configurable structured logging (JSON/pretty, log levels) with a `request_id` correlation header that flows from collector → NATS → analyzer → dashboard, enabling end-to-end troubleshooting in a log aggregator.

**Architecture:** New `pkg/logger` Go module provides `Init(level, format)` and an HTTP middleware that seeds/echoes `X-Request-ID`. `pkg/bus` reads the request ID from context and attaches it as a NATS message header. The dashboard gains a `pino` server-side logger that forwards `X-Request-ID` on every proxied request. All existing `log.X()` calls continue unchanged — only initialisation and middleware wiring change.

**Tech Stack:** Go 1.25, zerolog v1.35, google/uuid v1.3, Next.js 16, pino v9.

---

## File Map

| Action | Path | Responsibility |
|---|---|---|
| **Create** | `pkg/logger/go.mod` | standalone Go module declaration |
| **Create** | `pkg/logger/logger.go` | `Init`, `WithRequestID`, `RequestIDFromContext` |
| **Create** | `pkg/logger/logger_test.go` | unit tests for Init and context helpers |
| **Create** | `pkg/logger/middleware.go` | `RequestLogger` HTTP middleware |
| **Create** | `pkg/logger/middleware_test.go` | unit tests for middleware |
| **Modify** | `pkg/bus/go.mod` | add `pkg/logger` require + replace |
| **Modify** | `pkg/bus/nats.go` | attach X-Request-ID header on Publish |
| **Modify** | `src/analyzer/go.mod` | add `pkg/logger` require + replace |
| **Modify** | `src/analyzer/main.go` | call `logger.Init`, wrap API mux with middleware |
| **Modify** | `src/collector/go.mod` | add `pkg/logger` require + replace |
| **Modify** | `src/collector/main.go` | call `logger.Init`, wrap health mux with middleware |
| **Create** | `src/dashboard/lib/logger.ts` | pino singleton (JSON prod / pretty dev) |
| **Modify** | `src/dashboard/app/api/v1/[...path]/route.ts` | log every proxy request with request_id |
| **Create** | `src/dashboard/app/api/log-client-error/route.ts` | receive client-side error POSTs |
| **Modify** | `src/dashboard/app/error.tsx` | POST to log-client-error on React render crash |
| **Modify** | `src/dashboard/package.json` | add pino, pino-pretty |
| **Modify** | `values-dev.yaml` | add LOG_LEVEL/LOG_FORMAT/NODE_LOG_LEVEL per component |
| **Modify** | `deploy/helm/cluster-intel/values.yaml` | same env vars, production defaults |

---

## Task 1 — Create `pkg/logger` module (Init + context helpers)

**Files:**
- Create: `pkg/logger/go.mod`
- Create: `pkg/logger/logger.go`
- Create: `pkg/logger/logger_test.go`

- [ ] **Step 1: Write the failing tests**

Create `pkg/logger/logger_test.go`:

```go
package logger_test

import (
	"context"
	"testing"

	"github.com/hellodk/hetu/pkg/logger"
)

func TestInit_ValidLevel(t *testing.T) {
	// Should not panic for any valid level/format combination.
	for _, level := range []string{"debug", "info", "warn", "error"} {
		for _, format := range []string{"json", "pretty"} {
			logger.Init(level, format) // must not panic
		}
	}
}

func TestInit_InvalidLevelFallsBackToInfo(t *testing.T) {
	// Invalid level must not panic; zerolog falls back to info.
	logger.Init("garbage", "json")
}

func TestRequestID_RoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = logger.WithRequestID(ctx, "test-123")
	if got := logger.RequestIDFromContext(ctx); got != "test-123" {
		t.Fatalf("got %q, want %q", got, "test-123")
	}
}

func TestRequestIDFromContext_Empty(t *testing.T) {
	if got := logger.RequestIDFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
cd pkg/logger && go test ./... 2>&1 | head -5
```

Expected: `cannot find package` or build error (module doesn't exist yet).

- [ ] **Step 3: Create the Go module file**

Create `pkg/logger/go.mod`:

```
module github.com/hellodk/hetu/pkg/logger

go 1.25.0

require (
	github.com/rs/zerolog v1.35.0
	github.com/google/uuid v1.3.0
)
```

Then fetch deps:

```bash
cd pkg/logger && go mod tidy
```

- [ ] **Step 4: Implement `logger.go`**

Create `pkg/logger/logger.go`:

```go
package logger

import (
	"context"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type contextKey struct{}

// Init configures the global zerolog logger.
// level: "debug"|"info"|"warn"|"error" — defaults to "info" on unknown values.
// format: "json"|"pretty" — "pretty" uses ConsoleWriter for local dev.
func Init(level, format string) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	lvl, err := zerolog.ParseLevel(level)
	if err != nil || level == "" {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)

	if format == "pretty" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	} else {
		log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	}
}

// WithRequestID stores a request ID in the context for downstream use.
// It also attaches the ID to the zerolog context so log.Ctx(ctx) carries it.
func WithRequestID(ctx context.Context, id string) context.Context {
	ctx = context.WithValue(ctx, contextKey{}, id)
	l := log.Ctx(ctx).With().Str("request_id", id).Logger()
	return l.WithContext(ctx)
}

// RequestIDFromContext retrieves the request ID stored by WithRequestID.
// Returns "" if not set.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(contextKey{}).(string); ok {
		return id
	}
	return ""
}
```

- [ ] **Step 5: Run tests — verify they pass**

```bash
cd pkg/logger && go test ./... -v
```

Expected:
```
--- PASS: TestInit_ValidLevel
--- PASS: TestInit_InvalidLevelFallsBackToInfo
--- PASS: TestRequestID_RoundTrip
--- PASS: TestRequestIDFromContext_Empty
PASS
```

- [ ] **Step 6: Commit**

```bash
git add pkg/logger/
git commit -m "$(cat <<'EOF'
feat(logger): add pkg/logger module with Init and request-id context helpers

Provides LOG_LEVEL/LOG_FORMAT-driven zerolog initialisation and
WithRequestID/RequestIDFromContext helpers for correlation propagation.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2 — Add `RequestLogger` HTTP middleware to `pkg/logger`

**Files:**
- Create: `pkg/logger/middleware.go`
- Create: `pkg/logger/middleware_test.go`

- [ ] **Step 1: Write the failing tests**

Create `pkg/logger/middleware_test.go`:

```go
package logger_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hellodk/hetu/pkg/logger"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog"
	"os"
)

func init() {
	// Silence log output during tests.
	zerolog.SetGlobalLevel(zerolog.Disabled)
	log.Logger = zerolog.New(os.Stderr)
}

func TestRequestLogger_GeneratesRequestID(t *testing.T) {
	handler := logger.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := logger.RequestIDFromContext(r.Context())
		if id == "" {
			t.Error("expected request_id in context, got empty string")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID response header to be set")
	}
}

func TestRequestLogger_EchoesIncomingRequestID(t *testing.T) {
	const incomingID = "abc-123-xyz"
	handler := logger.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := logger.RequestIDFromContext(r.Context()); got != incomingID {
			t.Errorf("got %q, want %q", got, incomingID)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set("X-Request-ID", incomingID)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Header().Get("X-Request-ID") != incomingID {
		t.Errorf("response X-Request-ID = %q, want %q", rr.Header().Get("X-Request-ID"), incomingID)
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
cd pkg/logger && go test ./... 2>&1 | grep FAIL
```

Expected: build error — `RequestLogger` undefined.

- [ ] **Step 3: Implement `middleware.go`**

Create `pkg/logger/middleware.go`:

```go
package logger

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// responseRecorder wraps http.ResponseWriter to capture the status code.
type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// RequestLogger is an HTTP middleware that:
//   - reads X-Request-ID from the incoming request (or generates a UUID v4)
//   - injects the ID into the request context via WithRequestID
//   - sets X-Request-ID on the response header
//   - logs method, path, status, and duration at Info level after the handler returns
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}

		ctx := WithRequestID(r.Context(), id)
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-ID", id)

		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)

		log.Ctx(ctx).Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", rec.status).
			Int64("duration_ms", time.Since(start).Milliseconds()).
			Msg("http request")
	})
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
cd pkg/logger && go test ./... -v
```

Expected: all 6 tests pass (4 from Task 1 + 2 new).

- [ ] **Step 5: Commit**

```bash
git add pkg/logger/middleware.go pkg/logger/middleware_test.go
git commit -m "$(cat <<'EOF'
feat(logger): add RequestLogger HTTP middleware with X-Request-ID propagation

Generates UUID if header absent, injects into context, echoes on
response, logs method/path/status/duration_ms per request.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3 — Wire `pkg/logger` into the analyzer

**Files:**
- Modify: `src/analyzer/go.mod`
- Modify: `src/analyzer/main.go` (lines 2892–2893 and ~1853)

- [ ] **Step 1: Add `pkg/logger` to analyzer's go.mod**

Open `src/analyzer/go.mod`. In the `require` block, add:

```
github.com/hellodk/hetu/pkg/logger v0.0.0
```

In the `replace` block, add:

```
github.com/hellodk/hetu/pkg/logger => ../../pkg/logger
```

Then:

```bash
cd src/analyzer && go mod tidy
```

- [ ] **Step 2: Replace inline log setup in `main()`**

In `src/analyzer/main.go`, find the two-line log setup (lines ~2892–2893):

```go
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
```

Replace with:

```go
	logger.Init(os.Getenv("LOG_LEVEL"), os.Getenv("LOG_FORMAT"))
```

Add the import `"github.com/hellodk/hetu/pkg/logger"` to the import block.

Remove `"github.com/rs/zerolog"` from the import block if it is no longer referenced elsewhere in `main.go` (check with `go build` — leave it if other uses remain).

- [ ] **Step 3: Wrap the API mux with `RequestLogger`**

In `src/analyzer/main.go`, find the middleware chain (line ~1853):

```go
	handler := rateLimitMiddleware(corsMiddleware(mux))
```

Replace with:

```go
	handler := rateLimitMiddleware(corsMiddleware(logger.RequestLogger(mux)))
```

- [ ] **Step 4: Build to verify no compile errors**

```bash
cd src/analyzer && go build ./...
```

Expected: exits 0, no output.

- [ ] **Step 5: Run existing tests**

```bash
cd src/analyzer && go test ./... -count=1 -timeout=60s 2>&1 | tail -5
```

Expected: all pass (or same failures as before this change — do not introduce new failures).

- [ ] **Step 6: Commit**

```bash
git add src/analyzer/go.mod src/analyzer/go.sum src/analyzer/main.go
git commit -m "$(cat <<'EOF'
feat(analyzer): wire pkg/logger — configurable level/format + request-id middleware

LOG_LEVEL and LOG_FORMAT env vars now control zerolog output.
API server mux wrapped with RequestLogger for per-request structured logs.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4 — Wire `pkg/logger` into the collector

**Files:**
- Modify: `src/collector/go.mod`
- Modify: `src/collector/main.go` (lines 701–702 and ~669)

- [ ] **Step 1: Add `pkg/logger` to collector's go.mod**

Open `src/collector/go.mod`. In the `require` block, add:

```
github.com/hellodk/hetu/pkg/logger v0.0.0
```

In the `replace` block, add:

```
github.com/hellodk/hetu/pkg/logger => ../../pkg/logger
```

Then:

```bash
cd src/collector && go mod tidy
```

- [ ] **Step 2: Replace inline log setup in `main()`**

In `src/collector/main.go`, find lines ~701–702:

```go
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
```

Replace with:

```go
	logger.Init(os.Getenv("LOG_LEVEL"), os.Getenv("LOG_FORMAT"))
```

Add import `"github.com/hellodk/hetu/pkg/logger"`. Remove `"github.com/rs/zerolog"` from imports if no longer needed elsewhere in `main.go`.

- [ ] **Step 3: Wrap the health server mux with `RequestLogger`**

In `src/collector/main.go`, in the `serveHealth()` function, find (line ~669):

```go
	c.healthServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", c.config.BindAddress, c.config.HealthPort),
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}
```

Replace `Handler: mux` with `Handler: logger.RequestLogger(mux)`.

- [ ] **Step 4: Build and test**

```bash
cd src/collector && go build ./... && go test ./... -count=1 -timeout=60s 2>&1 | tail -5
```

Expected: builds clean, tests pass.

- [ ] **Step 5: Commit**

```bash
git add src/collector/go.mod src/collector/go.sum src/collector/main.go
git commit -m "$(cat <<'EOF'
feat(collector): wire pkg/logger — configurable level/format + request-id middleware

LOG_LEVEL and LOG_FORMAT env vars now control zerolog output.
Health server mux wrapped with RequestLogger.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5 — NATS correlation: attach X-Request-ID header on Publish

**Files:**
- Modify: `pkg/bus/go.mod`
- Modify: `pkg/bus/nats.go`

- [ ] **Step 1: Add `pkg/logger` to `pkg/bus/go.mod`**

Open `pkg/bus/go.mod`. In the `require` block, add:

```
github.com/hellodk/hetu/pkg/logger v0.0.0
```

In the `replace` block, add:

```
github.com/hellodk/hetu/pkg/logger => ../logger
```

Then:

```bash
cd pkg/bus && go mod tidy
```

- [ ] **Step 2: Write a failing test for header propagation**

Create `pkg/bus/nats_test.go`:

```go
package bus_test

import (
	"context"
	"testing"

	"github.com/hellodk/hetu/pkg/logger"
)

// TestRequestIDContextRoundTrip verifies that the context helper pkg/logger
// exposes works correctly — bus.Publish reads from this same context.
// (Full NATS integration test requires a running server; this covers the
// context wiring only.)
func TestRequestIDContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = logger.WithRequestID(ctx, "bus-test-id")
	if got := logger.RequestIDFromContext(ctx); got != "bus-test-id" {
		t.Fatalf("got %q, want bus-test-id", got)
	}
}
```

- [ ] **Step 3: Run test — verify it passes (it tests context, not NATS)**

```bash
cd pkg/bus && go test ./... -v
```

Expected: `PASS: TestRequestIDContextRoundTrip`.

- [ ] **Step 4: Modify `Publish` to attach the header**

In `pkg/bus/nats.go`, find the `Publish` method:

```go
func (b *Bus) Publish(ctx context.Context, subject string, data []byte) error {
	fqn := b.prefix + "." + subject
	_, err := b.js.Publish(ctx, fqn, data)
	if err != nil {
		return fmt.Errorf("bus: publish %q: %w", fqn, err)
	}
	return nil
}
```

Replace with:

```go
func (b *Bus) Publish(ctx context.Context, subject string, data []byte) error {
	fqn := b.prefix + "." + subject

	msg := &nats.Msg{Subject: fqn, Data: data}
	if id := logger.RequestIDFromContext(ctx); id != "" {
		msg.Header = nats.Header{}
		msg.Header.Set("X-Request-ID", id)
	}

	_, err := b.js.PublishMsg(ctx, msg)
	if err != nil {
		return fmt.Errorf("bus: publish %q: %w", fqn, err)
	}
	return nil
}
```

Add `"github.com/hellodk/hetu/pkg/logger"` to the import block.

- [ ] **Step 5: Build to verify**

```bash
cd pkg/bus && go build ./...
```

Expected: exits 0.

Note: `Subscribe` returns `jetstream.Msg` which already exposes `.Headers()` — consumers can call `msg.Headers().Get("X-Request-ID")` to extract the ID on the receive side when a NATS subscriber is added to the analyzer in a future task.

- [ ] **Step 6: Run tests**

```bash
cd pkg/bus && go test ./... -v 2>&1 | tail -5
```

Expected: `PASS`.

- [ ] **Step 7: Commit**

```bash
git add pkg/bus/go.mod pkg/bus/go.sum pkg/bus/nats.go pkg/bus/nats_test.go
git commit -m "$(cat <<'EOF'
feat(bus): attach X-Request-ID NATS header from context on Publish

When a request_id is present in the context (set by pkg/logger middleware),
bus.Publish embeds it as X-Request-ID in the NATS message header so
downstream subscribers can continue the correlation chain.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6 — Dashboard: pino logger, proxy logging, client-error endpoint

**Files:**
- Modify: `src/dashboard/package.json`
- Create: `src/dashboard/lib/logger.ts`
- Modify: `src/dashboard/app/api/v1/[...path]/route.ts`
- Create: `src/dashboard/app/api/log-client-error/route.ts`
- Modify: `src/dashboard/app/error.tsx`

- [ ] **Step 1: Install pino**

```bash
cd src/dashboard && npm install pino@^9 && npm install --save-dev pino-pretty@^13
```

Verify `package.json` now contains:
```json
"pino": "^9",
```
and in `devDependencies`:
```json
"pino-pretty": "^13"
```

- [ ] **Step 2: Create `lib/logger.ts`**

Create `src/dashboard/lib/logger.ts`:

```typescript
import pino from 'pino'

const logger = pino({
  level: process.env.LOG_LEVEL ?? 'info',
  ...(process.env.NODE_ENV !== 'production' && {
    transport: { target: 'pino-pretty', options: { colorize: true } },
  }),
})

export default logger
```

- [ ] **Step 3: Write a smoke-test for the logger module**

Verify it compiles by running the TypeScript checker:

```bash
cd src/dashboard && npx tsc --noEmit 2>&1 | grep "lib/logger" || echo "logger.ts: OK"
```

Expected: no errors referencing `lib/logger.ts`.

- [ ] **Step 4: Update the API proxy route**

Open `src/dashboard/app/api/v1/[...path]/route.ts`. Replace the entire file with:

```typescript
import { NextRequest, NextResponse } from 'next/server'
import { randomUUID } from 'crypto'
import logger from '@/lib/logger'

function getAnalyzerUrl(): string {
  if (process.env.ANALYZER_URL) return process.env.ANALYZER_URL
  if (process.env.NEXT_PUBLIC_ANALYZER_URL) return process.env.NEXT_PUBLIC_ANALYZER_URL
  const host = process.env.CLUSTER_INTEL_ANALYZER_SERVICE_HOST
  const port = process.env.CLUSTER_INTEL_ANALYZER_SERVICE_PORT_HTTP || process.env.CLUSTER_INTEL_ANALYZER_SERVICE_PORT || '8081'
  if (host) return `http://${host}:${port}`
  return 'http://cluster-intel-analyzer:8081'
}

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
    logger.error({ err: err.message, target: url, duration_ms: duration, request_id: requestId }, 'proxy error')
    return NextResponse.json({ error: `Proxy: ${err.message}`, target: url }, { status: 502 })
  }
}
```

- [ ] **Step 5: Create the client-error endpoint**

Create directory and file:

```bash
mkdir -p "src/dashboard/app/api/log-client-error"
```

Create `src/dashboard/app/api/log-client-error/route.ts`:

```typescript
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
```

- [ ] **Step 6: Update `app/error.tsx` to POST to the log endpoint**

Open `src/dashboard/app/error.tsx`. Replace the `useEffect` block:

```typescript
    useEffect(() => {
        // Log the error to an error reporting service
        console.error('Dashboard Application Error:', error)
    }, [error])
```

With:

```typescript
    useEffect(() => {
        console.error('Dashboard Application Error:', error)
        fetch('/api/log-client-error', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                message: error.message,
                stack: error.stack,
                componentStack: error.digest,
            }),
        }).catch(() => { /* best-effort: never throw from error boundary */ })
    }, [error])
```

- [ ] **Step 7: Type-check**

```bash
cd src/dashboard && npx tsc --noEmit 2>&1 | grep -v "^$" | head -20
```

Expected: no errors in files touched in this task.

- [ ] **Step 8: Run dashboard tests**

```bash
cd src/dashboard && npx playwright test --reporter=line 2>&1 | tail -10
```

Expected: same pass/fail count as before this task (no regressions).

- [ ] **Step 9: Commit**

```bash
git add src/dashboard/package.json src/dashboard/package-lock.json \
        src/dashboard/lib/logger.ts \
        "src/dashboard/app/api/v1/[...path]/route.ts" \
        src/dashboard/app/api/log-client-error/ \
        src/dashboard/app/error.tsx
git commit -m "$(cat <<'EOF'
feat(dashboard): add pino server-side logger with X-Request-ID correlation

API proxy logs method/path/status/duration_ms/request_id per request.
Client render errors POST to /api/log-client-error for server-side capture.
LOG_LEVEL env var controls pino level; pino-pretty used in dev.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7 — Helm values: LOG_LEVEL / LOG_FORMAT env vars

**Files:**
- Modify: `values-dev.yaml`
- Modify: `deploy/helm/cluster-intel/values.yaml`

- [ ] **Step 1: Add dev env vars to `values-dev.yaml`**

Open `values-dev.yaml` and append the following block at the end of the file:

```yaml
# Logging — dev overrides (human-readable, verbose)
collector:
  env:
    - name: LOG_LEVEL
      value: "debug"
    - name: LOG_FORMAT
      value: "pretty"

analyzer:
  env:
    - name: LOG_LEVEL
      value: "debug"
    - name: LOG_FORMAT
      value: "pretty"

dashboard:
  env:
    - name: LOG_LEVEL
      value: "debug"
    - name: NODE_ENV
      value: "development"
```

- [ ] **Step 2: Add production env vars to `deploy/helm/cluster-intel/values.yaml`**

In `deploy/helm/cluster-intel/values.yaml`, the three components each have an `env: []` line. Change each to:

For the collector (line ~99):
```yaml
  env:
    - name: LOG_LEVEL
      value: "info"
    - name: LOG_FORMAT
      value: "json"
```

For the analyzer (line ~139):
```yaml
  env:
    - name: LOG_LEVEL
      value: "info"
    - name: LOG_FORMAT
      value: "json"
```

For the dashboard (line ~171):
```yaml
  env:
    - name: LOG_LEVEL
      value: "info"
```

- [ ] **Step 3: Verify Helm renders cleanly**

```bash
helm template cluster-intel deploy/helm/cluster-intel -f values-dev.yaml 2>&1 | grep -E "LOG_LEVEL|LOG_FORMAT|error" | head -10
```

Expected: `LOG_LEVEL` and `LOG_FORMAT` values appear in the rendered YAML, no `error` lines.

- [ ] **Step 4: Commit**

```bash
git add values-dev.yaml deploy/helm/cluster-intel/values.yaml
git commit -m "$(cat <<'EOF'
feat(helm): add LOG_LEVEL / LOG_FORMAT env vars to collector, analyzer, dashboard

Dev values use debug+pretty; production defaults to info+json.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8 — End-to-end smoke test

- [ ] **Step 1: Start the dev server**

```bash
bash run-local.sh --yes
```

- [ ] **Step 2: Verify analyzer JSON logs**

```bash
LOG_FORMAT=json LOG_LEVEL=debug bash run-local.sh --yes 2>&1 | grep '"request_id"' | head -5
```

Expected: JSON log lines with `"request_id"` field appear when you make an API call (e.g. open the dashboard).

- [ ] **Step 3: Verify pretty logs in dev**

```bash
LOG_FORMAT=pretty LOG_LEVEL=debug bash run-local.sh --yes 2>&1 | head -20
```

Expected: coloured, human-readable zerolog output.

- [ ] **Step 4: Verify X-Request-ID header is forwarded through the stack**

Make a request to the dashboard proxy:

```bash
curl -v http://localhost:3003/api/v1/health 2>&1 | grep -i "x-request-id"
```

Expected: `< X-Request-ID: <some-uuid>` in the response headers.

- [ ] **Step 5: Verify correlation — same request_id in collector and analyzer logs**

In the server logs, search for a specific request_id that appears in a collector publish line and a matching analyzer receive line. Use:

```bash
grep -h "request_id" /tmp/collector.log /tmp/analyzer.log 2>/dev/null | sort | uniq -c | sort -rn | head -10
```

(Adjust log file paths to wherever run-local.sh writes service output.)

---

## Self-Review

**Spec coverage:**

| Spec requirement | Task |
|---|---|
| `LOG_LEVEL` env var on Go services | Task 3, 4, 7 |
| `LOG_FORMAT` env var (json/pretty) | Task 3, 4, 7 |
| `pkg/logger` shared Init | Task 1 |
| HTTP middleware — generate/forward X-Request-ID | Task 2 |
| Middleware — inject logger into context | Task 2 |
| Middleware — log method/path/status/duration | Task 2 |
| Analyzer mux wrapped with middleware | Task 3 |
| Collector mux wrapped with middleware | Task 4 |
| NATS Publish attaches X-Request-ID header | Task 5 |
| pino logger in dashboard | Task 6 |
| API proxy logs request_id, method, path, status, duration | Task 6 |
| API proxy forwards X-Request-ID to analyzer | Task 6 |
| `/api/log-client-error` endpoint | Task 6 |
| `error.tsx` POSTs to log endpoint | Task 6 |
| `LOG_LEVEL` for dashboard | Task 6, 7 |
| Helm values env vars | Task 7 |
