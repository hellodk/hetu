package main

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

// ErrorGroup represents a group of similar log errors (Sentry-style).
// In-memory for now; will be backed by Postgres when store is wired.
type ErrorGroup struct {
	ID            int64             `json:"id"`
	Fingerprint   string            `json:"fingerprint"`
	ClusterID     string            `json:"clusterId"`
	Service       string            `json:"service"`
	Namespace     string            `json:"namespace"`
	Title         string            `json:"title"`
	ExceptionType string            `json:"exceptionType,omitempty"`
	Reason        string            `json:"reason"`
	Level         string            `json:"level"`
	FirstSeen     time.Time         `json:"firstSeen"`
	LastSeen      time.Time         `json:"lastSeen"`
	Count         int64             `json:"count"`
	Status        string            `json:"status"` // open|resolved|ignored
	Tags          map[string]string `json:"tags,omitempty"`
	AISummary     string            `json:"aiSummary,omitempty"`
	LastPod       string            `json:"lastPod,omitempty"`
	LastURL       string            `json:"lastUrl,omitempty"`
	SampleMessage string            `json:"sampleMessage,omitempty"`
	SampleStack   string            `json:"sampleStack,omitempty"`

	// Rate aggregates computed on read from the occurrence ring buffer.
	// Phase 1.1: count-since-forever hides a "50 in 5 min" spike inside a
	// 12 000 all-time group. These are the sparkline inputs.
	Rate *ErrorRate `json:"rate,omitempty"`

	// Phase 2.1: SHA1(exceptionType | top-3 frames) — same root cause
	// across services collides on this key. Used by the cross-service
	// roll-up endpoint and the merge/split UI.
	FaultKey string `json:"faultKey,omitempty"`

	// Phase 2.3: audit trail when groups have been merged together.
	// Each entry is a former group's id+fingerprint that was folded into
	// this one. Survives an analyzer restart only if storage is wired.
	MergedFrom []MergeRef `json:"mergedFrom,omitempty"`

	// Phase 3.1: typed LLM analysis output. Replaces the legacy
	// AISummary markdown blob (kept for backwards-compat during
	// migration). When Analysis is present, the UI prefers it.
	Analysis *ErrorAnalysis `json:"analysis,omitempty"`

	// Phase 2.4: scratchpad for exemplar scoring (NOT serialized — the
	// score is a tiebreaker, not user-facing).
	exemplarScore int `json:"-"`
}

// MergeRef is a breadcrumb pointing at a group that was merged into
// another. The /merge endpoint records the source so an operator can
// later split if the merge was wrong.
type MergeRef struct {
	ID          int64     `json:"id"`
	Fingerprint string    `json:"fingerprint"`
	Service     string    `json:"service,omitempty"`
	MergedAt    time.Time `json:"mergedAt"`
	Count       int64     `json:"count"`
}

// ErrorRate is a lightweight time-bucketed view over the occurrence ring
// buffer. All fields are capped by maxOccur (50) — if the spike exceeds the
// ring, Truncated is true so the UI can annotate "≥ N".
type ErrorRate struct {
	Count1m   int   `json:"count1m"`
	Count5m   int   `json:"count5m"`
	Count1h   int   `json:"count1h"`
	Count24h  int   `json:"count24h"`
	Spark     []int `json:"spark"`     // 12 buckets × 5 min = last 60 min
	Truncated bool  `json:"truncated"` // ring buffer was full in the window
}

// ErrorAnalysis is the typed replacement for AISummary (markdown blob).
// Phase 3.1: each field has its own UI affordance. Severity drives a chip,
// Confidence a bar, Evidence becomes clickable links.
//
// Confidence is *not* the LLM's self-report alone — see computeConfidence
// for the signal weights (Phase 3.3).
type ErrorAnalysis struct {
	RootCause   string              `json:"rootCause"`
	Impact      string              `json:"impact,omitempty"`
	Fix         string              `json:"fix,omitempty"`
	Severity    string              `json:"severity"`   // critical|high|medium|low
	Confidence  float64             `json:"confidence"` // 0..1, signal-derived
	Evidence    []AnalysisEvidence  `json:"evidence,omitempty"`
	Model       string              `json:"model,omitempty"`
	GeneratedAt time.Time           `json:"generatedAt"`
	// Trigger records *why* this analysis ran: "manual" | "newGroup" |
	// "rateSpike" | "umbrellaFault". Helps debug what's auto-running.
	Trigger string `json:"trigger,omitempty"`
}

// AnalysisEvidence links an analysis to something concrete in the rest
// of the system: an open incident, a pod event, an optimizer rec, or a
// representative log line. The UI renders Ref as a link when known.
//
// (Named to avoid collision with rca.go's Evidence, which is a
// different concept — chunks of source data the RCA chain consulted.)
type AnalysisEvidence struct {
	Kind string `json:"kind"` // incident|podEvent|optimizer|log|context
	Ref  string `json:"ref"`
	Note string `json:"note,omitempty"`
}

