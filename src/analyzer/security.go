package main

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// SecFinding represents a security issue found in the cluster.
type SecFinding struct {
	ID          int64    `json:"id"`
	Category    string   `json:"category"` // cis, rbac, pod-security, network, secrets, general
	Severity    string   `json:"severity"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Affected    []string `json:"affectedResources"`
	CISControl  string   `json:"cisControl,omitempty"`
	Remediation string   `json:"remediation"`
	DetectedAt  time.Time `json:"detectedAt"`
}

// SecurityScanner runs CIS Kubernetes Benchmark checks against the cluster.
type SecurityScanner struct {
	mu        sync.RWMutex
	findings  map[int64]*SecFinding
	nextID    int64
	clientset kubernetes.Interface
}

func NewSecurityScanner(cs kubernetes.Interface) *SecurityScanner {
	return &SecurityScanner{
		findings:  make(map[int64]*SecFinding),
		nextID:    1,
		clientset: cs,
	}
}

// RunScan performs all CIS benchmark security checks.
func (s *SecurityScanner) RunScan(ctx context.Context) {
	if s.clientset == nil {
		return
	}

	var findings []SecFinding

	findings = append(findings, s.checkRBAC(ctx)...)
	findings = append(findings, s.checkPodSecurity(ctx)...)
	findings = append(findings, s.checkNetworkPolicies(ctx)...)
	findings = append(findings, s.checkSecrets(ctx)...)
	findings = append(findings, s.checkGeneral(ctx)...)

	s.mu.Lock()
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

// =========================================================================
// CIS 5.1 — RBAC and Service Accounts
// =========================================================================

func (s *SecurityScanner) checkRBAC(ctx context.Context) []SecFinding {
	var findings []SecFinding

	// 5.1.1 — Ensure cluster-admin role is used only where required
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
						Description: fmt.Sprintf("%s/%s has cluster-admin via %s", subj.Namespace, subj.Name, crb.Name),
						Affected:    []string{crb.Name},
						CISControl:  "5.1.1",
						Remediation: "Create a scoped ClusterRole with only the permissions this SA needs.",
					})
				}
			}
		}
	}

	// 5.1.3 — Minimize wildcard use in Roles and ClusterRoles
	crs, err := s.clientset.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, cr := range crs.Items {
			for _, rule := range cr.Rules {
				hasWildcardVerb := false
				hasWildcardResource := false
				for _, v := range rule.Verbs {
					if v == "*" {
						hasWildcardVerb = true
					}
				}
				for _, r := range rule.Resources {
					if r == "*" {
						hasWildcardResource = true
					}
				}
				if hasWildcardVerb {
					findings = append(findings, SecFinding{
						Category:    "rbac",
						Severity:    "medium",
						Title:       "ClusterRole uses wildcard verbs",
						Description: fmt.Sprintf("ClusterRole %s grants all verbs (*) on resources", cr.Name),
						Affected:    []string{cr.Name},
						CISControl:  "5.1.3",
						Remediation: "Replace wildcard with explicit verb list.",
					})
					break
				}
				if hasWildcardResource {
					findings = append(findings, SecFinding{
						Category:    "rbac",
						Severity:    "medium",
						Title:       "ClusterRole uses wildcard resources",
						Description: fmt.Sprintf("ClusterRole %s grants access to all resources (*)", cr.Name),
						Affected:    []string{cr.Name},
						CISControl:  "5.1.3",
						Remediation: "Replace wildcard with explicit resource list.",
					})
					break
				}
			}
		}
	}

	// 5.1.5 — Ensure default service accounts are not actively used
	sas, err := s.clientset.CoreV1().ServiceAccounts("").List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, sa := range sas.Items {
			if sa.Name == "default" && len(sa.Secrets) > 0 {
				findings = append(findings, SecFinding{
					Category:    "rbac",
					Severity:    "medium",
					Title:       "Default SA has mounted secrets",
					Description: fmt.Sprintf("Namespace %s default SA has %d secret(s) — may indicate active use", sa.Namespace, len(sa.Secrets)),
					Affected:    []string{sa.Namespace + "/default"},
					CISControl:  "5.1.5",
					Remediation: "Create dedicated service accounts for workloads instead of using default.",
				})
			}
		}
	}

	// 5.1.6 — Ensure automountServiceAccountToken is false where not needed
	pods, err := s.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, pod := range pods.Items {
			if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
				if pod.Spec.ServiceAccountName == "default" {
					findings = append(findings, SecFinding{
						Category:    "rbac",
						Severity:    "low",
						Title:       "Default SA token auto-mounted",
						Description: fmt.Sprintf("%s/%s auto-mounts default SA token", pod.Namespace, pod.Name),
						Affected:    []string{pod.Namespace + "/" + pod.Name},
						CISControl:  "5.1.6",
						Remediation: "Set automountServiceAccountToken: false on pods that don't need API access.",
					})
				}
			}
		}
	}

	// 5.7.1 — Ensure system:masters group is not used for authorization
	for _, crb := range crbs.Items {
		for _, subj := range crb.Subjects {
			if subj.Kind == "Group" && subj.Name == "system:masters" && crb.RoleRef.Name != "cluster-admin" {
				findings = append(findings, SecFinding{
					Category:    "rbac",
					Severity:    "high",
					Title:       "system:masters group bound to custom role",
					Description: fmt.Sprintf("CRB %s binds system:masters to %s", crb.Name, crb.RoleRef.Name),
					Affected:    []string{crb.Name},
					CISControl:  "5.7.1",
					Remediation: "Remove bindings to system:masters group; use specific group names.",
				})
			}
		}
	}

	return findings
}

// =========================================================================
// CIS 5.2 — Pod Security Standards
// =========================================================================

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
		ref := ns + "/" + name

		// 5.2.4 — Minimize admission of pods with HostPID/HostIPC
		if pod.Spec.HostPID {
			findings = append(findings, SecFinding{
				Category: "pod-security", Severity: "high",
				Title: "Pod uses hostPID", Description: ref + " has hostPID: true",
				Affected: []string{ref}, CISControl: "5.2.4",
				Remediation: "Remove hostPID: true.",
			})
		}
		if pod.Spec.HostIPC {
			findings = append(findings, SecFinding{
				Category: "pod-security", Severity: "high",
				Title: "Pod uses hostIPC", Description: ref + " has hostIPC: true",
				Affected: []string{ref}, CISControl: "5.2.4",
				Remediation: "Remove hostIPC: true.",
			})
		}

		// 5.2.5 — Minimize admission of pods with hostNetwork
		if pod.Spec.HostNetwork {
			findings = append(findings, SecFinding{
				Category: "pod-security", Severity: "high",
				Title: "Pod uses host networking", Description: ref + " has hostNetwork: true",
				Affected: []string{ref}, CISControl: "5.2.5",
				Remediation: "Remove hostNetwork: true unless absolutely required.",
			})
		}

		// 5.2.7 — HostPath volumes
		for _, vol := range pod.Spec.Volumes {
			if vol.HostPath != nil {
				findings = append(findings, SecFinding{
					Category: "pod-security", Severity: "high",
					Title: "Pod mounts hostPath volume", Description: ref + " mounts hostPath: " + vol.HostPath.Path,
					Affected: []string{ref}, CISControl: "5.2.7",
					Remediation: "Use PVCs or emptyDir instead of hostPath.",
				})
			}
		}

		for _, c := range pod.Spec.Containers {
			sc := c.SecurityContext
			cref := fmt.Sprintf("%s container %s", ref, c.Name)

			// 5.2.1 — Privileged containers
			if sc != nil && sc.Privileged != nil && *sc.Privileged {
				findings = append(findings, SecFinding{
					Category: "pod-security", Severity: "critical",
					Title: "Privileged container", Description: cref + " runs as privileged",
					Affected: []string{ref}, CISControl: "5.2.1",
					Remediation: "Remove privileged: true from the container security context.",
				})
			}

			// 5.2.2 — AllowPrivilegeEscalation
			if sc == nil || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
				findings = append(findings, SecFinding{
					Category: "pod-security", Severity: "medium",
					Title: "AllowPrivilegeEscalation not disabled", Description: cref + " does not set allowPrivilegeEscalation: false",
					Affected: []string{ref}, CISControl: "5.2.2",
					Remediation: "Set allowPrivilegeEscalation: false.",
				})
			}

			// 5.2.3 — Run as non-root
			nonRoot := false
			if sc != nil && sc.RunAsNonRoot != nil && *sc.RunAsNonRoot {
				nonRoot = true
			}
			if sc != nil && sc.RunAsUser != nil && *sc.RunAsUser > 0 {
				nonRoot = true
			}
			if !nonRoot {
				findings = append(findings, SecFinding{
					Category: "pod-security", Severity: "medium",
					Title: "Container may run as root", Description: cref + " does not set runAsNonRoot: true",
					Affected: []string{ref}, CISControl: "5.2.3",
					Remediation: "Set runAsNonRoot: true and runAsUser to a non-zero UID.",
				})
			}

			// 5.2.9 — NET_RAW capability
			if sc != nil && sc.Capabilities != nil {
				for _, cap := range sc.Capabilities.Add {
					if string(cap) == "NET_RAW" || string(cap) == "ALL" {
						findings = append(findings, SecFinding{
							Category: "pod-security", Severity: "medium",
							Title: "Container has NET_RAW capability", Description: cref + " adds " + string(cap),
							Affected: []string{ref}, CISControl: "5.2.9",
							Remediation: "Drop NET_RAW capability unless required for network diagnostics.",
						})
						break
					}
				}
			}

			// 5.2.11 — readOnlyRootFilesystem
			if sc == nil || sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem {
				findings = append(findings, SecFinding{
					Category: "pod-security", Severity: "low",
					Title: "Root filesystem is writable", Description: cref + " does not set readOnlyRootFilesystem: true",
					Affected: []string{ref}, CISControl: "5.2.11",
					Remediation: "Set readOnlyRootFilesystem: true and use emptyDir for writable paths.",
				})
			}

			// 5.2.12 — Drop ALL capabilities
			allDropped := false
			if sc != nil && sc.Capabilities != nil {
				for _, cap := range sc.Capabilities.Drop {
					if string(cap) == "ALL" {
						allDropped = true
						break
					}
				}
			}
			if !allDropped {
				findings = append(findings, SecFinding{
					Category: "pod-security", Severity: "low",
					Title: "Capabilities not dropped", Description: cref + " does not drop ALL capabilities",
					Affected: []string{ref}, CISControl: "5.2.12",
					Remediation: "Set capabilities.drop: [ALL] and add back only what's needed.",
				})
			}

			// Missing security context entirely
			if sc == nil {
				findings = append(findings, SecFinding{
					Category: "pod-security", Severity: "low",
					Title: "Missing security context", Description: cref + " has no securityContext",
					Affected: []string{ref}, CISControl: "5.7.2",
					Remediation: "Add a securityContext with runAsNonRoot, readOnlyRootFilesystem, and drop ALL capabilities.",
				})
			}

			// Check for latest tag in image
			img := c.Image
			if strings.HasSuffix(img, ":latest") || (!strings.Contains(img, ":") && !strings.Contains(img, "@")) {
				findings = append(findings, SecFinding{
					Category: "pod-security", Severity: "medium",
					Title: "Image uses latest or no tag", Description: cref + " uses image " + img,
					Affected: []string{ref}, CISControl: "5.5.1",
					Remediation: "Pin images to a specific version tag or SHA digest.",
				})
			}

			// Check resource limits
			if c.Resources.Limits.Cpu().IsZero() && c.Resources.Limits.Memory().IsZero() {
				findings = append(findings, SecFinding{
					Category: "pod-security", Severity: "low",
					Title: "No resource limits", Description: cref + " has no CPU or memory limits",
					Affected: []string{ref}, CISControl: "5.7.2",
					Remediation: "Set resource limits to prevent resource exhaustion.",
				})
			}
		}
	}

	return findings
}

// =========================================================================
// CIS 5.3 — Network Policies
// =========================================================================

func (s *SecurityScanner) checkNetworkPolicies(ctx context.Context) []SecFinding {
	var findings []SecFinding

	namespaces, err := s.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	netpols, err := s.clientset.NetworkingV1().NetworkPolicies("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	// Build set of namespaces that have at least one network policy
	nsWithPolicy := map[string]bool{}
	nsWithDefaultDeny := map[string]bool{}
	for _, np := range netpols.Items {
		nsWithPolicy[np.Namespace] = true
		// A default deny policy has an empty podSelector and at least one policyType
		if len(np.Spec.PodSelector.MatchLabels) == 0 && len(np.Spec.PolicyTypes) > 0 {
			nsWithDefaultDeny[np.Namespace] = true
		}
	}

	systemNamespaces := map[string]bool{
		"kube-system": true, "kube-public": true, "kube-node-lease": true,
	}

	for _, ns := range namespaces.Items {
		if systemNamespaces[ns.Name] {
			continue
		}

		// 5.3.1 — Ensure NetworkPolicy is defined for each namespace
		if !nsWithPolicy[ns.Name] {
			findings = append(findings, SecFinding{
				Category: "network", Severity: "medium",
				Title: "Namespace has no NetworkPolicy", Description: fmt.Sprintf("Namespace %s has no network policies — all traffic is allowed", ns.Name),
				Affected: []string{ns.Name}, CISControl: "5.3.1",
				Remediation: "Create at least one NetworkPolicy to restrict traffic.",
			})
		}

		// 5.3.2 — Ensure default deny policy exists
		if nsWithPolicy[ns.Name] && !nsWithDefaultDeny[ns.Name] {
			findings = append(findings, SecFinding{
				Category: "network", Severity: "low",
				Title: "No default deny NetworkPolicy", Description: fmt.Sprintf("Namespace %s has policies but no default deny (empty podSelector)", ns.Name),
				Affected: []string{ns.Name}, CISControl: "5.3.2",
				Remediation: "Add a default-deny NetworkPolicy with empty podSelector.",
			})
		}
	}

	return findings
}

// =========================================================================
// CIS 5.4 — Secrets Management
// =========================================================================

func (s *SecurityScanner) checkSecrets(ctx context.Context) []SecFinding {
	var findings []SecFinding

	pods, err := s.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	for _, pod := range pods.Items {
		ref := pod.Namespace + "/" + pod.Name

		for _, c := range pod.Spec.Containers {
			for _, env := range c.Env {
				// 5.4.1 — Prefer using secrets as files over environment variables
				if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
					findings = append(findings, SecFinding{
						Category: "secrets", Severity: "low",
						Title: "Secret exposed as env var", Description: fmt.Sprintf("%s container %s reads secret %s via env var %s", ref, c.Name, env.ValueFrom.SecretKeyRef.Name, env.Name),
						Affected: []string{ref}, CISControl: "5.4.1",
						Remediation: "Mount secrets as files instead of env vars to reduce exposure in logs/crash dumps.",
					})
				}

				// Check for common sensitive env var names with inline values
				lowerName := strings.ToLower(env.Name)
				if env.Value != "" && (strings.Contains(lowerName, "password") || strings.Contains(lowerName, "secret") || strings.Contains(lowerName, "api_key") || strings.Contains(lowerName, "apikey") || strings.Contains(lowerName, "token")) {
					if env.ValueFrom == nil {
						findings = append(findings, SecFinding{
							Category: "secrets", Severity: "high",
							Title: "Possible hardcoded credential", Description: fmt.Sprintf("%s container %s has env var %s with inline value", ref, c.Name, env.Name),
							Affected: []string{ref}, CISControl: "5.4.2",
							Remediation: "Use a Kubernetes Secret or external secret manager instead of inline values.",
						})
					}
				}
			}
		}
	}

	return findings
}

// =========================================================================
// CIS 5.7 — General Policies
// =========================================================================

func (s *SecurityScanner) checkGeneral(ctx context.Context) []SecFinding {
	var findings []SecFinding

	// 5.7.4 — Avoid deploying to the default namespace
	pods, err := s.clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{})
	if err == nil && len(pods.Items) > 0 {
		names := make([]string, 0, len(pods.Items))
		for _, p := range pods.Items {
			names = append(names, p.Name)
		}
		findings = append(findings, SecFinding{
			Category: "general", Severity: "medium",
			Title: "Pods running in default namespace", Description: fmt.Sprintf("%d pod(s) found in default namespace", len(pods.Items)),
			Affected: names, CISControl: "5.7.4",
			Remediation: "Deploy workloads to dedicated namespaces, not 'default'.",
		})
	}

	// 5.7.3 — Namespace resource quotas
	namespaces, err := s.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return findings
	}
	systemNS := map[string]bool{"kube-system": true, "kube-public": true, "kube-node-lease": true}
	for _, ns := range namespaces.Items {
		if systemNS[ns.Name] {
			continue
		}
		quotas, err := s.clientset.CoreV1().ResourceQuotas(ns.Name).List(ctx, metav1.ListOptions{})
		if err == nil && len(quotas.Items) == 0 {
			findings = append(findings, SecFinding{
				Category: "general", Severity: "low",
				Title: "No ResourceQuota in namespace", Description: fmt.Sprintf("Namespace %s has no ResourceQuota", ns.Name),
				Affected: []string{ns.Name}, CISControl: "5.7.3",
				Remediation: "Create a ResourceQuota to limit resource consumption per namespace.",
			})
		}
		limits, err := s.clientset.CoreV1().LimitRanges(ns.Name).List(ctx, metav1.ListOptions{})
		if err == nil && len(limits.Items) == 0 {
			findings = append(findings, SecFinding{
				Category: "general", Severity: "low",
				Title: "No LimitRange in namespace", Description: fmt.Sprintf("Namespace %s has no LimitRange", ns.Name),
				Affected: []string{ns.Name}, CISControl: "5.7.3",
				Remediation: "Create a LimitRange to set default resource requests/limits.",
			})
		}
	}

	return findings
}

// =========================================================================
// HTTP API
// =========================================================================

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
	byCIS := map[string]int{}
	for _, f := range s.findings {
		bySev[f.Severity]++
		byCat[f.Category]++
		if f.CISControl != "" {
			byCIS[f.CISControl]++
		}
	}

	writeJSON(w, map[string]any{
		"totalFindings": len(s.findings),
		"bySeverity":    bySev,
		"byCategory":    byCat,
		"byCISControl":  byCIS,
	})
}

func (s *SecurityScanner) handleTriggerScan(w http.ResponseWriter, r *http.Request) {
	go s.RunScan(r.Context())
	writeJSON(w, map[string]string{"status": "scan triggered"})
}
