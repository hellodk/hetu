// Package main provides enhanced LLM metrics collection
// This file adds detailed Prometheus metrics for LLM request monitoring

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// LLMMetrics holds all LLM-related Prometheus metrics
type LLMMetrics struct {
	// Request metrics
	RequestTotal     *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	RequestInFlight  prometheus.Gauge
	
	// Token metrics
	TokensInputTotal  *prometheus.CounterVec
	TokensOutputTotal *prometheus.CounterVec
	
	// Performance metrics
	TimeToFirstToken *prometheus.HistogramVec
	TokensPerSecond  *prometheus.GaugeVec
	
	// Error metrics
	ErrorsTotal *prometheus.CounterVec
	
	// Queue metrics (for tracking concurrency)
	QueueWaitTime *prometheus.HistogramVec
}

// LLMClient wraps the HTTP client with metrics and tracing
type LLMClient struct {
	httpClient *http.Client
	metrics    *LLMMetrics
	tracer     trace.Tracer
	config     LLMClientConfig
}

// LLMClientConfig holds configuration for the LLM client
type LLMClientConfig struct {
	Endpoint     string
	Model        string
	APIKey       string
	MaxTokens    int
	Temperature  float64
	Timeout      time.Duration
	Provider     string // openai, ollama, azure, anthropic
	MaxRetries   int
	RetryBackoff []time.Duration
}

// OllamaRequest represents a request to Ollama's native API
type OllamaRequest struct {
	Model    string       `json:"model"`
	Messages []LLMMessage `json:"messages"`
	Stream   bool         `json:"stream"`
	Options  *OllamaOptions `json:"options,omitempty"`
}

