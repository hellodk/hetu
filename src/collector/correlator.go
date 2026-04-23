package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	types "github.com/hellodk/hetu/pkg/types"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
)

// GetCorrelatedEvents correlates recent events with metrics and logs
func (c *Collector) GetCorrelatedEvents(ctx context.Context) []types.CorrelatedEvidence {
	var results []types.CorrelatedEvidence
	events := c.eventBuffer.GetAll()

	for _, event := range events {
		if event.Type != "Warning" && event.Type != "Error" {
			continue
		}

		evidence := types.CorrelatedEvidence{
			Event:       event,
			Metrics:     make(map[string][]types.DataPoint),
			LogLines:    []string{},
			RelatedPods: []string{},
		}

		// Connect to pod if applicable
		if event.InvolvedObject.Kind == "Pod" {
			evidence.RelatedPods = append(evidence.RelatedPods, event.InvolvedObject.Name)

			// Fetch logs if recent enough
			if time.Since(event.Timestamp) < time.Hour {
				logs, err := c.fetchPodLogs(ctx, event.InvolvedObject.Namespace, event.InvolvedObject.Name, 50)
				if err == nil {
					evidence.LogLines = logs
				} else {
					log.Debug().Err(err).Str("pod", event.InvolvedObject.Name).Msg("Failed to fetch pod logs for correlation")
				}
			}
		}

		// Query Prometheus if available
		if c.config.PrometheusEndpoint != "" {
			evidence.Metrics = c.queryPrometheusAroundTime(ctx, event.InvolvedObject, event.Timestamp)
		}

		results = append(results, evidence)
	}

	return results
}

// fetchPodLogs retrieves the tail lines from a pod's logs
func (c *Collector) fetchPodLogs(ctx context.Context, namespace, podName string, tailLines int64) ([]string, error) {
	req := c.clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		TailLines: &tailLines,
	})

	podLogs, err := req.Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer podLogs.Close()

	buf := new(strings.Builder)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	return lines, nil
}

// queryPrometheusAroundTime queries Prometheus for CPU/Memory ±5min of the event
func (c *Collector) queryPrometheusAroundTime(ctx context.Context, obj types.InvolvedObject, t time.Time) map[string][]types.DataPoint {
	metrics := make(map[string][]types.DataPoint)
	if obj.Kind != "Pod" && obj.Kind != "Node" {
		return metrics
	}

	start := t.Add(-5 * time.Minute).Unix()
	end := t.Add(5 * time.Minute).Unix()
	step := 30

	queries := map[string]string{}
	if obj.Kind == "Pod" {
		queries["cpu"] = fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{pod="%s",namespace="%s"}[1m]))`, obj.Name, obj.Namespace)
		queries["memory"] = fmt.Sprintf(`sum(container_memory_usage_bytes{pod="%s",namespace="%s"})`, obj.Name, obj.Namespace)
	} else if obj.Kind == "Node" {
		queries["cpu"] = fmt.Sprintf(`100 - (avg by(instance) (rate(node_cpu_seconds_total{mode="idle",instance=~"%s.*"}[1m])) * 100)`, obj.Name)
		queries["memory"] = fmt.Sprintf(`node_memory_MemTotal_bytes{instance=~"%s.*"} - node_memory_MemAvailable_bytes{instance=~"%s.*"}`, obj.Name, obj.Name)
	}

	for name, query := range queries {
		encodedQuery := url.QueryEscape(query)
		urlStr := fmt.Sprintf("%s/api/v1/query_range?query=%s&start=%d&end=%d&step=%d",
			c.config.PrometheusEndpoint, encodedQuery, start, end, step)

		req, _ := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Debug().Err(err).Str("query", name).Msg("Failed to query prometheus")
			continue
		}

		var pResp struct {
			Data struct {
				Result []struct {
					Values [][]any `json:"values"`
				} `json:"result"`
			} `json:"data"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&pResp); err == nil {
			if len(pResp.Data.Result) > 0 {
				var points []types.DataPoint
				for _, v := range pResp.Data.Result[0].Values {
					if len(v) == 2 {
						ts := int64(v[0].(float64))
						valStr, ok := v[1].(string)
						if !ok {
							continue
						}
						var val float64
						_, _ = fmt.Sscanf(valStr, "%f", &val)
						points = append(points, types.DataPoint{
							Timestamp: time.Unix(ts, 0),
							Value:     val,
						})
					}
				}
				metrics[name] = points
			}
		}
		resp.Body.Close()
	}

	return metrics
}
