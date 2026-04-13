package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// LLMConfigAPI exposes the current LLM configuration and allows runtime updates.
type LLMConfigAPI struct {
	mu     sync.RWMutex
	config LLMConfigState
}

// LLMConfigState is the API-visible LLM configuration.
type LLMConfigState struct {
	Provider             string  `json:"provider"`
	Endpoint             string  `json:"endpoint"`
	Model                string  `json:"model"`
	MaxTokens            int     `json:"maxTokens"`
	Temperature          float64 `json:"temperature"`
	DailyTokenBudget     int     `json:"dailyTokenBudget"`
	APIKeySet            bool    `json:"apiKeySet"` // true if key is configured (never expose the key itself)
	ExplainOptimizations bool    `json:"explainOptimizations"`
}

// ProviderDefaults returns sensible defaults for a given provider.
var ProviderDefaults = map[string]LLMConfigState{
	"anthropic": {Provider: "anthropic", Endpoint: "https://api.anthropic.com", Model: "claude-sonnet-4-6", MaxTokens: 4096, Temperature: 0.2, DailyTokenBudget: 1000000},
	"openai":    {Provider: "openai", Endpoint: "https://api.openai.com/v1", Model: "gpt-4-turbo", MaxTokens: 4096, Temperature: 0.3, DailyTokenBudget: 1000000},
	"ollama":    {Provider: "ollama", Endpoint: "http://localhost:11434", Model: "llama3", MaxTokens: 2048, Temperature: 0.2, DailyTokenBudget: 0},
	"vllm":     {Provider: "vllm", Endpoint: "http://localhost:8000", Model: "meta-llama/Llama-3-70b-chat-hf", MaxTokens: 4096, Temperature: 0.2, DailyTokenBudget: 0},
	"llamacpp": {Provider: "llamacpp", Endpoint: "http://localhost:8080", Model: "local", MaxTokens: 2048, Temperature: 0.2, DailyTokenBudget: 0},
	"azure":    {Provider: "azure", Endpoint: "https://YOUR_RESOURCE.openai.azure.com/openai/deployments/gpt-4", Model: "gpt-4", MaxTokens: 4096, Temperature: 0.3, DailyTokenBudget: 1000000},
	"bedrock":  {Provider: "bedrock", Endpoint: "https://bedrock-runtime.us-east-1.amazonaws.com", Model: "anthropic.claude-3-sonnet-20240229-v1:0", MaxTokens: 4096, Temperature: 0.2, DailyTokenBudget: 1000000},
	"custom":   {Provider: "custom", Endpoint: "http://localhost:8000/v1", Model: "default", MaxTokens: 4096, Temperature: 0.3, DailyTokenBudget: 0},
}

func NewLLMConfigAPI(provider, endpoint, model, apiKey string, maxTokens int, temperature float64, dailyBudget int) *LLMConfigAPI {
	return &LLMConfigAPI{
		config: LLMConfigState{
			Provider:         provider,
			Endpoint:         endpoint,
			Model:            model,
			MaxTokens:        maxTokens,
			Temperature:      temperature,
			DailyTokenBudget: dailyBudget,
			APIKeySet:        apiKey != "",
		},
	}
}

func (a *LLMConfigAPI) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/llm/config", a.handleGet)
	mux.HandleFunc("PUT /api/v1/llm/config", a.handleUpdate)
	mux.HandleFunc("GET /api/v1/llm/providers", a.handleProviders)
	mux.HandleFunc("POST /api/v1/llm/discover-models", a.handleDiscoverModels)
}

func (a *LLMConfigAPI) handleGet(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	writeJSON(w, a.config)
}

