// Package types provides shared data models for the K8s Cluster Intelligence Engine.
package types

import "time"

// TelemetryEvent represents a normalized cluster event
type TelemetryEvent struct {
	ID             string                 `json:"id"`
	Timestamp      time.Time              `json:"timestamp"`
	Cluster        string                 `json:"cluster"`
	Source         string                 `json:"source"`
	Type           string                 `json:"type"`
	Reason         string                 `json:"reason"`
	InvolvedObject InvolvedObject         `json:"involvedObject"`
	Message        string                 `json:"message"`
	Count          int32                  `json:"count"`
	FirstTimestamp time.Time              `json:"firstTimestamp"`
	LastTimestamp  time.Time              `json:"lastTimestamp"`
	Metadata       map[string]any `json:"metadata"`
}

// InvolvedObject represents the Kubernetes object involved in an event
type InvolvedObject struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}

// ResourceMetrics holds resource utilization metrics
type ResourceMetrics struct {
	Timestamp    time.Time              `json:"timestamp"`
	Cluster      string                 `json:"cluster"`
	ResourceType string                 `json:"resourceType"`
	Resource     ResourceIdentifier     `json:"resource"`
	Metrics      map[string]any `json:"metrics"`
}

// ResourceIdentifier identifies a Kubernetes resource
type ResourceIdentifier struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// CorrelatedEvidence aggregates events, metrics and logs
type CorrelatedEvidence struct {
	Event       TelemetryEvent         `json:"event"`
	Metrics     map[string][]DataPoint `json:"metrics"`
	LogLines    []string               `json:"logLines"`
	RelatedPods []string               `json:"relatedPods"`
}

// DataPoint represents a time-series point
type DataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// Profile names for the analyzer data source.
const (
	ProfileLive = "live"
	ProfileMock = "mock"
)

// Report state values.
const (
	StateOK       = "ok"
	StateAwaiting = "awaiting"
	StateDegraded = "degraded"
	StateError    = "error"
)

// ComponentHealth describes the reachability of an upstream dependency.
type ComponentHealth struct {
	Reachable bool       `json:"reachable"`
	Endpoint  string     `json:"endpoint,omitempty"`
	LastOKAt  *time.Time `json:"lastOkAt,omitempty"`
	LastError string     `json:"lastError,omitempty"`
}

// ReportStatus describes why a report looks the way it does. It is always
// present on a ClusterHealthReport so the dashboard can render diagnostics
// even when Scores is nil.
type ReportStatus struct {
	State             string          `json:"state"`   // ok | awaiting | degraded | error
	Message           string          `json:"message"` // human-readable summary
	Profile           string          `json:"profile"` // live | mock
	Collector         ComponentHealth `json:"collector"`
	LLM               ComponentHealth `json:"llm"`
	LastAnalysisAt    *time.Time      `json:"lastAnalysisAt,omitempty"`
	LastAnalysisError string          `json:"lastAnalysisError,omitempty"`
}

// ClusterHealthReport is the comprehensive health report.
//
// Scores is a pointer and MAY be nil when the analyzer has no LLM-derived
// scores available (collector unreachable, LLM unreachable, awaiting first
// analysis, etc). The dashboard must render a diagnostic panel in that case
// and MUST NOT substitute default numbers. Status is always populated to
// explain the current state of the report.
type ClusterHealthReport struct {
	ClusterID           string              `json:"clusterId"`
	Timestamp           time.Time           `json:"timestamp"`
	Scores              *HealthScores       `json:"scores"`
	Summary             ClusterSummary      `json:"summary"`
	ResourceUtilization ResourceUtilization `json:"resourceUtilization"`
	TopIssues           []Issue             `json:"topIssues"`
	Recommendations     []Recommendation    `json:"recommendations"`
	SecurityFindings    []SecurityFinding   `json:"securityFindings"`
	EstimatedSavings    float64             `json:"estimatedMonthlySavings"`
	Trends              HealthTrends        `json:"trends"`
	Status              *ReportStatus       `json:"status,omitempty"`
}

// HealthScores contains all health scores
type HealthScores struct {
	Overall      int `json:"overall"`
	Reliability  int `json:"reliability"`
	Security     int `json:"security"`
	Cost         int `json:"cost"`
	Architecture int `json:"architecture"`
}

// HealthTrends tracks score changes
type HealthTrends struct {
	Overall      int `json:"overall"`
	Reliability  int `json:"reliability"`
	Security     int `json:"security"`
	Cost         int `json:"cost"`
	Architecture int `json:"architecture"`
}

// TimelineEvent represents an event for UI rendering
type TimelineEvent struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Type        string    `json:"type"`
	Timestamp   time.Time `json:"timestamp"`
}

