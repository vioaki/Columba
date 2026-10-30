package server

import (
	"columba.local/backend/config"
	"strings"
	"testing"
)

func TestAdminRoutesExcludeRetiredFeatures(t *testing.T) {
	s := &Server{config: &config.Config{}, engine: newTestEngine(t)}
	s.setupRoutes()
	for _, r := range s.engine.Routes() {
		for _, prefix := range []string{"/gateway/", "/api/admin/protocols", "/api/admin/model-groups", "/api/admin/agent", "/mcp", "/a2a"} {
			if strings.HasPrefix(r.Path, prefix) {
				t.Errorf("retired route %s", r.Path)
			}
		}
	}
}
