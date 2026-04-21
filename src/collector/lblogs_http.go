//go:build !nolblogs

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// HTTPPusher batches LBRequest events and pushes them to the analyzer's
// /api/v1/lb/ingest endpoint. Used when NATS is unavailable.
type HTTPPusher struct {
	analyzerURL string
	client      *http.Client
	mu          sync.Mutex
	batch       []LBRequest
	batchSize   int
	flushTicker *time.Ticker
}

func NewHTTPPusher(analyzerURL string, batchSize int) *HTTPPusher {
	if batchSize <= 0 {
		batchSize = 50
	}
	return &HTTPPusher{
		analyzerURL: analyzerURL,
		client:      &http.Client{Timeout: 10 * time.Second},
		batchSize:   batchSize,
	}
}

func (p *HTTPPusher) Start(ctx context.Context) func(LBRequest) {
	p.flushTicker = time.NewTicker(5 * time.Second)

	go func() {
		for {
			select {
			case <-ctx.Done():
				p.flush(ctx)
				return
			case <-p.flushTicker.C:
				p.flush(ctx)
			}
		}
	}()

	return func(req LBRequest) {
		p.mu.Lock()
		p.batch = append(p.batch, req)
		shouldFlush := len(p.batch) >= p.batchSize
		p.mu.Unlock()
		if shouldFlush {
			go p.flush(ctx)
		}
	}
}

func (p *HTTPPusher) flush(ctx context.Context) {
	p.mu.Lock()
	if len(p.batch) == 0 {
		p.mu.Unlock()
		return
	}
	batch := p.batch
	p.batch = nil
	p.mu.Unlock()

	body, err := json.Marshal(batch)
	if err != nil {
		log.Error().Err(err).Msg("lblogs: failed to marshal LB request batch")
		return
	}

	url := p.analyzerURL + "/api/v1/lb/ingest"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		log.Error().Err(err).Msg("lblogs: failed to create ingest request")
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		log.Error().Err(err).Int("batch", len(batch)).Msg("lblogs: failed to push batch to analyzer")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Warn().Int("status", resp.StatusCode).Int("batch", len(batch)).Msg("lblogs: analyzer ingest returned non-200")
		return
	}

	var result struct {
		Accepted int `json:"accepted"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	log.Debug().Int("pushed", result.Accepted).Str("target", url).Msg("lblogs: pushed batch to analyzer")
}

func (p *HTTPPusher) Stop() {
	if p.flushTicker != nil {
		p.flushTicker.Stop()
	}
	p.flush(context.Background())
	log.Info().
		Str("url", fmt.Sprintf("%s/api/v1/lb/ingest", p.analyzerURL)).
		Msg("lblogs: HTTP pusher stopped")
}
