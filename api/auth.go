package api

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/fuuntz/a-list-tracker/shared"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type setupRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func SessionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	user, ok, err := shared.AuthenticateRequest(r)
	if err != nil {
		log.Printf("Session lookup failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Authentication service unavailable"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authentication required"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var input loginRequest
	if !readJSON(w, r, &input) {
		return
	}

	user, token, expires, err := shared.Login(r.Context(), input.Email, input.Password)
	if err != nil {
		status := http.StatusUnauthorized
		if strings.Contains(err.Error(), "too many login attempts") {
			status = http.StatusTooManyRequests
		} else if err.Error() != "invalid email or password" {
			log.Printf("Login failed: %v", err)
			status = http.StatusInternalServerError
		}
		message := "Invalid email or password"
		if status == http.StatusTooManyRequests {
			message = "Too many login attempts; try again later"
		} else if status == http.StatusInternalServerError {
			message = "Unable to sign in"
		}
		writeJSON(w, status, map[string]string{"error": message})
		return
	}

	shared.SetSessionCookie(w, r, token, expires)
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func SetupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var input setupRequest
	if !readJSON(w, r, &input) {
		return
	}

	user, sessionToken, expires, err := shared.ConsumeAccountToken(r.Context(), input.Token, input.Password)
	if err != nil {
		status := http.StatusBadRequest
		message := err.Error()
		if errors.Is(err, shared.ErrInvalidToken) {
			message = "This setup link is invalid, expired, or has already been used"
		} else if !strings.Contains(message, "password") {
			log.Printf("Account setup failed: %v", err)
			status = http.StatusInternalServerError
			message = "Unable to set up account"
		}
		writeJSON(w, status, map[string]string{"error": message})
		return
	}

	shared.SetSessionCookie(w, r, sessionToken, expires)
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	if err := shared.InitDB(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Unable to sign out"})
		return
	}
	if cookie, err := r.Cookie(shared.SessionCookieName); err == nil {
		if err := shared.DeleteCurrentSession(r.Context(), cookie.Value); err != nil {
			log.Printf("Failed to delete session: %v", err)
		}
	}
	shared.ClearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}
