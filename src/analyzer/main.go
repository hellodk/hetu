// Package main implements the K8s Cluster Intelligence Engine analyzer.
// It processes telemetry data and generates AI-powered insights.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	mw "github.com/your-org/cluster-intel/pkg/middleware"
	types "github.com/your-org/cluster-intel/pkg/types"
)

// Config holds analyzer configuration
type Config struct {
	ClusterID        string        `json:"clusterId"`
	CollectorURL     string        `json:"collectorUrl"`
	LLMBackend       string        `json:"llmBackend"`
	LLMEndpoint      string        `json:"llmEndpoint"`
	LLMModel         string        `json:"llmModel"`
	LLMAPIKey        string        `json:"-"`
	AnalysisInterval time.Duration `json:"analysisInterval"`
	MetricsPort      int           `json:"metricsPort"`
	APIPort          int           `json:"apiPort"`
	MaxTokens        int           `json:"maxTokens"`
	Temperature      float64       `json:"temperature"`
}

// Analyzer is the main analyzer service
type Analyzer struct {
	config          Config
	httpClient      *http.Client
	latestReport    *types.ClusterHealthReport
	previousReport  *types.ClusterHealthReport
	reportHistory   []*types.ClusterHealthReport
	reportMu        sync.RWMutex
	stopCh          chan struct{}
	wg              sync.WaitGroup
	promptTemplates map[string]*template.Template

	// SSE Support
	subscribers map[chan *types.ClusterHealthReport]struct{}
	subMu       sync.RWMutex

	// Prometheus metrics (custom registry)
	registry         *prometheus.Registry
	analysisRuns     prometheus.Counter
	analysisErrors   prometheus.Counter
	analysisDuration prometheus.Histogram
	llmTokensUsed    prometheus.Counter
	healthScore      prometheus.Gauge

	// HTTP servers for graceful shutdown
	metricsServer *http.Server
	apiServer     *http.Server
}

// NewAnalyzer creates a new analyzer instance
func NewAnalyzer(config Config) (*Analyzer, error) {
	analyzer := &Analyzer{
		config: config,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		stopCh:          make(chan struct{}),
		promptTemplates: make(map[string]*template.Template),
		subscribers:     make(map[chan *types.ClusterHealthReport]struct{}),
	}

	// Initialize prompt templates
	analyzer.initPromptTemplates()

	// Initialize Prometheus metrics
	analyzer.initMetrics()

	return analyzer, nil
}

// initMetrics initializes Prometheus metrics with a custom registry
func (a *Analyzer) initMetrics() {
	a.registry = prometheus.NewRegistry()

	a.analysisRuns = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cluster_intel_analysis_runs_total",
		Help: "Total number of analysis runs",
	})

	a.analysisErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cluster_intel_analysis_errors_total",
		Help: "Total number of analysis errors",
	})

	a.analysisDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "cluster_intel_analysis_duration_seconds",
		Help:    "Duration of analysis runs",
		Buckets: prometheus.ExponentialBuckets(1, 2, 10),
	})

	a.llmTokensUsed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cluster_intel_llm_tokens_used_total",
		Help: "Total LLM tokens used",
	})

	a.healthScore = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cluster_intel_health_score",
		Help: "Current cluster health score",
	})

	a.registry.MustRegister(
		a.analysisRuns,
		a.analysisErrors,
		a.analysisDuration,
		a.llmTokensUsed,
		a.healthScore,
	)
}

