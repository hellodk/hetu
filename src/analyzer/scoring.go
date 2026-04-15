package main

import (
	"fmt"
	"math"
)

// ScoreDeduction represents a single factor that reduced a dimension score.
// These form the drill-down data: each deduction names the rule, the impact,
// and the specific resources that triggered it.
type ScoreDeduction struct {
	Rule         string   `json:"rule"`
	Impact       int      `json:"impact"`    // negative number (deduction)
	Max          int      `json:"max"`       // max deduction for this rule
	Count        int      `json:"count"`     // how many resources triggered it
	Resources    []string `json:"resources"` // affected resource identifiers (truncated)
	AllResources []string `json:"-"`         // full list for drill-down; not serialized
}

// ScoreResult holds a computed dimension score and its breakdown.
type ScoreResult struct {
	Score      int              `json:"score"`
	Base       int              `json:"base"`
	Deductions []ScoreDeduction `json:"deductions"`
	Bonuses    []ScoreDeduction `json:"bonuses"`
}

// ClusterScoreInput aggregates all the data needed by the rule engine.
// Populated from the v7 scanners that already run (security, pod health,
// optimizer, anomaly, correlator, error aggregator).
type ClusterScoreInput struct {
	// From PodHealthScanner
	CrashLoopPods int
	OOMKilledPods int
	PendingPods   int
	EvictedPods   int
	FailedPods    int

	// From SecurityScanner (by severity)
	CriticalFindings int
	HighFindings     int
	MediumFindings   int
	LowFindings      int

	// From SecurityScanner (by category)
	PrivilegedContainers int
	RootContainers       int
	HostNetworkPods      int
	ClusterAdminBindings int
	SecretsInEnvVars     int
	NamespacesWithoutNP  int
	WritableRootFS       int
	MissingSecContext    int
	WildcardRBAC         int
	DefaultSAUsage       int

	// From ResourceMetrics (latest deduplicated)
	CPUUsed      float64
	CPURequested float64
	CPUCapacity  float64
	MemUsed      float64
	MemRequested float64
	MemCapacity  float64

	// From OptimizerRegistry
	RightsizingRecs int
	IdleDeployments int

	// From AnomalyDetector
	ActiveAnomalies int

	// From Correlator
	OpenIncidents int

	// From ErrorAggregator
	OpenErrorGroups int

	// Namespace-level
	NamespacesWithoutQuota int
	NamespacesWithoutLR    int

	// Resource names for drill-down
	CrashLoopPodNames      []string
	OOMKilledPodNames      []string
	PendingPodNames        []string
	EvictedPodNames        []string
	PrivilegedPodNames     []string
	RootPodNames           []string
	HostNetworkPodNames    []string
	ClusterAdminNames      []string
	SecretsInEnvPodNames   []string
	NsWithoutNPNames       []string
	NsWithoutQuotaNames    []string
	NsWithoutLRNames       []string
	RightsizingTargetNames []string
}

// CalculateScores runs the documented deduction-based scoring engine.
// Returns scores for all 4 dimensions plus detailed breakdowns.
func CalculateScores(input ClusterScoreInput) (
	reliability, security, cost, architecture ScoreResult,
) {
	reliability = calculateReliability(input)
	security = calculateSecurity(input)
	cost = calculateCost(input)
	architecture = calculateArchitecture(input)
	return
}

// BlendWithLLM blends rule-based scores with LLM scores when available.
// Rule-based scores are weighted 60%, LLM 40%. If LLM scores are nil,
// uses 100% rule-based.
func BlendWithLLM(rule ScoreResult, llmScore *int) int {
	if llmScore == nil || *llmScore < 0 {
		return rule.Score
	}
	blended := float64(rule.Score)*0.6 + float64(*llmScore)*0.4
	return clampScore(int(math.Round(blended)))
}

func clampScore(s int) int {
	if s < 0 {
		return 0
	}
	if s > 100 {
		return 100
	}
	return s
}

