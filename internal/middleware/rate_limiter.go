package middleware

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter implements a simple rate limiting middleware
type RateLimiter struct {
	requests int           // requests allowed in the time window
	duration time.Duration // time window for rate limiting
	clients  map[string][]time.Time
	mu       sync.Mutex
}

// NewRateLimiter creates a new rate limiter middleware
func NewRateLimiter(requests int, duration time.Duration) *RateLimiter {
	if requests <= 0 {
		requests = 100 // default: 100 requests
	}
	if duration <= 0 {
		duration = time.Minute // default: per minute
	}

	return &RateLimiter{
		requests: requests,
		duration: duration,
		clients:  make(map[string][]time.Time),
	}
}

// RateLimit is a middleware that limits request rates per client IP
func (rl *RateLimiter) RateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIP := r.RemoteAddr

		rl.mu.Lock()

		// Initialize if needed
		if _, exists := rl.clients[clientIP]; !exists {
			rl.clients[clientIP] = make([]time.Time, 0, rl.requests)
		}

		// Remove old timestamps
		now := time.Now()
		cutoff := now.Add(-rl.duration)

		timestamps := rl.clients[clientIP]
		i := 0
		for i < len(timestamps) && timestamps[i].Before(cutoff) {
			i++
		}

		// Keep only the timestamps that are within the window
		if i > 0 {
			timestamps = timestamps[i:]
			rl.clients[clientIP] = timestamps
		}

		// Check if rate limit exceeded
		if len(timestamps) >= rl.requests {
			rl.mu.Unlock()
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		// Add current timestamp
		rl.clients[clientIP] = append(timestamps, now)
		rl.mu.Unlock()

		next(w, r)
	}
}
