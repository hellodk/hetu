package main

// Phase-1 regression tests for fetchEvents (review issue #12, item C1):
// in the live profile a collector failure must surface as an error that
// feeds buildDiagnosticReport — synthetic telemetry must never be served.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	types "github.com/hellodk/hetu/pkg/types"
)

// TestFetchEvents_LiveModeNeverServesMockData pins the C1 contract: when the
// collector hostname cannot be resolved, fetchEvents returns an error and no
// events. The pre-fix implementation returned mockTelemetryEvents() with a
// nil error on exactly this path (main.go:1283-1289), which scored fabricated
// CrashLoopBackOff events as real cluster state.
func TestFetchEvents_LiveModeNeverServesMockData(t *testing.T) {
	a := newTestAnalyzer()
	a.profile = types.ProfileLive

	// .invalid is an RFC 6761 special-use domain guaranteed not to resolve.
	a.setCollectorURL("http://collector.invalid")

	events, err := a.fetchEvents(context.Background())
	if err == nil {
		t.Fatalf("expected error when collector is unreachable, got nil error with %d events", len(events))
	}
	if events != nil {
		t.Fatalf("expected no events on collector failure, got %d events (synthetic telemetry leak)", len(events))
	}
}

// TestFetchEvents_ParsesCollectorPayload guards the happy path so removing
// the fallback cannot regress normal live operation.
func TestFetchEvents_ParsesCollectorPayload(t *testing.T) {
	var sawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]types.TelemetryEvent{{
			ID: "evt-1", Cluster: "test-cluster", Source: "test", Reason: "TestReason",
		}})
	}))
	defer srv.Close()

	a := newTestAnalyzer()
	a.profile = types.ProfileLive
	a.setCollectorURL(srv.URL)

	events, err := a.fetchEvents(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sawPath != "/api/v1/events" {
		t.Fatalf("unexpected request path %q", sawPath)
	}
	if len(events) != 1 || events[0].Reason != "TestReason" {
		t.Fatalf("expected 1 TestReason event, got %+v", events)
	}
}