func deduct(count, perItem, maxDeduction int) int {
	d := count * perItem
	if d > maxDeduction {
		d = maxDeduction
	}
	return d
}

// Truncate at this many names when attaching Resources to a deduction —
// keeps the /breakdown payload bounded. AllResources retains the full list.
const truncResourceNames = 10

func truncNames(names []string) []string {
	if len(names) <= truncResourceNames {
		return names
	}
	return names[:truncResourceNames]
}

// =========================================================================
// Reliability Score (documented in SCORING_SYSTEM.md)
// =========================================================================

func calculateReliability(input ClusterScoreInput) ScoreResult {
	base := 100
	var deductions []ScoreDeduction

	if input.CrashLoopPods > 0 {
		d := deduct(input.CrashLoopPods, 10, 30)
		deductions = append(deductions, ScoreDeduction{
			Rule: "CrashLoopBackOff pods", Impact: -d, Max: -30,
			Count: input.CrashLoopPods, Resources: truncNames(input.CrashLoopPodNames),
			AllResources: input.CrashLoopPodNames,
		})
	}
	if input.OOMKilledPods > 0 {
		d := deduct(input.OOMKilledPods, 5, 20)
		deductions = append(deductions, ScoreDeduction{
			Rule: "OOMKilled pods", Impact: -d, Max: -20,
			Count: input.OOMKilledPods, Resources: truncNames(input.OOMKilledPodNames),
			AllResources: input.OOMKilledPodNames,
		})
	}
	if input.PendingPods > 0 {
		d := deduct(input.PendingPods, 3, 15)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Pending pods", Impact: -d, Max: -15,
			Count: input.PendingPods, Resources: truncNames(input.PendingPodNames),
			AllResources: input.PendingPodNames,
		})
	}
	if input.EvictedPods > 0 {
		d := deduct(input.EvictedPods, 3, 15)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Evicted pods", Impact: -d, Max: -15,
			Count: input.EvictedPods, Resources: truncNames(input.EvictedPodNames),
			AllResources: input.EvictedPodNames,
		})
	}
	if input.OpenErrorGroups > 0 {
		d := deduct(input.OpenErrorGroups, 1, 10)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Open error groups", Impact: -d, Max: -10,
			Count: input.OpenErrorGroups,
		})
	}

	totalDeduction := 0
	for _, d := range deductions {
		totalDeduction += -d.Impact
	}

	return ScoreResult{
		Score:      clampScore(base - totalDeduction),
		Base:       base,
		Deductions: deductions,
	}
}

// =========================================================================
// Security Score
// =========================================================================

