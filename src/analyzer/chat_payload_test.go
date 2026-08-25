package main

// Reasoning models (Qwen3-family served by vLLM/omlx/llama.cpp) stream their
// chain-of-thought as delta.reasoning_content and can exhaust max_tokens
// before emitting any answer content. For self-hosted OpenAI-compatible
// backends we therefore disable thinking at the chat-template level. Hosted
// APIs (OpenAI/Azure) reject unknown body params, so they must stay clean.

import (
	"strings"
	"testing"

	"github.com/hellodk/hetu/pkg/types"
)

func chatBodyForProvider(t *testing.T, provider string) map[string]any {
	t.Helper()
	body := buildChatCompletionsBody(llmSnapshot{
		Provider: provider, Endpoint: "http://ep/v1", Model: "m",
		MaxTokens: 1024, Temperature: 0.2,
	}, []types.LLMMessage{{Role: "user", Content: "hi"}})
	return body
}

func assertThinkingFlag(t *testing.T, provider string, want bool) {
	t.Helper()
	body := chatBodyForProvider(t, provider)
	_, has := body["chat_template_kwargs"]
	if has != want {
		t.Fatalf("provider %q: chat_template_kwargs present=%v, want %v", provider, has, want)
	}
}

func TestBuildChatBody_DisablesThinkingOnSelfHosted(t *testing.T) {
	for _, p := range []string{"custom", "vllm", "llamacpp", "ollama", ""} {
		assertThinkingFlag(t, p, true)
	}
	body := chatBodyForProvider(t, "custom")
	kw, ok := body["chat_template_kwargs"].(map[string]any)
	if !ok || kw["enable_thinking"] != false {
		t.Fatalf("expected enable_thinking:false, got %#v", body["chat_template_kwargs"])
	}
	if !strings.Contains(body["model"].(string), "m") {
		t.Fatalf("model lost: %#v", body)
	}
}

func TestBuildChatBody_NoExtraParamsForHostedAPIs(t *testing.T) {
	for _, p := range []string{"openai", "azure"} {
		assertThinkingFlag(t, p, false)
	}
}
