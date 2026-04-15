package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Signal is a normalized event from any source (logs, LB, k8s events, anomaly).
type Signal struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`   // logs, lb, k8s, anomaly
	Severity  string    `json:"severity"` // critical, high, medium, low
	Service   string    `json:"service,omitempty"`
	Namespace string    `json:"namespace,omitempty"`
	Pod       string    `json:"pod,omitempty"`
	Kind      string    `json:"kind"` // exception, timeout, spike, restart, oom, etc.
	Title     string    `json:"title"`
	Details   string    `json:"details,omitempty"`
}

// Incident represents a cluster of correlated signals.
type Incident struct {
	ID         int64      `json:"id"`
	ClusterID  string     `json:"clusterId"`
	Severity   string     `json:"severity"`
	Status     string     `json:"status"` // open, investigating, resolved, dismissed
	DetectedAt time.Time  `json:"detectedAt"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
	Signals    []Signal   `json:"signals"`
	Affected   []string   `json:"affected"` // service/namespace/pod names
	Summary    string     `json:"summary"`
	RCAReport  *RCAReport `json:"rcaReport,omitempty"`
}

// Correlator clusters incoming signals by topology+time into incidents.
type Correlator struct {
	mu           sync.RWMutex
	incidents    map[int64]*Incident
	nextID       int64
	signalWindow time.Duration
	clusterID    string

	// Callback to trigger RCA when a new incident is created.
	onNewIncident func(incidentID int64)
}

// NewCorrelator creates a correlator with the given time window.
func NewCorrelator(clusterID string, window time.Duration) *Correlator {
	return &Correlator{
		incidents:    make(map[int64]*Incident),
		nextID:       1,
		signalWindow: window,
		clusterID:    clusterID,
	}
}

// IngestSignal processes a signal and either attaches it to an existing
// open incident with matching topology or creates a new one.
func (c *Correlator) IngestSignal(sig Signal) *Incident {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Try to find an existing open incident sharing topology within the time window.
	for _, inc := range c.incidents {
		if inc.Status != "open" && inc.Status != "investigating" {
			continue
		}
		if time.Since(inc.DetectedAt) > c.signalWindow*3 {
			continue // incident too old
		}
		if c.sharesTopology(inc, sig) {
			inc.Signals = append(inc.Signals, sig)
			c.updateSeverity(inc)
			c.updateAffected(inc)
			inc.Summary = c.buildSummary(inc)
			return inc
		}
	}

	// Need at least a signal to create, but we create immediately
	// and let the RCA decide if it's worth investigating.
	inc := &Incident{
		ID:         c.nextID,
		ClusterID:  c.clusterID,
		Severity:   sig.Severity,
		Status:     "open",
		DetectedAt: time.Now(),
		Signals:    []Signal{sig},
		Affected:   []string{},
	}
	c.nextID++
	c.updateAffected(inc)
	inc.Summary = c.buildSummary(inc)
	c.incidents[inc.ID] = inc

	log.Info().Int64("id", inc.ID).Str("severity", inc.Severity).Str("kind", sig.Kind).Msg("New incident created")

	// Fire callback in a goroutine to avoid holding the lock
	if c.onNewIncident != nil {
		go c.onNewIncident(inc.ID)
	}

	return inc
}

func (c *Correlator) sharesTopology(inc *Incident, sig Signal) bool {
	for _, s := range inc.Signals {
		// Same service
		if s.Service != "" && sig.Service != "" && s.Service == sig.Service {
			return true
		}
		// Same namespace + pod
		if s.Namespace != "" && sig.Namespace != "" && s.Namespace == sig.Namespace {
			if s.Pod != "" && sig.Pod != "" && s.Pod == sig.Pod {
				return true
			}
		}
	}
	return false
}

func (c *Correlator) updateSeverity(inc *Incident) {
	order := map[string]int{"critical": 4, "high": 3, "medium": 2, "low": 1}
	maxSev := 0
	for _, s := range inc.Signals {
		if v, ok := order[s.Severity]; ok && v > maxSev {
			maxSev = v
		}
	}
	for k, v := range order {
		if v == maxSev {
			inc.Severity = k
			break
		}
	}
}