func calculateSecurity(input ClusterScoreInput) ScoreResult {
	base := 100
	var deductions []ScoreDeduction

	if input.PrivilegedContainers > 0 {
		d := deduct(input.PrivilegedContainers, 15, 30)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Privileged containers", Impact: -d, Max: -30,
			Count: input.PrivilegedContainers, Resources: truncNames(input.PrivilegedPodNames),
			AllResources: input.PrivilegedPodNames,
		})
	}
	if input.RootContainers > 0 {
		d := deduct(input.RootContainers, 10, 30)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Root containers", Impact: -d, Max: -30,
			Count: input.RootContainers, Resources: truncNames(input.RootPodNames),
			AllResources: input.RootPodNames,
		})
	}
	if input.HostNetworkPods > 0 {
		d := deduct(input.HostNetworkPods, 10, 20)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Host network/PID/IPC pods", Impact: -d, Max: -20,
			Count: input.HostNetworkPods, Resources: truncNames(input.HostNetworkPodNames),
			AllResources: input.HostNetworkPodNames,
		})
	}
	if input.ClusterAdminBindings > 0 {
		d := deduct(input.ClusterAdminBindings, 10, 20)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Cluster-admin bindings", Impact: -d, Max: -20,
			Count: input.ClusterAdminBindings, Resources: truncNames(input.ClusterAdminNames),
			AllResources: input.ClusterAdminNames,
		})
	}
	if input.SecretsInEnvVars > 0 {
		d := deduct(input.SecretsInEnvVars, 5, 15)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Secrets in env vars", Impact: -d, Max: -15,
			Count: input.SecretsInEnvVars, Resources: truncNames(input.SecretsInEnvPodNames),
			AllResources: input.SecretsInEnvPodNames,
		})
	}
	if input.NamespacesWithoutNP > 0 {
		d := deduct(input.NamespacesWithoutNP, 5, 20)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Namespaces without network policy", Impact: -d, Max: -20,
			Count: input.NamespacesWithoutNP, Resources: truncNames(input.NsWithoutNPNames),
			AllResources: input.NsWithoutNPNames,
		})
	}
	if input.WritableRootFS > 0 {
		d := deduct(input.WritableRootFS, 2, 10)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Writable root filesystem", Impact: -d, Max: -10,
			Count: input.WritableRootFS,
		})
	}
	if input.MissingSecContext > 0 {
		d := deduct(input.MissingSecContext, 2, 10)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Missing securityContext", Impact: -d, Max: -10,
			Count: input.MissingSecContext,
		})
	}
	if input.WildcardRBAC > 0 {
		d := deduct(input.WildcardRBAC, 3, 10)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Wildcard RBAC rules", Impact: -d, Max: -10,
			Count: input.WildcardRBAC,
		})
	}

	totalDeduction := 0
	for _, d := range deductions {
		totalDeduction += -d.Impact
	}

	return ScoreResult{
		Score:      clampScore(base - totalDeduction),
		Base:       base,
		Deductions: deductions,
	}
}

// =========================================================================
// Cost Efficiency Score
// =========================================================================

func calculateCost(input ClusterScoreInput) ScoreResult {
	// CPU efficiency (40% weight)
	cpuEfficiency := 100.0
	if input.CPURequested > 0 {
		ratio := input.CPUUsed / input.CPURequested
		if ratio >= 0.6 && ratio <= 0.8 {
			cpuEfficiency = 100
		} else if ratio < 0.3 {
			cpuEfficiency = ratio / 0.3 * 100
		} else if ratio > 0.9 {
			cpuEfficiency = 100 - (ratio-0.9)*200
		} else if ratio < 0.6 {
			cpuEfficiency = 50 + (ratio-0.3)/0.3*50
		} else {
			cpuEfficiency = 100 - (ratio-0.8)/0.1*10
		}
	}

	// Memory efficiency (40% weight)
	memEfficiency := 100.0
	if input.MemRequested > 0 {
		ratio := input.MemUsed / input.MemRequested
		if ratio >= 0.6 && ratio <= 0.8 {
			memEfficiency = 100
		} else if ratio < 0.3 {
			memEfficiency = ratio / 0.3 * 100
		} else if ratio > 0.9 {
			memEfficiency = 100 - (ratio-0.9)*200
		} else if ratio < 0.6 {
			memEfficiency = 50 + (ratio-0.3)/0.3*50
		} else {
			memEfficiency = 100 - (ratio-0.8)/0.1*10
		}
	}

	baseScore := int(cpuEfficiency*0.4 + memEfficiency*0.4 + 100*0.2)

	var deductions []ScoreDeduction
	if input.RightsizingRecs > 0 {
		d := deduct(input.RightsizingRecs, 2, 20)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Rightsizing opportunities", Impact: -d, Max: -20,
			Count:        input.RightsizingRecs,
			Resources:    truncNames(input.RightsizingTargetNames),
			AllResources: input.RightsizingTargetNames,
		})
	}

	totalDeduction := 0
	for _, d := range deductions {
		totalDeduction += -d.Impact
	}

	cpuRatioStr := "N/A"
	memRatioStr := "N/A"
	if input.CPURequested > 0 {
		cpuRatioStr = fmt.Sprintf("%.0f%%", input.CPUUsed/input.CPURequested*100)
	}
	if input.MemRequested > 0 {
		memRatioStr = fmt.Sprintf("%.0f%%", input.MemUsed/input.MemRequested*100)
	}

	// Add efficiency info as "bonuses" for drill-down visibility
	var bonuses []ScoreDeduction
	bonuses = append(bonuses, ScoreDeduction{
		Rule: fmt.Sprintf("CPU efficiency: %s used/requested", cpuRatioStr), Impact: int(cpuEfficiency * 0.4),
	})
	bonuses = append(bonuses, ScoreDeduction{
		Rule: fmt.Sprintf("Memory efficiency: %s used/requested", memRatioStr), Impact: int(memEfficiency * 0.4),
	})

	return ScoreResult{
		Score:      clampScore(baseScore - totalDeduction),
		Base:       baseScore,
		Deductions: deductions,
		Bonuses:    bonuses,
	}
}

