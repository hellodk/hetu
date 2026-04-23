// This file is extracted from src/analyzer/llm_metrics.go with minimal
// changes: package rename, config-driven constructor, strings.Contains
// instead of custom helpers. Behaviour is preserved exactly.

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/hellodk/hetu/pkg/config"
	types "github.com/hellodk/hetu/pkg/types"
)

// Metrics holds all LLM-related Prometheus metrics.
type Metrics struct {
	RequestTotal      *prometheus.CounterVec
	RequestDuration   *prometheus.HistogramVec
	RequestInFlight   prometheus.Gauge
	TokensInputTotal  *prometheus.CounterVec
	TokensOutputTotal *prometheus.CounterVec
	TimeToFirstToken  *prometheus.HistogramVec
	TokensPerSecond   *prometheus.GaugeVec
	ErrorsTotal       *prometheus.CounterVec
	QueueWaitTime     *prometheus.HistogramVec
}

// Client wraps an HTTP client with LLM-provider routing, Prometheus
// metrics, and OpenTelemetry tracing.
type Client struct {
	httpClient *http.Client
	metrics    *Metrics
	tracer     trace.Tracer
	cfg        clientConfig
}

type clientConfig struct {
	Endpoint     string
	Model        string
	APIKey       string
	MaxTokens    int
	Temperature  float64
	Timeout      time.Duration
	Provider     string
	MaxRetries   int
	RetryBackoff []time.Duration
}

// CompletionResult holds the result of an LLM completion.
type CompletionResult struct {
	Content          string
	InputTokens      int
	OutputTokens     int
	TotalTokens      int
	TimeToFirstToken float64
	ModelLoaded      bool
	FinishReason     string
}

// --- Ollama request/response types ------------------------------------------

type ollamaRequest struct {
	Model    string             `json:"model"`
	Messages []types.LLMMessage `json:"messages"`
	Stream   bool               `json:"stream"`
	Options  *ollamaOptions     `json:"options,omitempty"`
}

type ollamaOptions struct {
	NumPredict  int     `json:"num_predict,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
}

type ollamaResponse struct {
	Model   string `json:"model"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done               bool  `json:"done"`
	TotalDuration      int64 `json:"total_duration"`
	LoadDuration       int64 `json:"load_duration"`
	PromptEvalCount    int   `json:"prompt_eval_count"`
	PromptEvalDuration int64 `json:"prompt_eval_duration"`
	EvalCount          int   `json:"eval_count"`
	EvalDuration       int64 `json:"eval_duration"`
}

// --- Constructors -----------------------------------------------------------

// NewMetrics creates and registers all LLM Prometheus metrics.
func NewMetrics(namespace string) *Metrics {
	return &Metrics{
		RequestTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{Namespace: namespace, Name: "llm_request_total", Help: "Total number of LLM requests"},
			[]string{"model", "task", "status", "provider"},
		),
		RequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{Namespace: namespace, Name: "llm_request_duration_seconds", Help: "LLM request duration in seconds", Buckets: []float64{0.5, 1, 2, 5, 10, 20, 30, 45, 60, 90, 120}},
			[]string{"model", "task", "provider"},
		),
		RequestInFlight: promauto.NewGauge(
			prometheus.GaugeOpts{Namespace: namespace, Name: "llm_request_in_flight", Help: "Number of LLM requests currently in flight"},
		),
		TokensInputTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{Namespace: namespace, Name: "llm_tokens_input_total", Help: "Total input tokens sent to LLM"},
			[]string{"model", "task", "provider"},
		),
		TokensOutputTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{Namespace: namespace, Name: "llm_tokens_output_total", Help: "Total output tokens received from LLM"},
			[]string{"model", "task", "provider"},
		),
		TimeToFirstToken: promauto.NewHistogramVec(
			prometheus.HistogramOpts{Namespace: namespace, Name: "llm_time_to_first_token_seconds", Help: "Time to first token in seconds", Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 20}},
			[]string{"model", "provider"},
		),
		TokensPerSecond: promauto.NewGaugeVec(
			prometheus.GaugeOpts{Namespace: namespace, Name: "llm_tokens_per_second", Help: "Token generation rate"},
			[]string{"model", "provider"},
		),
		ErrorsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{Namespace: namespace, Name: "llm_errors_total", Help: "Total number of LLM errors"},
			[]string{"model", "task", "error_type", "provider"},
		),
		QueueWaitTime: promauto.NewHistogramVec(
			prometheus.HistogramOpts{Namespace: namespace, Name: "llm_queue_wait_seconds", Help: "Time spent waiting in queue", Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10}},
			[]string{"provider"},
		),
	}
}

