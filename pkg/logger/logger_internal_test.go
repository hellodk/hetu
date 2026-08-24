package logger

// In-package tests for buildLogger + TraceHook (issue #12, item C8):
// every log line must carry a static service field, and lines emitted with
// an active span context must carry trace_id/span_id so Loki entries can be
// jumped to Tempo traces.

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/trace"
)

// enableTestLogging re-enables output for the duration of one test — the
// package-wide middleware_test init() silences zerolog for the whole binary.
func enableTestLogging(t *testing.T) {
	t.Helper()
	prev := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(prev) })
}

func TestBuildLogger_AttachesServiceField(t *testing.T) {
	enableTestLogging(t)
	var buf bytes.Buffer
	l := buildLogger(&buf, "info", "json", "hetu-test")
	l.Info().Msg("hello")

	var rec struct {
		Service string `json:"service"`
		Msg     string `json:"message"`
	}
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("decode log line %q: %v", buf.String(), err)
	}
	if rec.Service != "hetu-test" {
		t.Fatalf("expected service=hetu-test, got %q", rec.Service)
	}
	if rec.Msg != "hello" {
		t.Fatalf("unexpected message %q", rec.Msg)
	}
}

func TestBuildLogger_EmptyServiceOmitsField(t *testing.T) {
	enableTestLogging(t)
	var buf bytes.Buffer
	l := buildLogger(&buf, "info", "json", "")
	l.Info().Msg("hello")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("decode log line %q: %v", buf.String(), err)
	}
	if _, ok := rec["service"]; ok {
		t.Fatalf("expected no service field when unset, got %v", rec["service"])
	}
}

func TestTraceHook_AddsTraceAndSpanIDs(t *testing.T) {
	enableTestLogging(t)
	tid, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatalf("trace id: %v", err)
	}
	sid, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatalf("span id: %v", err)
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, Remote: true})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	var buf bytes.Buffer
	l := buildLogger(&buf, "info", "json", "").Hook(TraceHook{})
	l.Info().Ctx(ctx).Msg("with span")

	var rec struct {
		TraceID string `json:"trace_id"`
		SpanID  string `json:"span_id"`
	}
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("decode log line %q: %v", buf.String(), err)
	}
	if rec.TraceID != tid.String() {
		t.Fatalf("expected trace_id %s, got %q", tid.String(), rec.TraceID)
	}
	if rec.SpanID != sid.String() {
		t.Fatalf("expected span_id %s, got %q", sid.String(), rec.SpanID)
	}
}

func TestTraceHook_NilSpanContextAddsNothing(t *testing.T) {
	enableTestLogging(t)
	var buf bytes.Buffer
	l := buildLogger(&buf, "info", "json", "").Hook(TraceHook{})
	l.Info().Msg("no span")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("decode log line %q: %v", buf.String(), err)
	}
	if _, ok := rec["trace_id"]; ok {
		t.Fatalf("expected no trace_id without a span, got %v", rec["trace_id"])
	}
	if zerolog.GlobalLevel() == zerolog.Disabled {
		t.Fatal("global level unexpectedly disabled")
	}
}

// WithRequestID must bake trace fields into the returned context's logger so
// every log.Ctx(ctx) line on an HTTP path carries correlation ids.
func TestWithRequestID_StampsTraceFields(t *testing.T) {
	enableTestLogging(t)
	tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	sid, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, Remote: true})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	var buf bytes.Buffer
	prev := log.Logger
	log.Logger = buildLogger(&buf, "info", "json", "")
	t.Cleanup(func() { log.Logger = prev })

	// Mirror RequestLogger: the base logger must be stored in the context
	// first (log.Ctx falls back to a disabled logger otherwise).
	ctx = log.Logger.WithContext(ctx)
	ctx = WithRequestID(ctx, "req-42")
	log.Ctx(ctx).Info().Msg("request line")

	var rec struct {
		TraceID   string `json:"trace_id"`
		SpanID    string `json:"span_id"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("decode log line %q: %v", buf.String(), err)
	}
	if rec.TraceID != tid.String() || rec.SpanID != sid.String() || rec.RequestID != "req-42" {
		t.Fatalf("missing correlation ids: %+v", rec)
	}
}