// faultKey hashes (exception type | top 3 stack frames) without service —
// the same root cause across multiple services produces the same key.
//
// Phase 2.1. Mirrors the producer-side fingerprint helpers in
// collector-podlogs/fingerprint.go but is intentionally narrower: 3 frames
// instead of 5 (the top of the stack drives more grouping than the bottom)
// and no service in the input. Returns "" if there's nothing to hash on.
func faultKey(stack, errorField, reason string) string {
	exType := extractExceptionTypeAnalyzer(errorField, stack, reason)
	if stack == "" && exType == "" {
		return "" // nothing service-independent to derive
	}
	frames := topNFramesAnalyzer(stack, 3)
	h := sha1.Sum([]byte(exType + "|" + frames))
	return fmt.Sprintf("%x", h[:8]) // 16 hex chars
}

// extractExceptionTypeAnalyzer pulls the exception class from common
// log shapes — the "Foo" in "FooException: bar" / "FooError: bar" /
// the first stack frame. Falls back to reason.
func extractExceptionTypeAnalyzer(errorField, stack, reason string) string {
	candidates := []string{errorField}
	if stack != "" {
		if i := strings.IndexByte(stack, '\n'); i > 0 {
			candidates = append(candidates, stack[:i])
		} else {
			candidates = append(candidates, stack)
		}
	}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if i := strings.IndexByte(c, ':'); i > 0 {
			cand := strings.TrimSpace(c[:i])
			if isExceptionLike(cand) {
				return cand
			}
		}
	}
	return reason
}

func isExceptionLike(s string) bool {
	return strings.Contains(s, "Exception") ||
		strings.Contains(s, "Error") ||
		strings.Contains(s, "Panic") ||
		strings.Contains(s, "panic") ||
		strings.Contains(s, "Fault")
}

// topNFramesAnalyzer extracts up to N frame-like lines from a stack
// trace and normalises volatile parts (line numbers, hex addresses).
func topNFramesAnalyzer(stack string, n int) string {
	if stack == "" {
		return ""
	}
	s := reLineNumAnalyzer.ReplaceAllString(stack, ":_")
	s = reHexAddrAnalyzer.ReplaceAllString(s, "0x_")
	out := make([]string, 0, n)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if isFrameLineAnalyzer(line) {
			out = append(out, line)
			if len(out) >= n {
				break
			}
		}
	}
	return strings.Join(out, "\n")
}

func isFrameLineAnalyzer(line string) bool {
	return strings.HasPrefix(line, "at ") ||
		strings.HasPrefix(line, "File \"") ||
		strings.HasPrefix(line, "goroutine ") ||
		strings.Contains(line, ".go:") ||
		strings.Contains(line, ".java:") ||
		strings.Contains(line, ".py:") ||
		strings.Contains(line, ".js:") ||
		strings.Contains(line, ".ts:")
}

var (
	reLineNumAnalyzer = regexp.MustCompile(`:\d+`)
	reHexAddrAnalyzer = regexp.MustCompile(`0x[0-9a-fA-F]+`)
)

// scoreExemplar ranks an occurrence by how *useful* it would be as the
// sample shown to operators. Phase 2.4 — replaces the prior "first 3
// messages win" rule that let an early bad sample dominate forever.
//
// Weights chosen empirically: an event with a stack trace beats anything
// without; with the same stack-presence, a longer message beats a short
// one (more context); a populated URL beats a missing one (request
// context); RequestID is a small bonus.
func scoreExemplar(occ ErrorOccurrence, stackPresent bool) int {
	score := 0
	if stackPresent {
		score += 100
	}
	score += min(len(occ.Message), 500) / 5 // up to +100 for length
	if occ.URL != "" {
		score += 30
	}
	if occ.RequestID != "" {
		score += 10
	}
	return score
}

// computeConfidence (Phase 3.3) — calibrated confidence based on signals
// you can verify, not the LLM's self-report.
//
//   stack-trace present       +0.20
//   seen in ≥3 distinct pods  +0.10
//   correlated incident       +0.20  (passed in, see context handler)
//   LLM self-report           +0.50  (capped via clamp at the end)
//
// Returns 0..1. The LLM term is the largest single weight only because
// without it we have nothing — but the structural signals can carry a
// useful score on their own (max 0.50 with all three checked).
func computeConfidence(hasStack, multiPod, correlatedIncident bool, llmSelfReport float64) float64 {
	c := 0.0
	if hasStack {
		c += 0.20
	}
	if multiPod {
		c += 0.10
	}
	if correlatedIncident {
		c += 0.20
	}
	if llmSelfReport < 0 {
		llmSelfReport = 0
	}
	if llmSelfReport > 1 {
		llmSelfReport = 1
	}
	c += 0.50 * llmSelfReport
	if c > 1 {
		c = 1
	}
	return c
}

// ErrorOccurrence is a single log event belonging to a group.
type ErrorOccurrence struct {
	Timestamp time.Time `json:"timestamp"`
	Pod       string    `json:"pod"`
	Container string    `json:"container"`
	Message   string    `json:"message"`
	URL       string    `json:"url,omitempty"`
	RequestID string    `json:"requestId,omitempty"`
}

