package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	types "github.com/your-org/cluster-intel/pkg/types"
)

// runErrorGroupAnalysis is the analyzer-side LLM closure wired into the
// ErrorAggregator (Phase 3.2). It is called in a goroutine from
// ErrorAggregator.triggerAnalysis whenever a new group is created or a
// rate spike is detected, with `trigger` describing why.
//
// Honours the daily token budget, records latency + skip metrics, parses
// the LLM JSON into ErrorAnalysis, computes signal-based confidence
// (Phase 3.3), and stores the result on the group.
//
// Always nil-safe — short-circuits on missing config / LLM / aggregator.
func (a *Analyzer) runErrorGroupAnalysis(grp *ErrorGroup, trigger string) {
	if grp == nil || a == nil || a.errorAggregator == nil {
		return
	}
	if a.config.LLMBackend == "" || a.config.LLMEndpoint == "" {
		a.errorAggregator.IncLLMSkipped("provider_missing")
		return
	}
	// Phase 3.2 — token budget gate. Reuse the rca engine's accounting
	// since both fight over the same daily quota. If the rca engine
	// hasn't been created (LLM not configured at startup), skip.
	if a.rcaEngine == nil {
		a.errorAggregator.IncLLMSkipped("provider_missing")
		return
	}
	if a.rcaEngine.dailyBudget > 0 {
		used := a.rcaEngine.tokensUsed.Load()
		// 10% headroom — same gate documented in ERRORS_PLAN.md.
		if used >= a.rcaEngine.dailyBudget*9/10 {
			a.errorAggregator.IncLLMSkipped("budget_exhausted")
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	prompt := buildErrorAnalysisPrompt(grp)

	llmReq := types.LLMRequest{
		Model: a.config.LLMModel,
		Messages: []types.LLMMessage{
			{Role: "system", Content: errorAnalysisSystemPrompt},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   1024, // small budget — RCA-style analyses are short
		Temperature: a.config.Temperature,
	}
	body, err := json.Marshal(llmReq)
	if err != nil {
		a.errorAggregator.IncLLMSkipped("marshal_error")
		log.Warn().Err(err).Msg("error analysis: marshal failed")
		return
	}

	endpoint := a.getLLMEndpoint() + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		a.errorAggregator.IncLLMSkipped("request_error")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if a.config.LLMAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.config.LLMAPIKey)
	}

	start := time.Now()
	resp, err := a.httpClient.Do(req)
	a.errorAggregator.ObserveLLMLatency(time.Since(start))
	if err != nil {
		a.errorAggregator.IncLLMSkipped("http_error")
		log.Warn().Err(err).Str("group", grp.Fingerprint).Msg("error analysis HTTP failed")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		a.errorAggregator.IncLLMSkipped("http_status_" + fmt.Sprintf("%d", resp.StatusCode))
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Warn().Int("status", resp.StatusCode).Bytes("body", bodyBytes).Msg("error analysis non-200")
		return
	}

	var llmResp types.LLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		a.errorAggregator.IncLLMSkipped("decode_error")
		return
	}
	if a.llmTokensUsed != nil {
		a.llmTokensUsed.Add(float64(llmResp.Usage.TotalTokens))
	}
	a.rcaEngine.tokensUsed.Add(int64(llmResp.Usage.TotalTokens))

	if len(llmResp.Choices) == 0 {
		a.errorAggregator.IncLLMSkipped("empty_response")
		return
	}
	content := llmResp.Choices[0].Message.Content

	// Try direct unmarshal, then fall back to extracting JSON from
	// markdown if the model wrapped it in a code block.
	var parsed errorAnalysisLLMOutput
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		clean := extractJSON(content)
		if err := json.Unmarshal([]byte(clean), &parsed); err != nil {
			a.errorAggregator.IncLLMSkipped("parse_error")
			log.Warn().Err(err).Str("content", content).Msg("error analysis: unparseable LLM JSON")
			return
		}
	}

	// Phase 3.3 — calibrated confidence. Combine structural signals
	// (stack present, multi-pod, correlated incident) with the LLM's
	// self-report. The structural-only component caps at 0.50.
	hasStack := grp.SampleStack != ""
	multiPod := uniquePodCount(a.errorAggregator, grp.Fingerprint) >= 3
	corrIncident := false
	if a.correlator != nil {
		corrIncident = len(a.correlator.IncidentsForTarget(grp.Namespace, grp.Service)) > 0
	}
	confidence := computeConfidence(hasStack, multiPod, corrIncident, parsed.Confidence)

	// Build evidence breadcrumbs from what we used.
	var evidence []AnalysisEvidence
	if corrIncident && a.correlator != nil {
		incs := a.correlator.IncidentsForTarget(grp.Namespace, grp.Service)
		for i, inc := range incs {
			if i >= 3 {
				break
			}
			evidence = append(evidence, AnalysisEvidence{
				Kind: "incident", Ref: fmt.Sprintf("%d", inc.ID),
				Note: inc.Severity + " · " + inc.Status,
			})
		}
	}
	if grp.SampleStack != "" {
		evidence = append(evidence, AnalysisEvidence{
			Kind: "log", Ref: grp.Fingerprint, Note: "sample stack trace present",
		})
	}

	analysis := &ErrorAnalysis{
		RootCause:   strings.TrimSpace(parsed.RootCause),
		Impact:      strings.TrimSpace(parsed.Impact),
		Fix:         strings.TrimSpace(parsed.Fix),
		Severity:    normalizeSeverity(parsed.Severity),
		Confidence:  confidence,
		Evidence:    evidence,
		Model:       a.config.LLMModel,
		GeneratedAt: time.Now(),
		Trigger:     trigger,
	}

	// Atomic update of the group's analysis. We re-look-up the group by
	// fingerprint because it may have been merged or evicted while the
	// LLM call was in flight.
	a.errorAggregator.SetAnalysis(grp.Fingerprint, analysis)
	log.Info().
		Str("fingerprint", grp.Fingerprint).
		Str("trigger", trigger).
		Float64("confidence", confidence).
		Str("severity", analysis.Severity).
		Msg("Error group analysis stored")
}

