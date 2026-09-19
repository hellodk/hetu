package main

import (
	"strings"
	"testing"
	"time"

	"github.com/hellodk/hetu/pkg/types"
)

// int64p returns a pointer to v (matching the JSON-optional incidentId field).
func int64p(v int64) *int64 { return &v }

// newChatEngineWithIncident builds a ChatEngine whose analyzer has an
// RCAEngine wired to a correlator that already contains one incident.
func newChatEngineWithIncident(t *testing.T) *ChatEngine {
	t.Helper()
	corr := NewCorrelator("test", time.Minute)
	corr.incidents[42] = &Incident{
		ID:         42,
		Severity:   "critical",
		Status:     "investigating",
		DetectedAt: time.Now(),
		Affected:   []string{"default/api-server-abc"},
		Summary:    "api-server-abc crashlooping in default",
		Signals: []Signal{{
			Timestamp: time.Now(),
			Source:    "logs",
			Severity:  "critical",
			Namespace: "default",
			Service:   "api-server-abc",
			Pod:       "api-server-abc-xyz",
			Kind:      "crashloop",
			Title:     "api-server-abc CrashLoopBackOff",
		}},
		RCAReport: &RCAReport{
			Summary:   "OOM kill from 256Mi memory limit",
			RootCause: RootCause{Primary: "memory limit exceeded", Confidence: 0.9},
		},
	}
	a := &Analyzer{
		rcaEngine:  &RCAEngine{correlator: corr},
		correlator: corr,
	}
	return &ChatEngine{analyzer: a}
}

// TestChatIncidentContext_Seeded verifies the incident context block is
// produced for an incident the correlator knows about.
func TestChatIncidentContext_Seeded(t *testing.T) {
	e := newChatEngineWithIncident(t)
	ctx := e.incidentContext(int64p(42))
	if ctx == "" {
		t.Fatal("expected incident context for known incident, got empty")
	}
	for _, want := range []string{"INC-42", "api-server-abc", "OOM kill from 256Mi memory limit"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("incident context missing %q:\n%s", want, ctx)
		}
	}
}

// TestChatIncidentContext_Unknown verifies an unknown or out-of-scope incident
// produces nothing (so chat does not invent incident context).
func TestChatIncidentContext_Unknown(t *testing.T) {
	e := newChatEngineWithIncident(t)
	if got := e.incidentContext(int64p(999)); got != "" {
		t.Fatalf("expected empty context for unknown incident, got %q", got)
	}
}

// TestChatBuildMessages_IncludesIncidentContext verifies a chat turn grounded
// on an incident carries that context inside the system prompt — the same
// grounding handleAsk gets, now available to the tool-calling engine.
func TestChatBuildMessages_IncludesIncidentContext(t *testing.T) {
	e := newChatEngineWithIncident(t)
	msgs := e.buildMessages("How many pods are affected?", nil, "", "", e.incidentContext(int64p(42)))
	if len(msgs) == 0 {
		t.Fatal("buildMessages returned no messages")
	}
	sys := msgs[0].Content
	if !strings.Contains(sys, "INC-42") {
		t.Fatalf("system prompt missing incident context:\n%s", sys)
	}
	// Non-grounding turn stays generic.
	plain := e.buildMessages("hello", nil, "", "", "")
	if strings.Contains(plain[0].Content, "INC-42") {
		t.Fatal("system prompt leaked incident context for unrelated turn")
	}
}

// TestChatBuildMessages_HistoryTrimGuard keeps the turn format stable: user
// question must be the last message and history must precede it.
func TestChatBuildMessages_HistoryTrimGuard(t *testing.T) {
	e := newChatEngineWithIncident(t)
	history := []chatMsg{
		{Role: "user", Content: "what broke?"},
		{Role: "assistant", Content: "the api-server"},
	}
	msgs := e.buildMessages("and the namespace?", history, "", "default", "")
	last := msgs[len(msgs)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "and the namespace?") {
		t.Fatalf("expected user question as final message, got %+v", last)
	}
	if got := len(msgs); got < 3 {
		t.Fatalf("expected system + 2 history + question, got %d messages", got)
	}
}

var _ = types.LLMMessage{} // keep types import when assertions above evolve
