// Package main implements the K8s Cluster Intelligence Engine collector.
// It watches Kubernetes resources, collects metrics, and publishes to the processing pipeline.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	ucconfig "github.com/your-org/cluster-intel/pkg/config"
	types "github.com/your-org/cluster-intel/pkg/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Config holds the collector configuration
type Config struct {
	ClusterID             string        `json:"clusterId"`
	MetricsPort           int           `json:"metricsPort"`
	HealthPort            int           `json:"healthPort"`
	ResyncPeriod          time.Duration `json:"resyncPeriod"`
	MetricsScrapeInterval time.Duration `json:"metricsScrapeInterval"`
	BufferSize            int           `json:"bufferSize"`
	NATSEndpoint          string        `json:"natsEndpoint"`
	PrometheusEndpoint    string        `json:"prometheusEndpoint"`
}

// Collector is the main collector service
type Collector struct {
	config          Config
	clientset       *kubernetes.Clientset
	metricsClient   *metricsv1beta1.Clientset
	informerFactory informers.SharedInformerFactory
	eventBuffer     *RingBuffer[types.TelemetryEvent]
	metricsBuffer   *RingBuffer[types.ResourceMetrics]
	stopCh          chan struct{}
	wg              sync.WaitGroup

	// HTTP servers for graceful shutdown
	metricsServer *http.Server
	healthServer  *http.Server

	// Prometheus metrics (custom registry to avoid MustRegister panics)
	registry          *prometheus.Registry
	eventsCollected   prometheus.Counter
	metricsCollected  prometheus.Counter
	collectionErrors  prometheus.Counter
	bufferUtilization prometheus.Gauge
}

// RingBuffer is a thread-safe generic circular buffer for telemetry data
type RingBuffer[T any] struct {
	mu       sync.RWMutex
	data     []T
	head     int
	tail     int
	size     int
	capacity int
}

// NewRingBuffer creates a new ring buffer
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	return &RingBuffer[T]{
		data:     make([]T, capacity),
		capacity: capacity,
	}
}

// Push adds an item to the buffer
func (rb *RingBuffer[T]) Push(item T) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.data[rb.tail] = item
	rb.tail = (rb.tail + 1) % rb.capacity

	if rb.size < rb.capacity {
		rb.size++
	} else {
		rb.head = (rb.head + 1) % rb.capacity
	}
}

// GetAll returns all items in the buffer
func (rb *RingBuffer[T]) GetAll() []T {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	result := make([]T, rb.size)
	for i := range rb.size {
		idx := (rb.head + i) % rb.capacity
		result[i] = rb.data[idx]
	}
	return result
}

// Size returns the current buffer size
func (rb *RingBuffer[T]) Size() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.size
}

// NewCollector creates a new collector instance
func NewCollector(config Config) (*Collector, error) {
	var kubeConfig *rest.Config
	var err error

	if kubeConfigPath := os.Getenv("KUBECONFIG"); kubeConfigPath != "" {
		kubeConfig, err = clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	} else {
		kubeConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	kubeConfig.QPS = 50
	kubeConfig.Burst = 100

	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	metricsClient, err := metricsv1beta1.NewForConfig(kubeConfig)
	if err != nil {
		log.Warn().Err(err).Msg("metrics API not available, some features disabled")
	}

	informerFactory := informers.NewSharedInformerFactory(clientset, config.ResyncPeriod)

	collector := &Collector{
		config:          config,
		clientset:       clientset,
		metricsClient:   metricsClient,
		informerFactory: informerFactory,
		eventBuffer:     NewRingBuffer[types.TelemetryEvent](config.BufferSize),
		metricsBuffer:   NewRingBuffer[types.ResourceMetrics](config.BufferSize),
		stopCh:          make(chan struct{}),
	}

	collector.initMetrics()

	return collector, nil
}

// initMetrics initializes Prometheus metrics with a custom registry
func (c *Collector) initMetrics() {
	c.registry = prometheus.NewRegistry()

	c.eventsCollected = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cluster_intel_events_collected_total",
		Help: "Total number of events collected",
	})

	c.metricsCollected = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cluster_intel_metrics_collected_total",
		Help: "Total number of metrics collected",
	})

	c.collectionErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cluster_intel_collection_errors_total",
		Help: "Total number of collection errors",
	})

	c.bufferUtilization = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cluster_intel_buffer_utilization",
		Help: "Current buffer utilization ratio",
	})

	c.registry.MustRegister(
		c.eventsCollected,
		c.metricsCollected,
		c.collectionErrors,
		c.bufferUtilization,
	)
}