// SetAnalysis safely attaches an analysis to a group identified by
// fingerprint. No-op if the group has been evicted or merged away.
func (ea *ErrorAggregator) SetAnalysis(fp string, analysis *ErrorAnalysis) {
	ea.mu.Lock()
	defer ea.mu.Unlock()
	if g, ok := ea.groups[fp]; ok {
		g.Analysis = analysis
		// Keep the markdown blob in sync for legacy clients during the
		// migration window. Phase 3.1 Step 2 will remove this.
		if analysis != nil {
			g.AISummary = "**Root Cause**: " + analysis.RootCause +
				"\n\n**Impact**: " + analysis.Impact +
				"\n\n**Fix**: " + analysis.Fix
		}
	}
}

// uniquePodCount counts distinct pods seen in this group's occurrence
// ring buffer. Used by the confidence signal "multi-pod presence".
func uniquePodCount(ea *ErrorAggregator, fp string) int {
	ea.mu.RLock()
	defer ea.mu.RUnlock()
	occs := ea.occurrences[fp]
	seen := map[string]struct{}{}
	for _, o := range occs {
		if o.Pod != "" {
			seen[o.Pod] = struct{}{}
		}
	}
	return len(seen)
}

// normalizeSeverity collapses LLM-emitted variants into the canonical
// 4-level set the UI expects (chip colour mapping).
func normalizeSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical", "fatal", "panic", "blocker":
		return "critical"
	case "high", "major":
		return "high"
	case "medium", "moderate":
		return "medium"
	case "low", "minor", "trivial":
		return "low"
	default:
		return "medium"
	}
}

// errorAnalysisLLMOutput is the shape we ask the model to emit.
type errorAnalysisLLMOutput struct {
	RootCause  string  `json:"rootCause"`
	Impact     string  `json:"impact"`
	Fix        string  `json:"fix"`
	Severity   string  `json:"severity"`
	Confidence float64 `json:"confidence"`
}

const errorAnalysisSystemPrompt = `You are a Kubernetes SRE expert. Analyze a single error group from a production cluster and return ONLY a JSON object matching this schema:
{
  "rootCause":  "<one-paragraph technical root cause>",
  "impact":     "<one-paragraph user-facing impact>",
  "fix":        "<concrete actionable remediation>",
  "severity":   "critical|high|medium|low",
  "confidence": <0.0 to 1.0, your self-assessed confidence>
}
Do not include markdown fences or any prose outside the JSON.`

func buildErrorAnalysisPrompt(grp *ErrorGroup) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Error group:\n")
	fmt.Fprintf(&b, "  service:        %s\n", grp.Service)
	fmt.Fprintf(&b, "  namespace:      %s\n", grp.Namespace)
	fmt.Fprintf(&b, "  reason:         %s\n", grp.Reason)
	fmt.Fprintf(&b, "  level:          %s\n", grp.Level)
	if grp.ExceptionType != "" {
		fmt.Fprintf(&b, "  exceptionType:  %s\n", grp.ExceptionType)
	}
	fmt.Fprintf(&b, "  totalCount:     %d\n", grp.Count)
	fmt.Fprintf(&b, "  firstSeen:      %s\n", grp.FirstSeen.Format(time.RFC3339))
	fmt.Fprintf(&b, "  lastSeen:       %s\n", grp.LastSeen.Format(time.RFC3339))
	if grp.LastPod != "" {
		fmt.Fprintf(&b, "  lastPod:        %s\n", grp.LastPod)
	}
	if grp.SampleMessage != "" {
		fmt.Fprintf(&b, "\nSample message:\n%s\n", grp.SampleMessage)
	}
	if grp.SampleStack != "" {
		fmt.Fprintf(&b, "\nSample stack:\n%s\n", grp.SampleStack)
	}
	return b.String()
}
