package main

// embedconfig.go — operator-facing embedding configuration.
//
// Mirrors llmconfig.go but for the embedding backend used by the RAG
// knowledge base and the near-duplicate error scorer. Embeddings often run
// on a different model (and sometimes a different endpoint) than the chat
// LLM — e.g. an Ollama server hosting both `mistral` and `nomic-embed-text`,
// or a dedicated TEI server. This API lets operators set that from the UI
// instead of only via env vars.
//
// State is held in-memory and applied at runtime via an onUpdate hook so the
// chat KB rebuilds its embedder without a pod restart. Non-secret fields are
// persisted into the runtime override layer (same pattern as the LLM config).

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// EmbeddingConfigState is the API-visible embedding configuration.
type EmbeddingConfigState struct {
	// Provider selects the request/response shape: "openai", "ollama", or
	// "auto" (heuristic from the endpoint URL). Defaults to "auto".
	Provider string `json:"provider"`
	// Endpoint is the base URL of the embeddings backend. Empty means "reuse
	// the chat LLM endpoint" (common when one Ollama server serves both).
	Endpoint string `json:"endpoint"`
	// Model is the embedding model name (e.g. nomic-embed-text, mxbai-embed-large,
	// text-embedding-3-small).
	Model string `json:"model"`
	// APIKeySet reports whether a key is configured (the key itself is never
	// returned). Defaults to reusing the LLM API key when empty.
	APIKeySet bool `json:"apiKeySet"`
	// Dimensions is the detected vector size, surfaced for the UI once known.
	Dimensions int `json:"dimensions"`
}

// EmbeddingConfigAPI exposes and mutates the embedding configuration.
type EmbeddingConfigAPI struct {
	mu       sync.RWMutex
	state    EmbeddingConfigState
	apiKey   string
	onUpdate func(state EmbeddingConfigState, apiKey string)
}

// NewEmbeddingConfigAPI builds the API with initial values (typically derived
// from env vars / the LLM config at startup).
func NewEmbeddingConfigAPI(provider, endpoint, model, apiKey string) *EmbeddingConfigAPI {
	if provider == "" {
		provider = "auto"
	}
	return &EmbeddingConfigAPI{
		state: EmbeddingConfigState{
			Provider:  provider,
			Endpoint:  endpoint,
			Model:     model,
			APIKeySet: apiKey != "",
		},
		apiKey: apiKey,
	}
}

func (a *EmbeddingConfigAPI) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/embedding/config", a.handleGet)
	mux.HandleFunc("PUT /api/v1/embedding/config", a.handleUpdate)
	mux.HandleFunc("POST /api/v1/embedding/discover-models", a.handleDiscoverModels)
}

// State returns a copy of the current configuration.
func (a *EmbeddingConfigAPI) State() EmbeddingConfigState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

// APIKey returns the configured key (used internally to build the scorer).
func (a *EmbeddingConfigAPI) APIKey() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.apiKey
}

// SetDimensions records the auto-detected vector size for UI display.
func (a *EmbeddingConfigAPI) SetDimensions(d int) {
	a.mu.Lock()
	a.state.Dimensions = d
	a.mu.Unlock()
}

func (a *EmbeddingConfigAPI) handleGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, a.State())
}

