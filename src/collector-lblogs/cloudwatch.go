package main

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/rs/zerolog/log"
)

// CloudWatchSource tails CloudWatch Log Groups for ALB/NLB access logs
// using FilterLogEvents polling. Each log group gets its own goroutine.
type CloudWatchSource struct {
	client    *cloudwatchlogs.Client
	logGroups []string
	interval  time.Duration
	lookback  time.Duration
	clusterID string
	publish   func(LBRequest) // callback: NATS or HTTP push

	mu      sync.Mutex
	cursors map[string]int64 // logGroup → last event timestamp (ms)
}

// NewCloudWatchSource creates a new CloudWatch tailing source.
func NewCloudWatchSource(client *cloudwatchlogs.Client, logGroups []string, interval, lookback time.Duration, clusterID string, publish func(LBRequest)) *CloudWatchSource {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if lookback <= 0 {
		lookback = 5 * time.Minute
	}
	return &CloudWatchSource{
		client:    client,
		logGroups: logGroups,
		interval:  interval,
		lookback:  lookback,
		clusterID: clusterID,
		publish:   publish,
		cursors:   make(map[string]int64),
	}
}

// Start begins tailing all configured log groups. Blocks until ctx is cancelled.
func (cw *CloudWatchSource) Start(ctx context.Context) {
	var wg sync.WaitGroup
	for _, lg := range cw.logGroups {
		wg.Add(1)
		go func(logGroup string) {
			defer wg.Done()
			cw.tailLogGroup(ctx, logGroup)
		}(lg)
	}
	log.Info().
		Int("logGroups", len(cw.logGroups)).
		Dur("interval", cw.interval).
		Msg("CloudWatch tailing started")
	wg.Wait()
}

func (cw *CloudWatchSource) tailLogGroup(ctx context.Context, logGroup string) {
	// Initialize cursor to lookback window
	cw.mu.Lock()
	if _, ok := cw.cursors[logGroup]; !ok {
		cw.cursors[logGroup] = time.Now().Add(-cw.lookback).UnixMilli()
	}
	cw.mu.Unlock()

	ticker := time.NewTicker(cw.interval)
	defer ticker.Stop()

	// First poll immediately
	cw.pollLogGroup(ctx, logGroup)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cw.pollLogGroup(ctx, logGroup)
		}
	}
}

func (cw *CloudWatchSource) pollLogGroup(ctx context.Context, logGroup string) {
	cw.mu.Lock()
	startTime := cw.cursors[logGroup]
	cw.mu.Unlock()

	// Derive LB name and type from log group name
	lbName := inferLBName(logGroup)
	lbType := "alb"

	var totalParsed int
	var maxTS int64

	input := &cloudwatchlogs.FilterLogEventsInput{
		LogGroupName: aws.String(logGroup),
		StartTime:    aws.Int64(startTime + 1), // exclusive of last seen
		Interleaved:  aws.Bool(true),
	}

	paginator := cloudwatchlogs.NewFilterLogEventsPaginator(cw.client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			log.Error().Err(err).Str("logGroup", logGroup).Msg("CloudWatch FilterLogEvents error")
			return
		}

		for _, event := range page.Events {
			if event.Message == nil {
				continue
			}

			msg := aws.ToString(event.Message)
			ts := aws.ToInt64(event.Timestamp)
			if ts > maxTS {
				maxTS = ts
			}

			// Parse ALB access log line
			req, err := ParseALBLine(msg, lbName, cw.clusterID)
			if err != nil || req == nil {
				continue
			}
			req.LBType = lbType
			totalParsed++
			cw.publish(*req)
		}
	}

	// Advance cursor
	if maxTS > 0 {
		cw.mu.Lock()
		if maxTS > cw.cursors[logGroup] {
			cw.cursors[logGroup] = maxTS
		}
		cw.mu.Unlock()
	}

	if totalParsed > 0 {
		log.Debug().
			Str("logGroup", logGroup).
			Int("parsed", totalParsed).
			Msg("CloudWatch poll complete")
	}
}

// inferLBName extracts a short LB name from a CloudWatch log group path.
// e.g., "/aws/elasticloadbalancing/app/my-alb/abc123" → "my-alb"
func inferLBName(logGroup string) string {
	parts := splitPath(logGroup)
	// Try to find a meaningful segment after "app" or "net"
	for i, p := range parts {
		if (p == "app" || p == "net") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	// Fallback: last non-empty segment
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return parts[i]
		}
	}
	return logGroup
}

func splitPath(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			if i > start {
				parts = append(parts, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}
