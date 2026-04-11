package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"text/template"
	"time"

	types "github.com/your-org/cluster-intel/pkg/types"
)

// newTestAnalyzer creates a minimal Analyzer suitable for unit tests.
// It does NOT start any goroutines, HTTP servers, or connect to external services.
func newTestAnalyzer() *Analyzer {
	a := &Analyzer{
		config: Config{
			ClusterID:        "test-cluster",
			CollectorURL:     "http://localhost:9999", // unused in unit tests
			AnalysisInterval: 5 * time.Minute,
		},
		httpClient:      &http.Client{Timeout: 5 * time.Second},
		stopCh:          make(chan struct{}),
		promptTemplates: make(map[string]*template.Template),
		subscribers:     make(map[chan *types.ClusterHealthReport]struct{}),
		profile:         types.ProfileLive,
	}
	a.initMetrics()
	// Create a mock source so handlers that reference it don't NPE.
	// Tests never Start() it so no goroutine is spawned.
	a.mockSource = newMockSource(a, 20*time.Second)
	return a
}

// ---------------------------------------------------------------------------
// extractJSON Tests
// ---------------------------------------------------------------------------

func TestExtractJSON_PureJSON(t *testing.T) {
	input := `{"key": "value"}`
	result := extractJSON(input)
	if result != input {
		t.Errorf("Expected %q, got %q", input, result)
	}
}

func TestExtractJSON_MarkdownWrapped(t *testing.T) {
	input := "```json\n{\"key\": \"value\"}\n```"
	result := extractJSON(input)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Errorf("Failed to parse extracted JSON: %v", err)
	}
	if parsed["key"] != "value" {
		t.Errorf("Expected 'value', got %v", parsed["key"])
	}
}

func TestExtractJSON_WithSurroundingText(t *testing.T) {
	input := "Here is the analysis:\n{\"issues\": []}\nEnd of analysis."
	result := extractJSON(input)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Errorf("Failed to parse extracted JSON: %v (result was %q)", err, result)
	}
}

func TestExtractJSON_NestedBraces(t *testing.T) {
	input := `{"outer": {"inner": "value"}}`
	result := extractJSON(input)
	if result != input {
		t.Errorf("Expected %q, got %q", input, result)
	}
}

func TestExtractJSON_DeeplyNested(t *testing.T) {
	input := `{"a":{"b":{"c":"deep"}}}`
	result := extractJSON(input)
	if result != input {
		t.Errorf("Expected %q, got %q", input, result)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Errorf("Failed to parse deeply nested JSON: %v", err)
	}
}

func TestExtractJSON_NoJSON(t *testing.T) {
	input := "no json here at all"
	result := extractJSON(input)
	if result != input {
		t.Errorf("Expected unchanged string, got %q", result)
	}
}

func TestExtractJSON_EmptyObject(t *testing.T) {
	input := `{}`
	result := extractJSON(input)
	if result != input {
		t.Errorf("Expected %q, got %q", input, result)
	}
}

func TestExtractJSON_MarkdownWithBackticks(t *testing.T) {
	input := "```\n{\"status\":\"ok\"}\n```"
	result := extractJSON(input)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Errorf("Failed to parse extracted JSON from backtick block: %v", err)
	}
	if parsed["status"] != "ok" {
		t.Errorf("Expected 'ok', got %v", parsed["status"])
	}
}

func TestExtractJSON_ArrayInsideObject(t *testing.T) {
	input := `{"items": [1, 2, 3]}`
	result := extractJSON(input)
	if result != input {
		t.Errorf("Expected %q, got %q", input, result)
	}
}

// ---------------------------------------------------------------------------
// filterWarningEvents Tests
// ---------------------------------------------------------------------------

func TestFilterWarningEvents_Mixed(t *testing.T) {
	events := []types.TelemetryEvent{
		{Type: "Warning", Reason: "BackOff"},
		{Type: "Normal", Reason: "Pulled"},
		{Type: "Warning", Reason: "FailedScheduling"},
		{Type: "Normal", Reason: "Created"},
	}

	warnings := filterWarningEvents(events)
	if len(warnings) != 2 {
		t.Errorf("Expected 2 warnings, got %d", len(warnings))
	}
	for _, w := range warnings {
		if w.Type != "Warning" {
			t.Errorf("Expected Warning type, got %s", w.Type)
		}
	}
}

func TestFilterWarningEvents_Empty(t *testing.T) {
	warnings := filterWarningEvents(nil)
	if len(warnings) != 0 {
		t.Errorf("Expected 0 warnings from nil, got %d", len(warnings))
	}
}

func TestFilterWarningEvents_EmptySlice(t *testing.T) {
	warnings := filterWarningEvents([]types.TelemetryEvent{})
	if len(warnings) != 0 {
		t.Errorf("Expected 0 warnings from empty slice, got %d", len(warnings))
	}
}

func TestFilterWarningEvents_AllWarnings(t *testing.T) {
	events := []types.TelemetryEvent{
		{Type: "Warning", Reason: "OOMKilled"},
		{Type: "Warning", Reason: "FailedMount"},
	}
	warnings := filterWarningEvents(events)
	if len(warnings) != 2 {
		t.Errorf("Expected 2, got %d", len(warnings))
	}
}

func TestFilterWarningEvents_NoWarnings(t *testing.T) {
	events := []types.TelemetryEvent{
		{Type: "Normal", Reason: "Scheduled"},
		{Type: "Normal", Reason: "Pulled"},
	}
	warnings := filterWarningEvents(events)
	if len(warnings) != 0 {
		t.Errorf("Expected 0, got %d", len(warnings))
	}
}

func TestFilterWarningEvents_PreservesOrder(t *testing.T) {
	events := []types.TelemetryEvent{
		{Type: "Warning", Reason: "First"},
		{Type: "Normal", Reason: "Skip"},
		{Type: "Warning", Reason: "Second"},
	}
	warnings := filterWarningEvents(events)
	if len(warnings) != 2 {
		t.Fatalf("Expected 2 warnings, got %d", len(warnings))
	}
	if warnings[0].Reason != "First" || warnings[1].Reason != "Second" {
		t.Errorf("Order not preserved: got %q, %q", warnings[0].Reason, warnings[1].Reason)
	}
}

