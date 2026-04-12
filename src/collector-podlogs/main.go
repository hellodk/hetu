// Package main implements the pod log collector for cluster-intel v7.
// It tails pod logs across configured namespaces, parses them for errors,
// timeouts, and exceptions, and publishes structured events to NATS.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/your-org/cluster-intel/pkg/bus"
	"github.com/your-org/cluster-intel/pkg/config"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// PodLogCollector tails logs from pods and publishes parsed events.
type PodLogCollector struct {
	clientset  kubernetes.Interface
	eventBus   *bus.Bus
	namespaces []string // empty = all namespaces
	stopCh     chan struct{}
	wg         sync.WaitGroup
	tracked    map[string]context.CancelFunc // pod key -> cancel
	trackedMu  sync.Mutex
}

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	cfg, err := config.LoadFromEnv("/etc/cluster-intel/config.yaml")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	// K8s client
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		kp := os.Getenv("KUBECONFIG")
		if kp == "" {
			kp = os.Getenv("HOME") + "/.kube/config"
		}
		restCfg, err = clientcmd.BuildConfigFromFlags("", kp)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to build k8s config")
		}
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create k8s clientset")
	}

	// NATS bus
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var eventBus *bus.Bus
	if cfg.Bus.NATS.Enabled {
		eventBus, err = bus.Connect(ctx, cfg.Bus.NATS)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to connect to NATS")
		}
		defer eventBus.Close()
	}

	// Namespace filter from env (comma-separated) or all
	nsStr := os.Getenv("WATCH_NAMESPACES")
	var namespaces []string
	if nsStr != "" {
		for _, ns := range strings.Split(nsStr, ",") {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				namespaces = append(namespaces, ns)
			}
		}
	}

	collector := &PodLogCollector{
		clientset:  cs,
		eventBus:   eventBus,
		namespaces: namespaces,
		stopCh:     make(chan struct{}),
		tracked:    make(map[string]context.CancelFunc),
	}

	collector.wg.Add(1)
	go collector.watchLoop(ctx)

	log.Info().Strs("namespaces", namespaces).Msg("Pod log collector started")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Info().Msg("Shutting down")
	cancel()
	close(collector.stopCh)
	collector.wg.Wait()
}

// watchLoop periodically discovers running pods and starts/stops tailers.
func (c *PodLogCollector) watchLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	c.reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.reconcile(ctx)
		}
	}
}

// reconcile discovers running pods and starts/stops tailers as needed.
func (c *PodLogCollector) reconcile(ctx context.Context) {
	namespaces := c.namespaces
	if len(namespaces) == 0 {
		namespaces = []string{""} // empty = all namespaces
	}

	activePods := map[string]bool{}

	for _, ns := range namespaces {
		pods, err := c.clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
			FieldSelector: "status.phase=Running",
		})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list pods")
			continue
		}

		for _, pod := range pods.Items {
			key := pod.Namespace + "/" + pod.Name
			activePods[key] = true

			c.trackedMu.Lock()
			_, exists := c.tracked[key]
			c.trackedMu.Unlock()

			if !exists {
				// Infer service name from labels or owner
				service := inferService(pod)
				for _, container := range pod.Spec.Containers {
					c.startTailer(ctx, pod.Namespace, pod.Name, container.Name, service)
				}
			}
		}
	}

	// Stop tailers for pods that no longer exist
	c.trackedMu.Lock()
	for key, cancelFn := range c.tracked {
		if !activePods[key] {
			cancelFn()
			delete(c.tracked, key)
		}
	}
	c.trackedMu.Unlock()
}

func (c *PodLogCollector) startTailer(ctx context.Context, namespace, pod, container, service string) {
	tailCtx, cancelFn := context.WithCancel(ctx)
	key := namespace + "/" + pod

	c.trackedMu.Lock()
	c.tracked[key] = cancelFn
	c.trackedMu.Unlock()

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.tailPod(tailCtx, namespace, pod, container, service)
	}()

	log.Debug().Str("pod", key).Str("container", container).Msg("Started log tailer")
}

func (c *PodLogCollector) tailPod(ctx context.Context, namespace, pod, container, service string) {
	tailLines := int64(100)
	opts := &corev1.PodLogOptions{
		Container: container,
		Follow:    true,
		TailLines: &tailLines,
	}

	stream, err := c.clientset.CoreV1().Pods(namespace).GetLogs(pod, opts).Stream(ctx)
	if err != nil {
		log.Debug().Err(err).Str("pod", namespace+"/"+pod).Msg("Failed to open log stream")
		return
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		parsed := ParseLogLine(line, service, namespace, pod, container)
		if parsed == nil {
			continue
		}

		// Compute fingerprint
		parsed.Fingerprint = Fingerprint(parsed)

		// Determine NATS subject based on reason
		subject := "logs.info"
		switch {
		case strings.HasPrefix(parsed.Reason, "exception"):
			subject = "logs.error"
		case parsed.Reason == "timeout":
			subject = "logs.timeout"
		case parsed.Reason == "oom":
			subject = "logs.error"
		case parsed.Reason == "http.5xx":
			subject = "logs.error"
		case parsed.Reason == "gc.pressure":
			subject = "logs.gc"
		case parsed.Level == "error" || parsed.Level == "fatal" || parsed.Level == "panic":
			subject = "logs.error"
		case parsed.Level == "warn" || parsed.Level == "warning":
			subject = "logs.warn"
		}

		// Publish to bus
		if c.eventBus != nil {
			data, _ := json.Marshal(parsed)
			if err := c.eventBus.Publish(ctx, subject, data); err != nil {
				log.Debug().Err(err).Msg("Failed to publish log event")
			}
		}
	}
}

func inferService(pod corev1.Pod) string {
	// Prefer app.kubernetes.io/name label
	if name, ok := pod.Labels["app.kubernetes.io/name"]; ok {
		return name
	}
	if name, ok := pod.Labels["app"]; ok {
		return name
	}
	// Fall back to owner reference name (deployment/statefulset)
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "ReplicaSet" || ref.Kind == "StatefulSet" || ref.Kind == "DaemonSet" {
			// Strip the replicaset hash suffix for deployment names
			parts := strings.Split(ref.Name, "-")
			if len(parts) > 1 {
				return strings.Join(parts[:len(parts)-1], "-")
			}
			return ref.Name
		}
	}
	return pod.Name
}

// ParsedLog is the structured output of the log parser. Published to NATS
// and consumed by the error aggregator.
type ParsedLog struct {
	Timestamp   time.Time         `json:"ts"`
	Cluster     string            `json:"cluster,omitempty"`
	Namespace   string            `json:"namespace"`
	Pod         string            `json:"pod"`
	Container   string            `json:"container"`
	Service     string            `json:"service"`
	Level       string            `json:"level"`
	Message     string            `json:"message"`
	Error       string            `json:"error,omitempty"`
	StackTrace  string            `json:"stackTrace,omitempty"`
	RequestID   string            `json:"requestId,omitempty"`
	TraceID     string            `json:"traceId,omitempty"`
	URL         string            `json:"url,omitempty"`
	StatusCode  int               `json:"statusCode,omitempty"`
	LatencyMs   float64           `json:"latencyMs,omitempty"`
	Reason      string            `json:"reason"` // exception.java, timeout, oom, http.5xx, gc.pressure, etc.
	Fingerprint string            `json:"fingerprint"`
	Raw         string            `json:"raw"`
	Fields      map[string]string `json:"fields,omitempty"`
}

// StreamCloser wraps io.ReadCloser for streaming
type StreamCloser = io.ReadCloser
