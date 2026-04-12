package main

import (
	"fmt"
	"math"

	"github.com/rs/zerolog/log"
)

// RightSizingOptimizer analyzes CPU/memory usage vs requests/limits.
type RightSizingOptimizer struct{}

func (o *RightSizingOptimizer) Name() string { return "rightsizing" }

func (o *RightSizingOptimizer) Run(ctx OptimizerContext) ([]OptRecommendation, error) {
	if ctx.PrometheusURL == "" {
		return nil, fmt.Errorf("prometheus URL not configured")
	}

	var recs []OptRecommendation

	// Query CPU usage p95 over 14d per container
	cpuUsage, err := queryPromInstant(ctx.PrometheusURL,
		`quantile_over_time(0.95, rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[5m])[14d:5m])`)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to query CPU usage")
	}

	// Query CPU requests per container
	cpuRequests, err := queryPromInstant(ctx.PrometheusURL,
		`kube_pod_container_resource_requests{resource="cpu",container!=""}`)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to query CPU requests")
	}

	// Query CPU limits
	cpuLimits, err := queryPromInstant(ctx.PrometheusURL,
		`kube_pod_container_resource_limits{resource="cpu",container!=""}`)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to query CPU limits")
	}

	// Query memory usage p95
	memUsage, err := queryPromInstant(ctx.PrometheusURL,
		`quantile_over_time(0.95, container_memory_working_set_bytes{container!="",container!="POD"}[14d:5m])`)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to query memory usage")
	}

	// Query memory requests
	memRequests, err := queryPromInstant(ctx.PrometheusURL,
		`kube_pod_container_resource_requests{resource="memory",container!=""}`)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to query memory requests")
	}

	// Build lookup maps: key = namespace/pod/container -> value
	cpuUsageMap := buildMetricMap(cpuUsage)
	cpuReqMap := buildMetricMap(cpuRequests)
	cpuLimMap := buildMetricMap(cpuLimits)
	memUsageMap := buildMetricMap(memUsage)
	memReqMap := buildMetricMap(memRequests)

	// Analyze each container
	analyzed := map[string]bool{}
	for key := range cpuUsageMap {
		if analyzed[key] {
			continue
		}
		analyzed[key] = true

		ns, pod, container := parseMetricKey(key)
		if ns == "" || container == "" {
			continue
		}

		cpuUsed := cpuUsageMap[key]
		cpuReq := cpuReqMap[key]
		cpuLim := cpuLimMap[key]
		memUsed := memUsageMap[key]
		memReq := memReqMap[key]

		// Skip if no requests set (can't compare)
		if cpuReq == 0 && memReq == 0 {
			continue
		}

		// Categorize
		cpuRatio := safeDivide(cpuUsed, cpuReq)
		memRatio := safeDivide(memUsed, memReq)

		var category string
		var severity string
		switch {
		case cpuRatio < 0.30 || memRatio < 0.30:
			category = "over-provisioned"
			severity = "medium"
		case cpuRatio > 0.85 || memRatio > 0.85:
			category = "under-provisioned"
			severity = "high"
		default:
			continue // optimal, skip
		}

		// Calculate suggested values
		sugCPUReq := roundCPU(cpuUsed * 1.2)
		sugCPULim := roundCPU(cpuUsed * 1.5)
		sugMemReq := roundMem(memUsed * 1.2)
		sugMemLim := roundMem(memUsed * 1.5)

		if sugCPULim < sugCPUReq {
			sugCPULim = sugCPUReq
		}
		if sugMemLim < sugMemReq {
			sugMemLim = sugMemReq
		}

		// Estimate savings (rough: $0.03/core-hour, $0.004/GB-hour)
		cpuSaved := math.Max(0, cpuReq-sugCPUReq) // cores saved
		memSavedGB := math.Max(0, (memReq-sugMemReq)/1e9)
		monthlySavings := (cpuSaved*0.03 + memSavedGB*0.004) * 730 // hours/month

		yaml := fmt.Sprintf(`resources:
  requests:
    cpu: "%sm"
    memory: "%sMi"
  limits:
    cpu: "%sm"
    memory: "%sMi"`,
			formatMillicores(sugCPUReq),
			formatMi(sugMemReq),
			formatMillicores(sugCPULim),
			formatMi(sugMemLim))

		rec := OptRecommendation{
			Type:     "rightsizing",
			Severity: severity,
			Confidence: 0.8,
			Target: OptTarget{
				Kind:      "Pod",
				Namespace: ns,
				Name:      pod,
				Container: container,
			},
			CurrentState: map[string]any{
				"cpuRequest": fmt.Sprintf("%.0fm", cpuReq*1000),
				"cpuLimit":   fmt.Sprintf("%.0fm", cpuLim*1000),
				"cpuUsedP95": fmt.Sprintf("%.0fm", cpuUsed*1000),
				"memRequest": formatBytes(memReq),
				"memUsedP95": formatBytes(memUsed),
				"cpuRatio":   fmt.Sprintf("%.0f%%", cpuRatio*100),
				"memRatio":   fmt.Sprintf("%.0f%%", memRatio*100),
				"category":   category,
			},
			SuggestedState: map[string]any{
				"cpuRequest": fmt.Sprintf("%.0fm", sugCPUReq*1000),
				"cpuLimit":   fmt.Sprintf("%.0fm", sugCPULim*1000),
				"memRequest": formatBytes(sugMemReq),
				"memLimit":   formatBytes(sugMemLim),
			},
			Rationale:               fmt.Sprintf("%s: CPU at %.0f%% of request, memory at %.0f%% of request", category, cpuRatio*100, memRatio*100),
			EstimatedSavingsMonthly: monthlySavings,
			YAMLPatch:               yaml,
		}

		recs = append(recs, rec)
	}

	return recs, nil
}

