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

	ucconfig "github.com/your-org/cluster-intel/pkg/config"
	mw "github.com/your-org/cluster-intel/pkg/middleware"
	types "github.com/your-org/cluster-intel/pkg/types"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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

	// Profile state: "live" or "mock". Controlled at startup via PROFILE env
	// var and at runtime via POST /api/v1/profile. The default is "live" —
	// no mock/fabricated data is served unless the operator explicitly
	// switches the profile.
	profileMu sync.RWMutex
	profile   string

	// Mock data source used only when profile == "mock". Runs on its own
	// goroutine and writes reports through broadcastReport().
	mockSource *mockSource

	// Diagnostics for the live path. Each field is updated by runAnalysis
	// and surfaced via GET /api/v1/status and the Status block on reports.
	diagMu             sync.RWMutex
	lastAnalysisAt     *time.Time
	lastAnalysisError  string
	collectorReachable bool
	collectorLastOKAt  *time.Time
	collectorLastError string
	llmReachable       bool
	llmLastOKAt        *time.Time
	llmLastError       string

	// SSE Support
	subscribers map[chan *types.ClusterHealthReport]struct{}
	subMu       sync.RWMutex

	// Workload browser (v7 Phase 1)
	workloadHandler *WorkloadHandler

	// Error aggregator (v7 Phase 3)
	errorAggregator *ErrorAggregator

	// LB log aggregator (v7 Phase 4)
	lbLogAggregator *LBLogAggregator

	// Correlator + RCA (v7 Phase 5)
	correlator *Correlator
	rcaEngine  *RCAEngine

	// Optimizer registry (v7 Phase 6)
	optimizerRegistry *OptimizerRegistry

	// Anomaly detection (v7 Phase 7)
	anomalyDetector *AnomalyDetector

	// Security scanner (v7 Phase 8)
	securityScanner *SecurityScanner

	// Pod health (v7 Phase 10)
	podHealthScanner *PodHealthScanner

	// Ingress scanner
	ingressScanner *IngressScanner

	// LLM config API
	llmConfigAPI *LLMConfigAPI

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
		profile:         resolveProfile(getEnvOrDefault("PROFILE", types.ProfileLive)),
	}

	// Initialize prompt templates
	analyzer.initPromptTemplates()

	// Initialize Prometheus metrics
	analyzer.initMetrics()

	// Initialize mock source; it does nothing until the profile is switched
	// to mock (either at startup via PROFILE=mock or at runtime via the API).
	interval := getDurationOrDefault("MOCK_INTERVAL", 20*time.Second)
	analyzer.mockSource = newMockSource(analyzer, interval)

	return analyzer, nil
}

// resolveProfile normalizes the incoming profile string to a known value,
// defaulting to "live" for anything unrecognized. This keeps PROFILE=foo
// from producing undefined behavior.
func resolveProfile(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case types.ProfileMock:
		return types.ProfileMock
	default:
		return types.ProfileLive
	}
}

// getProfile returns the current profile ("live" or "mock").
func (a *Analyzer) getProfile() string {
	a.profileMu.RLock()
	defer a.profileMu.RUnlock()
	return a.profile
}

// setProfile changes the active profile. When transitioning into "mock" it
// immediately generates a mock report so the dashboard gets instant
// feedback; when transitioning out of "mock" it clears the current report so
// the live analysis loop can repopulate it on the next tick (and the
// dashboard shows an awaiting/diagnostic state in the meantime).
// Returns the newly-active profile or an error if the requested profile is
// not recognized.
func (a *Analyzer) setProfile(p string) (string, error) {
	normalized := resolveProfile(p)
	// We want to reject unknown values rather than silently coerce to live.
	if strings.ToLower(strings.TrimSpace(p)) != normalized {
		return "", fmt.Errorf("unknown profile %q (expected %q or %q)", p, types.ProfileLive, types.ProfileMock)
	}

	a.profileMu.Lock()
	previous := a.profile
	a.profile = normalized
	a.profileMu.Unlock()

	if previous == normalized {
		return normalized, nil
	}

	log.Info().Str("from", previous).Str("to", normalized).Msg("Profile switched")

	if normalized == types.ProfileMock {
		// Drop any live report — the dashboard should show mock data only.
		a.reportMu.Lock()
		a.latestReport = nil
		a.previousReport = nil
		a.reportMu.Unlock()
		// Populate ALL v7 handlers with synthetic data so every dashboard
		// page shows plausible content during the demo.
		a.mockSource.populateAllHandlers()
		// Generate an immediate mock health report.
		a.mockSource.generateAndBroadcast()
	} else {
		// Switched back to live. Clear all mock data so real scans start fresh.
		a.mockSource.clearAllHandlers()
		a.reportMu.Lock()
		a.latestReport = nil
		a.previousReport = nil
		a.reportMu.Unlock()
	}
	return normalized, nil
}

// recordCollectorSuccess / recordCollectorError / recordLLMSuccess /
// recordLLMError update diagnostics under a write lock. They're tiny
// helpers so the runAnalysis flow can just call them at decision points.
func (a *Analyzer) recordCollectorSuccess() {
	now := time.Now()
	a.diagMu.Lock()
	a.collectorReachable = true
	a.collectorLastOKAt = &now
	a.collectorLastError = ""
	a.diagMu.Unlock()
}

func (a *Analyzer) recordCollectorError(err error) {
	a.diagMu.Lock()
	a.collectorReachable = false
	a.collectorLastError = err.Error()
	a.diagMu.Unlock()
}

func (a *Analyzer) recordLLMSuccess() {
	now := time.Now()
	a.diagMu.Lock()
	a.llmReachable = true
	a.llmLastOKAt = &now
	a.llmLastError = ""
	a.diagMu.Unlock()
}

func (a *Analyzer) recordLLMError(err error) {
	a.diagMu.Lock()
	a.llmReachable = false
	a.llmLastError = err.Error()
	a.diagMu.Unlock()
}

func (a *Analyzer) recordAnalysisOutcome(err error) {
	now := time.Now()
	a.diagMu.Lock()
	a.lastAnalysisAt = &now
	if err != nil {
		a.lastAnalysisError = err.Error()
	} else {
		a.lastAnalysisError = ""
	}
	a.diagMu.Unlock()
}

