package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/rs/zerolog/log"
)

type DNSHealth struct {
	RequestsPerSecond float64 `json:"requestsPerSecond"`
	ErrorRate         float64 `json:"errorRate"`
	AvgLatencyMs      float64 `json:"avgLatencyMs"`
	P99LatencyMs      float64 `json:"p99LatencyMs"`
	CacheHitRate      float64 `json:"cacheHitRate"`
	NxdomainRate      float64 `json:"nxdomainRate"`
	ServfailRate      float64 `json:"servfailRate"`
	ForwardHealthy    bool    `json:"forwardHealthy"`
}

func (c *Collector) getDNSHealth(ctx context.Context) *DNSHealth {
	if c.config.PrometheusEndpoint == "" {
		return nil
	}

	health := &DNSHealth{
		ForwardHealthy: true,
	}

	queries := map[string]string{
		"RequestsPerSecond": `sum(rate(coredns_dns_requests_total[1m]))`,
		"ErrorRate":         `sum(rate(coredns_dns_responses_total{rcode=~"SERVFAIL|NXDOMAIN"}[1m])) / sum(rate(coredns_dns_responses_total[1m]))`,
		"AvgLatencyMs":      `sum(rate(coredns_dns_request_duration_seconds_sum[1m])) / sum(rate(coredns_dns_request_duration_seconds_count[1m])) * 1000`,
		"P99LatencyMs":      `histogram_quantile(0.99, sum(rate(coredns_dns_request_duration_seconds_bucket[1m])) by (le)) * 1000`,
		"CacheHits":         `sum(rate(coredns_cache_hits_total[1m]))`,
		"CacheMisses":       `sum(rate(coredns_cache_misses_total[1m]))`,
		"NxdomainRate":      `sum(rate(coredns_dns_responses_total{rcode="NXDOMAIN"}[1m])) / sum(rate(coredns_dns_responses_total[1m]))`,
		"ServfailRate":      `sum(rate(coredns_dns_responses_total{rcode="SERVFAIL"}[1m])) / sum(rate(coredns_dns_responses_total[1m]))`,
		"ForwardFailures":   `sum(rate(coredns_forward_healthcheck_failures_total[1m]))`,
	}

	results := make(map[string]float64)

	for name, query := range queries {
		encodedQuery := url.QueryEscape(query)
		urlStr := fmt.Sprintf("%s/api/v1/query?query=%s", c.config.PrometheusEndpoint, encodedQuery)

		req, _ := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Debug().Err(err).Str("query", name).Msg("Failed to query prometheus for DNS")
			continue
		}

		var pResp struct {
			Data struct {
				Result []struct {
					Value []interface{} `json:"value"`
				} `json:"result"`
			} `json:"data"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&pResp); err == nil {
			if len(pResp.Data.Result) > 0 && len(pResp.Data.Result[0].Value) == 2 {
				valStr, ok := pResp.Data.Result[0].Value[1].(string)
				if ok && valStr != "NaN" {
					var val float64
					_, _ = fmt.Sscanf(valStr, "%f", &val)
					results[name] = val
				}
			}
		}
		resp.Body.Close()
	}

	health.RequestsPerSecond = results["RequestsPerSecond"]
	health.ErrorRate = results["ErrorRate"]
	health.AvgLatencyMs = results["AvgLatencyMs"]
	health.P99LatencyMs = results["P99LatencyMs"]
	health.NxdomainRate = results["NxdomainRate"]
	health.ServfailRate = results["ServfailRate"]

	if hits, okHits := results["CacheHits"]; okHits {
		if misses, okMisses := results["CacheMisses"]; okMisses {
			total := hits + misses
			if total > 0 {
				health.CacheHitRate = hits / total
			}
		}
	}

	if failures, ok := results["ForwardFailures"]; ok && failures > 0 {
		health.ForwardHealthy = false
	}

	return health
}
