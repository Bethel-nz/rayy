package middleware

import (
	"net/http"
	"sync"
	"time"
)

type RateLimiter struct {
	requests map[string]*ClientRequests
	mu       sync.RWMutex
}

type ClientRequests struct {
	count    int
	lastSeen time.Time
}

func NewRateLimiter() *RateLimiter {
	limiter := &RateLimiter{
		requests: make(map[string]*ClientRequests),
	}

	// Start cleanup routine
	go limiter.cleanup()
	return limiter
}

func (rl *RateLimiter) cleanup() {
	for {
		time.Sleep(time.Minute)
		rl.mu.Lock()
		for ip, req := range rl.requests {
			if time.Since(req.lastSeen) > time.Minute {
				delete(rl.requests, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *RateLimiter) RateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get client IP, preferring X-Forwarded-For header if present
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}

		rl.mu.Lock()
		if rl.requests[ip] == nil {
			rl.requests[ip] = &ClientRequests{
				count:    0,
				lastSeen: time.Now(),
			}
		}

		// Reset count if minute has passed
		if time.Since(rl.requests[ip].lastSeen) >= time.Minute {
			rl.requests[ip].count = 0
			rl.requests[ip].lastSeen = time.Now()
		}

		// Check rate limit
		if rl.requests[ip].count >= 100 {
			rl.mu.Unlock()
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Rate limit exceeded. Please try again in 1 minute.", http.StatusTooManyRequests)
			return
		}

		rl.requests[ip].count++
		rl.mu.Unlock()

		next(w, r)
	}
}
