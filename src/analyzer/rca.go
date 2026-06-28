package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/hellodk/hetu/pkg/config"
	llmclient "github.com/hellodk/hetu/pkg/llm"
	types "github.com/hellodk/hetu/pkg/types"
)

// RCAReport holds a structured root cause analysis for an incident.
type RCAReport struct {
	ID           int64             `json:"id"`
	IncidentID   int64             `json:"incidentId"`
	Model        string            `json:"model"`
	PromptTokens int               `json:"promptTokens"`
	OutputTokens int               `json:"outputTokens"`
	Confidence   float64           `json:"confidence"`
	Summary      string            `json:"summary"`
	RootCause    RootCause         `json:"rootCause"`
	Contributing []string          `json:"contributingFactors"`
	BlastRadius  BlastRadius       `json:"blastRadius"`
	Remediation  []RemediationStep `json:"remediation"`
	Preventive   []string          `json:"preventiveMeasures"`
	Evidence     []Evidence        `json:"evidence"`
	CreatedAt    time.Time         `json:"createdAt"`
	Raw          string            `json:"raw,omitempty"`
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
	mu        sync.RWMutex
	reports   map[int64]*RCAReport // incidentID -> latest report
	nextID    int64
	llmClient *llmclient.Client
	// metrics is created once (registered on the analyzer's served registry) and
	// reused when the client is rebuilt after an endpoint reconnect, so the
	// cluster_intel_llm_* series stay stable and we never double-register.
	metrics     *llmclient.Metrics
	llmConfig   config.LLMConfig
	correlator  *Correlator
	tokensUsed  atomic.Int64
	dailyBudget int64
	// healthReporter returns the latest cluster health report to enrich LLM prompts.
	// Set via SetHealthReporter after engine construction to avoid circular deps.
	healthReporter func() *types.ClusterHealthReport

	// Phase 2 enrichment sources — optional, set via SetEnrichmentSources.
	k8sClientset  kubernetes.Interface
	prometheusURL string

	// Phase 3: similar-incident index — keyword fingerprints of resolved RCAs.
	// Protected by mu. Only incidents with a stored RCA are indexed.
	fingerprints map[int64]*rcaFingerprint

	// Phase 4: semantic vector store — coexists with keyword overlap.
	vectorStore *VectorStore
}

// rcaFingerprint holds the searchable terms and outcome for a past incident.
type rcaFingerprint struct {
	incidentID int64
	terms      map[string]bool // namespace, service, pod, kind, severity tokens
	rootCause  string
	summary    string
	createdAt  time.Time
}

// SetHealthReporter attaches a getter for the current cluster health report.
// Called once after construction from main so the RCAEngine can include
// health scores and resource utilization in every prompt.
func (e *RCAEngine) SetHealthReporter(fn func() *types.ClusterHealthReport) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.healthReporter = fn
}

// SetEnrichmentSources wires in optional data sources for prompt enrichment:
// a Kubernetes clientset (pod logs + Warning events) and a Prometheus base URL.
// Both are optional — nil/empty means that enrichment section is skipped.
func (e *RCAEngine) SetEnrichmentSources(cs kubernetes.Interface, prometheusURL string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.k8sClientset = cs
	e.prometheusURL = prometheusURL
}

// TokenBudget returns current token usage and the daily budget.
// Used by handleStatus to expose budget telemetry without leaking unexported fields.
func (e *RCAEngine) TokenBudget() (used, budget int64) {
	return e.tokensUsed.Load(), e.dailyBudget
}

// SetVectorStore attaches a Qdrant-backed semantic search store.
// Vector results are merged with keyword results in findSimilarIncidents;
// keyword overlap acts as fallback when Qdrant is unreachable.
func (e *RCAEngine) SetVectorStore(vs *VectorStore) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.vectorStore = vs
}

