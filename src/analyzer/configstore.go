package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ConfigStore persists runtime configuration overrides (YAML) so UI-driven
// changes can survive process restarts and GitOps reconciles.
type ConfigStore interface {
	// Get returns (yaml, found, error).
	Get(ctx context.Context) (string, bool, error)
	// Put writes yaml as the current runtime override.
	Put(ctx context.Context, yaml string) error
	// Location returns a human-readable description for diagnostics.
	Location() string
}

// ---------------------------------------------------------------------------
// FileConfigStore (local/dev)
// ---------------------------------------------------------------------------

type FileConfigStore struct {
	Path string
}

func (s *FileConfigStore) Location() string { return s.Path }

func (s *FileConfigStore) Get(_ context.Context) (string, bool, error) {
	if strings.TrimSpace(s.Path) == "" {
		return "", false, errors.New("file store path is empty")
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(raw), true, nil
}

func (s *FileConfigStore) Put(_ context.Context, yaml string) error {
	if strings.TrimSpace(s.Path) == "" {
		return errors.New("file store path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.Path, []byte(yaml), 0o644)
}

// DefaultRuntimeOverridePath chooses a stable local path for runtime overrides.
func DefaultRuntimeOverridePath() string {
	if p := os.Getenv("CI_CONFIG_OVERRIDE"); strings.TrimSpace(p) != "" {
		return p
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(xdg) != "" {
		return filepath.Join(xdg, "cluster-intel", "runtime.yaml")
	}
	if home := os.Getenv("HOME"); strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".config", "cluster-intel", "runtime.yaml")
	}
	return "./.runtime-config.yaml"
}

// ---------------------------------------------------------------------------
// K8sConfigMapStore (in-cluster)
// ---------------------------------------------------------------------------

type K8sConfigMapStore struct {
	Client    kubernetes.Interface
	Namespace string
	Name      string
	Key       string
}

func (s *K8sConfigMapStore) Location() string {
	return fmt.Sprintf("ConfigMap/%s (ns=%s key=%s)", s.Name, s.Namespace, s.Key)
}

func (s *K8sConfigMapStore) Get(ctx context.Context) (string, bool, error) {
	if s.Client == nil {
		return "", false, errors.New("k8s client is nil")
	}
	cm, err := s.Client.CoreV1().ConfigMaps(s.Namespace).Get(ctx, s.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if cm.Data == nil {
		return "", false, nil
	}
	val, ok := cm.Data[s.Key]
	if !ok || strings.TrimSpace(val) == "" {
		return "", false, nil
	}
	return val, true, nil
}

func (s *K8sConfigMapStore) Put(ctx context.Context, yaml string) error {
	if s.Client == nil {
		return errors.New("k8s client is nil")
	}
	yaml = strings.TrimSpace(yaml) + "\n"
	cms := s.Client.CoreV1().ConfigMaps(s.Namespace)
	cm, err := cms.Get(ctx, s.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      s.Name,
				Namespace: s.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":      "cluster-intel",
					"app.kubernetes.io/component": "analyzer",
					"cluster-intel.io/managed":    "runtime-overrides",
				},
			},
			Data: map[string]string{s.Key: yaml},
		}
		_, err = cms.Create(ctx, cm, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[s.Key] = yaml
	_, err = cms.Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

func inClusterNamespace() string {
	if ns := os.Getenv("POD_NAMESPACE"); strings.TrimSpace(ns) != "" {
		return ns
	}
	raw, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err == nil {
		ns := strings.TrimSpace(string(raw))
		if ns != "" {
			return ns
		}
	}
	return ""
}

// NewDefaultConfigStore returns the best available config store:
// - in-cluster: a ConfigMap-backed store (GitOps-safe runtime layer)
// - local/dev: a file-backed store
func NewDefaultConfigStore() ConfigStore {
	// If K8s env isn’t present, assume local/dev.
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
		return &FileConfigStore{Path: DefaultRuntimeOverridePath()}
	}

	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return &FileConfigStore{Path: DefaultRuntimeOverridePath()}
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return &FileConfigStore{Path: DefaultRuntimeOverridePath()}
	}

	ns := inClusterNamespace()
	if ns == "" {
		// If we can’t determine namespace, fall back to file so we don’t crash.
		return &FileConfigStore{Path: DefaultRuntimeOverridePath()}
	}

	return &K8sConfigMapStore{
		Client:    clientset,
		Namespace: ns,
		Name:      "cluster-intel-runtime",
		Key:       "runtime.yaml",
	}
}