// =========================================================================
// Architecture Score
// =========================================================================

func calculateArchitecture(input ClusterScoreInput) ScoreResult {
	base := 100
	var deductions []ScoreDeduction

	if input.ActiveAnomalies > 0 {
		d := deduct(input.ActiveAnomalies, 5, 20)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Active anomalies", Impact: -d, Max: -20,
			Count: input.ActiveAnomalies,
		})
	}
	if input.OpenIncidents > 0 {
		// Cap more aggressively — hundreds of incidents shouldn't tank the score to 0
		d := deduct(min(input.OpenIncidents, 20), 2, 20)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Open incidents", Impact: -d, Max: -20,
			Count: input.OpenIncidents,
		})
	}
	if input.NamespacesWithoutQuota > 0 {
		d := deduct(input.NamespacesWithoutQuota, 5, 15)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Namespaces without ResourceQuota", Impact: -d, Max: -15,
			Count: input.NamespacesWithoutQuota, Resources: truncNames(input.NsWithoutQuotaNames),
			AllResources: input.NsWithoutQuotaNames,
		})
	}
	if input.NamespacesWithoutLR > 0 {
		d := deduct(input.NamespacesWithoutLR, 3, 10)
		deductions = append(deductions, ScoreDeduction{
			Rule: "Namespaces without LimitRange", Impact: -d, Max: -10,
			Count: input.NamespacesWithoutLR, Resources: truncNames(input.NsWithoutLRNames),
			AllResources: input.NsWithoutLRNames,
		})
	}

	totalDeduction := 0
	for _, d := range deductions {
		totalDeduction += -d.Impact
	}

	return ScoreResult{
		Score:      clampScore(base - totalDeduction),
		Base:       base,
		Deductions: deductions,
	}
}

