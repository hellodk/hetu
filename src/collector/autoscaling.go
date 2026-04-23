package main

import (
	"context"
	"time"

	types "github.com/hellodk/hetu/pkg/types"
	"k8s.io/apimachinery/pkg/labels"
)

// collectHPAMetrics gathers metrics for HorizontalPodAutoscalers
func (c *Collector) collectHPAMetrics(_ context.Context) {
	hpas, err := c.informerFactory.Autoscaling().V2().HorizontalPodAutoscalers().Lister().List(labels.Everything())
	if err != nil {
		return
	}

	for _, hpa := range hpas {
		metrics := types.ResourceMetrics{
			Timestamp:    time.Now(),
			Cluster:      c.config.ClusterID,
			ResourceType: "hpa",
			Resource: types.ResourceIdentifier{
				Namespace: hpa.Namespace,
				Name:      hpa.Name,
			},
			Metrics: map[string]any{
				"min_replicas":     1,
				"max_replicas":     hpa.Spec.MaxReplicas,
				"current_replicas": hpa.Status.CurrentReplicas,
				"desired_replicas": hpa.Status.DesiredReplicas,
			},
		}

		if hpa.Spec.MinReplicas != nil {
			metrics.Metrics["min_replicas"] = *hpa.Spec.MinReplicas
		}

		if hpa.Spec.MaxReplicas > 0 {
			metrics.Metrics["utilization_percent"] = (float64(hpa.Status.CurrentReplicas) / float64(hpa.Spec.MaxReplicas)) * 100
		}

		c.metricsBuffer.Push(metrics)
		c.metricsCollected.Inc()
	}
}
