package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	types "github.com/hellodk/hetu/pkg/types"
)

// Keep the time import; new live-path tests below use time.Now() to
// seed realistic timestamps on correlator incidents and error groups.

// newBreakdownMux builds a mux with the three drill-down routes for an
// analyzer. Mirrors how main wires them in Start().
func newBreakdownMux(a *Analyzer) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health/breakdown", a.handleHealthBreakdown)
	mux.HandleFunc("GET /api/v1/health/breakdown/{dimension}/{ruleIndex}", a.handleRuleBreakdown)
	mux.HandleFunc("GET /api/v1/health/resource-impact", a.handleResourceImpact)
	return mux
}

// breakdownResponse mirrors the anonymous struct returned by
// handleHealthBreakdown for test assertions.
type breakdownResponse struct {
	Reliability struct {
		Score   int `json:"score"`
		Factors []struct {
			Name      string   `json:"name"`
			Impact    int      `json:"impact"`
			Resources []string `json:"resources,omitempty"`
			Severity  string   `json:"severity,omitempty"`
		} `json:"factors"`
	} `json:"reliability"`
	Security struct {
		Score   int           `json:"score"`
		Factors []interface{} `json:"factors"`
	} `json:"security"`
	Cost struct {
		Score   int           `json:"score"`
		Factors []interface{} `json:"factors"`
	} `json:"cost"`
	Architecture struct {
		Score   int           `json:"score"`
		Factors []interface{} `json:"factors"`
	} `json:"architecture"`
}

// --- handleHealthBreakdown --------------------------------------------------

func TestHandleHealthBreakdown_WarmingUp(t *testing.T) {
	a := newTestAnalyzer()
	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/breakdown")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var bd breakdownResponse
	if err := json.NewDecoder(resp.Body).Decode(&bd); err != nil {
		t.Fatal(err)
	}
	if len(bd.Reliability.Factors) != 0 {
		t.Errorf("expected empty reliability factors on cold start, got %d", len(bd.Reliability.Factors))
	}
}

func TestHandleHealthBreakdown_SourcesFromCache(t *testing.T) {
	a := newTestAnalyzer()
	a.lastReliability = ScoreResult{
		Score: 85,
		Deductions: []ScoreDeduction{
			{Rule: "CrashLoopBackOff pods", Impact: -10, Count: 2,
				Resources: []string{"ns/a", "ns/b"}, AllResources: []string{"ns/a", "ns/b"}},
			{Rule: "Pending pods", Impact: -5, Count: 1,
				Resources: []string{"ns/c"}, AllResources: []string{"ns/c"}},
		},
	}
	// Also populate latestReport so the score on the response is non-zero.
	a.latestReport = &types.ClusterHealthReport{
		Scores: &types.HealthScores{Reliability: 85, Security: 90, Cost: 80, Architecture: 95},
	}

	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/breakdown")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var bd breakdownResponse
	if err := json.NewDecoder(resp.Body).Decode(&bd); err != nil {
		t.Fatal(err)
	}
	if bd.Reliability.Score != 85 {
		t.Errorf("reliability score: got %d, want 85", bd.Reliability.Score)
	}
	if len(bd.Reliability.Factors) != 2 {
		t.Fatalf("expected 2 factors, got %d", len(bd.Reliability.Factors))
	}
	if !strings.HasPrefix(bd.Reliability.Factors[0].Name, "CrashLoopBackOff pods") {
		t.Errorf("factor 0 name: got %q", bd.Reliability.Factors[0].Name)
	}
	if !strings.HasPrefix(bd.Reliability.Factors[1].Name, "Pending pods") {
		t.Errorf("factor 1 name: got %q", bd.Reliability.Factors[1].Name)
	}
}