// ErrorAggregator receives parsed log events and groups them by fingerprint.
type ErrorAggregator struct {
	mu          sync.RWMutex
	groups      map[string]*ErrorGroup       // fingerprint -> group
	occurrences map[string][]ErrorOccurrence // fingerprint -> recent occurrences (ring buffer)
	nextID      int64
	maxOccur    int // max occurrences per group to keep in memory

	// Optional cross-cutting dependencies. Wired by the analyzer at
	// startup; nil-checked at use-site so unit tests can construct a
	// bare aggregator without standing up the rest of the analyzer.
	correlator *Correlator
	optimizers *OptimizerRegistry

	// Phase 3.2: analyzer hook. When non-nil, called in a goroutine
	// after a new group is created or a rate spike is detected. The
	// trigger string is recorded on ErrorAnalysis.Trigger.
	analyzeFunc func(grp *ErrorGroup, trigger string)
	// guard against re-firing analysis on the same group too often.
	lastTriggered map[string]time.Time

	// Phase 1.6: Prometheus instruments. Optional — when nil, the
	// helpers no-op. Registered by initErrorMetrics() in main.go.
	mIngest   prometheus.Counter
	mEvict    *prometheus.CounterVec
	mActive   prometheus.Gauge
	mLLMLat   prometheus.Histogram
	mLLMSkip  *prometheus.CounterVec
}

// NewErrorAggregator creates a new in-memory error aggregator.
func NewErrorAggregator() *ErrorAggregator {
	return &ErrorAggregator{
		groups:        make(map[string]*ErrorGroup),
		occurrences:   make(map[string][]ErrorOccurrence),
		lastTriggered: make(map[string]time.Time),
		nextID:        1,
		maxOccur:      50,
	}
}

// AttachContext wires sibling subsystems for the context panel handler.
// Both args are optional — a nil correlator/optimizer just means the
// context handler will return an empty list for that kind.
func (ea *ErrorAggregator) AttachContext(c *Correlator, o *OptimizerRegistry) {
	ea.mu.Lock()
	defer ea.mu.Unlock()
	ea.correlator = c
	ea.optimizers = o
}

// AttachAnalyzer wires the LLM-backed analyser hook (Phase 3.2). Called
// async on new-group creation and on rate-spike detection. The hook is
// expected to honour any token-budget gate itself; the aggregator only
// throttles on minTriggerInterval (10 min — see triggerAnalysis).
func (ea *ErrorAggregator) AttachAnalyzer(fn func(grp *ErrorGroup, trigger string)) {
	ea.mu.Lock()
	defer ea.mu.Unlock()
	ea.analyzeFunc = fn
}

// AttachMetrics installs Prometheus instruments (Phase 1.6). Safe to
// call before or after RegisterRoutes; the helpers nil-check.
func (ea *ErrorAggregator) AttachMetrics(reg *prometheus.Registry, namespace string) {
	if reg == nil {
		return
	}
	ea.mu.Lock()
	defer ea.mu.Unlock()

	ns := namespace
	if ns == "" {
		ns = "cluster_intel"
	}
	ea.mIngest = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: ns, Subsystem: "errors",
		Name: "ingest_total", Help: "Total error events ingested into the aggregator",
	})
	ea.mEvict = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Subsystem: "errors",
		Name: "evict_total", Help: "Error groups evicted, by reason",
	}, []string{"reason"})
	ea.mActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: ns, Subsystem: "errors",
		Name: "groups_active", Help: "Current number of active error groups",
	})
	ea.mLLMLat = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: ns, Subsystem: "errors",
		Name: "llm_latency_seconds", Help: "Latency of LLM analysis calls",
		Buckets: prometheus.ExponentialBuckets(0.5, 2, 8),
	})
	ea.mLLMSkip = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Subsystem: "errors",
		Name: "llm_skipped_total", Help: "LLM analyses skipped, by reason",
	}, []string{"reason"})
	reg.MustRegister(ea.mIngest, ea.mEvict, ea.mActive, ea.mLLMLat, ea.mLLMSkip)
}

// ObserveLLMLatency / IncLLMSkipped — exposed for the analyser hook
// closure in main.go to record LLM call observability.
func (ea *ErrorAggregator) ObserveLLMLatency(d time.Duration) {
	if ea.mLLMLat != nil {
		ea.mLLMLat.Observe(d.Seconds())
	}
}
func (ea *ErrorAggregator) IncLLMSkipped(reason string) {
	if ea.mLLMSkip != nil {
		ea.mLLMSkip.WithLabelValues(reason).Inc()
	}
}

// triggerAnalysis runs ea.analyzeFunc in a goroutine, throttled per-fp
// (max once per 10 min for the same group). Caller must hold ea.mu.
func (ea *ErrorAggregator) triggerAnalysis(grp *ErrorGroup, trigger string) {
	if ea.analyzeFunc == nil {
		return
	}
	const minInterval = 10 * time.Minute
	if last, ok := ea.lastTriggered[grp.Fingerprint]; ok && time.Since(last) < minInterval {
		return
	}
	ea.lastTriggered[grp.Fingerprint] = time.Now()
	fn := ea.analyzeFunc
	go fn(grp, trigger)
}

