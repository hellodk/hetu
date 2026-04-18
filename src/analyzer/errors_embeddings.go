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
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// EmbeddingAPI describes how the scorer talks to the embedding backend.
// Two shapes today — OpenAI-compatible (`POST /embeddings` with
// {input, model}, response {data:[{embedding}]}) and Ollama native
// (`POST /api/embeddings` with {prompt, model}, response {embedding}).
type EmbeddingAPI string

const (
	EmbeddingAPIOpenAI EmbeddingAPI = "openai"
	EmbeddingAPIOllama EmbeddingAPI = "ollama"
)

// EmbeddingScorer is a pluggable NearDupScorer backed by a real
// embedding model served over HTTP.
type EmbeddingScorer struct {
	endpoint string
	model    string
	apiKey   string
	api      EmbeddingAPI
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
// `{endpoint}/embeddings` (OpenAI-compatible). A nil http.Client
// defaults to a 15-second timeout client. apiKey may be empty for
// unauthenticated local servers.
//
// Audit v2 #2 — for Ollama's native /api/embeddings schema use
// NewEmbeddingScorerForAPI(..., EmbeddingAPIOllama). Auto-detection
// heuristic in NewEmbeddingScorerAuto picks the right shape from the
// endpoint URL.
func NewEmbeddingScorer(endpoint, model, apiKey string, client *http.Client) *EmbeddingScorer {
	return NewEmbeddingScorerForAPI(endpoint, model, apiKey, EmbeddingAPIOpenAI, client)
}

// NewEmbeddingScorerForAPI explicitly selects the request/response shape.
func NewEmbeddingScorerForAPI(endpoint, model, apiKey string, api EmbeddingAPI, client *http.Client) *EmbeddingScorer {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if api == "" {
		api = EmbeddingAPIOpenAI
	}
	return &EmbeddingScorer{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		apiKey:   apiKey,
		api:      api,
		client:   client,
		timeout:  15 * time.Second,
		cache:    map[string][]float64{},
		fallback: cosineTokenSet,
	}
}

// NewEmbeddingScorerAuto picks the API shape from the endpoint URL:
//   - "…/v1"     → OpenAI-compatible (most remote providers; Ollama's
//     OpenAI adapter when operators prefer that path)
//   - anything else → Ollama native (covers bare "http://ollama:11434")
//
// Operators who want a specific shape should call NewEmbeddingScorerForAPI.
func NewEmbeddingScorerAuto(endpoint, model, apiKey string, client *http.Client) *EmbeddingScorer {
	api := EmbeddingAPIOllama
	trimmed := strings.TrimRight(endpoint, "/")
	if strings.HasSuffix(trimmed, "/v1") || strings.Contains(trimmed, "openai.com") ||
		strings.Contains(trimmed, "azure.com") {
		api = EmbeddingAPIOpenAI
	}
	return NewEmbeddingScorerForAPI(endpoint, model, apiKey, api, client)
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

// fetchEmbedding posts to the configured embeddings endpoint and
// returns the first vector from the response. Audit v2 #2 — now
// speaks both OpenAI-compatible and Ollama native schemas.
func (s *EmbeddingScorer) fetchEmbedding(ctx context.Context, text string) ([]float64, error) {
	var path string
	var buf []byte
	var err error
	switch s.api {
	case EmbeddingAPIOllama:
		path = "/api/embeddings"
		type reqBody struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
		}
		buf, err = json.Marshal(reqBody{Model: s.model, Prompt: text})
	default: // OpenAI-compatible
		path = "/embeddings"
		type reqBody struct {
			Input string `json:"input"`
			Model string `json:"model"`
		}
		buf, err = json.Marshal(reqBody{Input: text, Model: s.model})
	}
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.endpoint+path, bytes.NewReader(buf))
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

	if s.api == EmbeddingAPIOllama {
		var out struct {
			Embedding []float64 `json:"embedding"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, fmt.Errorf("decode ollama embeddings response: %w", err)
		}
		if len(out.Embedding) == 0 {
			return nil, fmt.Errorf("ollama embeddings response empty")
		}
		return out.Embedding, nil
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

// Embed returns the embedding vector for arbitrary text. Unlike Score, it
// does not use the token-cache key (which assumes sorted token slices) — it
// hashes the raw text so full sentences are cached correctly.
// Used by VectorStore to embed incident text for Qdrant upsert / search.
func (s *EmbeddingScorer) Embed(ctx context.Context, text string) ([]float64, error) {
	h := sha1.New()
	h.Write([]byte(text))
	key := hex.EncodeToString(h.Sum(nil))

	s.mu.Lock()
	if v, ok := s.cache[key]; ok {
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()

	v, err := s.fetchEmbedding(ctx, text)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[key] = v
	s.mu.Unlock()
	return v, nil
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

// configureNearDupFromEnv reads ERRORS_NEARDUP_* + ERRORS_EMBEDDING_*
// env vars and activates near-dup detection on the aggregator. No-op
// when ERRORS_NEARDUP_MODE is unset or "off".
//
// Separated from NewEmbeddingScorer so unit tests can construct a
// scorer with a mock client without going through env vars.
func configureNearDupFromEnv(ea *ErrorAggregator) {
	mode := strings.TrimSpace(os.Getenv("ERRORS_NEARDUP_MODE"))
	if mode == "" || strings.EqualFold(mode, "off") {
		return
	}
	var m NearDupMode
	switch strings.ToLower(mode) {
	case "shadow":
		m = NearDupShadow
	case "auto":
		m = NearDupAuto
	default:
		log.Warn().Str("ERRORS_NEARDUP_MODE", mode).
			Msg("near-dup: unknown mode (want off|shadow|auto); staying off")
		return
	}

	threshold := 0.85
	if v := os.Getenv("ERRORS_NEARDUP_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 1 {
			threshold = f
		}
	}

	endpoint := os.Getenv("ERRORS_EMBEDDING_ENDPOINT")
	model := os.Getenv("ERRORS_EMBEDDING_MODEL")
	if endpoint == "" || model == "" {
		// No embedding config — keep default token-set scorer.
		ea.ConfigureNearDupMode(m, threshold, nil)
		log.Info().Str("mode", string(m)).Float64("threshold", threshold).
			Msg("near-dup: activated with token-set cosine scorer (no embedding config)")
		return
	}

	apiKey := os.Getenv("ERRORS_EMBEDDING_API_KEY")
	apiChoice := strings.ToLower(os.Getenv("ERRORS_EMBEDDING_API"))
	var scorer *EmbeddingScorer
	switch apiChoice {
	case "openai":
		scorer = NewEmbeddingScorerForAPI(endpoint, model, apiKey, EmbeddingAPIOpenAI, nil)
	case "ollama":
		scorer = NewEmbeddingScorerForAPI(endpoint, model, apiKey, EmbeddingAPIOllama, nil)
	default: // "auto" or unset
		scorer = NewEmbeddingScorerAuto(endpoint, model, apiKey, nil)
	}
	ea.AttachEmbeddingScorer(m, threshold, scorer)
	log.Info().
		Str("mode", string(m)).
		Float64("threshold", threshold).
		Str("endpoint", endpoint).
		Str("model", model).
		Str("api", string(scorer.api)).
		Msg("near-dup: activated with embedding-backed scorer")
}
