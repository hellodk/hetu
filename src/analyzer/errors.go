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

// RegisterRoutes adds the error aggregator API endpoints.
func (ea *ErrorAggregator) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/errors/groups", ea.handleListGroups)
	mux.HandleFunc("GET /api/v1/errors/groups/{id}", ea.handleGetGroup)
	mux.HandleFunc("PATCH /api/v1/errors/groups/{id}/status", ea.handleUpdateStatus)
	mux.HandleFunc("GET /api/v1/errors/summary", ea.handleSummary)
}

// handleSummary returns aggregated error statistics with per-reason breakdown.
func (ea *ErrorAggregator) handleSummary(w http.ResponseWriter, r *http.Request) {
	ea.mu.RLock()
	defer ea.mu.RUnlock()

	totalGroups := 0
	totalOccurrences := int64(0)
	openCount := 0
	byReason := map[string]int{}
	byNamespace := map[string]int{}
	byService := map[string]int64{}
	var topGroups []map[string]any

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

	// Top 10 groups by count
	type kv struct {
		fp string
		g  *ErrorGroup
	}
	var sorted []kv
	for fp, g := range ea.groups {
		sorted = append(sorted, kv{fp, g})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].g.Count > sorted[j].g.Count })
	for i, s := range sorted {
		if i >= 10 {
			break
		}
		topGroups = append(topGroups, map[string]any{
			"id":        s.g.ID,
			"title":     s.g.Title,
			"reason":    s.g.Reason,
			"service":   s.g.Service,
			"namespace": s.g.Namespace,
			"count":     s.g.Count,
			"lastSeen":  s.g.LastSeen,
			"aiSummary": s.g.AISummary,
		})
	}

	// Top services by error count
	type svcCount struct {
		Service string `json:"service"`
		Count   int64  `json:"count"`
	}
	var topServices []svcCount
	for svc, cnt := range byService {
		topServices = append(topServices, svcCount{svc, cnt})
	}
	sort.Slice(topServices, func(i, j int) bool { return topServices[i].Count > topServices[j].Count })
	if len(topServices) > 10 {
		topServices = topServices[:10]
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

func (ea *ErrorAggregator) handleListGroups(w http.ResponseWriter, r *http.Request) {
	ea.mu.RLock()
	defer ea.mu.RUnlock()

	service := r.URL.Query().Get("service")
	namespace := r.URL.Query().Get("namespace")
	status := r.URL.Query().Get("status")
	search := strings.ToLower(r.URL.Query().Get("search"))

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

	// Sort by last_seen desc
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].LastSeen.After(result[i].LastSeen) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	writeJSON(w, map[string]any{
		"totalCount": len(result),
		"groups":     result,
	})
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
			writeJSON(w, map[string]any{
				"group":       grp,
				"occurrences": occs,
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
