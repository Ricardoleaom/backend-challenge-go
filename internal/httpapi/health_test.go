package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthLive(t *testing.T) {
	handler := NewHandler()

	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d",
			http.StatusOK, response.Code)
	}

	if contentType := response.Header().Get("Content-Type"); contentType != "aplication/json" {
		t.Errorf("unexpected response body: %s", response.Body.String())
	}
}

func TestHealthLiveRejectsNonGet(t *testing.T) {
	handler := NewHandler()

	request := httptest.NewRequest(http.MethodPost, "/health/live", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d",
			http.StatusMethodNotAllowed, response.Code)
	}
}
