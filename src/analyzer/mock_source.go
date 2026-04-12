// Package main — mock source for demo profile.
//
// The mock source is an internal "service" that synthesizes plausible
// ClusterHealthReport objects on a ticker. It only runs when the analyzer
// profile is set to "mock"; in the default "live" profile it is a no-op.
//
// The intent is to give demos a realistic-looking dashboard without any
// real cluster data, while keeping the normal code path completely free
// of hardcoded or mock values.
package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	types "github.com/your-org/cluster-intel/pkg/types"
)

// mockSource generates synthetic ClusterHealthReport objects and
// broadcasts them through the Analyzer's normal publish/broadcast path.
type mockSource struct {
	analyzer *Analyzer
	interval time.Duration

	mu     sync.Mutex
	stopCh chan struct{}
	rng    *rand.Rand
}

// newMockSource creates a new mock source bound to the given analyzer.
// The source does not start producing reports until Start() is called.
func newMockSource(a *Analyzer, interval time.Duration) *mockSource {
	if interval <= 0 {
		interval = 20 * time.Second
	}
	return &mockSource{
		analyzer: a,
		interval: interval,
		rng:      rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0xC2B2AE3D)),
	}
}

// Start begins the mock source goroutine. It's safe to call repeatedly —
// subsequent calls while already running are no-ops.
func (m *mockSource) Start(ctx context.Context) {
	m.mu.Lock()
	if m.stopCh != nil {
		m.mu.Unlock()
		return
	}
	m.stopCh = make(chan struct{})
	stopCh := m.stopCh
	m.mu.Unlock()

	m.analyzer.wg.Add(1)
	go func() {
		defer m.analyzer.wg.Done()
		log.Info().Dur("interval", m.interval).Msg("Mock source started")
		defer log.Info().Msg("Mock source stopped")

		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-m.analyzer.stopCh:
				return
			case <-ticker.C:
				// Only emit while the profile is still mock. We don't stop
				// the ticker on profile switch; setProfile handles that.
				if m.analyzer.getProfile() != types.ProfileMock {
					continue
				}
				m.generateAndBroadcast()
			}
		}
	}()
}

// Stop signals the mock source goroutine to exit. Safe to call repeatedly.
func (m *mockSource) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopCh == nil {
		return
	}
	close(m.stopCh)
	m.stopCh = nil
}

// generateAndBroadcast synthesizes a single report and publishes it.
// Exposed so that setProfile can trigger an immediate emission when the
// operator switches into mock mode — no need to wait for the first tick.
func (m *mockSource) generateAndBroadcast() {
	report := m.generate()
	m.analyzer.publishReport(report)
	if report.Scores != nil {
		m.analyzer.healthScore.Set(float64(report.Scores.Overall))
	}
}

