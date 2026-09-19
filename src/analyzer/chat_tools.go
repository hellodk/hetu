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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
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
		{"describe_pod", "Pod detail: container waiting/terminated state, exit codes, restart count, and pod events. The definitive read for why a pod is not running. args: {namespace:string, pod:string}"},
		{"pod_logs", "Tail of a pod container's logs; previous=true reads the last-terminated instance. args: {namespace:string, pod:string, container:string, previous:bool}"},
		{"rollout_status", "Deployment rollout state: conditions (e.g. ProgressDeadlineExceeded) and its ReplicaSets with revision. args: {namespace:string, deployment:string}"},
		{"job_status", "Job state: conditions (e.g. BackoffLimitExceeded), failed/succeeded counts, and the tail of the last completed pod. args: {namespace:string, job:string}"},
		{"secret_keys", "List of data keys present in a Secret (metadata only, never values). Use when container events name a missing Secret key. args: {namespace:string, name:string}"},
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
	case "describe_pod":
		return e.toolDescribePod(ctx, argStr(args, "namespace"), argStr(args, "pod"))
	case "pod_logs":
		return e.toolPodLogs(ctx, argStr(args, "namespace"), argStr(args, "pod"), argStr(args, "container"), argBool(args, "previous"))
	case "rollout_status":
		return e.toolRolloutStatus(ctx, argStr(args, "namespace"), argStr(args, "deployment"))
	case "job_status":
		return e.toolJobStatus(ctx, argStr(args, "namespace"), argStr(args, "job"))
	case "secret_keys":
		return e.toolSecretKeys(ctx, argStr(args, "namespace"), argStr(args, "name"))
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
			fmt.Fprintf(&b, "  - %s/%s: %s, ready %d/%d, restarts %d", p.Namespace, p.Name, phase, ready, len(p.Spec.Containers), restarts)
			if state := containerStateSummary(p); state != "" {
				fmt.Fprintf(&b, ", state: %s", state)
			}
			b.WriteString("\n")
			shown++
		}
	}
	fmt.Fprintf(&b, "(%d pods appear unhealthy)\n", unhealthy)
	return toolResult{Text: b.String(), Citations: []Citation{{Kind: "tool", Ref: "get_pods", Title: "Live pod status (Kubernetes API)"}}}
}

// containerStateSummary condenses the most telling container state per pod for
// the get_pods summary line: waiting reason/message, terminated reason/code, or
// the last termination state. Empty when every container is running cleanly.
func containerStateSummary(p corev1.Pod) string {
	var parts []string
	for _, cs := range p.Status.ContainerStatuses {
		switch {
		case cs.State.Waiting != nil && cs.State.Waiting.Reason != "":
			msg := cs.State.Waiting.Reason
			if m := strings.TrimSpace(cs.State.Waiting.Message); m != "" {
				msg += ":" + truncate(m, 100)
			}
			parts = append(parts, fmt.Sprintf("%s=%s", cs.Name, msg))
		case cs.State.Terminated != nil:
			parts = append(parts, fmt.Sprintf("%s=terminated:%s:%d", cs.Name, cs.State.Terminated.Reason, cs.State.Terminated.ExitCode))
		case cs.LastTerminationState.Terminated != nil:
			parts = append(parts, fmt.Sprintf("%s=last:%s:%d", cs.Name, cs.LastTerminationState.Terminated.Reason, cs.LastTerminationState.Terminated.ExitCode))
		}
	}
	return strings.Join(parts, ", ")
}