// BuildScoreInput populates ClusterScoreInput from the analyzer's live scanner data.
func (a *Analyzer) BuildScoreInput() ClusterScoreInput {
	input := ClusterScoreInput{}

	// Pod health
	if a.podHealthScanner != nil {
		a.podHealthScanner.mu.RLock()
		if rpt := a.podHealthScanner.report; rpt != nil {
			for _, cat := range rpt.Categories {
				for _, p := range cat.Pods {
					name := p.Namespace + "/" + p.Name
					switch cat.Name {
					case "crashloop":
						input.CrashLoopPods++
						input.CrashLoopPodNames = append(input.CrashLoopPodNames, name)
					case "oomkilled":
						input.OOMKilledPods++
						input.OOMKilledPodNames = append(input.OOMKilledPodNames, name)
					case "pending":
						input.PendingPods++
						input.PendingPodNames = append(input.PendingPodNames, name)
					case "evicted":
						input.EvictedPods++
						input.EvictedPodNames = append(input.EvictedPodNames, name)
					case "failed":
						input.FailedPods++
					}
				}
			}
		}
		a.podHealthScanner.mu.RUnlock()
	}

	// Security findings
	if a.securityScanner != nil {
		a.securityScanner.mu.RLock()
		for _, f := range a.securityScanner.findings {
			switch f.Severity {
			case "critical":
				input.CriticalFindings++
			case "high":
				input.HighFindings++
			case "medium":
				input.MediumFindings++
			case "low":
				input.LowFindings++
			}
			switch f.Category {
			case "pod-security":
				if f.Title == "Privileged container" {
					input.PrivilegedContainers++
					input.PrivilegedPodNames = append(input.PrivilegedPodNames, f.Affected...)
				}
				if f.Title == "Container may run as root" {
					input.RootContainers++
					input.RootPodNames = append(input.RootPodNames, f.Affected...)
				}
				if f.Title == "Pod uses host networking" || f.Title == "Pod uses hostPID" || f.Title == "Pod uses hostIPC" {
					input.HostNetworkPods++
					input.HostNetworkPodNames = append(input.HostNetworkPodNames, f.Affected...)
				}
				if f.Title == "Root filesystem is writable" {
					input.WritableRootFS++
				}
				if f.Title == "Missing security context" {
					input.MissingSecContext++
				}
			case "rbac":
				if f.Title == "ServiceAccount bound to cluster-admin" {
					input.ClusterAdminBindings++
					input.ClusterAdminNames = append(input.ClusterAdminNames, f.Affected...)
				}
				if f.Title == "ClusterRole uses wildcard verbs" || f.Title == "ClusterRole uses wildcard resources" {
					input.WildcardRBAC++
				}
			case "secrets":
				if f.Title == "Secret exposed as env var" || f.Title == "Possible hardcoded credential" {
					input.SecretsInEnvVars++
					input.SecretsInEnvPodNames = append(input.SecretsInEnvPodNames, f.Affected...)
				}
			case "network":
				if f.Title == "Namespace has no NetworkPolicy" {
					input.NamespacesWithoutNP++
					input.NsWithoutNPNames = append(input.NsWithoutNPNames, f.Affected...)
				}
			case "general":
				if f.Title == "No ResourceQuota in namespace" {
					input.NamespacesWithoutQuota++
					input.NsWithoutQuotaNames = append(input.NsWithoutQuotaNames, f.Affected...)
				}
				if f.Title == "No LimitRange in namespace" {
					input.NamespacesWithoutLR++
					input.NsWithoutLRNames = append(input.NsWithoutLRNames, f.Affected...)
				}
			}
		}
		a.securityScanner.mu.RUnlock()
	}

	// Resource utilization (from latest report)
	a.reportMu.RLock()
	if rpt := a.latestReport; rpt != nil {
		input.CPUUsed = rpt.ResourceUtilization.CPU.Used
		input.CPURequested = rpt.ResourceUtilization.CPU.Requested
		input.CPUCapacity = rpt.ResourceUtilization.CPU.Capacity
		input.MemUsed = rpt.ResourceUtilization.Memory.Used
		input.MemRequested = rpt.ResourceUtilization.Memory.Requested
		input.MemCapacity = rpt.ResourceUtilization.Memory.Capacity
	}
	a.reportMu.RUnlock()

	// Optimizer
	if a.optimizerRegistry != nil {
		a.optimizerRegistry.mu.RLock()
		for _, r := range a.optimizerRegistry.recommendations {
			if r.Status == "open" && r.Type == "rightsizing" {
				input.RightsizingRecs++
				target := r.Target.Name
				if r.Target.Namespace != "" {
					target = r.Target.Namespace + "/" + r.Target.Name
				}
				input.RightsizingTargetNames = append(input.RightsizingTargetNames, target)
			}
		}
		a.optimizerRegistry.mu.RUnlock()
	}

	// Anomalies
	if a.anomalyDetector != nil {
		a.anomalyDetector.mu.RLock()
		for _, an := range a.anomalyDetector.anomalies {
			if an.Status == "active" {
				input.ActiveAnomalies++
			}
		}
		a.anomalyDetector.mu.RUnlock()
	}

	// Incidents
	if a.correlator != nil {
		a.correlator.mu.RLock()
		for _, inc := range a.correlator.incidents {
			if inc.Status == "open" || inc.Status == "investigating" {
				input.OpenIncidents++
			}
		}
		a.correlator.mu.RUnlock()
	}

	// Error groups
	if a.errorAggregator != nil {
		a.errorAggregator.mu.RLock()
		for _, g := range a.errorAggregator.groups {
			if g.Status == "open" {
				input.OpenErrorGroups++
			}
		}
		a.errorAggregator.mu.RUnlock()
	}

	return input
}

