package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"columba.local/backend/config"
)

func TestAssistantRemoteRoutesAreRemoved(t *testing.T) {
	s := &Server{config: &config.Config{}, engine: newTestEngine(t)}
	s.setupRoutes()
	for _, path := range []string{"/api/admin/agent/sessions", "/api/agent/sessions", "/mcp", "/a2a", "/.well-known/agent-card.json"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			response := httptest.NewRecorder()
			serveTestHTTP(s.engine, response, httptest.NewRequest(method, path, nil))
			if response.Code != http.StatusNotFound {
				t.Errorf("%s %s: got %d", method, path, response.Code)
			}
		}
	}
}