// IngestEvent is a parsed log event from the pod log collector (via NATS).
type IngestEvent struct {
	Timestamp   time.Time `json:"ts"`
	Namespace   string    `json:"namespace"`
	Pod         string    `json:"pod"`
	Container   string    `json:"container"`
	Service     string    `json:"service"`
	Level       string    `json:"level"`
	Message     string    `json:"message"`
	Error       string    `json:"error,omitempty"`
	StackTrace  string    `json:"stackTrace,omitempty"`
	RequestID   string    `json:"requestId,omitempty"`
	URL         string    `json:"url,omitempty"`
	StatusCode  int       `json:"statusCode,omitempty"`
	Reason      string    `json:"reason"`
	Fingerprint string    `json:"fingerprint"`
	Raw         string    `json:"raw"`
}

// Ingest processes a parsed log event and upserts into the appropriate group.
func (ea *ErrorAggregator) Ingest(evt IngestEvent) {
	ea.mu.Lock()
	defer ea.mu.Unlock()

	if ea.mIngest != nil {
		ea.mIngest.Inc()
	}

	fp := evt.Fingerprint
	grp, exists := ea.groups[fp]
	now := time.Now()

	if !exists {
		grp = &ErrorGroup{
			ID:          ea.nextID,
			Fingerprint: fp,
			Service:     evt.Service,
			Namespace:   evt.Namespace,
			Title:       truncate(evt.Message, 200),
			Reason:      evt.Reason,
			Level:       evt.Level,
			FirstSeen:   now,
			LastSeen:    now,
			Count:       0,
			Status:      "open",
			// Phase 2.1: compute the service-less fault key once at
			// group creation. It's stable for the life of the group
			// (the stack/exception don't change after first seen).
			FaultKey: faultKey(evt.StackTrace, evt.Error, evt.Reason),
		}
		ea.nextID++
		ea.groups[fp] = grp
		log.Info().
			Str("fingerprint", fp).
			Str("faultKey", grp.FaultKey).
			Str("service", evt.Service).
			Str("reason", evt.Reason).
			Msg("New error group")
		// Phase 3.2 — auto-analyse first appearance.
		ea.triggerAnalysis(grp, "newGroup")
	}

	grp.Count++
	grp.LastSeen = now
	grp.LastPod = evt.Pod
	if evt.URL != "" {
		grp.LastURL = evt.URL
	}
	if evt.Error != "" {
		parts := strings.SplitN(evt.Error, ":", 2)
		grp.ExceptionType = strings.TrimSpace(parts[0])
	}

	// Phase 2.4 — scored exemplar selection. For the sample message we
	// keep, prefer events with stack traces, longer messages, populated
	// URL/RequestID. The legacy "first 3 messages win" rule let an early
	// uninformative sample dominate forever.
	occ := ErrorOccurrence{
		Timestamp: now,
		Pod:       evt.Pod,
		Container: evt.Container,
		Message:   truncate(evt.Message, 500),
		URL:       evt.URL,
		RequestID: evt.RequestID,
	}
	score := scoreExemplar(occ, evt.StackTrace != "")
	if score > grp.exemplarScore || grp.SampleMessage == "" {
		grp.SampleMessage = truncate(evt.Message, 500)
		if evt.StackTrace != "" {
			grp.SampleStack = truncate(evt.StackTrace, 2000)
		}
		grp.exemplarScore = score
	}

	// Store occurrence (ring buffer)
	occs := ea.occurrences[fp]
	if len(occs) >= ea.maxOccur {
		occs = occs[1:]
	}
	ea.occurrences[fp] = append(occs, occ)
}

// Evict removes error groups whose LastSeen timestamp is older than ttl
// and enforces a hard cap on map size by evicting the least-recently-seen
// groups. Also drops the matching occurrences ring buffer. Returns the
// number of groups removed.
func (ea *ErrorAggregator) Evict(ttl time.Duration, maxSize int) int {
	ea.mu.Lock()
	defer ea.mu.Unlock()

	now := time.Now()
	var removed, ttlRemoved, capRemoved int
	for fp, g := range ea.groups {
		if now.Sub(g.LastSeen) > ttl {
			delete(ea.groups, fp)
			delete(ea.occurrences, fp)
			removed++
			ttlRemoved++
		}
	}

	if maxSize > 0 && len(ea.groups) > maxSize {
		type kv struct {
			fp string
			t  time.Time
		}
		entries := make([]kv, 0, len(ea.groups))
		for fp, g := range ea.groups {
			entries = append(entries, kv{fp, g.LastSeen})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].t.Before(entries[j].t)
		})
		excess := len(ea.groups) - maxSize
		for i := range excess {
			delete(ea.groups, entries[i].fp)
			delete(ea.occurrences, entries[i].fp)
			removed++
			capRemoved++
		}
	}

	if ea.mEvict != nil {
		if ttlRemoved > 0 {
			ea.mEvict.WithLabelValues("ttl").Add(float64(ttlRemoved))
		}
		if capRemoved > 0 {
			ea.mEvict.WithLabelValues("cap").Add(float64(capRemoved))
		}
	}
	if ea.mActive != nil {
		ea.mActive.Set(float64(len(ea.groups)))
	}
	return removed
}