// Start begins collecting cluster telemetry
func (c *Collector) Start(ctx context.Context) error {
	log.Info().Str("cluster", c.config.ClusterID).Msg("Starting collector")

	c.setupEventInformer()
	c.setupPodInformer()
	c.setupNodeInformer()

	c.informerFactory.Start(c.stopCh)

	log.Info().Msg("Waiting for informer cache sync")
	c.informerFactory.WaitForCacheSync(c.stopCh)
	log.Info().Msg("Informer cache synced")

	c.wg.Add(1)
	go c.collectMetricsLoop(ctx)

	c.wg.Add(1)
	go c.updateBufferStats(ctx)

	c.wg.Add(2)
	go c.serveMetrics()
	go c.serveHealth()

	return nil
}

// setupEventInformer creates the event informer
func (c *Collector) setupEventInformer() {
	eventInformer := c.informerFactory.Core().V1().Events().Informer()

	eventInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			event := obj.(*corev1.Event)
			c.processEvent(event)
		},
		UpdateFunc: func(oldObj, newObj any) {
			event := newObj.(*corev1.Event)
			c.processEvent(event)
		},
	})
}

// setupPodInformer creates the pod informer
func (c *Collector) setupPodInformer() {
	podInformer := c.informerFactory.Core().V1().Pods().Informer()

	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			pod := obj.(*corev1.Pod)
			c.processPodStateChange(pod, "added")
		},
		UpdateFunc: func(oldObj, newObj any) {
			pod := newObj.(*corev1.Pod)
			c.processPodStateChange(pod, "updated")
		},
		DeleteFunc: func(obj any) {
			pod := obj.(*corev1.Pod)
			c.processPodStateChange(pod, "deleted")
		},
	})
}

// setupNodeInformer creates the node informer
func (c *Collector) setupNodeInformer() {
	nodeInformer := c.informerFactory.Core().V1().Nodes().Informer()

	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			node := obj.(*corev1.Node)
			c.processNodeStateChange(node, "added")
		},
		UpdateFunc: func(oldObj, newObj any) {
			node := newObj.(*corev1.Node)
			c.processNodeStateChange(node, "updated")
		},
		DeleteFunc: func(obj any) {
			node := obj.(*corev1.Node)
			c.processNodeStateChange(node, "deleted")
		},
	})
}

// processEvent converts a Kubernetes event to a telemetry event
func (c *Collector) processEvent(event *corev1.Event) {
	telemetryEvent := types.TelemetryEvent{
		ID:        fmt.Sprintf("evt-%s-%d", event.UID, event.Count),
		Timestamp: time.Now(),
		Cluster:   c.config.ClusterID,
		Source:    "kubernetes",
		Type:      event.Type,
		Reason:    event.Reason,
		InvolvedObject: types.InvolvedObject{
			Kind:      event.InvolvedObject.Kind,
			Namespace: event.InvolvedObject.Namespace,
			Name:      event.InvolvedObject.Name,
			UID:       string(event.InvolvedObject.UID),
		},
		Message:        event.Message,
		Count:          event.Count,
		FirstTimestamp: event.FirstTimestamp.Time,
		LastTimestamp:  event.LastTimestamp.Time,
		Metadata: map[string]any{
			"source": event.Source.Component,
			"host":   event.Source.Host,
		},
	}

	c.eventBuffer.Push(telemetryEvent)
	c.eventsCollected.Inc()

	if event.Type == "Warning" {
		log.Warn().
			Str("reason", event.Reason).
			Str("object", fmt.Sprintf("%s/%s", event.InvolvedObject.Namespace, event.InvolvedObject.Name)).
			Str("message", event.Message).
			Msg("Warning event detected")
	}
}

