package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	types "github.com/your-org/cluster-intel/pkg/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// --- RingBuffer Tests ---

func TestRingBuffer_NewRingBuffer(t *testing.T) {
	rb := NewRingBuffer[int](10)
	if rb.Size() != 0 {
		t.Errorf("New buffer should be empty, got size %d", rb.Size())
	}
	if rb.capacity != 10 {
		t.Errorf("Expected capacity 10, got %d", rb.capacity)
	}
}

func TestRingBuffer_Push(t *testing.T) {
	rb := NewRingBuffer[string](3)

	rb.Push("a")
	if rb.Size() != 1 {
		t.Errorf("Expected size 1, got %d", rb.Size())
	}

	rb.Push("b")
	rb.Push("c")
	if rb.Size() != 3 {
		t.Errorf("Expected size 3, got %d", rb.Size())
	}
}

func TestRingBuffer_PushOverflow(t *testing.T) {
	rb := NewRingBuffer[int](3)

	rb.Push(1)
	rb.Push(2)
	rb.Push(3)
	rb.Push(4) // Should overwrite 1

	if rb.Size() != 3 {
		t.Errorf("Expected size 3 after overflow, got %d", rb.Size())
	}

	items := rb.GetAll()
	if len(items) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(items))
	}
	if items[0] != 2 {
		t.Errorf("Expected first item 2 (oldest), got %d", items[0])
	}
	if items[2] != 4 {
		t.Errorf("Expected last item 4 (newest), got %d", items[2])
	}
}

func TestRingBuffer_GetAllEmpty(t *testing.T) {
	rb := NewRingBuffer[int](5)
	items := rb.GetAll()
	if len(items) != 0 {
		t.Errorf("Expected empty slice, got %d items", len(items))
	}
}

func TestRingBuffer_GetAllPreservesOrder(t *testing.T) {
	rb := NewRingBuffer[int](5)
	for i := range 5 {
		rb.Push(i + 1)
	}

	items := rb.GetAll()
	for i, v := range items {
		if v != i+1 {
			t.Errorf("Item %d: expected %d, got %d", i, i+1, v)
		}
	}
}

func TestRingBuffer_WrapAroundOrder(t *testing.T) {
	rb := NewRingBuffer[int](3)

	// Push 5 items into a buffer of capacity 3
	for i := range 5 {
		rb.Push(i + 1)
	}

	// Should have [3, 4, 5]
	items := rb.GetAll()
	expected := []int{3, 4, 5}
	for i, v := range items {
		if v != expected[i] {
			t.Errorf("Item %d: expected %d, got %d", i, expected[i], v)
		}
	}
}

func TestRingBuffer_ConcurrentAccess(t *testing.T) {
	rb := NewRingBuffer[int](100)
	var wg sync.WaitGroup

	// Concurrent writers
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 100 {
				rb.Push(id*100 + j)
			}
		}(i)
	}

	// Concurrent readers
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_ = rb.GetAll()
				_ = rb.Size()
			}
		}()
	}

	wg.Wait()

	// Should not panic and size should be <= capacity
	if rb.Size() > 100 {
		t.Errorf("Size %d exceeds capacity 100", rb.Size())
	}
}

func TestRingBuffer_SingleCapacity(t *testing.T) {
	rb := NewRingBuffer[string](1)
	rb.Push("first")
	rb.Push("second")

	items := rb.GetAll()
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
	if items[0] != "second" {
		t.Errorf("Expected 'second', got %q", items[0])
	}
}

func TestRingBuffer_WithTelemetryEvents(t *testing.T) {
	rb := NewRingBuffer[types.TelemetryEvent](2)

	rb.Push(types.TelemetryEvent{ID: "evt-1", Type: "Normal"})
	rb.Push(types.TelemetryEvent{ID: "evt-2", Type: "Warning"})
	rb.Push(types.TelemetryEvent{ID: "evt-3", Type: "Warning"})

	items := rb.GetAll()
	if len(items) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(items))
	}
	if items[0].ID != "evt-2" {
		t.Errorf("Expected evt-2, got %s", items[0].ID)
	}
	if items[1].ID != "evt-3" {
		t.Errorf("Expected evt-3, got %s", items[1].ID)
	}
}

