package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the application
type Config struct {
	Server    ServerConfig
	WebSocket WebSocketConfig
	RateLimit RateLimitConfig
	Room      RoomConfig
}

// ServerConfig holds settings for the HTTP server
type ServerConfig struct {
	Address         string
	ReadTimeoutSec  int
	WriteTimeoutSec int
}

// WebSocketConfig holds settings for WebSockets
type WebSocketConfig struct {
	ReadBufferSize   int
	WriteBufferSize  int
	AllowedOrigins   []string
	PingIntervalSec  int
	PongWaitSec      int
	WriteWaitSec     int
	MaxMessageSizeKB int
}

// RateLimitConfig holds settings for rate limiting
type RateLimitConfig struct {
	Requests int
	Duration time.Duration
}

// RoomConfig holds settings for room management
type RoomConfig struct {
	CleanupTimeoutSec int
	MaxClients        int
}

// Load returns configuration loaded from environment with fallback to defaults
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Address:         getEnv("SERVER_ADDR", ":8080"),
			ReadTimeoutSec:  getEnvAsInt("SERVER_READ_TIMEOUT", 15),
			WriteTimeoutSec: getEnvAsInt("SERVER_WRITE_TIMEOUT", 15),
		},
		WebSocket: WebSocketConfig{
			ReadBufferSize:   getEnvAsInt("WS_READ_BUFFER", 1024),
			WriteBufferSize:  getEnvAsInt("WS_WRITE_BUFFER", 1024),
			AllowedOrigins:   getEnvAsSlice("WS_ALLOWED_ORIGINS", "*"),
			PingIntervalSec:  getEnvAsInt("WS_PING_INTERVAL", 30),
			PongWaitSec:      getEnvAsInt("WS_PONG_WAIT", 60),
			WriteWaitSec:     getEnvAsInt("WS_WRITE_WAIT", 10),
			MaxMessageSizeKB: getEnvAsInt("WS_MAX_MESSAGE_SIZE", 512),
		},
		RateLimit: RateLimitConfig{
			Requests: getEnvAsInt("RATE_LIMIT_REQUESTS", 100),
			Duration: time.Duration(getEnvAsInt("RATE_LIMIT_DURATION", 60)) * time.Second,
		},
		Room: RoomConfig{
			CleanupTimeoutSec: getEnvAsInt("ROOM_CLEANUP_TIMEOUT", 30),
			MaxClients:        getEnvAsInt("ROOM_MAX_CLIENTS", 100),
		},
	}
}

// Helper function to get an environment variable with a fallback
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

// Helper function to get an environment variable as an integer
func getEnvAsInt(key string, fallback int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		} else {
			slog.Warn("Invalid integer environment variable", "key", key, "value", value, "error", err)
		}
	}
	return fallback
}

// Helper function to get an environment variable as a string slice
func getEnvAsSlice(key string, fallback string) []string {
	if value, exists := os.LookupEnv(key); exists {
		return strings.Split(value, ",")
	}
	return []string{fallback}
} 