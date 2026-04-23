package bus_test

import (
	"context"
	"testing"

	"github.com/hellodk/hetu/pkg/logger"
)

// TestRequestIDContextRoundTrip verifies that the context helper pkg/logger
// exposes works correctly — bus.Publish reads from this same context.
func TestRequestIDContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = logger.WithRequestID(ctx, "bus-test-id")
	if got := logger.RequestIDFromContext(ctx); got != "bus-test-id" {
		t.Fatalf("got %q, want bus-test-id", got)
	}
}
