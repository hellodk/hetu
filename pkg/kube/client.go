package kube

import (
	"fmt"

	"github.com/your-org/cluster-intel/pkg/config"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Clients bundles every Kubernetes client type a binary might need.
type Clients struct {
	Config    *rest.Config
	Clientset *kubernetes.Clientset
	Dynamic   dynamic.Interface
	Discovery discovery.DiscoveryInterface
}

// NewClients builds all K8s clients from the supplied config. It tries
// in-cluster first (when cfg.InCluster is true), falling back to the
// kubeconfig file at cfg.KubeconfigPath.
func NewClients(cfg config.KubeConfig) (*Clients, error) {
	restCfg, err := buildRESTConfig(cfg)
	if err != nil {
		return nil, err
	}
	restCfg.QPS = cfg.QPS
	restCfg.Burst = cfg.Burst

	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("kube: clientset: %w", err)
	}
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("kube: dynamic: %w", err)
	}
	disc, err := discovery.NewDiscoveryClientForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("kube: discovery: %w", err)
	}
	return &Clients{
		Config:    restCfg,
		Clientset: cs,
		Dynamic:   dyn,
		Discovery: disc,
	}, nil
}

func buildRESTConfig(cfg config.KubeConfig) (*rest.Config, error) {
	if cfg.InCluster {
		rc, err := rest.InClusterConfig()
		if err == nil {
			return rc, nil
		}
		// Fall through to kubeconfig if in-cluster fails (e.g. local dev).
	}
	if cfg.KubeconfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", cfg.KubeconfigPath)
	}
	// Try default kubeconfig rules (KUBECONFIG env, ~/.kube/config).
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, configOverrides).ClientConfig()
}
