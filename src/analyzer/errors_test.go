package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ----------------------------------------------------------------------------
// Phase 2.1 — faultKey
// ----------------------------------------------------------------------------

func TestFaultKey_StableAcrossServices(t *testing.T) {
	stack := "FooException: bad thing\n  at com.example.Foo.handle(Foo.java:42)\n  at com.example.Bar.run(Bar.java:99)"
	a := faultKey(stack, "FooException: bad thing", "exception")
	b := faultKey(stack, "FooException: bad thing", "exception")
	if a != b || a == "" {
		t.Fatalf("expected stable non-empty key, got %q vs %q", a, b)
	}
}

func TestFaultKey_ChangesWithDifferentRoot(t *testing.T) {
	s1 := "TimeoutException: db\n  at db/conn.go:42"
	s2 := "PanicException: nil deref\n  at app/main.go:99"
	a := faultKey(s1, "TimeoutException: db", "timeout")
	b := faultKey(s2, "PanicException: nil deref", "panic")
	if a == b {
		t.Fatalf("expected different keys for different roots, both %q", a)
	}
}

func TestFaultKey_IgnoresLineNumbers(t *testing.T) {
	a := faultKey("FooError: x\n  at app.go:10", "FooError: x", "exception")
	b := faultKey("FooError: x\n  at app.go:200", "FooError: x", "exception")
	if a != b {
		t.Fatalf("expected normalisation across line numbers, got %q vs %q", a, b)
	}
}

// ----------------------------------------------------------------------------
// Phase 2.4 — scored exemplar (only-better wins)
// ----------------------------------------------------------------------------

func TestScoreExemplar_StackBeatsNoStack(t *testing.T) {
	short := ErrorOccurrence{Message: "x"}
	long := ErrorOccurrence{Message: strings.Repeat("y", 250)}
	if scoreExemplar(short, true) <= scoreExemplar(long, false) {
		t.Fatalf("expected stack to beat 250-char no-stack")
	}
}

func TestExemplar_DoesNotRegress(t *testing.T) {
	// A high-quality first event should NOT be replaced by a low-quality
	// second event (the legacy bug: "first 3 messages win" let bad samples
	// dominate; the new bug to avoid is the inverse).
	ea := NewErrorAggregator()
	good := IngestEvent{
		Service: "svc", Namespace: "ns", Level: "error",
		Reason: "exception", Fingerprint: "fp1",
		Message:    strings.Repeat("informative-context ", 20),
		StackTrace: "FooException: x\n  at app.go:10",
		Error:      "FooException: x", URL: "/v1/api", RequestID: "req-1",
	}
	weak := IngestEvent{
		Service: "svc", Namespace: "ns", Level: "error",
		Reason: "exception", Fingerprint: "fp1",
		Message: "x",
	}
	ea.Ingest(good)
	keptGood := ea.groups["fp1"].SampleMessage
	for i := 0; i < 5; i++ {
		ea.Ingest(weak)
	}
	if ea.groups["fp1"].SampleMessage != keptGood {
		t.Fatalf("good exemplar got replaced by weaker one: %q", ea.groups["fp1"].SampleMessage)
	}
}

// ----------------------------------------------------------------------------
// Phase 3.3 — signal-based confidence
// ----------------------------------------------------------------------------

func TestComputeConfidence_AllSignals(t *testing.T) {
	c := computeConfidence(true, true, true, 1.0)
	if c < 0.99 || c > 1.0 {
		t.Fatalf("expected ≈1.0 with all signals, got %v", c)
	}
}

func TestComputeConfidence_NoSignals(t *testing.T) {
	c := computeConfidence(false, false, false, 0)
	if c != 0 {
		t.Fatalf("expected 0 with no signals, got %v", c)
	}
}

func TestComputeConfidence_StructuralAlone(t *testing.T) {
	// Without any LLM input, structural signals alone should give a
	// usable but sub-1.0 confidence.
	c := computeConfidence(true, true, true, 0)
	if c <= 0.4 || c >= 0.6 {
		t.Fatalf("expected ~0.5 with all structural / no LLM, got %v", c)
	}
}

func TestComputeConfidence_ClampsLLM(t *testing.T) {
	c := computeConfidence(false, false, false, 5.0)
	if c > 0.5+1e-6 {
		t.Fatalf("LLM term should clamp to 1 → c≤0.5, got %v", c)
	}
}

// ----------------------------------------------------------------------------
// Phase 1.1 — rate aggregates
// ----------------------------------------------------------------------------

func TestComputeRate_BucketsByAge(t *testing.T) {
	ea := NewErrorAggregator()
	now := time.Now()
	// inject occurrences directly so we control the timestamps
	ea.occurrences["fp"] = []ErrorOccurrence{
		{Timestamp: now.Add(-10 * time.Second)}, // last 1m + 5m + 1h + 24h
		{Timestamp: now.Add(-3 * time.Minute)},  // 5m + 1h + 24h
		{Timestamp: now.Add(-30 * time.Minute)}, // 1h + 24h
		{Timestamp: now.Add(-3 * time.Hour)},    // 24h only
		{Timestamp: now.Add(-25 * time.Hour)},   // outside everything
	}
	r := ea.computeRate("fp")
	if r.Count1m != 1 || r.Count5m != 2 || r.Count1h != 3 || r.Count24h != 4 {
		t.Fatalf("buckets wrong: 1m=%d 5m=%d 1h=%d 24h=%d", r.Count1m, r.Count5m, r.Count1h, r.Count24h)
	}
	if len(r.Spark) != 12 {
		t.Fatalf("expected 12 spark buckets, got %d", len(r.Spark))
	}
}

