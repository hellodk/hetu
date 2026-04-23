package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hellodk/hetu/pkg/bus"
	ucconfig "github.com/hellodk/hetu/pkg/config"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ParsedLog is the structured output of the log parser. Published to NATS
// on subjects logs.error / logs.warn / logs.timeout / logs.gc / logs.info.
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
	Reason      string            `json:"reason"`
	Fingerprint string            `json:"fingerprint"`
	Raw         string            `json:"raw"`
	Fields      map[string]string `json:"fields,omitempty"`
}

// StreamCloser is an alias kept for compatibility.
type StreamCloser = io.ReadCloser

// podLogCollector tails logs from pods and publishes parsed events.
type podLogCollector struct {
	clientset  kubernetes.Interface
	eventBus   *bus.Bus
	clusterID  string
	namespaces []string
	stopCh     chan struct{}
	wg         sync.WaitGroup
	tracked    map[string]context.CancelFunc
	trackedMu  sync.Mutex
}

// startPodLogs builds its own k8s client and NATS bus from cfg, then runs
// the pod log collector until ctx is cancelled. Intended to be called as a
// goroutine from main().
func startPodLogs(ctx context.Context, cfg Config) {
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		kp := os.Getenv("KUBECONFIG")
		if kp == "" {
			kp = os.Getenv("HOME") + "/.kube/config"
		}
		restCfg, err = clientcmd.BuildConfigFromFlags("", kp)
		if err != nil {
			log.Error().Err(err).Msg("podlogs: failed to build k8s config")
			return
		}
	}

	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		log.Error().Err(err).Msg("podlogs: failed to create k8s clientset")
		return
	}

	ucfg, _ := ucconfig.LoadFromEnv("/etc/hetu/config.yaml")

	var eventBus *bus.Bus
	if ucfg.Bus.NATS.Enabled {
		eventBus, err = bus.Connect(ctx, ucfg.Bus.NATS)
		if err != nil {
			log.Warn().Err(err).Msg("podlogs: NATS unavailable — events will be dropped")
		} else {
			defer eventBus.Close()
		}
	}

	var namespaces []string
	if cfg.WatchNamespaces != "" {
		for _, ns := range strings.Split(cfg.WatchNamespaces, ",") {
			if ns = strings.TrimSpace(ns); ns != "" {
				namespaces = append(namespaces, ns)
			}
		}
	}

	plc := &podLogCollector{
		clientset:  cs,
		eventBus:   eventBus,
		clusterID:  cfg.ClusterID,
		namespaces: namespaces,
		stopCh:     make(chan struct{}),
		tracked:    make(map[string]context.CancelFunc),
	}

	plc.wg.Add(1)
	go plc.watchLoop(ctx)

	log.Info().Strs("namespaces", namespaces).Msg("Pod log collector started")

	<-ctx.Done()
	close(plc.stopCh)
	plc.wg.Wait()
	log.Info().Msg("Pod log collector stopped")
}

func (c *podLogCollector) watchLoop(ctx context.Context) {
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

func (c *podLogCollector) reconcile(ctx context.Context) {
	namespaces := c.namespaces
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}

	activePods := map[string]bool{}

	for _, ns := range namespaces {
		pods, err := c.clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
			FieldSelector: "status.phase=Running",
		})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("podlogs: failed to list pods")
			continue
		}

		for _, pod := range pods.Items {
			key := pod.Namespace + "/" + pod.Name
			activePods[key] = true

			c.trackedMu.Lock()
			_, exists := c.tracked[key]
			c.trackedMu.Unlock()

			if !exists {
				svc := inferService(pod)
				for _, container := range pod.Spec.Containers {
					c.startTailer(ctx, pod.Namespace, pod.Name, container.Name, svc)
				}
			}
		}
	}

	c.trackedMu.Lock()
	for key, cancelFn := range c.tracked {
		if !activePods[key] {
			cancelFn()
			delete(c.tracked, key)
		}
	}
	c.trackedMu.Unlock()
}

func (c *podLogCollector) startTailer(ctx context.Context, namespace, pod, container, service string) {
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

	log.Debug().Str("pod", key).Str("container", container).Msg("podlogs: started tailer")
}

func (c *podLogCollector) tailPod(ctx context.Context, namespace, pod, container, service string) {
	tailLines := int64(100)
	stream, err := c.clientset.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{
		Container: container,
		Follow:    true,
		TailLines: &tailLines,
	}).Stream(ctx)
	if err != nil {
		log.Debug().Err(err).Str("pod", namespace+"/"+pod).Msg("podlogs: failed to open log stream")
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
		parsed.Cluster = c.clusterID
		parsed.Fingerprint = Fingerprint(parsed)

		subject := logSubject(parsed)

		if c.eventBus != nil {
			data, _ := json.Marshal(parsed)
			if err := c.eventBus.Publish(ctx, subject, data); err != nil {
				log.Debug().Err(err).Msg("podlogs: failed to publish")
			}
		}
	}
}

func logSubject(p *ParsedLog) string {
	switch {
	case strings.HasPrefix(p.Reason, "exception"), p.Reason == "oom", p.Reason == "http.5xx":
		return "logs.error"
	case p.Reason == "timeout":
		return "logs.timeout"
	case p.Reason == "gc.pressure":
		return "logs.gc"
	case p.Level == "error" || p.Level == "fatal" || p.Level == "panic":
		return "logs.error"
	case p.Level == "warn" || p.Level == "warning":
		return "logs.warn"
	default:
		return "logs.info"
	}
}

func inferService(pod corev1.Pod) string {
	if name, ok := pod.Labels["app.kubernetes.io/name"]; ok {
		return name
	}
	if name, ok := pod.Labels["app"]; ok {
		return name
	}
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "ReplicaSet" || ref.Kind == "StatefulSet" || ref.Kind == "DaemonSet" {
			parts := strings.Split(ref.Name, "-")
			if len(parts) > 1 {
				return strings.Join(parts[:len(parts)-1], "-")
			}
			return ref.Name
		}
	}
	return pod.Name
}
