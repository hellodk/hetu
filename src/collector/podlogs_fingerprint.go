package main

import (
	"crypto/sha1"
	"fmt"
	"regexp"
	"strings"
)

// Fingerprint produces a deterministic hash for grouping similar log events.
// Sentry-inspired: stack-based events group by (service + exception type +
// top N frames). Non-stack errors group by (service + level + templated message).
func Fingerprint(p *ParsedLog) string {
	if p.StackTrace != "" {
		normalized := normalizeStack(p.StackTrace)
		frames := topNFrames(normalized, 5)
		exType := extractExceptionType(p)
		return hash(p.Service + "|" + exType + "|" + frames)
	}
	tmpl := templateMessage(p.Message)
	return hash(p.Service + "|" + p.Level + "|" + tmpl)
}

func normalizeStack(stack string) string {
	s := stack
	s = reLineNumbers.ReplaceAllString(s, ":_")
	s = reHexAddr.ReplaceAllString(s, "0x_")
	s = reUUID.ReplaceAllString(s, "_UUID_")
	s = reTimestamp.ReplaceAllString(s, "_TS_")
	return s
}

func topNFrames(stack string, n int) string {
	lines := strings.Split(stack, "\n")
	var frames []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if isFrameLine(trimmed) {
			frames = append(frames, trimmed)
			if len(frames) >= n {
				break
			}
		}
	}
	return strings.Join(frames, "\n")
}

func isFrameLine(line string) bool {
	return strings.HasPrefix(line, "at ") ||
		strings.HasPrefix(line, "\tat ") ||
		strings.HasPrefix(line, "File \"") ||
		strings.HasPrefix(line, "goroutine ") ||
		strings.Contains(line, ".go:") ||
		strings.Contains(line, ".java:") ||
		strings.Contains(line, ".py:") ||
		strings.Contains(line, ".js:") ||
		strings.Contains(line, ".ts:")
}

func extractExceptionType(p *ParsedLog) string {
	if p.Error != "" {
		if idx := strings.Index(p.Error, ":"); idx > 0 {
			candidate := strings.TrimSpace(p.Error[:idx])
			if isExceptionName(candidate) {
				return candidate
			}
		}
	}
	lines := strings.Split(p.StackTrace, "\n")
	if len(lines) > 0 {
		first := strings.TrimSpace(lines[0])
		if idx := strings.Index(first, ":"); idx > 0 {
			candidate := strings.TrimSpace(first[:idx])
			if isExceptionName(candidate) {
				return candidate
			}
		}
	}
	return p.Reason
}

func isExceptionName(s string) bool {
	return strings.Contains(s, "Exception") ||
		strings.Contains(s, "Error") ||
		strings.Contains(s, "Panic") ||
		strings.Contains(s, "panic") ||
		strings.Contains(s, "Fault")
}

func templateMessage(msg string) string {
	s := msg
	s = reUUID.ReplaceAllString(s, ":id")
	s = reIPv4.ReplaceAllString(s, ":ip")
	s = reNumbers.ReplaceAllString(s, ":n")
	s = reHexHash.ReplaceAllString(s, ":hash")
	s = reQuotedStr.ReplaceAllString(s, `":str"`)
	return s
}

func hash(s string) string {
	h := sha1.Sum([]byte(s))
	return fmt.Sprintf("%x", h[:10])
}

// Compiled regexes for fingerprinting.
var (
	reLineNumbers = regexp.MustCompile(`:\d+`)
	reHexAddr     = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	reUUID        = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	reTimestamp   = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`)
	reIPv4        = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)
	reNumbers     = regexp.MustCompile(`\b\d{2,}\b`)
	reHexHash     = regexp.MustCompile(`\b[0-9a-fA-F]{8,}\b`)
	reQuotedStr   = regexp.MustCompile(`"[^"]{8,}"`)
)