func TestRingBuffer_WithResourceMetrics(t *testing.T) {
	rb := NewRingBuffer[types.ResourceMetrics](5)

	rb.Push(types.ResourceMetrics{
		ResourceType: "pod",
		Resource:     types.ResourceIdentifier{Name: "pod-1"},
	})

	items := rb.GetAll()
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
	if items[0].Resource.Name != "pod-1" {
		t.Errorf("Expected pod-1, got %s", items[0].Resource.Name)
	}
}

// --- isPodUnhealthy Tests ---

func TestIsPodUnhealthy_HealthyPod(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Ready:        true,
					RestartCount: 0,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
		},
	}

	if isPodUnhealthy(pod) {
		t.Error("Running pod with healthy containers should not be unhealthy")
	}
}

func TestIsPodUnhealthy_FailedPod(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{Phase: corev1.PodFailed},
	}
	if !isPodUnhealthy(pod) {
		t.Error("Failed pod should be unhealthy")
	}
}

func TestIsPodUnhealthy_UnknownPod(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{Phase: corev1.PodUnknown},
	}
	if !isPodUnhealthy(pod) {
		t.Error("Unknown phase pod should be unhealthy")
	}
}

func TestIsPodUnhealthy_CrashLoopBackOff(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "CrashLoopBackOff",
						},
					},
				},
			},
		},
	}
	if !isPodUnhealthy(pod) {
		t.Error("CrashLoopBackOff pod should be unhealthy")
	}
}

func TestIsPodUnhealthy_ImagePullBackOff(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "ImagePullBackOff",
						},
					},
				},
			},
		},
	}
	if !isPodUnhealthy(pod) {
		t.Error("ImagePullBackOff pod should be unhealthy")
	}
}

func TestIsPodUnhealthy_ErrImagePull(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "ErrImagePull",
						},
					},
				},
			},
		},
	}
	if !isPodUnhealthy(pod) {
		t.Error("ErrImagePull pod should be unhealthy")
	}
}

func TestIsPodUnhealthy_HighRestartCount(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Ready:        true,
					RestartCount: 6,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
		},
	}
	if !isPodUnhealthy(pod) {
		t.Error("Pod with restart count > 5 should be unhealthy")
	}
}

func TestIsPodUnhealthy_RestartCountExactly5(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Ready:        true,
					RestartCount: 5,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
		},
	}
	if isPodUnhealthy(pod) {
		t.Error("Pod with restart count exactly 5 should not be unhealthy (threshold is >5)")
	}
}

func TestIsPodUnhealthy_PendingPod(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
	if isPodUnhealthy(pod) {
		t.Error("Pending pod without bad container state should not be unhealthy")
	}
}

func TestIsPodUnhealthy_SucceededPod(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
		},
	}
	if isPodUnhealthy(pod) {
		t.Error("Succeeded pod should not be unhealthy")
	}
}

func TestIsPodUnhealthy_MultipleContainers_OneBad(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Ready:        true,
					RestartCount: 0,
					State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
			},
		},
	}
	if !isPodUnhealthy(pod) {
		t.Error("Pod with one CrashLoopBackOff container should be unhealthy")
	}
}

func TestIsPodUnhealthy_WaitingOtherReason(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "ContainerCreating",
						},
					},
				},
			},
		},
	}
	if isPodUnhealthy(pod) {
		t.Error("ContainerCreating is normal, should not be unhealthy")
	}
}

// --- Config helper Tests ---

func TestGetEnvOrDefault_WithEnv(t *testing.T) {
	os.Setenv("TEST_ENV_VAR", "custom_value")
	defer os.Unsetenv("TEST_ENV_VAR")

	result := getEnvOrDefault("TEST_ENV_VAR", "default")
	if result != "custom_value" {
		t.Errorf("Expected 'custom_value', got %q", result)
	}
}

func TestGetEnvOrDefault_WithoutEnv(t *testing.T) {
	os.Unsetenv("TEST_MISSING_VAR")
	result := getEnvOrDefault("TEST_MISSING_VAR", "default_val")
	if result != "default_val" {
		t.Errorf("Expected 'default_val', got %q", result)
	}
}

func TestGetEnvIntOrDefault_WithEnv(t *testing.T) {
	os.Setenv("TEST_INT_VAR", "42")
	defer os.Unsetenv("TEST_INT_VAR")

	result := getEnvIntOrDefault("TEST_INT_VAR", 10)
	if result != 42 {
		t.Errorf("Expected 42, got %d", result)
	}
}