// ----------------------------------------------------------------------------
// Phase 1.3 — pagination + sort
// ----------------------------------------------------------------------------

// ----------------------------------------------------------------------------
// Phase 1.4 — detail-page filters
// ----------------------------------------------------------------------------

func TestHandleGetGroup_OccurrenceFilters(t *testing.T) {
	ea := NewErrorAggregator()
	now := time.Now()
	ea.groups["fp"] = &ErrorGroup{ID: 1, Fingerprint: "fp", Service: "s", Namespace: "n"}
	ea.occurrences["fp"] = []ErrorOccurrence{
		{Timestamp: now.Add(-10 * time.Minute), Pod: "p-a", Message: "alpha bug"},
		{Timestamp: now.Add(-5 * time.Minute), Pod: "p-b", Message: "beta bug"},
		{Timestamp: now.Add(-2 * time.Minute), Pod: "p-a", Message: "gamma issue"},
	}
	mux := http.NewServeMux()
	ea.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Filter by pod
	resp, err := http.Get(srv.URL + "/api/v1/errors/groups/1?pod=p-a")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Occurrences   []ErrorOccurrence `json:"occurrences"`
		FilteredCount int               `json:"filteredCount"`
		TotalCount    int               `json:"totalCount"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if body.FilteredCount != 2 || body.TotalCount != 3 {
		t.Fatalf("pod filter: filtered=%d total=%d, want 2 of 3", body.FilteredCount, body.TotalCount)
	}

	// Filter by search term
	resp, err = http.Get(srv.URL + "/api/v1/errors/groups/1?search=beta")
	if err != nil {
		t.Fatal(err)
	}
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if body.FilteredCount != 1 {
		t.Fatalf("search filter: got %d, want 1", body.FilteredCount)
	}
}

// ----------------------------------------------------------------------------
// Phase 2.1 — fault rollup
// ----------------------------------------------------------------------------

func TestFaultRollup_GroupsBySharedKey(t *testing.T) {
	ea := NewErrorAggregator()
	stack := "FooException: x\n  at app.go:10\n  at lib.go:20\n  at db.go:30"
	// 3 services, same fault
	for _, svc := range []string{"svc-a", "svc-b", "svc-c"} {
		ea.Ingest(IngestEvent{
			Service: svc, Namespace: "ns", Level: "error", Reason: "exception",
			Fingerprint: "fp-" + svc,
			Message:     "FooException: x", StackTrace: stack, Error: "FooException: x",
		})
	}
	// One unrelated fault
	ea.Ingest(IngestEvent{
		Service: "svc-z", Namespace: "ns", Level: "error", Reason: "timeout",
		Fingerprint: "fp-z", Message: "timeout", StackTrace: "TimeoutErr: nope\n  at db.go:5",
		Error: "TimeoutErr: nope",
	})

	mux := http.NewServeMux()
	ea.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/errors/faults")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		TotalFaults int `json:"totalFaults"`
		Faults      []struct {
			GroupCount int      `json:"groupCount"`
			Services   []string `json:"services"`
		} `json:"faults"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.TotalFaults != 2 {
		t.Fatalf("expected 2 distinct faults, got %d", body.TotalFaults)
	}
	// The 3-service one should be first (sorted by total count desc)
	if body.Faults[0].GroupCount != 3 || len(body.Faults[0].Services) != 3 {
		t.Fatalf("expected top fault to span 3 services; got %d groups / %v services",
			body.Faults[0].GroupCount, body.Faults[0].Services)
	}
}

// ----------------------------------------------------------------------------
// Phase 2.3 — manual merge / split
// ----------------------------------------------------------------------------