// generate produces a single synthetic ClusterHealthReport.
//
// Numbers are chosen to be plausible rather than random noise: scores hover
// in the 70–92 range with small walks between ticks, resource utilization
// is proportional to a fixed mock cluster size, and the issues/
// recommendations lists are seeded from a fixed catalog so demos look
// coherent across time.
func (m *mockSource) generate() *types.ClusterHealthReport {
	now := time.Now()

	reliability := 78 + m.rng.IntN(15) // 78..92
	security := 72 + m.rng.IntN(18)    // 72..89
	cost := 70 + m.rng.IntN(20)        // 70..89
	architecture := 80 + m.rng.IntN(13) // 80..92

	scores := &types.HealthScores{
		Reliability:  reliability,
		Security:     security,
		Cost:         cost,
		Architecture: architecture,
	}
	scores.Overall = types.CalculateOverallScore(*scores)

	// Fixed mock cluster topology — stable across ticks.
	nsStats := map[string]*types.NamespaceStats{
		"default":     {CPUUsed: 2.1, MemoryUsed: 4.2, PodCount: 12, Warnings: 1},
		"kube-system": {CPUUsed: 6.2, MemoryUsed: 8.1, PodCount: 18, Warnings: 0},
		"monitoring":  {CPUUsed: 4.5, MemoryUsed: 6.8, PodCount: 9, Warnings: 2},
		"utilities":   {CPUUsed: 3.1, MemoryUsed: 5.6, PodCount: 7, Warnings: 0},
	}
	totalPods := 0
	for _, ns := range nsStats {
		totalPods += ns.PodCount
	}

	warningEvents := 2 + m.rng.IntN(6) // 2..7
	savings := 800 + m.rng.Float64()*1700

	topIssues := m.synthesizeIssues(now)
	recommendations := m.synthesizeRecommendations(now, savings)

	report := &types.ClusterHealthReport{
		ClusterID: m.analyzer.config.ClusterID + "-demo",
		Timestamp: now,
		Scores:    scores,
		Summary: types.ClusterSummary{
			TotalNodes:      3,
			TotalPods:       totalPods,
			TotalNamespaces: len(nsStats),
			HealthyPods:     totalPods - len(topIssues),
			UnhealthyPods:   len(topIssues),
			PendingPods:     0,
			WarningEvents:   warningEvents,
			CriticalEvents:  0,
			Namespaces:      nsStats,
		},
		ResourceUtilization: types.ResourceUtilization{
			CPU: types.ResourceUsage{
				Used:      15.9 + m.rng.Float64()*2,
				Requested: 22.0,
				Capacity:  32.0,
				Unit:      "cores",
			},
			Memory: types.ResourceUsage{
				Used:      24.7 + m.rng.Float64()*3,
				Requested: 38.0,
				Capacity:  64.0,
				Unit:      "Gi",
			},
			Storage: types.ResourceUsage{
				Used:      1.2 + m.rng.Float64()*0.3,
				Requested: 2.4,
				Capacity:  4.0,
				Unit:      "Ti",
			},
		},
		TopIssues:        topIssues,
		Recommendations:  recommendations,
		SecurityFindings: []types.SecurityFinding{},
		EstimatedSavings: savings,
		Trends: types.HealthTrends{
			Overall:      m.rng.IntN(5) - 2,
			Reliability:  m.rng.IntN(5) - 2,
			Security:     m.rng.IntN(5) - 2,
			Cost:         m.rng.IntN(5) - 2,
			Architecture: m.rng.IntN(5) - 2,
		},
		Status: &types.ReportStatus{
			State:   types.StateOK,
			Message: "Demo mode: synthetic data. No real cluster analysis is running.",
			Profile: types.ProfileMock,
			Collector: types.ComponentHealth{
				Reachable: false,
				Endpoint:  "(mock)",
			},
			LLM: types.ComponentHealth{
				Reachable: false,
				Endpoint:  "(mock)",
			},
			LastAnalysisAt: &now,
		},
	}
	return report
}

var mockIssueCatalog = []types.Issue{
	{
		Severity:    "high",
		Category:    "reliability",
		Title:       "Pod api-gateway in CrashLoopBackOff",
		Description: "api-gateway-7d8f9c6b5-x2k9m has restarted 14 times in the last hour.",
		RootCause:   "Database connection timeout — downstream Postgres is saturated.",
		Confidence:  0.92,
	},
	{
		Severity:    "medium",
		Category:    "cost",
		Title:       "Over-provisioned staging cluster",
		Description: "staging/frontend deployments request 3x more CPU than they use.",
		RootCause:   "Request values never updated since initial rollout.",
		Confidence:  0.78,
	},
	{
		Severity:    "high",
		Category:    "security",
		Title:       "Privileged pod in default namespace",
		Description: "default/debug-shell is running with privileged: true and hostPID.",
		RootCause:   "Debug pod left running after an incident on 2026-04-02.",
		Confidence:  0.95,
	},
}

func (m *mockSource) synthesizeIssues(now time.Time) []types.Issue {
	// Rotate 2-3 issues from the catalog each tick so the UI looks alive.
	n := 2 + m.rng.IntN(2)
	if n > len(mockIssueCatalog) {
		n = len(mockIssueCatalog)
	}
	out := make([]types.Issue, 0, n)
	for i := 0; i < n; i++ {
		entry := mockIssueCatalog[i]
		entry.ID = fmt.Sprintf("mock-issue-%d-%d", now.Unix(), i)
		entry.Timestamp = now
		out = append(out, entry)
	}
	return out
}

