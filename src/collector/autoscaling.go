package main

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/labels"
)

// collectHPAMetrics gathers metrics for HorizontalPodAutoscalers
func (c *Collector) collectHPAMetrics(ctx context.Context) {
	// Not all clusters use v2, but we use what we can from informers
	// Let's use v2 which is standard in 1.23+
	hpas, err := c.informerFactory.Autoscaling().V2().HorizontalPodAutoscalers().Lister().List(labels.Everything())
	if err != nil {
		return
	}

	for _, hpa := range hpas {
		metrics := ResourceMetrics{
			Timestamp:    time.Now(),
			Cluster:      c.config.ClusterID,
			ResourceType: "hpa",
			Resource: ResourceIdentifier{
				Namespace: hpa.Namespace,
				Name:      hpa.Name,
			},
			Metrics: map[string]interface{}{
				"min_replicas":     1, // Default if nil
				"max_replicas":     hpa.Spec.MaxReplicas,
				"current_replicas": hpa.Status.CurrentReplicas,
				"desired_replicas": hpa.Status.DesiredReplicas,
			},
		}

		if hpa.Spec.MinReplicas != nil {
			metrics.Metrics["min_replicas"] = *hpa.Spec.MinReplicas
		}

		// Calculate load percentage if possible
		if hpa.Spec.MaxReplicas > 0 {
			metrics.Metrics["utilization_percent"] = (float64(hpa.Status.CurrentReplicas) / float64(hpa.Spec.MaxReplicas)) * 100
		}

		c.metricsBuffer.Push(metrics)
		c.metricsCollected.Inc()
	}
}
