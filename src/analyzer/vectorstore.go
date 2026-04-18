package main

// VectorStore persists incident RCA embeddings in Qdrant so the RCA engine
// can find semantically similar past incidents even when exact keyword overlap
// is low (e.g. "connection refused" ~ "connection failed").
//
// It coexists with the keyword-overlap scorer in rca.go:
//   - Vector results are returned first (higher semantic quality).
//   - Keyword results fill the remainder up to maxResults.
//   - If Qdrant is unreachable the method logs and returns an empty slice;
//     the keyword scorer then handles everything — no user-visible impact.
//
// Collection vector dimensions are detected from the first real embedding so
// the store works with any model (nomic-embed-text=768, mxbai=1024, etc.).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const qdrantCollection = "rca_incidents"

// VectorStore is a thin Qdrant REST client.
type VectorStore struct {
	baseURL    string
	embedder   *EmbeddingScorer
	httpClient *http.Client

	// vectorDim is set on first successful upsert / search after collection
	// creation. Protected by mu.
	mu        sync.Mutex
	vectorDim int
}

// VectorResult is returned by VectorStore.Search.
type VectorResult struct {
	IncidentID int64
	Score      float64
	RootCause  string
	Summary    string
}

// NewVectorStore returns a VectorStore, or nil if baseURL is empty.
func NewVectorStore(qdrantURL string, embedder *EmbeddingScorer) *VectorStore {
	if qdrantURL == "" || embedder == nil {
		return nil
	}
	return &VectorStore{
		baseURL:  strings.TrimRight(qdrantURL, "/"),
		embedder: embedder,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Upsert embeds text and stores it as a Qdrant point with the given incidentID.
// Silently skips if the store is nil. Runs best-effort — errors are logged, not returned.
func (v *VectorStore) Upsert(ctx context.Context, incidentID int64, text, rootCause, summary string) {
	if v == nil {
		return
	}
	vec, err := v.embedder.Embed(ctx, text)
	if err != nil {
		log.Warn().Err(err).Int64("incident", incidentID).Msg("vectorstore: embed failed, skipping upsert")
		return
	}

	if err := v.ensureCollection(ctx, len(vec)); err != nil {
		log.Warn().Err(err).Msg("vectorstore: ensure collection failed")
		return
	}

	payload := map[string]any{
		"incidentId": incidentID,
		"rootCause":  rootCause,
		"summary":    summary,
		"upsertedAt": time.Now().UTC().Format(time.RFC3339),
	}
	type point struct {
		ID      uint64         `json:"id"`
		Vector  []float64      `json:"vector"`
		Payload map[string]any `json:"payload"`
	}
	body, _ := json.Marshal(map[string]any{
		"points": []point{{
			ID:      uint64(incidentID), //nolint:gosec // incident IDs are always positive
			Vector:  vec,
			Payload: payload,
		}},
	})

	url := fmt.Sprintf("%s/collections/%s/points", v.baseURL, qdrantCollection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		log.Warn().Err(err).Msg("vectorstore: build upsert request failed")
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		log.Warn().Err(err).Int64("incident", incidentID).Msg("vectorstore: upsert request failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		log.Warn().Int("status", resp.StatusCode).Str("body", string(b)).Msg("vectorstore: upsert non-OK")
	}
}

// Search embeds queryText and returns up to topK similar incidents from Qdrant.
// Returns an empty slice (never nil) on any error so callers can range safely.
func (v *VectorStore) Search(ctx context.Context, queryText string, topK int) []VectorResult {
	if v == nil {
		return nil
	}
	vec, err := v.embedder.Embed(ctx, queryText)
	if err != nil {
		log.Debug().Err(err).Msg("vectorstore: embed for search failed")
		return nil
	}

	type searchReq struct {
		Vector      []float64 `json:"vector"`
		Limit       int       `json:"limit"`
		WithPayload bool      `json:"with_payload"`
	}
	body, _ := json.Marshal(searchReq{Vector: vec, Limit: topK, WithPayload: true})

	url := fmt.Sprintf("%s/collections/%s/points/search", v.baseURL, qdrantCollection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		log.Debug().Err(err).Msg("vectorstore: search request failed")
		return nil
	}
	defer resp.Body.Close()

	var out struct {
		Result []struct {
			ID      uint64  `json:"id"`
			Score   float64 `json:"score"`
			Payload struct {
				IncidentID int64  `json:"incidentId"`
				RootCause  string `json:"rootCause"`
				Summary    string `json:"summary"`
			} `json:"payload"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256*1024)).Decode(&out); err != nil {
		log.Debug().Err(err).Msg("vectorstore: search decode failed")
		return nil
	}

	results := make([]VectorResult, 0, len(out.Result))
	for _, r := range out.Result {
		id := r.Payload.IncidentID
		if id == 0 {
			id = int64(r.ID) //nolint:gosec // Qdrant point IDs mirror incident IDs, always in int64 range
		}
		results = append(results, VectorResult{
			IncidentID: id,
			Score:      r.Score,
			RootCause:  r.Payload.RootCause,
			Summary:    r.Payload.Summary,
		})
	}
	return results
}

// ensureCollection creates the Qdrant collection if it does not exist.
// dim is the embedding vector size (auto-detected from the first real embed).
func (v *VectorStore) ensureCollection(ctx context.Context, dim int) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.vectorDim == dim {
		return nil // already created with this dimension
	}

	// Check if collection already exists.
	url := fmt.Sprintf("%s/collections/%s", v.baseURL, qdrantCollection)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant check collection: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		v.vectorDim = dim
		return nil
	}

	// Create collection.
	body, _ := json.Marshal(map[string]any{
		"vectors": map[string]any{
			"size":     dim,
			"distance": "Cosine",
		},
	})
	req, _ = http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant create collection: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("qdrant create collection HTTP %d: %s", resp.StatusCode, b)
	}

	log.Info().Int("dim", dim).Str("collection", qdrantCollection).Msg("vectorstore: collection created")
	v.vectorDim = dim
	return nil
}
