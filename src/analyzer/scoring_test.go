package main

import (
	"reflect"
	"testing"
)

// --- severityForRule --------------------------------------------------------

// NOTE: every static case in severityForRule (main.go) must appear here.
// When adding a new rule to the switch, add a corresponding test case below
// so "high/medium" classification is locked in.
func TestSeverityForRule(t *testing.T) {
	cases := []struct {
		rule   string
		impact int
		want   string
	}{
		// High-severity bucket (6 rules from severityForRule switch)
		{"CrashLoopBackOff pods", -10, "high"},
		{"OOMKilled pods", -5, "high"},
		{"Privileged containers", -15, "high"},
		{"Root containers", -10, "high"},
		{"Cluster-admin bindings", -10, "high"},
		{"Open incidents", -4, "high"},
		// Medium-severity bucket (13 rules)
		{"Pending pods", -3, "medium"},
		{"Evicted pods", -3, "medium"},
		{"Host network/PID/IPC pods", -5, "medium"},
		{"Secrets in env vars", -5, "medium"},
		{"Namespaces without network policy", -5, "medium"},
		{"Active anomalies", -5, "medium"},
		{"Rightsizing opportunities", -4, "medium"},
		{"Namespaces without ResourceQuota", -5, "medium"},
		{"Namespaces without LimitRange", -3, "medium"},
		{"Wildcard RBAC rules", -3, "medium"},
		{"Missing securityContext", -2, "medium"},
		{"Writable root filesystem", -2, "medium"},
		{"Open error groups", -1, "medium"},
		// Fallback branches for unknown rules
		{"Unknown rule", -25, "high"},
		{"Unknown rule", -5, "medium"},
		{"Unknown rule", 0, "medium"},
	}
	for _, tc := range cases {
		t.Run(tc.rule, func(t *testing.T) {
			got := severityForRule(tc.rule, tc.impact)
			if got != tc.want {
				t.Errorf("severityForRule(%q, %d) = %q, want %q", tc.rule, tc.impact, got, tc.want)
			}
		})
	}
}

// --- inferKindForRule -------------------------------------------------------

func TestInferKindForRule(t *testing.T) {
	cases := []struct {
		rule string
		want string
	}{
		{"CrashLoopBackOff pods", "Pod"},
		{"OOMKilled pods", "Pod"},
		{"Pending pods", "Pod"},
		{"Evicted pods", "Pod"},
		{"Privileged containers", "Pod"},
		{"Root containers", "Pod"},
		{"Host network/PID/IPC pods", "Pod"},
		{"Secrets in env vars", "Pod"},
		{"Rightsizing opportunities", "Pod"},
		{"Cluster-admin bindings", "ClusterRoleBinding"},
		{"Namespaces without network policy", "Namespace"},
		{"Namespaces without ResourceQuota", "Namespace"},
		{"Namespaces without LimitRange", "Namespace"},
		{"Unknown rule name", "Resource"},
	}
	for _, tc := range cases {
		t.Run(tc.rule, func(t *testing.T) {
			got := inferKindForRule(tc.rule)
			if got != tc.want {
				t.Errorf("inferKindForRule(%q) = %q, want %q", tc.rule, got, tc.want)
			}
		})
	}
}

// --- parseResourceIdentifier ------------------------------------------------