func TestGetEnvIntOrDefault_WithoutEnv(t *testing.T) {
	os.Unsetenv("TEST_MISSING_INT")
	result := getEnvIntOrDefault("TEST_MISSING_INT", 99)
	if result != 99 {
		t.Errorf("Expected 99, got %d", result)
	}
}

func TestGetEnvIntOrDefault_InvalidValue(t *testing.T) {
	os.Setenv("TEST_BAD_INT", "not_a_number")
	defer os.Unsetenv("TEST_BAD_INT")

	result := getEnvIntOrDefault("TEST_BAD_INT", 50)
	// Sscanf will parse 0 from "not_a_number"
	if result != 0 {
		t.Errorf("Expected 0 from invalid parse, got %d", result)
	}
}

func TestGetDurationOrDefault_WithEnv(t *testing.T) {
	os.Setenv("TEST_DUR_VAR", "30s")
	defer os.Unsetenv("TEST_DUR_VAR")

	result := getDurationOrDefault("TEST_DUR_VAR", 5*time.Minute)
	if result != 30*time.Second {
		t.Errorf("Expected 30s, got %v", result)
	}
}

func TestGetDurationOrDefault_WithoutEnv(t *testing.T) {
	os.Unsetenv("TEST_MISSING_DUR")
	result := getDurationOrDefault("TEST_MISSING_DUR", 5*time.Minute)
	if result != 5*time.Minute {
		t.Errorf("Expected 5m, got %v", result)
	}
}

func TestGetDurationOrDefault_InvalidFormat(t *testing.T) {
	os.Setenv("TEST_BAD_DUR", "invalid")
	defer os.Unsetenv("TEST_BAD_DUR")

	result := getDurationOrDefault("TEST_BAD_DUR", 10*time.Second)
	if result != 10*time.Second {
		t.Errorf("Expected default 10s for invalid format, got %v", result)
	}
}

// --- processEvent integration test (with mock collector) ---

func newTestCollector() *Collector {
	c := &Collector{
		config: Config{
			ClusterID:  "test-cluster",
			BufferSize: 100,
		},
		eventBuffer:   NewRingBuffer[types.TelemetryEvent](100),
		metricsBuffer: NewRingBuffer[types.ResourceMetrics](100),
		stopCh:        make(chan struct{}),
	}
	c.initMetrics()
	return c
}

func TestProcessEvent_Warning(t *testing.T) {
	c := newTestCollector()

	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{UID: "uid-1"},
		Type:       "Warning",
		Reason:     "FailedScheduling",
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Namespace: "default",
			Name:      "test-pod",
			UID:       "pod-uid-1",
		},
		Message: "Insufficient cpu",
		Count:   3,
	}

	c.processEvent(event)

	events := c.eventBuffer.GetAll()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.Type != "Warning" {
		t.Errorf("Expected Warning type, got %s", e.Type)
	}
	if e.Reason != "FailedScheduling" {
		t.Errorf("Expected FailedScheduling reason, got %s", e.Reason)
	}
	if e.InvolvedObject.Kind != "Pod" {
		t.Errorf("Expected Pod kind, got %s", e.InvolvedObject.Kind)
	}
	if e.Cluster != "test-cluster" {
		t.Errorf("Expected test-cluster, got %s", e.Cluster)
	}
	if e.Count != 3 {
		t.Errorf("Expected count 3, got %d", e.Count)
	}
}

func TestProcessEvent_Normal(t *testing.T) {
	c := newTestCollector()

	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{UID: "uid-2"},
		Type:       "Normal",
		Reason:     "Scheduled",
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Namespace: "kube-system",
			Name:      "coredns-abc",
		},
		Message: "Successfully assigned",
		Count:   1,
	}

	c.processEvent(event)

	events := c.eventBuffer.GetAll()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if events[0].Type != "Normal" {
		t.Errorf("Expected Normal type, got %s", events[0].Type)
	}
}

func TestProcessPodStateChange_UnhealthyPod(t *testing.T) {
	c := newTestCollector()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "crash-pod",
			Namespace: "default",
			UID:       "pod-uid-crash",
		},
		Spec: corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "CrashLoopBackOff",
						},
					},
				},
			},
		},
	}

	c.processPodStateChange(pod, "updated")

	events := c.eventBuffer.GetAll()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event for unhealthy pod, got %d", len(events))
	}
	if events[0].Reason != "PodUnhealthy" {
		t.Errorf("Expected PodUnhealthy reason, got %s", events[0].Reason)
	}
}

