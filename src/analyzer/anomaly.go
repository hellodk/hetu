package main

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Anomaly represents a detected statistical anomaly in a service metric.
type Anomaly struct {
	ID         int64     `json:"id"`
	Service    string    `json:"service"`
	Namespace  string    `json:"namespace"`
	Metric     string    `json:"metric"`
	Score      float64   `json:"score"`     // z-score or similar
	Expected   float64   `json:"expected"`
	Observed   float64   `json:"observed"`
	Severity   string    `json:"severity"`
	DetectedAt time.Time `json:"detectedAt"`
	Status     string    `json:"status"` // active, resolved
}

// AnomalyDetector runs statistical detectors on Prometheus metrics.
type AnomalyDetector struct {
	mu        sync.RWMutex
	anomalies map[int64]*Anomaly
	nextID    int64
	promURL   string
	clusterID string

	// Rolling stats per service+metric
	stats map[string]*rollingStats
}

type rollingStats struct {
	values []float64
	maxLen int
}

func (rs *rollingStats) push(v float64) {
	rs.values = append(rs.values, v)
	if len(rs.values) > rs.maxLen {
		rs.values = rs.values[1:]
	}
}

func (rs *rollingStats) mean() float64 {
	if len(rs.values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range rs.values {
		sum += v
	}
	return sum / float64(len(rs.values))
}

func (rs *rollingStats) stddev() float64 {
	if len(rs.values) < 2 {
		return 0
	}
	m := rs.mean()
	sumSq := 0.0
	for _, v := range rs.values {
		d := v - m
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(rs.values)-1))
}

func (rs *rollingStats) zscore(v float64) float64 {
	sd := rs.stddev()
	if sd == 0 {
		return 0
	}
	return (v - rs.mean()) / sd
}

// NewAnomalyDetector creates a new detector.
func NewAnomalyDetector(promURL, clusterID string) *AnomalyDetector {
	return &AnomalyDetector{
		anomalies: make(map[int64]*Anomaly),
		nextID:    1,
		promURL:   promURL,
		clusterID: clusterID,
		stats:     make(map[string]*rollingStats),
	}
}

// RunDetection queries Prometheus and detects anomalies.
func (d *AnomalyDetector) RunDetection() {
	if d.promURL == "" {
		return
	}

	metrics := []struct {
		name  string
		query string
	}{
		{"error_rate", `sum by (namespace, service) (rate(http_requests_total{code=~"5.."}[5m])) / sum by (namespace, service) (rate(http_requests_total[5m]))`},
		{"request_rate", `sum by (namespace, service) (rate(http_requests_total[5m]))`},
		{"p95_latency", `histogram_quantile(0.95, sum by (le, namespace, service) (rate(http_request_duration_seconds_bucket[5m])))`},
		{"restart_rate", `sum by (namespace, pod) (increase(kube_pod_container_status_restarts_total[5m]))`},
		{"cpu_usage", `sum by (namespace, pod) (rate(container_cpu_usage_seconds_total{container!=""}[5m]))`},
		{"memory_usage", `sum by (namespace, pod) (container_memory_working_set_bytes{container!=""})`},
	}

	for _, m := range metrics {
		results, err := queryPromInstant(d.promURL, m.query)
		if err != nil {
			log.Debug().Err(err).Str("metric", m.name).Msg("Anomaly query failed")
			continue
		}

		for _, result := range results {
			metric, ok := result["metric"].(map[string]any)
			if !ok {
				continue
			}
			ns, _ := metric["namespace"].(string)
			svc, _ := metric["service"].(string)
			if svc == "" {
				svc, _ = metric["pod"].(string)
			}
			if ns == "" || svc == "" {
				continue
			}

			value, ok := result["value"].([]any)
			if !ok || len(value) < 2 {
				continue
			}
			observed := parsePromValue(value[1])
			if math.IsNaN(observed) || math.IsInf(observed, 0) {
				continue
			}

			key := fmt.Sprintf("%s/%s/%s", ns, svc, m.name)
			d.mu.Lock()
			rs, exists := d.stats[key]
			if !exists {
				rs = &rollingStats{maxLen: 60} // ~5 hours at 5min intervals
				d.stats[key] = rs
			}

			z := rs.zscore(observed)
			expected := rs.mean()
			rs.push(observed)
			d.mu.Unlock()

			// Only alert if we have enough history and z-score is significant
			if len(rs.values) >= 10 && math.Abs(z) > 3.0 {
				severity := "medium"
				if math.Abs(z) > 5.0 {
					severity = "high"
				}
				if math.Abs(z) > 8.0 {
					severity = "critical"
				}

				anomaly := &Anomaly{
					Service:    svc,
					Namespace:  ns,
					Metric:     m.name,
					Score:      math.Round(z*100) / 100,
					Expected:   math.Round(expected*1000) / 1000,
					Observed:   math.Round(observed*1000) / 1000,
					Severity:   severity,
					DetectedAt: time.Now(),
					Status:     "active",
				}

				d.mu.Lock()
				anomaly.ID = d.nextID
				d.nextID++
				d.anomalies[anomaly.ID] = anomaly
				d.mu.Unlock()

				log.Info().Str("service", svc).Str("metric", m.name).Float64("zscore", z).Msg("Anomaly detected")
			}
		}
	}

	// Expire old anomalies (>1h)
	d.mu.Lock()
	for id, a := range d.anomalies {
		if time.Since(a.DetectedAt) > time.Hour {
			a.Status = "resolved"
			delete(d.anomalies, id)
		}
	}
	d.mu.Unlock()
}

// RegisterRoutes adds anomaly API endpoints.
func (d *AnomalyDetector) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/anomalies", d.handleList)
}

func (d *AnomalyDetector) handleList(w http.ResponseWriter, r *http.Request) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var result []*Anomaly
	for _, a := range d.anomalies {
		result = append(result, a)
	}
	sort.Slice(result, func(i, j int) bool {
		return math.Abs(result[i].Score) > math.Abs(result[j].Score)
	})

	writeJSON(w, map[string]any{
		"totalCount": len(result),
		"anomalies":  result,
	})
}
