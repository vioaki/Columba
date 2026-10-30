package server

import (
	"columba.local/backend/config"
	"encoding/json"
	"github.com/cloudwego/hertz/pkg/app"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func startupFixtureConfig(t *testing.T, directory string) *config.Config {
	t.Helper()
	path := filepath.Join(directory, "config.json")
	raw, _ := json.Marshal(map[string]any{"databasePath": filepath.Join(directory, "test.sqlite3"), "modelCatalog": map[string]any{"enabled": false}, "openBrowserOnStart": false})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
func newAdminTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := startupFixtureConfig(t, dir)
	s := New(cfg)
	if s.startupErr != nil {
		t.Fatal(s.startupErr)
	}
	s.engine = newTestEngine(t)
	s.skipOutboundValidation = true
	t.Cleanup(s.doShutdown)
	return s, filepath.Join(dir, "config.json")
}
func adminTestContext(method, target, body string) (*app.RequestContext, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c := newTestContext(rec)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	setTestRequest(c, req)
	return c, rec
}