// processPodStateChange handles pod state changes
func (c *Collector) processPodStateChange(pod *corev1.Pod, action string) {
	containerStatuses := make([]map[string]any, 0)
	for _, cs := range pod.Status.ContainerStatuses {
		status := map[string]any{
			"name":         cs.Name,
			"ready":        cs.Ready,
			"restartCount": cs.RestartCount,
		}

		if cs.State.Running != nil {
			status["state"] = "running"
			status["startedAt"] = cs.State.Running.StartedAt.Time
		} else if cs.State.Waiting != nil {
			status["state"] = "waiting"
			status["reason"] = cs.State.Waiting.Reason
			status["message"] = cs.State.Waiting.Message
		} else if cs.State.Terminated != nil {
			status["state"] = "terminated"
			status["reason"] = cs.State.Terminated.Reason
			status["exitCode"] = cs.State.Terminated.ExitCode
		}

		containerStatuses = append(containerStatuses, status)
	}

	if action == "updated" && isPodUnhealthy(pod) {
		event := types.TelemetryEvent{
			ID:        fmt.Sprintf("pod-state-%s-%d", pod.UID, time.Now().UnixNano()),
			Timestamp: time.Now(),
			Cluster:   c.config.ClusterID,
			Source:    "collector",
			Type:      "Warning",
			Reason:    "PodUnhealthy",
			InvolvedObject: types.InvolvedObject{
				Kind:      "Pod",
				Namespace: pod.Namespace,
				Name:      pod.Name,
				UID:       string(pod.UID),
			},
			Message: fmt.Sprintf("Pod %s/%s is unhealthy: %s", pod.Namespace, pod.Name, pod.Status.Phase),
			Metadata: map[string]any{
				"phase":             pod.Status.Phase,
				"containerStatuses": containerStatuses,
				"conditions":        pod.Status.Conditions,
				"nodeName":          pod.Spec.NodeName,
			},
		}
		c.eventBuffer.Push(event)
		c.eventsCollected.Inc()
	}
}

// isPodUnhealthy checks if a pod is in an unhealthy state
func isPodUnhealthy(pod *corev1.Pod) bool {
	if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodUnknown {
		return true
	}

	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			if reason == "CrashLoopBackOff" || reason == "ImagePullBackOff" || reason == "ErrImagePull" {
				return true
			}
		}
		if cs.RestartCount > 5 {
			return true
		}
	}

	return false
}

// processNodeStateChange handles node state changes
func (c *Collector) processNodeStateChange(node *corev1.Node, action string) {
	for _, condition := range node.Status.Conditions {
		if condition.Status == corev1.ConditionTrue {
			switch condition.Type {
			case corev1.NodeMemoryPressure, corev1.NodeDiskPressure, corev1.NodePIDPressure:
				event := types.TelemetryEvent{
					ID:        fmt.Sprintf("node-pressure-%s-%d", node.UID, time.Now().UnixNano()),
					Timestamp: time.Now(),
					Cluster:   c.config.ClusterID,
					Source:    "collector",
					Type:      "Warning",
					Reason:    string(condition.Type),
					InvolvedObject: types.InvolvedObject{
						Kind: "Node",
						Name: node.Name,
						UID:  string(node.UID),
					},
					Message: condition.Message,
					Metadata: map[string]any{
						"reason":         condition.Reason,
						"lastTransition": condition.LastTransitionTime,
					},
				}
				c.eventBuffer.Push(event)
				c.eventsCollected.Inc()
			}
		}
	}
}

// collectMetricsLoop periodically collects resource metrics
func (c *Collector) collectMetricsLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.MetricsScrapeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.collectPodMetrics(ctx)
			c.collectNodeMetrics(ctx)
			c.collectPVCMetrics(ctx)
			c.collectHPAMetrics(ctx)
		}
	}
}

