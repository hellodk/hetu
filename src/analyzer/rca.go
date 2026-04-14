package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/your-org/cluster-intel/pkg/config"
	llmclient "github.com/your-org/cluster-intel/pkg/llm"
	types "github.com/your-org/cluster-intel/pkg/types"
)

// RCAReport holds a structured root cause analysis for an incident.
type RCAReport struct {
	ID            int64     `json:"id"`
	IncidentID    int64     `json:"incidentId"`
	Model         string    `json:"model"`
	PromptTokens  int       `json:"promptTokens"`
	OutputTokens  int       `json:"outputTokens"`
	Confidence    float64   `json:"confidence"`
	Summary       string    `json:"summary"`
	RootCause     RootCause `json:"rootCause"`
	Contributing  []string  `json:"contributingFactors"`
	BlastRadius   BlastRadius `json:"blastRadius"`
	Remediation   []RemediationStep `json:"remediation"`
	Preventive    []string  `json:"preventiveMeasures"`
	Evidence      []Evidence `json:"evidence"`
	CreatedAt     time.Time `json:"createdAt"`
	Raw           string    `json:"raw,omitempty"`
}

type RootCause struct {
	Primary     string  `json:"primary"`
	Confidence  float64 `json:"confidence"`
	Description string  `json:"description"`
}

type BlastRadius struct {
	Services []string `json:"services"`
	Users    string   `json:"users"`
	Severity string   `json:"severity"`
}

type RemediationStep struct {
	Step            string `json:"step"`
	Risk            string `json:"risk"`
	Automatable     bool   `json:"automatable"`
	EstimatedEffort string `json:"estimatedEffort"`
}

type Evidence struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // log, metric, event, lb
	Ref     string `json:"ref"`
	Snippet string `json:"snippet"`
}

// RCAEngine orchestrates LLM-powered root cause analysis.
type RCAEngine struct {
	mu          sync.RWMutex
	reports     map[int64]*RCAReport // incidentID -> latest report
	nextID      int64
	llmClient   *llmclient.Client
	llmConfig   config.LLMConfig
	correlator  *Correlator
	tokensUsed  atomic.Int64
	dailyBudget int64
}

// NewRCAEngine creates an RCA engine with the given LLM client.
func NewRCAEngine(llmCfg config.LLMConfig, correlator *Correlator) *RCAEngine {
	engine := &RCAEngine{
		reports:     make(map[int64]*RCAReport),
		nextID:      1,
		llmConfig:   llmCfg,
		correlator:  correlator,
		dailyBudget: int64(llmCfg.DailyTokenBudget),
	}

	// Only create client if we have a provider configured
	if llmCfg.Provider != "" && llmCfg.Endpoint != "" {
		metrics := llmclient.NewMetrics("cluster_intel")
		engine.llmClient = llmclient.NewClient(llmCfg, metrics)
	}

	return engine
}

// Analyze runs an RCA for the given incident ID.
func (e *RCAEngine) Analyze(ctx context.Context, incidentID int64) (*RCAReport, error) {
	if e.llmClient == nil {
		return nil, fmt.Errorf("LLM not configured")
	}

	// Check daily budget
	if e.dailyBudget > 0 && e.tokensUsed.Load() >= e.dailyBudget {
		return nil, fmt.Errorf("daily token budget exhausted (%d/%d)", e.tokensUsed.Load(), e.dailyBudget)
	}

	inc := e.correlator.GetIncident(incidentID)
	if inc == nil {
		return nil, fmt.Errorf("incident %d not found", incidentID)
	}

	// Build context prompt
	prompt := e.buildPrompt(inc)

	messages := []types.LLMMessage{
		{Role: "system", Content: rcaSystemPrompt},
		{Role: "user", Content: prompt},
	}

	result, err := e.llmClient.Complete(ctx, "rca", messages)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// Track tokens
	e.tokensUsed.Add(int64(result.TotalTokens))

	// Parse structured output
	report, err := e.parseReport(incidentID, result)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to parse RCA response, storing raw")
		report = &RCAReport{
			IncidentID: incidentID,
			Model:      e.llmConfig.Model,
			Summary:    "Failed to parse structured output",
			Raw:        result.Content,
			CreatedAt:  time.Now(),
		}
	}

	e.mu.Lock()
	report.ID = e.nextID
	e.nextID++
	e.reports[incidentID] = report
	e.mu.Unlock()

	// Attach to incident
	e.correlator.mu.Lock()
	if i, ok := e.correlator.incidents[incidentID]; ok {
		i.RCAReport = report
		i.Status = "investigating"
	}
	e.correlator.mu.Unlock()

	log.Info().Int64("incident", incidentID).Str("model", report.Model).
		Int("tokens", report.PromptTokens+report.OutputTokens).Msg("RCA completed")

	return report, nil
}

