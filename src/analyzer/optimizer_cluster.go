package main

import (
	"fmt"

	"github.com/rs/zerolog/log"
)

// ClusterOptimizer analyzes node utilization for bin-packing opportunities.
type ClusterOptimizer struct{}

func (o *ClusterOptimizer) Name() string { return "cluster" }

func (o *ClusterOptimizer) Run(ctx OptimizerContext) ([]OptRecommendation, error) {
	if ctx.PrometheusURL == "" {
		return nil, fmt.Errorf("prometheus URL not configured")
	}

	var recs []OptRecommendation

	// Check node CPU utilization
	nodeUtil, err := queryPromInstant(ctx.PrometheusURL,
		`1 - avg by (node) (rate(node_cpu_seconds_total{mode="idle"}[30m]))`)
	if err == nil {
		for _, result := range nodeUtil {
			metric, ok := result["metric"].(map[string]any)
			if !ok {
				continue
			}
			node, _ := metric["node"].(string)
			value, ok := result["value"].([]any)
			if !ok || len(value) < 2 {
				continue
			}
			util := parsePromValue(value[1])
			if util < 0.15 { // <15% CPU utilization
				recs = append(recs, OptRecommendation{
					Type:       "cluster",
					Severity:   "medium",
					Confidence: 0.65,
					Target:     OptTarget{Kind: "Node", Name: node},
					CurrentState: map[string]any{
						"cpuUtilization": fmt.Sprintf("%.1f%%", util*100),
						"status":         "underutilized",
					},
					Rationale:               fmt.Sprintf("Node %s has only %.1f%% CPU utilization. Consider draining and removing this node to save costs.", node, util*100),
					EstimatedSavingsMonthly: 50, // rough per-node estimate
				})
			}
		}
	}

	// Check memory utilization
	memUtil, err := queryPromInstant(ctx.PrometheusURL,
		`1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)`)
	if err == nil {
		for _, result := range memUtil {
			metric, ok := result["metric"].(map[string]any)
			if !ok {
				continue
			}
			node, _ := metric["instance"].(string)
			value, ok := result["value"].([]any)
			if !ok || len(value) < 2 {
				continue
			}
			util := parsePromValue(value[1])
			if util > 0.90 { // >90% memory — pressure risk
				recs = append(recs, OptRecommendation{
					Type:       "cluster",
					Severity:   "high",
					Confidence: 0.8,
					Target:     OptTarget{Kind: "Node", Name: node},
					CurrentState: map[string]any{
						"memoryUtilization": fmt.Sprintf("%.1f%%", util*100),
						"status":            "memory-pressure-risk",
					},
					Rationale: fmt.Sprintf("Node %s memory is at %.1f%%. Risk of OOM kills. Consider adding nodes or right-sizing workloads.", node, util*100),
				})
			}
		}
	}

	log.Debug().Int("recs", len(recs)).Msg("Cluster optimizer completed")
	return recs, nil
}
