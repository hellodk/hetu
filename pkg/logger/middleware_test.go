package logger_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/hellodk/hetu/pkg/logger"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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
	gotID := rr.Header().Get("X-Request-ID")
	if gotID == "" {
		t.Error("expected X-Request-ID response header to be set")
	}
	if _, err := uuid.Parse(gotID); err != nil {
		t.Errorf("X-Request-ID %q is not a valid UUID: %v", gotID, err)
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
