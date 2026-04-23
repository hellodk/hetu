package main

import (
	"testing"
	"time"

	types "github.com/hellodk/hetu/pkg/types"
)

func TestMockSource_GenerateRanges(t *testing.T) {
	a := newTestAnalyzer()
	m := newMockSource(a, 20*time.Second)

	// Generate several reports and verify each stays within plausible ranges.
	for i := 0; i < 10; i++ {
		r := m.generate()
		if r.Scores == nil {
			t.Fatal("mock report must have non-nil Scores")
		}
		if r.Scores.Reliability < 70 || r.Scores.Reliability > 95 {
			t.Errorf("Reliability out of range: %d", r.Scores.Reliability)
		}
		if r.Scores.Security < 65 || r.Scores.Security > 95 {
			t.Errorf("Security out of range: %d", r.Scores.Security)
		}
		if r.Scores.Cost < 65 || r.Scores.Cost > 95 {
			t.Errorf("Cost out of range: %d", r.Scores.Cost)
		}
		if r.Scores.Architecture < 75 || r.Scores.Architecture > 95 {
			t.Errorf("Architecture out of range: %d", r.Scores.Architecture)
		}
		if r.Scores.Overall < 0 || r.Scores.Overall > 100 {
			t.Errorf("Overall out of range: %d", r.Scores.Overall)
		}
	}
}

func TestMockSource_ReportStructure(t *testing.T) {
	a := newTestAnalyzer()
	m := newMockSource(a, 20*time.Second)

	r := m.generate()

	// Status must mark this as mock profile OK.
	if r.Status == nil {
		t.Fatal("mock report must have Status")
	}
	if r.Status.Profile != types.ProfileMock {
		t.Errorf("Status.Profile = %q, want %q", r.Status.Profile, types.ProfileMock)
	}
	if r.Status.State != types.StateOK {
		t.Errorf("Status.State = %q, want %q", r.Status.State, types.StateOK)
	}

	// Topology fields should be populated.
	if r.Summary.TotalNodes == 0 {
		t.Error("Summary.TotalNodes should be non-zero")
	}
	if r.Summary.TotalPods == 0 {
		t.Error("Summary.TotalPods should be non-zero")
	}
	if len(r.Summary.Namespaces) == 0 {
		t.Error("Summary.Namespaces should be non-empty")
	}

	// Should have at least one issue and one recommendation.
	if len(r.TopIssues) == 0 {
		t.Error("Expected at least one mock issue")
	}
	if len(r.Recommendations) == 0 {
		t.Error("Expected at least one mock recommendation")
	}

	// Recommendations should have cost savings entries and an aggregate.
	if r.EstimatedSavings == 0 {
		t.Error("EstimatedSavings should be non-zero")
	}

	// Resource utilization should be plausible (used < requested < capacity).
	if r.ResourceUtilization.CPU.Used > r.ResourceUtilization.CPU.Capacity {
		t.Errorf("CPU used (%v) exceeds capacity (%v)",
			r.ResourceUtilization.CPU.Used, r.ResourceUtilization.CPU.Capacity)
	}
	if r.ResourceUtilization.Memory.Used > r.ResourceUtilization.Memory.Capacity {
		t.Errorf("Memory used (%v) exceeds capacity (%v)",
			r.ResourceUtilization.Memory.Used, r.ResourceUtilization.Memory.Capacity)
	}
}

func TestMockSource_GenerateAndBroadcast_PublishesReport(t *testing.T) {
	a := newTestAnalyzer()
	m := newMockSource(a, 20*time.Second)

	// Subscribe so we can observe the broadcast.
	ch := a.subscribe()
	defer a.unsubscribe(ch)

	m.generateAndBroadcast()

	// latestReport should be set immediately.
	a.reportMu.RLock()
	latest := a.latestReport
	a.reportMu.RUnlock()
	if latest == nil {
		t.Fatal("generateAndBroadcast did not publish a report")
	}
	if latest.Scores == nil {
		t.Fatal("published mock report missing Scores")
	}

	// SSE subscriber should have received the report.
	select {
	case got := <-ch:
		if got == nil {
			t.Fatal("Got nil from subscribe channel")
		}
		if got.Scores == nil {
			t.Error("Broadcast report missing Scores")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Timed out waiting for broadcast report")
	}
}

func TestMockSource_GeneratesVariedReports(t *testing.T) {
	a := newTestAnalyzer()
	m := newMockSource(a, 20*time.Second)

	// Sanity check: across multiple generations we should see at least some
	// variation in the scores (otherwise the "jitter" isn't happening).
	first := m.generate()
	varied := false
	for i := 0; i < 20; i++ {
		next := m.generate()
		if next.Scores.Overall != first.Scores.Overall ||
			next.Scores.Reliability != first.Scores.Reliability ||
			next.Scores.Cost != first.Scores.Cost {
			varied = true
			break
		}
	}
	if !varied {
		t.Error("Expected mock scores to vary across generations")
	}
}