func (a *LLMConfigAPI) handleProviders(w http.ResponseWriter, r *http.Request) {
	type providerInfo struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		DefaultEndpoint string `json:"defaultEndpoint"`
		DefaultModel    string `json:"defaultModel"`
		RequiresAPIKey  bool   `json:"requiresApiKey"`
		Description     string `json:"description"`
	}

	providers := []providerInfo{
		{ID: "anthropic", Name: "Anthropic (Claude)", DefaultEndpoint: "https://api.anthropic.com", DefaultModel: "claude-sonnet-4-6", RequiresAPIKey: true, Description: "Claude models via native API"},
		{ID: "openai", Name: "OpenAI", DefaultEndpoint: "https://api.openai.com/v1", DefaultModel: "gpt-4-turbo", RequiresAPIKey: true, Description: "GPT models via OpenAI API"},
		{ID: "ollama", Name: "Ollama (Local)", DefaultEndpoint: "http://localhost:11434", DefaultModel: "llama3", RequiresAPIKey: false, Description: "Local models via Ollama"},
		{ID: "vllm", Name: "vLLM", DefaultEndpoint: "http://localhost:8000", DefaultModel: "meta-llama/Llama-3-70b-chat-hf", RequiresAPIKey: false, Description: "Self-hosted models via vLLM"},
		{ID: "llamacpp", Name: "llama.cpp", DefaultEndpoint: "http://localhost:8080", DefaultModel: "local", RequiresAPIKey: false, Description: "Local models via llama-server"},
		{ID: "azure", Name: "Azure OpenAI", DefaultEndpoint: "https://YOUR_RESOURCE.openai.azure.com/openai/deployments/gpt-4", DefaultModel: "gpt-4", RequiresAPIKey: true, Description: "GPT models via Azure OpenAI"},
		{ID: "bedrock", Name: "AWS Bedrock", DefaultEndpoint: "https://bedrock-runtime.us-east-1.amazonaws.com", DefaultModel: "anthropic.claude-3-sonnet-20240229-v1:0", RequiresAPIKey: false, Description: "Claude/Llama via AWS Bedrock (uses IRSA)"},
		{ID: "custom", Name: "Custom (OpenAI-compatible)", DefaultEndpoint: "http://localhost:8000/v1", DefaultModel: "default", RequiresAPIKey: false, Description: "Any OpenAI-compatible endpoint"},
	}
	writeJSON(w, map[string]any{"providers": providers})
}

