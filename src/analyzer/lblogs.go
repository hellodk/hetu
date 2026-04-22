package main

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LBLogAggregator collects LB request stats from NATS and serves API queries.
// In-memory for now; ClickHouse-backed in a later phase.
type LBLogAggregator struct {
	mu             sync.RWMutex
	requests       map[string][]lbReqSummary // lbName -> recent requests
	configs        []lbInfo                  // observed via NATS ingestion
	desiredConfigs []lbDesiredConfig         // user-configured (from settings UI)
	maxPerLB       int
}

// lbDesiredConfig is a user-supplied load-balancer source definition.
// It maps to the LB_CONFIGS env-var format consumed by the collector-lblogs service.
type lbDesiredConfig struct {
	Name                string `json:"name"`
	Type                string `json:"type"`   // alb | nlb | elb
	Bucket              string `json:"bucket"` // S3 bucket name
	Prefix              string `json:"prefix,omitempty"`
	Region              string `json:"region"`
	PollIntervalSeconds int    `json:"pollIntervalSeconds,omitempty"`
}

type lbReqSummary struct {
	Timestamp    time.Time `json:"ts"`
	URLPattern   string    `json:"urlPattern"`
	HTTPMethod   string    `json:"httpMethod"`
	ELBStatus    int       `json:"elbStatus"`
	TargetStatus int       `json:"targetStatus"`
	TargetMs     float64   `json:"targetMs"`
	TargetGroup  string    `json:"targetGroup"`
	ClientIP     string    `json:"clientIp,omitempty"`
}

type lbInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
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

	a.ingestLocked(lbName, urlPattern, httpMethod, targetGroup, "", elbStatus, targetStatus, targetMs, ts)
}

// IngestWithClient is like Ingest but also records the client IP.
func (a *LBLogAggregator) IngestWithClient(lbName, lbType, urlPattern, httpMethod, targetGroup, clientIP string,
	elbStatus, targetStatus int, targetMs float64, ts time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()

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

	a.ingestLocked(lbName, urlPattern, httpMethod, targetGroup, clientIP, elbStatus, targetStatus, targetMs, ts)
}

func (a *LBLogAggregator) ingestLocked(lbName, urlPattern, httpMethod, targetGroup, clientIP string,
	elbStatus, targetStatus int, targetMs float64, ts time.Time) {
	reqs := a.requests[lbName]
	reqs = append(reqs, lbReqSummary{
		Timestamp:    ts,
		URLPattern:   urlPattern,
		HTTPMethod:   httpMethod,
		ELBStatus:    elbStatus,
		TargetStatus: targetStatus,
		TargetMs:     targetMs,
		TargetGroup:  targetGroup,
		ClientIP:     clientIP,
	})
	if len(reqs) > a.maxPerLB {
		reqs = reqs[len(reqs)-a.maxPerLB:]
	}
	a.requests[lbName] = reqs
}

// RegisterRoutes adds LB log API endpoints.
func (a *LBLogAggregator) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/lb/list", a.handleList)
	mux.HandleFunc("GET /api/v1/lb/config", a.handleGetConfig)
	mux.HandleFunc("PUT /api/v1/lb/config", a.handleSetConfig)
	mux.HandleFunc("GET /api/v1/lb/{name}/stats", a.handleStats)
	mux.HandleFunc("GET /api/v1/lb/{name}/top-urls", a.handleTopURLs)
	mux.HandleFunc("GET /api/v1/lb/{name}/timeline", a.handleTimeline)
	mux.HandleFunc("GET /api/v1/lb/{name}/errors", a.handleErrors)
	mux.HandleFunc("GET /api/v1/lb/{name}/slow", a.handleSlow)
	mux.HandleFunc("GET /api/v1/lb/{name}/clients", a.handleClients)
	mux.HandleFunc("GET /api/v1/lb/{name}/search", a.handleSearch)
	mux.HandleFunc("POST /api/v1/lb/ingest", a.handleIngest)
}

func (a *LBLogAggregator) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	cfgs := a.desiredConfigs
	if cfgs == nil {
		cfgs = []lbDesiredConfig{}
	}
	writeJSON(w, map[string]any{"configs": cfgs})
}