func (e *RCAEngine) buildPrompt(inc *Incident) string {
	var b strings.Builder

	b.WriteString("## Incident\n")
	fmt.Fprintf(&b, "ID: INC-%d\n", inc.ID)
	fmt.Fprintf(&b, "Severity: %s\n", inc.Severity)
	fmt.Fprintf(&b, "Detected: %s\n", inc.DetectedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "Affected: %s\n\n", strings.Join(inc.Affected, ", "))

	b.WriteString("## Signals (in time order)\n")
	for _, sig := range inc.Signals {
		fmt.Fprintf(&b, "  %s  [%s]  %s/%s  %s: %s\n",
			sig.Timestamp.Format("15:04:05"), sig.Source,
			sig.Namespace, sig.Service, sig.Kind, sig.Title)
		if sig.Details != "" {
			fmt.Fprintf(&b, "    Details: %s\n", truncate(sig.Details, 300))
		}
	}

	b.WriteString("\n## Question\n")
	b.WriteString("What is the most likely root cause? Cite evidence from the signals above.\n")
	b.WriteString("Respond strictly as JSON matching the schema.\n")

	return b.String()
}

func (e *RCAEngine) parseReport(incidentID int64, result *llmclient.CompletionResult) (*RCAReport, error) {
	content := result.Content

	// Try to extract JSON from markdown code blocks
	if idx := strings.Index(content, "```json"); idx >= 0 {
		content = content[idx+7:]
		if end := strings.Index(content, "```"); end >= 0 {
			content = content[:end]
		}
	} else if idx := strings.Index(content, "```"); idx >= 0 {
		content = content[idx+3:]
		if end := strings.Index(content, "```"); end >= 0 {
			content = content[:end]
		}
	}

	content = strings.TrimSpace(content)

	var parsed struct {
		Summary      string `json:"summary"`
		RootCause    struct {
			Primary     string  `json:"primary"`
			Confidence  float64 `json:"confidence"`
			Description string  `json:"description"`
		} `json:"rootCause"`
		ContributingFactors []string `json:"contributingFactors"`
		BlastRadius         struct {
			Services []string `json:"services"`
			Users    string   `json:"users"`
			Severity string   `json:"severity"`
		} `json:"blastRadius"`
		Remediation []struct {
			Step            string `json:"step"`
			Risk            string `json:"risk"`
			Automatable     bool   `json:"automatable"`
			EstimatedEffort string `json:"estimatedEffort"`
		} `json:"remediation"`
		PreventiveMeasures []string `json:"preventiveMeasures"`
		Evidence           []struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Ref     string `json:"ref"`
			Snippet string `json:"snippet"`
		} `json:"evidence"`
	}

	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("json parse: %w", err)
	}

	report := &RCAReport{
		IncidentID:   incidentID,
		Model:        e.llmConfig.Model,
		PromptTokens: result.InputTokens,
		OutputTokens: result.OutputTokens,
		Confidence:   parsed.RootCause.Confidence,
		Summary:      parsed.Summary,
		RootCause: RootCause{
			Primary:     parsed.RootCause.Primary,
			Confidence:  parsed.RootCause.Confidence,
			Description: parsed.RootCause.Description,
		},
		Contributing: parsed.ContributingFactors,
		BlastRadius: BlastRadius{
			Services: parsed.BlastRadius.Services,
			Users:    parsed.BlastRadius.Users,
			Severity: parsed.BlastRadius.Severity,
		},
		Preventive: parsed.PreventiveMeasures,
		CreatedAt:  time.Now(),
		Raw:        result.Content,
	}

	for _, r := range parsed.Remediation {
		report.Remediation = append(report.Remediation, RemediationStep{
			Step:            r.Step,
			Risk:            r.Risk,
			Automatable:     r.Automatable,
			EstimatedEffort: r.EstimatedEffort,
		})
	}
	for _, e := range parsed.Evidence {
		report.Evidence = append(report.Evidence, Evidence{
			ID: e.ID, Type: e.Type, Ref: e.Ref, Snippet: e.Snippet,
		})
	}

	return report, nil
}