// RegisterRoutes adds the error aggregator API endpoints.
func (ea *ErrorAggregator) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/errors/groups", ea.handleListGroups)
	mux.HandleFunc("GET /api/v1/errors/groups/{id}", ea.handleGetGroup)
	mux.HandleFunc("PATCH /api/v1/errors/groups/{id}/status", ea.handleUpdateStatus)
	mux.HandleFunc("GET /api/v1/errors/summary", ea.handleSummary)
	// Phase 1.5
	mux.HandleFunc("GET /api/v1/errors/groups/{id}/context", ea.handleGroupContext)
	// Phase 2.1
	mux.HandleFunc("GET /api/v1/errors/faults", ea.handleFaultRollup)
	mux.HandleFunc("GET /api/v1/errors/faults/{key}", ea.handleFaultDetail)
	// Phase 2.3
	mux.HandleFunc("POST /api/v1/errors/groups/{id}/merge-into/{target}", ea.handleMerge)
	mux.HandleFunc("POST /api/v1/errors/groups/{id}/split", ea.handleSplit)
}

// handleSummary returns aggregated error statistics with per-reason breakdown.
// Phase 1.3: the old `topGroups[:10]` / `topServices[:10]` hard caps are gone;
// they're now driven by ?limit= (default 10 so existing UI callers keep the
// same visible size, but any consumer can ask for more).
func (ea *ErrorAggregator) handleSummary(w http.ResponseWriter, r *http.Request) {
	ea.mu.RLock()
	defer ea.mu.RUnlock()

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 10 // backwards-compatible default
	}

	totalGroups := 0
	totalOccurrences := int64(0)
	openCount := 0
	byReason := map[string]int{}
	byNamespace := map[string]int{}
	byService := map[string]int64{}

	for _, g := range ea.groups {
		totalGroups++
		totalOccurrences += int64(g.Count)
		if g.Status == "open" {
			openCount++
		}
		byReason[g.Reason]++
		byNamespace[g.Namespace]++
		byService[g.Service] += int64(g.Count)
	}

	// Top-N groups by count
	type kv struct {
		fp string
		g  *ErrorGroup
	}
	var sorted []kv
	for fp, g := range ea.groups {
		sorted = append(sorted, kv{fp, g})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].g.Count > sorted[j].g.Count })
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	topGroups := make([]map[string]any, 0, len(sorted))
	for _, s := range sorted {
		topGroups = append(topGroups, map[string]any{
			"id":        s.g.ID,
			"title":     s.g.Title,
			"reason":    s.g.Reason,
			"service":   s.g.Service,
			"namespace": s.g.Namespace,
			"level":     s.g.Level,
			"count":     s.g.Count,
			"lastSeen":  s.g.LastSeen,
			"aiSummary": s.g.AISummary,
			"rate":      ea.computeRate(s.g.Fingerprint),
		})
	}

	// Top-N services by error count
	type svcCount struct {
		Service string `json:"service"`
		Count   int64  `json:"count"`
	}
	topServices := make([]svcCount, 0, len(byService))
	for svc, cnt := range byService {
		topServices = append(topServices, svcCount{svc, cnt})
	}
	sort.Slice(topServices, func(i, j int) bool { return topServices[i].Count > topServices[j].Count })
	if len(topServices) > limit {
		topServices = topServices[:limit]
	}

	writeJSON(w, map[string]any{
		"totalGroups":      totalGroups,
		"totalOccurrences": totalOccurrences,
		"openCount":        openCount,
		"byReason":         byReason,
		"byNamespace":      byNamespace,
		"topGroups":        topGroups,
		"topServices":      topServices,
	})
}

// computeRate walks the occurrence ring buffer once and produces bucketed
// counts + a 12-bucket sparkline covering the last 60 min (5 min each).
// Occurrences are append-only and chronologically ordered, so a single
// pass is enough. Caller must hold ea.mu at least for read.
func (ea *ErrorAggregator) computeRate(fp string) *ErrorRate {
	occs, ok := ea.occurrences[fp]
	if !ok || len(occs) == 0 {
		return &ErrorRate{Spark: make([]int, 12)}
	}
	now := time.Now()
	r := &ErrorRate{Spark: make([]int, 12)}
	for _, o := range occs {
		age := now.Sub(o.Timestamp)
		if age < time.Minute {
			r.Count1m++
		}
		if age < 5*time.Minute {
			r.Count5m++
		}
		if age < time.Hour {
			r.Count1h++
			// Sparkline: last 60 min in 5-min buckets, newest on the right.
			b := 11 - int(age/(5*time.Minute))
			if b >= 0 && b < 12 {
				r.Spark[b]++
			}
		}
		if age < 24*time.Hour {
			r.Count24h++
		}
	}
	// If the ring buffer was full AND the newest event in it is <24h old,
	// there's no way to know how many older events got dropped. Signal it.
	if len(occs) >= ea.maxOccur && now.Sub(occs[0].Timestamp) < 24*time.Hour {
		r.Truncated = true
	}
	return r
}

