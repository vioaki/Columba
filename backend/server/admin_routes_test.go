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

func TestMigrationReportIncludesDisabledChannelReasons(t *testing.T) {
	s, _ := newAdminTestServer(t)
	report := []string{"Channel custom disabled: unsupported native protocol"}
	if err := s.store.SetSetting(t.Context(), "migration-report", report); err != nil {
		t.Fatal(err)
	}
	c, rec := adminTestContext("GET", "/api/admin/migration-report", "")
	s.adminMigrationReport(t.Context(), c)
	flushTestResponse(c)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), report[0]) {
		t.Fatal(rec.Code, rec.Body.String())
	}
}
