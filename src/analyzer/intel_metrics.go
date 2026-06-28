package main

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// intel_metrics.go exports data that hetu ALREADY computes in-process as
// Prometheus gauges on the analyzer's custom registry (a.registry). These
// series back the cluster-intel dashboard. All gauges are (re)set on every
// scan cycle, including 0 for absent label values so stale highs clear.

// Fixed label sets. We always set every value (0 when absent) so the
// dashboard never shows stale counts after an issue clears.
var (
	// Pod health categories emitted by PodHealthScanner.Scan.
	intelPodHealthCategories = []string{
		"crashloop", "oomkilled", "imagepull", "pending",
		"failed", "evicted", "terminating", "completed",
	}
	// Severities emitted by SecurityScanner findings.
	intelVulnSeverities = []string{"critical", "high", "medium", "low"}
	// Resource categories hetu actually computes (rightsizing optimizer).
	intelResourceCategories = []string{"over_provisioned", "under_provisioned"}
)

// initIntelMetrics registers the cluster-intel gauges on a.registry. It must
// be called after initMetrics() (which creates a.registry).
func (a *Analyzer) initIntelMetrics() {
	a.podHealthGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cluster_intel_pod_health_total",
		Help: "Number of pods per health category from hetu's pod health scan",
	}, []string{"category"})

	a.vulnGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cluster_intel_vulnerabilities_total",
		Help: "Number of security findings by severity from hetu's security scan",
	}, []string{"severity"})

	a.cisChecksGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cluster_intel_cis_checks_total",
		Help: "Number of CIS benchmark checks by status (hetu only emits failures)",
	}, []string{"status"})

	a.resourceCatGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cluster_intel_resources_category_total",
		Help: "Number of open rightsizing recommendations by category from hetu's optimizer",
	}, []string{"category"})

	a.registry.MustRegister(
		a.podHealthGauge,
		a.vulnGauge,
		a.cisChecksGauge,
		a.resourceCatGauge,
	)
}

// setPodHealthMetrics refreshes cluster_intel_pod_health_total{category} from
// a completed pod health scan. Categories with no pods are set to 0.
func (a *Analyzer) setPodHealthMetrics(report *PodHealthReport) {
	if a.podHealthGauge == nil || report == nil {
		return
	}
	counts := make(map[string]int, len(report.Categories))
	for _, c := range report.Categories {
		counts[c.Name] = c.Count
	}
	for _, cat := range intelPodHealthCategories {
		a.podHealthGauge.WithLabelValues(cat).Set(float64(counts[cat]))
	}
}

// setSecurityMetrics refreshes cluster_intel_vulnerabilities_total{severity}
// and cluster_intel_cis_checks_total{status="fail"} from security findings.
// hetu does not track CIS passes, so only status="fail" is emitted.
func (a *Analyzer) setSecurityMetrics(findings []*SecFinding) {
	if a.vulnGauge == nil || a.cisChecksGauge == nil {
		return
	}
	bySev := make(map[string]int, len(intelVulnSeverities))
	cisFail := 0
	for _, f := range findings {
		if f == nil {
			continue
		}
		bySev[f.Severity]++
		if f.CISControl != "" {
			cisFail++
		}
	}
	for _, sev := range intelVulnSeverities {
		a.vulnGauge.WithLabelValues(sev).Set(float64(bySev[sev]))
	}
	a.cisChecksGauge.WithLabelValues("fail").Set(float64(cisFail))
}

// setResourceCategoryMetrics refreshes cluster_intel_resources_category_total
// {category}. Counts are already normalized to snake_case by the caller.
// Categories with no recommendations are set to 0.
func (a *Analyzer) setResourceCategoryMetrics(counts map[string]int) {
	if a.resourceCatGauge == nil {
		return
	}
	for _, cat := range intelResourceCategories {
		a.resourceCatGauge.WithLabelValues(cat).Set(float64(counts[cat]))
	}
}

// normalizeCategory converts an optimizer category string (e.g.
// "over-provisioned") to the snake_case label used by the metric.
func normalizeCategory(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "-", "_")
}