// NewRCAEngine creates an RCA engine with the given LLM client. reg is the
// analyzer's served Prometheus registry; the cluster_intel_llm_* metrics are
// registered on it once here and reused on reconnect.
func NewRCAEngine(llmCfg config.LLMConfig, correlator *Correlator, reg prometheus.Registerer) *RCAEngine {
	engine := &RCAEngine{
		reports:      make(map[int64]*RCAReport),
		fingerprints: make(map[int64]*rcaFingerprint),
		nextID:       1,
		llmConfig:    llmCfg,
		correlator:   correlator,
		dailyBudget:  int64(llmCfg.DailyTokenBudget),
		// Register the LLM metrics once, on the served registry, so they are
		// actually scraped (previously promauto sent them to the default
		// registry, which /metrics never serves).
		metrics: llmclient.NewMetrics(reg, "cluster_intel"),
	}

	// Only create client if we have a provider configured
	if llmCfg.Provider != "" && llmCfg.Endpoint != "" {
		engine.llmClient = llmclient.NewClient(llmCfg, engine.metrics)
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
	if report.RootCause.Primary != "" {
		e.fingerprints[incidentID] = buildFingerprint(incidentID, inc, report)
	}
	vs := e.vectorStore
	e.mu.Unlock()

	// Background upsert to Qdrant — non-blocking; errors only logged inside Upsert.
	if vs != nil && report.RootCause.Primary != "" {
		go func() {
			text := incidentToEmbedText(inc, report)
			uCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			vs.Upsert(uCtx, incidentID, text, report.RootCause.Primary, report.Summary)
		}()
	}

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
	podsWithInlineLog := make(map[string]bool) // track pods whose logs are already inline
	for _, sig := range inc.Signals {
		fmt.Fprintf(&b, "  %s  [%s]  %s/%s  %s: %s\n",
			sig.Timestamp.Format("15:04:05"), sig.Source,
			sig.Namespace, sig.Service, sig.Kind, sig.Title)
		if sig.Details != "" {
			fmt.Fprintf(&b, "    Details: %s\n", truncate(sig.Details, 500))
		}
		// LogSnippet captured at signal ingestion time (before pod restart).
		if sig.LogSnippet != "" {
			fmt.Fprintf(&b, "    Pod logs at event time: %s\n", sig.LogSnippet)
			if sig.Pod != "" {
				podsWithInlineLog[sig.Namespace+"/"+sig.Pod] = true
			}
		}
	}

	// Enrich with live cluster health snapshot so the LLM can correlate incident
	// signals against overall cluster state (resource pressure, health degradation).
	e.mu.RLock()
	hr := e.healthReporter
	e.mu.RUnlock()
	if hr != nil {
		if report := hr(); report != nil {
			b.WriteString("\n## Cluster Health at Analysis Time\n")
			if report.Scores != nil {
				fmt.Fprintf(&b, "Health Scores — Overall:%d  Reliability:%d  Security:%d  Cost:%d  Architecture:%d\n",
					report.Scores.Overall, report.Scores.Reliability, report.Scores.Security,
					report.Scores.Cost, report.Scores.Architecture)
			}
			cpu := report.ResourceUtilization.CPU
			mem := report.ResourceUtilization.Memory
			if cpu.Capacity > 0 {
				fmt.Fprintf(&b, "Resource Utilization — CPU:%.1f%% used (%.1f/%.1f%s)  Memory:%.1f%% used (%.1f/%.1f%s)\n",
					cpu.Used/cpu.Capacity*100, cpu.Used, cpu.Capacity, cpu.Unit,
					mem.Used/mem.Capacity*100, mem.Used, mem.Capacity, mem.Unit)
			}
			s := report.Summary
			if s.TotalPods > 0 {
				fmt.Fprintf(&b, "Cluster State — Nodes:%d  Pods:%d (%d unhealthy, %d pending)  Events:%d warning / %d critical\n",
					s.TotalNodes, s.TotalPods, s.UnhealthyPods, s.PendingPods, s.WarningEvents, s.CriticalEvents)
			}
		}
	}

	// Phase 2: live enrichment from K8s and Prometheus — each source gets a
	// tight 5-second deadline so a slow cluster never stalls the RCA request.
	e.mu.RLock()
	k8sCS := e.k8sClientset
	promURL := e.prometheusURL
	e.mu.RUnlock()

	if k8sCS != nil || promURL != "" {
		type section struct{ header, body string }
		ch := make(chan section, 10)
		var wg sync.WaitGroup

		if k8sCS != nil {
			namespaces := uniqueNamespacesFromIncident(inc)
			if len(namespaces) > 0 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					ctx5, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if s := rcaFetchK8sWarningEvents(ctx5, k8sCS, namespaces); s != "" {
						ch <- section{"K8s Warning Events (last hour)", s}
					}
				}()
			}
			for _, p := range uniquePodsFromIncident(inc, 3) {
				p := p
				// Skip pods whose logs were already captured at signal ingestion time.
				if podsWithInlineLog[p[0]+"/"+p[1]] {
					continue
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					ctx5, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if s := rcaFetchPodLogs(ctx5, k8sCS, p[0], p[1]); s != "" {
						ch <- section{fmt.Sprintf("Pod Logs: %s/%s (last 30 lines)", p[0], p[1]), s}
					}
				}()
			}
		}
		if promURL != "" {
			namespaces := uniqueNamespacesFromIncident(inc)
			if len(namespaces) > 0 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					ctx5, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if s := rcaFetchPrometheusMetrics(ctx5, promURL, namespaces); s != "" {
						ch <- section{"Prometheus Metrics", s}
					}
				}()
			}
		}

		go func() { wg.Wait(); close(ch) }()
		for sec := range ch {
			fmt.Fprintf(&b, "\n## %s\n%s\n", sec.header, sec.body)
		}
	}

	// Phase 3+4: include top-2 similar past incidents (vector + keyword).
	if similar := e.findSimilarIncidents(inc.ID, inc, 2, 2); len(similar) > 0 {
		b.WriteString("\n## Similar Past Incidents (for reference)\n")
		for _, si := range similar {
			fmt.Fprintf(&b, "  Past INC-%d root cause: %s\n  Summary: %s\n\n",
				si.IncidentID, si.RootCause, si.Summary)
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
		Summary   string `json:"summary"`
		RootCause struct {
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
			delete(e.fingerprints, incidentID)
			removed++
			continue
		}
		if now.Sub(report.CreatedAt) > ttl {
			delete(e.reports, incidentID)
			delete(e.fingerprints, incidentID)
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
			delete(e.fingerprints, entries[i].id)
			removed++
		}
	}
	return removed
}