// remediationHints maps each static scoring rule name to a short,
// actionable remediation text shown on the Level-4 "Score Impact"
// panel. Keys must match the Rule field set in the calculate*
// functions exactly. Dynamic rules (e.g. "CPU efficiency: 45%
// used/requested") are intentionally absent — remediationFor returns
// "" for them and the UI hides the remediation block.
var remediationHints = map[string]string{
	"CrashLoopBackOff pods":             "Check container logs for startup errors. Verify resource limits, liveness probes, and image tags. Consider increasing memory limits if OOM is suspected.",
	"OOMKilled pods":                    "Increase memory limits for affected containers. Profile memory usage with pprof or top. Check for memory leaks.",
	"Pending pods":                      "Check node capacity, resource requests, node selectors, tolerations, and PVC bindings. Run 'kubectl describe pod' for scheduling failure details.",
	"Evicted pods":                      "Nodes may be under memory or disk pressure. Set appropriate resource requests and use PriorityClasses to protect critical workloads.",
	"Open error groups":                 "Review error groups on the Errors page. Fix or suppress known issues. Set up alerting for new error patterns.",
	"Privileged containers":             "Remove 'privileged: true' from securityContext. Use specific capabilities (NET_ADMIN, SYS_PTRACE) instead of full privilege.",
	"Root containers":                   "Set 'runAsNonRoot: true' and specify a numeric 'runAsUser' in the pod securityContext. Rebuild images to run as non-root.",
	"Host network/PID/IPC pods":         "Remove 'hostNetwork', 'hostPID', 'hostIPC' from the pod spec. Use Services and NetworkPolicies for network access instead.",
	"Cluster-admin bindings":            "Replace cluster-admin ClusterRoleBindings with scoped Roles. Audit who needs cluster-wide access and create least-privilege roles.",
	"Secrets in env vars":               "Mount secrets as files via volume mounts instead of env vars. Use external secret stores (Vault, AWS Secrets Manager) where possible.",
	"Namespaces without network policy": "Create a default-deny NetworkPolicy in each namespace, then allow only required traffic flows.",
	"Writable root filesystem":          "Set 'readOnlyRootFilesystem: true' in securityContext. Use emptyDir volumes for temp files.",
	"Missing securityContext":           "Add a securityContext to every container with runAsNonRoot, readOnlyRootFilesystem, and drop ALL capabilities.",
	"Wildcard RBAC rules":               "Replace wildcard verbs/resources in ClusterRoles with specific verbs and named resources.",
	"Rightsizing opportunities":         "Adjust CPU/memory requests to match observed usage (see Optimization page). Right-size to 80th-percentile with 20% headroom.",
	"Active anomalies":                  "Investigate metric anomalies on the Anomalies page. Check if recent deployments or config changes correlate with the spike.",
	"Open incidents":                    "Triage open incidents on the Incidents page. Resolve or dismiss incidents that are no longer relevant.",
	"Namespaces without ResourceQuota":  "Create ResourceQuota objects to prevent any single namespace from consuming unbounded cluster resources.",
	"Namespaces without LimitRange":     "Create LimitRange objects to set default CPU/memory requests and limits for pods that don't specify them.",
}

// remediationFor returns the remediation hint for a scoring rule, or "" when
// the rule is dynamic (e.g. cost efficiency bonuses) or unrecognised.
func remediationFor(rule string) string {
	return remediationHints[rule]
}
