package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCalculateOverallScore(t *testing.T) {
	tests := []struct {
		name     string
		scores   HealthScores
		expected int
	}{
		{
			name: "all perfect scores",
			scores: HealthScores{
				Reliability:  100,
				Security:     100,
				Cost:         100,
				Architecture: 100,
			},
			expected: 100,
		},
		{
			name: "all zero scores",
			scores: HealthScores{
				Reliability:  0,
				Security:     0,
				Cost:         0,
				Architecture: 0,
			},
			expected: 0,
		},
		{
			name: "weighted calculation",
			scores: HealthScores{
				Reliability:  80,
				Security:     70,
				Cost:         60,
				Architecture: 90,
			},
			// 80*0.35 + 70*0.30 + 60*0.20 + 90*0.15 = 28+21+12+13.5 = 74.5 -> 74
			expected: 74,
		},
		{
			name: "security floor cap triggers",
			scores: HealthScores{
				Reliability:  90,
				Security:     40, // below 50
				Cost:         80,
				Architecture: 85,
			},
			// Without cap: 90*0.35 + 40*0.30 + 80*0.20 + 85*0.15 = 31.5+12+16+12.75 = 72.25
			// With security floor cap (sec<50 && overall>60): capped to 60
			expected: 60,
		},
		{
			name: "reliability floor cap triggers",
			scores: HealthScores{
				Reliability:  30, // below 50
				Security:     80,
				Cost:         70,
				Architecture: 90,
			},
			// Without cap: 30*0.35 + 80*0.30 + 70*0.20 + 90*0.15 = 10.5+24+14+13.5 = 62
			// With reliability floor cap (rel<50 && overall>50): capped to 50
			expected: 50,
		},
		{
			name: "both floor caps - reliability wins (lower)",
			scores: HealthScores{
				Reliability:  20, // below 50
				Security:     30, // below 50
				Cost:         90,
				Architecture: 95,
			},
			// Without cap: 20*0.35 + 30*0.30 + 90*0.20 + 95*0.15 = 7+9+18+14.25 = 48.25
			// Security cap: 48.25 < 60, no cap
			// Reliability cap: 48.25 < 50, no cap
			expected: 48,
		},
		{
			name: "security below threshold but overall already low",
			scores: HealthScores{
				Reliability:  50,
				Security:     30,
				Cost:         40,
				Architecture: 50,
			},
			// 50*0.35 + 30*0.30 + 40*0.20 + 50*0.15 = 17.5+9+8+7.5 = 42
			// sec<50 but overall<60, no cap
			expected: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateOverallScore(tt.scores)
			if result != tt.expected {
				t.Errorf("CalculateOverallScore() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestTelemetryEventJSON(t *testing.T) {
	event := TelemetryEvent{
		ID:        "test-1",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Cluster:   "test-cluster",
		Source:    "kubernetes",
		Type:      "Warning",
		Reason:    "CrashLoopBackOff",
		InvolvedObject: InvolvedObject{
			Kind:      "Pod",
			Namespace: "default",
			Name:      "test-pod",
			UID:       "uid-123",
		},
		Message: "Back-off restarting failed container",
		Count:   5,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal TelemetryEvent: %v", err)
	}

	var decoded TelemetryEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal TelemetryEvent: %v", err)
	}

	if decoded.ID != event.ID {
		t.Errorf("ID mismatch: got %q, want %q", decoded.ID, event.ID)
	}
	if decoded.InvolvedObject.Kind != "Pod" {
		t.Errorf("InvolvedObject.Kind mismatch: got %q, want %q", decoded.InvolvedObject.Kind, "Pod")
	}
	if decoded.Count != 5 {
		t.Errorf("Count mismatch: got %d, want %d", decoded.Count, 5)
	}
}

func TestHealthReportJSON(t *testing.T) {
	report := ClusterHealthReport{
		ClusterID: "test",
		Timestamp: time.Now(),
		Scores: HealthScores{
			Overall:      85,
			Reliability:  90,
			Security:     80,
			Cost:         75,
			Architecture: 85,
		},
		Summary: ClusterSummary{
			TotalNodes: 3,
			TotalPods:  50,
			Namespaces: make(map[string]*NamespaceStats),
		},
		TopIssues:       []Issue{},
		Recommendations: []Recommendation{},
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Failed to marshal ClusterHealthReport: %v", err)
	}

	var decoded ClusterHealthReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ClusterHealthReport: %v", err)
	}

	if decoded.ClusterID != "test" {
		t.Errorf("ClusterID mismatch: got %q, want %q", decoded.ClusterID, "test")
	}
	if decoded.Scores.Overall != 85 {
		t.Errorf("Scores.Overall mismatch: got %d, want %d", decoded.Scores.Overall, 85)
	}
}

func TestResourceMetricsJSON(t *testing.T) {
	m := ResourceMetrics{
		Timestamp:    time.Now(),
		Cluster:      "test",
		ResourceType: "pod",
		Resource: ResourceIdentifier{
			Namespace: "default",
			Name:      "test-pod",
		},
		Metrics: map[string]any{
			"cpu_millicores": float64(250),
			"memory_bytes":   float64(1073741824),
		},
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Failed to marshal ResourceMetrics: %v", err)
	}

	var decoded ResourceMetrics
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ResourceMetrics: %v", err)
	}

	if decoded.ResourceType != "pod" {
		t.Errorf("ResourceType mismatch: got %q, want %q", decoded.ResourceType, "pod")
	}
}

func TestScoringWeightsSumToOne(t *testing.T) {
	sum := WeightReliability + WeightSecurity + WeightCost + WeightArchitecture
	if sum < 0.99 || sum > 1.01 {
		t.Errorf("Scoring weights should sum to 1.0, got %f", sum)
	}
}