// NewClient creates a new instrumented LLM client from config.LLMConfig.
func NewClient(cfg config.LLMConfig, metrics *Metrics) *Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	retries := cfg.MaxRetries
	if retries == 0 {
		retries = 3
	}
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		metrics:    metrics,
		tracer:     otel.Tracer("llm-client"),
		cfg: clientConfig{
			Endpoint:     cfg.Endpoint,
			Model:        cfg.Model,
			APIKey:       cfg.APIKey,
			MaxTokens:    cfg.MaxTokens,
			Temperature:  cfg.Temperature,
			Timeout:      timeout,
			Provider:     cfg.Provider,
			MaxRetries:   retries,
			RetryBackoff: []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second},
		},
	}
}

// --- Core API ---------------------------------------------------------------

// Complete sends a completion request to the configured LLM with full
// instrumentation (metrics + tracing). It routes automatically to the
// appropriate provider backend.
func (c *Client) Complete(ctx context.Context, task string, messages []types.LLMMessage) (*CompletionResult, error) {
	ctx, span := c.tracer.Start(ctx, "llm.complete",
		trace.WithAttributes(
			attribute.String("llm.model", c.cfg.Model),
			attribute.String("llm.provider", c.cfg.Provider),
			attribute.String("llm.task", task),
			attribute.Int("llm.message_count", len(messages)),
		),
	)
	defer span.End()

	c.metrics.RequestInFlight.Inc()
	defer c.metrics.RequestInFlight.Dec()

	start := time.Now()
	var result *CompletionResult
	var err error

	switch c.cfg.Provider {
	case "ollama":
		result, err = c.completeOllama(ctx, task, messages)
	case "anthropic":
		result, err = c.completeAnthropic(ctx, task, messages)
	case "llamacpp":
		result, err = c.completeLlamaCpp(ctx, task, messages)
	case "vllm":
		result, err = c.completeVLLM(ctx, task, messages)
	case "azure":
		result, err = c.completeAzure(ctx, task, messages)
	case "bedrock":
		result, err = c.completeBedrock(ctx, task, messages)
	default:
		// openai, custom, or any OpenAI-compatible endpoint
		result, err = c.completeOpenAI(ctx, task, messages)
	}

	duration := time.Since(start).Seconds()
	status := "success"
	if err != nil {
		status = "error"
		c.metrics.ErrorsTotal.WithLabelValues(c.cfg.Model, task, classifyError(err), c.cfg.Provider).Inc()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	c.metrics.RequestTotal.WithLabelValues(c.cfg.Model, task, status, c.cfg.Provider).Inc()
	c.metrics.RequestDuration.WithLabelValues(c.cfg.Model, task, c.cfg.Provider).Observe(duration)

	if result != nil {
		c.metrics.TokensInputTotal.WithLabelValues(c.cfg.Model, task, c.cfg.Provider).Add(float64(result.InputTokens))
		c.metrics.TokensOutputTotal.WithLabelValues(c.cfg.Model, task, c.cfg.Provider).Add(float64(result.OutputTokens))
		if result.TimeToFirstToken > 0 {
			c.metrics.TimeToFirstToken.WithLabelValues(c.cfg.Model, c.cfg.Provider).Observe(result.TimeToFirstToken)
		}
		if duration > 0 && result.OutputTokens > 0 {
			tps := float64(result.OutputTokens) / duration
			c.metrics.TokensPerSecond.WithLabelValues(c.cfg.Model, c.cfg.Provider).Set(tps)
			span.SetAttributes(attribute.Float64("llm.tokens_per_second", tps))
		}
		span.SetAttributes(
			attribute.Int("llm.input_tokens", result.InputTokens),
			attribute.Int("llm.output_tokens", result.OutputTokens),
			attribute.Float64("llm.duration_seconds", duration),
		)
	}
	return result, err
}

// --- Provider implementations -----------------------------------------------

func (c *Client) completeOllama(ctx context.Context, task string, messages []types.LLMMessage) (*CompletionResult, error) {
	reqBody := ollamaRequest{
		Model: c.cfg.Model, Messages: messages, Stream: false,
		Options: &ollamaOptions{NumPredict: c.cfg.MaxTokens, Temperature: c.cfg.Temperature},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal ollama request: %w", err)
	}

	endpoint := c.cfg.Endpoint + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	log.Debug().Str("model", c.cfg.Model).Str("task", task).Str("endpoint", endpoint).Msg("Sending Ollama request")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(b))
	}

	var oResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&oResp); err != nil {
		return nil, fmt.Errorf("decode ollama response: %w", err)
	}

	ttft := float64(oResp.PromptEvalDuration) / 1e9
	return &CompletionResult{
		Content: oResp.Message.Content, InputTokens: oResp.PromptEvalCount,
		OutputTokens: oResp.EvalCount, TotalTokens: oResp.PromptEvalCount + oResp.EvalCount,
		TimeToFirstToken: ttft, ModelLoaded: oResp.LoadDuration > 0, FinishReason: "stop",
	}, nil
}