var mockRecCatalog = []types.Recommendation{
	{
		Category:    "cost",
		Title:       "Right-size api-gateway deployment",
		Description: "Reduce CPU request from 2 cores to 0.8 cores based on 7d P95.",
		Severity:    "medium",
		Confidence:  0.88,
		AIReasoning: "CPU utilization has stayed under 30% for 30 days.",
		Impact: types.RecommendationImpact{
			RiskLevel: "low",
			Effort:    "low",
		},
	},
	{
		Category:    "reliability",
		Title:       "Add PodDisruptionBudget for frontend",
		Description: "frontend has no PDB and was fully evicted during last node drain.",
		Severity:    "medium",
		Confidence:  0.81,
		AIReasoning: "Based on incident 2026-03-22.",
		Impact: types.RecommendationImpact{
			RiskLevel: "low",
			Effort:    "low",
		},
	},
	{
		Category:    "security",
		Title:       "Remove privileged flag from debug-shell",
		Description: "Delete the privileged debug pod left over from an old incident.",
		Severity:    "high",
		Confidence:  0.97,
		AIReasoning: "Zero legitimate use of this pod in audit logs for 5 days.",
		Impact: types.RecommendationImpact{
			RiskLevel: "low",
			Effort:    "low",
		},
	},
}

func (m *mockSource) synthesizeRecommendations(now time.Time, totalSavings float64) []types.Recommendation {
	n := 2 + m.rng.IntN(2)
	if n > len(mockRecCatalog) {
		n = len(mockRecCatalog)
	}
	out := make([]types.Recommendation, 0, n)
	per := totalSavings / float64(n)
	for i := 0; i < n; i++ {
		entry := mockRecCatalog[i]
		entry.ID = fmt.Sprintf("mock-rec-%d-%d", now.Unix(), i)
		entry.Timestamp = now
		entry.Impact.CostSavings = &types.CostSavings{Monthly: per, Currency: "USD"}
		out = append(out, entry)
	}
	return out
}

// ---------------------------------------------------------------------------
// Full demo mode: populate ALL v7 handlers with synthetic data
// ---------------------------------------------------------------------------

