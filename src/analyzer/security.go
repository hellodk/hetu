package main

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// SecurityFinding represents a security issue found in the cluster.
type SecFinding struct {
	ID          int64    `json:"id"`
	Category    string   `json:"category"` // cis, rbac, pod-security, image
	Severity    string   `json:"severity"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Affected    []string `json:"affectedResources"`
	CISControl  string   `json:"cisControl,omitempty"`
	Remediation string   `json:"remediation"`
	DetectedAt  time.Time `json:"detectedAt"`
}

// SecurityScanner runs security checks against the cluster.
type SecurityScanner struct {
	mu        sync.RWMutex
	findings  map[int64]*SecFinding
	nextID    int64
	clientset kubernetes.Interface
}

// NewSecurityScanner creates a scanner.
func NewSecurityScanner(cs kubernetes.Interface) *SecurityScanner {
	return &SecurityScanner{
		findings:  make(map[int64]*SecFinding),
		nextID:    1,
		clientset: cs,
	}
}

// RunScan performs all security checks.
func (s *SecurityScanner) RunScan(ctx context.Context) {
	if s.clientset == nil {
		return
	}

	var findings []SecFinding

	// RBAC checks
	findings = append(findings, s.checkRBAC(ctx)...)

	// Pod Security checks
	findings = append(findings, s.checkPodSecurity(ctx)...)

	s.mu.Lock()
	// Clear old findings and replace
	s.findings = make(map[int64]*SecFinding)
	for i := range findings {
		findings[i].ID = s.nextID
		findings[i].DetectedAt = time.Now()
		s.findings[s.nextID] = &findings[i]
		s.nextID++
	}
	s.mu.Unlock()

	log.Info().Int("findings", len(findings)).Msg("Security scan completed")
}

func (s *SecurityScanner) checkRBAC(ctx context.Context) []SecFinding {
	var findings []SecFinding

	// Check for cluster-admin bindings
	crbs, err := s.clientset.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Debug().Err(err).Msg("Failed to list CRBs")
		return nil
	}

	for _, crb := range crbs.Items {
		if crb.RoleRef.Name == "cluster-admin" {
			for _, subj := range crb.Subjects {
				if subj.Kind == "ServiceAccount" {
					findings = append(findings, SecFinding{
						Category:    "rbac",
						Severity:    "high",
						Title:       "ServiceAccount bound to cluster-admin",
						Description: subj.Namespace + "/" + subj.Name + " has cluster-admin via " + crb.Name,
						Affected:    []string{crb.Name},
						CISControl:  "5.1.1",
						Remediation: "Create a scoped ClusterRole with only the permissions this SA needs.",
					})
				}
			}
		}
	}

	// Check for wildcard verbs in ClusterRoles
	crs, err := s.clientset.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, cr := range crs.Items {
			for _, rule := range cr.Rules {
				for _, verb := range rule.Verbs {
					if verb == "*" {
						findings = append(findings, SecFinding{
							Category:    "rbac",
							Severity:    "medium",
							Title:       "ClusterRole uses wildcard verbs",
							Description: "ClusterRole " + cr.Name + " grants all verbs (*) on resources",
							Affected:    []string{cr.Name},
							CISControl:  "5.1.3",
							Remediation: "Replace wildcard with explicit verb list.",
						})
						break
					}
				}
			}
		}
	}

	return findings
}

func (s *SecurityScanner) checkPodSecurity(ctx context.Context) []SecFinding {
	var findings []SecFinding

	pods, err := s.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Debug().Err(err).Msg("Failed to list pods for security scan")
		return nil
	}

	for _, pod := range pods.Items {
		ns := pod.Namespace
		name := pod.Name

		// Check each container
		for _, c := range pod.Spec.Containers {
			sc := c.SecurityContext

			// Privileged
			if sc != nil && sc.Privileged != nil && *sc.Privileged {
				findings = append(findings, SecFinding{
					Category:    "pod-security",
					Severity:    "critical",
					Title:       "Privileged container",
					Description: ns + "/" + name + " container " + c.Name + " runs as privileged",
					Affected:    []string{ns + "/" + name},
					CISControl:  "5.2.1",
					Remediation: "Remove privileged: true from the container security context.",
				})
			}

			// Run as root
			if sc == nil || sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
				if sc == nil || sc.RunAsUser == nil || *sc.RunAsUser == 0 {
					findings = append(findings, SecFinding{
						Category:    "pod-security",
						Severity:    "medium",
						Title:       "Container may run as root",
						Description: ns + "/" + name + " container " + c.Name + " does not set runAsNonRoot: true",
						Affected:    []string{ns + "/" + name},
						CISControl:  "5.2.3",
						Remediation: "Set runAsNonRoot: true and runAsUser to a non-zero UID.",
					})
				}
			}

			// Missing security context entirely
			if sc == nil {
				findings = append(findings, SecFinding{
					Category:    "pod-security",
					Severity:    "low",
					Title:       "Missing security context",
					Description: ns + "/" + name + " container " + c.Name + " has no securityContext set",
					Affected:    []string{ns + "/" + name},
					CISControl:  "5.7.2",
					Remediation: "Add a securityContext with runAsNonRoot, readOnlyRootFilesystem, and drop ALL capabilities.",
				})
			}
		}

		// Host networking
		if pod.Spec.HostNetwork {
			findings = append(findings, SecFinding{
				Category:    "pod-security",
				Severity:    "high",
				Title:       "Pod uses host networking",
				Description: ns + "/" + name + " has hostNetwork: true",
				Affected:    []string{ns + "/" + name},
				CISControl:  "5.2.4",
				Remediation: "Remove hostNetwork: true unless absolutely required.",
			})
		}

		// HostPath volumes
		for _, vol := range pod.Spec.Volumes {
			if vol.HostPath != nil {
				findings = append(findings, SecFinding{
					Category:    "pod-security",
					Severity:    "high",
					Title:       "Pod mounts hostPath volume",
					Description: ns + "/" + name + " mounts hostPath: " + vol.HostPath.Path,
					Affected:    []string{ns + "/" + name},
					CISControl:  "5.2.7",
					Remediation: "Use PVCs or emptyDir instead of hostPath.",
				})
			}
		}
	}

	return findings
}

// RegisterRoutes adds security API endpoints.
func (s *SecurityScanner) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/security/findings", s.handleFindings)
	mux.HandleFunc("GET /api/v1/security/summary", s.handleSummary)
	mux.HandleFunc("POST /api/v1/security/scan", s.handleTriggerScan)
}

func (s *SecurityScanner) handleFindings(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	category := r.URL.Query().Get("category")
	severity := r.URL.Query().Get("severity")

	var result []*SecFinding
	for _, f := range s.findings {
		if category != "" && f.Category != category {
			continue
		}
		if severity != "" && f.Severity != severity {
			continue
		}
		result = append(result, f)
	}

	sort.Slice(result, func(i, j int) bool {
		sevOrder := map[string]int{"critical": 4, "high": 3, "medium": 2, "low": 1}
		return sevOrder[result[i].Severity] > sevOrder[result[j].Severity]
	})

	writeJSON(w, map[string]any{
		"totalCount": len(result),
		"findings":   result,
	})
}

func (s *SecurityScanner) handleSummary(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bySev := map[string]int{}
	byCat := map[string]int{}
	for _, f := range s.findings {
		bySev[f.Severity]++
		byCat[f.Category]++
	}

	writeJSON(w, map[string]any{
		"totalFindings": len(s.findings),
		"bySeverity":    bySev,
		"byCategory":    byCat,
	})
}

func (s *SecurityScanner) handleTriggerScan(w http.ResponseWriter, r *http.Request) {
	go s.RunScan(r.Context())
	writeJSON(w, map[string]string{"status": "scan triggered"})
}
