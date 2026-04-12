package main

import (
	"fmt"

	"github.com/rs/zerolog/log"
)

// GCOptimizer analyzes garbage collection metrics for JVM/Go/Node runtimes.
type GCOptimizer struct{}

func (o *GCOptimizer) Name() string { return "gc" }

func (o *GCOptimizer) Run(ctx OptimizerContext) ([]OptRecommendation, error) {
	if ctx.PrometheusURL == "" {
		return nil, fmt.Errorf("prometheus URL not configured")
	}

	var recs []OptRecommendation

	// Go GC — check if GOGC-related pauses are high
	goGC, err := queryPromInstant(ctx.PrometheusURL,
		`sum by (namespace, pod) (rate(go_gc_duration_seconds_sum[5m])) / sum by (namespace, pod) (rate(go_gc_duration_seconds_count[5m]))`)
	if err == nil {
		for _, result := range goGC {
			metric, ok := result["metric"].(map[string]any)
			if !ok {
				continue
			}
			ns, _ := metric["namespace"].(string)
			pod, _ := metric["pod"].(string)
			value, ok := result["value"].([]any)
			if !ok || len(value) < 2 {
				continue
			}
			avgPauseMs := parsePromValue(value[1]) * 1000
			if avgPauseMs > 50 { // >50ms avg GC pause
				recs = append(recs, OptRecommendation{
					Type:       "gc",
					Severity:   "medium",
					Confidence: 0.6,
					Target:     OptTarget{Kind: "Pod", Namespace: ns, Name: pod},
					CurrentState: map[string]any{
						"runtime":     "Go",
						"avgPauseMs":  fmt.Sprintf("%.1fms", avgPauseMs),
					},
					Rationale: fmt.Sprintf("Go GC avg pause is %.1fms. Consider tuning GOGC or increasing memory limits.", avgPauseMs),
					YAMLPatch: `env:
  - name: GOGC
    value: "200"   # default 100; higher = less frequent GC`,
				})
			}
		}
	}

	// JVM GC — check jvm_gc_pause_seconds if available
	jvmGC, err := queryPromInstant(ctx.PrometheusURL,
		`sum by (namespace, pod) (rate(jvm_gc_pause_seconds_sum[5m])) / sum by (namespace, pod) (rate(jvm_gc_pause_seconds_count[5m]))`)
	if err == nil {
		for _, result := range jvmGC {
			metric, ok := result["metric"].(map[string]any)
			if !ok {
				continue
			}
			ns, _ := metric["namespace"].(string)
			pod, _ := metric["pod"].(string)
			value, ok := result["value"].([]any)
			if !ok || len(value) < 2 {
				continue
			}
			avgPauseMs := parsePromValue(value[1]) * 1000
			if avgPauseMs > 100 { // >100ms avg
				recs = append(recs, OptRecommendation{
					Type:       "gc",
					Severity:   "high",
					Confidence: 0.7,
					Target:     OptTarget{Kind: "Pod", Namespace: ns, Name: pod},
					CurrentState: map[string]any{
						"runtime":    "JVM",
						"avgPauseMs": fmt.Sprintf("%.1fms", avgPauseMs),
					},
					Rationale: fmt.Sprintf("JVM GC avg pause is %.1fms. Consider switching to ZGC/Shenandoah or increasing heap (-Xmx).", avgPauseMs),
					YAMLPatch: `env:
  - name: JAVA_OPTS
    value: "-XX:+UseZGC -Xmx2g"  # or -XX:+UseShenandoahGC`,
				})
			}
		}
	}

	log.Debug().Int("recs", len(recs)).Msg("GC optimizer completed")
	return recs, nil
}
