package logger

import (
	"context"
	"io"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/trace"
)

type contextKey struct{}

// Init configures the global zerolog logger.
// level: "debug"|"info"|"warn"|"error" — defaults to "info" on unknown values.
// format: "json"|"pretty" — "pretty" uses ConsoleWriter for local dev.
func Init(level, format string) {
	InitWithService(level, format, "")
}

// InitWithService behaves like Init but stamps every log line with a static
// service field ("hetu-analyzer", "hetu-collector", …) so aggregated streams
// stay attributable once shipped to Loki.
func InitWithService(level, format, service string) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	lvl, err := zerolog.ParseLevel(level)
	if err != nil || level == "" {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)

	log.Logger = buildLogger(os.Stderr, level, format, service).Hook(TraceHook{})
}

// buildLogger constructs a zerolog logger with the given destination,
// level, output format and static service field. The OTEL TraceHook is NOT
// attached here — callers opt in explicitly (InitWithService always does).
func buildLogger(w io.Writer, level, format, service string) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil || level == "" {
		lvl = zerolog.InfoLevel
	}

	var lc zerolog.Context
	if format == "pretty" {
		lc = zerolog.New(zerolog.ConsoleWriter{Out: w}).Level(lvl).With()
	} else {
		lc = zerolog.New(w).Level(lvl).With()
	}
	lc = lc.Timestamp()
	if service != "" {
		lc = lc.Str("service", service)
	}
	return lc.Logger()
}

// TraceHook copies the active OTEL span context onto every event emitted by
// a context-aware logger (log.Ctx(ctx) / Logger.WithContext(ctx)), so each
// log line carries trace_id/span_id and Loki entries can be jumped to the
// corresponding Tempo trace. Events built from the bare global logger have
// no context attached and are passed through untouched.
type TraceHook struct{}

func (TraceHook) Run(e *zerolog.Event, _ zerolog.Level, _ string) {
	ctx := e.GetCtx()
	if ctx == nil {
		return
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return
	}
	e.Str("trace_id", sc.TraceID().String())
	e.Str("span_id", sc.SpanID().String())
}

// WithRequestID stores a request ID in the context for downstream use and
// returns a context whose logger carries it on every line. If the incoming
// context holds an active OTEL span context, trace_id/span_id are stamped
// onto the logger as well, giving each HTTP-path log line Loki→Tempo
// correlation once tracing is initialized upstream.
func WithRequestID(ctx context.Context, id string) context.Context {
	ctx = context.WithValue(ctx, contextKey{}, id)
	lc := log.Ctx(ctx).With()
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		lc = lc.Str("trace_id", sc.TraceID().String()).Str("span_id", sc.SpanID().String())
	}
	l := lc.Str("request_id", id).Logger()
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
