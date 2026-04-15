package main

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// Phase 2.2 — near-duplicate detection.
//
// The original ERRORS_PLAN proposed embeddings (bge-small) to catch
// pairs that the regex-based fingerprint misses (e.g. "connection
// refused" vs "connect: connection refused"). Real embeddings need
// either a local model file or an API round-trip per ingest.
//
// We achieve the same outcome with **token-set cosine similarity** over
// the templated message — entirely deterministic, no external deps:
//
//   1. Tokenise the message: lowercase → alphanum word split → drop
//      stopwords + numeric tokens.
//   2. Build a TF vector (count per token, then unit-normalised).
//   3. Cosine ≥ threshold (default 0.85) on a candidate pair → near-dup.
//
// What this does NOT do:
//   - Learn semantic relations beyond shared word forms ("error" ↔ "fail"
//     are different tokens). For that you'd need an actual embedding
//     model — keep this hook open via the ErrorAggregator.NearDupScorer
//     field below; production can swap in a real embedder later without
//     changing call sites.
//
// Safety:
//   - DEFAULT OFF. The aggregator's autoMergeNearDups flag must be set
//     explicitly (env: ERRORS_AUTO_MERGE_NEAR_DUPS=true). When off, the
//     scan still runs and emits a `merge_suggestion` entry on the group
//     so an operator can act manually via the merge endpoint.
//   - Threshold tunable via ERRORS_NEAR_DUP_THRESHOLD (0..1).
//   - Bails fast when len(groups) > 200 — quadratic scan dominates.

// NearDupScorer is the pluggable similarity function. cosineTokenSet is
// the default; production can swap in a real embedding-backed function.
type NearDupScorer func(a, b []string) float64

// MergeSuggestion records a candidate near-duplicate found during the
// background scan but NOT auto-merged (autoMergeNearDups=false). The UI
// surfaces these as "Possible duplicate of group #N (score 0.91)" with
// a one-click action that POSTs to /merge-into.
type MergeSuggestion struct {
	TargetID    int64     `json:"targetId"`
	Score       float64   `json:"score"`
	SuggestedAt time.Time `json:"suggestedAt"`
	Reason      string    `json:"reason"`
}

// nearDupConfig is held by ErrorAggregator. Threshold default mirrors the
// ERRORS_PLAN.md spec; autoMerge default false (gated).
type nearDupConfig struct {
	Enabled   bool
	AutoMerge bool
	Threshold float64
	Scorer    NearDupScorer
	scanLimit int // skip scans when len(groups) exceeds this
	lastRun   atomic.Int64
}

func defaultNearDup() *nearDupConfig {
	return &nearDupConfig{
		Enabled:   false, // off until operator explicitly enables
		AutoMerge: false,
		Threshold: 0.85,
		Scorer:    cosineTokenSet,
		scanLimit: 200,
	}
}

// ConfigureNearDup adjusts near-duplicate detection. Pass nil scorer to
// keep the default. Threshold ≤0 keeps the default 0.85.
func (ea *ErrorAggregator) ConfigureNearDup(enabled, autoMerge bool, threshold float64, scorer NearDupScorer) {
	ea.mu.Lock()
	defer ea.mu.Unlock()
	if ea.nearDup == nil {
		ea.nearDup = defaultNearDup()
	}
	ea.nearDup.Enabled = enabled
	ea.nearDup.AutoMerge = autoMerge
	if threshold > 0 && threshold <= 1 {
		ea.nearDup.Threshold = threshold
	}
	if scorer != nil {
		ea.nearDup.Scorer = scorer
	}
}

// Tokens for a group are computed lazily on first use and cached on the
// group itself (Signature field). The caller must hold ea.mu (read OK).
func ensureSignature(g *ErrorGroup) []string {
	if g.Signature != nil {
		return g.Signature
	}
	src := g.SampleMessage
	if src == "" {
		src = g.Title
	}
	g.Signature = tokenize(src)
	return g.Signature
}

// Cached results of the most recent scan, exposed via /errors/near-duplicates.
// Keyed by source group id — the value is the suggested merge target.
type NearDupReport struct {
	GeneratedAt time.Time         `json:"generatedAt"`
	Threshold   float64           `json:"threshold"`
	Suggestions []MergeSuggestion `json:"suggestions"`
}

