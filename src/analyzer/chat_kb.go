package main

// chat_kb.go — RAG knowledge base over Qdrant.
//
// Unlike vectorstore.go (which is purpose-built for the `rca_incidents`
// collection and incident payloads), KBStore is a general document store: it
// chunks markdown/runbook text, embeds each chunk with the operator-configured
// embedding model, and upserts into the `hetu_kb` collection. The chat engine
// queries it for grounded context with citations.
//
// Design notes:
//   - Vector dimension is auto-detected from the first real embedding, so it
//     works with any model (nomic-embed-text=768, mxbai=1024, MiniLM=384, …).
//   - Everything is best-effort: if Qdrant or the embedder is unavailable the
//     methods log and return empty results; chat still works (tools-only).
//   - Point IDs are derived deterministically from source+ordinal so re-index
//     overwrites rather than duplicates.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const kbCollection = "hetu_kb"

// KBChunk is a single indexable document fragment.
type KBChunk struct {
	Source  string // e.g. "docs/ARCHITECTURE.md"
	Title   string // document title (first H1 or filename)
	Heading string // nearest section heading
	Text    string // chunk body
}

// KBHit is a retrieval result.
type KBHit struct {
	Score   float64 `json:"score"`
	Source  string  `json:"source"`
	Title   string  `json:"title"`
	Heading string  `json:"heading"`
	Text    string  `json:"text"`
}

// KBStore is a thin, general-purpose Qdrant client for the knowledge base.
type KBStore struct {
	baseURL    string
	embedder   *EmbeddingScorer
	collection string
	httpClient *http.Client

	mu        sync.Mutex
	vectorDim int
	count     int // approximate indexed chunk count (best-effort)
}