// collectPodMetrics collects metrics from the metrics API
func (c *Collector) collectPodMetrics(ctx context.Context) {
	if c.metricsClient == nil {
		return
	}

	podMetricsList, err := c.metricsClient.MetricsV1beta1().PodMetricses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Error().Err(err).Msg("Failed to collect pod metrics")
		c.collectionErrors.Inc()
		return
	}

	for _, podMetrics := range podMetricsList.Items {
		var cpuUsage, memoryUsage int64
		var cpuReq, memoryReq int64

		for _, container := range podMetrics.Containers {
			cpuUsage += container.Usage.Cpu().MilliValue()
			memoryUsage += container.Usage.Memory().Value()
		}

		if pod, err := c.informerFactory.Core().V1().Pods().Lister().Pods(podMetrics.Namespace).Get(podMetrics.Name); err == nil {
			for _, container := range pod.Spec.Containers {
				if req := container.Resources.Requests.Cpu(); req != nil {
					cpuReq += req.MilliValue()
				}
				if req := container.Resources.Requests.Memory(); req != nil {
					memoryReq += req.Value()
				}
			}
		}

		metrics := types.ResourceMetrics{
			Timestamp:    time.Now(),
			Cluster:      c.config.ClusterID,
			ResourceType: "pod",
			Resource: types.ResourceIdentifier{
				Namespace: podMetrics.Namespace,
				Name:      podMetrics.Name,
			},
			Metrics: map[string]any{
				"cpu_millicores":           cpuUsage,
				"memory_bytes":             memoryUsage,
				"cpu_requested_millicores": cpuReq,
				"memory_requested_bytes":   memoryReq,
				"container_count":          len(podMetrics.Containers),
			},
		}

		c.metricsBuffer.Push(metrics)
		c.metricsCollected.Inc()
	}
}

// collectNodeMetrics collects node-level metrics
func (c *Collector) collectNodeMetrics(ctx context.Context) {
	if c.metricsClient == nil {
		return
	}

	nodeMetricsList, err := c.metricsClient.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Error().Err(err).Msg("Failed to collect node metrics")
		c.collectionErrors.Inc()
		return
	}

	for _, nodeMetrics := range nodeMetricsList.Items {
		var cpuCap, memCap int64

		if node, err := c.informerFactory.Core().V1().Nodes().Lister().Get(nodeMetrics.Name); err == nil {
			if cap := node.Status.Capacity.Cpu(); cap != nil {
				cpuCap = cap.MilliValue()
			}
			if cap := node.Status.Capacity.Memory(); cap != nil {
				memCap = cap.Value()
			}
		}

		metrics := types.ResourceMetrics{
			Timestamp:    time.Now(),
			Cluster:      c.config.ClusterID,
			ResourceType: "node",
			Resource: types.ResourceIdentifier{
				Name: nodeMetrics.Name,
			},
			Metrics: map[string]any{
				"cpu_millicores":          nodeMetrics.Usage.Cpu().MilliValue(),
				"memory_bytes":            nodeMetrics.Usage.Memory().Value(),
				"cpu_capacity_millicores": cpuCap,
				"memory_capacity_bytes":   memCap,
			},
		}

		c.metricsBuffer.Push(metrics)
		c.metricsCollected.Inc()
	}
}

// updateBufferStats updates buffer utilization metrics
func (c *Collector) updateBufferStats(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			eventUtil := float64(c.eventBuffer.Size()) / float64(c.config.BufferSize)
			metricsUtil := float64(c.metricsBuffer.Size()) / float64(c.config.BufferSize)
			c.bufferUtilization.Set((eventUtil + metricsUtil) / 2)
		}
	}
}

// serveMetrics starts the Prometheus metrics server
func (c *Collector) serveMetrics() {
	defer c.wg.Done()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(c.registry, promhttp.HandlerOpts{}))

	c.metricsServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", c.config.MetricsPort),
		Handler: mux,
	}

	log.Info().Int("port", c.config.MetricsPort).Msg("Starting metrics server")
	if err := c.metricsServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Error().Err(err).Msg("Metrics server error")
	}
}

