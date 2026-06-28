package main

// chat_tools.go — read-only tools the chat engine can invoke to gather live
// context. Every tool reads in-process analyzer state or the typed Kubernetes
// client (pkg/kube) — there are NO shell subprocesses and NO write actions,
// which removes the command-injection surface the Python prototype had.
//
// Each tool returns a compact text block (fed to the LLM as grounding) plus
// structured citations (surfaced in the UI so the operator can verify claims).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Citation is a single source attribution returned to the UI.
type Citation struct {
	Kind    string `json:"kind"` // doc | incident | error | tool | metric
	Ref     string `json:"ref"`
	Title   string `json:"title"`
	Snippet string `json:"snippet,omitempty"`
}

// toolResult is the output of a single tool invocation.
type toolResult struct {
	Text      string
	Citations []Citation
}

// chatToolSpec documents a tool for the planner prompt.
type chatToolSpec struct {
	Name        string
	Description string
}

// chatToolSpecs is the catalogue advertised to the planner LLM.
func chatToolSpecs() []chatToolSpec {
	return []chatToolSpec{
		{"get_cluster_health", "Overall health scores, cluster summary, and top issues from the latest analysis."},
		{"list_incidents", "Recent correlated incidents with severity, status, affected resources and RCA summary. args: {limit:int}"},
		{"list_error_groups", "Top recurring error groups (grouped log errors) with counts and AI root-cause. args: {limit:int, namespace:string}"},
		{"list_recommendations", "Optimizer/cost/reliability recommendations from the latest analysis. args: {limit:int}"},
		{"list_security_findings", "Security and CIS findings from the latest scan. args: {limit:int}"},
		{"get_pods", "Live pod status (phase, restarts, readiness) from the Kubernetes API. args: {namespace:string}"},
		{"query_prometheus", "Run an instant PromQL query against Prometheus. args: {query:string}"},
	}
}

// runTool dispatches a named tool. Unknown tools return an explanatory result
// rather than an error so the engine can keep going.
func (e *ChatEngine) runTool(ctx context.Context, name string, args map[string]any) toolResult {
	switch name {
	case "get_cluster_health":
		return e.toolClusterHealth()
	case "list_incidents":
		return e.toolListIncidents(argInt(args))
	case "list_error_groups":
		return e.toolListErrorGroups(argInt(args), argStr(args, "namespace"))
	case "list_recommendations":
		return e.toolListRecommendations(argInt(args))
	case "list_security_findings":
		return e.toolListSecurityFindings(argInt(args))
	case "get_pods":
		return e.toolGetPods(ctx, argStr(args, "namespace"))
	case "query_prometheus":
		return e.toolQueryPrometheus(ctx, argStr(args, "query"))
	default:
		return toolResult{Text: fmt.Sprintf("(unknown tool %q skipped)", name)}
	}
}

func (e *ChatEngine) toolClusterHealth() toolResult {
	a := e.analyzer
	a.reportMu.RLock()
	rep := a.latestReport
	a.reportMu.RUnlock()
	if rep == nil {
		return toolResult{Text: "No analysis report is available yet (the analyzer may still be starting or the collector/LLM is unreachable)."}
	}
	var b strings.Builder
	if rep.Scores != nil {
		fmt.Fprintf(&b, "Health scores — overall %d/100 (reliability %d, security %d, cost %d, architecture %d).\n",
			rep.Scores.Overall, rep.Scores.Reliability, rep.Scores.Security, rep.Scores.Cost, rep.Scores.Architecture)
	} else if rep.Status != nil {
		fmt.Fprintf(&b, "Scores unavailable: %s (state=%s).\n", rep.Status.Message, rep.Status.State)
	}
	s := rep.Summary
	fmt.Fprintf(&b, "Cluster: %d nodes, %d pods (%d healthy, %d unhealthy, %d pending) across %d namespaces. %d warning / %d critical events.\n",
		s.TotalNodes, s.TotalPods, s.HealthyPods, s.UnhealthyPods, s.PendingPods, s.TotalNamespaces, s.WarningEvents, s.CriticalEvents)
	if len(rep.TopIssues) > 0 {
		b.WriteString("Top issues:\n")
		for i, iss := range rep.TopIssues {
			if i >= 5 {
				break
			}
			fmt.Fprintf(&b, "  - [%s] %s — %s\n", iss.Severity, iss.Title, truncate(iss.Description, 140))
		}
	}
	return toolResult{
		Text:      b.String(),
		Citations: []Citation{{Kind: "tool", Ref: "get_cluster_health", Title: "Latest cluster health analysis"}},
	}
}

