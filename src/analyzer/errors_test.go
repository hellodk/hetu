package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
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