// initPromptTemplates initializes the prompt templates
func (a *Analyzer) initPromptTemplates() {
	// Root cause analysis template
	rcaTemplate := `You are a Kubernetes SRE expert. Analyze the following cluster telemetry and identify issues.

## Cluster: {{.ClusterID}}
## Time: {{.Timestamp}}

## Correlated Warning Events ({{len .CorrelatedEvents}} total):
{{range .CorrelatedEvents}}
- [{{.Event.Type}}] {{.Event.Reason}}: {{.Event.InvolvedObject.Kind}}/{{.Event.InvolvedObject.Namespace}}/{{.Event.InvolvedObject.Name}}
  Message: {{.Event.Message}}
  Count: {{.Event.Count}}
  Logs:
  {{range .LogLines}}
    {{.}}
  {{end}}
{{end}}

## Resource Metrics Summary:
{{range .Metrics}}
- {{.ResourceType}}: {{.Resource.Namespace}}/{{.Resource.Name}}
  CPU: {{index .Metrics "cpu_millicores"}}m, Memory: {{index .Metrics "memory_bytes"}} bytes
{{end}}

Analyze and provide a JSON response with this structure:
{
  "issues": [
    {
      "severity": "critical|high|medium|low",
      "category": "reliability|security|cost|architecture",
      "title": "Brief title",
      "description": "Detailed description",
      "affectedResources": ["resource1", "resource2"],
      "rootCause": "Root cause analysis",
      "confidence": 0.0-1.0
    }
  ],
  "recommendations": [
    {
      "category": "reliability|security|cost|architecture",
      "title": "Recommendation title",
      "description": "What to do",
      "priority": 1-10,
      "estimatedSavings": 0.0,
      "effort": "low|medium|high",
      "risk": "low|medium|high"
    }
  ],
  "healthScores": {
    "reliability": 0-100,
    "security": 0-100,
    "cost": 0-100,
    "architecture": 0-100
  }
}

Only output valid JSON, no other text.`

	a.promptTemplates["rca"] = template.Must(template.New("rca").Parse(rcaTemplate))

	// Security analysis template
	securityTemplate := `You are a Kubernetes security expert. Analyze the cluster for security issues.

## Cluster: {{.ClusterID}}

## Pod Security Contexts:
{{.PodSecurityData}}

## RBAC Configuration:
{{.RBACData}}

## Network Policies:
{{.NetworkPolicies}}

Identify security vulnerabilities and provide JSON response:
{
  "findings": [
    {
      "severity": "critical|high|medium|low",
      "category": "rbac|network|pod-security|image|secrets",
      "title": "Finding title",
      "description": "Detailed description",
      "affectedResources": ["resource1"],
      "cisControl": "CIS control number",
      "remediation": "How to fix"
    }
  ],
  "securityScore": 0-100
}

Only output valid JSON.`

	a.promptTemplates["security"] = template.Must(template.New("security").Parse(securityTemplate))

	// Cost optimization template
	costTemplate := `You are a cloud cost optimization expert for Kubernetes.

## Cluster: {{.ClusterID}}

## Resource Utilization:
{{range .Metrics}}
- {{.Resource.Namespace}}/{{.Resource.Name}}
  Requested CPU: {{index .Metrics "cpu_requested_millicores"}}m, Used CPU: {{index .Metrics "cpu_millicores"}}m
  Requested Memory: {{index .Metrics "memory_requested_bytes"}} bytes, Used Memory: {{index .Metrics "memory_bytes"}} bytes
{{end}}

## Cost Context:
- Cost per CPU core/hour: ${{.CostPerCore}}
- Cost per GB memory/hour: ${{.CostPerGB}}

Identify cost optimization opportunities. Provide JSON:
{
  "recommendations": [
    {
      "type": "right-sizing|idle-resource|spot-candidate",
      "title": "Recommendation",
      "description": "Details",
      "affectedResources": ["resource1"],
      "monthlySavings": 0.0,
      "confidence": 0.0-1.0
    }
  ],
  "totalPotentialSavings": 0.0,
  "costEfficiencyScore": 0-100
}

Only output valid JSON.`

	a.promptTemplates["cost"] = template.Must(template.New("cost").Parse(costTemplate))
}

// Start begins the analyzer service
func (a *Analyzer) Start(ctx context.Context) error {
	log.Info().Str("cluster", a.config.ClusterID).Msg("Starting analyzer")

	// Start analysis loop
	a.wg.Add(1)
	go a.analysisLoop(ctx)

	// Start HTTP servers
	a.wg.Add(2)
	go a.serveMetrics()
	go a.serveAPI()

	return nil
}