func (ea *ErrorAggregator) handleListGroups(w http.ResponseWriter, r *http.Request) {
	ea.mu.RLock()
	defer ea.mu.RUnlock()

	service := r.URL.Query().Get("service")
	namespace := r.URL.Query().Get("namespace")
	status := r.URL.Query().Get("status")
	search := strings.ToLower(r.URL.Query().Get("search"))
	sortBy := r.URL.Query().Get("sort") // lastSeen|count|severity|rate5m
	if sortBy == "" {
		sortBy = "lastSeen"
	}

	// Phase 1.3 — pagination. Default limit 100 (was unbounded list, hardcoded
	// 10 cap on summary). `limit=0` means "all".
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit < 0 {
		limit = 0
	}
	if offset < 0 {
		offset = 0
	}

	var result []*ErrorGroup
	for _, grp := range ea.groups {
		if service != "" && grp.Service != service {
			continue
		}
		if namespace != "" && grp.Namespace != namespace {
			continue
		}
		if status != "" && grp.Status != status {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(grp.Title), search) &&
			!strings.Contains(strings.ToLower(grp.Service), search) {
			continue
		}
		result = append(result, grp)
	}

	// Phase 1.1: attach rate aggregates to every group in the result.
	// Rate is computed (not stored) so it stays current without background
	// work. Cheap: at most maxOccur * len(result) iterations.
	rates := make(map[string]*ErrorRate, len(result))
	for _, g := range result {
		rates[g.Fingerprint] = ea.computeRate(g.Fingerprint)
	}

	// Sort
	sort.Slice(result, func(i, j int) bool {
		switch sortBy {
		case "count":
			return result[i].Count > result[j].Count
		case "severity":
			return severityRank(result[i].Level) > severityRank(result[j].Level)
		case "rate5m":
			return rates[result[i].Fingerprint].Count5m > rates[result[j].Fingerprint].Count5m
		default: // lastSeen
			return result[i].LastSeen.After(result[j].LastSeen)
		}
	})

	total := len(result)

	// Apply pagination before attaching rates to the serialized payload so we
	// only mutate the subset we return.
	if offset >= total {
		result = nil
	} else {
		result = result[offset:]
		if limit > 0 && len(result) > limit {
			result = result[:limit]
		}
	}

	// Attach rate onto the returned groups. We don't mutate the stored
	// ErrorGroup (it would race other readers) — we emit a copy.
	out := make([]ErrorGroup, len(result))
	for i, g := range result {
		cp := *g
		cp.Rate = rates[g.Fingerprint]
		out[i] = cp
	}

	writeJSON(w, map[string]any{
		"totalCount": total,
		"offset":     offset,
		"limit":      limit,
		"sort":       sortBy,
		"groups":     out,
	})
}

// severityRank turns level strings into a numeric order for sorting.
// Unknown levels rank lowest so real signals surface first.
func severityRank(level string) int {
	switch strings.ToLower(level) {
	case "fatal", "panic":
		return 5
	case "error":
		return 4
	case "warn", "warning":
		return 3
	case "info":
		return 2
	case "debug", "trace":
		return 1
	default:
		return 0
	}
}

func (ea *ErrorAggregator) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	ea.mu.RLock()
	defer ea.mu.RUnlock()

	for _, grp := range ea.groups {
		if grp.ID == id {
			occs := ea.occurrences[grp.Fingerprint]

			// Phase 1.4: optional client-side filters on the occurrence
			// list (?from=&to=&pod=&container=&search=). Empty params
			// = no filter. We compute filteredCount separately so the UI
			// can show "showing X of Y".
			from, _ := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
			to, _ := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
			podF := r.URL.Query().Get("pod")
			ctrF := r.URL.Query().Get("container")
			searchF := strings.ToLower(r.URL.Query().Get("search"))

			filtered := occs
			if !from.IsZero() || !to.IsZero() || podF != "" || ctrF != "" || searchF != "" {
				filtered = filtered[:0:0] // new slice, don't aliase
				for _, o := range occs {
					if !from.IsZero() && o.Timestamp.Before(from) {
						continue
					}
					if !to.IsZero() && o.Timestamp.After(to) {
						continue
					}
					if podF != "" && o.Pod != podF {
						continue
					}
					if ctrF != "" && o.Container != ctrF {
						continue
					}
					if searchF != "" && !strings.Contains(strings.ToLower(o.Message), searchF) {
						continue
					}
					filtered = append(filtered, o)
				}
			}

			cp := *grp
			cp.Rate = ea.computeRate(grp.Fingerprint)
			writeJSON(w, map[string]any{
				"group":       cp,
				"occurrences": filtered,
				// Phase 1.4: signal to the UI that older events beyond
				// maxOccur have been silently dropped from the ring buffer.
				// "filteredCount" is the result of post-filtering, "totalCount"
				// is the raw ring-buffer size.
				"filteredCount":        len(filtered),
				"totalCount":           len(occs),
				"occurrencesTruncated": len(occs) >= ea.maxOccur,
				"occurrenceCap":        ea.maxOccur,
			})
			return
		}
	}
	http.Error(w, "not found", http.StatusNotFound)
}