func TestParseResourceIdentifier(t *testing.T) {
	t.Run("namespaced name", func(t *testing.T) {
		got := parseResourceIdentifier("default/my-pod", "CrashLoopBackOff pods")
		if got.Namespace != "default" || got.Name != "my-pod" || got.Kind != "Pod" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("bare name", func(t *testing.T) {
		got := parseResourceIdentifier("cluster-admin-binding", "Cluster-admin bindings")
		if got.Namespace != "" || got.Name != "cluster-admin-binding" || got.Kind != "ClusterRoleBinding" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("namespace-only", func(t *testing.T) {
		got := parseResourceIdentifier("kube-system", "Namespaces without network policy")
		if got.Name != "kube-system" || got.Kind != "Namespace" {
			t.Errorf("got %+v", got)
		}
	})
}

// --- remediationFor ---------------------------------------------------------

// TestRemediationFor_Known verifies every key in the remediationHints map
// has a non-empty value. Guards against accidentally shipping a rule with
// no remediation text (which would render an empty gray card in the UI).
func TestRemediationFor_Known(t *testing.T) {
	if len(remediationHints) < 19 {
		t.Fatalf("expected at least 19 remediation entries, got %d", len(remediationHints))
	}
	for rule, hint := range remediationHints {
		if hint == "" {
			t.Errorf("remediation hint for rule %q is empty", rule)
		}
		if got := remediationFor(rule); got != hint {
			t.Errorf("remediationFor(%q) = %q, want %q", rule, got, hint)
		}
	}
}

func TestRemediationFor_Unknown(t *testing.T) {
	// Dynamic cost bonus names must return empty so the UI hides the block.
	if got := remediationFor("CPU efficiency: 45% used/requested"); got != "" {
		t.Errorf("dynamic rule should return empty remediation, got %q", got)
	}
	if got := remediationFor(""); got != "" {
		t.Errorf("empty rule should return empty remediation, got %q", got)
	}
}

// --- BlendWithLLM -----------------------------------------------------------

func TestBlendWithLLM_NilLLM(t *testing.T) {
	rule := ScoreResult{Score: 80}
	got := BlendWithLLM(rule, nil)
	if got != 80 {
		t.Errorf("nil LLM should return rule score unchanged, got %d", got)
	}
}

func TestBlendWithLLM_Weighted(t *testing.T) {
	rule := ScoreResult{Score: 80}
	llm := 60
	got := BlendWithLLM(rule, &llm)
	// 80*0.6 + 60*0.4 = 48 + 24 = 72
	if got != 72 {
		t.Errorf("expected 72, got %d", got)
	}
}

func TestBlendWithLLM_NegativeLLMIgnored(t *testing.T) {
	rule := ScoreResult{Score: 80}
	llm := -1
	got := BlendWithLLM(rule, &llm)
	if got != 80 {
		t.Errorf("negative LLM score should be treated as absent, got %d", got)
	}
}

// --- CalculateScores --------------------------------------------------------

func TestCalculateScores_AllZeroInput(t *testing.T) {
	rel, sec, cost, arch := CalculateScores(ClusterScoreInput{})
	if rel.Score != 100 {
		t.Errorf("empty input reliability: got %d, want 100", rel.Score)
	}
	if sec.Score != 100 {
		t.Errorf("empty input security: got %d, want 100", sec.Score)
	}
	// Cost uses efficiency ratios — with nothing requested it stays at base.
	if cost.Score == 0 {
		t.Errorf("cost score should not be 0 on empty input: got %d", cost.Score)
	}
	if arch.Score != 100 {
		t.Errorf("empty input architecture: got %d, want 100", arch.Score)
	}
	if len(rel.Deductions) != 0 {
		t.Errorf("empty input should produce no reliability deductions")
	}
}

func TestCalculateScores_CrashLoopPopulatesAllResources(t *testing.T) {
	want := []string{"ns-a/p1", "ns-a/p2", "ns-b/p3"}
	input := ClusterScoreInput{
		CrashLoopPods:     3,
		CrashLoopPodNames: want,
	}
	rel, _, _, _ := CalculateScores(input)
	if len(rel.Deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(rel.Deductions))
	}
	d := rel.Deductions[0]
	if d.Rule != "CrashLoopBackOff pods" {
		t.Errorf("unexpected rule: %q", d.Rule)
	}
	// Content equality — not just length — so a refactor that populated
	// AllResources from any other 3-element source would fail this test.
	if !reflect.DeepEqual(d.AllResources, want) {
		t.Errorf("AllResources: got %v, want %v", d.AllResources, want)
	}
	if !reflect.DeepEqual(d.Resources, want) {
		t.Errorf("Resources (under truncation limit): got %v, want %v", d.Resources, want)
	}
}

func TestCalculateScores_TruncationAt10(t *testing.T) {
	names := make([]string, 15)
	for i := range names {
		names[i] = "ns/pod-" + string(rune('a'+i))
	}
	input := ClusterScoreInput{
		CrashLoopPods:     15,
		CrashLoopPodNames: names,
	}
	rel, _, _, _ := CalculateScores(input)
	d := rel.Deductions[0]
	if len(d.Resources) != 10 {
		t.Errorf("Resources should be truncated to 10, got %d", len(d.Resources))
	}
	if len(d.AllResources) != 15 {
		t.Errorf("AllResources should keep all 15, got %d", len(d.AllResources))
	}
}

func TestCalculateScores_RightsizingResources(t *testing.T) {
	input := ClusterScoreInput{
		RightsizingRecs:        2,
		RightsizingTargetNames: []string{"ns/p1", "ns/p2"},
	}
	_, _, cost, _ := CalculateScores(input)
	var found *ScoreDeduction
	for i := range cost.Deductions {
		if cost.Deductions[i].Rule == "Rightsizing opportunities" {
			found = &cost.Deductions[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected Rightsizing opportunities deduction")
	}
	if len(found.AllResources) != 2 {
		t.Errorf("AllResources: got %d, want 2", len(found.AllResources))
	}
	if found.AllResources[0] != "ns/p1" {
		t.Errorf("AllResources[0]: got %q", found.AllResources[0])
	}
}
