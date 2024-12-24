package main

import (
	"log"
	"net/http"
	"notkyloren/rayy/internal/routes"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("Starting WebSocket server...")

	// Initialize router
	router := routes.NewRouter()
	router.SetupRoutes()

	// Start server
	log.Println("Server running on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