// ClusterSummary provides cluster statistics
type ClusterSummary struct {
	TotalNodes      int                        `json:"totalNodes"`
	TotalPods       int                        `json:"totalPods"`
	TotalNamespaces int                        `json:"totalNamespaces"`
	HealthyPods     int                        `json:"healthyPods"`
	UnhealthyPods   int                        `json:"unhealthyPods"`
	PendingPods     int                        `json:"pendingPods"`
	WarningEvents   int                        `json:"warningEvents"`
	CriticalEvents  int                        `json:"criticalEvents"`
	Namespaces      map[string]*NamespaceStats `json:"namespaces"`
}

// NamespaceStats holds metrics for a specific namespace
type NamespaceStats struct {
	CPUUsed    float64 `json:"cpuUsed"`
	MemoryUsed float64 `json:"memoryUsed"`
	PodCount   int     `json:"podCount"`
	Warnings   int     `json:"warnings"`
}

// ResourceUtilization tracks resource usage
type ResourceUtilization struct {
	CPU     ResourceUsage `json:"cpu"`
	Memory  ResourceUsage `json:"memory"`
	Storage ResourceUsage `json:"storage"`
}

// ResourceUsage represents a single resource's usage
type ResourceUsage struct {
	Requested float64 `json:"requested"`
	Used      float64 `json:"used"`
	Capacity  float64 `json:"capacity"`
	Unit      string  `json:"unit"`
}

// Issue represents a detected issue
type Issue struct {
	ID                string               `json:"id"`
	Severity          string               `json:"severity"`
	Category          string               `json:"category"`
	Title             string               `json:"title"`
	Description       string               `json:"description"`
	AffectedResources []string             `json:"affectedResources"`
	Confidence        float64              `json:"confidence"`
	RootCause         string               `json:"rootCause,omitempty"`
	BlastRadius       string               `json:"blastRadius,omitempty"`
	Evidence          []CorrelatedEvidence  `json:"evidence,omitempty"`
	Timestamp         time.Time            `json:"timestamp"`
}

// Recommendation represents an AI-generated recommendation
type Recommendation struct {
	ID                string               `json:"id"`
	Category          string               `json:"category"`
	Subcategory       string               `json:"subcategory"`
	Severity          string               `json:"severity"`
	Confidence        float64              `json:"confidence"`
	Title             string               `json:"title"`
	Description       string               `json:"description"`
	AffectedResources []string             `json:"affectedResources"`
	Impact            RecommendationImpact `json:"impact"`
	Remediation       *Remediation         `json:"remediation,omitempty"`
	AIReasoning       string               `json:"aiReasoning"`
	Timestamp         time.Time            `json:"timestamp"`
}

// RecommendationImpact describes the impact of a recommendation
type RecommendationImpact struct {
	CostSavings *CostSavings `json:"costSavings,omitempty"`
	RiskLevel   string       `json:"riskLevel"`
	BlastRadius string       `json:"blastRadius"`
	Effort      string       `json:"effort"`
}

// CostSavings represents potential cost savings
type CostSavings struct {
	Monthly  float64 `json:"monthly"`
	Currency string  `json:"currency"`
}

// Remediation provides fix instructions
type Remediation struct {
	Type         string                 `json:"type"`
	Automated    bool                   `json:"automated"`
	Patch        map[string]any `json:"patch,omitempty"`
	Instructions string                 `json:"instructions,omitempty"`
}

// SecurityFinding represents a security issue
type SecurityFinding struct {
	ID                string   `json:"id"`
	Category          string   `json:"category"`
	Severity          string   `json:"severity"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	AffectedResources []string `json:"affectedResources"`
	CISControl        string   `json:"cisControl,omitempty"`
	Compliance        []string `json:"compliance,omitempty"`
	Remediation       string   `json:"remediation"`
}

// LLMRequest represents a request to the LLM
type LLMRequest struct {
	Model       string       `json:"model"`
	Messages    []LLMMessage `json:"messages"`
	MaxTokens   int          `json:"max_tokens"`
	Temperature float64      `json:"temperature"`
}

// LLMMessage represents a message in the LLM conversation
type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMResponse represents the LLM response
type LLMResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// Scoring weight constants - single source of truth
const (
	WeightReliability  = 0.35
	WeightSecurity     = 0.30
	WeightCost         = 0.20
	WeightArchitecture = 0.15

	// Floor caps
	SecurityFloorThreshold    = 50
	SecurityFloorCap          = 60
	ReliabilityFloorThreshold = 50
	ReliabilityFloorCap       = 50
)

// CalculateOverallScore computes the weighted overall health score with floor caps
func CalculateOverallScore(scores HealthScores) int {
	overall := float64(scores.Reliability)*WeightReliability +
		float64(scores.Security)*WeightSecurity +
		float64(scores.Cost)*WeightCost +
		float64(scores.Architecture)*WeightArchitecture

	if scores.Security < SecurityFloorThreshold && overall > float64(SecurityFloorCap) {
		overall = float64(SecurityFloorCap)
	}
	if scores.Reliability < ReliabilityFloorThreshold && overall > float64(ReliabilityFloorCap) {
		overall = float64(ReliabilityFloorCap)
	}

	return int(overall)
}
