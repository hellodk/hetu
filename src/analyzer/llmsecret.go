package main

// API-key persistence for the assistant. The Settings UI lets the operator
// type an LLM API key; that key must (a) take effect immediately in the chat
// client and (b) survive pod restarts. ConfigMaps are the wrong home for it
// (readable by anything with CM get in the namespace), so it lives in a
// dedicated Secret, "hetu-llm-apikey", written by the analyzer itself.

import (
	"context"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// llmAPIKeySecretName is a Kubernetes object name, not a credential.
const llmAPIKeySecretName = "hetu-llm-apikey" // #nosec G101

// llmSecretKey is the data field holding the bearer token.
const llmSecretKey = "api-key"

type LLMSecretStore struct {
	Client    kubernetes.Interface
	Namespace string
	Name      string
	Key       string
}

func newLLMSecretStore(client kubernetes.Interface, namespace string) *LLMSecretStore {
	return &LLMSecretStore{
		Client:    client,
		Namespace: namespace,
		Name:      llmAPIKeySecretName,
		Key:       llmSecretKey,
	}
}

func (s *LLMSecretStore) Get(ctx context.Context) (string, bool, error) {
	sec, err := s.Client.CoreV1().Secrets(s.Namespace).Get(ctx, s.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	raw := strings.TrimSpace(string(sec.Data[s.Key]))
	if raw == "" {
		return "", false, nil
	}
	return raw, true, nil
}

// Put creates the secret on first save and updates it afterwards.
func (s *LLMSecretStore) Put(ctx context.Context, apiKey string) error {
	secrets := s.Client.CoreV1().Secrets(s.Namespace)
	existing, err := secrets.Get(ctx, s.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		_, err = secrets.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      s.Name,
				Namespace: s.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name": "hetu",
					"hetu.io/managed":        "llm-api-key",
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{s.Key: []byte(apiKey)},
		}, metav1.CreateOptions{})
		return err
	case err != nil:
		return err
	}
	if existing.Data == nil {
		existing.Data = map[string][]byte{}
	}
	existing.Data[s.Key] = []byte(apiKey)
	_, err = secrets.Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

// loadPersistedAPIKeyFrom reads the stored key; empty string means none.
func loadPersistedAPIKeyFrom(client kubernetes.Interface, namespace string) string {
	key, found, err := newLLMSecretStore(client, namespace).Get(context.Background())
	if err != nil || !found {
		return ""
	}
	return key
}

// loadPersistedAPIKey is the in-cluster boot helper: best-effort, never fails
// startup — without RBAC or outside a cluster it just yields "".
func loadPersistedAPIKey() string {
	clientset, ns, ok := inClusterSecretClient()
	if !ok {
		return ""
	}
	return loadPersistedAPIKeyFrom(clientset, ns)
}

// persistAPIKeyToSecret writes the typed key to the dedicated Secret. It is a
// no-op outside a cluster (dev/file mode has no secret store).
func persistAPIKeyToSecret(key string) error {
	clientset, ns, ok := inClusterSecretClient()
	if !ok {
		return nil
	}
	return newLLMSecretStore(clientset, ns).Put(context.Background(), key)
}

// inClusterSecretClient returns a clientset + namespace when running in a
// pod with API access; ok=false otherwise (local/dev).
func inClusterSecretClient() (kubernetes.Interface, string, bool) {
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
		return nil, "", false
	}
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, "", false
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, "", false
	}
	ns := inClusterNamespace()
	if ns == "" {
		return nil, "", false
	}
	return clientset, ns, true
}
