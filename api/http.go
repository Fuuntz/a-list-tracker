package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func readJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return false
	}
	return true
}

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; object-src 'none'")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func ProtectUnsafe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Cross-site request rejected"})
			return
		}

		if origin := r.Header.Get("Origin"); origin != "" && !sameRequestHost(origin, r.Host) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Request origin rejected"})
			return
		}
		if r.Header.Get("Origin") == "" {
			if referer := r.Header.Get("Referer"); referer != "" && !sameRequestHost(referer, r.Host) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "Request origin rejected"})
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func sameRequestHost(rawURL, host string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && strings.EqualFold(parsed.Host, host)
}
