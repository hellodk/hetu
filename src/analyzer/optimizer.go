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

// Recommendation represents an optimization suggestion.
type OptRecommendation struct {
	ID                      int64          `json:"id"`
	Type                    string         `json:"type"` // rightsizing, hpa, coredns, gc, cluster, scaling, security
	Severity                string         `json:"severity"`
	Confidence              float64        `json:"confidence"`
	Target                  OptTarget      `json:"target"`
	CurrentState            map[string]any `json:"currentState,omitempty"`
	SuggestedState          map[string]any `json:"suggestedState,omitempty"`
	Rationale               string         `json:"rationale"`
	AIExplanation           string         `json:"aiExplanation,omitempty"`
	EstimatedSavingsMonthly float64        `json:"estimatedSavingsMonthly,omitempty"`
	Status                  string         `json:"status"` // open, accepted, dismissed, applied
	YAMLPatch               string         `json:"yamlPatch,omitempty"`
	CreatedAt               time.Time      `json:"createdAt"`
}

// OptTarget identifies the K8s resource a recommendation applies to.
type OptTarget struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Container string `json:"container,omitempty"`
}

// Optimizer is the interface all optimization modules implement.
type Optimizer interface {
	Name() string
	Run(ctx OptimizerContext) ([]OptRecommendation, error)
}

// OptimizerContext provides shared dependencies to optimizers.
type OptimizerContext struct {
	PrometheusURL string
	ClusterID     string
}

// OptimizerRegistry manages and runs optimization modules.
type OptimizerRegistry struct {
	mu              sync.RWMutex
	optimizers      map[string]Optimizer
	recommendations map[int64]*OptRecommendation
	nextID          int64
	promURL         string
	clusterID       string
}

// NewOptimizerRegistry creates a new registry.
func NewOptimizerRegistry(promURL, clusterID string) *OptimizerRegistry {
	reg := &OptimizerRegistry{
		optimizers:      make(map[string]Optimizer),
		recommendations: make(map[int64]*OptRecommendation),
		nextID:          1,
		promURL:         promURL,
		clusterID:       clusterID,
	}
	// Register built-in optimizers
	reg.Register(&RightSizingOptimizer{})
	reg.Register(&HPAOptimizer{})
	reg.Register(&CoreDNSOptimizer{})
	reg.Register(&GCOptimizer{})
	reg.Register(&ClusterOptimizer{})
	return reg
}

// RecsForTarget returns open recommendations whose Target relates to
// the given namespace + service. Used by the errors-page context panel.
//
// Audit item #5 — the previous implementation only used
// strings.HasPrefix(Target.Name, service), which misses a lot of legit
// pairings (e.g. Deployment "web" owning pods named "web-7fc4b...,"
// whose pods are labelled service=api-gateway). We now match in this
// priority order and return results tagged with match strength:
//
//  1. exact: rec.Target.Name == service
//  2. container match: rec.Target.Container == service
//  3. prefix: rec.Target.Name HasPrefix service  (strict — avoids
//     false-positives where service is a substring inside the name)
//  4. namespace-only (only returned when no name/container matches at all)
//
// Results are sorted by match strength (1 → 4) so callers can cut off.
// Recommendations with Status != "open" are skipped.
func (r *OptimizerRegistry) RecsForTarget(namespace, service string) []*OptRecommendation {
	r.mu.RLock()
	defer r.mu.RUnlock()

	type scored struct {
		rec   *OptRecommendation
		score int // 1 = best, 4 = weakest
	}
	var exact, container, prefix, nsOnly []scored
	for _, rec := range r.recommendations {
		if rec.Status != "open" {
			continue
		}
		if namespace != "" && rec.Target.Namespace != namespace {
			continue
		}
		if service == "" {
			nsOnly = append(nsOnly, scored{rec, 4})
			continue
		}
		switch {
		case rec.Target.Name == service:
			exact = append(exact, scored{rec, 1})
		case rec.Target.Container != "" && rec.Target.Container == service:
			container = append(container, scored{rec, 2})
		case rec.Target.Name != "" && strings.HasPrefix(rec.Target.Name, service+"-"):
			// Require a "-" suffix to avoid "api" matching "apiserver".
			// Bare HasPrefix without the dash was the original footgun.
			prefix = append(prefix, scored{rec, 3})
		}
	}
	// Namespace-only fallback: return these ONLY when we have nothing
	// strong. Prevents the panel from flooding with unrelated recs.
	combined := append(append(append([]scored{}, exact...), container...), prefix...)
	if len(combined) == 0 {
		combined = nsOnly
	}
	out := make([]*OptRecommendation, 0, len(combined))
	for _, s := range combined {
		out = append(out, s.rec)
	}
	return out
}

// Register adds an optimizer module.
func (r *OptimizerRegistry) Register(opt Optimizer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.optimizers[opt.Name()] = opt
	log.Info().Str("optimizer", opt.Name()).Msg("Registered optimizer")
}

// RunAll executes all registered optimizers.
func (r *OptimizerRegistry) RunAll() {
	r.mu.RLock()
	names := make([]string, 0, len(r.optimizers))
	for name := range r.optimizers {
		names = append(names, name)
	}
	r.mu.RUnlock()

	for _, name := range names {
		r.RunOne(name)
	}
}

