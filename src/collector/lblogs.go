//go:build !nolblogs

package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/rs/zerolog/log"

	"github.com/your-org/cluster-intel/pkg/bus"
	ucconfig "github.com/your-org/cluster-intel/pkg/config"
)

// LBConfig describes a single load balancer log source (S3).
type LBConfig struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"` // alb | nlb | elb
	Bucket   string `yaml:"bucket"`
	Prefix   string `yaml:"prefix"`
	Region   string `yaml:"region"`
	PollSecs int    `yaml:"pollIntervalSeconds"`
}

// LBRequest is a parsed load balancer access log row.
type LBRequest struct {
	Timestamp            time.Time `json:"ts"`
	Cluster              string    `json:"cluster"`
	LBName               string    `json:"lbName"`
	LBType               string    `json:"lbType"`
	TargetGroup          string    `json:"targetGroup,omitempty"`
	URLPattern           string    `json:"urlPattern"`
	HTTPMethod           string    `json:"httpMethod"`
	ELBStatus            int       `json:"elbStatus"`
	TargetStatus         int       `json:"targetStatus"`
	ClientIP             string    `json:"clientIp"`
	RequestProcessingMs  float64   `json:"requestProcessingMs"`
	TargetProcessingMs   float64   `json:"targetProcessingMs"`
	ResponseProcessingMs float64   `json:"responseProcessingMs"`
	TraceID              string    `json:"traceId,omitempty"`
	UserAgent            string    `json:"userAgent,omitempty"`
	RequestURL           string    `json:"requestUrl,omitempty"`
}

// LBSpikeEvent is published to NATS when a traffic anomaly is detected.
type LBSpikeEvent struct {
	LBName      string    `json:"lbName"`
	TargetGroup string    `json:"targetGroup"`
	URLPattern  string    `json:"urlPattern,omitempty"`
	MetricName  string    `json:"metric"`
	Current     float64   `json:"current"`
	Baseline    float64   `json:"baseline"`
	Timestamp   time.Time `json:"ts"`
}

// LBStats holds aggregated stats for a time window.
type LBStats struct {
	LBName        string  `json:"lbName"`
	LBType        string  `json:"lbType"`
	TotalRequests int64   `json:"totalRequests"`
	Count5xx      int64   `json:"count5xx"`
	Count4xx      int64   `json:"count4xx"`
	Count2xx      int64   `json:"count2xx"`
	P50Ms         float64 `json:"p50Ms"`
	P95Ms         float64 `json:"p95Ms"`
	P99Ms         float64 `json:"p99Ms"`
	AvgMs         float64 `json:"avgMs"`
}

// URLStats holds per-URL-pattern stats.
type URLStats struct {
	URLPattern string  `json:"urlPattern"`
	HTTPMethod string  `json:"httpMethod"`
	TotalCount int64   `json:"totalCount"`
	Count5xx   int64   `json:"count5xx"`
	Count4xx   int64   `json:"count4xx"`
	P95Ms      float64 `json:"p95Ms"`
	P99Ms      float64 `json:"p99Ms"`
}

