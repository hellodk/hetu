package main

// The Settings UI PUTs the typed API key to /api/v1/llm/config; handleUpdate
// must forward the raw key (not just a boolean) to the onUpdate callback so
// it can be applied to the live chat client and persisted to the Secret.

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestHandleUpdate_ForwardsTypedAPIKey(t *testing.T) {
	api := NewLLMConfigAPI("openai", "http://ep", "m", "", 2048, 0.2, 0)

	var gotKey string
	var gotState LLMConfigState
	api.onUpdate = func(state LLMConfigState, apiKey string) {
		gotState = state
		gotKey = apiKey
	}

	body, _ := json.Marshal(map[string]any{
		"provider": "custom",
		"endpoint": "http://192.168.1.5:8000/v1",
		"model":    "mlx-community--Qwen3.5-4B-MLX-8bit",
		"apiKey":   "dummy",
	})
	req := httptest.NewRequest("PUT", "/api/v1/llm/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.handleUpdate(rec, req)

	if rec.Code != 200 {
		t.Fatalf("handleUpdate status = %d: %s", rec.Code, rec.Body.String())
	}
	if gotKey != "dummy" {
		t.Fatalf("onUpdate must receive the raw typed key, got %q", gotKey)
	}
	if gotState.Provider != "custom" || gotState.Endpoint != "http://192.168.1.5:8000/v1" {
		t.Fatalf("onUpdate state incomplete: %+v", gotState)
	}
}

func TestHandleUpdate_EmptyKeyForwardsEmptyString(t *testing.T) {
	api := NewLLMConfigAPI("openai", "http://ep", "m", "existing", 2048, 0.2, 0)

	var gotKey string
	api.onUpdate = func(_ LLMConfigState, apiKey string) { gotKey = apiKey }

	body, _ := json.Marshal(map[string]any{"model": "other"})
	req := httptest.NewRequest("PUT", "/api/v1/llm/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.handleUpdate(rec, req)

	if rec.Code != 200 {
		t.Fatalf("handleUpdate status = %d", rec.Code)
	}
	if gotKey != "" {
		t.Fatalf("keyless save must forward empty string, got %q", gotKey)
	}
}