func (m *mockSource) populateAllHandlers() {
	a := m.analyzer
	now := time.Now()

	if a.securityScanner != nil {
		findings := []SecFinding{
			{Severity: "critical", Category: "rbac", Title: "cluster-admin bound to default SA", Description: "The default ServiceAccount has cluster-admin privileges.", Affected: []string{"default/default"}, CISControl: "5.1.1", Remediation: "Remove the ClusterRoleBinding."},
			{Severity: "high", Category: "rbac", Title: "Wildcard permissions in ClusterRole", Description: "ClusterRole 'admin-all' grants * verbs on * resources.", Affected: []string{"clusterrole/admin-all"}, CISControl: "5.1.3", Remediation: "Use explicit resource and verb lists."},
			{Severity: "high", Category: "pod-security", Title: "Privileged container", Description: "monitoring/debug-shell runs privileged with hostPID.", Affected: []string{"monitoring/debug-shell"}, Remediation: "Remove privileged flag."},
			{Severity: "medium", Category: "pod-security", Title: "Container running as root", Description: "default/legacy-app runs as UID 0.", Affected: []string{"default/legacy-app"}, CISControl: "5.2.6", Remediation: "Set runAsNonRoot: true."},
			{Severity: "medium", Category: "rbac", Title: "SA can list secrets cluster-wide", Description: "monitoring/prometheus has get/list on secrets.", Affected: []string{"monitoring/prometheus"}, CISControl: "5.1.2", Remediation: "Scope to specific namespaces."},
			{Severity: "low", Category: "pod-security", Title: "No resource limits on 3 pods", Description: "Pods in default lack limits.", Affected: []string{"default/api-gw", "default/worker", "default/cron"}, Remediation: "Add limits."},
			{Severity: "high", Category: "pod-security", Title: "HostPath volume mount", Description: "kube-system/node-exporter mounts /proc.", Affected: []string{"kube-system/node-exporter"}, CISControl: "5.2.8", Remediation: "Use read-only mounts."},
			{Severity: "medium", Category: "rbac", Title: "Unused elevated SA", Description: "utilities/deploy-bot unused 30 days.", Affected: []string{"utilities/deploy-bot"}, Remediation: "Delete or rotate."},
		}
		a.securityScanner.mu.Lock()
		a.securityScanner.findings = make(map[int64]*SecFinding)
		for i := range findings {
			findings[i].ID = a.securityScanner.nextID
			findings[i].DetectedAt = now
			a.securityScanner.findings[a.securityScanner.nextID] = &findings[i]
			a.securityScanner.nextID++
		}
		a.securityScanner.mu.Unlock()
	}

	if a.podHealthScanner != nil {
		a.podHealthScanner.mu.Lock()
		a.podHealthScanner.report = &PodHealthReport{Timestamp: now, TotalPods: 46, HealthyPods: 39, Categories: []PodHealthCategory{
			{Name: "crashloop", Count: 3, Pods: []PodHealthItem{
				{Namespace: "default", Name: "api-gateway-x2k9m", Phase: "Running", Reason: "CrashLoopBackOff", Message: "Back-off restarting failed container", Restarts: 14, Age: "2h", Node: "node-1"},
				{Namespace: "elk", Name: "logstash-0", Phase: "Running", Reason: "CrashLoopBackOff", Message: "OOMKilled", Restarts: 8, Age: "45m", Node: "node-2"},
				{Namespace: "monitoring", Name: "alertmanager-0", Phase: "Running", Reason: "CrashLoopBackOff", Message: "config validation failed", Restarts: 5, Age: "1h", Node: "node-1"},
			}},
			{Name: "pending", Count: 2, Pods: []PodHealthItem{
				{Namespace: "default", Name: "ml-trainer-batch", Phase: "Pending", Reason: "Unschedulable", Message: "Insufficient cpu", Age: "15m"},
				{Namespace: "kube-system", Name: "coredns-backup", Phase: "Pending", Reason: "Unschedulable", Message: "Insufficient memory", Age: "8m"},
			}},
			{Name: "oomkilled", Count: 1, Pods: []PodHealthItem{
				{Namespace: "avika", Name: "backend-worker-abc", Phase: "Running", Reason: "OOMKilled", Message: "memory limit exceeded (512Mi)", Restarts: 3, Age: "30m", Node: "node-3"},
			}},
			{Name: "evicted", Count: 1, Pods: []PodHealthItem{
				{Namespace: "default", Name: "batch-processor-old", Phase: "Failed", Reason: "Evicted", Message: "low on ephemeral-storage", Age: "4h", Node: "node-2"},
			}},
		}}
		a.podHealthScanner.mu.Unlock()
	}

	if a.anomalyDetector != nil {
		a.anomalyDetector.mu.Lock()
		a.anomalyDetector.anomalies = map[int64]*Anomaly{
			1: {ID: 1, Service: "api-gateway", Namespace: "default", Metric: "error_rate", Score: 4.2, Expected: 0.02, Observed: 0.18, Severity: "critical", DetectedAt: now.Add(-10 * time.Minute), Status: "active"},
			2: {ID: 2, Service: "backend-worker", Namespace: "avika", Metric: "memory_usage", Score: 3.5, Expected: 0.45, Observed: 0.92, Severity: "high", DetectedAt: now.Add(-5 * time.Minute), Status: "active"},
			3: {ID: 3, Service: "frontend", Namespace: "default", Metric: "p95_latency", Score: -3.1, Expected: 120, Observed: 350, Severity: "high", DetectedAt: now.Add(-3 * time.Minute), Status: "active"},
			4: {ID: 4, Service: "coredns", Namespace: "kube-system", Metric: "request_rate", Score: 2.8, Expected: 1200, Observed: 3400, Severity: "medium", DetectedAt: now.Add(-15 * time.Minute), Status: "active"},
		}
		a.anomalyDetector.nextID = 5
		a.anomalyDetector.mu.Unlock()
	}

	if a.optimizerRegistry != nil {
		recs := []OptRecommendation{
			{Type: "rightsizing", Severity: "medium", Confidence: 0.91, Target: OptTarget{Kind: "Deployment", Namespace: "default", Name: "api-gateway", Container: "api"}, CurrentState: map[string]any{"cpuRequest": "2000m", "cpuUsedP95": "450m"}, SuggestedState: map[string]any{"cpuRequest": "800m"}, Rationale: "CPU under 25% for 7d", EstimatedSavingsMonthly: 42.50, Status: "open"},
			{Type: "rightsizing", Severity: "medium", Confidence: 0.87, Target: OptTarget{Kind: "Deployment", Namespace: "avika", Name: "backend", Container: "worker"}, CurrentState: map[string]any{"memReq": "1Gi", "memP95": "280Mi"}, SuggestedState: map[string]any{"memReq": "512Mi"}, Rationale: "Memory at 28%", EstimatedSavingsMonthly: 18.00, Status: "open"},
			{Type: "hpa", Severity: "high", Confidence: 0.95, Target: OptTarget{Kind: "Deployment", Namespace: "default", Name: "api-gateway"}, CurrentState: map[string]any{"replicas": 5, "maxReplicas": 5}, SuggestedState: map[string]any{"maxReplicas": 10}, Rationale: "HPA stuck at max", Status: "open"},
			{Type: "coredns", Severity: "low", Confidence: 0.78, Target: OptTarget{Kind: "ConfigMap", Namespace: "kube-system", Name: "coredns"}, CurrentState: map[string]any{"ndots": "5"}, SuggestedState: map[string]any{"ndots": "2"}, Rationale: "Saves 60% DNS queries", Status: "open"},
			{Type: "cluster", Severity: "medium", Confidence: 0.85, Target: OptTarget{Kind: "Node", Name: "node-3"}, CurrentState: map[string]any{"cpuUtil": "12%", "memUtil": "18%"}, SuggestedState: map[string]any{"action": "drain"}, Rationale: "Underutilized", EstimatedSavingsMonthly: 120, Status: "open"},
		}
		a.optimizerRegistry.mu.Lock()
		a.optimizerRegistry.recommendations = make(map[int64]*OptRecommendation)
		for i := range recs {
			recs[i].ID = a.optimizerRegistry.nextID
			recs[i].CreatedAt = now
			a.optimizerRegistry.recommendations[a.optimizerRegistry.nextID] = &recs[i]
			a.optimizerRegistry.nextID++
		}
		a.optimizerRegistry.mu.Unlock()
	}

	if a.errorAggregator != nil {
		for _, e := range []IngestEvent{
			{Timestamp: now.Add(-30 * time.Minute), Namespace: "default", Pod: "api-gateway-x2k9m", Service: "api-gateway", Level: "error", Message: "Back-off restarting", Reason: "CrashLoopBackOff", Fingerprint: "CrashLoopBackOff/default/api-gateway"},
			{Timestamp: now.Add(-25 * time.Minute), Namespace: "elk", Pod: "logstash-0", Service: "logstash", Level: "error", Message: "OOMKilled", Reason: "OOMKilled", Fingerprint: "OOMKilled/elk/logstash"},
			{Timestamp: now.Add(-20 * time.Minute), Namespace: "default", Pod: "ml-trainer-batch", Service: "ml-trainer", Level: "warn", Message: "Insufficient cpu", Reason: "FailedScheduling", Fingerprint: "FailedScheduling/default/ml-trainer"},
			{Timestamp: now.Add(-15 * time.Minute), Namespace: "avika", Pod: "backend-worker-abc", Service: "backend-worker", Level: "error", Message: "memory limit exceeded", Reason: "OOMKilled", Fingerprint: "OOMKilled/avika/backend-worker"},
			{Timestamp: now.Add(-10 * time.Minute), Namespace: "monitoring", Pod: "alertmanager-0", Service: "alertmanager", Level: "error", Message: "config validation failed", Reason: "CrashLoopBackOff", Fingerprint: "CrashLoopBackOff/monitoring/alertmanager"},
			{Timestamp: now.Add(-5 * time.Minute), Namespace: "kube-system", Pod: "coredns-4fz96", Service: "coredns", Level: "warn", Message: "Search Line limits exceeded", Reason: "DNSConfigForming", Fingerprint: "DNSConfigForming/kube-system/coredns"},
		} {
			a.errorAggregator.Ingest(e)
		}
	}

	if a.correlator != nil {
		for _, sig := range []Signal{
			{ID: "mock-1", Timestamp: now.Add(-30 * time.Minute), Source: "k8s", Severity: "high", Service: "api-gateway", Namespace: "default", Pod: "api-gateway-x2k9m", Kind: "restart", Title: "CrashLoopBackOff: default/api-gateway"},
			{ID: "mock-2", Timestamp: now.Add(-29 * time.Minute), Source: "anomaly", Severity: "critical", Service: "api-gateway", Namespace: "default", Kind: "spike", Title: "Error rate spike on api-gateway"},
			{ID: "mock-3", Timestamp: now.Add(-28 * time.Minute), Source: "security", Severity: "high", Namespace: "monitoring", Kind: "pod-security", Title: "Privileged container: monitoring/debug-shell"},
			{ID: "mock-4", Timestamp: now.Add(-15 * time.Minute), Source: "k8s", Severity: "medium", Service: "ml-trainer", Namespace: "default", Kind: "pending", Title: "Unschedulable: Insufficient cpu"},
			{ID: "mock-5", Timestamp: now.Add(-10 * time.Minute), Source: "k8s", Severity: "high", Service: "logstash", Namespace: "elk", Pod: "logstash-0", Kind: "oom", Title: "OOMKilled: elk/logstash-0"},
			{ID: "mock-6", Timestamp: now.Add(-5 * time.Minute), Source: "anomaly", Severity: "high", Service: "backend-worker", Namespace: "avika", Kind: "spike", Title: "Memory spike on backend-worker"},
		} {
			a.correlator.IngestSignal(sig)
		}
	}

	// --- LB Log Aggregator ---
	if a.lbLogAggregator != nil {
		now := time.Now()
		urls := []string{"/api/v1/users", "/api/v1/orders", "/api/v1/products", "/healthz", "/api/v1/search", "/api/v1/auth/login", "/static/app.js", "/api/v1/payments"}
		methods := []string{"GET", "POST", "GET", "GET", "GET", "POST", "GET", "POST"}
		for i := 0; i < 500; i++ {
			idx := i % len(urls)
			status := 200
			targetStatus := 200
			latency := 15.0 + float64(m.rng.IntN(100))
			// Inject some errors
			if i%50 == 0 {
				status = 502
				targetStatus = 500
				latency = 800 + float64(m.rng.IntN(2000))
			} else if i%30 == 0 {
				status = 429
				targetStatus = 429
				latency = 5
			} else if i%20 == 0 {
				status = 404
				targetStatus = 404
			}
			a.lbLogAggregator.Ingest(
				"app-alb", "ALB",
				urls[idx], methods[idx], "tg-app-prod",
				status, targetStatus, latency,
				now.Add(-time.Duration(i)*3*time.Second),
			)
		}
		// Second LB
		for i := 0; i < 200; i++ {
			status := 200
			latency := 5.0 + float64(m.rng.IntN(30))
			if i%40 == 0 {
				status = 503
				latency = 5000
			}
			a.lbLogAggregator.Ingest(
				"internal-nlb", "NLB",
				"/grpc.health.v1.Health/Check", "POST", "tg-internal",
				status, status, latency,
				now.Add(-time.Duration(i)*5*time.Second),
			)
		}
	}

	log.Info().Msg("Mock data populated for all dashboard pages")
}