// analysisLoop runs periodic analysis
func (a *Analyzer) analysisLoop(ctx context.Context) {
	defer a.wg.Done()

	// Run initial analysis
	a.runAnalysis(ctx)

	ticker := time.NewTicker(a.config.AnalysisInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.runAnalysis(ctx)
		}
	}
}

// runAnalysis performs a full cluster analysis
func (a *Analyzer) runAnalysis(ctx context.Context) {
	start := time.Now()
	a.analysisRuns.Inc()

	log.Info().Msg("Starting cluster analysis")

	// Fetch telemetry from collector
	events, err := a.fetchEvents(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch events")
		a.analysisErrors.Inc()
		return
	}

	metrics, err := a.fetchMetrics(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch metrics")
		a.analysisErrors.Inc()
		return
	}

	log.Info().Int("events", len(events)).Int("metrics", len(metrics)).Msg("Fetched telemetry")

	correlatedEvents, err := a.fetchCorrelatedEvents(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch correlated events")
	}

	// Build analysis context - limit data to avoid overwhelming the LLM
	warningEvents := filterWarningEvents(events)
	if len(warningEvents) > 50 {
		warningEvents = warningEvents[len(warningEvents)-50:]
	}
	limitedMetrics := metrics
	if len(limitedMetrics) > 100 {
		limitedMetrics = limitedMetrics[len(limitedMetrics)-100:]
	}
	limitedCorrelated := correlatedEvents
	if len(limitedCorrelated) > 20 {
		limitedCorrelated = limitedCorrelated[len(limitedCorrelated)-20:]
	}

	analysisCtx := map[string]any{
		"ClusterID":        a.config.ClusterID,
		"Timestamp":        time.Now().Format(time.RFC3339),
		"Events":           warningEvents,
		"Metrics":          limitedMetrics,
		"CorrelatedEvents": limitedCorrelated,
	}

	// Run LLM analysis
	llmResponse, err := a.runLLMAnalysis(ctx, "rca", analysisCtx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to run LLM analysis")
		a.analysisErrors.Inc()
		// Continue with basic analysis
	}

	// Build health report
	report := a.buildHealthReport(events, metrics, llmResponse)

	// Store report
	a.reportMu.Lock()
	if a.latestReport != nil {
		a.previousReport = a.latestReport

		report.Trends = types.HealthTrends{
			Overall:      report.Scores.Overall - a.previousReport.Scores.Overall,
			Reliability:  report.Scores.Reliability - a.previousReport.Scores.Reliability,
			Security:     report.Scores.Security - a.previousReport.Scores.Security,
			Cost:         report.Scores.Cost - a.previousReport.Scores.Cost,
			Architecture: report.Scores.Architecture - a.previousReport.Scores.Architecture,
		}
	} else {
		report.Trends = types.HealthTrends{}
	}

	a.latestReport = report
	a.reportHistory = append(a.reportHistory, report)
	if len(a.reportHistory) > 100 {
		a.reportHistory = a.reportHistory[1:] // keep last 100
	}
	a.reportMu.Unlock()

	// Update metrics
	a.healthScore.Set(float64(report.Scores.Overall))
	a.analysisDuration.Observe(time.Since(start).Seconds())

	log.Info().
		Int("healthScore", report.Scores.Overall).
		Int("issues", len(report.TopIssues)).
		Int("recommendations", len(report.Recommendations)).
		Dur("duration", time.Since(start)).
		Msg("Analysis complete")

	// Broadcast to SSE clients
	a.subMu.RLock()
	for ch := range a.subscribers {
		select {
		case ch <- report:
		default:
			// Client slow, drop the report
		}
	}
	a.subMu.RUnlock()
}

// subscribe creates a new channel for SSE
func (a *Analyzer) subscribe() chan *types.ClusterHealthReport {
	ch := make(chan *types.ClusterHealthReport, 1)
	a.subMu.Lock()
	a.subscribers[ch] = struct{}{}
	a.subMu.Unlock()
	return ch
}

