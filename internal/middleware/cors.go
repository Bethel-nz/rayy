package middleware

import (
	"net/http"
)

// CORS middleware to handle Cross-Origin Resource Sharing
type CORS struct {
	allowedOrigins []string
}

// NewCORS creates a new CORS middleware
func NewCORS(allowedOrigins ...string) *CORS {
	// Default to allowing all origins if none specified
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"*"}
	}

	return &CORS{
		allowedOrigins: allowedOrigins,
	}
}

// CORS is middleware that adds CORS headers to responses
func (c *CORS) CORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if this origin is allowed
		allowedOrigin := "*" // Default
		if origin != "" {
			for _, allowed := range c.allowedOrigins {
				if allowed == "*" || allowed == origin {
					allowedOrigin = origin
					break
				}
			}
		}

		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}