// NewKBStore returns a store, or nil if prerequisites are missing.
func NewKBStore(qdrantURL string, embedder *EmbeddingScorer) *KBStore {
	if qdrantURL == "" || embedder == nil {
		return nil
	}
	return &KBStore{
		baseURL:    strings.TrimRight(qdrantURL, "/"),
		embedder:   embedder,
		collection: kbCollection,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// Count returns the approximate number of indexed chunks this process wrote.
func (k *KBStore) Count() int {
	if k == nil {
		return 0
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.count
}

// Search embeds the query and returns up to topK matching chunks.
func (k *KBStore) Search(ctx context.Context, query string, topK int) []KBHit {
	if k == nil || strings.TrimSpace(query) == "" {
		return nil
	}
	vec, err := k.embedder.Embed(ctx, query)
	if err != nil || len(vec) == 0 {
		log.Debug().Err(err).Msg("kb: embed query failed")
		return nil
	}
	body, _ := json.Marshal(map[string]any{
		"vector":       vec,
		"limit":        topK,
		"with_payload": true,
	})
	url := fmt.Sprintf("%s/collections/%s/points/search", k.baseURL, k.collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := k.httpClient.Do(req)
	if err != nil {
		log.Debug().Err(err).Msg("kb: search request failed")
		return nil
	}
	defer resp.Body.Close()

	var out struct {
		Result []struct {
			Score   float64 `json:"score"`
			Payload struct {
				Source  string `json:"source"`
				Title   string `json:"title"`
				Heading string `json:"heading"`
				Text    string `json:"text"`
			} `json:"payload"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil
	}
	hits := make([]KBHit, 0, len(out.Result))
	for _, r := range out.Result {
		hits = append(hits, KBHit{
			Score:   r.Score,
			Source:  r.Payload.Source,
			Title:   r.Payload.Title,
			Heading: r.Payload.Heading,
			Text:    r.Payload.Text,
		})
	}
	return hits
}

// Index embeds and upserts a batch of chunks. Best-effort; logs on failure.
func (k *KBStore) Index(ctx context.Context, chunks []KBChunk) (int, error) {
	if k == nil || len(chunks) == 0 {
		return 0, nil
	}
	type point struct {
		ID      uint64         `json:"id"`
		Vector  []float64      `json:"vector"`
		Payload map[string]any `json:"payload"`
	}
	var points []point
	for _, c := range chunks {
		text := strings.TrimSpace(c.Text)
		if text == "" {
			continue
		}
		vec, err := k.embedder.Embed(ctx, embedInput(c))
		if err != nil || len(vec) == 0 {
			log.Debug().Err(err).Str("source", c.Source).Msg("kb: embed chunk failed")
			continue
		}
		if err := k.ensureCollection(ctx, len(vec)); err != nil {
			return 0, err
		}
		points = append(points, point{
			ID:     chunkID(c),
			Vector: vec,
			Payload: map[string]any{
				"source":    c.Source,
				"title":     c.Title,
				"heading":   c.Heading,
				"text":      text,
				"indexedAt": time.Now().UTC().Format(time.RFC3339),
			},
		})
	}
	if len(points) == 0 {
		return 0, nil
	}

	body, _ := json.Marshal(map[string]any{"points": points})
	url := fmt.Sprintf("%s/collections/%s/points", k.baseURL, k.collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := k.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("kb upsert: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, fmt.Errorf("kb upsert HTTP %d: %s", resp.StatusCode, b)
	}
	k.mu.Lock()
	k.count += len(points)
	k.mu.Unlock()
	return len(points), nil
}

// IndexRepoDocs walks a docs directory and indexes all markdown files. The
// directory is best-effort: when running in-cluster the docs may not be
// mounted, in which case nothing is indexed and chat falls back to tools +
// in-process incident/error context.
func (k *KBStore) IndexRepoDocs(ctx context.Context, root string) (int, error) {
	if k == nil {
		return 0, nil
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		log.Info().Str("dir", root).Msg("kb: docs dir not present, skipping doc indexing")
		return 0, nil
	}
	var chunks []KBChunk
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries
		}
		if d.IsDir() {
			// Skip noisy / non-knowledge directories.
			base := strings.ToLower(d.Name())
			if base == "node_modules" || base == "screenshots" || base == "ui-mockups" ||
				base == "presentation" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		content, rerr := os.ReadFile(path) //nolint:gosec // operator-provided docs dir
		if rerr != nil {
			return nil
		}
		rel := path
		if r, e := filepath.Rel(filepath.Dir(root), path); e == nil {
			rel = r
		}
		chunks = append(chunks, chunkMarkdown(rel, string(content))...)
		return nil
	})
	if walkErr != nil {
		return 0, walkErr
	}

	total := 0
	// Index in batches to bound memory and request size.
	const batch = 32
	for i := 0; i < len(chunks); i += batch {
		end := i + batch
		if end > len(chunks) {
			end = len(chunks)
		}
		n, err := k.Index(ctx, chunks[i:end])
		if err != nil {
			log.Warn().Err(err).Msg("kb: batch index failed")
			break
		}
		total += n
	}
	log.Info().Int("chunks", total).Str("dir", root).Msg("kb: repo docs indexed")
	return total, nil
}

// ensureCollection creates the hetu_kb collection if it doesn't exist.
func (k *KBStore) ensureCollection(ctx context.Context, dim int) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.vectorDim == dim {
		return nil
	}
	url := fmt.Sprintf("%s/collections/%s", k.baseURL, k.collection)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := k.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("kb check collection: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		k.vectorDim = dim
		return nil
	}
	body, _ := json.Marshal(map[string]any{
		"vectors": map[string]any{"size": dim, "distance": "Cosine"},
	})
	req, _ = http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = k.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("kb create collection: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("kb create collection HTTP %d: %s", resp.StatusCode, b)
	}
	log.Info().Int("dim", dim).Str("collection", k.collection).Msg("kb: collection created")
	k.vectorDim = dim
	return nil
}

// embedInput prefixes the chunk text with its title/heading so the embedding
// captures section context, improving retrieval for short chunks.
func embedInput(c KBChunk) string {
	var b strings.Builder
	if c.Title != "" {
		b.WriteString(c.Title)
		b.WriteString("\n")
	}
	if c.Heading != "" && c.Heading != c.Title {
		b.WriteString(c.Heading)
		b.WriteString("\n")
	}
	b.WriteString(c.Text)
	return b.String()
}

// chunkID derives a stable uint64 point id from source + heading + a prefix of
// the text, so re-indexing the same content overwrites rather than duplicates.
func chunkID(c KBChunk) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(c.Source))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(c.Heading))
	_, _ = h.Write([]byte{0})
	t := c.Text
	if len(t) > 64 {
		t = t[:64]
	}
	_, _ = h.Write([]byte(t))
	return h.Sum64()
}

// chunkMarkdown splits a markdown document into heading-scoped chunks of a
// bounded size. It is intentionally simple (no full markdown AST): it tracks
// the current heading and flushes when a chunk grows past ~1200 chars or a new
// heading starts.
func chunkMarkdown(source, content string) []KBChunk {
	const maxChars = 1200
	lines := strings.Split(content, "\n")

	title := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	heading := ""
	var buf strings.Builder
	var chunks []KBChunk

	flush := func() {
		text := strings.TrimSpace(buf.String())
		buf.Reset()
		if len(text) < 24 { // skip trivially small fragments
			return
		}
		chunks = append(chunks, KBChunk{Source: source, Title: title, Heading: heading, Text: text})
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && title == strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)) {
			title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
		if strings.HasPrefix(trimmed, "#") {
			// New section — flush the previous one.
			flush()
			heading = strings.TrimSpace(strings.TrimLeft(trimmed, "# "))
			continue
		}
		buf.WriteString(line)
		buf.WriteString("\n")
		if buf.Len() >= maxChars {
			flush()
		}
	}
	flush()
	return chunks
}
