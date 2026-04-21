package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParseLogLine parses a raw log line into a structured ParsedLog.
// Returns nil if the line is uninteresting (info-level, no patterns).
func ParseLogLine(raw, service, namespace, pod, container string) *ParsedLog {
	p := &ParsedLog{
		Timestamp: time.Now(),
		Namespace: namespace,
		Pod:       pod,
		Container: container,
		Service:   service,
		Raw:       raw,
		Level:     "info",
	}

	line := strings.TrimSpace(raw)
	if line == "" {
		return nil
	}

	// Try JSON first (most common in production k8s)
	if line[0] == '{' {
		if parseJSON(line, p) {
			if p.Level == "info" || p.Level == "debug" || p.Level == "trace" {
				// Still check patterns even for info — might have an exception in msg
				detectPatterns(p)
				if p.Reason == "" {
					return nil // truly uninteresting
				}
			}
			detectPatterns(p)
			return p
		}
	}

	// Try logfmt: key=value pairs
	if strings.Contains(line, "level=") || strings.Contains(line, "msg=") {
		parseLogfmt(line, p)
		detectPatterns(p)
		if p.Level == "info" || p.Level == "debug" || p.Level == "trace" {
			if p.Reason == "" {
				return nil
			}
		}
		return p
	}

	// Plain text
	parsePlain(line, p)
	detectPatterns(p)
	if p.Level == "info" || p.Level == "debug" || p.Level == "trace" {
		if p.Reason == "" {
			return nil
		}
	}
	return p
}

// --- JSON parser ---

func parseJSON(line string, p *ParsedLog) bool {
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return false
	}

	p.Level = extractStringAny(m, "level", "severity", "lvl")
	p.Message = extractStringAny(m, "msg", "message", "error_message")
	p.Error = extractStringAny(m, "error", "err", "exception")
	p.StackTrace = extractStringAny(m, "stack", "stacktrace", "stack_trace", "stackTrace")
	p.RequestID = extractStringAny(m, "request_id", "requestId", "req_id", "x-request-id")
	p.TraceID = extractStringAny(m, "trace_id", "traceId", "dd.trace_id")
	p.URL = extractStringAny(m, "url", "path", "http.url", "uri", "request_uri")

	if sc := extractStringAny(m, "status", "status_code", "http.status_code", "statusCode"); sc != "" {
		p.StatusCode, _ = strconv.Atoi(sc)
	}
	if lat := extractStringAny(m, "latency", "duration", "latency_ms", "response_time", "elapsed"); lat != "" {
		p.LatencyMs, _ = strconv.ParseFloat(strings.TrimSuffix(lat, "ms"), 64)
	}

	if ts := extractStringAny(m, "time", "timestamp", "ts", "@timestamp"); ts != "" {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			p.Timestamp = t
		}
	}

	p.Level = normalizeLevel(p.Level)
	return true
}

// --- Logfmt parser ---

func parseLogfmt(line string, p *ParsedLog) {
	fields := map[string]string{}
	parts := splitLogfmt(line)
	for k, v := range parts {
		fields[k] = v
	}

	p.Level = normalizeLevel(firstOf(fields, "level", "lvl", "severity"))
	p.Message = firstOf(fields, "msg", "message")
	p.Error = firstOf(fields, "error", "err")
	p.RequestID = firstOf(fields, "request_id", "req_id")
	p.URL = firstOf(fields, "url", "path")
	if sc := firstOf(fields, "status", "status_code"); sc != "" {
		p.StatusCode, _ = strconv.Atoi(sc)
	}
	p.Fields = fields
}

func splitLogfmt(line string) map[string]string {
	result := map[string]string{}
	i := 0
	for i < len(line) {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		eqIdx := strings.IndexByte(line[i:], '=')
		if eqIdx == -1 {
			break
		}
		key := line[i : i+eqIdx]
		i += eqIdx + 1
		if i < len(line) && line[i] == '"' {
			endQuote := strings.IndexByte(line[i+1:], '"')
			if endQuote == -1 {
				result[key] = line[i+1:]
				break
			}
			result[key] = line[i+1 : i+1+endQuote]
			i += endQuote + 2
		} else {
			spIdx := strings.IndexByte(line[i:], ' ')
			if spIdx == -1 {
				result[key] = line[i:]
				break
			}
			result[key] = line[i : i+spIdx]
			i += spIdx
		}
	}
	return result
}