// Evict removes RCA reports whose underlying incident no longer exists
// (orphan detection via e.correlator.GetIncident), reports older than
// ttl, and enforces a hard cap on map size. Must run AFTER
// Correlator.Evict so orphan detection works correctly. Returns the
// number of reports removed.
func (e *RCAEngine) Evict(ttl time.Duration, maxSize int) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	var removed int
	for incidentID, report := range e.reports {
		if e.correlator != nil && e.correlator.GetIncident(incidentID) == nil {
			delete(e.reports, incidentID)
			removed++
			continue
		}
		if now.Sub(report.CreatedAt) > ttl {
			delete(e.reports, incidentID)
			removed++
		}
	}

	if maxSize > 0 && len(e.reports) > maxSize {
		type kv struct {
			id int64
			t  time.Time
		}
		entries := make([]kv, 0, len(e.reports))
		for id, r := range e.reports {
			entries = append(entries, kv{id, r.CreatedAt})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].t.Before(entries[j].t)
		})
		excess := len(e.reports) - maxSize
		for i := range excess {
			delete(e.reports, entries[i].id)
			removed++
		}
	}
	return removed
}

// RegisterRoutes adds RCA-specific API endpoints.
func (e *RCAEngine) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/incidents/{id}/rca", e.handleGetRCA)
	mux.HandleFunc("POST /api/v1/incidents/{id}/rca/regenerate", e.handleRegenerate)
	mux.HandleFunc("POST /api/v1/llm/ask", e.handleAsk)
}

func (e *RCAEngine) handleGetRCA(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	e.mu.RLock()
	report, ok := e.reports[id]
	e.mu.RUnlock()
	if !ok {
		http.Error(w, "no RCA report for this incident", http.StatusNotFound)
		return
	}
	writeJSON(w, report)
}

func (e *RCAEngine) handleRegenerate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	report, err := e.Analyze(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, report)
}

func (e *RCAEngine) handleAsk(w http.ResponseWriter, r *http.Request) {
	if e.llmClient == nil {
		http.Error(w, "LLM not configured", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Question   string `json:"question"`
		IncidentID *int64 `json:"incidentId,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var context string
	if body.IncidentID != nil {
		inc := e.correlator.GetIncident(*body.IncidentID)
		if inc != nil {
			context = e.buildPrompt(inc) + "\n\nPrevious RCA:\n"
			if inc.RCAReport != nil {
				context += inc.RCAReport.Summary + "\nRoot cause: " + inc.RCAReport.RootCause.Primary
			}
		}
	}

	messages := []types.LLMMessage{
		{Role: "system", Content: "You are a Kubernetes SRE expert. Answer concisely."},
	}
	if context != "" {
		messages = append(messages, types.LLMMessage{Role: "user", Content: context})
	}
	messages = append(messages, types.LLMMessage{Role: "user", Content: body.Question})

	result, err := e.llmClient.Complete(r.Context(), "ask", messages)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	e.tokensUsed.Add(int64(result.TotalTokens))
	writeJSON(w, map[string]any{
		"answer":      result.Content,
		"tokensUsed":  result.TotalTokens,
		"model":       e.llmConfig.Model,
	})
}

const rcaSystemPrompt = `You are a Kubernetes SRE expert analyzing cluster incidents. You have deep knowledge of:
- Kubernetes internals (scheduler, kubelet, controllers)
- Container runtime behavior
- Network policies and service mesh
- Resource management and QoS classes
- Common failure patterns and anti-patterns

Analyze incidents systematically:
1. Identify the primary symptom
2. Trace the causal chain
3. Consider contributing factors
4. Assess blast radius
5. Prioritize recommendations

Always provide confidence scores (0.0-1.0) for your assessments.

Respond ONLY with valid JSON matching this schema:
{
  "summary": "one-sentence summary",
  "rootCause": {"primary": "...", "confidence": 0.0, "description": "..."},
  "contributingFactors": ["..."],
  "blastRadius": {"services": [], "users": "...", "severity": "high|medium|low"},
  "remediation": [{"step": "...", "risk": "low|medium|high", "automatable": false, "estimatedEffort": "minutes|hours|days"}],
  "preventiveMeasures": ["..."],
  "evidence": [{"id": "ev-1", "type": "log|metric|event|lb", "ref": "...", "snippet": "..."}]
}`
