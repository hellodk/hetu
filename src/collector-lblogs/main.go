// Package main implements the LB log collector for cluster-intel v7.
// It polls S3 buckets for ALB/NLB/ELB access logs, parses them, publishes
// parsed rows and spike events to NATS, and tracks processed objects.
package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/your-org/cluster-intel/pkg/bus"
	"github.com/your-org/cluster-intel/pkg/config"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	cfg, err := config.LoadFromEnv("/etc/cluster-intel/config.yaml")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// NATS bus
	var eventBus *bus.Bus
	if cfg.Bus.NATS.Enabled {
		eventBus, err = bus.Connect(ctx, cfg.Bus.NATS)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to connect to NATS")
		}
		defer eventBus.Close()
	}

	// Determine delivery mode: NATS (default) or HTTP fallback
	deliveryMode := os.Getenv("DELIVERY_MODE")
	if deliveryMode == "" {
		deliveryMode = "nats"
	}

	// Build the publish callback based on delivery mode
	var publishFn func(LBRequest)
	var httpPusher *HTTPPusher

	switch deliveryMode {
	case "http":
		analyzerURL := os.Getenv("ANALYZER_URL")
		if analyzerURL == "" {
			analyzerURL = "http://analyzer:8081"
		}
		httpPusher = NewHTTPPusher(analyzerURL, 50)
		publishFn = httpPusher.Start(ctx)
		log.Info().Str("target", analyzerURL).Msg("Using HTTP delivery mode")
	default:
		if eventBus != nil {
			publishFn = func(req LBRequest) {
				data, _ := json.Marshal(req)
				eventBus.Publish(ctx, "lb.request", data)
			}
			log.Info().Msg("Using NATS delivery mode")
		} else {
			log.Warn().Msg("NATS not available and DELIVERY_MODE=nats; events will be dropped")
			publishFn = func(LBRequest) {}
		}
	}

	var wg sync.WaitGroup

	// --- CloudWatch source ---
	cwLogGroups := os.Getenv("CW_LOG_GROUPS")
	if cwLogGroups != "" {
		groups := strings.Split(cwLogGroups, ",")
		for i := range groups {
			groups[i] = strings.TrimSpace(groups[i])
		}

		awsRegion := os.Getenv("AWS_REGION")
		opts := []func(*awsconfig.LoadOptions) error{}
		if awsRegion != "" {
			opts = append(opts, awsconfig.WithRegion(awsRegion))
		}
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to load AWS config for CloudWatch")
		}
		cwClient := cloudwatchlogs.NewFromConfig(awsCfg)

		pollInterval := parseDuration(os.Getenv("CW_POLL_INTERVAL"), 10*time.Second)
		lookback := parseDuration(os.Getenv("CW_LOOKBACK"), 5*time.Minute)

		cwSource := NewCloudWatchSource(cwClient, groups, pollInterval, lookback, cfg.Cluster.ID, publishFn)
		wg.Add(1)
		go func() {
			defer wg.Done()
			cwSource.Start(ctx)
		}()
	}

	// --- S3 source (existing) ---
	lbConfigs := loadLBConfigs()
	if len(lbConfigs) > 0 {
		processed := &processedTracker{keys: make(map[string]bool)}
		for _, lbc := range lbConfigs {
			wg.Add(1)
			go func(lb LBConfig) {
				defer wg.Done()
				pollLoop(ctx, lb, cfg.Cluster.ID, eventBus, processed)
			}(lbc)
		}
		log.Info().Int("lbs", len(lbConfigs)).Msg("S3 LB log polling started")
	}

	if cwLogGroups == "" && len(lbConfigs) == 0 {
		log.Warn().Msg("No LB sources configured. Set CW_LOG_GROUPS or LB_CONFIGS env var.")
	}

	log.Info().Str("delivery", deliveryMode).Msg("LB log collector started")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Info().Msg("Shutting down")
	cancel()
	wg.Wait()
	if httpPusher != nil {
		httpPusher.Stop()
	}
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

