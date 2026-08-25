package main

// Settings must survive pod restarts — including the API key. The key is
// deliberately kept out of the runtime ConfigMap (world-readable) and stored
// in a dedicated Secret instead. These tests pin that contract.

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestLLMSecretStore_PutGetRoundtrip(t *testing.T) {
	client := fake.NewSimpleClientset()
	s := newLLMSecretStore(client, "hetu")
	ctx := context.Background()

	if _, found, err := s.Get(ctx); err != nil || found {
		t.Fatalf("expected not-found before Put, found=%v err=%v", found, err)
	}

	if err := s.Put(ctx, "dummy"); err != nil {
		t.Fatalf("Put (create path): %v", err)
	}
	got, found, err := s.Get(ctx)
	if err != nil || !found || got != "dummy" {
		t.Fatalf("after Put: got=%q found=%v err=%v", got, found, err)
	}

	// Update path — secret already exists.
	if err := s.Put(ctx, "dummy-v2"); err != nil {
		t.Fatalf("Put (update path): %v", err)
	}
	if got, _, _ := s.Get(ctx); got != "dummy-v2" {
		t.Fatalf("expected updated key, got %q", got)
	}

	sec, err := client.CoreV1().Secrets("hetu").Get(ctx, "hetu-llm-apikey", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("secret must live at hetu/hetu-llm-apikey: %v", err)
	}
	if sec.Type != corev1.SecretTypeOpaque {
		t.Fatalf("expected Opaque secret, got %q", sec.Type)
	}
}

// The chart renders the Secret as an EMPTY SHELL (no data key) because
// resourceNames-scoped RBAC cannot authorize create requests — the analyzer
// must therefore adopt the existing object via its update path.
func TestLLMSecretStore_PutAdoptsChartRenderedShell(t *testing.T) {
	client := fake.NewSimpleClientset()
	if _, err := client.CoreV1().Secrets("hetu").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "hetu-llm-apikey", Namespace: "hetu"},
		Type:       corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed shell: %v", err)
	}

	s := newLLMSecretStore(client, "hetu")
	if err := s.Put(context.Background(), "dummy"); err != nil {
		t.Fatalf("Put onto shell: %v", err)
	}
	if got, found, _ := s.Get(context.Background()); !found || got != "dummy" {
		t.Fatalf("expected adopted key, got=%q found=%v", got, found)
	}
}

func TestLoadPersistedAPIKey_EmptyWhenAbsent(t *testing.T) {
	got := loadPersistedAPIKeyFrom(fake.NewSimpleClientset(), "hetu")
	if got != "" {
		t.Fatalf("expected empty key when no secret exists, got %q", got)
	}
}

func TestLoadPersistedAPIKey_ReadsStoredKey(t *testing.T) {
	client := fake.NewSimpleClientset()
	s := newLLMSecretStore(client, "hetu")
	if err := s.Put(context.Background(), "dummy"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if got := loadPersistedAPIKeyFrom(client, "hetu"); got != "dummy" {
		t.Fatalf("expected persisted key, got %q", got)
	}
}

// applyLLMUpdate is the onUpdate closure body: it must feed the typed API key
// into the runtime config the chat client reads (previously it was discarded
// with `_ = apiKeyProvided`, so the assistant always called the LLM without
// an Authorization header), while never writing the key into the ConfigMap.
func TestApplyLLMUpdate_SetsRuntimeKeyAndPersistsNonSecrets(t *testing.T) {
	a := &Analyzer{configStore: &FileConfigStore{Path: t.TempDir() + "/runtime.yaml"}}

	a.applyLLMUpdate(LLMConfigState{
		Provider: "custom", Endpoint: "http://192.168.1.5:8000/v1",
		Model: "mlx-community--Qwen3.5-4B-MLX-8bit", MaxTokens: 4096, Temperature: 0.2,
	}, "dummy")

	if a.config.LLMAPIKey != "dummy" {
		t.Fatalf("chat client must see the typed key immediately, got %q", a.config.LLMAPIKey)
	}

	raw, found, err := a.configStore.Get(context.Background())
	if err != nil || !found {
		t.Fatalf("non-secret fields must persist: found=%v err=%v", found, err)
	}
	for _, want := range []string{"custom", "http://192.168.1.5:8000/v1", "Qwen3.5"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("override missing %q:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "dummy") {
		t.Fatal("API key must never be written into the runtime ConfigMap")
	}
}

// A later save WITHOUT re-typing the key must keep the previously applied one.
func TestApplyLLMUpdate_KeepsKeyWhenUntyped(t *testing.T) {
	a := &Analyzer{configStore: &FileConfigStore{Path: t.TempDir() + "/runtime.yaml"}}
	a.applyLLMUpdate(LLMConfigState{Provider: "custom", Endpoint: "e", Model: "m"}, "k1")
	a.applyLLMUpdate(LLMConfigState{Provider: "openai", Endpoint: "e2", Model: "m2"}, "")
	if a.config.LLMAPIKey != "k1" {
		t.Fatalf("key lost on keyless save: %q", a.config.LLMAPIKey)
	}
}