func (e *ChatEngine) toolListIncidents(limit int) toolResult {
	a := e.analyzer
	if a.correlator == nil {
		return toolResult{Text: "Incident correlation is not enabled."}
	}
	a.correlator.mu.RLock()
	incidents := make([]*Incident, 0, len(a.correlator.incidents))
	for _, inc := range a.correlator.incidents {
		incidents = append(incidents, inc)
	}
	a.correlator.mu.RUnlock()

	sort.Slice(incidents, func(i, j int) bool { return incidents[i].DetectedAt.After(incidents[j].DetectedAt) })
	if limit <= 0 || limit > len(incidents) {
		limit = len(incidents)
	}
	if len(incidents) == 0 {
		return toolResult{Text: "No incidents have been detected."}
	}
	var b strings.Builder
	var cites []Citation
	fmt.Fprintf(&b, "%d incident(s); showing %d most recent:\n", len(incidents), limit)
	for i := 0; i < limit; i++ {
		inc := incidents[i]
		fmt.Fprintf(&b, "  - #%d [%s/%s] %s · affected: %s · %s\n",
			inc.ID, inc.Severity, inc.Status, truncate(inc.Summary, 160),
			strings.Join(inc.Affected, ", "), inc.DetectedAt.Format(time.RFC3339))
		if inc.RCAReport != nil {
			fmt.Fprintf(&b, "    RCA: %s\n", truncate(rcaReportText(inc.RCAReport), 240))
		}
		cites = append(cites, Citation{Kind: "incident", Ref: fmt.Sprintf("%d", inc.ID),
			Title: fmt.Sprintf("Incident #%d (%s)", inc.ID, inc.Severity), Snippet: truncate(inc.Summary, 160)})
	}
	return toolResult{Text: b.String(), Citations: cites}
}

func (e *ChatEngine) toolListErrorGroups(limit int, namespace string) toolResult {
	a := e.analyzer
	if a.errorAggregator == nil {
		return toolResult{Text: "Error aggregation is not enabled."}
	}
	a.errorAggregator.mu.RLock()
	groups := make([]*ErrorGroup, 0, len(a.errorAggregator.groups))
	for _, g := range a.errorAggregator.groups {
		if namespace != "" && g.Namespace != namespace {
			continue
		}
		groups = append(groups, g)
	}
	a.errorAggregator.mu.RUnlock()

	sort.Slice(groups, func(i, j int) bool { return groups[i].Count > groups[j].Count })
	if limit <= 0 || limit > len(groups) {
		limit = len(groups)
	}
	if len(groups) == 0 {
		return toolResult{Text: "No error groups recorded."}
	}
	var b strings.Builder
	var cites []Citation
	fmt.Fprintf(&b, "%d error group(s); showing top %d by volume:\n", len(groups), limit)
	for i := 0; i < limit; i++ {
		g := groups[i]
		title := g.Title
		if title == "" {
			title = g.Reason
		}
		fmt.Fprintf(&b, "  - #%d [%s] %s/%s ×%d · %s\n", g.ID, g.Level, g.Namespace, g.Service, g.Count, truncate(title, 140))
		if g.Analysis != nil && g.Analysis.RootCause != "" {
			fmt.Fprintf(&b, "    root cause: %s | fix: %s\n", truncate(g.Analysis.RootCause, 200), truncate(g.Analysis.Fix, 160))
		} else if g.SampleMessage != "" {
			fmt.Fprintf(&b, "    sample: %s\n", truncate(g.SampleMessage, 200))
		}
		cites = append(cites, Citation{Kind: "error", Ref: fmt.Sprintf("%d", g.ID),
			Title: truncate(title, 80), Snippet: fmt.Sprintf("%s/%s ×%d", g.Namespace, g.Service, g.Count)})
	}
	return toolResult{Text: b.String(), Citations: cites}
}