// OllamaOptions represents Ollama-specific options
type OllamaOptions struct {
	NumPredict  int     `json:"num_predict,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
}

// OllamaResponse represents a response from Ollama's native API
type OllamaResponse struct {
	Model     string `json:"model"`
	Message   struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done              bool   `json:"done"`
	TotalDuration     int64  `json:"total_duration"`
	LoadDuration      int64  `json:"load_duration"`
	PromptEvalCount   int    `json:"prompt_eval_count"`
	PromptEvalDuration int64 `json:"prompt_eval_duration"`
	EvalCount         int    `json:"eval_count"`
	EvalDuration      int64  `json:"eval_duration"`
}

// NewLLMMetrics creates and registers all LLM metrics
func NewLLMMetrics(namespace string) *LLMMetrics {
	return &LLMMetrics{
		RequestTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "llm_request_total",
				Help:      "Total number of LLM requests",
			},
			[]string{"model", "task", "status", "provider"},
		),
		RequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "llm_request_duration_seconds",
				Help:      "LLM request duration in seconds",
				Buckets:   []float64{0.5, 1, 2, 5, 10, 20, 30, 45, 60, 90, 120},
			},
			[]string{"model", "task", "provider"},
		),
		RequestInFlight: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "llm_request_in_flight",
				Help:      "Number of LLM requests currently in flight",
			},
		),
		TokensInputTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "llm_tokens_input_total",
				Help:      "Total input tokens sent to LLM",
			},
			[]string{"model", "task", "provider"},
		),
		TokensOutputTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "llm_tokens_output_total",
				Help:      "Total output tokens received from LLM",
			},
			[]string{"model", "task", "provider"},
		),
		TimeToFirstToken: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "llm_time_to_first_token_seconds",
				Help:      "Time to first token in seconds",
				Buckets:   []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 20},
			},
			[]string{"model", "provider"},
		),
		TokensPerSecond: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "llm_tokens_per_second",
				Help:      "Token generation rate",
			},
			[]string{"model", "provider"},
		),
		ErrorsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "llm_errors_total",
				Help:      "Total number of LLM errors",
			},
			[]string{"model", "task", "error_type", "provider"},
		),
		QueueWaitTime: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "llm_queue_wait_seconds",
				Help:      "Time spent waiting in queue before processing",
				Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10},
			},
			[]string{"provider"},
		),
	}
}

// NewLLMClient creates a new instrumented LLM client
func NewLLMClient(config LLMClientConfig, metrics *LLMMetrics) *LLMClient {
	if config.Timeout == 0 {
		config.Timeout = 60 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if len(config.RetryBackoff) == 0 {
		config.RetryBackoff = []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	}
	
	return &LLMClient{
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		metrics: metrics,
		tracer:  otel.Tracer("llm-client"),
		config:  config,
	}
}

// Complete sends a completion request to the LLM with full instrumentation
func (c *LLMClient) Complete(ctx context.Context, task string, messages []LLMMessage) (*LLMCompletionResult, error) {
	// Start tracing span
	ctx, span := c.tracer.Start(ctx, "llm.complete",
		trace.WithAttributes(
			attribute.String("llm.model", c.config.Model),
			attribute.String("llm.provider", c.config.Provider),
			attribute.String("llm.task", task),
			attribute.Int("llm.message_count", len(messages)),
		),
	)
	defer span.End()
	
	// Track in-flight requests
	c.metrics.RequestInFlight.Inc()
	defer c.metrics.RequestInFlight.Dec()
	
	start := time.Now()
	
	var result *LLMCompletionResult
	var err error
	
	// Route to appropriate provider
	switch c.config.Provider {
	case "ollama":
		result, err = c.completeOllama(ctx, task, messages)
	default:
		result, err = c.completeOpenAI(ctx, task, messages)
	}
	
	duration := time.Since(start).Seconds()
	
	// Record metrics
	status := "success"
	if err != nil {
		status = "error"
		c.metrics.ErrorsTotal.WithLabelValues(c.config.Model, task, classifyError(err), c.config.Provider).Inc()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	
	c.metrics.RequestTotal.WithLabelValues(c.config.Model, task, status, c.config.Provider).Inc()
	c.metrics.RequestDuration.WithLabelValues(c.config.Model, task, c.config.Provider).Observe(duration)
	
	if result != nil {
		c.metrics.TokensInputTotal.WithLabelValues(c.config.Model, task, c.config.Provider).Add(float64(result.InputTokens))
		c.metrics.TokensOutputTotal.WithLabelValues(c.config.Model, task, c.config.Provider).Add(float64(result.OutputTokens))
		
		if result.TimeToFirstToken > 0 {
			c.metrics.TimeToFirstToken.WithLabelValues(c.config.Model, c.config.Provider).Observe(result.TimeToFirstToken)
		}
		
		if duration > 0 && result.OutputTokens > 0 {
			tokensPerSec := float64(result.OutputTokens) / duration
			c.metrics.TokensPerSecond.WithLabelValues(c.config.Model, c.config.Provider).Set(tokensPerSec)
			span.SetAttributes(attribute.Float64("llm.tokens_per_second", tokensPerSec))
		}
		
		span.SetAttributes(
			attribute.Int("llm.input_tokens", result.InputTokens),
			attribute.Int("llm.output_tokens", result.OutputTokens),
			attribute.Float64("llm.duration_seconds", duration),
		)
	}
	
	return result, err
}

// LLMCompletionResult holds the result of an LLM completion
type LLMCompletionResult struct {
	Content          string
	InputTokens      int
	OutputTokens     int
	TotalTokens      int
	TimeToFirstToken float64
	ModelLoaded      bool
	FinishReason     string
}

// completeOllama sends a request to Ollama's native API
func (c *LLMClient) completeOllama(ctx context.Context, task string, messages []LLMMessage) (*LLMCompletionResult, error) {
	reqBody := OllamaRequest{
		Model:    c.config.Model,
		Messages: messages,
		Stream:   false,
		Options: &OllamaOptions{
			NumPredict:  c.config.MaxTokens,
			Temperature: c.config.Temperature,
		},
	}
	
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	endpoint := c.config.Endpoint + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	
	log.Debug().
		Str("model", c.config.Model).
		Str("task", task).
		Str("endpoint", endpoint).
		Msg("Sending Ollama request")
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(body))
	}
	
	var ollamaResp OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to decode ollama response: %w", err)
	}
	
	// Calculate time to first token from Ollama's timing data
	// PromptEvalDuration is the time spent evaluating the prompt
	ttft := float64(ollamaResp.PromptEvalDuration) / 1e9 // Convert nanoseconds to seconds
	
	return &LLMCompletionResult{
		Content:          ollamaResp.Message.Content,
		InputTokens:      ollamaResp.PromptEvalCount,
		OutputTokens:     ollamaResp.EvalCount,
		TotalTokens:      ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		TimeToFirstToken: ttft,
		ModelLoaded:      ollamaResp.LoadDuration > 0,
		FinishReason:     "stop",
	}, nil
}

// completeOpenAI sends a request to OpenAI-compatible API
func (c *LLMClient) completeOpenAI(ctx context.Context, task string, messages []LLMMessage) (*LLMCompletionResult, error) {
	reqBody := LLMRequest{
		Model:       c.config.Model,
		Messages:    messages,
		MaxTokens:   c.config.MaxTokens,
		Temperature: c.config.Temperature,
	}
	
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	endpoint := c.config.Endpoint + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Content-Type", "application/json")
	if c.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai returned status %d: %s", resp.StatusCode, string(body))
	}
	
	var openaiResp LLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&openaiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	if len(openaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}
	
	return &LLMCompletionResult{
		Content:      openaiResp.Choices[0].Message.Content,
		TotalTokens:  openaiResp.Usage.TotalTokens,
		FinishReason: "stop",
	}, nil
}

// classifyError categorizes errors for metrics
func classifyError(err error) string {
	errStr := err.Error()
	switch {
	case contains(errStr, "timeout"):
		return "timeout"
	case contains(errStr, "connection refused"):
		return "connection_refused"
	case contains(errStr, "429"), contains(errStr, "rate limit"):
		return "rate_limited"
	case contains(errStr, "500"), contains(errStr, "502"), contains(errStr, "503"):
		return "server_error"
	case contains(errStr, "context canceled"):
		return "canceled"
	default:
		return "unknown"
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
