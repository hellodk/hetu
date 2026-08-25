package main

// chat.go — agentic RAG chat engine.
//
// Flow per user message (see docs/AI_CHAT_RAG_PLAN.md):
//
//  1. PLAN     — one cheap LLM call returns a JSON plan: which read-only tools
//                to run and a knowledge-base search query. Falls back to a
//                keyword heuristic if the model/plan is unavailable.
//  2. RETRIEVE — run the selected tools (in-process state + typed K8s client +
//                Prometheus) and a Qdrant semantic search over hetu_kb and the
//                incident store, concurrently. Collect grounding text + cites.
//  3. SYNTHES. — stream the final, grounded answer to the browser via SSE,
//                then persist the assistant turn.
//
// The engine reuses the operator-configured LLM (analyzer runtime config) and
// embedding model. It degrades gracefully: no Qdrant → tools-only; no LLM →
// a deterministic summary of the gathered context.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/kubernetes"

	types "github.com/hellodk/hetu/pkg/types"
)

// ChatEngine orchestrates retrieval-augmented conversations.
type ChatEngine struct {
	analyzer    *Analyzer
	kbMu        sync.RWMutex
	kb          *KBStore
	clientset   kubernetes.Interface
	promURL     string
	httpClient  *http.Client
	embedConfig *EmbeddingConfigAPI
	store       *chatConvStore
}

// getKB returns the current KB store under a read lock (it may be swapped when
// the embedding config changes at runtime).
func (e *ChatEngine) getKB() *KBStore {
	e.kbMu.RLock()
	defer e.kbMu.RUnlock()
	return e.kb
}

// SetKB swaps the KB store (called when the embedding config is updated).
func (e *ChatEngine) SetKB(kb *KBStore) {
	e.kbMu.Lock()
	e.kb = kb
	e.kbMu.Unlock()
}

// NewChatEngine builds the engine. kb/clientset/promURL may be empty/nil;
// the engine adapts to whatever is available.
func NewChatEngine(a *Analyzer, kb *KBStore, cs kubernetes.Interface, promURL string, ec *EmbeddingConfigAPI) *ChatEngine {
	return &ChatEngine{
		analyzer:    a,
		kb:          kb,
		clientset:   cs,
		promURL:     promURL,
		embedConfig: ec,
		httpClient:  &http.Client{Timeout: 5 * time.Minute},
		store:       newChatConvStore(),
	}
}

func (e *ChatEngine) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/chat", e.handleChat)
	mux.HandleFunc("GET /api/v1/chat/conversations/{id}", e.handleGetConversation)
	mux.HandleFunc("POST /api/v1/chat/reindex", e.handleReindex)
	mux.HandleFunc("GET /api/v1/chat/status", e.handleStatus)
}

// --- conversation store -----------------------------------------------------

type chatMsg struct {
	Role    string    `json:"role"`
	Content string    `json:"content"`
	At      time.Time `json:"at"`
}

type chatConvStore struct {
	mu    sync.RWMutex
	convs map[string][]chatMsg
}

func newChatConvStore() *chatConvStore { return &chatConvStore{convs: map[string][]chatMsg{}} }

func (s *chatConvStore) get(id string) []chatMsg {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]chatMsg, len(s.convs[id]))
	copy(out, s.convs[id])
	return out
}

func (s *chatConvStore) append(id string, m chatMsg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.convs[id] = append(s.convs[id], m)
	// Bound memory: keep last 40 turns per conversation.
	if len(s.convs[id]) > 40 {
		s.convs[id] = s.convs[id][len(s.convs[id])-40:]
	}
}

// --- HTTP handlers ----------------------------------------------------------

func (e *ChatEngine) handleStatus(w http.ResponseWriter, r *http.Request) {
	snap := e.analyzer.chatLLMConfig()
	embReady := false
	embModel := ""
	if e.embedConfig != nil {
		st := e.embedConfig.State()
		embModel = st.Model
		embReady = st.Model != ""
	}
	writeJSON(w, map[string]any{
		"llmReady":       snap.Endpoint != "",
		"llmModel":       snap.Model,
		"llmProvider":    snap.Provider,
		"embeddingReady": embReady,
		"embeddingModel": embModel,
		"kbEnabled":      e.getKB() != nil,
		"kbChunks":       e.getKB().Count(),
		"tools":          chatToolSpecs(),
	})
}

