package main

import (
	"fmt"

	"github.com/rs/zerolog/log"
)

// HPAOptimizer analyzes HPA configurations and scaling behavior.
type HPAOptimizer struct{}

func (o *HPAOptimizer) Name() string { return "hpa" }

func (o *HPAOptimizer) Run(ctx OptimizerContext) ([]OptRecommendation, error) {
	if ctx.PrometheusURL == "" {
		return nil, fmt.Errorf("prometheus URL not configured")
	}

	var recs []OptRecommendation

	// Query HPA current vs max replicas over time
	stuckAtMax, err := queryPromInstant(ctx.PrometheusURL,
		`kube_horizontalpodautoscaler_status_current_replicas == kube_horizontalpodautoscaler_spec_max_replicas`)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to query HPA stuck-at-max")
	}

	for _, result := range stuckAtMax {
		metric, ok := result["metric"].(map[string]any)
		if !ok {
			continue
		}
		ns, _ := metric["namespace"].(string)
		name, _ := metric["horizontalpodautoscaler"].(string)
		if ns == "" || name == "" {
			continue
		}

		value, ok := result["value"].([]any)
		if !ok || len(value) < 2 {
			continue
		}
		currentMax := parsePromValue(value[1])

		suggestedMax := int(currentMax * 1.5)
		if suggestedMax < int(currentMax)+2 {
			suggestedMax = int(currentMax) + 2
		}

		recs = append(recs, OptRecommendation{
			Type:       "hpa",
			Severity:   "high",
			Confidence: 0.75,
			Target: OptTarget{
				Kind:      "HorizontalPodAutoscaler",
				Namespace: ns,
				Name:      name,
			},
			CurrentState: map[string]any{
				"maxReplicas":     int(currentMax),
				"currentReplicas": int(currentMax),
				"issue":           "stuck-at-max",
			},
			SuggestedState: map[string]any{
				"maxReplicas": suggestedMax,
			},
			Rationale: fmt.Sprintf("HPA %s/%s is running at max replicas (%d). The workload may need more capacity.", ns, name, int(currentMax)),
			YAMLPatch: fmt.Sprintf(`spec:
  maxReplicas: %d`, suggestedMax),
		})
	}

	// Query HPAs with min == current for extended periods (over-provisioned min)
	stuckAtMin, err := queryPromInstant(ctx.PrometheusURL,
		`kube_horizontalpodautoscaler_status_current_replicas == kube_horizontalpodautoscaler_spec_min_replicas and kube_horizontalpodautoscaler_spec_min_replicas > 1`)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to query HPA stuck-at-min")
	}

	for _, result := range stuckAtMin {
		metric, ok := result["metric"].(map[string]any)
		if !ok {
			continue
		}
		ns, _ := metric["namespace"].(string)
		name, _ := metric["horizontalpodautoscaler"].(string)
		if ns == "" || name == "" {
			continue
		}

		value, ok := result["value"].([]any)
		if !ok || len(value) < 2 {
			continue
		}
		currentMin := parsePromValue(value[1])

		if currentMin <= 1 {
			continue
		}

		suggestedMin := int(currentMin) - 1
		if suggestedMin < 1 {
			suggestedMin = 1
		}

		recs = append(recs, OptRecommendation{
			Type:       "hpa",
			Severity:   "low",
			Confidence: 0.6,
			Target: OptTarget{
				Kind:      "HorizontalPodAutoscaler",
				Namespace: ns,
				Name:      name,
			},
			CurrentState: map[string]any{
				"minReplicas":     int(currentMin),
				"currentReplicas": int(currentMin),
				"issue":           "possibly-overprovisioned-min",
			},
			SuggestedState: map[string]any{
				"minReplicas": suggestedMin,
			},
			Rationale: fmt.Sprintf("HPA %s/%s consistently runs at min replicas (%d). Consider lowering min to save resources.", ns, name, int(currentMin)),
			YAMLPatch: fmt.Sprintf(`spec:
  minReplicas: %d`, suggestedMin),
		})
	}

	return recs, nil
}
