package main

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// EvictionConfig controls TTL and max-size limits for the analyzer's
// in-memory handler maps. Every field has a safe default so a
// deployment needs no configuration to get bounded memory use. All
// fields can be tuned via environment variables at startup.
type EvictionConfig struct {
	Interval time.Duration

	IncidentResolvedTTL time.Duration
	IncidentActiveTTL   time.Duration
	IncidentMax         int

	ErrorGroupTTL time.Duration
	ErrorGroupMax int

	AnomalyStatsTTL time.Duration
	AnomalyStatsMax int

	RCAReportTTL time.Duration
	RCAReportMax int

	OptRecNonOpenTTL time.Duration
	OptRecMax        int
}

// loadEvictionConfig reads eviction tuning from environment variables,
// falling back to the documented defaults when a variable is unset or
// cannot be parsed.
func loadEvictionConfig() EvictionConfig {
	return EvictionConfig{
		Interval:            getDurationOrDefault("EVICT_INTERVAL", 5*time.Minute),
		IncidentResolvedTTL: getDurationOrDefault("EVICT_INCIDENT_RESOLVED_TTL", 24*time.Hour),
		IncidentActiveTTL:   getDurationOrDefault("EVICT_INCIDENT_ACTIVE_TTL", 48*time.Hour),
		IncidentMax:         getEnvIntOrDefault("EVICT_INCIDENT_MAX", 500),
		ErrorGroupTTL:       getDurationOrDefault("EVICT_ERROR_GROUP_TTL", 168*time.Hour),
		ErrorGroupMax:       getEnvIntOrDefault("EVICT_ERROR_GROUP_MAX", 200),
		AnomalyStatsTTL:     getDurationOrDefault("EVICT_ANOMALY_STATS_TTL", 2*time.Hour),
		AnomalyStatsMax:     getEnvIntOrDefault("EVICT_ANOMALY_STATS_MAX", 1000),
		RCAReportTTL:        getDurationOrDefault("EVICT_RCA_REPORT_TTL", 48*time.Hour),
		RCAReportMax:        getEnvIntOrDefault("EVICT_RCA_REPORT_MAX", 500),
		OptRecNonOpenTTL:    getDurationOrDefault("EVICT_OPT_REC_TTL", 168*time.Hour),
		OptRecMax:           getEnvIntOrDefault("EVICT_OPT_REC_MAX", 300),
	}
}

// startEvictionLoop runs a periodic sweep that evicts stale entries
// from the analyzer's in-memory handler maps. The sweep order matters:
// the correlator runs first so RCA orphan detection is correct. The
// loop exits cleanly on ctx cancellation or analyzer stopCh close.
func (a *Analyzer) startEvictionLoop(ctx context.Context) {
	cfg := loadEvictionConfig()
	log.Info().
		Dur("interval", cfg.Interval).
		Int("incidentMax", cfg.IncidentMax).
		Int("errorGroupMax", cfg.ErrorGroupMax).
		Int("anomalyStatsMax", cfg.AnomalyStatsMax).
		Int("rcaReportMax", cfg.RCAReportMax).
		Int("optRecMax", cfg.OptRecMax).
		Msg("Starting eviction loop")

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.runEvictionSweep(cfg)
		}
	}
}

// runEvictionSweep invokes each handler's Evict method in dependency
// order and logs a single summary line when anything was removed.
func (a *Analyzer) runEvictionSweep(cfg EvictionConfig) {
	var incidents, rcaReports, errorGroups, anomalyStats, optRecs int

	if a.correlator != nil {
		incidents = a.correlator.Evict(cfg.IncidentResolvedTTL, cfg.IncidentActiveTTL, cfg.IncidentMax)
	}
	// RCA must run AFTER correlator so orphan detection sees the evicted incidents.
	if a.rcaEngine != nil {
		rcaReports = a.rcaEngine.Evict(cfg.RCAReportTTL, cfg.RCAReportMax)
	}
	if a.errorAggregator != nil {
		errorGroups = a.errorAggregator.Evict(cfg.ErrorGroupTTL, cfg.ErrorGroupMax)
	}
	if a.anomalyDetector != nil {
		anomalyStats = a.anomalyDetector.EvictStats(cfg.AnomalyStatsTTL, cfg.AnomalyStatsMax)
	}
	if a.optimizerRegistry != nil {
		optRecs = a.optimizerRegistry.Evict(cfg.OptRecNonOpenTTL, cfg.OptRecMax)
	}

	total := incidents + rcaReports + errorGroups + anomalyStats + optRecs
	if total > 0 {
		log.Info().
			Int("incidents", incidents).
			Int("rcaReports", rcaReports).
			Int("errorGroups", errorGroups).
			Int("anomalyStats", anomalyStats).
			Int("optRecs", optRecs).
			Int("total", total).
			Msg("Eviction sweep completed")
	}
}