// startLBLogs is the self-contained LB log subsystem coordinator.
// It reads env vars for AWS/delivery configuration, starts S3 and CloudWatch
// sources, and blocks until ctx is cancelled.
func startLBLogs(ctx context.Context, cfg Config) {
	ucfg, _ := ucconfig.LoadFromEnv("/etc/cluster-intel/config.yaml")

	deliveryMode := cfg.LBDeliveryMode
	if deliveryMode == "" {
		deliveryMode = "nats"
	}

	var publishFn func(LBRequest)
	var httpPusher *HTTPPusher

	switch deliveryMode {
	case "http":
		analyzerURL := cfg.AnalyzerURL
		if analyzerURL == "" {
			analyzerURL = "http://cluster-intel-analyzer:8081"
		}
		httpPusher = NewHTTPPusher(analyzerURL, 50)
		publishFn = httpPusher.Start(ctx)
		log.Info().Str("target", analyzerURL).Msg("lblogs: HTTP delivery mode")
	default:
		var eventBus *bus.Bus
		if ucfg.Bus.NATS.Enabled {
			var err error
			eventBus, err = bus.Connect(ctx, ucfg.Bus.NATS)
			if err != nil {
				log.Warn().Err(err).Msg("lblogs: NATS unavailable — events will be dropped")
			} else {
				defer eventBus.Close()
			}
		}
		if eventBus != nil {
			publishFn = func(req LBRequest) {
				data, _ := json.Marshal(req)
				if err := eventBus.Publish(ctx, "lb.request", data); err != nil {
					log.Debug().Err(err).Msg("lblogs: failed to publish to NATS")
				}
			}
			log.Info().Msg("lblogs: NATS delivery mode")
		} else {
			log.Warn().Msg("lblogs: no delivery target; events will be dropped")
			publishFn = func(LBRequest) {}
		}
	}

	var wg sync.WaitGroup

	// --- CloudWatch source ---
	if cfg.CWLogGroups != "" {
		groups := strings.Split(cfg.CWLogGroups, ",")
		for i := range groups {
			groups[i] = strings.TrimSpace(groups[i])
		}

		opts := []func(*awsconfig.LoadOptions) error{}
		if cfg.AWSRegion != "" {
			opts = append(opts, awsconfig.WithRegion(cfg.AWSRegion))
		}
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			log.Error().Err(err).Msg("lblogs: failed to load AWS config for CloudWatch")
		} else {
			cwClient := cloudwatchlogs.NewFromConfig(awsCfg)
			pollInterval := cfg.CWPollInterval
			if pollInterval == 0 {
				pollInterval = 10 * time.Second
			}
			lookback := cfg.CWLookback
			if lookback == 0 {
				lookback = 5 * time.Minute
			}
			cwSource := NewCloudWatchSource(cwClient, groups, pollInterval, lookback, cfg.ClusterID, publishFn)
			wg.Add(1)
			go func() {
				defer wg.Done()
				cwSource.Start(ctx)
			}()
		}
	}

	// --- S3 source ---
	lbConfigs := loadLBConfigs(cfg.LBConfigs)
	if len(lbConfigs) > 0 {
		processed := &processedTracker{keys: make(map[string]bool)}
		for _, lbc := range lbConfigs {
			wg.Add(1)
			go func(lb LBConfig) {
				defer wg.Done()
				pollLoop(ctx, lb, cfg.ClusterID, publishFn, processed)
			}(lbc)
		}
		log.Info().Int("lbs", len(lbConfigs)).Msg("lblogs: S3 polling started")
	}

	if cfg.CWLogGroups == "" && len(lbConfigs) == 0 {
		log.Warn().Msg("lblogs: no sources configured (set CW_LOG_GROUPS or LB_CONFIGS)")
	}

	log.Info().Str("delivery", deliveryMode).Msg("LB log subsystem started")
	wg.Wait()

	if httpPusher != nil {
		httpPusher.Stop()
	}
}

func loadLBConfigs(raw string) []LBConfig {
	if raw == "" {
		return nil
	}
	var configs []LBConfig
	if err := json.Unmarshal([]byte(raw), &configs); err != nil {
		log.Error().Err(err).Msg("lblogs: failed to parse LB_CONFIGS")
		return nil
	}
	return configs
}

// processedTracker keeps track of S3 keys already processed (in-memory).
type processedTracker struct {
	mu   sync.Mutex
	keys map[string]bool
}

func (pt *processedTracker) isProcessed(bucket, key string) bool {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.keys[bucket+"/"+key]
}

func (pt *processedTracker) markProcessed(bucket, key string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.keys[bucket+"/"+key] = true
}