// ScanNearDuplicates walks the group set once and produces a list of
// suggested merges (or, if autoMerge is on, performs them). Returns the
// suggestion list either way. Safe to call from a periodic timer.
//
// Quadratic in len(groups) — bails when above scanLimit. Caller should
// not call more than once a minute on a busy aggregator.
func (ea *ErrorAggregator) ScanNearDuplicates() *NearDupReport {
	ea.mu.Lock()
	cfg := ea.nearDup
	if cfg == nil {
		cfg = defaultNearDup()
		ea.nearDup = cfg
	}
	if !cfg.Enabled {
		ea.mu.Unlock()
		return &NearDupReport{GeneratedAt: time.Now(), Threshold: cfg.Threshold}
	}
	if len(ea.groups) > cfg.scanLimit {
		log.Debug().Int("groups", len(ea.groups)).Int("limit", cfg.scanLimit).
			Msg("near-dup scan skipped — group count over limit")
		ea.mu.Unlock()
		return &NearDupReport{GeneratedAt: time.Now(), Threshold: cfg.Threshold}
	}

	// Build a stable order so newer groups are compared against older
	// ones (FirstSeen ASC). The "older" group is always the merge target.
	type entry struct {
		g      *ErrorGroup
		fp     string
		tokens []string
	}
	all := make([]entry, 0, len(ea.groups))
	for fp, g := range ea.groups {
		all = append(all, entry{g, fp, ensureSignature(g)})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].g.FirstSeen.Before(all[j].g.FirstSeen) })

	scorer := cfg.Scorer
	threshold := cfg.Threshold
	autoMerge := cfg.AutoMerge

	suggestions := make([]MergeSuggestion, 0)
	for i := 1; i < len(all); i++ {
		newer := all[i]
		if len(newer.tokens) == 0 {
			continue
		}
		bestScore := 0.0
		bestIdx := -1
		for j := 0; j < i; j++ {
			older := all[j]
			if len(older.tokens) == 0 {
				continue
			}
			s := scorer(newer.tokens, older.tokens)
			if s > bestScore {
				bestScore = s
				bestIdx = j
			}
		}
		if bestIdx < 0 || bestScore < threshold {
			continue
		}
		target := all[bestIdx].g
		ts := time.Now()
		newer.g.MergeSuggestion = &MergeSuggestion{
			TargetID:    target.ID,
			Score:       bestScore,
			SuggestedAt: ts,
			Reason:      "token-set cosine ≥ threshold",
		}
		suggestions = append(suggestions, MergeSuggestion{
			TargetID:    target.ID,
			Score:       bestScore,
			SuggestedAt: ts,
			Reason:      "source=" + newer.fp,
		})
	}
	cfg.lastRun.Store(time.Now().Unix())
	ea.mu.Unlock()

	// Auto-merge OUTSIDE the lock — handleMerge takes the lock itself.
	// We perform merges by direct method call so we don't go through HTTP.
	if autoMerge {
		for _, s := range suggestions {
			ea.autoMergeBySuggestion(s)
		}
	}
	return &NearDupReport{
		GeneratedAt: time.Now(),
		Threshold:   threshold,
		Suggestions: suggestions,
	}
}

// autoMergeBySuggestion folds a near-duplicate group into its target.
// Mirrors handleMerge minus the HTTP plumbing. No-op if either side has
// vanished by now.
func (ea *ErrorAggregator) autoMergeBySuggestion(s MergeSuggestion) {
	ea.mu.Lock()
	defer ea.mu.Unlock()

	// Find source by reading the suggestion-bearing group. Linear, but
	// only at most ~200 groups (scanLimit).
	var src, tgt *ErrorGroup
	var srcFP string
	for fp, g := range ea.groups {
		if g.MergeSuggestion != nil && g.MergeSuggestion.TargetID == s.TargetID && g.MergeSuggestion.SuggestedAt.Equal(s.SuggestedAt) {
			src = g
			srcFP = fp
		}
		if g.ID == s.TargetID {
			tgt = g
		}
	}
	if src == nil || tgt == nil {
		return
	}
	tgt.Count += src.Count
	if src.LastSeen.After(tgt.LastSeen) {
		tgt.LastSeen = src.LastSeen
	}
	if src.FirstSeen.Before(tgt.FirstSeen) {
		tgt.FirstSeen = src.FirstSeen
	}
	tgt.MergedFrom = append(tgt.MergedFrom, MergeRef{
		ID:          src.ID,
		Fingerprint: src.Fingerprint,
		Service:     src.Service,
		MergedAt:    time.Now(),
		Count:       src.Count,
	})
	srcOccs := ea.occurrences[srcFP]
	combined := append(ea.occurrences[tgt.Fingerprint], srcOccs...)
	if len(combined) > ea.maxOccur {
		combined = combined[len(combined)-ea.maxOccur:]
	}
	ea.occurrences[tgt.Fingerprint] = combined
	delete(ea.groups, srcFP)
	delete(ea.occurrences, srcFP)
	log.Info().Int64("target", tgt.ID).Float64("score", s.Score).
		Msg("auto-merged near-duplicate group")
}

// ----------------------------------------------------------------------------
// Tokeniser + cosine similarity
// ----------------------------------------------------------------------------

var (
	reSplitToken = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9_]+`)
	stopwords    = map[string]struct{}{
		"the": {}, "a": {}, "an": {}, "is": {}, "are": {}, "was": {}, "were": {},
		"in": {}, "on": {}, "at": {}, "to": {}, "for": {}, "of": {}, "by": {},
		"with": {}, "from": {}, "and": {}, "or": {}, "but": {}, "this": {},
		"that": {}, "it": {}, "its": {}, "be": {}, "been": {}, "have": {},
		"has": {}, "had": {}, "do": {}, "does": {}, "did": {}, "will": {},
		"would": {}, "should": {}, "could": {}, "may": {}, "might": {},
		"can": {}, "cannot": {}, "not": {}, "no": {}, "yes": {},
	}
)

// tokenize lowercases, splits on alphanumeric word boundaries, strips
// short tokens and stopwords. Returns a SORTED slice (so callers can
// hand it to set-cosine without re-sorting).
func tokenize(s string) []string {
	if s == "" {
		return nil
	}
	matches := reSplitToken.FindAllString(strings.ToLower(s), -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		if _, drop := stopwords[m]; drop {
			continue
		}
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// cosineTokenSet computes cosine similarity over count vectors derived
// from sorted token slices. O(|a|+|b|) by sweeping both in lockstep.
func cosineTokenSet(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	freqA := tokenFreq(a)
	freqB := tokenFreq(b)

	var dot, magA, magB float64
	for tok, fa := range freqA {
		magA += fa * fa
		if fb, ok := freqB[tok]; ok {
			dot += fa * fb
		}
	}
	for _, fb := range freqB {
		magB += fb * fb
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}

func tokenFreq(toks []string) map[string]float64 {
	out := make(map[string]float64, len(toks))
	for _, t := range toks {
		out[t]++
	}
	return out
}