// RegisterRoutes adds RCA-specific API endpoints.
func (e *RCAEngine) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/incidents/{id}/rca", e.handleGetRCA)
	mux.HandleFunc("POST /api/v1/incidents/{id}/rca/regenerate", e.handleRegenerate)
	mux.HandleFunc("GET /api/v1/incidents/{id}/rca/stream", e.handleStreamRCA)
	mux.HandleFunc("POST /api/v1/llm/ask", e.handleAsk)
}

// handleStreamRCA streams RCA progress as SSE events.
// If a report already exists it responds immediately with "complete".
// Otherwise it triggers Analyze() and sends "running" heartbeats every 2s until done.
func (e *RCAEngine) handleStreamRCA(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	sendEvent := func(event string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		flusher.Flush()
	}

	// If a completed report already exists, respond immediately.
	e.mu.RLock()
	existing := e.reports[id]
	e.mu.RUnlock()
	if existing != nil {
		sendEvent("complete", existing)
		return
	}

	if e.llmClient == nil {
		sendEvent("error", map[string]string{"message": "LLM not configured"})
		return
	}

	type result struct {
		report *RCAReport
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		rep, err := e.Analyze(ctx, id)
		resultCh <- result{rep, err}
	}()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	phases := []string{"correlating signals", "fetching cluster context", "running LLM analysis", "structuring report"}
	phase := 0
	for {
		select {
		case res := <-resultCh:
			if res.err != nil {
				sendEvent("error", map[string]string{"message": res.err.Error()})
			} else {
				sendEvent("complete", res.report)
			}
			return
		case <-ticker.C:
			msg := "analyzing"
			if phase < len(phases) {
				msg = phases[phase]
				phase++
			}
			sendEvent("running", map[string]string{"phase": msg})
		case <-r.Context().Done():
			return
		}
	}
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
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	report, err := e.Analyze(ctx, id)
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
		History    []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"history,omitempty"`
		Context *struct {
			Summary  string `json:"summary,omitempty"`
			Severity string `json:"severity,omitempty"`
			Status   string `json:"status,omitempty"`
		} `json:"context,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Question) == "" {
		http.Error(w, "question is required", http.StatusBadRequest)
		return
	}
	if len(body.Question) > 2000 {
		http.Error(w, "question too long (max 2000 characters)", http.StatusBadRequest)
		return
	}
	// Cap history to the last 20 messages (10 exchanges) to bound prompt size.
	if len(body.History) > 20 {
		body.History = body.History[len(body.History)-20:]
	}

	// Build rich system prompt with incident context so context never appears
	// as a user message (avoids user→user adjacency that some LLMs reject).
	systemContent := `You are a Kubernetes SRE expert with deep knowledge of:
- Kubernetes internals (scheduler, kubelet, kube-proxy, controllers, etcd)
- Container runtimes (containerd, CRI-O) and OCI image layer behaviour
- Network policies, CNI plugins (Calico, Cilium, Flannel), and service mesh (Istio, Linkerd)
- Resource management, QoS classes, OOM killer, CFS quota throttling
- Prometheus metrics, PromQL, alerting rules, and recording rules
- Load balancer behaviour, Ingress controllers, and upstream health checks
- Cluster autoscaler, HPA/VPA, and node pressure eviction
- Common failure patterns: pod crash loops, image pull errors, DNS failures, certificate expiry

When answering:
1. Be precise and cite specific signal timestamps or metric values from the context when available.
2. When giving commands or code, wrap them in triple-backtick code blocks with a language tag (e.g. ` + "```bash" + ` or ` + "```json" + `).
3. Format JSON examples as ` + "```json" + ` blocks with proper indentation.
4. For kubectl commands use ` + "```bash" + ` blocks.
5. Keep explanations concise but complete enough for an SRE to act on immediately.`

	if body.IncidentID != nil {
		inc := e.correlator.GetIncident(*body.IncidentID)
		if inc != nil {
			systemContent += "\n\n" + e.buildPrompt(inc)
			if inc.RCAReport != nil {
				systemContent += "\n\nPrevious RCA summary: " + inc.RCAReport.Summary +
					"\nRoot cause identified: " + inc.RCAReport.RootCause.Primary +
					fmt.Sprintf("\nRCA confidence: %.0f%%", inc.RCAReport.RootCause.Confidence*100)
			}
		}
	} else if body.Context != nil {
		// Fallback: use lightweight context forwarded from the frontend.
		systemContent += "\n\nIncident: " + body.Context.Summary +
			" | severity=" + body.Context.Severity +
			" | status=" + body.Context.Status
	}

	// Include live cluster health for general questions (no specific incident).
	e.mu.RLock()
	hr := e.healthReporter
	e.mu.RUnlock()
	if hr != nil && body.IncidentID == nil {
		if report := hr(); report != nil && report.Scores != nil {
			systemContent += fmt.Sprintf("\n\nCurrent cluster health — Overall:%d Reliability:%d Security:%d",
				report.Scores.Overall, report.Scores.Reliability, report.Scores.Security)
		}
	}

	messages := []types.LLMMessage{
		{Role: "system", Content: systemContent},
	}
	// Replay prior conversation turns so the LLM has full dialogue context.
	for _, msg := range body.History {
		if msg.Role == "user" || msg.Role == "assistant" {
			messages = append(messages, types.LLMMessage{Role: msg.Role, Content: msg.Content})
		}
	}
	messages = append(messages, types.LLMMessage{Role: "user", Content: body.Question})

	askCtx, askCancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer askCancel()
	result, err := e.llmClient.Complete(askCtx, "ask", messages)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	e.tokensUsed.Add(int64(result.TotalTokens))
	writeJSON(w, map[string]any{
		"answer":     result.Content,
		"tokensUsed": result.TotalTokens,
		"model":      e.llmConfig.Model,
	})
}