func TestProcessPodStateChange_HealthyPod(t *testing.T) {
	c := newTestCollector()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "healthy-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Ready: true,
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			},
		},
	}

	c.processPodStateChange(pod, "updated")

	events := c.eventBuffer.GetAll()
	if len(events) != 0 {
		t.Errorf("Expected 0 events for healthy pod, got %d", len(events))
	}
}

func TestProcessPodStateChange_AddedAction(t *testing.T) {
	c := newTestCollector()

	pod := &corev1.Pod{
		Status: corev1.PodStatus{Phase: corev1.PodFailed},
	}

	// Only "updated" action triggers unhealthy check
	c.processPodStateChange(pod, "added")

	events := c.eventBuffer.GetAll()
	if len(events) != 0 {
		t.Errorf("Expected 0 events for 'added' action, got %d", len(events))
	}
}

func TestProcessNodeStateChange_MemoryPressure(t *testing.T) {
	c := newTestCollector()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
			UID:  "node-uid-1",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:    corev1.NodeMemoryPressure,
					Status:  corev1.ConditionTrue,
					Message: "Node has memory pressure",
					Reason:  "KubeletHasInsufficientMemory",
				},
			},
		},
	}

	c.processNodeStateChange(node, "updated")

	events := c.eventBuffer.GetAll()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event for memory pressure, got %d", len(events))
	}
	if events[0].Reason != string(corev1.NodeMemoryPressure) {
		t.Errorf("Expected MemoryPressure reason, got %s", events[0].Reason)
	}
}

func TestProcessNodeStateChange_NoPressure(t *testing.T) {
	c := newTestCollector()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-ok"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	c.processNodeStateChange(node, "updated")

	events := c.eventBuffer.GetAll()
	if len(events) != 0 {
		t.Errorf("Expected 0 events for healthy node, got %d", len(events))
	}
}

func TestProcessNodeStateChange_MultiplePressures(t *testing.T) {
	c := newTestCollector()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "stressed-node", UID: "uid-stressed"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue, Message: "mem"},
				{Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue, Message: "disk"},
				{Type: corev1.NodePIDPressure, Status: corev1.ConditionTrue, Message: "pid"},
			},
		},
	}

	c.processNodeStateChange(node, "updated")

	events := c.eventBuffer.GetAll()
	if len(events) != 3 {
		t.Errorf("Expected 3 events for triple pressure, got %d", len(events))
	}
}

// --- HTTP Handler Tests ---

func TestHealthEndpoint(t *testing.T) {
	c := newTestCollector()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
	if rr.Body.String() != "ok" {
		t.Errorf("Expected 'ok', got %q", rr.Body.String())
	}
	_ = c
}

func TestEventsEndpoint(t *testing.T) {
	c := newTestCollector()
	c.eventBuffer.Push(types.TelemetryEvent{ID: "evt-1", Type: "Warning"})
	c.eventBuffer.Push(types.TelemetryEvent{ID: "evt-2", Type: "Normal"})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		events := c.eventBuffer.GetAll()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(events)
	})

	req := httptest.NewRequest("GET", "/api/v1/events", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	var events []types.TelemetryEvent
	if err := json.NewDecoder(rr.Body).Decode(&events); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(events))
	}
}

func TestMetricsEndpoint(t *testing.T) {
	c := newTestCollector()
	c.metricsBuffer.Push(types.ResourceMetrics{
		ResourceType: "pod",
		Resource:     types.ResourceIdentifier{Name: "pod-1"},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/metrics", func(w http.ResponseWriter, r *http.Request) {
		metrics := c.metricsBuffer.GetAll()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metrics)
	})

	req := httptest.NewRequest("GET", "/api/v1/metrics", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	var metrics []types.ResourceMetrics
	if err := json.NewDecoder(rr.Body).Decode(&metrics); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if len(metrics) != 1 {
		t.Errorf("Expected 1 metric, got %d", len(metrics))
	}
}

func TestInitMetrics_NoPanic(t *testing.T) {
	// Tests that initMetrics with custom registry doesn't panic on repeated calls
	c1 := newTestCollector()
	c2 := newTestCollector()
	_ = c1
	_ = c2
	// If we reach here without panic, the custom registry approach works
}