func TestMergeAndSplit_RoundTrip(t *testing.T) {
	ea := NewErrorAggregator()
	a := IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex", Fingerprint: "fp-a", Message: "a"}
	b := IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex", Fingerprint: "fp-b", Message: "b"}
	for i := 0; i < 5; i++ {
		ea.Ingest(a)
	}
	for i := 0; i < 3; i++ {
		ea.Ingest(b)
	}
	src := ea.groups["fp-a"]
	tgt := ea.groups["fp-b"]
	if src == nil || tgt == nil {
		t.Fatal("setup failed")
	}
	srcID := src.ID
	tgtID := tgt.ID
	srcCount := src.Count

	mux := http.NewServeMux()
	ea.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Merge a into b
	mergeURL := srv.URL + "/api/v1/errors/groups/" + strconv.FormatInt(srcID, 10) +
		"/merge-into/" + strconv.FormatInt(tgtID, 10)
	resp, err := http.Post(mergeURL, "application/json", nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("merge failed: err=%v status=%d", err, resp.StatusCode)
	}
	resp.Body.Close()

	// Source is gone; target has +5 count and a MergedFrom entry
	if _, ok := ea.groups["fp-a"]; ok {
		t.Fatal("source group should be deleted after merge")
	}
	merged := ea.groups["fp-b"]
	if merged.Count != 8 {
		t.Fatalf("merged count want 8, got %d", merged.Count)
	}
	if len(merged.MergedFrom) != 1 || merged.MergedFrom[0].ID != srcID {
		t.Fatalf("MergedFrom: %+v", merged.MergedFrom)
	}

	// Split it back out
	splitURL := srv.URL + "/api/v1/errors/groups/" + strconv.FormatInt(tgtID, 10) + "/split"
	resp, err = http.Post(splitURL, "application/json",
		strings.NewReader(`{"mergedFromId":`+strconv.FormatInt(srcID, 10)+`}`))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("split failed: err=%v status=%d", err, resp.StatusCode)
	}
	resp.Body.Close()
	// Target should be back to 3, MergedFrom empty, source revived
	tgtAfter := ea.groups["fp-b"]
	if tgtAfter.Count != 3 || len(tgtAfter.MergedFrom) != 0 {
		t.Fatalf("after split, target count=%d MergedFrom=%v", tgtAfter.Count, tgtAfter.MergedFrom)
	}
	if revived, ok := ea.groups["fp-a"]; !ok || revived.Count != srcCount {
		t.Fatalf("revived group missing or wrong count: %+v", revived)
	}
}

// ----------------------------------------------------------------------------
// Phase 1.5 — context panel (with nil correlator/optimizer for unit isolation)
// ----------------------------------------------------------------------------

