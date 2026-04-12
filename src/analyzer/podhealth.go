package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PodHealthCategory describes the type of pod health issue.
type PodHealthCategory struct {
	Name        string          `json:"name"`
	Count       int             `json:"count"`
	Pods        []PodHealthItem `json:"pods"`
}

// PodHealthItem is a single unhealthy pod with diagnosis.
type PodHealthItem struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Phase     string    `json:"phase"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	Restarts  int32     `json:"restarts"`
	Age       string    `json:"age"`
	Node      string    `json:"node"`
}

// PodHealthReport is the result of a pod health scan.
type PodHealthReport struct {
	Timestamp   time.Time           `json:"timestamp"`
	TotalPods   int                 `json:"totalPods"`
	HealthyPods int                 `json:"healthyPods"`
	Categories  []PodHealthCategory `json:"categories"`
}

// PodHealthScanner detects non-running and problematic pods.
type PodHealthScanner struct {
	mu        sync.RWMutex
	report    *PodHealthReport
	clientset kubernetes.Interface
}

// NewPodHealthScanner creates a scanner.
func NewPodHealthScanner(cs kubernetes.Interface) *PodHealthScanner {
	return &PodHealthScanner{clientset: cs}
}

// Scan performs a pod health check across all namespaces.
func (s *PodHealthScanner) Scan(ctx context.Context) {
	if s.clientset == nil {
		return
	}

	pods, err := s.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Error().Err(err).Msg("Failed to list pods for health scan")
		return
	}

	categories := map[string][]PodHealthItem{
		"evicted":        {},
		"failed":         {},
		"pending":        {},
		"crashloop":      {},
		"imagepull":      {},
		"oomkilled":      {},
		"terminating":    {},
		"completed":      {},
	}

	totalPods := len(pods.Items)
	healthyPods := 0

	for _, pod := range pods.Items {
		item := PodHealthItem{
			Namespace: pod.Namespace,
			Name:      pod.Name,
			Phase:     string(pod.Status.Phase),
			Node:      pod.Spec.NodeName,
		}

		if !pod.CreationTimestamp.IsZero() {
			item.Age = time.Since(pod.CreationTimestamp.Time).Truncate(time.Second).String()
		}

		// Count restarts
		for _, cs := range pod.Status.ContainerStatuses {
			item.Restarts += cs.RestartCount
		}

		// Categorize
		switch {
		case pod.Status.Phase == corev1.PodSucceeded:
			categories["completed"] = append(categories["completed"], item)

		case pod.Status.Phase == corev1.PodFailed && pod.Status.Reason == "Evicted":
			item.Reason = "Evicted"
			item.Message = pod.Status.Message
			categories["evicted"] = append(categories["evicted"], item)

		case pod.Status.Phase == corev1.PodFailed:
			item.Reason = pod.Status.Reason
			item.Message = pod.Status.Message
			categories["failed"] = append(categories["failed"], item)

		case pod.Status.Phase == corev1.PodPending:
			item.Reason = "Pending"
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
					item.Message = cond.Message
				}
			}
			categories["pending"] = append(categories["pending"], item)

		case pod.DeletionTimestamp != nil && time.Since(pod.DeletionTimestamp.Time) > 5*time.Minute:
			item.Reason = "Stuck terminating"
			categories["terminating"] = append(categories["terminating"], item)

		default:
			// Check container statuses for issues
			isHealthy := true
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil {
					switch cs.State.Waiting.Reason {
					case "CrashLoopBackOff":
						item.Reason = "CrashLoopBackOff"
						item.Message = cs.State.Waiting.Message
						categories["crashloop"] = append(categories["crashloop"], item)
						isHealthy = false
					case "ImagePullBackOff", "ErrImagePull":
						item.Reason = cs.State.Waiting.Reason
						item.Message = cs.State.Waiting.Message
						categories["imagepull"] = append(categories["imagepull"], item)
						isHealthy = false
					}
				}
				if cs.LastTerminationState.Terminated != nil && cs.LastTerminationState.Terminated.Reason == "OOMKilled" {
					item.Reason = "OOMKilled"
					categories["oomkilled"] = append(categories["oomkilled"], item)
					isHealthy = false
				}
			}
			if isHealthy {
				healthyPods++
			}
		}
	}

	var catList []PodHealthCategory
	order := []string{"crashloop", "oomkilled", "imagepull", "pending", "failed", "evicted", "terminating", "completed"}
	for _, name := range order {
		pods := categories[name]
		if len(pods) > 0 {
			catList = append(catList, PodHealthCategory{
				Name:  name,
				Count: len(pods),
				Pods:  pods,
			})
		}
	}

	report := &PodHealthReport{
		Timestamp:   time.Now(),
		TotalPods:   totalPods,
		HealthyPods: healthyPods,
		Categories:  catList,
	}

	s.mu.Lock()
	s.report = report
	s.mu.Unlock()

	unhealthy := totalPods - healthyPods
	log.Info().Int("total", totalPods).Int("healthy", healthyPods).Int("unhealthy", unhealthy).Msg("Pod health scan completed")
}

// RegisterRoutes adds pod health API endpoints.
func (s *PodHealthScanner) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/pods/health", s.handleHealth)
	mux.HandleFunc("POST /api/v1/pods/health/scan", s.handleTriggerScan)
}

func (s *PodHealthScanner) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.report == nil {
		writeJSON(w, map[string]string{"status": "no scan yet"})
		return
	}
	writeJSON(w, s.report)
}

func (s *PodHealthScanner) handleTriggerScan(w http.ResponseWriter, r *http.Request) {
	go s.Scan(r.Context())
	writeJSON(w, map[string]string{"status": "scan triggered"})
}