// TestHandleHealthBreakdown_IndexAlignment is the regression test for the
// critical bug where Level-2 factor indices did not match the Level-3
// drill-down indices. Both endpoints must agree on what factor N is.
func TestHandleHealthBreakdown_IndexAlignment(t *testing.T) {
	a := newTestAnalyzer()
	a.lastReliability = ScoreResult{
		Score: 75,
		Deductions: []ScoreDeduction{
			{Rule: "CrashLoopBackOff pods", Impact: -10, Count: 2,
				AllResources: []string{"ns/a", "ns/b"}, Resources: []string{"ns/a", "ns/b"}},
			{Rule: "OOMKilled pods", Impact: -5, Count: 1,
				AllResources: []string{"ns/c"}, Resources: []string{"ns/c"}},
			{Rule: "Pending pods", Impact: -3, Count: 1,
				AllResources: []string{"ns/d"}, Resources: []string{"ns/d"}},
		},
	}

	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/breakdown")
	if err != nil {
		t.Fatal(err)
	}
	var bd breakdownResponse
	if err := json.NewDecoder(resp.Body).Decode(&bd); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Strip the trailing " (N)" count suffix that handleHealthBreakdown
	// appends to match against the raw Rule name.
	stripCount := regexp.MustCompile(`\s*\(\d+\)$`)

	for i, f := range bd.Reliability.Factors {
		url := fmt.Sprintf("%s/api/v1/health/breakdown/reliability/%d", srv.URL, i)
		r, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		if r.StatusCode != 200 {
			t.Fatalf("index %d: got status %d", i, r.StatusCode)
		}
		var rb types.RuleBreakdownResponse
		if err := json.NewDecoder(r.Body).Decode(&rb); err != nil {
			t.Fatal(err)
		}
		r.Body.Close()

		wantRule := stripCount.ReplaceAllString(f.Name, "")
		if rb.Rule != wantRule {
			t.Errorf("index %d: factor name %q → drill-down rule %q (alignment broken)",
				i, f.Name, rb.Rule)
		}
	}
}

// --- handleRuleBreakdown ----------------------------------------------------

func TestHandleRuleBreakdown_ColdStart(t *testing.T) {
	a := newTestAnalyzer()
	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/breakdown/reliability/0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "analyzer warming up" {
		t.Errorf("expected warming-up error, got %q", body["error"])
	}
}

func TestHandleRuleBreakdown_InvalidDimension(t *testing.T) {
	a := newTestAnalyzer()
	// Put something in one of the caches so warming-up doesn't short-circuit.
	a.lastReliability = ScoreResult{Deductions: []ScoreDeduction{{Rule: "x", Impact: -1}}}

	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/breakdown/nonsense/0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHandleRuleBreakdown_InvalidIndex(t *testing.T) {
	a := newTestAnalyzer()
	a.lastReliability = ScoreResult{Deductions: []ScoreDeduction{{Rule: "x", Impact: -1}}}
	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/breakdown/reliability/99")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleRuleBreakdown_StandardRule(t *testing.T) {
	a := newTestAnalyzer()
	a.lastReliability = ScoreResult{
		Deductions: []ScoreDeduction{{
			Rule: "CrashLoopBackOff pods", Impact: -10, Count: 2,
			AllResources: []string{"default/pod-1", "default/pod-2"},
		}},
	}
	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/breakdown/reliability/0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rb types.RuleBreakdownResponse
	if err := json.NewDecoder(resp.Body).Decode(&rb); err != nil {
		t.Fatal(err)
	}
	if rb.Rule != "CrashLoopBackOff pods" {
		t.Errorf("rule: got %q", rb.Rule)
	}
	if rb.TotalImpact != -10 {
		t.Errorf("total impact: got %d", rb.TotalImpact)
	}
	if len(rb.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(rb.Resources))
	}
	// Assert EVERY resource has the correct per-resource impact, kind,
	// and namespace — not just the first. A bug that only broke some
	// resources would slip past a single-index check.
	for i, r := range rb.Resources {
		if r.Impact != -5 {
			t.Errorf("resource[%d].Impact = %d, want -5 (−10/2)", i, r.Impact)
		}
		if r.Kind != "Pod" {
			t.Errorf("resource[%d].Kind = %q, want Pod", i, r.Kind)
		}
		if r.Namespace != "default" {
			t.Errorf("resource[%d].Namespace = %q, want default", i, r.Namespace)
		}
	}
}