func TestGroupContext_NilDepsReturnsEmptyArrays(t *testing.T) {
	ea := NewErrorAggregator()
	ea.Ingest(IngestEvent{
		Service: "svc-a", Namespace: "ns", Level: "error", Reason: "ex",
		Fingerprint: "fp-1", Message: "x",
		StackTrace: "FooException: x\n  at app.go:10",
		Error:      "FooException: x",
	})
	ea.Ingest(IngestEvent{
		Service: "svc-b", Namespace: "ns", Level: "error", Reason: "ex",
		Fingerprint: "fp-2", Message: "x",
		StackTrace: "FooException: x\n  at app.go:10",
		Error:      "FooException: x",
	})
	id1 := ea.groups["fp-1"].ID

	mux := http.NewServeMux()
	ea.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/errors/groups/" + strconv.FormatInt(id1, 10) + "/context")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Incidents       []any `json:"incidents"`
		Recommendations []any `json:"recommendations"`
		Siblings        []struct {
			Service string `json:"service"`
		} `json:"siblings"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Incidents) != 0 || len(body.Recommendations) != 0 {
		t.Fatalf("with nil correlator/opt, incidents+recs should be empty")
	}
	if len(body.Siblings) != 1 || body.Siblings[0].Service != "svc-b" {
		t.Fatalf("expected exactly 1 sibling (svc-b), got %+v", body.Siblings)
	}
}

// ----------------------------------------------------------------------------
// Audit #6 — evict metric advances on TTL + cap eviction
// ----------------------------------------------------------------------------

func TestEvictMetric_RecordsTTLAndCap(t *testing.T) {
	ea := NewErrorAggregator()
	reg := prometheus.NewRegistry()
	ea.AttachMetrics(reg, "test")

	// Seed 5 groups
	for i := 0; i < 5; i++ {
		ea.Ingest(IngestEvent{
			Service: "s", Namespace: "n", Level: "error", Reason: "ex",
			Fingerprint: "fp" + string(rune('a'+i)),
			Message:     "x",
		})
	}

	// Force 3 of them to look stale (older than TTL).
	ea.mu.Lock()
	staleTS := time.Now().Add(-24 * time.Hour)
	for i, fp := range []string{"fpa", "fpb", "fpc"} {
		_ = i
		ea.groups[fp].LastSeen = staleTS
	}
	ea.mu.Unlock()

	removed := ea.Evict(1*time.Hour, 0)
	if removed != 3 {
		t.Fatalf("expected 3 evictions, got %d", removed)
	}

	// Walk the registry for our cluster_intel_errors_evict_total counter.
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var ttlCount float64
	for _, mf := range mfs {
		if mf.GetName() != "test_errors_evict_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "reason" && l.GetValue() == "ttl" {
					ttlCount = m.GetCounter().GetValue()
				}
			}
		}
	}
	if ttlCount != 3 {
		t.Fatalf("evict_total{reason=ttl} = %v, want 3", ttlCount)
	}

	// Now trigger cap-eviction: drop maxSize to 1, which forces 1 more eviction.
	removed = ea.Evict(24*time.Hour, 1)
	if removed != 1 {
		t.Fatalf("expected 1 cap eviction, got %d", removed)
	}
	mfs, _ = reg.Gather()
	var capCount float64
	for _, mf := range mfs {
		if mf.GetName() != "test_errors_evict_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "reason" && l.GetValue() == "cap" {
					capCount = m.GetCounter().GetValue()
				}
			}
		}
	}
	if capCount != 1 {
		t.Fatalf("evict_total{reason=cap} = %v, want 1", capCount)
	}
}

// ----------------------------------------------------------------------------
// Audit #4 — rate-spike + umbrella-fault scanner
// ----------------------------------------------------------------------------

func TestScanTriggers_SpikeFires(t *testing.T) {
	ea := NewErrorAggregator()
	fired := make(chan string, 8)
	ea.AttachAnalyzer(func(g *ErrorGroup, trigger string) { fired <- trigger })

	// Seed a group (this fires "newGroup" — we drain below) then backdate
	// 5 recent events so count5m=5, count1h=5, projected=60, 60 > 2*5 ⇒ spike.
	now := time.Now()
	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "fp", Message: "x"})
	ea.mu.Lock()
	ea.occurrences["fp"] = []ErrorOccurrence{
		{Timestamp: now.Add(-3 * time.Minute)},
		{Timestamp: now.Add(-2 * time.Minute)},
		{Timestamp: now.Add(-1 * time.Minute)},
		{Timestamp: now.Add(-30 * time.Second)},
		{Timestamp: now.Add(-10 * time.Second)},
	}
	// Clear the per-fp throttle so the scan is allowed to fire again.
	delete(ea.lastTriggered, "fp")
	ea.mu.Unlock()

	// Drain the newGroup trigger from Ingest.
	select {
	case <-fired:
	case <-time.After(200 * time.Millisecond):
	}

	spikes, umbrellas := ea.ScanTriggers()
	if spikes != 1 || umbrellas != 0 {
		t.Fatalf("ScanTriggers returned spikes=%d umbrellas=%d, want 1/0", spikes, umbrellas)
	}
	select {
	case trig := <-fired:
		if trig != "rateSpike" {
			t.Fatalf("got trigger %q, want rateSpike", trig)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("rateSpike hook was never called")
	}
}

func TestScanTriggers_UmbrellaFiresOnLargest(t *testing.T) {
	ea := NewErrorAggregator()
	type rec struct {
		trigger string
		fp      string
	}
	fired := make(chan rec, 8)
	ea.AttachAnalyzer(func(g *ErrorGroup, trigger string) {
		fired <- rec{trigger, g.Fingerprint}
	})

	// 3 groups, same faultKey (same stack/exception, different services).
	// Count is set directly so we don't trip the rate-spike check (which
	// would fire first and throttle the umbrella). The scenario we're
	// testing is umbrella-by-group-count, not spike.
	stack := "FooException: x\n  at app.go:10\n  at lib.go:20\n  at db.go:30"
	for _, svc := range []string{"svc-a", "svc-b", "svc-c"} {
		ea.Ingest(IngestEvent{
			Service: svc, Namespace: "n", Level: "error", Reason: "ex",
			Fingerprint: "fp-" + svc, Message: "x",
			StackTrace: stack, Error: "FooException: x",
		})
	}
	// Set Count directly, with occurrence timestamps spread far enough back
	// that count5m=0 and the rate-spike pass skips every group.
	ea.mu.Lock()
	ea.groups["fp-svc-a"].Count = 1
	ea.groups["fp-svc-b"].Count = 2
	ea.groups["fp-svc-c"].Count = 3 // largest
	past := time.Now().Add(-2 * time.Hour)
	ea.occurrences["fp-svc-a"] = []ErrorOccurrence{{Timestamp: past}}
	ea.occurrences["fp-svc-b"] = []ErrorOccurrence{{Timestamp: past}}
	ea.occurrences["fp-svc-c"] = []ErrorOccurrence{{Timestamp: past}}
	for k := range ea.lastTriggered {
		delete(ea.lastTriggered, k)
	}
	ea.mu.Unlock()

	// Drain newGroup triggers from Ingest.
	time.Sleep(50 * time.Millisecond)
	for {
		select {
		case <-fired:
			continue
		default:
		}
		break
	}

	_, umbrellas := ea.ScanTriggers()
	if umbrellas != 1 {
		t.Fatalf("umbrellas=%d, want 1", umbrellas)
	}

	// Wait for the goroutine and collect every event fired.
	time.Sleep(150 * time.Millisecond)
	events := []rec{}
drain:
	for {
		select {
		case r := <-fired:
			events = append(events, r)
		default:
			break drain
		}
	}
	found := false
	for _, e := range events {
		if e.trigger == "umbrellaFault" && e.fp == "fp-svc-c" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("umbrellaFault did not fire on fp-svc-c; saw: %+v", events)
	}
}

// ----------------------------------------------------------------------------
// Audit #5 — RecsForTarget matching order
// ----------------------------------------------------------------------------

func TestRecsForTarget_MatchOrderAndPrecedence(t *testing.T) {
	reg := &OptimizerRegistry{
		optimizers:      map[string]Optimizer{},
		recommendations: map[int64]*OptRecommendation{},
	}
	add := func(name, container string) *OptRecommendation {
		id := int64(len(reg.recommendations) + 1)
		rec := &OptRecommendation{
			ID: id, Status: "open",
			Target: OptTarget{Namespace: "ns", Name: name, Container: container},
		}
		reg.recommendations[id] = rec
		return rec
	}
	exactRec := add("payments", "")
	containerRec := add("api-deployment", "payments")
	prefixRec := add("payments-worker", "")
	unrelated := add("web-ui", "")
	// A legit prefix-on-different-word should NOT match "api": "apiserver" shares
	// prefix "api" but we required the "-" so it's excluded (the audit fix).
	apiserverRec := add("apiserver", "")
	_ = apiserverRec
	_ = unrelated

	got := reg.RecsForTarget("ns", "payments")
	if len(got) != 3 {
		t.Fatalf("want 3 matching recs, got %d", len(got))
	}
	// Exact must come first, container second, prefix third.
	if got[0].ID != exactRec.ID || got[1].ID != containerRec.ID || got[2].ID != prefixRec.ID {
		t.Fatalf("match order wrong: got [%d, %d, %d], want [%d, %d, %d]",
			got[0].ID, got[1].ID, got[2].ID, exactRec.ID, containerRec.ID, prefixRec.ID)
	}

	// Audit fix: "api" must NOT match "apiserver"
	gotAPI := reg.RecsForTarget("ns", "api")
	for _, r := range gotAPI {
		if r.ID == apiserverRec.ID {
			t.Fatalf("bare-prefix bug still present — 'api' matched 'apiserver'")
		}
	}
}

// ----------------------------------------------------------------------------
// Audit #1 — embedding NearDupScorer
// ----------------------------------------------------------------------------

// fakeEmbeddingServer returns a deterministic vector for each input
// token set: one dimension per distinct vocabulary word in a fixed map.
// That lets the test design "semantically similar" inputs (same words,
// different wording) and verify they score closer than unrelated ones.
func fakeEmbeddingServer(t *testing.T, calls *int) *httptest.Server {
	t.Helper()
	// Vocabulary chosen so synonyms share most dimensions.
	vocab := map[string]int{
		"connection": 0, "refused": 1, "failed": 1, // synonym: same axis
		"upstream":   2,
		"database":   3,
		"permission": 4,
		"denied":     5,
		"access":     5, // synonym
		"filesystem": 6,
		"path":       7,
	}
	const dim = 8
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			*calls++
		}
		var req struct {
			Input string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		vec := make([]float64, dim)
		for _, tok := range strings.Fields(strings.ToLower(req.Input)) {
			if d, ok := vocab[tok]; ok {
				vec[d]++
			}
		}
		resp := map[string]any{
			"data": []map[string]any{{"embedding": vec}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestEmbeddingScorer_SynonymsScoreHigherThanUnrelated(t *testing.T) {
	var calls int
	srv := fakeEmbeddingServer(t, &calls)
	defer srv.Close()

	s := NewEmbeddingScorer(srv.URL, "test", "", nil)
	a := tokenize("connection refused upstream database")
	b := tokenize("connection failed upstream database") // one synonym swap
	c := tokenize("permission denied filesystem path")   // unrelated

	sim := s.Score(a, b)
	unrel := s.Score(a, c)
	if !(sim > unrel) {
		t.Fatalf("synonym pair should score higher than unrelated: sim=%v unrel=%v", sim, unrel)
	}
	if sim < 0.9 {
		// With vocab designed so "refused" and "failed" share a dimension,
		// a/b should score very close to 1.0.
		t.Fatalf("expected sim ≥ 0.9 for synonym pair, got %v", sim)
	}
}

func TestEmbeddingScorer_CachesPerTokenSet(t *testing.T) {
	var calls int
	srv := fakeEmbeddingServer(t, &calls)
	defer srv.Close()

	s := NewEmbeddingScorer(srv.URL, "test", "", nil)
	a := tokenize("connection refused upstream")
	b := tokenize("connection refused upstream") // same tokens

	_ = s.Score(a, b)
	callsAfterFirst := calls
	_ = s.Score(a, b)
	if calls != callsAfterFirst {
		t.Fatalf("second Score with same tokens should hit cache; calls went %d → %d", callsAfterFirst, calls)
	}
	if s.CacheSize() != 1 {
		t.Fatalf("cacheSize=%d, want 1 (identical inputs)", s.CacheSize())
	}
}

func TestEmbeddingScorer_EvictDropsStale(t *testing.T) {
	var calls int
	srv := fakeEmbeddingServer(t, &calls)
	defer srv.Close()

	s := NewEmbeddingScorer(srv.URL, "test", "", nil)
	// Seed 3 cache entries.
	s.Score(tokenize("aaa"), tokenize("aaa"))
	s.Score(tokenize("bbb"), tokenize("bbb"))
	s.Score(tokenize("ccc"), tokenize("ccc"))
	if s.CacheSize() != 3 {
		t.Fatalf("setup: cache size = %d, want 3", s.CacheSize())
	}
	// Keep only "aaa"
	keep := map[string]struct{}{
		cacheKey(tokenize("aaa")): {},
	}
	removed := s.Evict(keep)
	if removed != 2 || s.CacheSize() != 1 {
		t.Fatalf("Evict removed %d / remaining %d; want 2 removed / 1 remaining", removed, s.CacheSize())
	}
}

func TestNearDup_ScanCallsScorerEvict(t *testing.T) {
	// Wire an aggregator with a scorer whose Evict we can observe.
	ea := NewErrorAggregator()
	var evictCalled int
	var evictLastKeep int
	ea.mu.Lock()
	ea.nearDup = &nearDupConfig{
		Mode:      NearDupShadow,
		Threshold: 0.5,
		Scorer:    cosineTokenSet,
		ScorerEvict: func(keep map[string]struct{}) int {
			evictCalled++
			evictLastKeep = len(keep)
			return 0
		},
		scanLimit: 200,
	}
	ea.mu.Unlock()

	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "a", Message: "connection refused"})
	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "b", Message: "disk full"})

	_ = ea.ScanNearDuplicates()
	if evictCalled != 1 {
		t.Fatalf("ScorerEvict should be called once per scan; got %d", evictCalled)
	}
	if evictLastKeep != 2 {
		t.Fatalf("keep set should have 2 entries (one per live group); got %d", evictLastKeep)
	}
}

func TestEmbeddingScorer_FallsBackOnHTTPError(t *testing.T) {
	// Point at an unreachable server so the HTTP call fails fast.
	s := NewEmbeddingScorer("http://127.0.0.1:1/bad", "test", "", &http.Client{
		Timeout: 200 * time.Millisecond,
	})
	a := tokenize("connection refused upstream database")
	b := tokenize("connection refused upstream database")
	got := s.Score(a, b)
	// Identical inputs via cosineTokenSet fallback = 1.0.
	if got < 0.99 {
		t.Fatalf("expected fallback to give identical-inputs score ≈ 1, got %v", got)
	}
}

// ----------------------------------------------------------------------------
// Audit v2 #1 — spike hysteresis
// ----------------------------------------------------------------------------

func TestScanTriggers_HysteresisSkipsStableSpike(t *testing.T) {
	ea := NewErrorAggregator()
	fired := make(chan string, 16)
	ea.AttachAnalyzer(func(g *ErrorGroup, trigger string) { fired <- trigger })

	// Seed the group + a spike-level rate (5 recent events).
	now := time.Now()
	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "fp", Message: "x"})
	ea.mu.Lock()
	ea.occurrences["fp"] = []ErrorOccurrence{
		{Timestamp: now.Add(-3 * time.Minute)},
		{Timestamp: now.Add(-2 * time.Minute)},
		{Timestamp: now.Add(-90 * time.Second)},
		{Timestamp: now.Add(-1 * time.Minute)},
		{Timestamp: now.Add(-30 * time.Second)},
	}
	// Clear new-group breadcrumb + per-fp throttle so the FIRST scan fires.
	delete(ea.lastTriggered, "fp")
	ea.mu.Unlock()

	// Drain newGroup.
	time.Sleep(20 * time.Millisecond)
	for len(fired) > 0 {
		<-fired
	}

	// First scan fires the spike.
	spikes, _ := ea.ScanTriggers()
	if spikes != 1 {
		t.Fatalf("first scan spikes=%d, want 1", spikes)
	}
	// Drain the goroutine's output.
	time.Sleep(30 * time.Millisecond)
	for len(fired) > 0 {
		<-fired
	}

	// Clear the per-fp throttle (simulating 10 min later) but KEEP the
	// spike state. The rate hasn't grown, so hysteresis should skip.
	ea.mu.Lock()
	delete(ea.lastTriggered, "fp")
	ea.mu.Unlock()
	spikes2, _ := ea.ScanTriggers()
	if spikes2 != 0 {
		t.Fatalf("stable spike should be skipped by hysteresis; got spikes=%d", spikes2)
	}
}

// ----------------------------------------------------------------------------
// Audit v2 #2 — Ollama native /api/embeddings shape
// ----------------------------------------------------------------------------

// fakeOllamaEmbeddingServer mimics Ollama's /api/embeddings response.
func fakeOllamaEmbeddingServer(t *testing.T, path *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if path != nil {
			*path = r.URL.Path
		}
		var req struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		vec := []float64{1.0, 0.0, 0.0}
		if strings.Contains(req.Prompt, "refused") {
			vec = []float64{1.0, 1.0, 0.0}
		}
		json.NewEncoder(w).Encode(map[string]any{"embedding": vec})
	}))
}

func TestEmbeddingScorer_OllamaNativeSchema(t *testing.T) {
	var path string
	srv := fakeOllamaEmbeddingServer(t, &path)
	defer srv.Close()

	s := NewEmbeddingScorerForAPI(srv.URL, "nomic-embed-text", "", EmbeddingAPIOllama, nil)
	a := tokenize("connection refused upstream")
	_, _ = s.vectorFor(a) // force a round-trip
	if path != "/api/embeddings" {
		t.Fatalf("Ollama mode hit wrong path: %q, want /api/embeddings", path)
	}
}

func TestNewEmbeddingScorerAuto_DetectsAPIShape(t *testing.T) {
	openai := NewEmbeddingScorerAuto("https://api.openai.com/v1", "m", "k", nil)
	if openai.api != EmbeddingAPIOpenAI {
		t.Fatalf("openai URL → api=%q, want openai", openai.api)
	}
	ollama := NewEmbeddingScorerAuto("http://ollama:11434", "m", "", nil)
	if ollama.api != EmbeddingAPIOllama {
		t.Fatalf("bare Ollama URL → api=%q, want ollama", ollama.api)
	}
	v1 := NewEmbeddingScorerAuto("http://ollama:11434/v1", "m", "", nil)
	if v1.api != EmbeddingAPIOpenAI {
		t.Fatalf("/v1 URL → api=%q, want openai", v1.api)
	}
}

// ----------------------------------------------------------------------------
// Audit v2 #3 — review stats / accept / reject
// ----------------------------------------------------------------------------

func TestReviewAPI_AcceptRejectAndStats(t *testing.T) {
	ea := NewErrorAggregator()
	ea.ConfigureNearDupMode(NearDupShadow, 0.5, nil)

	// Two near-dup pairs so we have two suggestions to act on.
	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "a1", Message: "connection refused upstream database"})
	time.Sleep(2 * time.Millisecond)
	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "a2", Message: "connect connection refused upstream database server"})
	time.Sleep(2 * time.Millisecond)
	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "b1", Message: "disk full path"})
	time.Sleep(2 * time.Millisecond)
	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "b2", Message: "disk full path quota"})

	ea.ScanNearDuplicates()

	mux := http.NewServeMux()
	ea.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Find the two groups that have pending suggestions.
	var pending []*ErrorGroup
	ea.mu.RLock()
	for _, g := range ea.groups {
		if g.MergeSuggestion != nil {
			pending = append(pending, g)
		}
	}
	ea.mu.RUnlock()
	if len(pending) < 2 {
		t.Fatalf("setup: expected ≥2 pending suggestions, got %d", len(pending))
	}

	// Accept the first, reject the second.
	postNoBody := func(url string) int {
		resp, err := http.Post(url, "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	acceptURL := srv.URL + "/api/v1/errors/near-duplicates/" + strconv.FormatInt(pending[0].ID, 10) + "/accept"
	rejectURL := srv.URL + "/api/v1/errors/near-duplicates/" + strconv.FormatInt(pending[1].ID, 10) + "/reject"
	if c := postNoBody(acceptURL); c != 200 {
		t.Fatalf("accept → HTTP %d", c)
	}
	if c := postNoBody(rejectURL); c != 200 {
		t.Fatalf("reject → HTTP %d", c)
	}

	// Stats: one accept and one reject should show up somewhere.
	resp, err := http.Get(srv.URL + "/api/v1/errors/near-duplicates/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Bands []struct {
			Band     string `json:"band"`
			Accepted int    `json:"accepted"`
			Rejected int    `json:"rejected"`
		} `json:"bands"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	var totalA, totalR int
	for _, b := range body.Bands {
		totalA += b.Accepted
		totalR += b.Rejected
	}
	if totalA != 1 || totalR != 1 {
		t.Fatalf("stats totals: accepted=%d rejected=%d, want 1/1", totalA, totalR)
	}

	// The accepted source group must be gone (merged), the rejected one
	// must still exist but without the suggestion.
	if _, ok := ea.groups[pending[0].Fingerprint]; ok {
		t.Fatalf("accepted group should have been merged away")
	}
	if g, ok := ea.groups[pending[1].Fingerprint]; !ok || g.MergeSuggestion != nil {
		t.Fatalf("rejected group must remain with MergeSuggestion cleared; got %+v", g)
	}
}