// unsubscribe removes a channel
func (a *Analyzer) unsubscribe(ch chan *types.ClusterHealthReport) {
	a.subMu.Lock()
	delete(a.subscribers, ch)
	a.subMu.Unlock()
	close(ch)
}

// fetchEvents fetches events from the collector
func (a *Analyzer) fetchEvents(ctx context.Context) ([]types.TelemetryEvent, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", a.config.CollectorURL+"/api/v1/events", nil)
	if err != nil {
		return nil, err
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var events []types.TelemetryEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, err
	}

	return events, nil
}

// fetchCorrelatedEvents fetches correlated events from the collector
func (a *Analyzer) fetchCorrelatedEvents(ctx context.Context) ([]types.CorrelatedEvidence, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", a.config.CollectorURL+"/api/v1/events/correlated", nil)
	if err != nil {
		return nil, err
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var events []types.CorrelatedEvidence
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, err
	}

	return events, nil
}

// fetchMetrics fetches metrics from the collector
func (a *Analyzer) fetchMetrics(ctx context.Context) ([]types.ResourceMetrics, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", a.config.CollectorURL+"/api/v1/metrics", nil)
	if err != nil {
		return nil, err
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var metrics []types.ResourceMetrics
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		return nil, err
	}

	return metrics, nil
}

// runLLMAnalysis executes LLM analysis with the given template
func (a *Analyzer) runLLMAnalysis(ctx context.Context, templateName string, data map[string]any) (map[string]any, error) {
	tmpl, ok := a.promptTemplates[templateName]
	if !ok {
		return nil, fmt.Errorf("template %s not found", templateName)
	}

	// Render prompt
	var promptBuf bytes.Buffer
	if err := tmpl.Execute(&promptBuf, data); err != nil {
		return nil, fmt.Errorf("failed to render prompt: %w", err)
	}

	prompt := promptBuf.String()
	log.Debug().Str("template", templateName).Int("promptLen", len(prompt)).Msg("Generated prompt")

	// Build LLM request
	llmReq := types.LLMRequest{
		Model: a.config.LLMModel,
		Messages: []types.LLMMessage{
			{
				Role:    "system",
				Content: "You are a Kubernetes cluster analysis expert. Always respond with valid JSON only.",
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
		MaxTokens:   a.config.MaxTokens,
		Temperature: a.config.Temperature,
	}

	// Send request to LLM
	reqBody, err := json.Marshal(llmReq)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.config.LLMEndpoint+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if a.config.LLMAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.config.LLMAPIKey)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, string(body))
	}

	var llmResp types.LLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return nil, fmt.Errorf("failed to decode LLM response: %w", err)
	}

	// Update token usage
	a.llmTokensUsed.Add(float64(llmResp.Usage.TotalTokens))

	// Parse response content
	if len(llmResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in LLM response")
	}

	content := llmResp.Choices[0].Message.Content

	// Extract JSON from response
	var result map[string]any
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// Try to extract JSON from markdown code block
		content = extractJSON(content)
		if err := json.Unmarshal([]byte(content), &result); err != nil {
			return nil, fmt.Errorf("failed to parse LLM JSON: %w", err)
		}
	}

	return result, nil
}