func (m *mockSource) clearAllHandlers() {
	a := m.analyzer
	if a.securityScanner != nil {
		a.securityScanner.mu.Lock()
		a.securityScanner.findings = make(map[int64]*SecFinding)
		a.securityScanner.mu.Unlock()
	}
	if a.podHealthScanner != nil {
		a.podHealthScanner.mu.Lock()
		a.podHealthScanner.report = nil
		a.podHealthScanner.mu.Unlock()
	}
	if a.anomalyDetector != nil {
		a.anomalyDetector.mu.Lock()
		a.anomalyDetector.anomalies = make(map[int64]*Anomaly)
		a.anomalyDetector.mu.Unlock()
	}
	if a.optimizerRegistry != nil {
		a.optimizerRegistry.mu.Lock()
		a.optimizerRegistry.recommendations = make(map[int64]*OptRecommendation)
		a.optimizerRegistry.mu.Unlock()
	}
	if a.errorAggregator != nil {
		a.errorAggregator.mu.Lock()
		a.errorAggregator.groups = make(map[string]*ErrorGroup)
		a.errorAggregator.mu.Unlock()
	}
	if a.correlator != nil {
		a.correlator.mu.Lock()
		a.correlator.incidents = make(map[int64]*Incident)
		a.correlator.mu.Unlock()
	}
	if a.lbLogAggregator != nil {
		a.lbLogAggregator.mu.Lock()
		a.lbLogAggregator.requests = make(map[string][]lbReqSummary)
		a.lbLogAggregator.configs = nil
		a.lbLogAggregator.mu.Unlock()
	}
	log.Info().Msg("Mock data cleared from all handlers")
}