// ----------------------------------------------------------------------------
// Phase 2.2 — near-duplicate detection
// ----------------------------------------------------------------------------

func TestCosineTokenSet_IdenticalIs1(t *testing.T) {
	a := tokenize("connection refused to upstream database")
	if c := cosineTokenSet(a, a); c < 0.999 {
		t.Fatalf("identical → cos %v, want ≈1", c)
	}
}

func TestCosineTokenSet_NearDuplicate(t *testing.T) {
	// Same root cause, slight wording variant — the regex-based
	// fingerprint would treat these as different but our scanner
	// should catch them.
	a := tokenize("connection refused to upstream database server")
	b := tokenize("connect: connection refused to upstream database server")
	c := cosineTokenSet(a, b)
	if c < 0.85 {
		t.Fatalf("expected near-duplicate ≥ 0.85, got %v", c)
	}
}

func TestCosineTokenSet_UnrelatedIsLow(t *testing.T) {
	a := tokenize("connection refused database")
	b := tokenize("permission denied filesystem")
	c := cosineTokenSet(a, b)
	if c >= 0.5 {
		t.Fatalf("unrelated messages should score < 0.5, got %v", c)
	}
}

// Audit #3 — shadow mode: scan runs, suggestions recorded, nothing fused.
func TestScanNearDuplicates_ShadowModeNoFusion(t *testing.T) {
	ea := NewErrorAggregator()
	ea.ConfigureNearDupMode(NearDupShadow, 0.5, nil)
	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "older", Message: "connection refused upstream database"})
	time.Sleep(2 * time.Millisecond)
	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "newer", Message: "connect connection refused upstream database server"})

	report := ea.ScanNearDuplicates()
	if len(ea.groups) != 2 {
		t.Fatalf("shadow mode must NOT fuse groups; have %d, want 2", len(ea.groups))
	}
	if len(report.Suggestions) == 0 {
		t.Fatal("shadow mode must still record suggestions")
	}
	newer := ea.groups["newer"]
	if newer.MergeSuggestion == nil {
		t.Fatal("newer group missing MergeSuggestion breadcrumb")
	}
	if !strings.HasPrefix(newer.MergeSuggestion.Reason, "SHADOW ") {
		t.Fatalf("shadow reason not annotated: %q", newer.MergeSuggestion.Reason)
	}
}