// RunOne executes a single optimizer by name.
func (r *OptimizerRegistry) RunOne(name string) {
	r.mu.RLock()
	opt, ok := r.optimizers[name]
	r.mu.RUnlock()
	if !ok {
		log.Warn().Str("optimizer", name).Msg("Optimizer not found")
		return
	}

	ctx := OptimizerContext{
		PrometheusURL: r.promURL,
		ClusterID:     r.clusterID,
	}

	start := time.Now()
	recs, err := opt.Run(ctx)
	if err != nil {
		log.Error().Err(err).Str("optimizer", name).Msg("Optimizer failed")
		return
	}

	r.mu.Lock()
	// Clear previous recommendations of the same type to avoid duplicates
	// across scan cycles. Keep non-open (accepted/dismissed/applied) recs.
	for id, existing := range r.recommendations {
		if existing.Type == name && existing.Status == "open" {
			delete(r.recommendations, id)
		}
	}
	for i := range recs {
		recs[i].ID = r.nextID
		recs[i].CreatedAt = time.Now()
		if recs[i].Status == "" {
			recs[i].Status = "open"
		}
		r.recommendations[r.nextID] = &recs[i]
		r.nextID++
	}
	r.mu.Unlock()

	log.Info().Str("optimizer", name).Int("recs", len(recs)).Dur("elapsed", time.Since(start)).Msg("Optimizer completed")
}

// Evict removes closed (accepted/dismissed/applied) recommendations older
// than ttl and enforces a hard cap on map size. When capping, non-open
// recommendations are evicted first. Returns the number of recs removed.
func (r *OptimizerRegistry) Evict(ttl time.Duration, maxSize int) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	var removed int
	closed := func(s string) bool {
		return s == "accepted" || s == "dismissed" || s == "applied"
	}
	for id, rec := range r.recommendations {
		if closed(rec.Status) && now.Sub(rec.CreatedAt) > ttl {
			delete(r.recommendations, id)
			removed++
		}
	}

	if maxSize > 0 && len(r.recommendations) > maxSize {
		type kv struct {
			id     int64
			closed bool
			t      time.Time
		}
		entries := make([]kv, 0, len(r.recommendations))
		for id, rec := range r.recommendations {
			entries = append(entries, kv{id, closed(rec.Status), rec.CreatedAt})
		}
		// Evict closed recs first, then open ones; within each group, oldest first.
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].closed != entries[j].closed {
				return entries[i].closed
			}
			return entries[i].t.Before(entries[j].t)
		})
		excess := len(r.recommendations) - maxSize
		for i := range excess {
			delete(r.recommendations, entries[i].id)
			removed++
		}
	}
	return removed
}

// RegisterRoutes adds optimization API endpoints.
func (r *OptimizerRegistry) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/recommendations", r.handleList)
	mux.HandleFunc("POST /api/v1/recommendations/run", r.handleRun)
	mux.HandleFunc("PATCH /api/v1/recommendations/{id}/status", r.handleUpdateStatus)
	mux.HandleFunc("GET /api/v1/recommendations/summary", r.handleSummary)
}

func (r *OptimizerRegistry) handleList(w http.ResponseWriter, req *http.Request) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	typeFilter := req.URL.Query().Get("type")
	status := req.URL.Query().Get("status")

	var result []*OptRecommendation
	for _, rec := range r.recommendations {
		if typeFilter != "" && rec.Type != typeFilter {
			continue
		}
		if status != "" && rec.Status != status {
			continue
		}
		result = append(result, rec)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].EstimatedSavingsMonthly > result[j].EstimatedSavingsMonthly
	})

	writeJSON(w, map[string]any{
		"totalCount":      len(result),
		"recommendations": result,
	})
}

func (r *OptimizerRegistry) handleRun(w http.ResponseWriter, req *http.Request) {
	typeName := req.URL.Query().Get("type")
	if typeName != "" {
		r.RunOne(typeName)
	} else {
		r.RunAll()
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (r *OptimizerRegistry) handleUpdateStatus(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	r.mu.Lock()
	rec, ok := r.recommendations[id]
	if ok {
		rec.Status = body.Status
	}
	r.mu.Unlock()

	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, rec)
}

func (r *OptimizerRegistry) handleSummary(w http.ResponseWriter, req *http.Request) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	byType := map[string]int{}
	totalSavings := 0.0
	openCount := 0
	for _, rec := range r.recommendations {
		byType[rec.Type]++
		if rec.Status == "open" {
			openCount++
			totalSavings += rec.EstimatedSavingsMonthly
		}
	}

	types := make([]string, 0)
	for name := range r.optimizers {
		types = append(types, name)
	}
	sort.Strings(types)

	writeJSON(w, map[string]any{
		"totalRecommendations": len(r.recommendations),
		"openRecommendations":  openCount,
		"totalSavingsMonthly":  totalSavings,
		"byType":               byType,
		"availableOptimizers":  types,
	})
}

// --- Helper to parse prometheus results (shared by optimizers) ---

func parsePromValue(v any) float64 {
	switch val := v.(type) {
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	case float64:
		return val
	default:
		return 0
	}
}

func queryPromInstant(promURL, query string) ([]map[string]any, error) {
	if promURL == "" {
		return nil, nil
	}
	resp, err := http.Get(promURL + "/api/v1/query?query=" + strings.ReplaceAll(query, " ", "+"))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data struct {
			Result []map[string]any `json:"result"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Data.Result, nil
}