// buildReportStatus snapshots the current diagnostics into a ReportStatus
// block. The state is derived from the presence of scores and the
// reachability of the upstream dependencies.
func (a *Analyzer) buildReportStatus(hasScores bool) *types.ReportStatus {
	a.diagMu.RLock()
	defer a.diagMu.RUnlock()

	profile := a.getProfile()
	status := &types.ReportStatus{
		Profile:           profile,
		LastAnalysisAt:    a.lastAnalysisAt,
		LastAnalysisError: a.lastAnalysisError,
		Collector: types.ComponentHealth{
			Reachable: a.collectorReachable,
			Endpoint:  a.config.CollectorURL,
			LastOKAt:  a.collectorLastOKAt,
			LastError: a.collectorLastError,
		},
		LLM: types.ComponentHealth{
			Reachable: a.llmReachable,
			Endpoint:  a.config.LLMEndpoint,
			LastOKAt:  a.llmLastOKAt,
			LastError: a.llmLastError,
		},
	}

	switch {
	case profile == types.ProfileMock:
		status.State = types.StateOK
		status.Message = "Demo mode: synthetic data is being served. No real cluster analysis is running."
	case hasScores:
		status.State = types.StateOK
		status.Message = "Live analysis is up to date."
	case a.lastAnalysisAt == nil:
		status.State = types.StateAwaiting
		status.Message = "Awaiting first cluster analysis."
	case !a.collectorReachable:
		status.State = types.StateError
		status.Message = "Collector unreachable — cannot fetch cluster telemetry."
	case !a.llmReachable:
		status.State = types.StateDegraded
		status.Message = "LLM unreachable — cluster telemetry is flowing but no AI insights are available."
	default:
		status.State = types.StateDegraded
		status.Message = "Scores unavailable — see lastAnalysisError for details."
	}

	return status
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
	log.Info().
		Str("cluster", a.config.ClusterID).
		Str("profile", a.getProfile()).
		Msg("Starting analyzer")

	// Start analysis loop. It no-ops while the profile is "mock" — we still
	// keep it running so a switch back to "live" takes effect on the next
	// tick without having to restart any goroutines.
	a.wg.Add(1)
	go a.analysisLoop(ctx)

	// Start the mock source goroutine. It also no-ops while the profile is
	// "live"; switching to "mock" is sufficient to make it produce data.
	a.mockSource.Start(ctx)

	// If the initial profile is "mock", emit a first synthetic report
	// immediately so the dashboard doesn't have to wait for the first tick.
	if a.getProfile() == types.ProfileMock {
		a.mockSource.generateAndBroadcast()
	}

	// Start background scanners (security, pod health, anomaly, optimizers).
	// These populate the v7 feature pages with real data from K8s API and
	// Prometheus. They're no-ops when the profile is "mock".
	a.wg.Add(1)
	go a.startBackgroundScans(ctx)

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

// ---------------------------------------------------------------------------
// Background scanners — populate Security, Pod Health, Anomaly, Optimization,
// Errors, and Incidents pages from existing K8s API and Prometheus data.
// ---------------------------------------------------------------------------

// startBackgroundScans runs the v7 scanners on independent intervals. Each
// scan runs only when the profile is "live" — mock mode generates its own
// synthetic data. After each scan, findings are bridged into the Correlator
// as Signals so the Incidents page receives correlated data.
func (a *Analyzer) startBackgroundScans(ctx context.Context) {
	defer a.wg.Done()

	secInterval := getDurationOrDefault("SCAN_SECURITY_INTERVAL", 5*time.Minute)
	podInterval := getDurationOrDefault("SCAN_PODHEALTH_INTERVAL", 2*time.Minute)
	anomalyInterval := getDurationOrDefault("SCAN_ANOMALY_INTERVAL", 3*time.Minute)
	optInterval := getDurationOrDefault("SCAN_OPTIMIZER_INTERVAL", 10*time.Minute)

	secTicker := time.NewTicker(secInterval)
	podTicker := time.NewTicker(podInterval)
	anomalyTicker := time.NewTicker(anomalyInterval)
	optTicker := time.NewTicker(optInterval)
	defer secTicker.Stop()
	defer podTicker.Stop()
	defer anomalyTicker.Stop()
	defer optTicker.Stop()

	log.Info().
		Dur("security", secInterval).
		Dur("podHealth", podInterval).
		Dur("anomaly", anomalyInterval).
		Dur("optimizer", optInterval).
		Msg("Background scanners started")

	// Run all scans once immediately at startup.
	a.runSecurityScan(ctx)
	a.runPodHealthScan(ctx)
	a.runIngressScan(ctx)
	a.runAnomalyDetection()
	a.runOptimizers()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-secTicker.C:
			a.runSecurityScan(ctx)
		case <-podTicker.C:
			a.runPodHealthScan(ctx)
		case <-anomalyTicker.C:
			a.runAnomalyDetection()
		case <-optTicker.C:
			a.runOptimizers()
		}
	}
}

// runSecurityScan triggers a security scan and bridges findings → Correlator.
func (a *Analyzer) runSecurityScan(ctx context.Context) {
	if a.getProfile() != types.ProfileLive || a.securityScanner == nil {
		return
	}
	a.securityScanner.RunScan(ctx)

	// Bridge findings to Correlator for incident creation.
	if a.correlator == nil {
		return
	}
	a.securityScanner.mu.RLock()
	defer a.securityScanner.mu.RUnlock()
	for _, f := range a.securityScanner.findings {
		a.correlator.IngestSignal(Signal{
			ID:        fmt.Sprintf("sec-%d", f.ID),
			Timestamp: f.DetectedAt,
			Source:    "security",
			Severity:  f.Severity,
			Namespace: extractNamespace(f.Affected),
			Kind:      f.Category,
			Title:     f.Title,
			Details:   f.Description,
		})
	}
}

