package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/fuuntz/a-list-tracker/api"
)

//go:embed public
var publicFiles embed.FS

func newHandler() http.Handler {
	publicFS, err := fs.Sub(publicFiles, "public")
	if err != nil {
		panic("failed to load embedded public files: " + err.Error())
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/movies", api.MoviesHandler)
	mux.HandleFunc("/api/settings", api.SettingsHandler)
	mux.HandleFunc("/api/mark", api.MarkHandler)
	mux.Handle("/", http.FileServer(http.FS(publicFS)))

	return mux
}

func main() {

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000" // Default for local development
	}

	log.Printf("Server listening on port %s", port)
	if err := http.ListenAndServe(":"+port, newHandler()); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