func pollLoop(ctx context.Context, lb LBConfig, clusterID string, publishFn func(LBRequest), pt *processedTracker) {
	interval := time.Duration(lb.PollSecs) * time.Second
	if interval == 0 {
		interval = 60 * time.Second
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(lb.Region))
	if err != nil {
		log.Error().Err(err).Str("lb", lb.Name).Msg("lblogs: failed to load AWS config")
		return
	}
	s3Client := s3.NewFromConfig(awsCfg)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pollS3(ctx, s3Client, lb, clusterID, publishFn, pt)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pollS3(ctx, s3Client, lb, clusterID, publishFn, pt)
		}
	}
}

func pollS3(ctx context.Context, client *s3.Client, lb LBConfig, clusterID string, publishFn func(LBRequest), pt *processedTracker) {
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(lb.Bucket),
		Prefix: aws.String(lb.Prefix),
	})

	var newKeys []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			log.Error().Err(err).Str("lb", lb.Name).Msg("lblogs: S3 list error")
			return
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			if !pt.isProcessed(lb.Bucket, key) {
				newKeys = append(newKeys, key)
			}
		}
	}

	if len(newKeys) == 0 {
		return
	}

	sort.Strings(newKeys)
	log.Info().Str("lb", lb.Name).Int("files", len(newKeys)).Msg("lblogs: processing new S3 log files")

	var windowRequests []LBRequest
	for _, key := range newKeys {
		requests, err := processLBFile(ctx, client, lb, clusterID, key)
		if err != nil {
			log.Error().Err(err).Str("key", key).Msg("lblogs: failed to process log file")
			continue
		}
		windowRequests = append(windowRequests, requests...)
		pt.markProcessed(lb.Bucket, key)

		for _, req := range requests {
			publishFn(req)
		}
	}

	spikes := detectSpikes(windowRequests, lb.Name)
	for _, spike := range spikes {
		log.Info().
			Str("lb", spike.LBName).
			Str("tg", spike.TargetGroup).
			Float64("rate", spike.Current).
			Msg("lblogs: 5xx spike detected")
	}
}

func processLBFile(ctx context.Context, client *s3.Client, lb LBConfig, clusterID, key string) ([]LBRequest, error) {
	result, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(lb.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 get %s: %w", key, err)
	}
	defer result.Body.Close()

	var reader io.Reader = result.Body
	if strings.HasSuffix(key, ".gz") {
		gz, err := gzip.NewReader(result.Body)
		if err != nil {
			return nil, fmt.Errorf("gzip open %s: %w", key, err)
		}
		defer gz.Close()
		reader = gz
	}

	parseFunc := selectLBParser(lb.Type)
	var requests []LBRequest

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		req, err := parseFunc(line, lb.Name, clusterID)
		if err != nil || req == nil {
			continue
		}
		requests = append(requests, *req)
	}

	return requests, scanner.Err()
}

type lbParseFunc func(line, lbName, cluster string) (*LBRequest, error)

func selectLBParser(lbType string) lbParseFunc {
	switch strings.ToLower(lbType) {
	case "nlb":
		return ParseNLBLine
	case "elb", "classic":
		return ParseClassicELBLine
	default:
		return ParseALBLine
	}
}

func detectSpikes(requests []LBRequest, lbName string) []LBSpikeEvent {
	if len(requests) == 0 {
		return nil
	}

	type tgStats struct {
		total int
		err5  int
	}
	byTG := map[string]*tgStats{}
	for _, r := range requests {
		tg := r.TargetGroup
		if tg == "" {
			tg = "_default"
		}
		s, ok := byTG[tg]
		if !ok {
			s = &tgStats{}
			byTG[tg] = s
		}
		s.total++
		if r.ELBStatus >= 500 || r.TargetStatus >= 500 {
			s.err5++
		}
	}

	var spikes []LBSpikeEvent
	for tg, s := range byTG {
		if s.total < 10 {
			continue
		}
		rate := float64(s.err5) / float64(s.total)
		if rate > 0.05 {
			spikes = append(spikes, LBSpikeEvent{
				LBName:      lbName,
				TargetGroup: tg,
				MetricName:  "5xx_rate",
				Current:     rate,
				Baseline:    0.01,
				Timestamp:   time.Now(),
			})
		}
	}
	return spikes
}