func TestScanNearDuplicates_OffByDefault(t *testing.T) {
	ea := NewErrorAggregator()
	for i, msg := range []string{"connection refused database", "connection refused upstream"} {
		ea.Ingest(IngestEvent{
			Service: "s", Namespace: "n", Level: "error", Reason: "ex",
			Fingerprint: "fp" + string(rune('a'+i)),
			Message:     msg,
		})
	}
	report := ea.ScanNearDuplicates()
	if len(report.Suggestions) != 0 {
		t.Fatalf("default should be off; got %d suggestions", len(report.Suggestions))
	}
}

func TestScanNearDuplicates_FindsCandidates(t *testing.T) {
	ea := NewErrorAggregator()
	ea.ConfigureNearDup(true, false, 0.5, nil)
	// Two near-duplicates + one unrelated
	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "older", Message: "connection refused upstream database"})
	time.Sleep(2 * time.Millisecond) // ensure newer FirstSeen
	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "newer", Message: "connect connection refused upstream database server"})
	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "unrelated", Message: "permission denied filesystem path"})

	report := ea.ScanNearDuplicates()
	if len(report.Suggestions) == 0 {
		t.Fatalf("expected ≥1 near-dup suggestion, got 0")
	}
	// The "newer" group should have a MergeSuggestion pointing at "older"
	newer := ea.groups["newer"]
	if newer.MergeSuggestion == nil {
		t.Fatalf("newer group missing MergeSuggestion")
	}
	older := ea.groups["older"]
	if newer.MergeSuggestion.TargetID != older.ID {
		t.Fatalf("suggestion should target older.ID=%d, got %d", older.ID, newer.MergeSuggestion.TargetID)
	}
}