// extractJSON attempts to extract JSON from a string that may contain markdown
func extractJSON(s string) string {
	// Try to find JSON object
	start := -1
	end := -1
	depth := 0

	for i, c := range s {
		if c == '{' {
			if start == -1 {
				start = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 && start != -1 {
				end = i + 1
				break
			}
		}
	}

	if start != -1 && end != -1 {
		return s[start:end]
	}

	return s
}

// filterWarningEvents filters for warning events only
func filterWarningEvents(events []types.TelemetryEvent) []types.TelemetryEvent {
	var warnings []types.TelemetryEvent
	for _, e := range events {
		if e.Type == "Warning" {
			warnings = append(warnings, e)
		}
	}
	return warnings
}

// buildHealthReport constructs the health report from analysis results
func (a *Analyzer) buildHealthReport(events []types.TelemetryEvent, metrics []types.ResourceMetrics, llmResponse map[string]any) *types.ClusterHealthReport {
	report := &types.ClusterHealthReport{
		ClusterID: a.config.ClusterID,
		Timestamp: time.Now(),
		Scores: types.HealthScores{
			Overall:      85,
			Reliability:  90,
			Security:     80,
			Cost:         75,
			Architecture: 85,
		},
		Summary: types.ClusterSummary{
			WarningEvents: len(filterWarningEvents(events)),
			Namespaces:    make(map[string]*types.NamespaceStats),
		},
	}

	// Pre-aggregate namespace warnings
	for _, e := range filterWarningEvents(events) {
		ns := e.InvolvedObject.Namespace
		if ns != "" {
			if report.Summary.Namespaces[ns] == nil {
				report.Summary.Namespaces[ns] = &types.NamespaceStats{}
			}
			report.Summary.Namespaces[ns].Warnings++
		}
	}

	// Aggregate resource utilization
	var cpuUsed, cpuReq, cpuCap float64
	var memUsed, memReq, memCap float64
	var storageUsed, storageCap float64

	podSet := make(map[string]bool)
	nodeSet := make(map[string]bool)

	for _, m := range metrics {
		if m.ResourceType == "pod" {
			ns := m.Resource.Namespace
			podSet[ns+"/"+m.Resource.Name] = true

			if report.Summary.Namespaces[ns] == nil {
				report.Summary.Namespaces[ns] = &types.NamespaceStats{}
			}
			report.Summary.Namespaces[ns].PodCount++

			if v, ok := m.Metrics["cpu_millicores"].(float64); ok {
				cpuUsed += v / 1000.0
				report.Summary.Namespaces[ns].CPUUsed += v / 1000.0
			}
			if v, ok := m.Metrics["memory_bytes"].(float64); ok {
				memGi := v / (1024 * 1024 * 1024)
				memUsed += memGi
				report.Summary.Namespaces[ns].MemoryUsed += memGi
			}
			if v, ok := m.Metrics["cpu_requested_millicores"].(float64); ok {
				cpuReq += v / 1000.0
			}
			if v, ok := m.Metrics["memory_requested_bytes"].(float64); ok {
				memReq += v / (1024 * 1024 * 1024)
			}
		} else if m.ResourceType == "node" {
			nodeSet[m.Resource.Name] = true
			if v, ok := m.Metrics["cpu_capacity_millicores"].(float64); ok {
				cpuCap += v / 1000.0
			}
			if v, ok := m.Metrics["memory_capacity_bytes"].(float64); ok {
				memCap += v / (1024 * 1024 * 1024)
			}
		} else if m.ResourceType == "pvc" {
			if v, ok := m.Metrics["capacity_bytes"].(float64); ok {
				storageCap += v / (1024 * 1024 * 1024 * 1024) // Convert to Ti
			}
			if v, ok := m.Metrics["used_bytes"].(float64); ok {
				storageUsed += v / (1024 * 1024 * 1024 * 1024)
			}
		}
	}

	report.ResourceUtilization = types.ResourceUtilization{
		CPU: types.ResourceUsage{
			Used:      cpuUsed,
			Requested: cpuReq,
			Capacity:  cpuCap,
			Unit:      "cores",
		},
		Memory: types.ResourceUsage{
			Used:      memUsed,
			Requested: memReq,
			Capacity:  memCap,
			Unit:      "Gi",
		},
		Storage: types.ResourceUsage{
			Used:      storageUsed,
			Requested: storageCap, // typically same or just track capacity
			Capacity:  storageCap,
			Unit:      "Ti",
		},
	}

	// Extract scores from LLM response
	if llmResponse != nil {
		if scores, ok := llmResponse["healthScores"].(map[string]any); ok {
			if v, ok := scores["reliability"].(float64); ok {
				report.Scores.Reliability = int(v)
			}
			if v, ok := scores["security"].(float64); ok {
				report.Scores.Security = int(v)
			}
			if v, ok := scores["cost"].(float64); ok {
				report.Scores.Cost = int(v)
			}
			if v, ok := scores["architecture"].(float64); ok {
				report.Scores.Architecture = int(v)
			}
		}

		// Calculate overall score using shared constants
		report.Scores.Overall = types.CalculateOverallScore(report.Scores)

		// Extract issues
		if issues, ok := llmResponse["issues"].([]any); ok {
			for i, issue := range issues {
				if issueMap, ok := issue.(map[string]any); ok {
					report.TopIssues = append(report.TopIssues, types.Issue{
						ID:          fmt.Sprintf("issue-%d", i),
						Severity:    getString(issueMap, "severity", "medium"),
						Category:    getString(issueMap, "category", "reliability"),
						Title:       getString(issueMap, "title", "Unknown Issue"),
						Description: getString(issueMap, "description", ""),
						RootCause:   getString(issueMap, "rootCause", ""),
						Confidence:  getFloat(issueMap, "confidence", 0.5),
						Timestamp:   time.Now(),
					})
				}
			}
		}

		// Extract recommendations
		if recs, ok := llmResponse["recommendations"].([]any); ok {
			for i, rec := range recs {
				if recMap, ok := rec.(map[string]any); ok {
					savings := getFloat(recMap, "estimatedSavings", 0)
					report.Recommendations = append(report.Recommendations, types.Recommendation{
						ID:          fmt.Sprintf("rec-%d", i),
						Category:    getString(recMap, "category", "reliability"),
						Title:       getString(recMap, "title", "Recommendation"),
						Description: getString(recMap, "description", ""),
						Severity:    mapPriorityToSeverity(getFloat(recMap, "priority", 5)),
						Confidence:  0.8,
						Timestamp:   time.Now(),
						Impact: types.RecommendationImpact{
							Effort:    getString(recMap, "effort", "medium"),
							RiskLevel: getString(recMap, "risk", "low"),
							CostSavings: &types.CostSavings{
								Monthly:  savings,
								Currency: "USD",
							},
						},
					})
					report.EstimatedSavings += savings
				}
			}
		}
	}

	// Pod counts already set implicitly above
	report.Summary.TotalPods = len(podSet)
	report.Summary.TotalNodes = len(nodeSet)
	report.Summary.HealthyPods = report.Summary.TotalPods - len(report.TopIssues)
	if report.Summary.HealthyPods < 0 {
		report.Summary.HealthyPods = 0
	}

	return report
}

// Helper functions
func getString(m map[string]any, key, defaultVal string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return defaultVal
}

func getFloat(m map[string]any, key string, defaultVal float64) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return defaultVal
}