func (a *LBLogAggregator) handleSetConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Configs []lbDesiredConfig `json:"configs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	a.desiredConfigs = body.Configs
	a.mu.Unlock()
	a.mu.RLock()
	defer a.mu.RUnlock()
	writeJSON(w, map[string]any{"configs": a.desiredConfigs})
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
		Pattern   string    `json:"urlPattern"`
		Method    string    `json:"httpMethod"`
		Total     int64     `json:"totalCount"`
		C5xx      int64     `json:"count5xx"`
		C4xx      int64     `json:"count4xx"`
		Latencies []float64 `json:"-"`
		P95Ms     float64   `json:"p95Ms"`
		P99Ms     float64   `json:"p99Ms"`
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

// =========================================================================
// Enhanced analytics endpoints
// =========================================================================

func getMinutes(r *http.Request) int {
	if v := r.URL.Query().Get("minutes"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 60
}

func getLimit(r *http.Request, def int) int {
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func statusClass(status int) string {
	if status == 0 {
		return "unknown"
	}
	return strconv.Itoa(status/100) + "xx"
}

// handleTimeline returns per-minute request count + error rate + latency percentiles.
func (a *LBLogAggregator) handleTimeline(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	minutes := getMinutes(r)

	a.mu.RLock()
	defer a.mu.RUnlock()

	reqs, ok := a.requests[name]
	if !ok {
		writeJSON(w, map[string]any{"buckets": []any{}})
		return
	}

	cutoff := time.Now().Add(-time.Duration(minutes) * time.Minute)

	type bucket struct {
		Minute    string  `json:"minute"`
		Total     int64   `json:"total"`
		C2xx      int64   `json:"count2xx"`
		C4xx      int64   `json:"count4xx"`
		C5xx      int64   `json:"count5xx"`
		ErrorRate float64 `json:"errorRate"`
		P50Ms     float64 `json:"p50Ms"`
		P95Ms     float64 `json:"p95Ms"`
	}

	byMinute := map[string]*struct {
		b         bucket
		latencies []float64
	}{}

	for _, req := range reqs {
		if req.Timestamp.Before(cutoff) {
			continue
		}
		key := req.Timestamp.Truncate(time.Minute).Format("15:04")
		entry, ok := byMinute[key]
		if !ok {
			entry = &struct {
				b         bucket
				latencies []float64
			}{b: bucket{Minute: key}}
			byMinute[key] = entry
		}
		entry.b.Total++
		status := req.ELBStatus
		if status == 0 {
			status = req.TargetStatus
		}
		switch {
		case status >= 200 && status < 300:
			entry.b.C2xx++
		case status >= 400 && status < 500:
			entry.b.C4xx++
		case status >= 500:
			entry.b.C5xx++
		}
		if req.TargetMs > 0 {
			entry.latencies = append(entry.latencies, req.TargetMs)
		}
	}

	var buckets []bucket
	for _, entry := range byMinute {
		sort.Float64s(entry.latencies)
		entry.b.P50Ms = percentile(entry.latencies, 0.50)
		entry.b.P95Ms = percentile(entry.latencies, 0.95)
		if entry.b.Total > 0 {
			entry.b.ErrorRate = float64(entry.b.C5xx) / float64(entry.b.Total) * 100
		}
		buckets = append(buckets, entry.b)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Minute < buckets[j].Minute })

	writeJSON(w, map[string]any{"buckets": buckets})
}

// handleErrors returns per-URL error breakdown over time.
func (a *LBLogAggregator) handleErrors(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	minutes := getMinutes(r)

	a.mu.RLock()
	defer a.mu.RUnlock()

	reqs, ok := a.requests[name]
	if !ok {
		writeJSON(w, map[string]any{"errors": []any{}})
		return
	}

	cutoff := time.Now().Add(-time.Duration(minutes) * time.Minute)

	type urlError struct {
		URLPattern string  `json:"urlPattern"`
		HTTPMethod string  `json:"httpMethod"`
		Total      int64   `json:"total"`
		C5xx       int64   `json:"count5xx"`
		C4xx       int64   `json:"count4xx"`
		ErrorRate  float64 `json:"errorRate"`
	}

	type urlKey struct{ pattern, method string }
	byURL := map[urlKey]*urlError{}

	for _, req := range reqs {
		if req.Timestamp.Before(cutoff) {
			continue
		}
		status := req.ELBStatus
		if status == 0 {
			status = req.TargetStatus
		}
		if status < 400 {
			continue
		}
		k := urlKey{req.URLPattern, req.HTTPMethod}
		entry, ok := byURL[k]
		if !ok {
			entry = &urlError{URLPattern: req.URLPattern, HTTPMethod: req.HTTPMethod}
			byURL[k] = entry
		}
		entry.Total++
		if status >= 500 {
			entry.C5xx++
		} else {
			entry.C4xx++
		}
	}

	var errors []urlError
	for _, e := range byURL {
		e.ErrorRate = float64(e.C5xx) / math.Max(float64(e.Total), 1) * 100
		errors = append(errors, *e)
	}
	sort.Slice(errors, func(i, j int) bool { return errors[i].C5xx > errors[j].C5xx })
	if len(errors) > 50 {
		errors = errors[:50]
	}

	writeJSON(w, map[string]any{"errors": errors})
}

// handleSlow returns the slowest requests.
func (a *LBLogAggregator) handleSlow(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	limit := getLimit(r, 50)

	a.mu.RLock()
	defer a.mu.RUnlock()

	reqs, ok := a.requests[name]
	if !ok {
		writeJSON(w, map[string]any{"requests": []any{}})
		return
	}

	// Copy and sort by latency desc
	sorted := make([]lbReqSummary, len(reqs))
	copy(sorted, reqs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].TargetMs > sorted[j].TargetMs })
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}

	writeJSON(w, map[string]any{"requests": sorted})
}

// handleClients returns top client IPs by request count.
func (a *LBLogAggregator) handleClients(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	limit := getLimit(r, 50)

	a.mu.RLock()
	defer a.mu.RUnlock()

	reqs, ok := a.requests[name]
	if !ok {
		writeJSON(w, map[string]any{"clients": []any{}})
		return
	}

	type clientStats struct {
		IP       string    `json:"ip"`
		Count    int64     `json:"count"`
		C5xx     int64     `json:"count5xx"`
		LastSeen time.Time `json:"lastSeen"`
	}

	byIP := map[string]*clientStats{}
	for _, req := range reqs {
		if req.ClientIP == "" {
			continue
		}
		entry, ok := byIP[req.ClientIP]
		if !ok {
			entry = &clientStats{IP: req.ClientIP}
			byIP[req.ClientIP] = entry
		}
		entry.Count++
		status := req.ELBStatus
		if status == 0 {
			status = req.TargetStatus
		}
		if status >= 500 {
			entry.C5xx++
		}
		if req.Timestamp.After(entry.LastSeen) {
			entry.LastSeen = req.Timestamp
		}
	}

	var clients []clientStats
	for _, c := range byIP {
		clients = append(clients, *c)
	}
	sort.Slice(clients, func(i, j int) bool { return clients[i].Count > clients[j].Count })
	if len(clients) > limit {
		clients = clients[:limit]
	}

	writeJSON(w, map[string]any{"clients": clients})
}

// handleSearch returns filtered requests matching query params.
func (a *LBLogAggregator) handleSearch(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	limit := getLimit(r, 100)
	statusFilter := r.URL.Query().Get("status") // "2xx", "4xx", "5xx"
	urlFilter := r.URL.Query().Get("url")
	minLatencyStr := r.URL.Query().Get("min_latency")
	var minLatency float64
	if minLatencyStr != "" {
		minLatency, _ = strconv.ParseFloat(minLatencyStr, 64)
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	reqs, ok := a.requests[name]
	if !ok {
		writeJSON(w, map[string]any{"requests": []any{}, "total": 0})
		return
	}

	var result []lbReqSummary
	for i := len(reqs) - 1; i >= 0 && len(result) < limit; i-- {
		req := reqs[i]
		status := req.ELBStatus
		if status == 0 {
			status = req.TargetStatus
		}

		if statusFilter != "" && statusClass(status) != statusFilter {
			continue
		}
		if urlFilter != "" && !strings.Contains(strings.ToLower(req.URLPattern), strings.ToLower(urlFilter)) {
			continue
		}
		if minLatency > 0 && req.TargetMs < minLatency {
			continue
		}

		result = append(result, req)
	}

	writeJSON(w, map[string]any{"requests": result, "total": len(result)})
}

// handleIngest accepts batches of LB requests from the collector's HTTP
// fallback mode (when NATS is unavailable).
func (a *LBLogAggregator) handleIngest(w http.ResponseWriter, r *http.Request) {
	var batch []struct {
		LBName       string  `json:"lbName"`
		LBType       string  `json:"lbType"`
		URLPattern   string  `json:"urlPattern"`
		HTTPMethod   string  `json:"httpMethod"`
		TargetGroup  string  `json:"targetGroup"`
		ELBStatus    int     `json:"elbStatus"`
		TargetStatus int     `json:"targetStatus"`
		TargetMs     float64 `json:"targetMs"`
		ClientIP     string  `json:"clientIp"`
		Timestamp    string  `json:"timestamp"`
	}

	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	for _, req := range batch {
		ts, _ := time.Parse(time.RFC3339Nano, req.Timestamp)
		if ts.IsZero() {
			ts = time.Now()
		}
		a.Ingest(req.LBName, req.LBType, req.URLPattern, req.HTTPMethod,
			req.TargetGroup, req.ELBStatus, req.TargetStatus, req.TargetMs, ts)
	}

	writeJSON(w, map[string]any{"accepted": len(batch)})
}
