package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayRejectsInvalidAccountIDWithoutGCRequest(t *testing.T) {
	state := newAgentState()
	gateway := newGCGateway(state)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/players/{accountID}/matches", gateway.playerHistoryHandler)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/players/nope/matches", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if state.snapshot().Details.GCRequestsTotal != 0 {
		t.Fatal("invalid input must not consume a GC request")
	}
}

func TestGatewayReturnsUnavailableWithoutGCSession(t *testing.T) {
	state := newAgentState()
	gateway := newGCGateway(state)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/matches/{matchID}", gateway.matchDetailsHandler)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/matches/123", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}
