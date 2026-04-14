package main

import (
	"testing"
	"time"
)

// --- Correlator.Evict -------------------------------------------------------

func TestCorrelatorEvict_ResolvedTTL(t *testing.T) {
	c := NewCorrelator("test", time.Minute)
	old := time.Now().Add(-48 * time.Hour)
	fresh := time.Now().Add(-10 * time.Minute)

	c.incidents[1] = &Incident{ID: 1, Status: "resolved", DetectedAt: old, ResolvedAt: &old}
	c.incidents[2] = &Incident{ID: 2, Status: "resolved", DetectedAt: fresh, ResolvedAt: &fresh}

	n := c.Evict(24*time.Hour, 72*time.Hour, 0)
	if n != 1 {
		t.Fatalf("expected 1 eviction, got %d", n)
	}
	if _, ok := c.incidents[1]; ok {
		t.Error("incident 1 should have been evicted")
	}
	if _, ok := c.incidents[2]; !ok {
		t.Error("incident 2 should have been kept")
	}
}

func TestCorrelatorEvict_ActiveTTLWhenStillOpen(t *testing.T) {
	c := NewCorrelator("test", time.Minute)
	veryOld := time.Now().Add(-100 * time.Hour)

	c.incidents[1] = &Incident{ID: 1, Status: "open", DetectedAt: veryOld}

	n := c.Evict(24*time.Hour, 48*time.Hour, 0)
	if n != 1 {
		t.Fatalf("expected 1 eviction, got %d", n)
	}
	if _, ok := c.incidents[1]; ok {
		t.Error("open incident past active TTL should have been evicted")
	}
}

func TestCorrelatorEvict_NilResolvedAtFallsThrough(t *testing.T) {
	c := NewCorrelator("test", time.Minute)
	old := time.Now().Add(-48 * time.Hour)

	// Resolved but no ResolvedAt timestamp — Evict should fall through to
	// the DetectedAt comparison.
	c.incidents[1] = &Incident{ID: 1, Status: "resolved", DetectedAt: old, ResolvedAt: nil}

	n := c.Evict(24*time.Hour, 72*time.Hour, 0)
	if n != 1 {
		t.Fatalf("expected 1 eviction with nil ResolvedAt fall-through, got %d", n)
	}
}

func TestCorrelatorEvict_MaxSizeEvictsOldest(t *testing.T) {
	c := NewCorrelator("test", time.Minute)
	base := time.Now().Add(-time.Hour)
	for i := int64(1); i <= 10; i++ {
		c.incidents[i] = &Incident{ID: i, Status: "open", DetectedAt: base.Add(time.Duration(i) * time.Minute)}
	}

	// Generous TTLs so nothing goes via TTL path.
	n := c.Evict(24*time.Hour, 24*time.Hour, 3)
	if n != 7 {
		t.Fatalf("expected 7 evictions to reach cap 3, got %d", n)
	}
	if len(c.incidents) != 3 {
		t.Fatalf("expected 3 incidents remaining, got %d", len(c.incidents))
	}
	// The three newest (IDs 8, 9, 10) should survive.
	for _, id := range []int64{8, 9, 10} {
		if _, ok := c.incidents[id]; !ok {
			t.Errorf("expected incident %d to survive", id)
		}
	}
}

// --- ErrorAggregator.Evict --------------------------------------------------

func TestErrorAggregatorEvict_LastSeenTTL(t *testing.T) {
	ea := NewErrorAggregator()
	now := time.Now()

	ea.groups["fp-old"] = &ErrorGroup{Fingerprint: "fp-old", LastSeen: now.Add(-8 * 24 * time.Hour), Status: "open"}
	ea.groups["fp-new"] = &ErrorGroup{Fingerprint: "fp-new", LastSeen: now.Add(-1 * time.Hour), Status: "open"}
	ea.occurrences["fp-old"] = []ErrorOccurrence{{Timestamp: now.Add(-8 * 24 * time.Hour)}}
	ea.occurrences["fp-new"] = []ErrorOccurrence{{Timestamp: now.Add(-1 * time.Hour)}}

	n := ea.Evict(7*24*time.Hour, 0)
	if n != 1 {
		t.Fatalf("expected 1 eviction, got %d", n)
	}
	if _, ok := ea.groups["fp-old"]; ok {
		t.Error("old group should have been removed")
	}
	if _, ok := ea.occurrences["fp-old"]; ok {
		t.Error("occurrences for old fingerprint should have been removed")
	}
}

func TestErrorAggregatorEvict_MaxSize(t *testing.T) {
	ea := NewErrorAggregator()
	now := time.Now()
	for i := 1; i <= 5; i++ {
		fp := "fp-" + string(rune('a'+i-1))
		ea.groups[fp] = &ErrorGroup{Fingerprint: fp, LastSeen: now.Add(time.Duration(i) * time.Minute), Status: "open"}
	}
	n := ea.Evict(24*time.Hour, 2)
	if n != 3 {
		t.Fatalf("expected 3 evictions to reach cap, got %d", n)
	}
	if len(ea.groups) != 2 {
		t.Fatalf("expected 2 groups remaining, got %d", len(ea.groups))
	}
}

