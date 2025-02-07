package middleware

import "net/http"

type CORS struct {
	allowedOrigins map[string]bool
}

func NewCORS() *CORS {
	return &CORS{
		allowedOrigins: map[string]bool{
			"http://localhost:3000": true,
			"http://localhost:8080": true,
		},
	}
}

func (c *CORS) CORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		if c.allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*") // Fallback for development
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}