// buildFingerprint extracts searchable tokens from an incident and its RCA result.
func buildFingerprint(incidentID int64, inc *Incident, report *RCAReport) *rcaFingerprint {
	terms := make(map[string]bool)
	add := func(s string) {
		for _, t := range strings.Fields(strings.ToLower(s)) {
			if len(t) > 2 { // skip tiny tokens like "a", "is"
				terms[t] = true
			}
		}
	}
	add(inc.Severity)
	for _, sig := range inc.Signals {
		add(sig.Namespace)
		add(sig.Service)
		add(sig.Pod)
		add(sig.Kind)
		add(sig.Title)
	}
	for _, a := range inc.Affected {
		add(a)
	}
	return &rcaFingerprint{
		incidentID: incidentID,
		terms:      terms,
		rootCause:  report.RootCause.Primary,
		summary:    report.Summary,
		createdAt:  report.CreatedAt,
	}
}

// SimilarIncident is a unified result from keyword and/or vector similar-incident search.
type SimilarIncident struct {
	IncidentID int64
	RootCause  string
	Summary    string
	Score      float64 // keyword overlap count or vector cosine similarity
	Source     string  // "keyword" or "vector"
}

// findSimilarIncidents merges semantic vector search (Phase 4) with keyword
// overlap (Phase 3). Vector results take priority; keyword fills remaining slots.
// Returns at most maxResults entries, excluding currentID.
func (e *RCAEngine) findSimilarIncidents(currentID int64, inc *Incident, maxResults, minOverlap int) []SimilarIncident {
	seen := make(map[int64]bool)
	var results []SimilarIncident

	// Phase 4: vector search — runs concurrently if VectorStore is available.
	e.mu.RLock()
	vs := e.vectorStore
	e.mu.RUnlock()

	if vs != nil {
		ctx5, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		vrs := vs.Search(ctx5, incidentToEmbedText(inc, nil), maxResults+1)
		for _, vr := range vrs {
			if vr.IncidentID == currentID || seen[vr.IncidentID] {
				continue
			}
			seen[vr.IncidentID] = true
			results = append(results, SimilarIncident{
				IncidentID: vr.IncidentID,
				RootCause:  vr.RootCause,
				Summary:    vr.Summary,
				Score:      vr.Score,
				Source:     "vector",
			})
			if len(results) >= maxResults {
				return results
			}
		}
	}

	// Phase 3: keyword overlap — fills remaining slots.
	query := make(map[string]bool)
	addTerms := func(s string) {
		for _, t := range strings.Fields(strings.ToLower(s)) {
			if len(t) > 2 {
				query[t] = true
			}
		}
	}
	for _, sig := range inc.Signals {
		addTerms(sig.Namespace)
		addTerms(sig.Service)
		addTerms(sig.Pod)
		addTerms(sig.Kind)
	}
	addTerms(inc.Severity)

	type scored struct {
		fp    *rcaFingerprint
		score int
	}
	e.mu.RLock()
	var candidates []scored
	for id, fp := range e.fingerprints {
		if id == currentID || seen[id] {
			continue
		}
		overlap := 0
		for t := range query {
			if fp.terms[t] {
				overlap++
			}
		}
		if overlap >= minOverlap {
			candidates = append(candidates, scored{fp, overlap})
		}
	}
	e.mu.RUnlock()

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].fp.createdAt.After(candidates[j].fp.createdAt)
	})
	for _, c := range candidates {
		if len(results) >= maxResults {
			break
		}
		results = append(results, SimilarIncident{
			IncidentID: c.fp.incidentID,
			RootCause:  c.fp.rootCause,
			Summary:    c.fp.summary,
			Score:      float64(c.score),
			Source:     "keyword",
		})
	}
	return results
}