func TestScanNearDuplicates_AutoMergeFolds(t *testing.T) {
	ea := NewErrorAggregator()
	ea.ConfigureNearDup(true, true, 0.5, nil) // autoMerge ON
	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "older", Message: "connection refused upstream database"})
	time.Sleep(2 * time.Millisecond)
	ea.Ingest(IngestEvent{Service: "s", Namespace: "n", Level: "error", Reason: "ex",
		Fingerprint: "newer", Message: "connect connection refused upstream database server"})

	if len(ea.groups) != 2 {
		t.Fatalf("setup: want 2 groups, have %d", len(ea.groups))
	}
	ea.ScanNearDuplicates()
	if len(ea.groups) != 1 {
		t.Fatalf("autoMerge should have folded; %d groups remain", len(ea.groups))
	}
	tgt := ea.groups["older"]
	if len(tgt.MergedFrom) != 1 {
		t.Fatalf("target should have a MergedFrom entry; got %v", tgt.MergedFrom)
	}
}

func TestHandleListGroups_PaginationAndSort(t *testing.T) {
	ea := NewErrorAggregator()
	for i := 0; i < 5; i++ {
		ea.Ingest(IngestEvent{
			Service: "s", Namespace: "n", Level: "error", Reason: "exception",
			Fingerprint: "fp" + string(rune('a'+i)),
			Message:     "msg",
		})
	}
	// Bump count on fp0 so we can verify count-sort
	for i := 0; i < 10; i++ {
		ea.Ingest(IngestEvent{
			Service: "s", Namespace: "n", Level: "error", Reason: "exception",
			Fingerprint: "fpa", Message: "again",
		})
	}

	mux := http.NewServeMux()
	ea.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// limit=2, sort=count → fpa should be first, total still 5
	resp, err := http.Get(srv.URL + "/api/v1/errors/groups?limit=2&offset=0&sort=count")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		TotalCount int           `json:"totalCount"`
		Groups     []*ErrorGroup `json:"groups"`
		Limit      int           `json:"limit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.TotalCount != 5 {
		t.Fatalf("totalCount=%d, want 5", body.TotalCount)
	}
	if len(body.Groups) != 2 {
		t.Fatalf("got %d groups, expected limit=2", len(body.Groups))
	}
	if body.Groups[0].Fingerprint != "fpa" {
		t.Fatalf("count-sort failed: top fp=%q want fpa", body.Groups[0].Fingerprint)
	}
}
