package main

import "time"

// LBConfig describes a single load balancer log source.
type LBConfig struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`     // alb | nlb | elb
	Bucket   string `yaml:"bucket"`
	Prefix   string `yaml:"prefix"`
	Region   string `yaml:"region"`
	PollSecs int    `yaml:"pollIntervalSeconds"`
}

// LBRequest is a parsed load balancer access log row.
// Field names match the ClickHouse lb_requests table from PLAN_V7 §5.2.2.
type LBRequest struct {
	Timestamp              time.Time `json:"ts"`
	Cluster                string    `json:"cluster"`
	LBName                 string    `json:"lbName"`
	LBType                 string    `json:"lbType"`
	TargetGroup            string    `json:"targetGroup,omitempty"`
	URLPattern             string    `json:"urlPattern"`
	HTTPMethod             string    `json:"httpMethod"`
	ELBStatus              int       `json:"elbStatus"`
	TargetStatus           int       `json:"targetStatus"`
	ClientIP               string    `json:"clientIp"`
	RequestProcessingMs    float64   `json:"requestProcessingMs"`
	TargetProcessingMs     float64   `json:"targetProcessingMs"`
	ResponseProcessingMs   float64   `json:"responseProcessingMs"`
	TraceID                string    `json:"traceId,omitempty"`
	UserAgent              string    `json:"userAgent,omitempty"`
	RequestURL             string    `json:"requestUrl,omitempty"`
}

// LBSpikeEvent is published to NATS when a traffic anomaly is detected.
type LBSpikeEvent struct {
	LBName      string  `json:"lbName"`
	TargetGroup string  `json:"targetGroup"`
	URLPattern  string  `json:"urlPattern,omitempty"`
	MetricName  string  `json:"metric"`  // 5xx_rate, p99_latency, target_health
	Current     float64 `json:"current"`
	Baseline    float64 `json:"baseline"`
	Timestamp   time.Time `json:"ts"`
}

// LBStats holds aggregated stats for a time window — used by the analyzer API.
type LBStats struct {
	LBName       string  `json:"lbName"`
	LBType       string  `json:"lbType"`
	TotalRequests int64  `json:"totalRequests"`
	Count5xx     int64   `json:"count5xx"`
	Count4xx     int64   `json:"count4xx"`
	Count2xx     int64   `json:"count2xx"`
	P50Ms        float64 `json:"p50Ms"`
	P95Ms        float64 `json:"p95Ms"`
	P99Ms        float64 `json:"p99Ms"`
	AvgMs        float64 `json:"avgMs"`
}

// URLStats holds per-URL-pattern stats.
type URLStats struct {
	URLPattern   string  `json:"urlPattern"`
	HTTPMethod   string  `json:"httpMethod"`
	TotalCount   int64   `json:"totalCount"`
	Count5xx     int64   `json:"count5xx"`
	Count4xx     int64   `json:"count4xx"`
	P95Ms        float64 `json:"p95Ms"`
	P99Ms        float64 `json:"p99Ms"`
}
