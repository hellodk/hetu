//go:build !nolblogs

package main

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParseALBLine parses a single ALB access log line.
func ParseALBLine(line, lbName, cluster string) (*LBRequest, error) {
	fields := splitALBFields(line)
	if len(fields) < 25 {
		return nil, nil
	}

	ts, err := time.Parse(time.RFC3339Nano, fields[1])
	if err != nil {
		return nil, nil
	}

	clientIP := extractIP(fields[3])
	elbStatus, _ := strconv.Atoi(fields[8])
	targetStatus, _ := strconv.Atoi(fields[9])
	reqProcTime := parseSeconds(fields[5])
	tgtProcTime := parseSeconds(fields[6])
	resProcTime := parseSeconds(fields[7])

	method, rawURL := parseRequestField(fields[12])
	urlPattern := templateURL(rawURL)

	tgArn := ""
	if len(fields) > 13 {
		tgArn = fields[13]
	}
	traceID := ""
	if len(fields) > 19 {
		traceID = fields[19]
	}
	ua := ""
	if len(fields) > 21 {
		ua = fields[21]
	}

	return &LBRequest{
		Timestamp:            ts,
		Cluster:              cluster,
		LBName:               lbName,
		LBType:               "alb",
		TargetGroup:          extractTGName(tgArn),
		URLPattern:           urlPattern,
		HTTPMethod:           method,
		ELBStatus:            elbStatus,
		TargetStatus:         targetStatus,
		ClientIP:             clientIP,
		RequestProcessingMs:  reqProcTime * 1000,
		TargetProcessingMs:   tgtProcTime * 1000,
		ResponseProcessingMs: resProcTime * 1000,
		TraceID:              traceID,
		UserAgent:            ua,
		RequestURL:           rawURL,
	}, nil
}

// ParseNLBLine parses a single NLB access log line.
func ParseNLBLine(line, lbName, cluster string) (*LBRequest, error) {
	fields := strings.Fields(line)
	if len(fields) < 10 {
		return nil, nil
	}

	ts, err := time.Parse(time.RFC3339Nano, fields[2])
	if err != nil {
		return nil, nil
	}

	clientIP := extractIP(fields[5])

	return &LBRequest{
		Timestamp: ts,
		Cluster:   cluster,
		LBName:    lbName,
		LBType:    "nlb",
		ClientIP:  clientIP,
	}, nil
}

// ParseClassicELBLine parses a classic ELB access log line.
func ParseClassicELBLine(line, lbName, cluster string) (*LBRequest, error) {
	fields := splitALBFields(line)
	if len(fields) < 12 {
		return nil, nil
	}

	ts, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return nil, nil
	}

	clientIP := extractIP(fields[2])
	elbStatus, _ := strconv.Atoi(fields[7])
	targetStatus, _ := strconv.Atoi(fields[8])
	reqProcTime := parseSeconds(fields[4])
	tgtProcTime := parseSeconds(fields[5])
	resProcTime := parseSeconds(fields[6])

	method, rawURL := parseRequestField(fields[11])

	return &LBRequest{
		Timestamp:            ts,
		Cluster:              cluster,
		LBName:               lbName,
		LBType:               "elb",
		URLPattern:           templateURL(rawURL),
		HTTPMethod:           method,
		ELBStatus:            elbStatus,
		TargetStatus:         targetStatus,
		ClientIP:             clientIP,
		RequestProcessingMs:  reqProcTime * 1000,
		TargetProcessingMs:   tgtProcTime * 1000,
		ResponseProcessingMs: resProcTime * 1000,
		RequestURL:           rawURL,
	}, nil
}

// --- URL templating ---

var (
	reNumericSegment = regexp.MustCompile(`/\d+`)
	reUUIDSegment    = regexp.MustCompile(`/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	reHexSegment     = regexp.MustCompile(`/[0-9a-fA-F]{8,}`)
	reBase64Segment  = regexp.MustCompile(`/[A-Za-z0-9+/]{20,}=*`)
)

func templateURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" {
		return rawURL
	}
	path := u.Path
	path = reUUIDSegment.ReplaceAllString(path, "/:uuid")
	path = reHexSegment.ReplaceAllString(path, "/:hash")
	path = reBase64Segment.ReplaceAllString(path, "/:b64")
	path = reNumericSegment.ReplaceAllString(path, "/:id")
	return path
}

// --- Field parsing helpers ---

func splitALBFields(line string) []string {
	var fields []string
	i := 0
	for i < len(line) {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		if i >= len(line) {
			break
		}
		if line[i] == '"' {
			end := strings.IndexByte(line[i+1:], '"')
			if end == -1 {
				fields = append(fields, line[i+1:])
				break
			}
			fields = append(fields, line[i+1:i+1+end])
			i += end + 2
		} else {
			end := strings.IndexByte(line[i:], ' ')
			if end == -1 {
				fields = append(fields, line[i:])
				break
			}
			fields = append(fields, line[i:i+end])
			i += end
		}
	}
	return fields
}

func extractIP(hostPort string) string {
	if idx := strings.LastIndex(hostPort, ":"); idx > 0 {
		return hostPort[:idx]
	}
	return hostPort
}

func parseSeconds(s string) float64 {
	if s == "-1" || s == "-" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func parseRequestField(field string) (method, rawURL string) {
	parts := strings.SplitN(field, " ", 3)
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return "", field
}

func extractTGName(arn string) string {
	if idx := strings.Index(arn, "targetgroup/"); idx >= 0 {
		rest := arn[idx+len("targetgroup/"):]
		if slashIdx := strings.IndexByte(rest, '/'); slashIdx > 0 {
			return rest[:slashIdx]
		}
		return rest
	}
	return arn
}
