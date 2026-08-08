package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/fuuntz/a-list-tracker/api"
	"github.com/fuuntz/a-list-tracker/shared"
)

//go:embed public
var publicFiles embed.FS

func newHandler() http.Handler {
	publicFS, err := fs.Sub(publicFiles, "public")
	if err != nil {
		panic("failed to load embedded public files: " + err.Error())
	}

	mux := http.NewServeMux()

	mux.Handle("/api/auth/session", http.HandlerFunc(api.SessionHandler))
	mux.Handle("/api/auth/login", api.ProtectUnsafe(http.HandlerFunc(api.LoginHandler)))
	mux.Handle("/api/auth/setup", api.ProtectUnsafe(http.HandlerFunc(api.SetupHandler)))
	mux.Handle("/api/auth/logout", api.ProtectUnsafe(shared.RequireUser(http.HandlerFunc(api.LogoutHandler))))

	mux.Handle("/api/movies", shared.RequireUser(http.HandlerFunc(api.MoviesHandler)))
	mux.Handle("/api/sync", api.ProtectUnsafe(shared.RequireUser(http.HandlerFunc(api.SyncHandler))))
	mux.Handle("/api/import/preview", api.ProtectUnsafe(shared.RequireUser(http.HandlerFunc(api.ImportPreviewHandler))))
	mux.Handle("/api/import/confirm", api.ProtectUnsafe(shared.RequireUser(http.HandlerFunc(api.ImportConfirmHandler))))
	mux.Handle("/api/export", shared.RequireUser(http.HandlerFunc(api.ExportHandler)))
	mux.Handle("/api/data/reset", api.ProtectUnsafe(shared.RequireUser(http.HandlerFunc(api.ResetDataHandler))))
	mux.Handle("/api/settings", api.ProtectUnsafe(shared.RequireUser(http.HandlerFunc(api.SettingsHandler))))
	mux.Handle("/api/mark", api.ProtectUnsafe(shared.RequireUser(http.HandlerFunc(api.MarkHandler))))
	fileServer := http.FileServer(http.FS(publicFS))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/setup" {
			copy := r.Clone(r.Context())
			urlCopy := *r.URL
			urlCopy.Path = "/"
			copy.URL = &urlCopy
			fileServer.ServeHTTP(w, copy)
			return
		}
		fileServer.ServeHTTP(w, r)
	}))

	return api.SecurityHeaders(mux)
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
