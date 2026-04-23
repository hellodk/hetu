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
