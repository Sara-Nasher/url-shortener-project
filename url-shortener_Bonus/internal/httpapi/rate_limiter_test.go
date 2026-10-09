package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"url-shortener/internal/shortener"
	"url-shortener/internal/store"
)

func TestRateLimitOnShorten(t *testing.T) {
	db := store.New()
	service := shortener.NewService(db)
	handler := NewHandler(service, "http://localhost:8080")

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/shorten",
			strings.NewReader(`{"url":"https://example.com/page/`+string(rune('a'+i))+`"}`),
		)
		req.RemoteAddr = "192.168.1.10:1234"

		res := httptest.NewRecorder()

		handler.ServeHTTP(res, req)

		if res.Code != http.StatusCreated {
			t.Fatalf("request %d: expected 201, got %d", i+1, res.Code)
		}
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/shorten",
		strings.NewReader(`{"url":"https://example.com/blocked"}`),
	)
	req.RemoteAddr = "192.168.1.10:1234"

	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", res.Code)
	}

	if res.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestRateLimitPerIP(t *testing.T) {
	db := store.New()
	service := shortener.NewService(db)
	handler := NewHandler(service, "http://localhost:8080")

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/shorten",
			strings.NewReader(`{"url":"https://example.com/first/`+string(rune('a'+i))+`"}`),
		)
		req.RemoteAddr = "192.168.1.10:1234"

		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)

		if res.Code != http.StatusCreated {
			t.Fatalf("request %d: expected 201, got %d", i+1, res.Code)
		}
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/shorten",
		strings.NewReader(`{"url":"https://example.com/second"}`),
	)
	req.RemoteAddr = "192.168.1.20:1234"

	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected different IP to be allowed, got %d", res.Code)
	}
}

func TestRateLimiterWindowReset(t *testing.T) {
	limiter := newRateLimiter(2, 50*time.Millisecond)

	if !limiter.allow("192.168.1.10") {
		t.Fatal("first request should be allowed")
	}

	if !limiter.allow("192.168.1.10") {
		t.Fatal("second request should be allowed")
	}

	if limiter.allow("192.168.1.10") {
		t.Fatal("third request should be blocked")
	}

	time.Sleep(60 * time.Millisecond)

	if !limiter.allow("192.168.1.10") {
		t.Fatal("request should be allowed after window reset")
	}
}

func TestRateLimiterRemovesExpiredClients(t *testing.T) {
	limiter := newRateLimiter(2, time.Minute)
	limiter.clients["expired"] = rateLimitEntry{count: 1, resetAt: time.Now().Add(-time.Second)}
	limiter.clients["active"] = rateLimitEntry{count: 1, resetAt: time.Now().Add(time.Minute)}
	limiter.allowCalls = 255

	if !limiter.allow("new-client") {
		t.Fatal("first request from a new client should be allowed")
	}
	if _, exists := limiter.clients["expired"]; exists {
		t.Fatal("expired client was not removed")
	}
	if _, exists := limiter.clients["active"]; !exists {
		t.Fatal("active client should remain")
	}
}