// serveHealth starts the health check and API server
func (c *Collector) serveHealth() {
	defer c.wg.Done()

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("/api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		events := c.eventBuffer.GetAll()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(events)
	})

	mux.HandleFunc("/api/v1/events/correlated", func(w http.ResponseWriter, r *http.Request) {
		correlated := c.GetCorrelatedEvents(r.Context())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(correlated)
	})

	mux.HandleFunc("/api/v1/dns/health", func(w http.ResponseWriter, r *http.Request) {
		dns := c.getDNSHealth(r.Context())
		if dns == nil {
			http.Error(w, "DNS metrics unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dns)
	})

	mux.HandleFunc("/api/v1/pods/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) != 7 || parts[6] != "logs" {
			http.NotFound(w, r)
			return
		}
		namespace := parts[4]
		pod := parts[5]

		tailLines := int64(100)
		if lines := r.URL.Query().Get("tailLines"); lines != "" {
			var parsed int64
			fmt.Sscanf(lines, "%d", &parsed)
			if parsed > 0 {
				tailLines = parsed
			}
		}

		logs, err := c.fetchPodLogs(r.Context(), namespace, pod, tailLines)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string][]string{"logs": logs})
	})

	mux.HandleFunc("/api/v1/metrics", func(w http.ResponseWriter, r *http.Request) {
		metrics := c.metricsBuffer.GetAll()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metrics)
	})

	c.healthServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", c.config.HealthPort),
		Handler: mux,
	}

	log.Info().Int("port", c.config.HealthPort).Msg("Starting health server")
	if err := c.healthServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Error().Err(err).Msg("Health server error")
	}
}

// Stop gracefully stops the collector
func (c *Collector) Stop() {
	log.Info().Msg("Stopping collector")
	close(c.stopCh)

	// Gracefully shutdown HTTP servers
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if c.metricsServer != nil {
		c.metricsServer.Shutdown(shutdownCtx)
	}
	if c.healthServer != nil {
		c.healthServer.Shutdown(shutdownCtx)
	}

	c.wg.Wait()
}

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// v7: Try loading unified config file (CI_CONFIG env or /etc/cluster-intel/config.yaml).
	// If available, its values seed the Config below; env vars still override as before.
	ucfg, ucfgErr := ucconfig.LoadFromEnv("/etc/cluster-intel/config.yaml")
	if ucfgErr != nil {
		log.Debug().Err(ucfgErr).Msg("No unified config loaded, using legacy env vars")
	} else {
		log.Info().Str("cluster", ucfg.Cluster.ID).Msg("Loaded unified config")
	}

	config := Config{
		ClusterID:             coalesce(getEnvOrDefault("CLUSTER_ID", ""), ucfg.Cluster.ID, "default"),
		MetricsPort:           getEnvIntOrDefault("METRICS_PORT", ucfg.Server.MetricsPort),
		HealthPort:            getEnvIntOrDefault("HEALTH_PORT", ucfg.Server.APIPort),
		ResyncPeriod:          getDurationOrDefault("RESYNC_PERIOD", 5*time.Minute),
		MetricsScrapeInterval: getDurationOrDefault("METRICS_SCRAPE_INTERVAL", 30*time.Second),
		BufferSize:            getEnvIntOrDefault("BUFFER_SIZE", 10000),
		NATSEndpoint:          coalesce(getEnvOrDefault("NATS_ENDPOINT", ""), ucfg.Bus.NATS.URL, "nats://nats:4222"),
		PrometheusEndpoint:    getEnvOrDefault("PROMETHEUS_ENDPOINT", "http://prometheus:9090"),
	}

	collector, err := NewCollector(config)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create collector")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := collector.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to start collector")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.Info().Str("signal", sig.String()).Msg("Received shutdown signal")

	cancel()
	collector.Stop()
	log.Info().Msg("Collector stopped")
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvIntOrDefault(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		var result int
		fmt.Sscanf(val, "%d", &result)
		return result
	}
	return defaultVal
}

func getDurationOrDefault(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}

// coalesce returns the first non-empty string.
func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
