package main

// Phase-1 regression tests for readiness semantics (issue #12, item 4):
// /readyz must report 503 until the analyzer has published its first report
// (live analysis or mock generation), then 200 monotonically. /healthz stays
// an unconditional liveness endpoint.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	types "github.com/hellodk/hetu/pkg/types"
)

func TestReadyz_NotReadyBeforeFirstReport(t *testing.T) {
	a := newTestAnalyzer()

	rec := httptest.NewRecorder()
	a.handleReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 before first report, got %d", rec.Code)
	}
}

func TestReadyz_ReadyAfterFirstPublishedReport(t *testing.T) {
	a := newTestAnalyzer()

	a.publishReport(&types.ClusterHealthReport{
		ClusterID: a.config.ClusterID,
		Timestamp: time.Now(),
		Summary:   types.ClusterSummary{Namespaces: make(map[string]*types.NamespaceStats)},
	})

	rec := httptest.NewRecorder()
	a.handleReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after first report, got %d", rec.Code)
	}
}

func TestReadyz_StaysReadyAcrossLaterFailures(t *testing.T) {
	a := newTestAnalyzer()

	a.publishReport(&types.ClusterHealthReport{
		ClusterID: a.config.ClusterID,
		Timestamp: time.Now(),
		Summary:   types.ClusterSummary{Namespaces: make(map[string]*types.NamespaceStats)},
	})
	// A later degraded cycle publishes a diagnostic-only report; readiness
	// is monotonic — the process still serves its API meaningfully.
	a.publishReport(a.buildDiagnosticReport())

	rec := httptest.NewRecorder()
	a.handleReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected monotonic 200 after diagnostic report, got %d", rec.Code)
	}
}
