package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	types "github.com/your-org/cluster-intel/pkg/types"
)

// collectPVCMetrics queries prometheus for PVC metrics and adds to the metrics buffer
func (c *Collector) collectPVCMetrics(ctx context.Context) {
	if c.config.PrometheusEndpoint == "" {
		return
	}

	queries := map[string]string{
		"capacity_bytes": `kubelet_volume_stats_capacity_bytes`,
		"used_bytes":     `kubelet_volume_stats_used_bytes`,
	}

	results := make(map[string]map[string]float64)

	for name, query := range queries {
		encodedQuery := url.QueryEscape(query)
		urlStr := fmt.Sprintf("%s/api/v1/query?query=%s", c.config.PrometheusEndpoint, encodedQuery)

		req, _ := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Debug().Err(err).Str("query", name).Msg("Failed to query prometheus for PVCs")
			continue
		}

		var pResp struct {
			Data struct {
				Result []struct {
					Metric map[string]string `json:"metric"`
					Value  []any             `json:"value"`
				} `json:"result"`
			} `json:"data"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&pResp); err == nil {
			for _, res := range pResp.Data.Result {
				if len(res.Value) == 2 {
					valStr, ok := res.Value[1].(string)
					pvcName := res.Metric["persistentvolumeclaim"]
					namespace := res.Metric["namespace"]
					if ok && valStr != "NaN" && pvcName != "" {
						var val float64
						_, _ = fmt.Sscanf(valStr, "%f", &val)

						key := fmt.Sprintf("%s/%s", namespace, pvcName)
						if _, exists := results[key]; !exists {
							results[key] = make(map[string]float64)
						}
						results[key][name] = val
					}
				}
			}
		}
		resp.Body.Close()
	}

	for key, metricsMap := range results {
		parts := strings.Split(key, "/")
		if len(parts) != 2 {
			continue
		}
		ns := parts[0]
		name := parts[1]

		metricMap := map[string]any{}
		if capBytes, ok := metricsMap["capacity_bytes"]; ok {
			metricMap["capacity_bytes"] = capBytes
			if usedBytes, ok := metricsMap["used_bytes"]; ok {
				metricMap["used_bytes"] = usedBytes
				if capBytes > 0 {
					metricMap["usage_percent"] = (usedBytes / capBytes) * 100
				}
			}
		}

		status := "Unknown"
		if pvc, err := c.informerFactory.Core().V1().PersistentVolumeClaims().Lister().PersistentVolumeClaims(ns).Get(name); err == nil {
			status = string(pvc.Status.Phase)
		}
		metricMap["status"] = status

		metrics := types.ResourceMetrics{
			Timestamp:    time.Now(),
			Cluster:      c.config.ClusterID,
			ResourceType: "pvc",
			Resource: types.ResourceIdentifier{
				Namespace: ns,
				Name:      name,
			},
			Metrics: metricMap,
		}

		c.metricsBuffer.Push(metrics)
		c.metricsCollected.Inc()
	}
}