func (e *ChatEngine) toolDescribePod(ctx context.Context, namespace, pod string) toolResult {
	if e.clientset == nil {
		return toolResult{Text: "Kubernetes API access is not configured for the analyzer."}
	}
	if namespace == "" || pod == "" {
		return toolResult{Text: "describe_pod requires both namespace and pod arguments."}
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	p, err := e.clientset.CoreV1().Pods(namespace).Get(cctx, pod, metav1.GetOptions{})
	if err != nil {
		return toolResult{Text: fmt.Sprintf("Failed to describe pod %s/%s: %v", namespace, pod, err)}
	}
	ready := 0
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Ready {
			ready++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Pod %s/%s — phase %s, ready %d/%d\n", namespace, pod, p.Status.Phase, ready, len(p.Spec.Containers))
	for _, cs := range p.Status.ContainerStatuses {
		fmt.Fprintf(&b, "  container %s: restarts %d\n", cs.Name, cs.RestartCount)
		switch {
		case cs.State.Waiting != nil:
			fmt.Fprintf(&b, "    waiting: %s", cs.State.Waiting.Reason)
			if m := strings.TrimSpace(cs.State.Waiting.Message); m != "" {
				fmt.Fprintf(&b, " — %s", truncate(m, 220))
			}
			b.WriteString("\n")
		case cs.State.Terminated != nil:
			fmt.Fprintf(&b, "    terminated: %s exit=%d", cs.State.Terminated.Reason, cs.State.Terminated.ExitCode)
			if !cs.State.Terminated.FinishedAt.IsZero() {
				fmt.Fprintf(&b, " finished %s", cs.State.Terminated.FinishedAt.Format(time.RFC3339))
			}
			b.WriteString("\n")
		default:
			b.WriteString("    running\n")
		}
		if cs.LastTerminationState.Terminated != nil {
			fmt.Fprintf(&b, "    last termination: %s exit=%d\n", cs.LastTerminationState.Terminated.Reason, cs.LastTerminationState.Terminated.ExitCode)
		}
	}
	if evs := podEvents(cctx, e.clientset, namespace, pod); len(evs) > 0 {
		b.WriteString("  events:\n")
		for _, ev := range evs {
			msg := ev.Message
			if msg == "" {
				msg = ev.Reason
			}
			fmt.Fprintf(&b, "    [%s] %s %s — %s\n", ev.Type, ev.Reason, ev.LastTimestamp.Format(time.RFC3339), truncate(msg, 220))
		}
	}
	return toolResult{Text: b.String(), Citations: []Citation{{Kind: "tool", Ref: "describe_pod", Title: fmt.Sprintf("%s/%s pod detail", namespace, pod)}}}
}

// podEvents returns the most recent events (warnings first) whose InvolvedObject
// is the named pod. Bounded to 15 entries to keep the grounding block compact.
func podEvents(ctx context.Context, cs kubernetes.Interface, namespace, pod string) []corev1.Event {
	evs, err := cs.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	var out []corev1.Event
	for _, ev := range evs.Items {
		if ev.InvolvedObject.Kind == "Pod" && ev.InvolvedObject.Name == pod {
			out = append(out, ev)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ti, tj := out[i].LastTimestamp.Time, out[j].LastTimestamp.Time
		if ti.Equal(tj) {
			return out[i].Type == "Warning" && out[j].Type != "Warning"
		}
		return ti.After(tj)
	})
	if len(out) > 15 {
		out = out[:15]
	}
	return out
}

func (e *ChatEngine) toolPodLogs(ctx context.Context, namespace, pod, container string, previous bool) toolResult {
	if e.clientset == nil {
		return toolResult{Text: "Kubernetes API access is not configured for the analyzer."}
	}
	if namespace == "" || pod == "" {
		return toolResult{Text: "pod_logs requires both namespace and pod arguments."}
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tail := int64(60)
	raw, err := e.clientset.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{Container: container, Previous: previous, TailLines: &tail}).Do(cctx).Raw()
	if err != nil {
		return toolResult{Text: fmt.Sprintf("Failed to fetch logs for %s/%s: %v", namespace, pod, err)}
	}
	return formatPodLogs(namespace, pod, container, previous, raw)
}

func formatPodLogs(namespace, pod, container string, previous bool, raw []byte) toolResult {
	if len(raw) == 0 {
		return toolResult{Text: fmt.Sprintf("No logs returned for %s/%s (container %q, previous=%v).", namespace, pod, container, previous)}
	}
	label := fmt.Sprintf("Logs for %s/%s (container %q, previous=%v):\n%s", namespace, pod, container, previous, truncate(string(raw), 6000))
	return toolResult{Text: label, Citations: []Citation{{Kind: "tool", Ref: "pod_logs", Title: fmt.Sprintf("%s/%s logs", namespace, pod)}}}
}

func (e *ChatEngine) toolRolloutStatus(ctx context.Context, namespace, deployment string) toolResult {
	if e.clientset == nil {
		return toolResult{Text: "Kubernetes API access is not configured for the analyzer."}
	}
	if namespace == "" || deployment == "" {
		return toolResult{Text: "rollout_status requires both namespace and deployment arguments."}
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	d, err := e.clientset.AppsV1().Deployments(namespace).Get(cctx, deployment, metav1.GetOptions{})
	if err != nil {
		return toolResult{Text: fmt.Sprintf("Failed to get deployment %s/%s: %v", namespace, deployment, err)}
	}
	var b strings.Builder
	replicas := int32(0)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	fmt.Fprintf(&b, "Deployment %s/%s — desired %d, updated %d, ready %d, unavailable %d\n",
		namespace, deployment, replicas, d.Status.UpdatedReplicas, d.Status.ReadyReplicas, d.Status.UnavailableReplicas)
	for _, c := range d.Status.Conditions {
		fmt.Fprintf(&b, "  condition %s=%s", c.Type, c.Status)
		if c.Reason != "" {
			fmt.Fprintf(&b, " (%s)", c.Reason)
		}
		if m := strings.TrimSpace(c.Message); m != "" {
			fmt.Fprintf(&b, " — %s", truncate(m, 200))
		}
		b.WriteString("\n")
	}
	rss, err := e.clientset.AppsV1().ReplicaSets(namespace).List(cctx, metav1.ListOptions{})
	if err == nil && d.Spec.Selector != nil {
		matched := 0
		for _, rs := range rss.Items {
			if rs.Spec.Template.Labels == nil {
				continue
			}
			ok := true
			for k, v := range d.Spec.Selector.MatchLabels {
				if rs.Spec.Template.Labels[k] != v {
					ok = false
					break
				}
			}
			if ok {
				fmt.Fprintf(&b, "  ReplicaSet %s: desired %d, ready %d, revision %s\n",
					rs.Name, rs.Status.Replicas, rs.Status.ReadyReplicas, rs.Annotations["deployment.kubernetes.io/revision"])
				matched++
			}
		}
		if matched == 0 {
			b.WriteString("  (no ReplicaSets matched the deployment selector)\n")
		}
	}
	return toolResult{Text: b.String(), Citations: []Citation{{Kind: "tool", Ref: "rollout_status", Title: fmt.Sprintf("%s/%s rollout state", namespace, deployment)}}}
}

func (e *ChatEngine) toolJobStatus(ctx context.Context, namespace, job string) toolResult {
	if e.clientset == nil {
		return toolResult{Text: "Kubernetes API access is not configured for the analyzer."}
	}
	if namespace == "" || job == "" {
		return toolResult{Text: "job_status requires both namespace and job arguments."}
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	j, err := e.clientset.BatchV1().Jobs(namespace).Get(cctx, job, metav1.GetOptions{})
	if err != nil {
		return toolResult{Text: fmt.Sprintf("Failed to get job %s/%s: %v", namespace, job, err)}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Job %s/%s — active %d, succeeded %d, failed %d (backoffLimit %d)\n",
		namespace, job, j.Status.Active, j.Status.Succeeded, j.Status.Failed, derefI32(j.Spec.BackoffLimit))
	for _, c := range j.Status.Conditions {
		fmt.Fprintf(&b, "  condition %s=%s", c.Type, c.Status)
		if c.Reason != "" {
			fmt.Fprintf(&b, " (%s)", c.Reason)
		}
		if m := strings.TrimSpace(c.Message); m != "" {
			fmt.Fprintf(&b, " — %s", truncate(m, 200))
		}
		b.WriteString("\n")
	}
	sel := labels.SelectorFromSet(labels.Set{"job-name": job}).String()
	pods, err := e.clientset.CoreV1().Pods(namespace).List(cctx, metav1.ListOptions{LabelSelector: sel})
	if err == nil {
		for i := len(pods.Items) - 1; i >= 0; i-- {
			p := pods.Items[i]
			if p.Status.Phase != corev1.PodFailed && p.Status.Phase != corev1.PodSucceeded {
				continue
			}
			tail := int64(30)
			raw, err := e.clientset.CoreV1().Pods(namespace).GetLogs(p.Name, &corev1.PodLogOptions{TailLines: &tail}).Do(cctx).Raw()
			if err == nil && len(raw) > 0 {
				fmt.Fprintf(&b, "  last pod %s (%s) logs:\n%s\n", p.Name, p.Status.Phase, truncate(string(raw), 1500))
			}
			break
		}
	}
	return toolResult{Text: b.String(), Citations: []Citation{{Kind: "tool", Ref: "job_status", Title: fmt.Sprintf("%s/%s job state", namespace, job)}}}
}

func (e *ChatEngine) toolSecretKeys(ctx context.Context, namespace, name string) toolResult {
	if e.clientset == nil {
		return toolResult{Text: "Kubernetes API access is not configured for the analyzer."}
	}
	if namespace == "" || name == "" {
		return toolResult{Text: "secret_keys requires both namespace and name arguments."}
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	s, err := e.clientset.CoreV1().Secrets(namespace).Get(cctx, name, metav1.GetOptions{})
	if err != nil {
		return toolResult{Text: fmt.Sprintf("Failed to get secret %s/%s: %v", namespace, name, err)}
	}
	keys := make([]string, 0, len(s.Data))
	for k := range s.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	if len(keys) == 0 {
		b.WriteString(fmt.Sprintf("Secret %s/%s: NO data keys (the secret is empty). Referenced env vars will fail to resolve.", namespace, name))
	} else {
		fmt.Fprintf(&b, "Secret %s/%s has %d data key(s): %s.\nValues are never included here — match keys against what the failing container's events reference.",
			namespace, name, len(keys), strings.Join(keys, ", "))
	}
	return toolResult{Text: b.String(), Citations: []Citation{{Kind: "tool", Ref: "secret_keys", Title: fmt.Sprintf("%s/%s secret keys", namespace, name)}}}
}

// derefI32 dereferences an int32 pointer, defaulting to 0 when nil.
func derefI32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
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

// argBool reads a boolean argument by key, defaulting to false when absent.
func argBool(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
}