// --- AnomalyDetector.EvictStats ---------------------------------------------

func TestAnomalyDetectorEvictStats_TTL(t *testing.T) {
	d := NewAnomalyDetector("", "test")
	now := time.Now()

	d.stats["ns/svc/a"] = &rollingStats{maxLen: 60, lastUpdated: now.Add(-3 * time.Hour)}
	d.stats["ns/svc/b"] = &rollingStats{maxLen: 60, lastUpdated: now.Add(-5 * time.Minute)}

	n := d.EvictStats(2*time.Hour, 0)
	if n != 1 {
		t.Fatalf("expected 1 eviction, got %d", n)
	}
	if _, ok := d.stats["ns/svc/a"]; ok {
		t.Error("stale stats should have been removed")
	}
}

func TestAnomalyDetectorEvictStats_MaxSize(t *testing.T) {
	d := NewAnomalyDetector("", "test")
	base := time.Now().Add(-time.Hour)
	for i := 1; i <= 5; i++ {
		key := "ns/svc/m" + string(rune('0'+i))
		d.stats[key] = &rollingStats{maxLen: 60, lastUpdated: base.Add(time.Duration(i) * time.Minute)}
	}

	n := d.EvictStats(24*time.Hour, 2)
	if n != 3 {
		t.Fatalf("expected 3 evictions, got %d", n)
	}
	if len(d.stats) != 2 {
		t.Fatalf("expected 2 stats remaining, got %d", len(d.stats))
	}
}

// --- RCAEngine.Evict --------------------------------------------------------

func TestRCAEngineEvict_OrphanDetection(t *testing.T) {
	c := NewCorrelator("test", time.Minute)
	// Incident 1 exists; incident 2 never existed (simulates prior correlator eviction).
	c.incidents[1] = &Incident{ID: 1, Status: "open", DetectedAt: time.Now()}

	e := &RCAEngine{
		reports:    make(map[int64]*RCAReport),
		correlator: c,
	}
	now := time.Now()
	e.reports[1] = &RCAReport{IncidentID: 1, CreatedAt: now}
	e.reports[2] = &RCAReport{IncidentID: 2, CreatedAt: now} // orphan

	n := e.Evict(24*time.Hour, 0)
	if n != 1 {
		t.Fatalf("expected 1 orphan eviction, got %d", n)
	}
	if _, ok := e.reports[1]; !ok {
		t.Error("report for existing incident should have been kept")
	}
	if _, ok := e.reports[2]; ok {
		t.Error("orphan report should have been evicted")
	}
}

func TestRCAEngineEvict_CreatedAtTTL(t *testing.T) {
	c := NewCorrelator("test", time.Minute)
	c.incidents[1] = &Incident{ID: 1, Status: "open", DetectedAt: time.Now()}

	e := &RCAEngine{
		reports:    make(map[int64]*RCAReport),
		correlator: c,
	}
	e.reports[1] = &RCAReport{IncidentID: 1, CreatedAt: time.Now().Add(-5 * 24 * time.Hour)}

	n := e.Evict(48*time.Hour, 0)
	if n != 1 {
		t.Fatalf("expected TTL eviction, got %d", n)
	}
}

// --- OptimizerRegistry.Evict ------------------------------------------------

func TestOptimizerRegistryEvict_NonOpenTTL(t *testing.T) {
	r := NewOptimizerRegistry("", "test")
	r.recommendations = make(map[int64]*OptRecommendation) // reset to known state
	old := time.Now().Add(-30 * 24 * time.Hour)

	r.recommendations[1] = &OptRecommendation{ID: 1, Status: "open", CreatedAt: old}
	r.recommendations[2] = &OptRecommendation{ID: 2, Status: "accepted", CreatedAt: old}
	r.recommendations[3] = &OptRecommendation{ID: 3, Status: "dismissed", CreatedAt: old}
	r.recommendations[4] = &OptRecommendation{ID: 4, Status: "applied", CreatedAt: old}

	n := r.Evict(7*24*time.Hour, 0)
	if n != 3 {
		t.Fatalf("expected 3 closed evictions (open preserved), got %d", n)
	}
	if _, ok := r.recommendations[1]; !ok {
		t.Error("open recommendation should have been preserved regardless of age")
	}
}

