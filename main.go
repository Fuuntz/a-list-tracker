package main

import (
	"log"
	"net/http"
	"os"

	"github.com/fuuntz/a-list-tracker/api"
)

func main() {
	// Serve static files from the public directory
	fs := http.FileServer(http.Dir("public"))
	http.Handle("/", fs)

	// API endpoints
	http.HandleFunc("/api/movies", api.MoviesHandler)
	http.HandleFunc("/api/settings", api.SettingsHandler)
	http.HandleFunc("/api/mark", api.MarkHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000" // Default for local development
	}

	log.Printf("Server listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