func mapPriorityToSeverity(priority float64) string {
	if priority <= 2 {
		return "critical"
	} else if priority <= 4 {
		return "high"
	} else if priority <= 6 {
		return "medium"
	}
	return "low"
}

// serveMetrics starts the Prometheus metrics server
func (a *Analyzer) serveMetrics() {
	defer a.wg.Done()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(a.registry, promhttp.HandlerOpts{}))

	a.metricsServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", a.config.MetricsPort),
		Handler: mux,
	}

	log.Info().Int("port", a.config.MetricsPort).Msg("Starting metrics server")
	if err := a.metricsServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Error().Err(err).Msg("Metrics server error")
	}
}

// serveAPI starts the API server
func (a *Analyzer) serveAPI() {
	defer a.wg.Done()

	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// API endpoints
	mux.HandleFunc("/api/v1/health", a.handleHealthReport)
	mux.HandleFunc("/api/v1/scores", a.handleScores)
	mux.HandleFunc("/api/v1/recommendations", a.handleRecommendations)
	mux.HandleFunc("/api/v1/issues", a.handleIssues)
	mux.HandleFunc("/api/v1/health/stream", a.handleHealthStream)
	mux.HandleFunc("/api/v1/analysis/trigger", a.handleTriggerAnalysis)
	mux.HandleFunc("/api/v1/events/timeline", a.handleTimeline)
	mux.HandleFunc("/api/v1/history", a.handleHistory)
	mux.HandleFunc("/api/v1/dns/health", a.handleDNSHealth)
	mux.HandleFunc("/api/v1/pods/", a.handlePodLogs)

	// Configure CORS from environment
	allowedOriginsStr := getEnvOrDefault("CORS_ALLOWED_ORIGINS", "*")
	allowedOrigins := strings.Split(allowedOriginsStr, ",")
	for i := range allowedOrigins {
		allowedOrigins[i] = strings.TrimSpace(allowedOrigins[i])
	}

	corsMiddleware := mw.CORS(mw.CORSConfig{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	})

	// Add rate limiting: 100 requests per second, burst of 200
	limiter := mw.NewRateLimiter(100, time.Second, 200)
	rateLimitMiddleware := mw.RateLimit(limiter)

	// Chain middleware: rate limit -> CORS -> mux
	handler := rateLimitMiddleware(corsMiddleware(mux))

	a.apiServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", a.config.APIPort),
		Handler: handler,
	}

	log.Info().Int("port", a.config.APIPort).Msg("Starting API server")
	if err := a.apiServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Error().Err(err).Msg("API server error")
	}
}

