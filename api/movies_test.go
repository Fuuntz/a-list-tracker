package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCanonicalViewingKey(t *testing.T) {
	date := time.Date(2026, time.August, 8, 0, 0, 0, 0, time.UTC)
	year := 2025
	first := canonicalViewingKey("  Mission: Impossible!  ", &year, date)
	second := canonicalViewingKey("mission impossible", &year, date)
	if first != second {
		t.Fatalf("expected normalized titles to match: %q != %q", first, second)
	}

	differentDate := canonicalViewingKey("Mission Impossible", &year, date.AddDate(0, 0, 1))
	if first == differentDate {
		t.Fatal("expected separate watched dates to produce separate keys")
	}
}

func TestViewingStatuses(t *testing.T) {
	for _, status := range []string{"unreviewed", "a_list", "not_a_list", "excluded"} {
		if !validViewingStatus(status) {
			t.Fatalf("expected %q to be valid", status)
		}
	}
	if validViewingStatus("A-List") {
		t.Fatal("expected an unknown status to be rejected")
	}
}

func TestProtectUnsafeRejectsCrossSiteRequest(t *testing.T) {
	called := false
	handler := ProtectUnsafe(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	request := httptest.NewRequest(http.MethodPost, "https://tracker.example/api/mark", nil)
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", response.Code)
	}
	if called {
		t.Fatal("expected protected handler not to be called")
	}
}
