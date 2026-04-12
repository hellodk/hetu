// Package llm hosts the cluster-intel LLM client and prompt registry.
//
// In v7 this package is the canonical home for what previously lived in
// src/analyzer/llm_metrics.go — extracted so collectors, optimizers, and
// the RCA pipeline can all share a single, instrumented HTTP client.
package llm