func TestHandleRuleBreakdown_ActiveAnomaliesLive(t *testing.T) {
	a := newTestAnalyzer()
	a.anomalyDetector = NewAnomalyDetector("", "test")
	a.anomalyDetector.anomalies[1] = &Anomaly{
		ID:        1,
		Service:   "api",
		Namespace: "prod",
		Metric:    "error_rate",
		Score:     5.2,
		Severity:  "high",
		Status:    "active",
	}

	// Rule has empty AllResources — handler must fall back to live anomaly list.
	a.lastArchitecture = ScoreResult{
		Deductions: []ScoreDeduction{{Rule: "Active anomalies", Impact: -3, Count: 1}},
	}

	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/breakdown/architecture/0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rb types.RuleBreakdownResponse
	if err := json.NewDecoder(resp.Body).Decode(&rb); err != nil {
		t.Fatal(err)
	}
	if len(rb.Resources) != 1 {
		t.Fatalf("expected 1 live anomaly, got %d", len(rb.Resources))
	}
	if rb.Resources[0].Kind != "Anomaly" {
		t.Errorf("kind: got %q, want Anomaly", rb.Resources[0].Kind)
	}
	if rb.Resources[0].Name != "api" {
		t.Errorf("name: got %q", rb.Resources[0].Name)
	}
}

// --- handleResourceImpact ---------------------------------------------------

func TestHandleResourceImpact_MissingParams(t *testing.T) {
	a := newTestAnalyzer()
	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/resource-impact?namespace=default")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("missing kind/name should return 400, got %d", resp.StatusCode)
	}
}

func TestHandleResourceImpact_Matches(t *testing.T) {
	a := newTestAnalyzer()
	a.lastReliability = ScoreResult{
		Deductions: []ScoreDeduction{{
			Rule: "CrashLoopBackOff pods", Impact: -10, Count: 2,
			AllResources: []string{"default/pod-1", "default/pod-2"},
		}},
	}

	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/resource-impact?kind=Pod&namespace=default&name=pod-1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body types.ResourceImpactResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Rules) != 1 {
		t.Fatalf("expected 1 matching rule, got %d", len(body.Rules))
	}
	if body.Rules[0].Rule != "CrashLoopBackOff pods" {
		t.Errorf("unexpected rule: %q", body.Rules[0].Rule)
	}
	if body.Rules[0].Dimension != "reliability" {
		t.Errorf("unexpected dimension: %q", body.Rules[0].Dimension)
	}
	if body.Rules[0].Remediation == "" {
		t.Error("expected non-empty remediation hint")
	}
}

func TestHandleResourceImpact_NoMatch(t *testing.T) {
	a := newTestAnalyzer()
	a.lastReliability = ScoreResult{
		Deductions: []ScoreDeduction{{
			Rule: "CrashLoopBackOff pods", Impact: -10, Count: 1,
			AllResources: []string{"default/pod-1"},
		}},
	}

	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/resource-impact?kind=Pod&namespace=default&name=unknown-pod")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body types.ResourceImpactResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Rules) != 0 {
		t.Errorf("expected no matches, got %d rules", len(body.Rules))
	}
}

// --- additional live-handler paths ------------------------------------------

// TestHandleRuleBreakdown_OpenIncidentsLive verifies that the
// "Open incidents" dynamic rule falls through to liveIncidentResources
// and returns data from the correlator's live incident map.
func TestHandleRuleBreakdown_OpenIncidentsLive(t *testing.T) {
	a := newTestAnalyzer()
	a.correlator = NewCorrelator("test", time.Minute)
	a.correlator.incidents[42] = &Incident{
		ID:         42,
		Status:     "open",
		Severity:   "high",
		DetectedAt: time.Now(),
		Summary:    "database pod flapping",
	}

	a.lastArchitecture = ScoreResult{
		Deductions: []ScoreDeduction{{Rule: "Open incidents", Impact: -5, Count: 1}},
	}

	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/breakdown/architecture/0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rb types.RuleBreakdownResponse
	if err := json.NewDecoder(resp.Body).Decode(&rb); err != nil {
		t.Fatal(err)
	}
	if len(rb.Resources) != 1 {
		t.Fatalf("expected 1 live incident, got %d", len(rb.Resources))
	}
	if rb.Resources[0].Kind != "Incident" {
		t.Errorf("kind: got %q, want Incident", rb.Resources[0].Kind)
	}
	if rb.Resources[0].Name != "42" {
		t.Errorf("name: got %q, want 42 (incident ID)", rb.Resources[0].Name)
	}
	if rb.Resources[0].Detail != "database pod flapping" {
		t.Errorf("detail: got %q", rb.Resources[0].Detail)
	}
}