// ----------------------------------------------------------------------------
// Phase 1.5 — context panel
// ----------------------------------------------------------------------------

func (ea *ErrorAggregator) handleGroupContext(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	ea.mu.RLock()
	var grp *ErrorGroup
	for _, g := range ea.groups {
		if g.ID == id {
			grp = g
			break
		}
	}
	corr := ea.correlator
	opts := ea.optimizers
	ea.mu.RUnlock()

	if grp == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Build the panel by fanning out to siblings. Each subsystem may be
	// nil in tests; nil-safety is per-section, not per-handler.
	out := map[string]any{
		"groupId":     grp.ID,
		"namespace":   grp.Namespace,
		"service":     grp.Service,
		"incidents":   []any{},
		"recommendations": []any{},
		"siblings":    []any{}, // groups with same faultKey across services
	}

	if corr != nil {
		incs := corr.IncidentsForTarget(grp.Namespace, grp.Service)
		// Cap to top 10 most recent so the panel doesn't bloat.
		if len(incs) > 10 {
			incs = incs[:10]
		}
		out["incidents"] = incs
	}
	if opts != nil {
		recs := opts.RecsForTarget(grp.Namespace, grp.Service)
		if len(recs) > 10 {
			recs = recs[:10]
		}
		out["recommendations"] = recs
	}

	// Cross-service siblings — same faultKey, different fingerprint.
	if grp.FaultKey != "" {
		ea.mu.RLock()
		var siblings []map[string]any
		for _, g := range ea.groups {
			if g.ID == grp.ID || g.FaultKey != grp.FaultKey {
				continue
			}
			siblings = append(siblings, map[string]any{
				"id":       g.ID,
				"service":  g.Service,
				"namespace": g.Namespace,
				"title":    g.Title,
				"count":    g.Count,
				"lastSeen": g.LastSeen,
			})
		}
		ea.mu.RUnlock()
		out["siblings"] = siblings
	}

	writeJSON(w, out)
}

// ----------------------------------------------------------------------------
// Phase 2.1 — cross-service rollup by faultKey
// ----------------------------------------------------------------------------

func (ea *ErrorAggregator) handleFaultRollup(w http.ResponseWriter, r *http.Request) {
	ea.mu.RLock()
	defer ea.mu.RUnlock()

	type bucket struct {
		FaultKey      string         `json:"faultKey"`
		ExceptionType string         `json:"exceptionType,omitempty"`
		GroupCount    int            `json:"groupCount"`
		TotalCount    int64          `json:"totalCount"`
		Services      []string       `json:"services"`
		Namespaces    []string       `json:"namespaces"`
		LastSeen      time.Time      `json:"lastSeen"`
		Sample        string         `json:"sample,omitempty"`
		GroupIDs      []int64        `json:"groupIds"`
	}
	by := map[string]*bucket{}
	for _, g := range ea.groups {
		k := g.FaultKey
		if k == "" {
			continue
		}
		b, ok := by[k]
		if !ok {
			b = &bucket{FaultKey: k, ExceptionType: g.ExceptionType, Sample: g.SampleMessage}
			by[k] = b
		}
		b.GroupCount++
		b.TotalCount += g.Count
		if !sliceHas(b.Services, g.Service) {
			b.Services = append(b.Services, g.Service)
		}
		if !sliceHas(b.Namespaces, g.Namespace) {
			b.Namespaces = append(b.Namespaces, g.Namespace)
		}
		if g.LastSeen.After(b.LastSeen) {
			b.LastSeen = g.LastSeen
		}
		b.GroupIDs = append(b.GroupIDs, g.ID)
	}
	out := make([]*bucket, 0, len(by))
	for _, b := range by {
		out = append(out, b)
	}
	// Sort by total count desc — biggest faults first
	sort.Slice(out, func(i, j int) bool { return out[i].TotalCount > out[j].TotalCount })

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	writeJSON(w, map[string]any{
		"totalFaults": len(by),
		"faults":      out,
	})
}

func (ea *ErrorAggregator) handleFaultDetail(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	ea.mu.RLock()
	defer ea.mu.RUnlock()
	var groups []*ErrorGroup
	for _, g := range ea.groups {
		if g.FaultKey == key {
			groups = append(groups, g)
		}
	}
	if len(groups) == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].LastSeen.After(groups[j].LastSeen) })
	writeJSON(w, map[string]any{
		"faultKey": key,
		"groups":   groups,
	})
}