// ---------------------------------------------------------------------------
// Helper function Tests
// ---------------------------------------------------------------------------

func TestGetString(t *testing.T) {
	m := map[string]any{
		"key1": "value1",
		"key2": 42,
		"key3": nil,
	}

	if v := getString(m, "key1", "default"); v != "value1" {
		t.Errorf("Expected 'value1', got %q", v)
	}
	if v := getString(m, "key2", "default"); v != "default" {
		t.Errorf("Expected 'default' for int value, got %q", v)
	}
	if v := getString(m, "missing", "fallback"); v != "fallback" {
		t.Errorf("Expected 'fallback', got %q", v)
	}
	if v := getString(m, "key3", "nildefault"); v != "nildefault" {
		t.Errorf("Expected 'nildefault' for nil value, got %q", v)
	}
}

func TestGetString_EmptyString(t *testing.T) {
	m := map[string]any{
		"empty": "",
	}
	// An empty string IS a string, so it should be returned (not the default).
	if v := getString(m, "empty", "default"); v != "" {
		t.Errorf("Expected empty string, got %q", v)
	}
}

func TestGetFloat(t *testing.T) {
	m := map[string]any{
		"key1": float64(3.14),
		"key2": "not a float",
		"key3": nil,
	}

	if v := getFloat(m, "key1", 0); v != 3.14 {
		t.Errorf("Expected 3.14, got %f", v)
	}
	if v := getFloat(m, "key2", 1.0); v != 1.0 {
		t.Errorf("Expected default 1.0 for string value, got %f", v)
	}
	if v := getFloat(m, "missing", 2.5); v != 2.5 {
		t.Errorf("Expected default 2.5, got %f", v)
	}
	if v := getFloat(m, "key3", 9.9); v != 9.9 {
		t.Errorf("Expected default 9.9 for nil value, got %f", v)
	}
}

func TestGetFloat_Zero(t *testing.T) {
	m := map[string]any{
		"zero": float64(0),
	}
	if v := getFloat(m, "zero", 42.0); v != 0 {
		t.Errorf("Expected 0, got %f", v)
	}
}