func (c *Correlator) updateAffected(inc *Incident) {
	seen := map[string]bool{}
	for _, s := range inc.Signals {
		if s.Service != "" {
			seen[s.Service] = true
		}
		if s.Namespace != "" && s.Pod != "" {
			seen[s.Namespace+"/"+s.Pod] = true
		}
	}
	var list []string
	for k := range seen {
		list = append(list, k)
	}
	sort.Strings(list)
	inc.Affected = list
}

func (c *Correlator) buildSummary(inc *Incident) string {
	kinds := map[string]int{}
	for _, s := range inc.Signals {
		kinds[s.Kind]++
	}
	var parts []string
	for k, v := range kinds {
		parts = append(parts, fmt.Sprintf("%d %s", v, k))
	}
	sort.Strings(parts)
	return fmt.Sprintf("%d signals: %s affecting %s",
		len(inc.Signals), strings.Join(parts, ", "), strings.Join(inc.Affected, ", "))
}

// GetIncident returns an incident by ID.
func (c *Correlator) GetIncident(id int64) *Incident {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.incidents[id]
}

// IncidentsForTarget returns all non-dismissed incidents whose signals
// reference the given namespace and (optionally) service. Used by the
// errors-page context panel (Phase 1.5) to show "what else is broken
// nearby". Pass service=="" to filter by namespace only.
func (c *Correlator) IncidentsForTarget(namespace, service string) []*Incident {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*Incident, 0)
	for _, inc := range c.incidents {
		if inc.Status == "dismissed" {
			continue
		}
		match := false
		for _, sig := range inc.Signals {
			if namespace != "" && sig.Namespace != namespace {
				continue
			}
			if service != "" && sig.Service != service {
				continue
			}
			match = true
			break
		}
		if match {
			out = append(out, inc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DetectedAt.After(out[j].DetectedAt) })
	return out
}

// Evict removes incidents past their TTL and enforces a hard cap on the
// map size. Resolved/dismissed incidents use resolvedTTL (from
// ResolvedAt if set, else DetectedAt); anything older than activeTTL
// is evicted regardless of status. Returns the number of incidents
// removed.
func (c *Correlator) Evict(resolvedTTL, activeTTL time.Duration, maxSize int) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	var removed int
	for id, inc := range c.incidents {
		age := now.Sub(inc.DetectedAt)
		if age > activeTTL {
			delete(c.incidents, id)
			removed++
			continue
		}
		if inc.Status == "resolved" || inc.Status == "dismissed" {
			ref := inc.DetectedAt
			if inc.ResolvedAt != nil {
				ref = *inc.ResolvedAt
			}
			if now.Sub(ref) > resolvedTTL {
				delete(c.incidents, id)
				removed++
			}
		}
	}

	if maxSize > 0 && len(c.incidents) > maxSize {
		type kv struct {
			id int64
			t  time.Time
		}
		entries := make([]kv, 0, len(c.incidents))
		for id, inc := range c.incidents {
			entries = append(entries, kv{id, inc.DetectedAt})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].t.Before(entries[j].t)
		})
		excess := len(c.incidents) - maxSize
		for i := range excess {
			delete(c.incidents, entries[i].id)
			removed++
		}
	}
	return removed
}

// RegisterRoutes adds incident API endpoints.
func (c *Correlator) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/incidents", c.handleListIncidents)
	mux.HandleFunc("GET /api/v1/incidents/{id}", c.handleGetIncident)
	mux.HandleFunc("PATCH /api/v1/incidents/{id}/status", c.handleUpdateStatus)
}

func (c *Correlator) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := r.URL.Query().Get("status")
	var result []*Incident
	for _, inc := range c.incidents {
		if status != "" && inc.Status != status {
			continue
		}
		result = append(result, inc)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].DetectedAt.After(result[j].DetectedAt)
	})

	writeJSON(w, map[string]any{
		"totalCount": len(result),
		"incidents":  result,
	})
}

func (c *Correlator) handleGetIncident(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	c.mu.RLock()
	inc, ok := c.incidents[id]
	c.mu.RUnlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, inc)
}

func (c *Correlator) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
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

	c.mu.Lock()
	inc, ok := c.incidents[id]
	if ok {
		inc.Status = body.Status
		if body.Status == "resolved" {
			now := time.Now()
			inc.ResolvedAt = &now
		}
	}
	c.mu.Unlock()

	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, inc)
}
