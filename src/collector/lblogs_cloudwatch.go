//go:build !nolblogs

package main

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/rs/zerolog/log"
)

// CloudWatchSource tails CloudWatch Log Groups for ALB/NLB access logs.
type CloudWatchSource struct {
	client    *cloudwatchlogs.Client
	logGroups []string
	interval  time.Duration
	lookback  time.Duration
	clusterID string
	publish   func(LBRequest)

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
		Msg("lblogs: CloudWatch tailing started")
	wg.Wait()
}

func (cw *CloudWatchSource) tailLogGroup(ctx context.Context, logGroup string) {
	cw.mu.Lock()
	if _, ok := cw.cursors[logGroup]; !ok {
		cw.cursors[logGroup] = time.Now().Add(-cw.lookback).UnixMilli()
	}
	cw.mu.Unlock()

	ticker := time.NewTicker(cw.interval)
	defer ticker.Stop()

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

	lbName := inferLBName(logGroup)

	var totalParsed int
	var maxTS int64

	input := &cloudwatchlogs.FilterLogEventsInput{
		LogGroupName: aws.String(logGroup),
		StartTime:    aws.Int64(startTime + 1),
		Interleaved:  aws.Bool(true),
	}

	paginator := cloudwatchlogs.NewFilterLogEventsPaginator(cw.client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			log.Error().Err(err).Str("logGroup", logGroup).Msg("lblogs: CloudWatch FilterLogEvents error")
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

			req, err := ParseALBLine(msg, lbName, cw.clusterID)
			if err != nil || req == nil {
				continue
			}
			req.LBType = "alb"
			totalParsed++
			cw.publish(*req)
		}
	}

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
			Msg("lblogs: CloudWatch poll complete")
	}
}

// inferLBName extracts a short LB name from a CloudWatch log group path.
// e.g., "/aws/elasticloadbalancing/app/my-alb/abc123" → "my-alb"
func inferLBName(logGroup string) string {
	parts := splitLBPath(logGroup)
	for i, p := range parts {
		if (p == "app" || p == "net") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return parts[i]
		}
	}
	return logGroup
}

func splitLBPath(s string) []string {
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

// LogGroupInfo describes a discovered CloudWatch Log Group.
type LogGroupInfo struct {
	Name          string    `json:"name"`
	ARN           string    `json:"arn,omitempty"`
	RetentionDays int32     `json:"retentionDays,omitempty"`
	StoredBytes   int64     `json:"storedBytes"`
	LastEventAt   time.Time `json:"lastEventAt,omitempty"`
}

// DiscoverLogGroups finds CloudWatch Log Groups matching the given prefix.
func DiscoverLogGroups(ctx context.Context, client *cloudwatchlogs.Client, prefix string) ([]LogGroupInfo, error) {
	input := &cloudwatchlogs.DescribeLogGroupsInput{}
	if prefix != "" {
		input.LogGroupNamePrefix = aws.String(prefix)
	}

	var results []LogGroupInfo

	paginator := cloudwatchlogs.NewDescribeLogGroupsPaginator(client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, lg := range page.LogGroups {
			info := LogGroupInfo{
				Name:        aws.ToString(lg.LogGroupName),
				ARN:         aws.ToString(lg.Arn),
				StoredBytes: aws.ToInt64(lg.StoredBytes),
			}
			if lg.RetentionInDays != nil {
				info.RetentionDays = *lg.RetentionInDays
			}
			if lg.CreationTime != nil {
				info.LastEventAt = time.UnixMilli(*lg.CreationTime)
			}
			results = append(results, info)
		}
	}

	return results, nil
}