func TestMapPriorityToSeverity(t *testing.T) {
	tests := []struct {
		priority float64
		expected string
	}{
		{0.5, "critical"},
		{1, "critical"},
		{2, "critical"},
		{2.5, "high"},
		{3, "high"},
		{4, "high"},
		{4.5, "medium"},
		{5, "medium"},
		{6, "medium"},
		{6.5, "low"},
		{7, "low"},
		{10, "low"},
		{100, "low"},
	}

	for _, tt := range tests {
		result := mapPriorityToSeverity(tt.priority)
		if result != tt.expected {
			t.Errorf("mapPriorityToSeverity(%v) = %q, want %q", tt.priority, result, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Config helper Tests
// ---------------------------------------------------------------------------

func TestGetEnvOrDefault_Set(t *testing.T) {
	os.Setenv("ANALYZER_TEST_VAR", "custom")
	defer os.Unsetenv("ANALYZER_TEST_VAR")

	result := getEnvOrDefault("ANALYZER_TEST_VAR", "default")
	if result != "custom" {
		t.Errorf("Expected 'custom', got %q", result)
	}
}

func TestGetEnvOrDefault_Unset(t *testing.T) {
	os.Unsetenv("ANALYZER_MISSING_VAR")
	result := getEnvOrDefault("ANALYZER_MISSING_VAR", "fallback")
	if result != "fallback" {
		t.Errorf("Expected 'fallback', got %q", result)
	}
}

func TestGetEnvOrDefault_EmptyIsUnset(t *testing.T) {
	os.Setenv("ANALYZER_EMPTY_VAR", "")
	defer os.Unsetenv("ANALYZER_EMPTY_VAR")

	result := getEnvOrDefault("ANALYZER_EMPTY_VAR", "default")
	if result != "default" {
		t.Errorf("Expected 'default' for empty env var, got %q", result)
	}
}

func TestGetEnvIntOrDefault_Set(t *testing.T) {
	os.Setenv("ANALYZER_INT_VAR", "8081")
	defer os.Unsetenv("ANALYZER_INT_VAR")

	result := getEnvIntOrDefault("ANALYZER_INT_VAR", 9999)
	if result != 8081 {
		t.Errorf("Expected 8081, got %d", result)
	}
}

func TestGetEnvIntOrDefault_Unset(t *testing.T) {
	os.Unsetenv("ANALYZER_INT_MISSING")
	result := getEnvIntOrDefault("ANALYZER_INT_MISSING", 9999)
	if result != 9999 {
		t.Errorf("Expected 9999, got %d", result)
	}
}

func TestGetEnvIntOrDefault_Zero(t *testing.T) {
	os.Setenv("ANALYZER_INT_ZERO", "0")
	defer os.Unsetenv("ANALYZER_INT_ZERO")

	// "0" is not empty, so it should parse and return 0.
	result := getEnvIntOrDefault("ANALYZER_INT_ZERO", 42)
	if result != 0 {
		t.Errorf("Expected 0, got %d", result)
	}
}

func TestGetDurationOrDefault_Set(t *testing.T) {
	os.Setenv("ANALYZER_DUR_VAR", "10m")
	defer os.Unsetenv("ANALYZER_DUR_VAR")

	result := getDurationOrDefault("ANALYZER_DUR_VAR", time.Minute)
	if result != 10*time.Minute {
		t.Errorf("Expected 10m, got %v", result)
	}
}

func TestGetDurationOrDefault_Unset(t *testing.T) {
	os.Unsetenv("ANALYZER_DUR_MISSING")
	result := getDurationOrDefault("ANALYZER_DUR_MISSING", 3*time.Second)
	if result != 3*time.Second {
		t.Errorf("Expected 3s, got %v", result)
	}
}

func TestGetDurationOrDefault_Invalid(t *testing.T) {
	os.Setenv("ANALYZER_DUR_BAD", "not-a-duration")
	defer os.Unsetenv("ANALYZER_DUR_BAD")

	result := getDurationOrDefault("ANALYZER_DUR_BAD", 7*time.Minute)
	if result != 7*time.Minute {
		t.Errorf("Expected 7m fallback for invalid duration, got %v", result)
	}
}

func TestGetEnvFloatOrDefault_Set(t *testing.T) {
	os.Setenv("ANALYZER_FLOAT_VAR", "0.7")
	defer os.Unsetenv("ANALYZER_FLOAT_VAR")

	result := getEnvFloatOrDefault("ANALYZER_FLOAT_VAR", 0.3)
	if result != 0.7 {
		t.Errorf("Expected 0.7, got %f", result)
	}
}

func TestGetEnvFloatOrDefault_Unset(t *testing.T) {
	os.Unsetenv("ANALYZER_FLOAT_MISSING")
	result := getEnvFloatOrDefault("ANALYZER_FLOAT_MISSING", 0.42)
	if result != 0.42 {
		t.Errorf("Expected 0.42, got %f", result)
	}
}

func TestGetEnvFloatOrDefault_Zero(t *testing.T) {
	os.Setenv("ANALYZER_FLOAT_ZERO", "0.0")
	defer os.Unsetenv("ANALYZER_FLOAT_ZERO")

	result := getEnvFloatOrDefault("ANALYZER_FLOAT_ZERO", 1.5)
	if result != 0.0 {
		t.Errorf("Expected 0.0, got %f", result)
	}
}

// ---------------------------------------------------------------------------
// SSE subscribe / unsubscribe Tests
// ---------------------------------------------------------------------------

func TestSubscribeUnsubscribe(t *testing.T) {
	a := newTestAnalyzer()

	ch := a.subscribe()
	if ch == nil {
		t.Fatal("subscribe() returned nil channel")
	}

	a.subMu.RLock()
	if len(a.subscribers) != 1 {
		t.Errorf("Expected 1 subscriber, got %d", len(a.subscribers))
	}
	a.subMu.RUnlock()

	a.unsubscribe(ch)

	a.subMu.RLock()
	if len(a.subscribers) != 0 {
		t.Errorf("Expected 0 subscribers after unsubscribe, got %d", len(a.subscribers))
	}
	a.subMu.RUnlock()
}

func TestMultipleSubscribers(t *testing.T) {
	a := newTestAnalyzer()

	ch1 := a.subscribe()
	ch2 := a.subscribe()
	ch3 := a.subscribe()

	a.subMu.RLock()
	if len(a.subscribers) != 3 {
		t.Errorf("Expected 3 subscribers, got %d", len(a.subscribers))
	}
	a.subMu.RUnlock()

	a.unsubscribe(ch2)

	a.subMu.RLock()
	if len(a.subscribers) != 2 {
		t.Errorf("Expected 2 subscribers after removing one, got %d", len(a.subscribers))
	}
	a.subMu.RUnlock()

	a.unsubscribe(ch1)
	a.unsubscribe(ch3)

	a.subMu.RLock()
	if len(a.subscribers) != 0 {
		t.Errorf("Expected 0 subscribers after removing all, got %d", len(a.subscribers))
	}
	a.subMu.RUnlock()
}

func TestSubscribe_ChannelIsBuffered(t *testing.T) {
	a := newTestAnalyzer()
	ch := a.subscribe()
	defer a.unsubscribe(ch)

	if cap(ch) != 1 {
		t.Errorf("Expected channel buffer capacity 1, got %d", cap(ch))
	}
}

// ---------------------------------------------------------------------------
// Analyzer initMetrics no-panic test
// ---------------------------------------------------------------------------

func TestAnalyzerInitMetrics_NoPanic(t *testing.T) {
	// Each newTestAnalyzer call creates its own prometheus.Registry,
	// so calling it multiple times must not panic.
	a1 := newTestAnalyzer()
	a2 := newTestAnalyzer()
	_ = a1
	_ = a2
}

func TestAnalyzerInitMetrics_RegistryNotNil(t *testing.T) {
	a := newTestAnalyzer()
	if a.registry == nil {
		t.Error("Expected non-nil prometheus registry after initMetrics")
	}
	if a.analysisRuns == nil {
		t.Error("Expected non-nil analysisRuns counter")
	}
	if a.analysisErrors == nil {
		t.Error("Expected non-nil analysisErrors counter")
	}
	if a.analysisDuration == nil {
		t.Error("Expected non-nil analysisDuration histogram")
	}
	if a.llmTokensUsed == nil {
		t.Error("Expected non-nil llmTokensUsed counter")
	}
	if a.healthScore == nil {
		t.Error("Expected non-nil healthScore gauge")
	}
}

// ---------------------------------------------------------------------------
// buildHealthReport Tests
// ---------------------------------------------------------------------------

func TestBuildHealthReport_Empty(t *testing.T) {
	a := newTestAnalyzer()

	report := a.buildHealthReport(nil, nil, nil)

	if report.ClusterID != "test-cluster" {
		t.Errorf("Expected test-cluster, got %s", report.ClusterID)
	}
	if report.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}
	// No LLM response → Scores MUST be nil (no hardcoded defaults).
	if report.Scores != nil {
		t.Errorf("Expected Scores to be nil when no LLM response, got %+v", report.Scores)
	}
	if report.Summary.TotalPods != 0 {
		t.Errorf("Expected 0 pods, got %d", report.Summary.TotalPods)
	}
	if report.Summary.TotalNodes != 0 {
		t.Errorf("Expected 0 nodes, got %d", report.Summary.TotalNodes)
	}
	if report.Summary.WarningEvents != 0 {
		t.Errorf("Expected 0 warning events, got %d", report.Summary.WarningEvents)
	}
	if report.Summary.Namespaces == nil {
		t.Error("Expected non-nil Namespaces map")
	}
}

func TestBuildHealthReport_WithMetrics(t *testing.T) {
	a := newTestAnalyzer()

	metrics := []types.ResourceMetrics{
		{
			ResourceType: "pod",
			Resource:     types.ResourceIdentifier{Namespace: "default", Name: "pod-1"},
			Metrics: map[string]any{
				"cpu_millicores":           float64(250),
				"memory_bytes":             float64(536870912), // 0.5 Gi
				"cpu_requested_millicores": float64(500),
				"memory_requested_bytes":   float64(1073741824), // 1 Gi
			},
		},
		{
			ResourceType: "pod",
			Resource:     types.ResourceIdentifier{Namespace: "kube-system", Name: "coredns"},
			Metrics: map[string]any{
				"cpu_millicores":           float64(100),
				"memory_bytes":             float64(268435456), // 0.25 Gi
				"cpu_requested_millicores": float64(200),
				"memory_requested_bytes":   float64(536870912), // 0.5 Gi
			},
		},
		{
			ResourceType: "node",
			Resource:     types.ResourceIdentifier{Name: "node-1"},
			Metrics: map[string]any{
				"cpu_millicores":          float64(4000),
				"memory_bytes":            float64(8589934592),
				"cpu_capacity_millicores": float64(8000),
				"memory_capacity_bytes":   float64(17179869184),
			},
		},
	}

	report := a.buildHealthReport(nil, metrics, nil)

	if report.Summary.TotalPods != 2 {
		t.Errorf("Expected 2 pods, got %d", report.Summary.TotalPods)
	}
	if report.Summary.TotalNodes != 1 {
		t.Errorf("Expected 1 node, got %d", report.Summary.TotalNodes)
	}
	if report.ResourceUtilization.CPU.Unit != "cores" {
		t.Errorf("Expected 'cores' unit, got %s", report.ResourceUtilization.CPU.Unit)
	}
	if report.ResourceUtilization.Memory.Unit != "Gi" {
		t.Errorf("Expected 'Gi' unit, got %s", report.ResourceUtilization.Memory.Unit)
	}
	// CPU used: (250 + 100) / 1000 = 0.35 cores
	if report.ResourceUtilization.CPU.Used != 0.35 {
		t.Errorf("Expected CPU used 0.35, got %f", report.ResourceUtilization.CPU.Used)
	}
	// CPU requested: (500 + 200) / 1000 = 0.7 cores
	if report.ResourceUtilization.CPU.Requested != 0.7 {
		t.Errorf("Expected CPU requested 0.7, got %f", report.ResourceUtilization.CPU.Requested)
	}
	// CPU capacity: 8000 / 1000 = 8 cores
	if report.ResourceUtilization.CPU.Capacity != 8.0 {
		t.Errorf("Expected CPU capacity 8.0, got %f", report.ResourceUtilization.CPU.Capacity)
	}
	// Namespace pod counts
	if ns := report.Summary.Namespaces["default"]; ns == nil || ns.PodCount != 1 {
		t.Errorf("Expected 1 pod in default namespace, got %v", ns)
	}
	if ns := report.Summary.Namespaces["kube-system"]; ns == nil || ns.PodCount != 1 {
		t.Errorf("Expected 1 pod in kube-system namespace, got %v", ns)
	}
}

func TestBuildHealthReport_WithPVCMetrics(t *testing.T) {
	a := newTestAnalyzer()

	metrics := []types.ResourceMetrics{
		{
			ResourceType: "pvc",
			Resource:     types.ResourceIdentifier{Namespace: "default", Name: "data-pvc"},
			Metrics: map[string]any{
				"capacity_bytes": float64(1099511627776), // 1 Ti
				"used_bytes":     float64(549755813888),  // 0.5 Ti
			},
		},
	}

	report := a.buildHealthReport(nil, metrics, nil)

	if report.ResourceUtilization.Storage.Unit != "Ti" {
		t.Errorf("Expected 'Ti' unit, got %s", report.ResourceUtilization.Storage.Unit)
	}
	if report.ResourceUtilization.Storage.Capacity != 1.0 {
		t.Errorf("Expected storage capacity 1.0, got %f", report.ResourceUtilization.Storage.Capacity)
	}
	if report.ResourceUtilization.Storage.Used != 0.5 {
		t.Errorf("Expected storage used 0.5, got %f", report.ResourceUtilization.Storage.Used)
	}
}

func TestBuildHealthReport_WithLLMResponse(t *testing.T) {
	a := newTestAnalyzer()

	llmResp := map[string]any{
		"healthScores": map[string]any{
			"reliability":  float64(80),
			"security":     float64(70),
			"cost":         float64(60),
			"architecture": float64(90),
		},
		"issues": []any{
			map[string]any{
				"severity":    "high",
				"category":    "reliability",
				"title":       "Pod crash loop detected",
				"description": "Pod is crashing repeatedly",
				"rootCause":   "OOM",
				"confidence":  float64(0.9),
			},
		},
		"recommendations": []any{
			map[string]any{
				"category":         "cost",
				"title":            "Right-size deployment",
				"description":      "Reduce CPU request",
				"priority":         float64(3),
				"estimatedSavings": float64(50.0),
				"effort":           "low",
				"risk":             "low",
			},
		},
	}

	report := a.buildHealthReport(nil, nil, llmResp)

	if report.Scores == nil {
		t.Fatal("Expected Scores to be populated when LLM returns healthScores, got nil")
	}
	if report.Scores.Reliability != 80 {
		t.Errorf("Expected reliability 80, got %d", report.Scores.Reliability)
	}
	if report.Scores.Security != 70 {
		t.Errorf("Expected security 70, got %d", report.Scores.Security)
	}
	if report.Scores.Cost != 60 {
		t.Errorf("Expected cost 60, got %d", report.Scores.Cost)
	}
	if report.Scores.Architecture != 90 {
		t.Errorf("Expected architecture 90, got %d", report.Scores.Architecture)
	}

	// Overall should be calculated via CalculateOverallScore
	expectedOverall := types.CalculateOverallScore(*report.Scores)
	if report.Scores.Overall != expectedOverall {
		t.Errorf("Expected overall %d, got %d", expectedOverall, report.Scores.Overall)
	}

	// Issues
	if len(report.TopIssues) != 1 {
		t.Fatalf("Expected 1 issue, got %d", len(report.TopIssues))
	}
	if report.TopIssues[0].Severity != "high" {
		t.Errorf("Expected high severity, got %s", report.TopIssues[0].Severity)
	}
	if report.TopIssues[0].Title != "Pod crash loop detected" {
		t.Errorf("Expected 'Pod crash loop detected', got %s", report.TopIssues[0].Title)
	}
	if report.TopIssues[0].RootCause != "OOM" {
		t.Errorf("Expected root cause 'OOM', got %s", report.TopIssues[0].RootCause)
	}
	if report.TopIssues[0].Confidence != 0.9 {
		t.Errorf("Expected confidence 0.9, got %f", report.TopIssues[0].Confidence)
	}
	if report.TopIssues[0].ID != "issue-0" {
		t.Errorf("Expected ID 'issue-0', got %s", report.TopIssues[0].ID)
	}

	// Recommendations
	if len(report.Recommendations) != 1 {
		t.Fatalf("Expected 1 recommendation, got %d", len(report.Recommendations))
	}
	rec := report.Recommendations[0]
	if rec.ID != "rec-0" {
		t.Errorf("Expected ID 'rec-0', got %s", rec.ID)
	}
	if rec.Category != "cost" {
		t.Errorf("Expected category 'cost', got %s", rec.Category)
	}
	// priority 3 maps to "high"
	if rec.Severity != "high" {
		t.Errorf("Expected severity 'high' (mapped from priority 3), got %s", rec.Severity)
	}
	if rec.Impact.Effort != "low" {
		t.Errorf("Expected effort 'low', got %s", rec.Impact.Effort)
	}
	if rec.Impact.RiskLevel != "low" {
		t.Errorf("Expected risk 'low', got %s", rec.Impact.RiskLevel)
	}
	if rec.Impact.CostSavings == nil || rec.Impact.CostSavings.Monthly != 50.0 {
		t.Errorf("Expected monthly savings 50.0, got %v", rec.Impact.CostSavings)
	}
	if rec.Impact.CostSavings.Currency != "USD" {
		t.Errorf("Expected currency 'USD', got %s", rec.Impact.CostSavings.Currency)
	}
	if report.EstimatedSavings != 50.0 {
		t.Errorf("Expected estimated savings 50.0, got %f", report.EstimatedSavings)
	}
}

func TestBuildHealthReport_WithMultipleRecommendations_SumsEstimatedSavings(t *testing.T) {
	a := newTestAnalyzer()

	llmResp := map[string]any{
		"healthScores": map[string]any{
			"reliability":  float64(85),
			"security":     float64(85),
			"cost":         float64(85),
			"architecture": float64(85),
		},
		"recommendations": []any{
			map[string]any{
				"category":         "cost",
				"title":            "Rec 1",
				"description":      "First",
				"priority":         float64(5),
				"estimatedSavings": float64(30.0),
				"effort":           "low",
				"risk":             "low",
			},
			map[string]any{
				"category":         "cost",
				"title":            "Rec 2",
				"description":      "Second",
				"priority":         float64(1),
				"estimatedSavings": float64(70.0),
				"effort":           "high",
				"risk":             "medium",
			},
		},
	}

	report := a.buildHealthReport(nil, nil, llmResp)

	if len(report.Recommendations) != 2 {
		t.Fatalf("Expected 2 recommendations, got %d", len(report.Recommendations))
	}
	if report.EstimatedSavings != 100.0 {
		t.Errorf("Expected total estimated savings 100.0, got %f", report.EstimatedSavings)
	}
	// Check priority mapping: priority 5 -> medium, priority 1 -> critical
	if report.Recommendations[0].Severity != "medium" {
		t.Errorf("Expected severity 'medium' (priority 5), got %s", report.Recommendations[0].Severity)
	}
	if report.Recommendations[1].Severity != "critical" {
		t.Errorf("Expected severity 'critical' (priority 1), got %s", report.Recommendations[1].Severity)
	}
}

func TestBuildHealthReport_WithWarningEvents(t *testing.T) {
	a := newTestAnalyzer()

	events := []types.TelemetryEvent{
		{Type: "Warning", InvolvedObject: types.InvolvedObject{Namespace: "default"}},
		{Type: "Warning", InvolvedObject: types.InvolvedObject{Namespace: "default"}},
		{Type: "Normal", InvolvedObject: types.InvolvedObject{Namespace: "kube-system"}},
	}

	report := a.buildHealthReport(events, nil, nil)

	if report.Summary.WarningEvents != 2 {
		t.Errorf("Expected 2 warning events, got %d", report.Summary.WarningEvents)
	}

	// Namespace warning counts
	if ns := report.Summary.Namespaces["default"]; ns == nil || ns.Warnings != 2 {
		t.Errorf("Expected 2 warnings in 'default' namespace, got %v", ns)
	}
}

func TestBuildHealthReport_HealthyPodsFloor(t *testing.T) {
	a := newTestAnalyzer()

	// No metrics means 0 pods, but 3 issues. HealthyPods should floor at 0.
	llmResp := map[string]any{
		"healthScores": map[string]any{
			"reliability":  float64(85),
			"security":     float64(85),
			"cost":         float64(85),
			"architecture": float64(85),
		},
		"issues": []any{
			map[string]any{"severity": "high", "title": "Issue 1"},
			map[string]any{"severity": "high", "title": "Issue 2"},
			map[string]any{"severity": "high", "title": "Issue 3"},
		},
	}

	report := a.buildHealthReport(nil, nil, llmResp)

	if report.Summary.HealthyPods != 0 {
		t.Errorf("Expected HealthyPods floored at 0, got %d", report.Summary.HealthyPods)
	}
}

// ---------------------------------------------------------------------------
// HTTP Handler Tests
// ---------------------------------------------------------------------------

func TestHandleHealthReport_NoReport(t *testing.T) {
	a := newTestAnalyzer()

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	a.handleHealthReport(rr, req)

	// When there's no report yet, the handler now returns 200 with a
	// diagnostic "awaiting" report rather than 503 so the dashboard can
	// render the status block and help the operator diagnose.
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200 with diagnostic report, got %d", rr.Code)
	}
	var diag types.ClusterHealthReport
	if err := json.NewDecoder(rr.Body).Decode(&diag); err != nil {
		t.Fatalf("Failed to decode diagnostic report: %v", err)
	}
	if diag.Scores != nil {
		t.Errorf("Expected diagnostic report Scores to be nil, got %+v", diag.Scores)
	}
	if diag.Status == nil {
		t.Fatal("Expected diagnostic report to have a Status block")
	}
	if diag.Status.State != types.StateAwaiting {
		t.Errorf("Expected Status.State=%q, got %q", types.StateAwaiting, diag.Status.State)
	}
	if diag.Status.Profile != types.ProfileLive {
		t.Errorf("Expected Status.Profile=%q, got %q", types.ProfileLive, diag.Status.Profile)
	}
}

func TestHandleHealthReport_WithReport(t *testing.T) {
	a := newTestAnalyzer()
	a.latestReport = &types.ClusterHealthReport{
		ClusterID: "test",
		Scores:    &types.HealthScores{Overall: 85},
	}

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	a.handleHealthReport(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
	}

	var report types.ClusterHealthReport
	if err := json.NewDecoder(rr.Body).Decode(&report); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}
	if report.Scores == nil {
		t.Fatal("Expected Scores to be populated")
	}
	if report.Scores.Overall != 85 {
		t.Errorf("Expected overall 85, got %d", report.Scores.Overall)
	}
	if report.ClusterID != "test" {
		t.Errorf("Expected clusterID 'test', got %q", report.ClusterID)
	}
}

func TestHandleScores_NoReport(t *testing.T) {
	a := newTestAnalyzer()

	req := httptest.NewRequest("GET", "/api/v1/scores", nil)
	rr := httptest.NewRecorder()
	a.handleScores(rr, req)

	// No report → 200 with a JSON null body so clients can distinguish
	// "scores unavailable" from "server error".
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
	if body := strings.TrimSpace(rr.Body.String()); body != "null" {
		t.Errorf("Expected body 'null', got %q", body)
	}
}

func TestHandleScores_WithReport(t *testing.T) {
	a := newTestAnalyzer()
	a.latestReport = &types.ClusterHealthReport{
		Scores: &types.HealthScores{
			Overall:      85,
			Reliability:  90,
			Security:     80,
			Cost:         70,
			Architecture: 88,
		},
	}

	req := httptest.NewRequest("GET", "/api/v1/scores", nil)
	rr := httptest.NewRecorder()
	a.handleScores(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	var scores types.HealthScores
	if err := json.NewDecoder(rr.Body).Decode(&scores); err != nil {
		t.Fatalf("Failed to decode scores: %v", err)
	}
	if scores.Overall != 85 {
		t.Errorf("Expected overall 85, got %d", scores.Overall)
	}
	if scores.Security != 80 {
		t.Errorf("Expected security 80, got %d", scores.Security)
	}
	if scores.Architecture != 88 {
		t.Errorf("Expected architecture 88, got %d", scores.Architecture)
	}
}

// TestHandleScores_WithNilScores covers the case where a report exists but
// the LLM call failed, so Scores is nil. handleScores should still return
// null, not a zero-valued struct.
func TestHandleScores_WithNilScores(t *testing.T) {
	a := newTestAnalyzer()
	a.latestReport = &types.ClusterHealthReport{
		ClusterID: "test",
		Scores:    nil,
		Status: &types.ReportStatus{
			State:   types.StateDegraded,
			Profile: types.ProfileLive,
		},
	}

	req := httptest.NewRequest("GET", "/api/v1/scores", nil)
	rr := httptest.NewRecorder()
	a.handleScores(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
	if body := strings.TrimSpace(rr.Body.String()); body != "null" {
		t.Errorf("Expected body 'null', got %q", body)
	}
}

func TestHandleIssues_NoReport(t *testing.T) {
	a := newTestAnalyzer()
	req := httptest.NewRequest("GET", "/api/v1/issues", nil)
	rr := httptest.NewRecorder()
	a.handleIssues(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
	if body := strings.TrimSpace(rr.Body.String()); body != "[]" {
		t.Errorf("Expected body '[]', got %q", body)
	}
}

func TestHandleIssues_WithReport(t *testing.T) {
	a := newTestAnalyzer()
	a.latestReport = &types.ClusterHealthReport{
		TopIssues: []types.Issue{
			{ID: "issue-1", Severity: "high", Title: "OOM Kill"},
			{ID: "issue-2", Severity: "low", Title: "Slow start"},
		},
	}

	req := httptest.NewRequest("GET", "/api/v1/issues", nil)
	rr := httptest.NewRecorder()
	a.handleIssues(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	var issues []types.Issue
	if err := json.NewDecoder(rr.Body).Decode(&issues); err != nil {
		t.Fatalf("Failed to decode issues: %v", err)
	}
	if len(issues) != 2 {
		t.Errorf("Expected 2 issues, got %d", len(issues))
	}
}

func TestHandleRecommendations_NoReport(t *testing.T) {
	a := newTestAnalyzer()

	req := httptest.NewRequest("GET", "/api/v1/recommendations", nil)
	rr := httptest.NewRecorder()
	a.handleRecommendations(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
	if body := strings.TrimSpace(rr.Body.String()); body != "[]" {
		t.Errorf("Expected body '[]', got %q", body)
	}
}

func TestHandleRecommendations_WithReport(t *testing.T) {
	a := newTestAnalyzer()
	a.latestReport = &types.ClusterHealthReport{
		Recommendations: []types.Recommendation{
			{ID: "rec-1", Title: "Right-size pods"},
		},
	}

	req := httptest.NewRequest("GET", "/api/v1/recommendations", nil)
	rr := httptest.NewRecorder()
	a.handleRecommendations(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	var recs []types.Recommendation
	if err := json.NewDecoder(rr.Body).Decode(&recs); err != nil {
		t.Fatalf("Failed to decode recommendations: %v", err)
	}
	if len(recs) != 1 {
		t.Errorf("Expected 1 recommendation, got %d", len(recs))
	}
	if recs[0].Title != "Right-size pods" {
		t.Errorf("Expected title 'Right-size pods', got %q", recs[0].Title)
	}
}

func TestHandleTriggerAnalysis_PostOnly(t *testing.T) {
	a := newTestAnalyzer()

	// GET should be rejected
	req := httptest.NewRequest("GET", "/api/v1/analysis/trigger", nil)
	rr := httptest.NewRecorder()
	a.handleTriggerAnalysis(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for GET, got %d", rr.Code)
	}
}

func TestHandleTriggerAnalysis_PutRejected(t *testing.T) {
	a := newTestAnalyzer()

	req := httptest.NewRequest("PUT", "/api/v1/analysis/trigger", nil)
	rr := httptest.NewRecorder()
	a.handleTriggerAnalysis(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for PUT, got %d", rr.Code)
	}
}

func TestHandleHistory_Empty(t *testing.T) {
	a := newTestAnalyzer()

	req := httptest.NewRequest("GET", "/api/v1/history", nil)
	rr := httptest.NewRecorder()
	a.handleHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
	}
}

func TestHandleHistory_WithData(t *testing.T) {
	a := newTestAnalyzer()
	a.reportHistory = []*types.ClusterHealthReport{
		{ClusterID: "test-1", Scores: &types.HealthScores{Overall: 80}},
		{ClusterID: "test-2", Scores: &types.HealthScores{Overall: 90}},
	}

	req := httptest.NewRequest("GET", "/api/v1/history", nil)
	rr := httptest.NewRecorder()
	a.handleHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	var history []*types.ClusterHealthReport
	if err := json.NewDecoder(rr.Body).Decode(&history); err != nil {
		t.Fatalf("Failed to decode history: %v", err)
	}
	if len(history) != 2 {
		t.Errorf("Expected 2 history items, got %d", len(history))
	}
	if history[0].ClusterID != "test-1" {
		t.Errorf("Expected first entry 'test-1', got %q", history[0].ClusterID)
	}
	if history[1].ClusterID != "test-2" {
		t.Errorf("Expected second entry 'test-2', got %q", history[1].ClusterID)
	}
}

// ---------------------------------------------------------------------------
// NewAnalyzer constructor test (does not start servers or loops)
// ---------------------------------------------------------------------------

func TestNewAnalyzer(t *testing.T) {
	cfg := Config{
		ClusterID:        "my-cluster",
		CollectorURL:     "http://localhost:8080",
		LLMBackend:       "openai",
		LLMEndpoint:      "http://localhost:11434/v1",
		LLMModel:         "gpt-4",
		AnalysisInterval: 5 * time.Minute,
		MetricsPort:      9091,
		APIPort:          8081,
		MaxTokens:        4096,
		Temperature:      0.3,
	}

	analyzer, err := NewAnalyzer(cfg)
	if err != nil {
		t.Fatalf("NewAnalyzer returned error: %v", err)
	}
	if analyzer == nil {
		t.Fatal("NewAnalyzer returned nil")
	}
	if analyzer.config.ClusterID != "my-cluster" {
		t.Errorf("Expected cluster ID 'my-cluster', got %q", analyzer.config.ClusterID)
	}
	if analyzer.promptTemplates == nil {
		t.Error("Expected non-nil promptTemplates")
	}
	if _, ok := analyzer.promptTemplates["rca"]; !ok {
		t.Error("Expected 'rca' prompt template to be initialized")
	}
	if _, ok := analyzer.promptTemplates["security"]; !ok {
		t.Error("Expected 'security' prompt template to be initialized")
	}
	if _, ok := analyzer.promptTemplates["cost"]; !ok {
		t.Error("Expected 'cost' prompt template to be initialized")
	}
	if analyzer.subscribers == nil {
		t.Error("Expected non-nil subscribers map")
	}
	if analyzer.registry == nil {
		t.Error("Expected non-nil prometheus registry")
	}
	// New fields added for Live/Mock profile support.
	if analyzer.profile != types.ProfileLive {
		t.Errorf("Expected default profile %q, got %q", types.ProfileLive, analyzer.profile)
	}
	if analyzer.mockSource == nil {
		t.Error("Expected non-nil mockSource")
	}
}

// ---------------------------------------------------------------------------
// Profile, status, and diagnostic report tests
// ---------------------------------------------------------------------------

func TestResolveProfile(t *testing.T) {
	cases := map[string]string{
		"live":    types.ProfileLive,
		"LIVE":    types.ProfileLive,
		"  live ": types.ProfileLive,
		"mock":    types.ProfileMock,
		"Mock":    types.ProfileMock,
		"":        types.ProfileLive,
		"garbage": types.ProfileLive,
	}
	for in, expected := range cases {
		if got := resolveProfile(in); got != expected {
			t.Errorf("resolveProfile(%q) = %q, want %q", in, got, expected)
		}
	}
}

func TestSetProfile_SwitchAndReject(t *testing.T) {
	a := newTestAnalyzer()

	// Default is live.
	if a.getProfile() != types.ProfileLive {
		t.Fatalf("expected default live, got %q", a.getProfile())
	}

	// Switch to mock.
	got, err := a.setProfile("mock")
	if err != nil {
		t.Fatalf("setProfile(mock) returned error: %v", err)
	}
	if got != types.ProfileMock {
		t.Errorf("setProfile returned %q, want %q", got, types.ProfileMock)
	}
	if a.getProfile() != types.ProfileMock {
		t.Errorf("getProfile = %q after switch, want %q", a.getProfile(), types.ProfileMock)
	}

	// Switching to mock should have triggered an immediate mock report.
	a.reportMu.RLock()
	latest := a.latestReport
	a.reportMu.RUnlock()
	if latest == nil {
		t.Fatal("Expected a mock report to be published immediately after switching to mock")
	}
	if latest.Scores == nil {
		t.Fatal("Expected mock report to have Scores populated")
	}
	if latest.Status == nil || latest.Status.Profile != types.ProfileMock {
		t.Errorf("Expected mock report Status.Profile=%q", types.ProfileMock)
	}

	// Switch back to live.
	if _, err := a.setProfile("live"); err != nil {
		t.Fatalf("setProfile(live) returned error: %v", err)
	}
	a.reportMu.RLock()
	latestAfter := a.latestReport
	a.reportMu.RUnlock()
	if latestAfter != nil {
		t.Error("Expected latestReport to be cleared when switching back to live")
	}

	// Unknown profile should be rejected.
	if _, err := a.setProfile("garbage"); err == nil {
		t.Error("Expected error when setting unknown profile")
	}
}

func TestHandleProfile_Get(t *testing.T) {
	a := newTestAnalyzer()
	req := httptest.NewRequest("GET", "/api/v1/profile", nil)
	rr := httptest.NewRecorder()
	a.handleProfile(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}
	var body struct {
		Profile   string   `json:"profile"`
		Available []string `json:"available"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Profile != types.ProfileLive {
		t.Errorf("expected profile=%q, got %q", types.ProfileLive, body.Profile)
	}
	if len(body.Available) != 2 {
		t.Errorf("expected 2 available profiles, got %d", len(body.Available))
	}
}

func TestHandleProfile_PostSwitchesAndRejects(t *testing.T) {
	a := newTestAnalyzer()

	// POST with valid payload.
	postReq := httptest.NewRequest("POST", "/api/v1/profile",
		strings.NewReader(`{"profile":"mock"}`))
	postReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	a.handleProfile(rr, postReq)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if a.getProfile() != types.ProfileMock {
		t.Errorf("expected profile to be %q after POST, got %q", types.ProfileMock, a.getProfile())
	}

	// POST with invalid payload.
	badReq := httptest.NewRequest("POST", "/api/v1/profile",
		strings.NewReader(`{"profile":"nonsense"}`))
	badReq.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	a.handleProfile(rr2, badReq)
	if rr2.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for unknown profile, got %d", rr2.Code)
	}

	// Wrong method.
	putReq := httptest.NewRequest("PUT", "/api/v1/profile", nil)
	rr3 := httptest.NewRecorder()
	a.handleProfile(rr3, putReq)
	if rr3.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for PUT, got %d", rr3.Code)
	}
}

func TestHandleStatus(t *testing.T) {
	a := newTestAnalyzer()
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	rr := httptest.NewRecorder()
	a.handleStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}
	var body struct {
		Status    types.ReportStatus `json:"status"`
		HasReport bool               `json:"hasReport"`
		HasScores bool               `json:"hasScores"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.HasReport {
		t.Error("Expected hasReport=false on fresh analyzer")
	}
	if body.HasScores {
		t.Error("Expected hasScores=false on fresh analyzer")
	}
	if body.Status.Profile != types.ProfileLive {
		t.Errorf("Expected status.profile=%q, got %q", types.ProfileLive, body.Status.Profile)
	}
	if body.Status.State != types.StateAwaiting {
		t.Errorf("Expected status.state=%q, got %q", types.StateAwaiting, body.Status.State)
	}
}

func TestBuildDiagnosticReport(t *testing.T) {
	a := newTestAnalyzer()
	r := a.buildDiagnosticReport()
	if r.Scores != nil {
		t.Error("Diagnostic report should have nil Scores")
	}
	if r.Status == nil {
		t.Fatal("Diagnostic report should have a Status block")
	}
	if r.Status.State != types.StateAwaiting {
		t.Errorf("Expected state=awaiting, got %q", r.Status.State)
	}
	if r.Status.Profile != types.ProfileLive {
		t.Errorf("Expected profile=live, got %q", r.Status.Profile)
	}
	// Empty-slice JSON marshaling is important for the dashboard — make
	// sure the diagnostic report doesn't serialize these as null.
	if r.TopIssues == nil || r.Recommendations == nil || r.SecurityFindings == nil {
		t.Error("Slices should be empty (not nil) so JSON marshals to [], not null")
	}
}

func TestBuildReportStatus_StatesFromDiagnostics(t *testing.T) {
	a := newTestAnalyzer()

	// Fresh analyzer → awaiting.
	st := a.buildReportStatus(false)
	if st.State != types.StateAwaiting {
		t.Errorf("fresh → expected awaiting, got %q", st.State)
	}

	// Collector broken → error state (even after an analysis attempt).
	now := time.Now()
	a.diagMu.Lock()
	a.lastAnalysisAt = &now
	a.collectorReachable = false
	a.collectorLastError = "connection refused"
	a.llmReachable = false
	a.diagMu.Unlock()
	st = a.buildReportStatus(false)
	if st.State != types.StateError {
		t.Errorf("collector down → expected error, got %q", st.State)
	}
	if st.Collector.LastError != "connection refused" {
		t.Errorf("expected collector error to flow through, got %q", st.Collector.LastError)
	}

	// Collector ok, LLM broken, still no scores → degraded.
	a.diagMu.Lock()
	a.collectorReachable = true
	a.collectorLastOKAt = &now
	a.collectorLastError = ""
	a.llmReachable = false
	a.llmLastError = "llm timeout"
	a.diagMu.Unlock()
	st = a.buildReportStatus(false)
	if st.State != types.StateDegraded {
		t.Errorf("LLM down → expected degraded, got %q", st.State)
	}

	// Collector and LLM ok, scores present → ok.
	a.diagMu.Lock()
	a.llmReachable = true
	a.llmLastOKAt = &now
	a.llmLastError = ""
	a.diagMu.Unlock()
	st = a.buildReportStatus(true)
	if st.State != types.StateOK {
		t.Errorf("all ok → expected ok, got %q", st.State)
	}

	// Switched to mock → always ok regardless of diagnostics.
	if _, err := a.setProfile("mock"); err != nil {
		t.Fatalf("setProfile(mock): %v", err)
	}
	st = a.buildReportStatus(false)
	if st.State != types.StateOK {
		t.Errorf("mock profile → expected ok, got %q", st.State)
	}
	if st.Profile != types.ProfileMock {
		t.Errorf("mock profile → expected profile=mock, got %q", st.Profile)
	}
}

func TestDiagRecorders(t *testing.T) {
	a := newTestAnalyzer()

	a.recordCollectorSuccess()
	a.diagMu.RLock()
	ok := a.collectorReachable && a.collectorLastOKAt != nil
	a.diagMu.RUnlock()
	if !ok {
		t.Error("recordCollectorSuccess should set reachable=true and timestamp")
	}

	a.recordCollectorError(fmt.Errorf("boom"))
	a.diagMu.RLock()
	down := !a.collectorReachable && a.collectorLastError == "boom"
	a.diagMu.RUnlock()
	if !down {
		t.Error("recordCollectorError should set reachable=false and error message")
	}

	a.recordLLMSuccess()
	a.recordLLMError(fmt.Errorf("oops"))
	a.diagMu.RLock()
	llmdown := !a.llmReachable && a.llmLastError == "oops"
	a.diagMu.RUnlock()
	if !llmdown {
		t.Error("recordLLMError should set reachable=false and error")
	}

	a.recordAnalysisOutcome(fmt.Errorf("failure"))
	a.diagMu.RLock()
	hasErr := a.lastAnalysisError == "failure" && a.lastAnalysisAt != nil
	a.diagMu.RUnlock()
	if !hasErr {
		t.Error("recordAnalysisOutcome should set error and timestamp")
	}

	a.recordAnalysisOutcome(nil)
	a.diagMu.RLock()
	cleared := a.lastAnalysisError == ""
	a.diagMu.RUnlock()
	if !cleared {
		t.Error("recordAnalysisOutcome(nil) should clear error")
	}
}