// --- Helpers ---

func buildMetricMap(results []map[string]any) map[string]float64 {
	m := map[string]float64{}
	for _, r := range results {
		metric, ok := r["metric"].(map[string]any)
		if !ok {
			continue
		}
		ns, _ := metric["namespace"].(string)
		pod, _ := metric["pod"].(string)
		container, _ := metric["container"].(string)
		if ns == "" || container == "" {
			continue
		}
		key := ns + "/" + pod + "/" + container

		value, ok := r["value"].([]any)
		if !ok || len(value) < 2 {
			continue
		}
		m[key] = parsePromValue(value[1])
	}
	return m
}

func parseMetricKey(key string) (ns, pod, container string) {
	parts := splitN(key, "/", 3)
	if len(parts) == 3 {
		return parts[0], parts[1], parts[2]
	}
	return "", "", ""
}

func splitN(s, sep string, n int) []string {
	result := make([]string, 0, n)
	for i := 0; i < n-1; i++ {
		idx := indexOf(s, sep)
		if idx < 0 {
			break
		}
		result = append(result, s[:idx])
		s = s[idx+len(sep):]
	}
	result = append(result, s)
	return result
}

func indexOf(s, sep string) int {
	for i := range s {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}

func roundCPU(cores float64) float64 {
	// Round to nearest 50m (0.05 cores)
	return math.Ceil(cores*20) / 20
}

func roundMem(bytes float64) float64 {
	// Round up to nearest 32Mi
	mi := bytes / (1024 * 1024)
	return math.Ceil(mi/32) * 32 * 1024 * 1024
}

func formatMillicores(cores float64) string {
	return fmt.Sprintf("%.0f", cores*1000)
}

func formatMi(bytes float64) string {
	return fmt.Sprintf("%.0f", bytes/(1024*1024))
}

func formatBytes(b float64) string {
	if b > 1e9 {
		return fmt.Sprintf("%.1fGi", b/1e9)
	}
	return fmt.Sprintf("%.0fMi", b/(1024*1024))
}