// TestHandleRuleBreakdown_OpenErrorGroupsLive verifies the
// "Open error groups" dynamic rule pulls from errorAggregator.groups.
func TestHandleRuleBreakdown_OpenErrorGroupsLive(t *testing.T) {
	a := newTestAnalyzer()
	a.errorAggregator = NewErrorAggregator()
	a.errorAggregator.groups["fp1"] = &ErrorGroup{
		ID:        1,
		Title:     "NullPointerException",
		Service:   "api",
		Namespace: "prod",
		Level:     "error",
		Status:    "open",
		Count:     17,
	}

	a.lastReliability = ScoreResult{
		Deductions: []ScoreDeduction{{Rule: "Open error groups", Impact: -3, Count: 1}},
	}

	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/breakdown/reliability/0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rb types.RuleBreakdownResponse
	if err := json.NewDecoder(resp.Body).Decode(&rb); err != nil {
		t.Fatal(err)
	}
	if len(rb.Resources) != 1 {
		t.Fatalf("expected 1 live error group, got %d", len(rb.Resources))
	}
	if rb.Resources[0].Kind != "ErrorGroup" {
		t.Errorf("kind: got %q, want ErrorGroup", rb.Resources[0].Kind)
	}
	if rb.Resources[0].Name != "NullPointerException" {
		t.Errorf("name: got %q", rb.Resources[0].Name)
	}
}

// TestHandleRuleBreakdown_EmptyAllResources verifies that a deduction
// with no AllResources returns a non-null, empty resources array in
// the JSON response. Guards the defensive normalisation that the
// frontend's resources.length access depends on.
func TestHandleRuleBreakdown_EmptyAllResources(t *testing.T) {
	a := newTestAnalyzer()
	a.lastReliability = ScoreResult{
		Deductions: []ScoreDeduction{{
			Rule:         "CrashLoopBackOff pods",
			Impact:       -5,
			Count:        0, // no resources; edge case
			AllResources: nil,
		}},
	}

	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/breakdown/reliability/0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Decode into a raw map so we can inspect the literal JSON value
	// of "resources" — DeepEqual on an empty slice is indistinguishable
	// from a decoded null, so we must check via json.RawMessage.
	var raw struct {
		Resources json.RawMessage `json:"resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if string(raw.Resources) != "[]" {
		t.Errorf("expected resources JSON to be [], got %s", string(raw.Resources))
	}
}

// TestHandleResourceImpact_LiveIncidentMatch verifies that
// ruleMatchesResource's live-handler path for "Open incidents" is
// executed when a resource is listed in an incident's Affected array.
func TestHandleResourceImpact_LiveIncidentMatch(t *testing.T) {
	a := newTestAnalyzer()
	a.correlator = NewCorrelator("test", time.Minute)
	a.correlator.incidents[7] = &Incident{
		ID:         7,
		Status:     "open",
		Severity:   "high",
		DetectedAt: time.Now(),
		Affected:   []string{"default/my-pod"},
		Summary:    "checking the live match",
	}
	a.lastArchitecture = ScoreResult{
		Deductions: []ScoreDeduction{{Rule: "Open incidents", Impact: -5, Count: 1}},
	}

	srv := httptest.NewServer(newBreakdownMux(a))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health/resource-impact?kind=Pod&namespace=default&name=my-pod")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body types.ResourceImpactResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Rules) != 1 {
		t.Fatalf("expected 1 rule match via live incident path, got %d", len(body.Rules))
	}
	if body.Rules[0].Rule != "Open incidents" {
		t.Errorf("rule: got %q, want Open incidents", body.Rules[0].Rule)
	}
	if body.Rules[0].Dimension != "architecture" {
		t.Errorf("dimension: got %q, want architecture", body.Rules[0].Dimension)
	}
	if body.Rules[0].Remediation == "" {
		t.Error("expected non-empty remediation for Open incidents")
	}
}
