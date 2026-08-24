package main

// Issue #15 — the in-cluster ConfigStore must persist runtime overrides to
// the chart-managed ConfigMap "hetu-runtime". It previously targeted
// "cluster-intel-runtime", which RBAC forbids and which nothing reads, so
// UI settings silently failed to persist across restarts.

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestInClusterConfigStore_TargetsChartConfigMap(t *testing.T) {
	s := newInClusterConfigStore(fake.NewSimpleClientset(), "hetu")
	k8s, ok := s.(*K8sConfigMapStore)
	if !ok {
		t.Fatalf("expected *K8sConfigStore, got %T", s)
	}
	if k8s.Name != "hetu-runtime" {
		t.Fatalf("runtime overrides must target hetu-runtime (chart-managed), got %q", k8s.Name)
	}
	if k8s.Key != "runtime.yaml" {
		t.Fatalf("unexpected key %q", k8s.Key)
	}
}

func TestK8sConfigMapStore_PutGetRoundtrip(t *testing.T) {
	client := fake.NewSimpleClientset()
	s := newInClusterConfigStore(client, "hetu")
	ctx := context.Background()

	// Chart renders hetu-runtime empty by default; emulate that.
	if _, err := client.CoreV1().ConfigMaps("hetu").Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "hetu-runtime"},
		Data:       map[string]string{"runtime.yaml": ""},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed configmap: %v", err)
	}

	if _, found, err := s.Get(ctx); err != nil || found {
		t.Fatalf("expected not-found on empty override, found=%v err=%v", found, err)
	}

	if err := s.Put(ctx, "llm:\n  endpoint: http://ollama:11434\n"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	raw, found, err := s.Get(ctx)
	if err != nil || !found {
		t.Fatalf("expected override after Put, found=%v err=%v", found, err)
	}
	if raw != "llm:\n  endpoint: http://ollama:11434\n" {
		t.Fatalf("roundtrip mismatch: %q", raw)
	}

	cm, _ := client.CoreV1().ConfigMaps("hetu").Get(ctx, "hetu-runtime", metav1.GetOptions{})
	if lbl := cm.Labels["hetu.io/managed"]; lbl != "runtime-overrides" {
		t.Fatalf("expected hetu.io/managed label, labels=%v", cm.Labels)
	}
}