// incidentToEmbedText builds the text to embed for an incident.
// report may be nil (used for search queries where no RCA exists yet).
func incidentToEmbedText(inc *Incident, report *RCAReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "severity:%s affected:%s ", inc.Severity, strings.Join(inc.Affected, ","))
	for _, sig := range inc.Signals {
		fmt.Fprintf(&b, "%s %s %s %s %s ", sig.Namespace, sig.Service, sig.Pod, sig.Kind, sig.Title)
		if sig.Details != "" {
			b.WriteString(truncate(sig.Details, 100))
			b.WriteByte(' ')
		}
	}
	if report != nil && report.RootCause.Primary != "" {
		fmt.Fprintf(&b, "rootcause:%s ", report.RootCause.Primary)
	}
	return strings.TrimSpace(b.String())
}

// uniqueNamespacesFromIncident returns de-duplicated non-empty namespaces from signals.
func uniqueNamespacesFromIncident(inc *Incident) []string {
	seen := make(map[string]bool)
	var ns []string
	for _, sig := range inc.Signals {
		if sig.Namespace != "" && !seen[sig.Namespace] {
			seen[sig.Namespace] = true
			ns = append(ns, sig.Namespace)
		}
	}
	return ns
}

// uniquePodsFromIncident returns up to max [namespace, pod] pairs from signals.
func uniquePodsFromIncident(inc *Incident, max int) [][2]string {
	seen := make(map[string]bool)
	var pods [][2]string
	for _, sig := range inc.Signals {
		if sig.Pod == "" || sig.Namespace == "" || len(pods) >= max {
			continue
		}
		key := sig.Namespace + "/" + sig.Pod
		if !seen[key] {
			seen[key] = true
			pods = append(pods, [2]string{sig.Namespace, sig.Pod})
		}
	}
	return pods
}