func TestOptimizerRegistryEvict_MaxSizeClosedFirst(t *testing.T) {
	r := NewOptimizerRegistry("", "test")
	r.recommendations = make(map[int64]*OptRecommendation)
	now := time.Now()

	r.recommendations[1] = &OptRecommendation{ID: 1, Status: "open", CreatedAt: now.Add(-1 * time.Minute)}
	r.recommendations[2] = &OptRecommendation{ID: 2, Status: "open", CreatedAt: now.Add(-2 * time.Minute)}
	r.recommendations[3] = &OptRecommendation{ID: 3, Status: "accepted", CreatedAt: now.Add(-3 * time.Minute)}
	r.recommendations[4] = &OptRecommendation{ID: 4, Status: "dismissed", CreatedAt: now.Add(-4 * time.Minute)}

	// Cap at 2 with a TTL too short to trigger the TTL path for the closed ones.
	n := r.Evict(24*time.Hour, 2)
	if n != 2 {
		t.Fatalf("expected 2 evictions, got %d", n)
	}
	// The two closed recommendations (3 and 4) should be evicted first.
	if _, ok := r.recommendations[3]; ok {
		t.Error("accepted rec should have been evicted before opens")
	}
	if _, ok := r.recommendations[4]; ok {
		t.Error("dismissed rec should have been evicted before opens")
	}
	if _, ok := r.recommendations[1]; !ok {
		t.Error("open rec 1 should have survived")
	}
	if _, ok := r.recommendations[2]; !ok {
		t.Error("open rec 2 should have survived")
	}
}

// --- EvictionConfig + sweep ordering ----------------------------------------

func TestLoadEvictionConfig_Defaults(t *testing.T) {
	// Use t.Setenv with empty string to override any leaked values from
	// parallel tests or the environment. getEnvIntOrDefault and
	// getDurationOrDefault both treat "" as unset. t.Setenv restores
	// the original value automatically when the test ends.
	for _, k := range []string{
		"EVICT_INTERVAL", "EVICT_INCIDENT_RESOLVED_TTL", "EVICT_INCIDENT_ACTIVE_TTL",
		"EVICT_INCIDENT_MAX", "EVICT_ERROR_GROUP_TTL", "EVICT_ERROR_GROUP_MAX",
		"EVICT_ANOMALY_STATS_TTL", "EVICT_ANOMALY_STATS_MAX",
		"EVICT_RCA_REPORT_TTL", "EVICT_RCA_REPORT_MAX",
		"EVICT_OPT_REC_TTL", "EVICT_OPT_REC_MAX",
	} {
		t.Setenv(k, "")
	}

	cfg := loadEvictionConfig()
	if cfg.Interval != 5*time.Minute {
		t.Errorf("Interval default: got %v, want 5m", cfg.Interval)
	}
	if cfg.IncidentMax != 500 {
		t.Errorf("IncidentMax default: got %d, want 500", cfg.IncidentMax)
	}
	if cfg.ErrorGroupTTL != 168*time.Hour {
		t.Errorf("ErrorGroupTTL default: got %v, want 168h", cfg.ErrorGroupTTL)
	}
	if cfg.AnomalyStatsMax != 1000 {
		t.Errorf("AnomalyStatsMax default: got %d, want 1000", cfg.AnomalyStatsMax)
	}
	if cfg.OptRecMax != 300 {
		t.Errorf("OptRecMax default: got %d, want 300", cfg.OptRecMax)
	}
}

func TestLoadEvictionConfig_EnvOverrides(t *testing.T) {
	t.Setenv("EVICT_INTERVAL", "10s")
	t.Setenv("EVICT_INCIDENT_MAX", "42")
	cfg := loadEvictionConfig()
	if cfg.Interval != 10*time.Second {
		t.Errorf("env override Interval: got %v, want 10s", cfg.Interval)
	}
	if cfg.IncidentMax != 42 {
		t.Errorf("env override IncidentMax: got %d, want 42", cfg.IncidentMax)
	}
}

func TestRunEvictionSweep_CorrelatorBeforeRCA(t *testing.T) {
	// Regression test: a single sweep must evict the incident AND its
	// orphan RCA report on the same tick. Getting the order wrong means
	// the RCA report survives until the next sweep.
	a := newTestAnalyzer()
	a.correlator = NewCorrelator("test", time.Minute)
	a.rcaEngine = &RCAEngine{
		reports:    make(map[int64]*RCAReport),
		correlator: a.correlator,
	}

	veryOld := time.Now().Add(-100 * time.Hour)
	a.correlator.incidents[1] = &Incident{ID: 1, Status: "resolved", DetectedAt: veryOld, ResolvedAt: &veryOld}
	a.rcaEngine.reports[1] = &RCAReport{IncidentID: 1, CreatedAt: time.Now()}

	cfg := EvictionConfig{
		IncidentResolvedTTL: 24 * time.Hour,
		IncidentActiveTTL:   48 * time.Hour,
		IncidentMax:         500,
		RCAReportTTL:        48 * time.Hour,
		RCAReportMax:        500,
		ErrorGroupTTL:       168 * time.Hour,
		ErrorGroupMax:       200,
		AnomalyStatsTTL:     2 * time.Hour,
		AnomalyStatsMax:     1000,
		OptRecNonOpenTTL:    168 * time.Hour,
		OptRecMax:           300,
	}
	a.runEvictionSweep(cfg)

	if len(a.correlator.incidents) != 0 {
		t.Error("correlator should have evicted the incident")
	}
	if len(a.rcaEngine.reports) != 0 {
		t.Error("RCA report should have been evicted as orphan on the same sweep")
	}
}