func sliceHas(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------------
// Phase 2.3 — manual merge / split
// ----------------------------------------------------------------------------

// handleMerge folds the source group's count + occurrences into the
// target. The source group is *removed* — but a MergeRef is recorded on
// the target so the operator can split it back out if the merge was
// wrong (handleSplit). Idempotent: merging the same source twice is a
// 404 the second time (source no longer exists).
func (ea *ErrorAggregator) handleMerge(w http.ResponseWriter, r *http.Request) {
	srcID, err1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
	tgtID, err2 := strconv.ParseInt(r.PathValue("target"), 10, 64)
	if err1 != nil || err2 != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if srcID == tgtID {
		http.Error(w, "cannot merge into self", http.StatusBadRequest)
		return
	}

	ea.mu.Lock()
	defer ea.mu.Unlock()

	var src, tgt *ErrorGroup
	for _, g := range ea.groups {
		if g.ID == srcID {
			src = g
		}
		if g.ID == tgtID {
			tgt = g
		}
	}
	if src == nil || tgt == nil {
		http.Error(w, "source or target not found", http.StatusNotFound)
		return
	}

	tgt.Count += src.Count
	if src.LastSeen.After(tgt.LastSeen) {
		tgt.LastSeen = src.LastSeen
	}
	if src.FirstSeen.Before(tgt.FirstSeen) {
		tgt.FirstSeen = src.FirstSeen
	}
	tgt.MergedFrom = append(tgt.MergedFrom, MergeRef{
		ID:          src.ID,
		Fingerprint: src.Fingerprint,
		Service:     src.Service,
		MergedAt:    time.Now(),
		Count:       src.Count,
	})

	// Splice in the source's occurrences (newest at the end), capped.
	srcOccs := ea.occurrences[src.Fingerprint]
	combined := append(ea.occurrences[tgt.Fingerprint], srcOccs...)
	sort.Slice(combined, func(i, j int) bool { return combined[i].Timestamp.Before(combined[j].Timestamp) })
	if len(combined) > ea.maxOccur {
		combined = combined[len(combined)-ea.maxOccur:]
	}
	ea.occurrences[tgt.Fingerprint] = combined

	// Remove the source completely.
	delete(ea.groups, src.Fingerprint)
	delete(ea.occurrences, src.Fingerprint)

	log.Info().Int64("source", srcID).Int64("target", tgtID).Msg("Error groups merged")
	writeJSON(w, tgt)
}

// handleSplit removes a MergeRef and re-creates the source as its own
// group. Note: occurrences cannot be perfectly demultiplexed (we lost
// the per-event fingerprint when we merged) — the new group starts
// with zero occurrences but inherits the recorded count.
func (ea *ErrorAggregator) handleSplit(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		MergedFromID int64 `json:"mergedFromId"` // which entry in MergedFrom to extract
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "body must be {mergedFromId: <int>}", http.StatusBadRequest)
		return
	}

	ea.mu.Lock()
	defer ea.mu.Unlock()

	var tgt *ErrorGroup
	for _, g := range ea.groups {
		if g.ID == id {
			tgt = g
			break
		}
	}
	if tgt == nil {
		http.Error(w, "group not found", http.StatusNotFound)
		return
	}

	// Find and remove the MergeRef
	idx := -1
	for i, m := range tgt.MergedFrom {
		if m.ID == body.MergedFromID {
			idx = i
			break
		}
	}
	if idx < 0 {
		http.Error(w, "mergedFromId not found in this group's history", http.StatusNotFound)
		return
	}
	ref := tgt.MergedFrom[idx]
	tgt.MergedFrom = append(tgt.MergedFrom[:idx], tgt.MergedFrom[idx+1:]...)
	tgt.Count -= ref.Count
	if tgt.Count < 0 {
		tgt.Count = 0
	}

	// Re-create the source group with a fresh ID. Keep the original
	// fingerprint so future ingests of that pattern re-attach.
	revived := &ErrorGroup{
		ID:          ea.nextID,
		Fingerprint: ref.Fingerprint,
		Service:     ref.Service,
		Namespace:   tgt.Namespace, // best-effort — original may have differed
		Title:       "(split from #" + strconv.FormatInt(tgt.ID, 10) + ")",
		Reason:      tgt.Reason,
		Level:       tgt.Level,
		FirstSeen:   ref.MergedAt,
		LastSeen:    ref.MergedAt,
		Count:       ref.Count,
		Status:      "open",
	}
	ea.nextID++
	ea.groups[revived.Fingerprint] = revived

	log.Info().Int64("group", id).Int64("revived", revived.ID).Msg("Error group split")
	writeJSON(w, map[string]any{"target": tgt, "revived": revived})
}

func (ea *ErrorAggregator) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.Status != "open" && body.Status != "resolved" && body.Status != "ignored" {
		http.Error(w, "status must be open|resolved|ignored", http.StatusBadRequest)
		return
	}

	ea.mu.Lock()
	defer ea.mu.Unlock()

	for _, grp := range ea.groups {
		if grp.ID == id {
			grp.Status = body.Status
			writeJSON(w, grp)
			return
		}
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