func (a *EmbeddingConfigAPI) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var update struct {
		Provider string `json:"provider"`
		Endpoint string `json:"endpoint"`
		Model    string `json:"model"`
		APIKey   string `json:"apiKey,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	a.mu.Lock()
	if strings.TrimSpace(update.Provider) != "" {
		a.state.Provider = strings.TrimSpace(update.Provider)
	}
	// Endpoint may be intentionally cleared (to reuse the LLM endpoint), so we
	// honour an explicit empty string only when the caller sends the field.
	a.state.Endpoint = strings.TrimSpace(update.Endpoint)
	if strings.TrimSpace(update.Model) != "" {
		a.state.Model = strings.TrimSpace(update.Model)
	}
	if update.APIKey != "" {
		a.apiKey = update.APIKey
		a.state.APIKeySet = true
	}
	// Dimensions reset — re-detected on next embed.
	a.state.Dimensions = 0
	updated := a.state
	key := a.apiKey
	a.mu.Unlock()

	log.Info().
		Str("provider", updated.Provider).
		Str("endpoint", updated.Endpoint).
		Str("model", updated.Model).
		Msg("Embedding config updated via UI")

	if a.onUpdate != nil {
		a.onUpdate(updated, key)
	}

	writeJSON(w, updated)
}

// handleDiscoverModels probes the embedding endpoint for available models.
// Reuses the same discovery shapes as the LLM config: Ollama /api/tags and
// OpenAI-compatible /v1/models. Embedding models are surfaced preferentially.
func (a *EmbeddingConfigAPI) handleDiscoverModels(w http.ResponseWriter, r *http.Request) {
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
		Description string `json:"description,omitempty"`
	}
	var models []modelInfo
	var probeErr string

	// Heuristic: treat anything that isn't an explicit OpenAI shape as Ollama,
	// matching NewEmbeddingScorerAuto's logic.
	isOpenAI := req.Provider == "openai" ||
		strings.HasSuffix(endpoint, "/v1") ||
		strings.Contains(endpoint, "openai.com") ||
		strings.Contains(endpoint, "azure.com")

	if isOpenAI {
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
			probeErr = "Cannot reach " + url + ": " + err.Error()
		} else {
			defer resp.Body.Close()
			var data struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if json.NewDecoder(resp.Body).Decode(&data) == nil {
				for _, m := range data.Data {
					models = append(models, modelInfo{ID: m.ID, Name: m.ID})
				}
			}
		}
	} else {
		resp, err := client.Get(endpoint + "/api/tags")
		if err != nil {
			probeErr = "Cannot reach Ollama at " + endpoint + ": " + err.Error()
		} else {
			defer resp.Body.Close()
			var data struct {
				Models []struct {
					Name    string `json:"name"`
					Model   string `json:"model"`
					Details struct {
						Family        string `json:"family"`
						ParameterSize string `json:"parameter_size"`
					} `json:"details"`
				} `json:"models"`
			}
			if json.NewDecoder(resp.Body).Decode(&data) == nil {
				for _, m := range data.Models {
					name := m.Name
					if name == "" {
						name = m.Model
					}
					desc := m.Details.Family
					// Hint which models are embedding models.
					if isEmbeddingModelName(name) {
						desc = strings.TrimSpace("embedding " + desc)
					}
					models = append(models, modelInfo{ID: name, Name: name, Description: desc})
				}
			}
		}
	}

	writeJSON(w, map[string]any{
		"models":   models,
		"error":    probeErr,
		"provider": req.Provider,
		"endpoint": req.Endpoint,
	})
}

// buildKBEmbedder constructs an EmbeddingScorer from the embedding config,
// falling back to the chat LLM endpoint/key when the embedding endpoint/key are
// not separately set. Returns nil when no usable endpoint+model is available.
func buildKBEmbedder(st EmbeddingConfigState, apiKey, llmEndpoint, llmAPIKey string) *EmbeddingScorer {
	endpoint := strings.TrimSpace(st.Endpoint)
	if endpoint == "" {
		endpoint = llmEndpoint
	}
	if endpoint == "" || strings.TrimSpace(st.Model) == "" {
		return nil
	}
	key := apiKey
	if key == "" {
		key = llmAPIKey
	}
	switch st.Provider {
	case "openai":
		return NewEmbeddingScorerForAPI(endpoint, st.Model, key, EmbeddingAPIOpenAI, nil)
	case "ollama":
		return NewEmbeddingScorerForAPI(endpoint, st.Model, key, EmbeddingAPIOllama, nil)
	default:
		return NewEmbeddingScorerAuto(endpoint, st.Model, key, nil)
	}
}

// isEmbeddingModelName is a best-effort name heuristic for surfacing embedding
// models in the discovery list.
func isEmbeddingModelName(name string) bool {
	l := strings.ToLower(name)
	for _, hint := range []string{"embed", "nomic", "minilm", "bge", "gte", "mxbai", "e5-"} {
		if strings.Contains(l, hint) {
			return true
		}
	}
	return false
}
