package main

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// IngressRule represents a single rule with host + paths.
type IngressRule struct {
	Host  string        `json:"host"`
	Paths []IngressPath `json:"paths"`
}

// IngressPath represents a path within a rule.
type IngressPath struct {
	Path        string `json:"path"`
	PathType    string `json:"pathType"`
	ServiceName string `json:"serviceName"`
	ServicePort int32  `json:"servicePort"`
}

// IngressInfo is the API representation of a K8s Ingress resource.
type IngressInfo struct {
	Namespace    string            `json:"namespace"`
	Name         string            `json:"name"`
	Class        string            `json:"ingressClass"`
	Hosts        []string          `json:"hosts"`
	Rules        []IngressRule     `json:"rules"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	TLS          bool              `json:"tls"`
	LoadBalancer string            `json:"loadBalancer,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
}

// IngressScanner watches K8s Ingress resources and maps them to LBs.
type IngressScanner struct {
	mu        sync.RWMutex
	ingresses []IngressInfo
	clientset kubernetes.Interface
}

// NewIngressScanner creates a scanner.
func NewIngressScanner(cs kubernetes.Interface) *IngressScanner {
	return &IngressScanner{clientset: cs}
}

// Scan lists all Ingress resources and extracts LB mappings.
func (s *IngressScanner) Scan(ctx context.Context) {
	if s.clientset == nil {
		return
	}

	ingList, err := s.clientset.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Error().Err(err).Msg("Failed to list Ingresses")
		return
	}

	var result []IngressInfo
	for _, ing := range ingList.Items {
		info := convertIngress(&ing)
		result = append(result, info)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Namespace != result[j].Namespace {
			return result[i].Namespace < result[j].Namespace
		}
		return result[i].Name < result[j].Name
	})

	s.mu.Lock()
	s.ingresses = result
	s.mu.Unlock()

	log.Info().Int("ingresses", len(result)).Msg("Ingress scan completed")
}

func convertIngress(ing *networkingv1.Ingress) IngressInfo {
	info := IngressInfo{
		Namespace:   ing.Namespace,
		Name:        ing.Name,
		Annotations: ing.Annotations,
		TLS:         len(ing.Spec.TLS) > 0,
		CreatedAt:   ing.CreationTimestamp.Time,
	}

	// Ingress class
	if ing.Spec.IngressClassName != nil {
		info.Class = *ing.Spec.IngressClassName
	} else if v, ok := ing.Annotations["kubernetes.io/ingress.class"]; ok {
		info.Class = v
	}

	// Load balancer hostname from status
	for _, lb := range ing.Status.LoadBalancer.Ingress {
		if lb.Hostname != "" {
			info.LoadBalancer = lb.Hostname
			break
		}
		if lb.IP != "" {
			info.LoadBalancer = lb.IP
			break
		}
	}

	// Rules → hosts + paths
	hostSet := map[string]bool{}
	for _, rule := range ing.Spec.Rules {
		host := rule.Host
		if host != "" {
			hostSet[host] = true
		}
		ir := IngressRule{Host: host}
		if rule.HTTP != nil {
			for _, p := range rule.HTTP.Paths {
				ip := IngressPath{
					Path: p.Path,
				}
				if p.PathType != nil {
					ip.PathType = string(*p.PathType)
				}
				if p.Backend.Service != nil {
					ip.ServiceName = p.Backend.Service.Name
					ip.ServicePort = p.Backend.Service.Port.Number
				}
				ir.Paths = append(ir.Paths, ip)
			}
		}
		info.Rules = append(info.Rules, ir)
	}

	for h := range hostSet {
		info.Hosts = append(info.Hosts, h)
	}
	sort.Strings(info.Hosts)

	return info
}

// RegisterRoutes adds ingress API endpoints.
func (s *IngressScanner) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/ingress", s.handleList)
	mux.HandleFunc("GET /api/v1/ingress/{ns}/{name}", s.handleDetail)
}

func (s *IngressScanner) handleList(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nsFilter := r.URL.Query().Get("namespace")
	classFilter := r.URL.Query().Get("class")

	var result []IngressInfo
	for _, ing := range s.ingresses {
		if nsFilter != "" && ing.Namespace != nsFilter {
			continue
		}
		if classFilter != "" && ing.Class != classFilter {
			continue
		}
		result = append(result, ing)
	}

	writeJSON(w, map[string]any{
		"totalCount": len(result),
		"ingresses":  result,
	})
}

func (s *IngressScanner) handleDetail(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, ing := range s.ingresses {
		if ing.Namespace == ns && ing.Name == name {
			writeJSON(w, ing)
			return
		}
	}

	http.NotFound(w, r)
}
