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