// runPodHealthScan triggers a pod health scan and bridges issues → Correlator + ErrorAggregator.
func (a *Analyzer) runPodHealthScan(ctx context.Context) {
	if a.getProfile() != types.ProfileLive || a.podHealthScanner == nil {
		return
	}
	a.podHealthScanner.Scan(ctx)

	a.podHealthScanner.mu.RLock()
	report := a.podHealthScanner.report
	a.podHealthScanner.mu.RUnlock()
	if report == nil {
		return
	}

	for _, cat := range report.Categories {
		if len(cat.Pods) == 0 {
			continue
		}
		severity := "medium"
		kind := cat.Name
		if kind == "crashloop" || kind == "oomkilled" {
			severity = "high"
		}
		for _, pod := range cat.Pods {
			// Bridge to Correlator
			if a.correlator != nil {
				a.correlator.IngestSignal(Signal{
					ID:        fmt.Sprintf("pod-%s-%s-%s", kind, pod.Namespace, pod.Name),
					Timestamp: time.Now(),
					Source:    "k8s",
					Severity:  severity,
					Service:   pod.Name,
					Namespace: pod.Namespace,
					Pod:       pod.Name,
					Kind:      kind,
					Title:     fmt.Sprintf("%s: %s/%s", cat.Name, pod.Namespace, pod.Name),
					Details:   pod.Reason + ": " + pod.Message,
				})
			}
			// Bridge to ErrorAggregator
			if a.errorAggregator != nil {
				a.errorAggregator.Ingest(IngestEvent{
					Timestamp: time.Now(),
					Namespace: pod.Namespace,
					Pod:       pod.Name,
					Service:   pod.Name,
					Level:     "error",
					Message:   pod.Message,
					Reason:    kind,
					Fingerprint: fmt.Sprintf("%s/%s/%s", kind, pod.Namespace, pod.Name),
				})
			}
		}
	}
}

// runIngressScan triggers an Ingress resource scan.
func (a *Analyzer) runIngressScan(ctx context.Context) {
	if a.getProfile() != types.ProfileLive || a.ingressScanner == nil {
		return
	}
	a.ingressScanner.Scan(ctx)
}

// runAnomalyDetection triggers anomaly detection and bridges detections → Correlator.
func (a *Analyzer) runAnomalyDetection() {
	if a.getProfile() != types.ProfileLive || a.anomalyDetector == nil {
		return
	}
	a.anomalyDetector.RunDetection()

	if a.correlator == nil {
		return
	}
	a.anomalyDetector.mu.RLock()
	anomalies := make([]*Anomaly, 0, len(a.anomalyDetector.anomalies))
	for _, an := range a.anomalyDetector.anomalies {
		if an.Status == "active" {
			anomalies = append(anomalies, an)
		}
	}
	a.anomalyDetector.mu.RUnlock()
	for _, anomaly := range anomalies {
		kind := "spike"
		if anomaly.Score < 0 {
			kind = "drop"
		}
		a.correlator.IngestSignal(Signal{
			ID:        fmt.Sprintf("anomaly-%d", anomaly.ID),
			Timestamp: anomaly.DetectedAt,
			Source:    "anomaly",
			Severity:  anomaly.Severity,
			Service:   anomaly.Service,
			Namespace: anomaly.Namespace,
			Kind:      kind,
			Title:     fmt.Sprintf("Anomaly: %s %s (z=%.1f)", anomaly.Metric, kind, anomaly.Score),
			Details:   fmt.Sprintf("expected=%.2f observed=%.2f", anomaly.Expected, anomaly.Observed),
		})
	}
}

// runOptimizers triggers all optimizer runs.
func (a *Analyzer) runOptimizers() {
	if a.getProfile() != types.ProfileLive || a.optimizerRegistry == nil {
		return
	}
	a.optimizerRegistry.RunAll()
}

