package main

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func podWithRestarts(name, ns string, restarts int32, age time.Duration) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         ns,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-age)),
		},
		Spec: corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{RestartCount: restarts, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
		},
	}
}

func podWithInitFailure(name, ns string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         ns,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-30 * time.Minute)),
		},
		Spec: corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name:  "init-setup",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off restarting failed container"}},
			}},
			ContainerStatuses: []corev1.ContainerStatus{
				{RestartCount: 0, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			},
		},
	}
}

func TestScan_HighRestarts_Categorized(t *testing.T) {
	pods := []corev1.Pod{
		podWithRestarts("jfrog", "utilities", 742, 48*time.Hour),
		podWithRestarts("kube-proxy", "kube-system", 72, 72*time.Hour),
		podWithRestarts("stable-app", "default", 2, 24*time.Hour),
	}
	cs := fake.NewSimpleClientset(&pods[0], &pods[1], &pods[2])
	scanner := NewPodHealthScanner(cs)
	scanner.Scan(context.Background())

	report := scanner.report
	if report == nil {
		t.Fatal("report is nil")
	}

	var highRestarts *PodHealthCategory
	for i := range report.Categories {
		if report.Categories[i].Name == "high-restarts" {
			highRestarts = &report.Categories[i]
			break
		}
	}
	if highRestarts == nil {
		t.Fatal("expected 'high-restarts' category, got none")
	}
	if highRestarts.Count != 2 {
		t.Fatalf("expected 2 high-restart pods, got %d", highRestarts.Count)
	}
	// stable-app with 2 restarts should NOT be in high-restarts
	for _, p := range highRestarts.Pods {
		if p.Name == "stable-app" {
			t.Error("stable-app (2 restarts) should not be in high-restarts")
		}
	}
}

func TestScan_HighRestarts_BelowThreshold_Healthy(t *testing.T) {
	pods := []corev1.Pod{
		podWithRestarts("healthy-app", "default", 3, 24*time.Hour),
	}
	cs := fake.NewSimpleClientset(&pods[0])
	scanner := NewPodHealthScanner(cs)
	scanner.Scan(context.Background())

	report := scanner.report
	for _, cat := range report.Categories {
		if cat.Name == "high-restarts" {
			t.Errorf("pod with 3 restarts should NOT be in high-restarts, but found %d pods", cat.Count)
		}
	}
	if report.HealthyPods != 1 {
		t.Fatalf("expected 1 healthy pod, got %d", report.HealthyPods)
	}
}

func TestScan_RestartsPerHour(t *testing.T) {
	pods := []corev1.Pod{
		podWithRestarts("fast-crasher", "default", 100, 10*time.Hour),
	}
	cs := fake.NewSimpleClientset(&pods[0])
	scanner := NewPodHealthScanner(cs)
	scanner.Scan(context.Background())

	var found *PodHealthItem
	for _, cat := range scanner.report.Categories {
		for i := range cat.Pods {
			if cat.Pods[i].Name == "fast-crasher" {
				found = &cat.Pods[i]
			}
		}
	}
	if found == nil {
		t.Fatal("fast-crasher not found in any category")
	}
	if found.RestartsPerHour < 9.9 || found.RestartsPerHour > 10.1 {
		t.Fatalf("expected ~10 restarts/hour, got %f", found.RestartsPerHour)
	}
}

func TestScan_InitFailure_Categorized(t *testing.T) {
	pods := []corev1.Pod{
		podWithInitFailure("trivy-scan", "trivy-system"),
	}
	cs := fake.NewSimpleClientset(&pods[0])
	scanner := NewPodHealthScanner(cs)
	scanner.Scan(context.Background())

	var initFail *PodHealthCategory
	for i := range scanner.report.Categories {
		if scanner.report.Categories[i].Name == "init-failure" {
			initFail = &scanner.report.Categories[i]
			break
		}
	}
	if initFail == nil {
		t.Fatal("expected 'init-failure' category, got none")
	}
	if initFail.Count != 1 {
		t.Fatalf("expected 1 init-failure pod, got %d", initFail.Count)
	}
	if initFail.Pods[0].Reason != "InitCrashLoopBackOff" {
		t.Fatalf("expected reason InitCrashLoopBackOff, got %q", initFail.Pods[0].Reason)
	}
}

func TestScan_InitHealthy_NotFlagged(t *testing.T) {
	pods := []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "normal-pod",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
		},
		Spec: corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name:  "init",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}},
			}},
			ContainerStatuses: []corev1.ContainerStatus{
				{RestartCount: 0, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			},
		},
	}}
	cs := fake.NewSimpleClientset(&pods[0])
	scanner := NewPodHealthScanner(cs)
	scanner.Scan(context.Background())

	for _, cat := range scanner.report.Categories {
		if cat.Name == "init-failure" {
			t.Error("pod with init Waiting=PodInitializing should not be flagged")
		}
	}
	if scanner.report.HealthyPods != 1 {
		t.Fatalf("expected 1 healthy pod, got %d", scanner.report.HealthyPods)
	}
}

func TestScan_HighRestartPod_NotCountedHealthy(t *testing.T) {
	pods := []corev1.Pod{
		podWithRestarts("crasher", "default", 10, 5*time.Hour),
	}
	cs := fake.NewSimpleClientset(&pods[0])
	scanner := NewPodHealthScanner(cs)
	scanner.Scan(context.Background())

	if scanner.report.HealthyPods != 0 {
		t.Fatalf("high-restart pod should NOT count as healthy, got healthyPods=%d", scanner.report.HealthyPods)
	}
}
