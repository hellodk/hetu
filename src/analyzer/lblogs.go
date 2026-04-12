package main

import (
	"math"
	"net/http"
	"sort"
	"sync"
	"time"
)

// LBLogAggregator collects LB request stats from NATS and serves API queries.
// In-memory for now; ClickHouse-backed in a later phase.
type LBLogAggregator struct {
	mu       sync.RWMutex
	requests map[string][]lbReqSummary // lbName -> recent requests
	configs  []lbInfo
	maxPerLB int
}

type lbReqSummary struct {
	Timestamp     time.Time `json:"ts"`
	URLPattern    string    `json:"urlPattern"`
	HTTPMethod    string    `json:"httpMethod"`
	ELBStatus     int       `json:"elbStatus"`
	TargetStatus  int       `json:"targetStatus"`
	TargetMs      float64   `json:"targetMs"`
	TargetGroup   string    `json:"targetGroup"`
}

type lbInfo struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
}

// NewLBLogAggregator creates an in-memory LB aggregator.
func NewLBLogAggregator() *LBLogAggregator {
	return &LBLogAggregator{
		requests: make(map[string][]lbReqSummary),
		maxPerLB: 10000,
	}
}

// Ingest adds a parsed LB request (from NATS lb.request subject).
func (a *LBLogAggregator) Ingest(lbName, lbType, urlPattern, httpMethod, targetGroup string,
	elbStatus, targetStatus int, targetMs float64, ts time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Register LB if not seen
	found := false
	for _, c := range a.configs {
		if c.Name == lbName {
			found = true
			break
		}
	}
	if !found {
		a.configs = append(a.configs, lbInfo{Name: lbName, Type: lbType})
	}

	reqs := a.requests[lbName]
	reqs = append(reqs, lbReqSummary{
		Timestamp:    ts,
		URLPattern:   urlPattern,
		HTTPMethod:   httpMethod,
		ELBStatus:    elbStatus,
		TargetStatus: targetStatus,
		TargetMs:     targetMs,
		TargetGroup:  targetGroup,
	})
	if len(reqs) > a.maxPerLB {
		reqs = reqs[len(reqs)-a.maxPerLB:]
	}
	a.requests[lbName] = reqs
}

// RegisterRoutes adds LB log API endpoints.
func (a *LBLogAggregator) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/lb/list", a.handleList)
	mux.HandleFunc("GET /api/v1/lb/{name}/stats", a.handleStats)
	mux.HandleFunc("GET /api/v1/lb/{name}/top-urls", a.handleTopURLs)
}

func (a *LBLogAggregator) handleList(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	writeJSON(w, map[string]any{"loadBalancers": a.configs})
}

func (a *LBLogAggregator) handleStats(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	a.mu.RLock()
	defer a.mu.RUnlock()

	reqs, ok := a.requests[name]
	if !ok {
		writeJSON(w, map[string]any{"error": "lb not found"})
		return
	}

	var lbType string
	for _, c := range a.configs {
		if c.Name == name {
			lbType = c.Type
			break
		}
	}

	var total, c2xx, c4xx, c5xx int64
	var latencies []float64
	var sumMs float64

	for _, req := range reqs {
		total++
		status := req.ELBStatus
		if status == 0 {
			status = req.TargetStatus
		}
		switch {
		case status >= 200 && status < 300:
			c2xx++
		case status >= 400 && status < 500:
			c4xx++
		case status >= 500:
			c5xx++
		}
		if req.TargetMs > 0 {
			latencies = append(latencies, req.TargetMs)
			sumMs += req.TargetMs
		}
	}

	sort.Float64s(latencies)

	stats := map[string]any{
		"lbName":        name,
		"lbType":        lbType,
		"totalRequests": total,
		"count2xx":      c2xx,
		"count4xx":      c4xx,
		"count5xx":      c5xx,
		"p50Ms":         percentile(latencies, 0.50),
		"p95Ms":         percentile(latencies, 0.95),
		"p99Ms":         percentile(latencies, 0.99),
		"avgMs":         safeDivide(sumMs, float64(len(latencies))),
	}
	writeJSON(w, stats)
}

func (a *LBLogAggregator) handleTopURLs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	a.mu.RLock()
	defer a.mu.RUnlock()

	reqs, ok := a.requests[name]
	if !ok {
		writeJSON(w, map[string]any{"urls": []any{}})
		return
	}

	type urlKey struct {
		pattern string
		method  string
	}
	type urlAgg struct {
		Pattern  string    `json:"urlPattern"`
		Method   string    `json:"httpMethod"`
		Total    int64     `json:"totalCount"`
		C5xx     int64     `json:"count5xx"`
		C4xx     int64     `json:"count4xx"`
		Latencies []float64 `json:"-"`
		P95Ms    float64   `json:"p95Ms"`
		P99Ms    float64   `json:"p99Ms"`
	}

	byURL := map[urlKey]*urlAgg{}
	for _, req := range reqs {
		k := urlKey{pattern: req.URLPattern, method: req.HTTPMethod}
		agg, ok := byURL[k]
		if !ok {
			agg = &urlAgg{Pattern: k.pattern, Method: k.method}
			byURL[k] = agg
		}
		agg.Total++
		status := req.ELBStatus
		if status == 0 {
			status = req.TargetStatus
		}
		if status >= 500 {
			agg.C5xx++
		} else if status >= 400 {
			agg.C4xx++
		}
		if req.TargetMs > 0 {
			agg.Latencies = append(agg.Latencies, req.TargetMs)
		}
	}

	var urls []*urlAgg
	for _, agg := range byURL {
		sort.Float64s(agg.Latencies)
		agg.P95Ms = percentile(agg.Latencies, 0.95)
		agg.P99Ms = percentile(agg.Latencies, 0.99)
		urls = append(urls, agg)
	}

	// Sort by 5xx count desc, then total desc
	sort.Slice(urls, func(i, j int) bool {
		if urls[i].C5xx != urls[j].C5xx {
			return urls[i].C5xx > urls[j].C5xx
		}
		return urls[i].Total > urls[j].Total
	})

	// Limit to top 50
	if len(urls) > 50 {
		urls = urls[:50]
	}

	writeJSON(w, map[string]any{"urls": urls})
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func safeDivide(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}