// extractNamespace tries to pull a namespace from affected resource strings
// like "kube-system/pod-name". Returns empty string if none found.
func extractNamespace(resources []string) string {
	for _, r := range resources {
		if i := strings.Index(r, "/"); i > 0 {
			return r[:i]
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Error event ingestion from collector telemetry
// ---------------------------------------------------------------------------

// ingestWarningEventsAsErrors feeds the ErrorAggregator from K8s warning
// events that the collector already captures. This populates the Errors page
// without requiring NATS or collector-podlogs.
func (a *Analyzer) ingestWarningEventsAsErrors(events []types.TelemetryEvent) {
	if a.errorAggregator == nil {
		return
	}
	for _, evt := range events {
		if evt.Type != "Warning" {
			continue
		}
		a.errorAggregator.Ingest(IngestEvent{
			Timestamp:   evt.Timestamp,
			Namespace:   evt.InvolvedObject.Namespace,
			Pod:         evt.InvolvedObject.Name,
			Service:     evt.InvolvedObject.Name,
			Level:       "warn",
			Message:     evt.Message,
			Reason:      evt.Reason,
			Fingerprint: fmt.Sprintf("%s/%s/%s", evt.Reason, evt.InvolvedObject.Namespace, evt.InvolvedObject.Name),
		})
	}
}

// runAnalysis performs a full cluster analysis. It is a no-op when the
// profile is set to "mock" — in that case the mockSource goroutine is the
// one producing reports and we don't want to poison latestReport with real
// (or partial) data.
func (a *Analyzer) runAnalysis(ctx context.Context) {
	if a.getProfile() == types.ProfileMock {
		return
	}

	start := time.Now()
	a.analysisRuns.Inc()

	log.Info().Msg("Starting cluster analysis")

	// Fetch telemetry from collector
	events, err := a.fetchEvents(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch events")
		a.analysisErrors.Inc()
		a.recordCollectorError(err)
		a.recordAnalysisOutcome(err)
		// Publish a diagnostic report so the dashboard can render the error.
		a.publishReport(a.buildDiagnosticReport())
		return
	}

	metrics, err := a.fetchMetrics(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch metrics")
		a.analysisErrors.Inc()
		a.recordCollectorError(err)
		a.recordAnalysisOutcome(err)
		a.publishReport(a.buildDiagnosticReport())
		return
	}

	// Both collector calls succeeded.
	a.recordCollectorSuccess()

	// Feed warning events into the ErrorAggregator so the Errors page shows
	// K8s-native error patterns (CrashLoopBackOff, OOMKilled, etc.) without
	// needing NATS or collector-podlogs.
	a.ingestWarningEventsAsErrors(events)

	log.Info().Int("events", len(events)).Int("metrics", len(metrics)).Msg("Fetched telemetry")

	correlatedEvents, err := a.fetchCorrelatedEvents(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch correlated events")
	}

	// Build analysis context - limit data so the prompt fits small LLM
	// context windows (e.g. llama.cpp defaults to 4k tokens). The caps are
	// env-tunable: bigger models can raise them for richer analysis.
	//
	// Defaults (5 / 10 / 3) produce prompts well under 4k tokens which is
	// the typical minimum for self-hosted backends. For OpenAI-class models
	// you can reasonably set LLM_MAX_EVENTS=50, LLM_MAX_METRICS=100,
	// LLM_MAX_CORRELATED=20.
	maxEvents := getEnvIntOrDefault("LLM_MAX_EVENTS", 5)
	maxMetrics := getEnvIntOrDefault("LLM_MAX_METRICS", 10)
	maxCorrelated := getEnvIntOrDefault("LLM_MAX_CORRELATED", 3)

	warningEvents := filterWarningEvents(events)
	if len(warningEvents) > maxEvents {
		warningEvents = warningEvents[len(warningEvents)-maxEvents:]
	}
	limitedMetrics := metrics
	if len(limitedMetrics) > maxMetrics {
		limitedMetrics = limitedMetrics[len(limitedMetrics)-maxMetrics:]
	}
	limitedCorrelated := correlatedEvents
	if len(limitedCorrelated) > maxCorrelated {
		limitedCorrelated = limitedCorrelated[len(limitedCorrelated)-maxCorrelated:]
	}
	// Each correlated event carries up to 50 pod log lines. Trim them to
	// keep the prompt small — small-context backends can't afford the full
	// tail. LLM_MAX_LOG_LINES=0 disables log lines entirely.
	maxLogLines := getEnvIntOrDefault("LLM_MAX_LOG_LINES", 10)
	if maxLogLines >= 0 {
		trimmed := make([]types.CorrelatedEvidence, len(limitedCorrelated))
		for i, ev := range limitedCorrelated {
			trimmed[i] = ev
			if len(ev.LogLines) > maxLogLines {
				trimmed[i].LogLines = ev.LogLines[len(ev.LogLines)-maxLogLines:]
			}
		}
		limitedCorrelated = trimmed
	}

	analysisCtx := map[string]any{
		"ClusterID":        a.config.ClusterID,
		"Timestamp":        time.Now().Format(time.RFC3339),
		"Events":           warningEvents,
		"Metrics":          limitedMetrics,
		"CorrelatedEvents": limitedCorrelated,
	}

	// Run LLM analysis
	llmResponse, llmErr := a.runLLMAnalysis(ctx, "rca", analysisCtx)
	if llmErr != nil {
		log.Error().Err(llmErr).Msg("Failed to run LLM analysis")
		a.analysisErrors.Inc()
		a.recordLLMError(llmErr)
		// Continue with basic analysis — scores will be nil.
	} else {
		a.recordLLMSuccess()
	}

	// Build health report. When llmResponse is nil, scores will be nil and
	// the dashboard will render a diagnostic panel instead of fake numbers.
	report := a.buildHealthReport(events, metrics, llmResponse)
	report.Status = a.buildReportStatus(report.Scores != nil)

	// Store report and compute trends only when both reports have scores.
	a.reportMu.Lock()
	if a.latestReport != nil && a.latestReport.Scores != nil && report.Scores != nil {
		a.previousReport = a.latestReport
		report.Trends = types.HealthTrends{
			Overall:      report.Scores.Overall - a.previousReport.Scores.Overall,
			Reliability:  report.Scores.Reliability - a.previousReport.Scores.Reliability,
			Security:     report.Scores.Security - a.previousReport.Scores.Security,
			Cost:         report.Scores.Cost - a.previousReport.Scores.Cost,
			Architecture: report.Scores.Architecture - a.previousReport.Scores.Architecture,
		}
	} else {
		if a.latestReport != nil {
			a.previousReport = a.latestReport
		}
		report.Trends = types.HealthTrends{}
	}

	a.latestReport = report
	a.reportHistory = append(a.reportHistory, report)
	if len(a.reportHistory) > 100 {
		a.reportHistory = a.reportHistory[1:] // keep last 100
	}
	a.reportMu.Unlock()

	// Update metrics
	if report.Scores != nil {
		a.healthScore.Set(float64(report.Scores.Overall))
	}
	a.analysisDuration.Observe(time.Since(start).Seconds())

	a.recordAnalysisOutcome(llmErr)

	logEvent := log.Info()
	if report.Scores != nil {
		logEvent = logEvent.Int("healthScore", report.Scores.Overall)
	} else {
		logEvent = logEvent.Str("healthScore", "unavailable")
	}
	logEvent.
		Int("issues", len(report.TopIssues)).
		Int("recommendations", len(report.Recommendations)).
		Dur("duration", time.Since(start)).
		Msg("Analysis complete")

	// Broadcast to SSE clients
	a.broadcastReport(report)
}

// broadcastReport sends a report to all SSE subscribers. Used by both the
// live analysis loop and the mock source.
func (a *Analyzer) broadcastReport(report *types.ClusterHealthReport) {
	a.subMu.RLock()
	defer a.subMu.RUnlock()
	for ch := range a.subscribers {
		select {
		case ch <- report:
		default:
			// Client slow, drop the report
		}
	}
}

// publishReport stores a report as the latest report, appends to history,
// and broadcasts. Used when a collector failure means we want to surface a
// diagnostic-only report to connected clients.
func (a *Analyzer) publishReport(report *types.ClusterHealthReport) {
	a.reportMu.Lock()
	if a.latestReport != nil {
		a.previousReport = a.latestReport
	}
	a.latestReport = report
	a.reportHistory = append(a.reportHistory, report)
	if len(a.reportHistory) > 100 {
		a.reportHistory = a.reportHistory[1:]
	}
	a.reportMu.Unlock()
	a.broadcastReport(report)
}

// buildDiagnosticReport produces a minimal report containing only status
// information. It's used when the collector is unreachable (so we can't
// build a real report) but we still want the dashboard to show diagnostics.
func (a *Analyzer) buildDiagnosticReport() *types.ClusterHealthReport {
	return &types.ClusterHealthReport{
		ClusterID:        a.config.ClusterID,
		Timestamp:        time.Now(),
		Scores:           nil,
		Summary:          types.ClusterSummary{Namespaces: make(map[string]*types.NamespaceStats)},
		TopIssues:        []types.Issue{},
		Recommendations:  []types.Recommendation{},
		SecurityFindings: []types.SecurityFinding{},
		Status:           a.buildReportStatus(false),
	}
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
	// Scores intentionally start as nil. They are only populated when the
	// LLM returns a parsable healthScores block below. When the LLM is
	// unreachable (or its output is unparsable) the report is emitted with
	// scores == nil so the dashboard can render a diagnostic panel instead
	// of inventing numbers.
	report := &types.ClusterHealthReport{
		ClusterID: a.config.ClusterID,
		Timestamp: time.Now(),
		Scores:    nil,
		Summary: types.ClusterSummary{
			WarningEvents: len(filterWarningEvents(events)),
			Namespaces:    make(map[string]*types.NamespaceStats),
		},
		// Initialize as empty slices so JSON marshals to [] not null
		TopIssues:        []types.Issue{},
		Recommendations:  []types.Recommendation{},
		SecurityFindings: []types.SecurityFinding{},
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

	// Extract scores from LLM response. Only allocate the Scores struct
	// when the LLM actually returned a healthScores block — otherwise
	// leave it as nil so the dashboard knows AI insights are unavailable.
	if llmResponse != nil {
		if rawScores, ok := llmResponse["healthScores"].(map[string]any); ok {
			scores := &types.HealthScores{}
			if v, ok := rawScores["reliability"].(float64); ok {
				scores.Reliability = int(v)
			}
			if v, ok := rawScores["security"].(float64); ok {
				scores.Security = int(v)
			}
			if v, ok := rawScores["cost"].(float64); ok {
				scores.Cost = int(v)
			}
			if v, ok := rawScores["architecture"].(float64); ok {
				scores.Architecture = int(v)
			}
			scores.Overall = types.CalculateOverallScore(*scores)
			report.Scores = scores
		}

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
	mux.HandleFunc("/api/v1/health/breakdown", a.handleHealthBreakdown)
	mux.HandleFunc("/api/v1/scores", a.handleScores)
	mux.HandleFunc("/api/v1/recommendations", a.handleRecommendations)
	mux.HandleFunc("/api/v1/issues", a.handleIssues)
	mux.HandleFunc("/api/v1/health/stream", a.handleHealthStream)
	mux.HandleFunc("/api/v1/analysis/trigger", a.handleTriggerAnalysis)
	mux.HandleFunc("/api/v1/events/timeline", a.handleTimeline)
	mux.HandleFunc("/api/v1/history", a.handleHistory)
	mux.HandleFunc("/api/v1/dns/health", a.handleDNSHealth)
	mux.HandleFunc("/api/v1/pods/", a.handlePodLogs)
	// Profile + diagnostics
	mux.HandleFunc("/api/v1/profile", a.handleProfile)
	mux.HandleFunc("/api/v1/status", a.handleStatus)
	mux.HandleFunc("POST /api/v1/errors/analyze", a.handleErrorsAnalyze)

	// v7 Phase 3: Error aggregator routes
	if a.errorAggregator != nil {
		a.errorAggregator.RegisterRoutes(mux)
	}

	// v7 Phase 4: LB log aggregator routes
	if a.lbLogAggregator != nil {
		a.lbLogAggregator.RegisterRoutes(mux)
	}

	// Ingress scanner routes
	if a.ingressScanner != nil {
		a.ingressScanner.RegisterRoutes(mux)
	}

	// v7 Phase 6: Optimizer routes
	if a.optimizerRegistry != nil {
		a.optimizerRegistry.RegisterRoutes(mux)
	}

	// v7 Phase 7: Anomaly detection routes
	if a.anomalyDetector != nil {
		a.anomalyDetector.RegisterRoutes(mux)
	}

	// v7 Phase 8: Security scanner routes
	if a.securityScanner != nil {
		a.securityScanner.RegisterRoutes(mux)
	}

	// v7 Phase 10: Pod health routes
	if a.podHealthScanner != nil {
		a.podHealthScanner.RegisterRoutes(mux)
	}

	// LLM config API
	if a.llmConfigAPI != nil {
		a.llmConfigAPI.RegisterRoutes(mux)
	}

	// v7 Phase 5: Correlator + RCA routes
	if a.correlator != nil {
		a.correlator.RegisterRoutes(mux)
	}
	if a.rcaEngine != nil {
		a.rcaEngine.RegisterRoutes(mux)
	}

	// v7 Phase 1: Workload browser routes
	if a.workloadHandler != nil {
		a.workloadHandler.RegisterRoutes(mux)

		// v7 Phase 2: WS routes (logs, exec) + write actions
		execEnabled := getEnvOrDefault("EXEC_ENABLED", "false") == "true"
		writeEnabled := getEnvOrDefault("WRITE_ACTIONS_ENABLED", "false") == "true"
		protectedStr := getEnvOrDefault("PROTECTED_NAMESPACES", "kube-system,kube-public,kube-node-lease")
		execCmdsStr := getEnvOrDefault("EXEC_ALLOWED_COMMANDS", "/bin/sh,/bin/bash,/bin/ash")
		var restCfg *rest.Config
		if a.workloadHandler.restConfig == nil {
			// Try to get restConfig from the K8s client setup
			if rc, err := rest.InClusterConfig(); err == nil {
				restCfg = rc
			}
		} else {
			restCfg = a.workloadHandler.restConfig
		}
		a.workloadHandler.RegisterWSRoutes(mux, restCfg, wsConfig{
			ExecEnabled:         execEnabled,
			ExecAllowedCommands: strings.Split(execCmdsStr, ","),
			WriteEnabled:        writeEnabled,
			ProtectedNamespaces: strings.Split(protectedStr, ","),
		})
		log.Info().
			Bool("exec", execEnabled).
			Bool("writeActions", writeEnabled).
			Msg("Registered workload browser API routes")
	}

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

// handleHealthReport returns the full health report. If no report has been
// produced yet, returns a synthetic diagnostic report (200 OK) rather than a
// 503 so the dashboard can render the status block and help the operator
// understand why data is missing.
func (a *Analyzer) handleHealthReport(w http.ResponseWriter, r *http.Request) {
	a.reportMu.RLock()
	report := a.latestReport
	a.reportMu.RUnlock()

	if report == nil {
		report = a.buildDiagnosticReport()
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

	// Send initial report immediately. If no real report exists yet, send
	// a synthetic diagnostic report so clients see the status block right
	// away instead of a blank screen.
	a.reportMu.RLock()
	initialReport := a.latestReport
	a.reportMu.RUnlock()
	if initialReport == nil {
		initialReport = a.buildDiagnosticReport()
	}
	data, _ := json.Marshal(initialReport)
	fmt.Fprintf(w, "data: %s\n\n", string(data))
	flusher.Flush()

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

// handleHealthBreakdown returns a per-dimension breakdown showing which
// cluster resources and conditions contribute to each health score. This
// is built from real data (security findings, pod health, optimizer recs,
// anomalies) — NOT from the LLM. It gives the operator drill-down
// visibility into why each score is what it is.
func (a *Analyzer) handleHealthBreakdown(w http.ResponseWriter, r *http.Request) {
	type factor struct {
		Name      string   `json:"name"`
		Impact    int      `json:"impact"`
		Resources []string `json:"resources,omitempty"`
		Severity  string   `json:"severity,omitempty"`
	}
	type dimension struct {
		Score   int      `json:"score"`
		Factors []factor `json:"factors"`
	}
	type breakdown struct {
		Reliability  dimension `json:"reliability"`
		Security     dimension `json:"security"`
		Cost         dimension `json:"cost"`
		Architecture dimension `json:"architecture"`
	}

	bd := breakdown{}

	// Current scores from latest report (if any)
	a.reportMu.RLock()
	if a.latestReport != nil && a.latestReport.Scores != nil {
		bd.Reliability.Score = a.latestReport.Scores.Reliability
		bd.Security.Score = a.latestReport.Scores.Security
		bd.Cost.Score = a.latestReport.Scores.Cost
		bd.Architecture.Score = a.latestReport.Scores.Architecture
	}
	a.reportMu.RUnlock()

	// --- Reliability factors from Pod Health ---
	if a.podHealthScanner != nil {
		a.podHealthScanner.mu.RLock()
		if rpt := a.podHealthScanner.report; rpt != nil {
			for _, cat := range rpt.Categories {
				if cat.Count == 0 {
					continue
				}
				impact := -3 * cat.Count
				sev := "medium"
				switch cat.Name {
				case "crashloop", "oomkilled":
					impact = -5 * cat.Count
					sev = "high"
				case "evicted":
					impact = -4 * cat.Count
				case "pending":
					impact = -2 * cat.Count
				case "completed", "terminating":
					impact = -1 * cat.Count
					sev = "low"
				}
				resources := make([]string, 0, len(cat.Pods))
				for _, p := range cat.Pods {
					resources = append(resources, p.Namespace+"/"+p.Name)
				}
				bd.Reliability.Factors = append(bd.Reliability.Factors, factor{
					Name:      fmt.Sprintf("%s pods (%d)", cat.Name, cat.Count),
					Impact:    impact,
					Resources: resources,
					Severity:  sev,
				})
			}
		}
		a.podHealthScanner.mu.RUnlock()
	}

	// --- Security factors from Security Scanner ---
	if a.securityScanner != nil {
		a.securityScanner.mu.RLock()
		bySev := map[string][]string{}
		for _, f := range a.securityScanner.findings {
			bySev[f.Severity] = append(bySev[f.Severity], f.Title)
		}
		a.securityScanner.mu.RUnlock()
		for sev, titles := range bySev {
			impact := -2 * len(titles)
			if sev == "critical" {
				impact = -8 * len(titles)
			} else if sev == "high" {
				impact = -5 * len(titles)
			} else if sev == "low" {
				impact = -1 * len(titles)
			}
			bd.Security.Factors = append(bd.Security.Factors, factor{
				Name:      fmt.Sprintf("%s severity findings (%d)", sev, len(titles)),
				Impact:    impact,
				Resources: titles,
				Severity:  sev,
			})
		}
	}

	// --- Cost factors from Optimizer ---
	if a.optimizerRegistry != nil {
		a.optimizerRegistry.mu.RLock()
		byType := map[string]struct {
			count   int
			savings float64
			targets []string
		}{}
		for _, rec := range a.optimizerRegistry.recommendations {
			if rec.Status == "dismissed" || rec.Status == "applied" {
				continue
			}
			entry := byType[rec.Type]
			entry.count++
			entry.savings += rec.EstimatedSavingsMonthly
			entry.targets = append(entry.targets, rec.Target.Namespace+"/"+rec.Target.Name)
			byType[rec.Type] = entry
		}
		a.optimizerRegistry.mu.RUnlock()
		for typ, entry := range byType {
			impact := -2 * entry.count
			bd.Cost.Factors = append(bd.Cost.Factors, factor{
				Name:      fmt.Sprintf("%s opportunities (%d, ~$%.0f/mo)", typ, entry.count, entry.savings),
				Impact:    impact,
				Resources: entry.targets,
				Severity:  "medium",
			})
		}
	}

	// --- Architecture factors from Anomalies ---
	if a.anomalyDetector != nil {
		a.anomalyDetector.mu.RLock()
		var activeAnomalies []string
		for _, an := range a.anomalyDetector.anomalies {
			if an.Status == "active" {
				activeAnomalies = append(activeAnomalies, fmt.Sprintf("%s/%s %s (z=%.1f)", an.Namespace, an.Service, an.Metric, an.Score))
			}
		}
		a.anomalyDetector.mu.RUnlock()
		if len(activeAnomalies) > 0 {
			bd.Architecture.Factors = append(bd.Architecture.Factors, factor{
				Name:      fmt.Sprintf("Active anomalies (%d)", len(activeAnomalies)),
				Impact:    -3 * len(activeAnomalies),
				Resources: activeAnomalies,
				Severity:  "medium",
			})
		}
	}

	// --- Architecture: also count incidents as a factor ---
	if a.correlator != nil {
		a.correlator.mu.RLock()
		openCount := 0
		for _, inc := range a.correlator.incidents {
			if inc.Status == "open" || inc.Status == "investigating" {
				openCount++
			}
		}
		a.correlator.mu.RUnlock()
		if openCount > 0 {
			bd.Architecture.Factors = append(bd.Architecture.Factors, factor{
				Name:     fmt.Sprintf("Open incidents (%d)", openCount),
				Impact:   -2 * openCount,
				Severity: "high",
			})
		}
	}

	// Default empty slices so JSON returns [] not null
	if bd.Reliability.Factors == nil {
		bd.Reliability.Factors = []factor{}
	}
	if bd.Security.Factors == nil {
		bd.Security.Factors = []factor{}
	}
	if bd.Cost.Factors == nil {
		bd.Cost.Factors = []factor{}
	}
	if bd.Architecture.Factors == nil {
		bd.Architecture.Factors = []factor{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bd)
}

// handleScores returns just the health scores. Returns null (JSON) when
// scores are unavailable so clients can discriminate unambiguously.
func (a *Analyzer) handleScores(w http.ResponseWriter, r *http.Request) {
	a.reportMu.RLock()
	report := a.latestReport
	a.reportMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if report == nil || report.Scores == nil {
		w.Write([]byte("null"))
		return
	}
	json.NewEncoder(w).Encode(report.Scores)
}

// handleRecommendations returns the recommendations. Returns [] when no
// report is available — the Summary/recommendations slice is a list, so
// "no data" and "empty list" are indistinguishable at this endpoint.
// Clients that need to know should consult /api/v1/status.
func (a *Analyzer) handleRecommendations(w http.ResponseWriter, r *http.Request) {
	a.reportMu.RLock()
	report := a.latestReport
	a.reportMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if report == nil {
		w.Write([]byte("[]"))
		return
	}
	json.NewEncoder(w).Encode(report.Recommendations)
}

// handleIssues returns the top issues. Same semantics as
// handleRecommendations — [] when no report.
func (a *Analyzer) handleIssues(w http.ResponseWriter, r *http.Request) {
	a.reportMu.RLock()
	report := a.latestReport
	a.reportMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if report == nil {
		w.Write([]byte("[]"))
		return
	}
	json.NewEncoder(w).Encode(report.TopIssues)
}

// handleErrorsAnalyze sends the top error groups to the LLM for analysis.
// The LLM returns a structured analysis per group which is stored in
// aiSummary and returned to the caller.
func (a *Analyzer) handleErrorsAnalyze(w http.ResponseWriter, r *http.Request) {
	if a.errorAggregator == nil {
		http.Error(w, "error aggregator not initialized", http.StatusServiceUnavailable)
		return
	}

	// Collect top 10 error groups by count
	a.errorAggregator.mu.RLock()
	type kv struct {
		fp string
		g  *ErrorGroup
	}
	var sorted []kv
	for fp, g := range a.errorAggregator.groups {
		sorted = append(sorted, kv{fp, g})
	}
	a.errorAggregator.mu.RUnlock()

	sort.Slice(sorted, func(i, j int) bool { return sorted[i].g.Count > sorted[j].g.Count })
	if len(sorted) > 10 {
		sorted = sorted[:10]
	}

	if len(sorted) == 0 {
		writeJSON(w, map[string]any{"analyzed": 0, "message": "No error groups to analyze"})
		return
	}

	// Build prompt
	var sb strings.Builder
	sb.WriteString("Analyze these Kubernetes cluster error groups. For each, provide:\n")
	sb.WriteString("1. Root cause analysis\n2. Impact assessment\n3. Recommended fix\n\n")
	for i, s := range sorted {
		sb.WriteString(fmt.Sprintf("Error %d: reason=%s service=%s namespace=%s count=%d\n",
			i+1, s.g.Reason, s.g.Service, s.g.Namespace, s.g.Count))
		sb.WriteString(fmt.Sprintf("  Title: %s\n", s.g.Title))
		if s.g.SampleMessage != "" {
			msg := s.g.SampleMessage
			if len(msg) > 200 {
				msg = msg[:200] + "..."
			}
			sb.WriteString(fmt.Sprintf("  Sample: %s\n", msg))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nRespond with a JSON array where each element has: {\"index\": N, \"rootCause\": \"...\", \"impact\": \"...\", \"fix\": \"...\", \"severity\": \"critical|high|medium|low\"}\nOnly output valid JSON.")

	// Call LLM
	analysisCtx := map[string]any{
		"ClusterID": a.config.ClusterID,
		"Timestamp": time.Now().Format(time.RFC3339),
		"Prompt":    sb.String(),
	}

	// Use a simple direct LLM call
	llmReq := types.LLMRequest{
		Model: a.config.LLMModel,
		Messages: []types.LLMMessage{
			{Role: "system", Content: "You are a Kubernetes SRE expert. Analyze error patterns and provide actionable root cause analysis. Always respond with valid JSON only."},
			{Role: "user", Content: sb.String()},
		},
		MaxTokens:   a.config.MaxTokens,
		Temperature: a.config.Temperature,
	}

	reqBody, _ := json.Marshal(llmReq)
	httpReq, err := http.NewRequestWithContext(r.Context(), "POST", a.config.LLMEndpoint+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, "failed to create LLM request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if a.config.LLMAPIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.config.LLMAPIKey)
	}

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		http.Error(w, "LLM request failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("LLM returned %d: %s", resp.StatusCode, string(body)), http.StatusBadGateway)
		return
	}

	var llmResp types.LLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		http.Error(w, "failed to decode LLM response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if len(llmResp.Choices) == 0 {
		http.Error(w, "no choices in LLM response", http.StatusInternalServerError)
		return
	}

	content := llmResp.Choices[0].Message.Content
	content = extractJSON(content)

	// Parse the array response
	var analyses []struct {
		Index    int    `json:"index"`
		RootCause string `json:"rootCause"`
		Impact   string `json:"impact"`
		Fix      string `json:"fix"`
		Severity string `json:"severity"`
	}
	if err := json.Unmarshal([]byte(content), &analyses); err != nil {
		// Try wrapping in array if it's a single object
		var single struct {
			Index    int    `json:"index"`
			RootCause string `json:"rootCause"`
			Impact   string `json:"impact"`
			Fix      string `json:"fix"`
			Severity string `json:"severity"`
		}
		if err2 := json.Unmarshal([]byte(content), &single); err2 == nil {
			analyses = append(analyses, single)
		} else {
			// Store raw response as summary for the first group
			if len(sorted) > 0 {
				a.errorAggregator.mu.Lock()
				sorted[0].g.AISummary = content
				a.errorAggregator.mu.Unlock()
			}
			writeJSON(w, map[string]any{
				"analyzed": 1,
				"raw":      content,
				"model":    a.config.LLMModel,
				"provider": a.config.LLMBackend,
			})
			return
		}
	}

	// Store analysis back on each error group
	a.errorAggregator.mu.Lock()
	for _, analysis := range analyses {
		idx := analysis.Index - 1
		if idx >= 0 && idx < len(sorted) {
			sorted[idx].g.AISummary = fmt.Sprintf("**Root Cause**: %s\n\n**Impact**: %s\n\n**Fix**: %s",
				analysis.RootCause, analysis.Impact, analysis.Fix)
		}
	}
	a.errorAggregator.mu.Unlock()

	_ = analysisCtx // used for potential logging

	writeJSON(w, map[string]any{
		"analyzed": len(analyses),
		"model":    a.config.LLMModel,
		"provider": a.config.LLMBackend,
		"analyses": analyses,
	})
}

// handleProfile handles GET (return current profile) and POST (switch
// profile). POST body is {"profile":"live"|"mock"}.
func (a *Analyzer) handleProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(map[string]any{
			"profile":   a.getProfile(),
			"available": []string{types.ProfileLive, types.ProfileMock},
		})
	case http.MethodPost:
		var body struct {
			Profile string `json:"profile"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
			return
		}
		newProfile, err := a.setProfile(body.Profile)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"profile":   newProfile,
			"available": []string{types.ProfileLive, types.ProfileMock},
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleStatus returns full diagnostics about the analyzer's current state:
// profile, collector reachability, LLM reachability, last analysis
// timestamp and error, and whether scores are currently present.
func (a *Analyzer) handleStatus(w http.ResponseWriter, r *http.Request) {
	a.reportMu.RLock()
	hasReport := a.latestReport != nil
	hasScores := hasReport && a.latestReport.Scores != nil
	a.reportMu.RUnlock()

	status := a.buildReportStatus(hasScores)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":    status,
		"hasReport": hasReport,
		"hasScores": hasScores,
	})
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

	// Stop the mock source goroutine (no-op if it never started).
	if a.mockSource != nil {
		a.mockSource.Stop()
	}

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

	// v7: Try loading unified config file (CI_CONFIG env or /etc/cluster-intel/config.yaml).
	// If available, its values seed the Config below; env vars still override as before.
	ucfg, ucfgErr := ucconfig.LoadFromEnv("/etc/cluster-intel/config.yaml")
	if ucfgErr != nil {
		log.Debug().Err(ucfgErr).Msg("No unified config loaded, using legacy env vars")
	} else {
		log.Info().Str("cluster", ucfg.Cluster.ID).Msg("Loaded unified config")
	}

	// Load configuration
	config := Config{
		ClusterID:        coalesce(getEnvOrDefault("CLUSTER_ID", ""), ucfg.Cluster.ID, "default"),
		CollectorURL:     getEnvOrDefault("COLLECTOR_URL", "http://collector:8080"),
		LLMBackend:       coalesce(getEnvOrDefault("LLM_BACKEND", ""), ucfg.LLM.Provider, "openai"),
		LLMEndpoint:      coalesce(getEnvOrDefault("LLM_ENDPOINT", ""), ucfg.LLM.Endpoint, "https://api.openai.com/v1"),
		LLMModel:         coalesce(getEnvOrDefault("LLM_MODEL", ""), ucfg.LLM.Model, "gpt-4-turbo"),
		LLMAPIKey:        coalesce(os.Getenv("LLM_API_KEY"), ucfg.LLM.APIKey),
		AnalysisInterval: getDurationOrDefault("ANALYSIS_INTERVAL", 5*time.Minute),
		MetricsPort:      getEnvIntOrDefault("METRICS_PORT", ucfg.Server.MetricsPort),
		APIPort:          getEnvIntOrDefault("API_PORT", ucfg.Server.APIPort),
		MaxTokens:        getEnvIntOrDefault("LLM_MAX_TOKENS", ucfg.LLM.MaxTokens),
		Temperature:      getEnvFloatOrDefault("LLM_TEMPERATURE", ucfg.LLM.Temperature),
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

	// v7 Phase 3: Error aggregator
	analyzer.errorAggregator = NewErrorAggregator()

	// v7 Phase 4: LB log aggregator
	analyzer.lbLogAggregator = NewLBLogAggregator()

	// LLM config API (for UI settings page)
	analyzer.llmConfigAPI = NewLLMConfigAPI(
		config.LLMBackend, config.LLMEndpoint, config.LLMModel, config.LLMAPIKey,
		config.MaxTokens, config.Temperature, 1000000,
	)

	// v7 Phase 6-10: Optimizers, anomaly, security, pod health
	promURL := coalesce(os.Getenv("PROMETHEUS_URL"), os.Getenv("PROMETHEUS_ENDPOINT"), "")
	analyzer.optimizerRegistry = NewOptimizerRegistry(promURL, config.ClusterID)
	analyzer.anomalyDetector = NewAnomalyDetector(promURL, config.ClusterID)

	// v7 Phase 5: Correlator + RCA
	analyzer.correlator = NewCorrelator(config.ClusterID, 5*time.Minute)
	analyzer.rcaEngine = NewRCAEngine(ucfg.LLM, analyzer.correlator)
	analyzer.correlator.onNewIncident = func(incidentID int64) {
		if analyzer.rcaEngine != nil && analyzer.rcaEngine.llmClient != nil {
			ctx := context.Background()
			if _, err := analyzer.rcaEngine.Analyze(ctx, incidentID); err != nil {
				log.Warn().Err(err).Int64("incident", incidentID).Msg("Auto-RCA failed")
			}
		}
	}

	// v7 Phase 1: Initialize K8s clients for the workload browser.
	// Tries in-cluster first, falls back to kubeconfig.
	restCfg, k8sErr := rest.InClusterConfig()
	if k8sErr != nil {
		kubeconfigPath := os.Getenv("KUBECONFIG")
		if kubeconfigPath == "" {
			kubeconfigPath = os.Getenv("HOME") + "/.kube/config"
		}
		restCfg, k8sErr = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}
	if k8sErr != nil {
		log.Warn().Err(k8sErr).Msg("K8s client init failed — workload browser disabled")
	} else {
		cs, _ := kubernetes.NewForConfig(restCfg)
		dyn, _ := dynamic.NewForConfig(restCfg)
		disc, _ := discovery.NewDiscoveryClientForConfig(restCfg)
		if cs != nil && dyn != nil && disc != nil {
			analyzer.workloadHandler = NewWorkloadHandler(cs, dyn, disc)
			analyzer.workloadHandler.restConfig = restCfg
			analyzer.securityScanner = NewSecurityScanner(cs)
			analyzer.podHealthScanner = NewPodHealthScanner(cs)
			analyzer.ingressScanner = NewIngressScanner(cs)
			log.Info().Msg("Workload browser + security + pod health enabled")
		}
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

// coalesce returns the first non-empty string.
func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