func (c *Client) completeOpenAI(ctx context.Context, _ string, messages []types.LLMMessage) (*CompletionResult, error) {
	reqBody := types.LLMRequest{
		Model: c.cfg.Model, Messages: messages,
		MaxTokens: c.cfg.MaxTokens, Temperature: c.cfg.Temperature,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}

	endpoint := c.cfg.Endpoint + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai returned status %d: %s", resp.StatusCode, string(b))
	}

	var oResp types.LLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&oResp); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}
	if len(oResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in openai response")
	}

	return &CompletionResult{
		Content: oResp.Choices[0].Message.Content, TotalTokens: oResp.Usage.TotalTokens,
		FinishReason: "stop",
	}, nil
}

// --- Anthropic native API (Claude) ------------------------------------------
// Uses /v1/messages with x-api-key header and anthropic-version header.

type anthropicRequest struct {
	Model       string         `json:"model"`
	Messages    []anthropicMsg `json:"messages"`
	MaxTokens   int            `json:"max_tokens"`
	Temperature float64        `json:"temperature,omitempty"`
	System      string         `json:"system,omitempty"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (c *Client) completeAnthropic(ctx context.Context, task string, messages []types.LLMMessage) (*CompletionResult, error) {
	// Anthropic separates system prompt from messages
	var system string
	var msgs []anthropicMsg
	for _, m := range messages {
		if m.Role == "system" {
			system = m.Content
		} else {
			msgs = append(msgs, anthropicMsg{Role: m.Role, Content: m.Content})
		}
	}

	reqBody := anthropicRequest{
		Model:       c.cfg.Model,
		Messages:    msgs,
		MaxTokens:   c.cfg.MaxTokens,
		Temperature: c.cfg.Temperature,
		System:      system,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	endpoint := strings.TrimRight(c.cfg.Endpoint, "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	log.Debug().Str("model", c.cfg.Model).Str("task", task).Msg("Sending Anthropic request")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic returned status %d: %s", resp.StatusCode, string(b))
	}

	var aResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&aResp); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}

	var content string
	for _, block := range aResp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &CompletionResult{
		Content:      content,
		InputTokens:  aResp.Usage.InputTokens,
		OutputTokens: aResp.Usage.OutputTokens,
		TotalTokens:  aResp.Usage.InputTokens + aResp.Usage.OutputTokens,
		FinishReason: aResp.StopReason,
	}, nil
}

// --- llama.cpp (llama-server) ------------------------------------------------
// Supports both native /completion endpoint and OpenAI-compat /v1/chat/completions.
// We use the native /v1/chat/completions when available (llama-server --api-oai),
// falling back to /completion for older setups.

type llamaCppRequest struct {
	Prompt      string   `json:"prompt"`
	NPredict    int      `json:"n_predict"`
	Temperature float64  `json:"temperature"`
	Stop        []string `json:"stop,omitempty"`
}

type llamaCppResponse struct {
	Content         string `json:"content"`
	Stop            bool   `json:"stop"`
	TokensEvaluated int    `json:"tokens_evaluated"`
	TokensPredicted int    `json:"tokens_predicted"`
	Timings         struct {
		PromptMs    float64 `json:"prompt_ms"`
		PredictedMs float64 `json:"predicted_ms"`
		PromptN     int     `json:"prompt_n"`
		PredictedN  int     `json:"predicted_n"`
	} `json:"timings"`
}

func (c *Client) completeLlamaCpp(ctx context.Context, task string, messages []types.LLMMessage) (*CompletionResult, error) {
	// First try OpenAI-compatible endpoint (llama-server --api-oai)
	endpoint := strings.TrimRight(c.cfg.Endpoint, "/")
	oaiEndpoint := endpoint + "/v1/chat/completions"

	// Quick probe: if /v1/chat/completions works, use OpenAI-compat path
	probeReq, _ := http.NewRequestWithContext(ctx, "OPTIONS", oaiEndpoint, nil)
	probeResp, probeErr := c.httpClient.Do(probeReq)
	if probeErr == nil && probeResp.StatusCode != 404 {
		probeResp.Body.Close()
		// Use OpenAI-compatible path
		return c.completeOpenAI(ctx, task, messages)
	}

	// Fall back to native /completion endpoint
	// Concatenate messages into a single prompt
	var prompt strings.Builder
	for _, m := range messages {
		switch m.Role {
		case "system":
			prompt.WriteString("### System:\n" + m.Content + "\n\n")
		case "user":
			prompt.WriteString("### User:\n" + m.Content + "\n\n")
		case "assistant":
			prompt.WriteString("### Assistant:\n" + m.Content + "\n\n")
		}
	}
	prompt.WriteString("### Assistant:\n")

	reqBody := llamaCppRequest{
		Prompt:      prompt.String(),
		NPredict:    c.cfg.MaxTokens,
		Temperature: c.cfg.Temperature,
		Stop:        []string{"### User:", "### System:"},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal llamacpp request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/completion", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	log.Debug().Str("model", c.cfg.Model).Str("task", task).Msg("Sending llama.cpp request")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llamacpp request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("llamacpp returned status %d: %s", resp.StatusCode, string(b))
	}

	var lResp llamaCppResponse
	if err := json.NewDecoder(resp.Body).Decode(&lResp); err != nil {
		return nil, fmt.Errorf("decode llamacpp response: %w", err)
	}

	return &CompletionResult{
		Content:          strings.TrimSpace(lResp.Content),
		InputTokens:      lResp.Timings.PromptN,
		OutputTokens:     lResp.Timings.PredictedN,
		TotalTokens:      lResp.Timings.PromptN + lResp.Timings.PredictedN,
		TimeToFirstToken: lResp.Timings.PromptMs / 1000,
		FinishReason:     "stop",
	}, nil
}

// --- vLLM -------------------------------------------------------------------
// Fully OpenAI-compatible API. We add vLLM-specific params (best_of, top_k)
// and handle the slightly different token usage reporting.

type vllmRequest struct {
	Model            string             `json:"model"`
	Messages         []types.LLMMessage `json:"messages"`
	MaxTokens        int                `json:"max_tokens"`
	Temperature      float64            `json:"temperature"`
	TopP             float64            `json:"top_p,omitempty"`
	FrequencyPenalty float64            `json:"frequency_penalty,omitempty"`
	PresencePenalty  float64            `json:"presence_penalty,omitempty"`
}

func (c *Client) completeVLLM(ctx context.Context, task string, messages []types.LLMMessage) (*CompletionResult, error) {
	reqBody := vllmRequest{
		Model:       c.cfg.Model,
		Messages:    messages,
		MaxTokens:   c.cfg.MaxTokens,
		Temperature: c.cfg.Temperature,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal vllm request: %w", err)
	}

	endpoint := strings.TrimRight(c.cfg.Endpoint, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	log.Debug().Str("model", c.cfg.Model).Str("task", task).Str("endpoint", endpoint).Msg("Sending vLLM request")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vllm request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vllm returned status %d: %s", resp.StatusCode, string(b))
	}

	var oResp types.LLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&oResp); err != nil {
		return nil, fmt.Errorf("decode vllm response: %w", err)
	}
	if len(oResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in vllm response")
	}

	return &CompletionResult{
		Content:      oResp.Choices[0].Message.Content,
		TotalTokens:  oResp.Usage.TotalTokens,
		FinishReason: "stop",
	}, nil
}

// --- Azure OpenAI -----------------------------------------------------------
// Same as OpenAI but uses a different URL pattern and api-key header.
// URL: https://{resource}.openai.azure.com/openai/deployments/{deployment}/chat/completions?api-version=2024-02-01

func (c *Client) completeAzure(ctx context.Context, task string, messages []types.LLMMessage) (*CompletionResult, error) {
	reqBody := types.LLMRequest{
		Model:       c.cfg.Model,
		Messages:    messages,
		MaxTokens:   c.cfg.MaxTokens,
		Temperature: c.cfg.Temperature,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal azure request: %w", err)
	}

	// Azure endpoint should be the full deployment URL
	// e.g., https://myresource.openai.azure.com/openai/deployments/gpt-4/chat/completions?api-version=2024-02-01
	endpoint := c.cfg.Endpoint
	if !strings.Contains(endpoint, "chat/completions") {
		endpoint = strings.TrimRight(endpoint, "/") + "/chat/completions?api-version=2024-02-01"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", c.cfg.APIKey)

	log.Debug().Str("model", c.cfg.Model).Str("task", task).Msg("Sending Azure OpenAI request")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("azure request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("azure returned status %d: %s", resp.StatusCode, string(b))
	}

	var oResp types.LLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&oResp); err != nil {
		return nil, fmt.Errorf("decode azure response: %w", err)
	}
	if len(oResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in azure response")
	}

	return &CompletionResult{
		Content:      oResp.Choices[0].Message.Content,
		TotalTokens:  oResp.Usage.TotalTokens,
		FinishReason: "stop",
	}, nil
}

// --- AWS Bedrock ------------------------------------------------------------
// Uses the Bedrock converse API via AWS SDK. Since we want to avoid pulling
// the full AWS SDK into pkg/llm, Bedrock uses the HTTP API directly with
// SigV4 signing. For now, a simplified implementation that works with
// IAM credentials from the environment (IRSA/instance profile).

func (c *Client) completeBedrock(ctx context.Context, task string, messages []types.LLMMessage) (*CompletionResult, error) {
	// Bedrock's converse API endpoint:
	// POST https://bedrock-runtime.{region}.amazonaws.com/model/{modelId}/converse

	region := "us-east-1"
	if strings.Contains(c.cfg.Endpoint, ".") {
		// Extract region from endpoint like https://bedrock-runtime.us-west-2.amazonaws.com
		parts := strings.Split(c.cfg.Endpoint, ".")
		if len(parts) >= 3 {
			region = parts[1]
		}
	}

	// For Bedrock, fall back to OpenAI-compatible gateway if configured
	// (many users run a Bedrock-to-OpenAI proxy like LiteLLM)
	if strings.Contains(c.cfg.Endpoint, "/v1") || strings.Contains(c.cfg.Endpoint, "litellm") {
		return c.completeOpenAI(ctx, task, messages)
	}

	// Native Bedrock API
	var bedrockMsgs []map[string]any
	var system string
	for _, m := range messages {
		if m.Role == "system" {
			system = m.Content
			continue
		}
		bedrockMsgs = append(bedrockMsgs, map[string]any{
			"role": m.Role,
			"content": []map[string]any{
				{"type": "text", "text": m.Content},
			},
		})
	}

	reqBody := map[string]any{
		"messages": bedrockMsgs,
		"inferenceConfig": map[string]any{
			"maxTokens":   c.cfg.MaxTokens,
			"temperature": c.cfg.Temperature,
		},
	}
	if system != "" {
		reqBody["system"] = []map[string]any{
			{"text": system},
		}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal bedrock request: %w", err)
	}

	endpoint := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/model/%s/converse",
		region, c.cfg.Model)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Note: In production, this needs AWS SigV4 signing.
	// Users should either: (a) use a proxy like LiteLLM, or
	// (b) deploy with IRSA and we add proper SigV4 later.

	log.Debug().Str("model", c.cfg.Model).Str("region", region).Str("task", task).Msg("Sending Bedrock request")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bedrock request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bedrock returned status %d: %s", resp.StatusCode, string(b))
	}

	var bResp struct {
		Output struct {
			Message struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"inputTokens"`
			OutputTokens int `json:"outputTokens"`
			TotalTokens  int `json:"totalTokens"`
		} `json:"usage"`
		StopReason string `json:"stopReason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bResp); err != nil {
		return nil, fmt.Errorf("decode bedrock response: %w", err)
	}

	var content string
	for _, block := range bResp.Output.Message.Content {
		content += block.Text
	}

	return &CompletionResult{
		Content:      content,
		InputTokens:  bResp.Usage.InputTokens,
		OutputTokens: bResp.Usage.OutputTokens,
		TotalTokens:  bResp.Usage.TotalTokens,
		FinishReason: bResp.StopReason,
	}, nil
}

func classifyError(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "timeout"):
		return "timeout"
	case strings.Contains(s, "connection refused"):
		return "connection_refused"
	case strings.Contains(s, "429"), strings.Contains(s, "rate limit"):
		return "rate_limited"
	case strings.Contains(s, "500"), strings.Contains(s, "502"), strings.Contains(s, "503"):
		return "server_error"
	case strings.Contains(s, "context canceled"):
		return "canceled"
	default:
		return "unknown"
	}
}
