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
	// Divide the total savings across the recommendations so the sum stays
	// consistent with report.EstimatedSavings.
	per := totalSavings / float64(n)
	for i := 0; i < n; i++ {
		entry := mockRecCatalog[i]
		entry.ID = fmt.Sprintf("mock-rec-%d-%d", now.Unix(), i)
		entry.Timestamp = now
		entry.Impact.CostSavings = &types.CostSavings{
			Monthly:  per,
			Currency: "USD",
		}
		out = append(out, entry)
	}
	return out
}