func (e *ChatEngine) toolListRecommendations(limit int) toolResult {
	a := e.analyzer
	a.reportMu.RLock()
	rep := a.latestReport
	a.reportMu.RUnlock()
	if rep == nil || len(rep.Recommendations) == 0 {
		return toolResult{Text: "No recommendations are available."}
	}
	if limit <= 0 || limit > len(rep.Recommendations) {
		limit = len(rep.Recommendations)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d recommendation(s); showing %d:\n", len(rep.Recommendations), limit)
	for i := 0; i < limit; i++ {
		rc := rep.Recommendations[i]
		fmt.Fprintf(&b, "  - [%s/%s] %s — %s\n", rc.Severity, rc.Category, rc.Title, truncate(rc.Description, 160))
		if rc.Impact.CostSavings != nil && rc.Impact.CostSavings.Monthly > 0 {
			fmt.Fprintf(&b, "    est. savings: %.0f %s/mo\n", rc.Impact.CostSavings.Monthly, rc.Impact.CostSavings.Currency)
		}
	}
	return toolResult{Text: b.String(), Citations: []Citation{{Kind: "tool", Ref: "list_recommendations", Title: "Optimizer recommendations"}}}
}

func (e *ChatEngine) toolListSecurityFindings(limit int) toolResult {
	a := e.analyzer
	a.reportMu.RLock()
	rep := a.latestReport
	a.reportMu.RUnlock()
	if rep == nil || len(rep.SecurityFindings) == 0 {
		return toolResult{Text: "No security findings are available."}
	}
	if limit <= 0 || limit > len(rep.SecurityFindings) {
		limit = len(rep.SecurityFindings)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d security finding(s); showing %d:\n", len(rep.SecurityFindings), limit)
	for i := 0; i < limit; i++ {
		f := rep.SecurityFindings[i]
		fmt.Fprintf(&b, "  - [%s/%s] %s — %s", f.Severity, f.Category, f.Title, truncate(f.Description, 160))
		if f.CISControl != "" {
			fmt.Fprintf(&b, " (CIS %s)", f.CISControl)
		}
		b.WriteString("\n")
	}
	return toolResult{Text: b.String(), Citations: []Citation{{Kind: "tool", Ref: "list_security_findings", Title: "Security scan findings"}}}
}

func (e *ChatEngine) toolGetPods(ctx context.Context, namespace string) toolResult {
	if e.clientset == nil {
		return toolResult{Text: "Kubernetes API access is not configured for the analyzer."}
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	list, err := e.clientset.CoreV1().Pods(namespace).List(cctx, metav1.ListOptions{Limit: 200})
	if err != nil {
		return toolResult{Text: fmt.Sprintf("Failed to list pods: %v", err)}
	}
	if len(list.Items) == 0 {
		scope := namespace
		if scope == "" {
			scope = "all namespaces"
		}
		return toolResult{Text: fmt.Sprintf("No pods found in %s.", scope)}
	}
	var b strings.Builder
	unhealthy := 0
	scope := namespace
	if scope == "" {
		scope = "all namespaces"
	}
	fmt.Fprintf(&b, "Pods in %s (%d):\n", scope, len(list.Items))
	shown := 0
	for _, p := range list.Items {
		restarts := 0
		ready := 0
		for _, cs := range p.Status.ContainerStatuses {
			restarts += int(cs.RestartCount)
			if cs.Ready {
				ready++
			}
		}
		phase := string(p.Status.Phase)
		bad := phase != "Running" && phase != "Succeeded"
		if bad || restarts > 0 && ready < len(p.Status.ContainerStatuses) {
			unhealthy++
		}
		// Show unhealthy pods first / cap output to keep context small.
		if shown < 40 {
			fmt.Fprintf(&b, "  - %s/%s: %s, ready %d/%d, restarts %d\n",
				p.Namespace, p.Name, phase, ready, len(p.Spec.Containers), restarts)
			shown++
		}
	}
	fmt.Fprintf(&b, "(%d pods appear unhealthy)\n", unhealthy)
	return toolResult{Text: b.String(), Citations: []Citation{{Kind: "tool", Ref: "get_pods", Title: "Live pod status (Kubernetes API)"}}}
}

func (e *ChatEngine) toolQueryPrometheus(ctx context.Context, query string) toolResult {
	if e.promURL == "" {
		return toolResult{Text: "Prometheus is not configured."}
	}
	if strings.TrimSpace(query) == "" {
		return toolResult{Text: "No PromQL query was provided."}
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, strings.TrimRight(e.promURL, "/")+"/api/v1/query", nil)
	if err != nil {
		return toolResult{Text: fmt.Sprintf("Failed to build Prometheus request: %v", err)}
	}
	q := req.URL.Query()
	q.Set("query", query)
	req.URL.RawQuery = q.Encode()

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return toolResult{Text: fmt.Sprintf("Prometheus query failed: %v", err)}
	}
	defer resp.Body.Close()
	var out struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return toolResult{Text: fmt.Sprintf("Failed to parse Prometheus response: %v", err)}
	}
	if out.Status != "success" {
		return toolResult{Text: "Prometheus returned a non-success status for the query."}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "PromQL `%s` → %d series:\n", query, len(out.Data.Result))
	for i, r := range out.Data.Result {
		if i >= 20 {
			fmt.Fprintf(&b, "  … (%d more)\n", len(out.Data.Result)-20)
			break
		}
		val := ""
		if len(r.Value) == 2 {
			val = fmt.Sprintf("%v", r.Value[1])
		}
		fmt.Fprintf(&b, "  - %s = %s\n", labelString(r.Metric), val)
	}
	return toolResult{Text: b.String(), Citations: []Citation{{Kind: "metric", Ref: query, Title: "Prometheus query"}}}
}

// --- helpers ---------------------------------------------------------------

func labelString(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		if k == "__name__" {
			continue
		}
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	name := m["__name__"]
	return name + "{" + strings.Join(parts, ",") + "}"
}

// argInt reads the integer "limit" argument, defaulting to 5. (Only "limit"
// is ever an int arg in the tool catalogue, always with the same default.)
func argInt(args map[string]any) int {
	const def = 5
	if args == nil {
		return def
	}
	switch v := args["limit"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

// argStr reads a string argument by key, defaulting to "" when absent.
func argStr(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}
