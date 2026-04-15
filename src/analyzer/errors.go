package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
	groups      map[string]*ErrorGroup // fingerprint -> group
	occurrences map[string][]ErrorOccurrence // fingerprint -> recent occurrences (ring buffer)
	nextID      int64
	maxOccur    int // max occurrences per group to keep in memory
}

// NewErrorAggregator creates a new in-memory error aggregator.
func NewErrorAggregator() *ErrorAggregator {
	return &ErrorAggregator{
		groups:      make(map[string]*ErrorGroup),
		occurrences: make(map[string][]ErrorOccurrence),
		nextID:      1,
		maxOccur:    50,
	}
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
		}
		ea.nextID++
		ea.groups[fp] = grp
		log.Info().Str("fingerprint", fp).Str("service", evt.Service).Str("reason", evt.Reason).Msg("New error group")
	}

	grp.Count++
	grp.LastSeen = now
	grp.LastPod = evt.Pod
	if evt.URL != "" {
		grp.LastURL = evt.URL
	}
	if grp.SampleMessage == "" || grp.Count <= 3 {
		grp.SampleMessage = truncate(evt.Message, 500)
	}
	if evt.StackTrace != "" && (grp.SampleStack == "" || grp.Count <= 3) {
		grp.SampleStack = truncate(evt.StackTrace, 2000)
	}
	if evt.Error != "" {
		parts := strings.SplitN(evt.Error, ":", 2)
		grp.ExceptionType = strings.TrimSpace(parts[0])
	}

	// Store occurrence
	occ := ErrorOccurrence{
		Timestamp: now,
		Pod:       evt.Pod,
		Container: evt.Container,
		Message:   truncate(evt.Message, 500),
		URL:       evt.URL,
		RequestID: evt.RequestID,
	}
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
	var removed int
	for fp, g := range ea.groups {
		if now.Sub(g.LastSeen) > ttl {
			delete(ea.groups, fp)
			delete(ea.occurrences, fp)
			removed++
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
		}
	}
	return removed
}

// RegisterRoutes adds the error aggregator API endpoints.
func (ea *ErrorAggregator) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/errors/groups", ea.handleListGroups)
	mux.HandleFunc("GET /api/v1/errors/groups/{id}", ea.handleGetGroup)
	mux.HandleFunc("PATCH /api/v1/errors/groups/{id}/status", ea.handleUpdateStatus)
	mux.HandleFunc("GET /api/v1/errors/summary", ea.handleSummary)
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
			cp := *grp
			cp.Rate = ea.computeRate(grp.Fingerprint)
			writeJSON(w, map[string]any{
				"group":       cp,
				"occurrences": occs,
				// Phase 1.4 prep: signal to the UI that older events beyond
				// maxOccur have been silently dropped from the ring buffer.
				"occurrencesTruncated": len(occs) >= ea.maxOccur,
				"occurrenceCap":        ea.maxOccur,
			})
			return
		}
	}
	http.Error(w, "not found", http.StatusNotFound)
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