// --- Plain text parser ---

var plainLevelRe = regexp.MustCompile(`(?i)\b(FATAL|PANIC|ERROR|WARN(?:ING)?|INFO|DEBUG|TRACE)\b`)

func parsePlain(line string, p *ParsedLog) {
	if m := plainLevelRe.FindString(line); m != "" {
		p.Level = normalizeLevel(m)
	}
	p.Message = line
}

// --- Pattern detectors ---

var (
	javaStackRe  = regexp.MustCompile(`(?m)^\s+at\s+[\w.$]+\(`)
	goPanicRe    = regexp.MustCompile(`(?m)^goroutine \d+ \[`)
	pythonTBRe   = regexp.MustCompile(`Traceback \(most recent call last\)`)
	nodeStackRe  = regexp.MustCompile(`at\s+.+\(.+:\d+:\d+\)`)
	dotnetExRe   = regexp.MustCompile(`System\.\w+Exception`)
	timeoutRe    = regexp.MustCompile(`(?i)(context deadline exceeded|i/o timeout|connect ETIMEDOUT|upstream timeout|read tcp .+ timeout|connection timed out|request timeout|gateway timeout)`)
	oomRe        = regexp.MustCompile(`(?i)(OOMKilled|out of memory|OutOfMemoryError|cannot allocate memory|ENOMEM)`)
	http5xxRe    = regexp.MustCompile(`(?:HTTP|status|code)\s*[=: ]\s*5\d\d\b`)
	gcPressureRe = regexp.MustCompile(`(?i)(gc pause|Full GC|\[gc\]|pause time|STW|stop-the-world)`)
)

func detectPatterns(p *ParsedLog) {
	text := p.Message + " " + p.Error + " " + p.StackTrace + " " + p.Raw

	switch {
	case javaStackRe.MatchString(text):
		p.Reason = "exception.java"
		if p.StackTrace == "" {
			p.StackTrace = text
		}
	case goPanicRe.MatchString(text):
		p.Reason = "panic.go"
		if p.StackTrace == "" {
			p.StackTrace = text
		}
	case pythonTBRe.MatchString(text):
		p.Reason = "exception.python"
		if p.StackTrace == "" {
			p.StackTrace = text
		}
	case dotnetExRe.MatchString(text):
		p.Reason = "exception.dotnet"
	case nodeStackRe.MatchString(text) && (p.Level == "error" || p.Level == "fatal"):
		p.Reason = "exception.node"
		if p.StackTrace == "" {
			p.StackTrace = text
		}
	case timeoutRe.MatchString(text):
		p.Reason = "timeout"
	case oomRe.MatchString(text):
		p.Reason = "oom"
	case http5xxRe.MatchString(text):
		p.Reason = "http.5xx"
	case gcPressureRe.MatchString(text):
		p.Reason = "gc.pressure"
	default:
		if p.Level == "error" || p.Level == "fatal" || p.Level == "panic" {
			p.Reason = "error"
		}
	}

	// Promote level if pattern is severe
	if p.Reason != "" && (p.Level == "info" || p.Level == "debug") {
		p.Level = "error"
	}
}

// --- Helpers ---

func extractStringAny(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch val := v.(type) {
			case string:
				return val
			case float64:
				return strconv.FormatFloat(val, 'f', -1, 64)
			case int:
				return strconv.Itoa(val)
			}
		}
	}
	return ""
}

func firstOf(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

func normalizeLevel(l string) string {
	switch strings.ToLower(strings.TrimSpace(l)) {
	case "fatal", "crit", "critical":
		return "fatal"
	case "panic":
		return "panic"
	case "error", "err":
		return "error"
	case "warn", "warning":
		return "warn"
	case "info", "information":
		return "info"
	case "debug", "dbg":
		return "debug"
	case "trace":
		return "trace"
	default:
		return "info"
	}
}
