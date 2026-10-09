package httpapi

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type rateLimitEntry struct {
	count   int
	resetAt time.Time
}

type rateLimiter struct {
	mu         sync.Mutex
	limit      int
	window     time.Duration
	clients    map[string]rateLimitEntry
	allowCalls uint64
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		limit:   limit,
		window:  window,
		clients: make(map[string]rateLimitEntry),
	}
}

func (l *rateLimiter) allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.allowCalls++
	if l.allowCalls%256 == 0 {
		for client, entry := range l.clients {
			if !now.Before(entry.resetAt) {
				delete(l.clients, client)
			}
		}
	}

	entry, exists := l.clients[key]

	if !exists || !now.Before(entry.resetAt) {
		l.clients[key] = rateLimitEntry{
			count:   1,
			resetAt: now.Add(l.window),
		}
		return true
	}

	if entry.count >= l.limit {
		return false
	}

	entry.count++
	l.clients[key] = entry

	return true
}

func (l *rateLimiter) retryAfter(key string) int {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	entry, exists := l.clients[key]
	if !exists || !now.Before(entry.resetAt) {
		return 0
	}

	seconds := int(time.Until(entry.resetAt).Seconds())
	if seconds < 1 {
		return 1
	}

	return seconds
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}

	value := strings.TrimSpace(r.RemoteAddr)
	if value == "" {
		return "unknown"
	}

	return value
}