// handleHealthReport returns the full health report
func (a *Analyzer) handleHealthReport(w http.ResponseWriter, r *http.Request) {
	a.reportMu.RLock()
	report := a.latestReport
	a.reportMu.RUnlock()

	if report == nil {
		http.Error(w, "No report available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report)
}

// handleHealthStream handles Server-Sent Events for live reports
func (a *Analyzer) handleHealthStream(w http.ResponseWriter, r *http.Request) {
	// Let CORS middleware handle CORS if possible, but explicitly set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// Send initial report immediately if available
	a.reportMu.RLock()
	initialReport := a.latestReport
	a.reportMu.RUnlock()

	if initialReport != nil {
		data, _ := json.Marshal(initialReport)
		fmt.Fprintf(w, "data: %s\n\n", string(data))
		flusher.Flush()
	}

	ch := a.subscribe()
	defer a.unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-a.stopCh:
			return
		case report := <-ch:
			data, err := json.Marshal(report)
			if err == nil {
				fmt.Fprintf(w, "data: %s\n\n", string(data))
				flusher.Flush()
			}
		}
	}
}

// handleScores returns just the health scores
func (a *Analyzer) handleScores(w http.ResponseWriter, r *http.Request) {
	a.reportMu.RLock()
	report := a.latestReport
	a.reportMu.RUnlock()

	if report == nil {
		http.Error(w, "No report available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report.Scores)
}

// handleRecommendations returns the recommendations
func (a *Analyzer) handleRecommendations(w http.ResponseWriter, r *http.Request) {
	a.reportMu.RLock()
	report := a.latestReport
	a.reportMu.RUnlock()

	if report == nil {
		http.Error(w, "No report available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report.Recommendations)
}

// handleIssues returns the top issues
func (a *Analyzer) handleIssues(w http.ResponseWriter, r *http.Request) {
	a.reportMu.RLock()
	report := a.latestReport
	a.reportMu.RUnlock()

	if report == nil {
		http.Error(w, "No report available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report.TopIssues)
}

// handleTriggerAnalysis triggers an immediate analysis
func (a *Analyzer) handleTriggerAnalysis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	go a.runAnalysis(context.Background())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "analysis triggered",
	})
}

// handleTimeline returns timeline events
func (a *Analyzer) handleTimeline(w http.ResponseWriter, r *http.Request) {
	events, err := a.fetchEvents(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var timeline []types.TimelineEvent
	for _, e := range events {
		eventType := "info"
		if e.Type == "Warning" || e.Type == "Error" {
			if e.Reason == "CrashLoopBackOff" || e.Reason == "Failed" {
				eventType = "incident"
			} else {
				eventType = "warning"
			}
		} else if e.Type == "Normal" && e.Reason == "SuccessfulCreate" {
			eventType = "recovery"
		}

		timeline = append(timeline, types.TimelineEvent{
			ID:          e.ID,
			Type:        eventType,
			Title:       e.Reason,
			Description: e.Message,
			Timestamp:   e.Timestamp,
		})
	}

	// Sort by timestamp descending (newest first)
	sort.Slice(timeline, func(i, j int) bool {
		return timeline[i].Timestamp.After(timeline[j].Timestamp)
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(timeline)
}

// handleHistory returns history of reports
func (a *Analyzer) handleHistory(w http.ResponseWriter, r *http.Request) {
	a.reportMu.RLock()
	history := a.reportHistory
	a.reportMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

// handleDNSHealth proxies the DNS metrics from the collector
func (a *Analyzer) handleDNSHealth(w http.ResponseWriter, r *http.Request) {
	req, err := http.NewRequestWithContext(r.Context(), "GET", a.config.CollectorURL+"/api/v1/dns/health", nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := a.httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		http.Error(w, "DNS health unavailable", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

// handlePodLogs proxies pod logs from the collector
func (a *Analyzer) handlePodLogs(w http.ResponseWriter, r *http.Request) {
	targetURL := a.config.CollectorURL + r.URL.Path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(r.Context(), "GET", targetURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		http.Error(w, "Failed to fetch pod logs", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "Error fetching pod logs", resp.StatusCode)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

// Stop gracefully stops the analyzer
func (a *Analyzer) Stop() {
	log.Info().Msg("Stopping analyzer")
	close(a.stopCh)

	// Gracefully shut down HTTP servers with a 5-second timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if a.apiServer != nil {
		if err := a.apiServer.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("API server shutdown error")
		}
	}
	if a.metricsServer != nil {
		if err := a.metricsServer.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Metrics server shutdown error")
		}
	}

	a.wg.Wait()
}

func main() {
	// Configure logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Load configuration
	config := Config{
		ClusterID:        getEnvOrDefault("CLUSTER_ID", "default"),
		CollectorURL:     getEnvOrDefault("COLLECTOR_URL", "http://collector:8080"),
		LLMBackend:       getEnvOrDefault("LLM_BACKEND", "openai"),
		LLMEndpoint:      getEnvOrDefault("LLM_ENDPOINT", "https://api.openai.com/v1"),
		LLMModel:         getEnvOrDefault("LLM_MODEL", "gpt-4-turbo"),
		LLMAPIKey:        os.Getenv("LLM_API_KEY"),
		AnalysisInterval: getDurationOrDefault("ANALYSIS_INTERVAL", 5*time.Minute),
		MetricsPort:      getEnvIntOrDefault("METRICS_PORT", 9091),
		APIPort:          getEnvIntOrDefault("API_PORT", 8081),
		MaxTokens:        getEnvIntOrDefault("LLM_MAX_TOKENS", 4096),
		Temperature:      getEnvFloatOrDefault("LLM_TEMPERATURE", 0.3),
	}

	// Validate LLM configuration
	if config.LLMAPIKey == "" {
		log.Warn().Msg("LLM_API_KEY not set, LLM analysis will be disabled")
	}

	// Create analyzer
	analyzer, err := NewAnalyzer(config)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create analyzer")
	}

	// Setup context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start analyzer
	if err := analyzer.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to start analyzer")
	}

	// Wait for shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.Info().Str("signal", sig.String()).Msg("Received shutdown signal")

	cancel()
	analyzer.Stop()
	log.Info().Msg("Analyzer stopped")
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvIntOrDefault(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		var result int
		fmt.Sscanf(val, "%d", &result)
		return result
	}
	return defaultVal
}

func getEnvFloatOrDefault(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		var result float64
		fmt.Sscanf(val, "%f", &result)
		return result
	}
	return defaultVal
}

func getDurationOrDefault(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}
