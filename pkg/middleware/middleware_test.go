package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCORS_AllowedOrigin(t *testing.T) {
	config := CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type"},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("Expected Allow-Origin 'https://example.com', got %q", got)
	}
}

func TestCORS_DeniedOrigin(t *testing.T) {
	config := CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Content-Type"},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://evil.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Expected empty Allow-Origin for denied origin, got %q", got)
	}
}

func TestCORS_WildcardOrigin(t *testing.T) {
	config := CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Content-Type"},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://anything.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://anything.com" {
		t.Errorf("Expected origin echo for wildcard, got %q", got)
	}
}

func TestCORS_PreflightOptions(t *testing.T) {
	config := CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	}

	called := false
	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("OPTIONS preflight should not call next handler")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200 for OPTIONS, got %d", rr.Code)
	}
}

func TestCORS_NoOriginHeader(t *testing.T) {
	config := CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Content-Type"},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	// No Origin header
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Expected no Allow-Origin without Origin header, got %q", got)
	}
}

func TestDefaultCORSConfig(t *testing.T) {
	config := DefaultCORSConfig()
	if len(config.AllowedOrigins) != 0 {
		t.Errorf("Default should have empty allowed origins, got %v", config.AllowedOrigins)
	}
	if len(config.AllowedMethods) != 3 {
		t.Errorf("Default should have 3 methods, got %d", len(config.AllowedMethods))
	}
}

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter(1, time.Second, 3)

	// First 3 requests should be allowed (burst)
	for i := range 3 {
		if !rl.Allow("1.2.3.4") {
			t.Errorf("Request %d should be allowed within burst", i+1)
		}
	}

	// 4th request should be denied
	if rl.Allow("1.2.3.4") {
		t.Error("Request 4 should be denied (burst exhausted)")
	}
}

func TestRateLimiter_DifferentIPs(t *testing.T) {
	rl := NewRateLimiter(1, time.Second, 2)

	if !rl.Allow("1.1.1.1") {
		t.Error("First IP first request should be allowed")
	}
	if !rl.Allow("2.2.2.2") {
		t.Error("Second IP first request should be allowed")
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	rl := NewRateLimiter(1, 50*time.Millisecond, 1)

	if !rl.Allow("1.1.1.1") {
		t.Error("First request should be allowed")
	}
	if rl.Allow("1.1.1.1") {
		t.Error("Second request should be denied")
	}

	// Wait for refill
	time.Sleep(60 * time.Millisecond)

	if !rl.Allow("1.1.1.1") {
		t.Error("Request after refill should be allowed")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	rl := NewRateLimiter(1, time.Second, 1)
	handler := RateLimit(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request allowed
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("First request: expected 200, got %d", rr.Code)
	}

	// Second request denied
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("Second request: expected 429, got %d", rr.Code)
	}
}

func TestRateLimitMiddleware_XForwardedFor(t *testing.T) {
	rl := NewRateLimiter(1, time.Second, 1)
	handler := RateLimit(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	req.RemoteAddr = "proxy:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestIsOriginAllowed(t *testing.T) {
	tests := []struct {
		origin   string
		allowed  []string
		expected bool
	}{
		{"https://example.com", []string{"https://example.com"}, true},
		{"https://evil.com", []string{"https://example.com"}, false},
		{"https://anything.com", []string{"*"}, true},
		{"", []string{"*"}, false},
		{"https://example.com", []string{}, false},
	}

	for _, tt := range tests {
		result := isOriginAllowed(tt.origin, tt.allowed)
		if result != tt.expected {
			t.Errorf("isOriginAllowed(%q, %v) = %v, want %v", tt.origin, tt.allowed, result, tt.expected)
		}
	}
}
