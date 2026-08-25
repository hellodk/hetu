package main

import (
	"context"
	"strings"

	"github.com/rs/zerolog/log"
	sigsyaml "sigs.k8s.io/yaml"
)

func deepMerge(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		if vmap, ok := v.(map[string]any); ok {
			if existing, ok := dst[k].(map[string]any); ok {
				dst[k] = deepMerge(existing, vmap)
			} else {
				dst[k] = deepMerge(map[string]any{}, vmap)
			}
			continue
		}
		dst[k] = v
	}
	return dst
}

// persistOverridePatch merges patch into the persisted override YAML.
func (a *Analyzer) persistOverridePatch(ctx context.Context, patch map[string]any) {
	if a.configStore == nil || patch == nil {
		return
	}

	raw, found, err := a.configStore.Get(ctx)
	if err != nil {
		log.Warn().Err(err).Str("store", a.configStore.Location()).Msg("Failed to read runtime override")
		return
	}

	current := map[string]any{}
	if found && strings.TrimSpace(raw) != "" {
		if err := sigsyaml.Unmarshal([]byte(raw), &current); err != nil {
			// If existing YAML is corrupt, replace it (but keep it logged).
			log.Warn().Err(err).Msg("Existing runtime override YAML invalid; replacing")
			current = map[string]any{}
		}
	}

	merged := deepMerge(current, patch)
	out, err := sigsyaml.Marshal(merged)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to marshal runtime override YAML")
		return
	}
	if err := a.configStore.Put(ctx, strings.TrimSpace(string(out))); err != nil {
		log.Warn().Err(err).Str("store", a.configStore.Location()).Msg("Failed to persist runtime override")
		return
	}
}

// applyLLMUpdate is the onUpdate callback for the LLM settings API. It applies
// the submitted state to the live runtime config so the chat client picks it
// up without a restart, then persists: non-secret fields to the runtime
// ConfigMap, the API key (if one was typed) to its dedicated Secret.
func (a *Analyzer) applyLLMUpdate(state LLMConfigState, apiKey string) {
	a.configMu.Lock()
	if strings.TrimSpace(state.Provider) != "" {
		a.config.LLMBackend = strings.TrimSpace(state.Provider)
	}
	if strings.TrimSpace(state.Endpoint) != "" {
		a.config.LLMEndpoint = strings.TrimSpace(state.Endpoint)
	}
	if strings.TrimSpace(state.Model) != "" {
		a.config.LLMModel = strings.TrimSpace(state.Model)
	}
	if state.MaxTokens > 0 {
		a.config.MaxTokens = state.MaxTokens
	}
	if state.Temperature > 0 {
		a.config.Temperature = state.Temperature
	}
	// An empty key means "nothing newly typed" — keep whatever is live.
	if strings.TrimSpace(apiKey) != "" {
		a.config.LLMAPIKey = strings.TrimSpace(apiKey)
	}
	a.configMu.Unlock()

	if strings.TrimSpace(apiKey) != "" {
		if err := persistAPIKeyToSecret(strings.TrimSpace(apiKey)); err != nil {
			log.Warn().Err(err).Str("secret", llmAPIKeySecretName).Msg("Failed to persist LLM API key secret")
		}
	}

	// Persist non-secret LLM fields into the runtime override layer.
	a.persistOverridePatch(context.Background(), map[string]any{
		"llm": map[string]any{
			"provider":             state.Provider,
			"endpoint":             state.Endpoint,
			"model":                state.Model,
			"maxTokens":            state.MaxTokens,
			"temperature":          state.Temperature,
			"dailyTokenBudget":     state.DailyTokenBudget,
			"explainOptimizations": state.ExplainOptimizations,
		},
	})
}