func (e *ChatEngine) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	msgs := e.store.get(id)
	if len(msgs) == 0 {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"conversationId": id, "messages": msgs})
}

func (e *ChatEngine) handleReindex(w http.ResponseWriter, r *http.Request) {
	kb := e.getKB()
	if kb == nil {
		http.Error(w, "knowledge base not enabled (set QDRANT_URL and configure embeddings)", http.StatusServiceUnavailable)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		dir := getEnvOrDefault("KB_DOCS_DIR", "docs")
		n, err := kb.IndexRepoDocs(ctx, dir)
		if err != nil {
			log.Warn().Err(err).Msg("chat: reindex failed")
			return
		}
		log.Info().Int("chunks", n).Msg("chat: reindex complete")
	}()
	writeJSON(w, map[string]any{"status": "reindexing", "chunks": kb.Count()})
}

func (e *ChatEngine) handleChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message        string `json:"message"`
		ConversationID string `json:"conversationId"`
		Namespace      string `json:"namespace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}
	convID := req.ConversationID
	if convID == "" {
		convID = "conv_" + uuid.NewString()
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Conversation-ID", convID)
	w.WriteHeader(http.StatusOK)

	emit := func(ev map[string]any) {
		b, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	ctx := r.Context()
	history := e.store.get(convID)
	e.store.append(convID, chatMsg{Role: "user", Content: req.Message, At: time.Now()})
	emit(map[string]any{"type": "conversation", "conversationId": convID})

	// 1. PLAN
	plan := e.plan(ctx, req.Message, history, req.Namespace)
	for _, t := range plan.Tools {
		emit(map[string]any{"type": "tool", "name": t.Name, "args": t.Args})
	}

	// 2. RETRIEVE (tools + KB concurrently)
	grounding, citations := e.retrieve(ctx, plan)
	for _, c := range citations {
		emit(map[string]any{"type": "citation", "citation": c})
	}

	// 3. SYNTHESIZE
	messages := e.buildMessages(req.Message, history, grounding, req.Namespace)
	var answer strings.Builder
	err := e.streamAnswer(ctx, messages, func(tok string) {
		answer.WriteString(tok)
		emit(map[string]any{"type": "token", "content": tok})
	})
	if err != nil {
		log.Warn().Err(err).Msg("chat: synthesis failed")
		if answer.Len() == 0 {
			// Deterministic fallback so the operator still gets the gathered data.
			fb := "I couldn't reach the language model, but here is what I gathered:\n\n" + grounding
			answer.WriteString(fb)
			emit(map[string]any{"type": "token", "content": fb})
		}
		emit(map[string]any{"type": "error", "message": err.Error()})
	}

	e.store.append(convID, chatMsg{Role: "assistant", Content: answer.String(), At: time.Now()})
	emit(map[string]any{"type": "done", "conversationId": convID})
}

// --- planning ---------------------------------------------------------------

type plannedTool struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type chatPlan struct {
	KBQuery   string        `json:"kb_query"`
	Tools     []plannedTool `json:"tools"`
	Namespace string        `json:"-"`
}

// plan asks the LLM which tools to run and what to search the KB for, with a
// keyword heuristic fallback that keeps chat useful even without an LLM.
func (e *ChatEngine) plan(ctx context.Context, message string, history []chatMsg, namespace string) chatPlan {
	fallback := heuristicPlan(message, namespace)

	a := e.analyzer
	if a.rcaEngine == nil || a.rcaEngine.llmClient == nil {
		return fallback
	}

	var cat strings.Builder
	for _, t := range chatToolSpecs() {
		fmt.Fprintf(&cat, "- %s: %s\n", t.Name, t.Description)
	}
	sys := "You are the planning step of a Kubernetes SRE assistant. Given the user's question, decide which read-only tools to call and what to search the knowledge base for. " +
		"Respond with ONLY a JSON object: {\"kb_query\": string, \"tools\": [{\"name\": string, \"args\": object}]}. " +
		"Pick at most 3 tools. Use [] if no tool is needed. Available tools:\n" + cat.String()

	userCtx := message
	if namespace != "" {
		userCtx += "\n(current namespace context: " + namespace + ")"
	}
	// Fold in a little recent conversation so follow-ups ("and that pod?")
	// plan against the right context.
	if n := len(history); n > 0 {
		start := n - 4
		if start < 0 {
			start = 0
		}
		var hb strings.Builder
		for _, m := range history[start:] {
			fmt.Fprintf(&hb, "%s: %s\n", m.Role, m.Content)
		}
		userCtx = "Recent conversation:\n" + hb.String() + "\nCurrent question: " + userCtx
	}
	msgs := []types.LLMMessage{
		{Role: "system", Content: sys},
		{Role: "user", Content: userCtx},
	}

	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := a.rcaEngine.llmClient.Complete(pctx, "chat-plan", msgs)
	if err != nil || res == nil || strings.TrimSpace(res.Content) == "" {
		return fallback
	}
	var p chatPlan
	if err := json.Unmarshal([]byte(res.Content), &p); err != nil {
		if err := json.Unmarshal([]byte(extractJSON(res.Content)), &p); err != nil {
			return fallback
		}
	}
	// Inject namespace into tool args where relevant and validate names.
	valid := map[string]bool{}
	for _, t := range chatToolSpecs() {
		valid[t.Name] = true
	}
	cleaned := p.Tools[:0]
	for _, t := range p.Tools {
		if !valid[t.Name] {
			continue
		}
		if namespace != "" && (t.Name == "get_pods" || t.Name == "list_error_groups") {
			if t.Args == nil {
				t.Args = map[string]any{}
			}
			if _, ok := t.Args["namespace"]; !ok {
				t.Args["namespace"] = namespace
			}
		}
		cleaned = append(cleaned, t)
		if len(cleaned) >= 3 {
			break
		}
	}
	p.Tools = cleaned
	if strings.TrimSpace(p.KBQuery) == "" {
		p.KBQuery = message
	}
	if len(p.Tools) == 0 && len(fallback.Tools) > 0 {
		p.Tools = fallback.Tools
	}
	return p
}

// heuristicPlan is the no-LLM fallback: keyword routing to tools.
func heuristicPlan(message, namespace string) chatPlan {
	l := strings.ToLower(message)
	p := chatPlan{KBQuery: message}
	add := func(name string, args map[string]any) { p.Tools = append(p.Tools, plannedTool{Name: name, Args: args}) }

	matched := false
	if containsAny(l, "incident", "outage", "root cause", "rca", "went down", "broke") {
		add("list_incidents", map[string]any{"limit": 5})
		matched = true
	}
	if containsAny(l, "error", "exception", "crash", "log", "stack") {
		args := map[string]any{"limit": 5}
		if namespace != "" {
			args["namespace"] = namespace
		}
		add("list_error_groups", args)
		matched = true
	}
	if containsAny(l, "pod", "restart", "crashloop", "pending", "evicted", "running") {
		add("get_pods", map[string]any{"namespace": namespace})
		matched = true
	}
	if containsAny(l, "cpu", "memory", "usage", "metric", "throttl", "saturat", "latency") {
		matched = true // metrics need a concrete PromQL; let the LLM form it next turn
	}
	if containsAny(l, "secur", "cis", "vulnerab", "rbac", "compliance") {
		add("list_security_findings", map[string]any{"limit": 5})
		matched = true
	}
	if containsAny(l, "cost", "optimi", "right-siz", "rightsiz", "savings", "recommend") {
		add("list_recommendations", map[string]any{"limit": 5})
		matched = true
	}
	if containsAny(l, "health", "score", "status", "overall", "summary") || !matched {
		add("get_cluster_health", nil)
	}
	return p
}

// --- retrieval --------------------------------------------------------------

func (e *ChatEngine) retrieve(ctx context.Context, plan chatPlan) (string, []Citation) {
	var (
		mu        sync.Mutex
		blocks    []string
		citations []Citation
		wg        sync.WaitGroup
	)
	addBlock := func(header, text string, cites []Citation) {
		mu.Lock()
		defer mu.Unlock()
		if strings.TrimSpace(text) != "" {
			blocks = append(blocks, "### "+header+"\n"+text)
		}
		citations = append(citations, cites...)
	}

	// Knowledge base + incident similarity search.
	if kb := e.getKB(); kb != nil && strings.TrimSpace(plan.KBQuery) != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hits := kb.Search(ctx, plan.KBQuery, 5)
			if len(hits) == 0 {
				return
			}
			var b strings.Builder
			var cites []Citation
			for _, h := range hits {
				heading := h.Heading
				if heading == "" {
					heading = h.Title
				}
				fmt.Fprintf(&b, "[%s › %s]\n%s\n\n", h.Source, heading, truncate(h.Text, 700))
				cites = append(cites, Citation{Kind: "doc", Ref: h.Source, Title: strings.TrimSpace(h.Title + " › " + heading), Snippet: truncate(h.Text, 160)})
			}
			addBlock("Knowledge base", b.String(), cites)
		}()
	}

	// Tools.
	for _, t := range plan.Tools {
		t := t
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := e.runTool(ctx, t.Name, t.Args)
			addBlock("Tool: "+t.Name, res.Text, res.Citations)
		}()
	}

	wg.Wait()
	return strings.Join(blocks, "\n\n"), dedupeCitations(citations)
}

// --- synthesis --------------------------------------------------------------

func (e *ChatEngine) buildMessages(message string, history []chatMsg, grounding, namespace string) []types.LLMMessage {
	sys := chatSystemPrompt
	if namespace != "" {
		sys += "\nThe operator's current namespace context is: " + namespace + "."
	}
	msgs := []types.LLMMessage{{Role: "system", Content: sys}}

	// Recent history (last 8 turns) for continuity.
	start := 0
	if len(history) > 8 {
		start = len(history) - 8
	}
	for _, m := range history[start:] {
		if m.Role == "user" || m.Role == "assistant" {
			msgs = append(msgs, types.LLMMessage{Role: m.Role, Content: m.Content})
		}
	}

	ground := grounding
	if strings.TrimSpace(ground) == "" {
		ground = "(no additional context was retrieved)"
	}
	userTurn := fmt.Sprintf(
		"Operator question:\n%s\n\n---\nRetrieved context (use this to ground your answer; cite sources inline like [docs/FILE.md] or [incident #N]):\n%s",
		message, ground)
	msgs = append(msgs, types.LLMMessage{Role: "user", Content: userTurn})
	return msgs
}

const chatSystemPrompt = `You are Hetu, an expert Kubernetes SRE assistant embedded in a cluster-health platform.
You help operators understand and troubleshoot their cluster using the retrieved context provided to you.
Guidelines:
- Ground every factual claim in the retrieved context. If the context is insufficient, say so and suggest what to check next.
- Be concise and actionable. Lead with the answer, then the evidence, then recommended next steps.
- When you reference a fact, cite its source inline, e.g. [docs/SCORING_SYSTEM.md] or [incident #42] or [tool:get_pods].
- Never invent pod names, metrics, or incident IDs that are not in the context.
- All your actions are read-only; when suggesting changes, give the exact kubectl/YAML but do not claim to have executed anything.`

// streamAnswer streams the final completion. It supports OpenAI-compatible and
// Ollama streaming; other providers fall back to a single blocking completion.
func (e *ChatEngine) streamAnswer(ctx context.Context, messages []types.LLMMessage, onToken func(string)) error {
	snap := e.analyzer.chatLLMConfig()
	if snap.Endpoint == "" {
		return fmt.Errorf("no LLM endpoint configured")
	}
	switch snap.Provider {
	case "ollama":
		return e.streamOllama(ctx, snap, messages, onToken)
	case "anthropic", "bedrock", "azure":
		return e.blockingComplete(ctx, messages, onToken)
	default: // openai, vllm, llamacpp, custom, "" → OpenAI-compatible
		return e.streamOpenAI(ctx, snap, messages, onToken)
	}
}

func (e *ChatEngine) blockingComplete(ctx context.Context, messages []types.LLMMessage, onToken func(string)) error {
	a := e.analyzer
	if a.rcaEngine == nil || a.rcaEngine.llmClient == nil {
		return fmt.Errorf("LLM client unavailable")
	}
	res, err := a.rcaEngine.llmClient.Complete(ctx, "chat", messages)
	if err != nil {
		return err
	}
	if res != nil && res.Content != "" {
		onToken(res.Content)
	}
	return nil
}

// buildChatCompletionsBody constructs the OpenAI-compatible request payload.
// Self-hosted reasoning models (Qwen3-family on vLLM/omlx/llama.cpp) can burn
// the whole token budget on delta.reasoning_content before answering; for
// those backends we disable thinking via chat_template_kwargs. Hosted APIs
// (OpenAI/Azure) reject unknown body parameters and must not receive it.
func buildChatCompletionsBody(snap llmSnapshot, messages []types.LLMMessage) map[string]any {
	maxTok := snap.MaxTokens
	if maxTok <= 0 {
		maxTok = 1024
	}
	body := map[string]any{
		"model":       snap.Model,
		"messages":    messages,
		"temperature": snap.Temperature,
		"max_tokens":  maxTok,
		"stream":      true,
	}
	switch snap.Provider {
	case "openai", "azure", "anthropic", "bedrock":
		// Hosted APIs — no vendor-specific extras.
	default:
		body["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
	}
	return body
}

func (e *ChatEngine) streamOpenAI(ctx context.Context, snap llmSnapshot, messages []types.LLMMessage, onToken func(string)) error {
	body, _ := json.Marshal(buildChatCompletionsBody(snap, messages))
	endpoint := strings.TrimRight(snap.Endpoint, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if snap.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+snap.APIKey)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("LLM returned status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			onToken(chunk.Choices[0].Delta.Content)
		}
	}
	return scanner.Err()
}

func (e *ChatEngine) streamOllama(ctx context.Context, snap llmSnapshot, messages []types.LLMMessage, onToken func(string)) error {
	opts := map[string]any{"temperature": snap.Temperature}
	if snap.MaxTokens > 0 {
		opts["num_predict"] = snap.MaxTokens
	}
	body, _ := json.Marshal(map[string]any{
		"model":    snap.Model,
		"messages": messages,
		"stream":   true,
		"options":  opts,
	})
	endpoint := strings.TrimRight(snap.Endpoint, "/") + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
		}
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		if chunk.Message.Content != "" {
			onToken(chunk.Message.Content)
		}
		if chunk.Done {
			break
		}
	}
	return scanner.Err()
}

// --- helpers ----------------------------------------------------------------

// llmSnapshot is a point-in-time copy of the runtime LLM config.
type llmSnapshot struct {
	Provider    string
	Endpoint    string
	Model       string
	APIKey      string
	MaxTokens   int
	Temperature float64
}

func (a *Analyzer) chatLLMConfig() llmSnapshot {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return llmSnapshot{
		Provider:    a.config.LLMBackend,
		Endpoint:    a.config.LLMEndpoint,
		Model:       a.config.LLMModel,
		APIKey:      a.config.LLMAPIKey,
		MaxTokens:   a.config.MaxTokens,
		Temperature: a.config.Temperature,
	}
}

func rcaReportText(r *RCAReport) string {
	if r == nil {
		return ""
	}
	if r.Summary != "" {
		return r.Summary
	}
	return r.RootCause.Primary
}

func dedupeCitations(in []Citation) []Citation {
	seen := map[string]bool{}
	out := in[:0]
	for _, c := range in {
		key := c.Kind + "|" + c.Ref
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
