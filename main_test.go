package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedFrontend(t *testing.T) {
	tests := []struct {
		path        string
		contentType string
		contains    string
	}{
		{path: "/", contentType: "text/html", contains: "<title>A-List Tracker</title>"},
		{path: "/setup", contentType: "text/html", contains: "Create your password"},
		{path: "/app.js", contentType: "text/javascript", contains: "DOMContentLoaded"},
		{path: "/style.css", contentType: "text/css", contains: "--bg:"},
	}

	handler := newHandler()
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", response.Code)
			}
			if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, test.contentType) {
				t.Errorf("expected content type %q, got %q", test.contentType, contentType)
			}
			if !strings.Contains(response.Body.String(), test.contains) {
				t.Errorf("response did not contain %q", test.contains)
			}
		})
	}
}
