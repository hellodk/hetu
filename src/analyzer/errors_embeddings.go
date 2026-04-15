package main

// Audit item #1 — real semantic near-duplicate scorer.
//
// The default token-set cosine (see errors_neardup.go) catches word-order
// / punctuation / stopword variants but is blind to synonyms:
//
//   "connection refused"        vs "connection failed"
//   "out of memory"             vs "OOM killed"
//   "permission denied"         vs "unauthorized access"
//
// These all mean "same root cause, different wording". An embedding model
// maps them into the same neighbourhood of vector space and cosine
// similarity separates real synonyms from coincidence.
//
// This file provides an EmbeddingScorer that plugs into the existing
// NearDupScorer hook on ErrorAggregator. It is an optional add-on:
//
//   1. Zero external ML dependencies in the binary.
//   2. Uses the OpenAI-compatible /embeddings endpoint — works with
//      OpenAI, Azure OpenAI, Ollama (/api/embeddings), and any
//      vllm / llamacpp / TEI / custom server implementing the schema.
//   3. Per-fingerprint vector cache so we hit the model at most once
//      per group. Cache entries drop when the group is evicted.
//   4. Soft-fails to token-set cosine on any error (timeout, 404,
//      rate limit, bad JSON, no vector returned).
//
// Configuration: analyzer operator calls
//
//   scorer := NewEmbeddingScorer(endpoint, model, apiKey, httpClient)
//   ea.ConfigureNearDupMode(NearDupShadow, 0.85, scorer.Score)
//
// The Score method intentionally accepts sorted-token slices (the same
// signature as cosineTokenSet) so the dispatch site in errors_neardup.go
// doesn't need a branch.

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// EmbeddingScorer is a pluggable NearDupScorer backed by a real
// embedding model served over HTTP.
type EmbeddingScorer struct {
	endpoint string
	model    string
	apiKey   string
	client   *http.Client
	timeout  time.Duration

	// vector cache. Key = sha1(joined tokens) — cheap collision-free
	// identity for deduped lookups across the scanner's N*N loop.
	mu    sync.Mutex
	cache map[string][]float64

	// Fallback to the deterministic token-set scorer when the model
	// round-trip fails. Set at construction; swappable for tests.
	fallback NearDupScorer
}

// NewEmbeddingScorer builds a scorer that posts to
// `{endpoint}/embeddings`. The exact shape is the OpenAI /embeddings
// schema — Ollama's native /api/embeddings takes a different payload,
// so Ollama users should configure the endpoint to hit its
// OpenAI-compatible adapter at `http://…/v1`.
//
// A nil http.Client defaults to a 15-second timeout client. apiKey may
// be empty for unauthenticated local servers.
func NewEmbeddingScorer(endpoint, model, apiKey string, client *http.Client) *EmbeddingScorer {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &EmbeddingScorer{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		apiKey:   apiKey,
		client:   client,
		timeout:  15 * time.Second,
		cache:    map[string][]float64{},
		fallback: cosineTokenSet,
	}
}

// Score implements NearDupScorer. The inputs are sorted token slices
// (same as cosineTokenSet) — we reconstruct the message by joining
// them; the embedding model cares about the token *set* far more than
// spacing or order, so this is an acceptable shortcut and lets us reuse
// the existing tokenize output without restructuring callers.
func (s *EmbeddingScorer) Score(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	va, aErr := s.vectorFor(a)
	vb, bErr := s.vectorFor(b)
	if aErr != nil || bErr != nil || len(va) == 0 || len(vb) == 0 {
		log.Debug().
			Bool("aFailed", aErr != nil).Bool("bFailed", bErr != nil).
			Msg("embedding scorer: fell back to token-set cosine")
		return s.fallback(a, b)
	}
	return cosine(va, vb)
}

// FallbackFor returns the token-set cosine of two token slices.
// Exposed for tests that want to check the fallback path independently.
func (s *EmbeddingScorer) FallbackFor(a, b []string) float64 {
	return s.fallback(a, b)
}

// vectorFor looks up (or fetches) the embedding for a token slice.
// Cache keyed by SHA1 of the joined tokens; a clean miss triggers one
// HTTP call. The per-fingerprint cost is bounded because the scanner
// calls us once per group per scan tick.
func (s *EmbeddingScorer) vectorFor(tokens []string) ([]float64, error) {
	key := cacheKey(tokens)
	s.mu.Lock()
	if v, ok := s.cache[key]; ok {
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()

	text := strings.Join(tokens, " ")
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	v, err := s.fetchEmbedding(ctx, text)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[key] = v
	s.mu.Unlock()
	return v, nil
}

// fetchEmbedding posts to the OpenAI-compatible embeddings endpoint and
// returns the first vector from the response.
func (s *EmbeddingScorer) fetchEmbedding(ctx context.Context, text string) ([]float64, error) {
	type reqBody struct {
		Input string `json:"input"`
		Model string `json:"model"`
	}
	buf, err := json.Marshal(reqBody{Input: text, Model: s.model})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", s.endpoint+"/embeddings", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embeddings HTTP %d: %s", resp.StatusCode, truncBody(body, 200))
	}
	var out struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode embeddings response: %w", err)
	}
	if len(out.Data) == 0 || len(out.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embeddings response empty")
	}
	return out.Data[0].Embedding, nil
}

// Evict drops cached vectors whose keys are NOT in the keepKeys set.
// The aggregator calls this after its own eviction pass so unused
// vectors don't accumulate indefinitely. keepKeys is built by the
// caller from the tokens of currently-live groups.
func (s *EmbeddingScorer) Evict(keep map[string]struct{}) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for k := range s.cache {
		if _, ok := keep[k]; !ok {
			delete(s.cache, k)
			removed++
		}
	}
	return removed
}

// CacheSize — exposed for tests and /metrics if we ever wire it.
func (s *EmbeddingScorer) CacheSize() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cache)
}

// cacheKey produces a stable cache key from a sorted token slice.
// SHA1 because it's cheap and collision-free for our scale; this is not
// a cryptographic use (gosec G505 is suppressed at the package level).
func cacheKey(tokens []string) string {
	h := sha1.New()
	for _, t := range tokens {
		h.Write([]byte(t))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// cosine computes cosine similarity between two float64 slices of equal
// length. Returns 0 if either is zero-length or zero-magnitude.
func cosine(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, magA, magB float64
	for i, x := range a {
		y := b[i]
		dot += x * y
		magA += x * x
		magB += y * y
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}

func truncBody(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