// rcaFetchK8sWarningEvents lists Warning events for the given namespaces.
func rcaFetchK8sWarningEvents(ctx context.Context, cs kubernetes.Interface, namespaces []string) string {
	var lines []string
	for _, ns := range namespaces {
		evList, err := cs.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
			FieldSelector: "type=Warning",
			Limit:         15,
		})
		if err != nil {
			continue
		}
		sort.Slice(evList.Items, func(i, j int) bool {
			return evList.Items[i].LastTimestamp.After(evList.Items[j].LastTimestamp.Time)
		})
		for _, ev := range evList.Items {
			lines = append(lines, fmt.Sprintf("  [%s] %s/%s: %s — %s (×%d)",
				ev.LastTimestamp.Format("15:04:05"),
				ev.InvolvedObject.Kind, ev.InvolvedObject.Name,
				ev.Reason, truncate(ev.Message, 200), ev.Count))
		}
	}
	return strings.Join(lines, "\n")
}

// rcaFetchPodLogs returns the last 30 log lines for a pod.
// For multi-container pods it picks the first non-init container.
func rcaFetchPodLogs(ctx context.Context, cs kubernetes.Interface, namespace, pod string) string {
	tailLines := int64(30)
	opts := &corev1.PodLogOptions{TailLines: &tailLines}

	// For multi-container pods GetLogs without a container name fails.
	// Resolve the container by fetching the pod spec first.
	podObj, err := cs.CoreV1().Pods(namespace).Get(ctx, pod, metav1.GetOptions{})
	if err == nil && len(podObj.Spec.Containers) > 0 {
		opts.Container = podObj.Spec.Containers[0].Name
	}

	raw, err := cs.CoreV1().Pods(namespace).GetLogs(pod, opts).DoRaw(ctx)
	if err != nil || len(raw) == 0 {
		return ""
	}
	return truncate(string(raw), 1000)
}

// prometheusVectorResponse is the subset of the Prometheus HTTP API response we parse.
type prometheusVectorResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Metric map[string]string  `json:"metric"`
			Value  [2]json.RawMessage `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// rcaFetchPrometheusMetrics queries CPU throttle rate and memory working-set
// for the given namespaces and returns a compact multi-line summary.
func rcaFetchPrometheusMetrics(ctx context.Context, baseURL string, namespaces []string) string {
	query := func(q string) string {
		u := baseURL + "/api/v1/query?query=" + url.QueryEscape(q)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return ""
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return ""
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		if err != nil {
			return ""
		}
		var pqr prometheusVectorResponse
		if err := json.Unmarshal(body, &pqr); err != nil || pqr.Status != "success" {
			return ""
		}
		var parts []string
		for _, r := range pqr.Data.Result {
			var val string
			if len(r.Value) == 2 {
				_ = json.Unmarshal(r.Value[1], &val)
			}
			label := r.Metric["pod"]
			if label == "" {
				label = r.Metric["namespace"]
			}
			if label != "" && val != "" {
				parts = append(parts, fmt.Sprintf("%s=%s", label, val))
			}
		}
		return strings.Join(parts, ", ")
	}

	var lines []string
	for _, ns := range namespaces {
		if v := query(fmt.Sprintf(`sum(rate(container_cpu_cfs_throttled_seconds_total{namespace=%q}[5m])) by (pod)`, ns)); v != "" {
			lines = append(lines, fmt.Sprintf("  CPU throttle [%s]: %s", ns, v))
		}
		if v := query(fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace=%q,container!=""})/1024/1024 by (pod)`, ns)); v != "" {
			lines = append(lines, fmt.Sprintf("  Memory MiB [%s]: %s", ns, v))
		}
	}
	return strings.Join(lines, "\n")
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
