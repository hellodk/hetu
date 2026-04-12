package main

import (
	"fmt"

	"github.com/rs/zerolog/log"
)

// CoreDNSOptimizer analyzes CoreDNS metrics for tuning opportunities.
type CoreDNSOptimizer struct{}

func (o *CoreDNSOptimizer) Name() string { return "coredns" }

func (o *CoreDNSOptimizer) Run(ctx OptimizerContext) ([]OptRecommendation, error) {
	if ctx.PrometheusURL == "" {
		return nil, fmt.Errorf("prometheus URL not configured")
	}

	var recs []OptRecommendation

	// Check NXDOMAIN rate
	nxResults, err := queryPromInstant(ctx.PrometheusURL,
		`sum(rate(coredns_dns_responses_total{rcode="NXDOMAIN"}[5m])) / sum(rate(coredns_dns_responses_total[5m]))`)
	if err == nil && len(nxResults) > 0 {
		if value, ok := nxResults[0]["value"].([]any); ok && len(value) >= 2 {
			nxRate := parsePromValue(value[1])
			if nxRate > 0.05 { // >5% NXDOMAIN
				recs = append(recs, OptRecommendation{
					Type:       "coredns",
					Severity:   "medium",
					Confidence: 0.7,
					Target:     OptTarget{Kind: "CoreDNS", Name: "coredns"},
					CurrentState: map[string]any{
						"nxdomainRate": fmt.Sprintf("%.1f%%", nxRate*100),
					},
					SuggestedState: map[string]any{
						"ndots": 2,
					},
					Rationale: fmt.Sprintf("NXDOMAIN rate is %.1f%%. High NXDOMAIN rates often indicate excessive DNS search path lookups. Set ndots:2 in pod DNS config or add autopath plugin.", nxRate*100),
					YAMLPatch: `# In pod spec:
dnsConfig:
  options:
    - name: ndots
      value: "2"`,
				})
			}
		}
	}

	// Check cache hit rate
	cacheResults, err := queryPromInstant(ctx.PrometheusURL,
		`sum(rate(coredns_cache_hits_total[5m])) / (sum(rate(coredns_cache_hits_total[5m])) + sum(rate(coredns_cache_misses_total[5m])))`)
	if err == nil && len(cacheResults) > 0 {
		if value, ok := cacheResults[0]["value"].([]any); ok && len(value) >= 2 {
			hitRate := parsePromValue(value[1])
			if hitRate < 0.60 && hitRate > 0 {
				recs = append(recs, OptRecommendation{
					Type:       "coredns",
					Severity:   "low",
					Confidence: 0.6,
					Target:     OptTarget{Kind: "CoreDNS", Name: "coredns"},
					CurrentState: map[string]any{
						"cacheHitRate": fmt.Sprintf("%.1f%%", hitRate*100),
					},
					SuggestedState: map[string]any{
						"cacheSize": "increased",
					},
					Rationale: fmt.Sprintf("CoreDNS cache hit rate is %.1f%%. Consider increasing cache size in the Corefile or deploying NodeLocalDNS.", hitRate*100),
				})
			}
		}
	}

	log.Debug().Int("recs", len(recs)).Msg("CoreDNS optimizer completed")
	return recs, nil
}
