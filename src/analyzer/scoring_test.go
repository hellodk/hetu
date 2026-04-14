package main

import (
	"testing"
)

// --- severityForRule --------------------------------------------------------

func TestSeverityForRule(t *testing.T) {
	cases := []struct {
		rule   string
		impact int
		want   string
	}{
		{"CrashLoopBackOff pods", -10, "high"},
		{"OOMKilled pods", -5, "high"},
		{"Privileged containers", -15, "high"},
		{"Cluster-admin bindings", -10, "high"},
		{"Open incidents", -4, "high"},
		{"Pending pods", -3, "medium"},
		{"Evicted pods", -3, "medium"},
		{"Active anomalies", -5, "medium"},
		{"Rightsizing opportunities", -4, "medium"},
		{"Unknown rule", -25, "high"},   // falls through to impact check
		{"Unknown rule", -5, "medium"},  // small impact fallback
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

func TestRemediationFor_Known(t *testing.T) {
	rule := "CrashLoopBackOff pods"
	got := remediationFor(rule)
	if got == "" {
		t.Fatalf("expected non-empty remediation for %q, got empty", rule)
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
	input := ClusterScoreInput{
		CrashLoopPods:     3,
		CrashLoopPodNames: []string{"ns-a/p1", "ns-a/p2", "ns-b/p3"},
	}
	rel, _, _, _ := CalculateScores(input)
	if len(rel.Deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(rel.Deductions))
	}
	d := rel.Deductions[0]
	if d.Rule != "CrashLoopBackOff pods" {
		t.Errorf("unexpected rule: %q", d.Rule)
	}
	if len(d.AllResources) != 3 {
		t.Errorf("AllResources: got %d, want 3", len(d.AllResources))
	}
	if len(d.Resources) != 3 {
		t.Errorf("Resources: got %d, want 3 (under truncation limit of 10)", len(d.Resources))
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