func loadLBConfigs() []LBConfig {
	raw := os.Getenv("LB_CONFIGS")
	if raw == "" {
		return nil
	}
	var configs []LBConfig
	if err := json.Unmarshal([]byte(raw), &configs); err != nil {
		log.Error().Err(err).Msg("Failed to parse LB_CONFIGS")
		return nil
	}
	return configs
}

// processedTracker keeps track of S3 keys already processed.
// In-memory for now; will be backed by Postgres lb_processed_objects.
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

func pollLoop(ctx context.Context, lb LBConfig, clusterID string, eventBus *bus.Bus, pt *processedTracker) {
	interval := time.Duration(lb.PollSecs) * time.Second
	if interval == 0 {
		interval = 60 * time.Second
	}

	// Initialize AWS S3 client
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(lb.Region))
	if err != nil {
		log.Error().Err(err).Str("lb", lb.Name).Msg("Failed to load AWS config")
		return
	}
	s3Client := s3.NewFromConfig(awsCfg)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// First poll immediately
	poll(ctx, s3Client, lb, clusterID, eventBus, pt)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll(ctx, s3Client, lb, clusterID, eventBus, pt)
		}
	}
}

func poll(ctx context.Context, client *s3.Client, lb LBConfig, clusterID string, eventBus *bus.Bus, pt *processedTracker) {
	log.Debug().Str("lb", lb.Name).Str("bucket", lb.Bucket).Msg("Polling S3 for new log files")

	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(lb.Bucket),
		Prefix: aws.String(lb.Prefix),
	})

	var newKeys []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			log.Error().Err(err).Str("lb", lb.Name).Msg("S3 list error")
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

	// Process oldest first
	sort.Strings(newKeys)
	log.Info().Str("lb", lb.Name).Int("files", len(newKeys)).Msg("Processing new log files")

	// Track stats for spike detection
	var windowRequests []LBRequest
	for _, key := range newKeys {
		requests, err := processFile(ctx, client, lb, clusterID, key)
		if err != nil {
			log.Error().Err(err).Str("key", key).Msg("Failed to process log file")
			continue
		}
		windowRequests = append(windowRequests, requests...)
		pt.markProcessed(lb.Bucket, key)

		// Publish individual parsed requests to NATS
		if eventBus != nil {
			for _, req := range requests {
				data, _ := json.Marshal(req)
				eventBus.Publish(ctx, "lb.request", data)
			}
		}
	}

	// Detect spikes and publish events
	if eventBus != nil {
		spikes := detectSpikes(windowRequests, lb.Name)
		for _, spike := range spikes {
			data, _ := json.Marshal(spike)
			eventBus.Publish(ctx, "lb.spike", data)
		}
	}
}

func processFile(ctx context.Context, client *s3.Client, lb LBConfig, clusterID, key string) ([]LBRequest, error) {
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

	parseFunc := selectParser(lb.Type)
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

	log.Debug().Str("key", key).Int("requests", len(requests)).Msg("Parsed log file")
	return requests, scanner.Err()
}

type parseFunc func(line, lbName, cluster string) (*LBRequest, error)

func selectParser(lbType string) parseFunc {
	switch strings.ToLower(lbType) {
	case "nlb":
		return ParseNLBLine
	case "elb", "classic":
		return ParseClassicELBLine
	default:
		return ParseALBLine
	}
}

// detectSpikes checks for anomalous 5xx rates in a batch of parsed requests.
func detectSpikes(requests []LBRequest, lbName string) []LBSpikeEvent {
	if len(requests) == 0 {
		return nil
	}

	// Count 5xx per target group
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
			continue // not enough data
		}
		rate := float64(s.err5) / float64(s.total)
		if rate > 0.05 { // >5% 5xx rate
			spikes = append(spikes, LBSpikeEvent{
				LBName:      lbName,
				TargetGroup: tg,
				MetricName:  "5xx_rate",
				Current:     rate,
				Baseline:    0.01, // assumed baseline
				Timestamp:   time.Now(),
			})
		}
	}
	return spikes
}