// handleDiscoverModels probes a provider endpoint and returns available models.
// Each provider exposes models differently:
//   - OpenAI/vLLM/custom: GET /v1/models
//   - Ollama: GET /api/tags
//   - llama.cpp: GET /v1/models or GET /props (single model)
//   - Anthropic: no discovery API (returns hardcoded list)
//   - Azure: no discovery API (returns hardcoded list)
//   - Bedrock: no discovery API (returns hardcoded list)
func (a *LLMConfigAPI) handleDiscoverModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Endpoint string `json:"endpoint"`
		APIKey   string `json:"apiKey,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Endpoint == "" {
		http.Error(w, "endpoint required", http.StatusBadRequest)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	endpoint := strings.TrimRight(req.Endpoint, "/")

	type modelInfo struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Size        string `json:"size,omitempty"`
		Description string `json:"description,omitempty"`
	}

	var models []modelInfo
	var probeErr string

	switch req.Provider {
	case "ollama":
		// Ollama: GET /api/tags
		resp, err := client.Get(endpoint + "/api/tags")
		if err != nil {
			probeErr = fmt.Sprintf("Cannot reach Ollama at %s: %v", endpoint, err)
			break
		}
		defer resp.Body.Close()
		var data struct {
			Models []struct {
				Name       string `json:"name"`
				Model      string `json:"model"`
				Size       int64  `json:"size"`
				ModifiedAt string `json:"modified_at"`
				Details    struct {
					Family            string `json:"family"`
					ParameterSize     string `json:"parameter_size"`
					QuantizationLevel string `json:"quantization_level"`
				} `json:"details"`
			} `json:"models"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			probeErr = fmt.Sprintf("Failed to parse Ollama response: %v", err)
			break
		}
		for _, m := range data.Models {
			name := m.Name
			if name == "" {
				name = m.Model
			}
			size := ""
			if m.Details.ParameterSize != "" {
				size = m.Details.ParameterSize
			} else if m.Size > 0 {
				size = fmt.Sprintf("%.1fGB", float64(m.Size)/(1024*1024*1024))
			}
			desc := ""
			if m.Details.Family != "" {
				desc = m.Details.Family
				if m.Details.QuantizationLevel != "" {
					desc += " " + m.Details.QuantizationLevel
				}
			}
			models = append(models, modelInfo{ID: name, Name: name, Size: size, Description: desc})
		}

	case "llamacpp":
		// llama.cpp: try /v1/models first (OpenAI compat), then /props
		resp, err := client.Get(endpoint + "/v1/models")
		if err == nil && resp.StatusCode == 200 {
			var data struct {
				Data []struct {
					ID      string `json:"id"`
					Object  string `json:"object"`
					OwnedBy string `json:"owned_by"`
				} `json:"data"`
			}
			json.NewDecoder(resp.Body).Decode(&data)
			resp.Body.Close()
			for _, m := range data.Data {
				models = append(models, modelInfo{ID: m.ID, Name: m.ID, Description: m.OwnedBy})
			}
		} else {
			if resp != nil {
				resp.Body.Close()
			}
			// Try /props (llama-server native)
			resp2, err2 := client.Get(endpoint + "/props")
			if err2 == nil && resp2.StatusCode == 200 {
				var props struct {
					DefaultGenSettings struct {
						Model string `json:"model"`
					} `json:"default_generation_settings"`
				}
				json.NewDecoder(resp2.Body).Decode(&props)
				resp2.Body.Close()
				if props.DefaultGenSettings.Model != "" {
					models = append(models, modelInfo{ID: props.DefaultGenSettings.Model, Name: props.DefaultGenSettings.Model, Description: "loaded model"})
				}
			} else {
				if resp2 != nil {
					resp2.Body.Close()
				}
				probeErr = fmt.Sprintf("Cannot reach llama.cpp at %s", endpoint)
			}
		}

	case "openai", "vllm", "custom":
		// OpenAI-compatible: GET /v1/models (or /models)
		url := endpoint + "/v1/models"
		if strings.HasSuffix(endpoint, "/v1") {
			url = endpoint + "/models"
		}
		httpReq, _ := http.NewRequest("GET", url, nil)
		if req.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			probeErr = fmt.Sprintf("Cannot reach %s: %v", url, err)
			break
		}
		defer resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			probeErr = "Authentication failed — check your API key"
			break
		}
		var data struct {
			Data []struct {
				ID      string `json:"id"`
				Object  string `json:"object"`
				OwnedBy string `json:"owned_by"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			probeErr = fmt.Sprintf("Failed to parse models response: %v", err)
			break
		}
		for _, m := range data.Data {
			models = append(models, modelInfo{ID: m.ID, Name: m.ID, Description: m.OwnedBy})
		}

	case "anthropic":
		// Anthropic has no model list API — return known models
		models = []modelInfo{
			{ID: "claude-opus-4-6", Name: "Claude Opus 4.6", Description: "Most capable"},
			{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Description: "Best balance of speed and capability"},
			{ID: "claude-haiku-4-5-20251001", Name: "Claude Haiku 4.5", Description: "Fastest"},
			{ID: "claude-sonnet-4-5-20241022", Name: "Claude Sonnet 4.5", Description: "Previous generation"},
		}

	case "azure":
		// Azure has no standard list — return common deployments
		models = []modelInfo{
			{ID: "gpt-4", Name: "GPT-4", Description: "Deployment name — must match your Azure deployment"},
			{ID: "gpt-4-turbo", Name: "GPT-4 Turbo", Description: ""},
			{ID: "gpt-4o", Name: "GPT-4o", Description: ""},
			{ID: "gpt-35-turbo", Name: "GPT-3.5 Turbo", Description: ""},
		}

	case "bedrock":
		// Bedrock model IDs
		models = []modelInfo{
			{ID: "anthropic.claude-3-5-sonnet-20241022-v2:0", Name: "Claude 3.5 Sonnet v2", Description: "Anthropic via Bedrock"},
			{ID: "anthropic.claude-3-sonnet-20240229-v1:0", Name: "Claude 3 Sonnet", Description: "Anthropic via Bedrock"},
			{ID: "anthropic.claude-3-haiku-20240307-v1:0", Name: "Claude 3 Haiku", Description: "Anthropic via Bedrock"},
			{ID: "meta.llama3-70b-instruct-v1:0", Name: "Llama 3 70B", Description: "Meta via Bedrock"},
			{ID: "meta.llama3-8b-instruct-v1:0", Name: "Llama 3 8B", Description: "Meta via Bedrock"},
			{ID: "amazon.titan-text-express-v1", Name: "Titan Text Express", Description: "Amazon"},
		}

	default:
		probeErr = fmt.Sprintf("Unknown provider: %s", req.Provider)
	}

	writeJSON(w, map[string]any{
		"models":   models,
		"error":    probeErr,
		"provider": req.Provider,
		"endpoint": req.Endpoint,
	})
}

func (a *LLMConfigAPI) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var update struct {
		Provider         string  `json:"provider"`
		Endpoint         string  `json:"endpoint"`
		Model            string  `json:"model"`
		APIKey           string  `json:"apiKey,omitempty"`
		MaxTokens        int     `json:"maxTokens"`
		Temperature      float64 `json:"temperature"`
		DailyTokenBudget int     `json:"dailyTokenBudget"`
	}
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	a.mu.Lock()
	if update.Provider != "" {
		a.config.Provider = update.Provider
	}
	if update.Endpoint != "" {
		a.config.Endpoint = update.Endpoint
	}
	if update.Model != "" {
		a.config.Model = update.Model
	}
	if update.MaxTokens > 0 {
		a.config.MaxTokens = update.MaxTokens
	}
	if update.Temperature > 0 {
		a.config.Temperature = update.Temperature
	}
	if update.DailyTokenBudget >= 0 {
		a.config.DailyTokenBudget = update.DailyTokenBudget
	}
	if update.APIKey != "" {
		a.config.APIKeySet = true
	}
	a.mu.Unlock()

	log.Info().Str("provider", a.config.Provider).Str("model", a.config.Model).Str("endpoint", a.config.Endpoint).Msg("LLM config updated via UI")

	a.mu.RLock()
	defer a.mu.RUnlock()
	writeJSON(w, a.config)
}

// ---------------------------------------------------------------------------
// Smart model router: auto-detect the best available model at startup
// ---------------------------------------------------------------------------

// AutoDetectModel probes the configured LLM endpoint and returns the best
// available model name. Prefers larger models and instruction-tuned variants.
// Returns the original model name unchanged if detection fails or the model exists.
type discoveredModel struct {
	id   string
	size string
}

func AutoDetectModel(provider, endpoint, currentModel, apiKey string) string {
	client := &http.Client{Timeout: 10 * time.Second}
	baseEndpoint := strings.TrimRight(endpoint, "/")
	var models []discoveredModel

	switch provider {
	case "ollama":
		ollamaBase := strings.TrimSuffix(baseEndpoint, "/v1")
		resp, err := client.Get(ollamaBase + "/api/tags")
		if err != nil {
			log.Debug().Err(err).Msg("Model auto-detect: cannot reach Ollama")
			return currentModel
		}
		defer resp.Body.Close()
		var data struct {
			Models []struct {
				Name    string `json:"name"`
				Details struct {
					ParameterSize string `json:"parameter_size"`
				} `json:"details"`
			} `json:"models"`
		}
		if json.NewDecoder(resp.Body).Decode(&data) == nil {
			for _, m := range data.Models {
				models = append(models, discoveredModel{id: m.Name, size: m.Details.ParameterSize})
			}
		}

	case "llamacpp":
		url := baseEndpoint + "/v1/models"
		resp, err := client.Get(url)
		if err != nil {
			return currentModel
		}
		defer resp.Body.Close()
		var data struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if json.NewDecoder(resp.Body).Decode(&data) == nil {
			for _, m := range data.Data {
				models = append(models, discoveredModel{id: m.ID})
			}
		}

	case "openai", "vllm":
		url := baseEndpoint + "/models"
		if !strings.HasSuffix(baseEndpoint, "/v1") {
			url = baseEndpoint + "/v1/models"
		}
		httpReq, _ := http.NewRequest("GET", url, nil)
		if apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			return currentModel
		}
		defer resp.Body.Close()
		var data struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if json.NewDecoder(resp.Body).Decode(&data) == nil {
			for _, m := range data.Data {
				models = append(models, discoveredModel{id: m.ID})
			}
		}

	default:
		return currentModel
	}

	if len(models) == 0 {
		log.Warn().Str("provider", provider).Str("endpoint", endpoint).Msg("Model auto-detect: no models found")
		return currentModel
	}

	// Check if the configured model exists
	for _, m := range models {
		if m.id == currentModel {
			log.Info().Str("model", currentModel).Msg("Configured model found at endpoint")
			return currentModel
		}
	}

	// Configured model not found — pick the best available
	best := selectBestModel(models)
	log.Warn().
		Str("configured", currentModel).
		Str("selected", best).
		Int("available", len(models)).
		Msg("Configured model not found — auto-selected best available model")
	return best
}

func selectBestModel(models []discoveredModel) string {
	if len(models) == 0 {
		return ""
	}

	type scored struct {
		id    string
		score int
	}
	var candidates []scored
	for _, m := range models {
		s := 0
		lower := strings.ToLower(m.id)

		if strings.Contains(lower, "instruct") {
			s += 50
		}
		if strings.Contains(lower, "chat") {
			s += 40
		}
		if strings.Contains(lower, "70b") || strings.Contains(lower, "72b") {
			s += 60
		}
		if strings.Contains(lower, "14b") || strings.Contains(lower, "13b") {
			s += 30
		}
		if strings.Contains(lower, "7b") || strings.Contains(lower, "8b") {
			s += 20
		}
		if strings.Contains(lower, "qwen") {
			s += 15
		}
		if strings.Contains(lower, "deepseek") || strings.Contains(lower, "coder") {
			s += 15
		}
		if strings.Contains(lower, "llama") {
			s += 10
		}
		if strings.Contains(lower, "mistral") || strings.Contains(lower, "codestral") {
			s += 10
		}
		if strings.Contains(lower, "embed") || strings.Contains(lower, "nomic") {
			s -= 100
		}

		candidates = append(candidates, scored{id: m.id, score: s})
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}
	return best.id
}
