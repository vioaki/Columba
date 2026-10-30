package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"columba.local/backend/storage"
)

func panelRequest(t *testing.T, web *hertzTestServer, method, path, body string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, web.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Columba-Panel", "1")
	req.Header.Set("Origin", web.URL)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	r, err := web.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Body.Close() })
	return r
}

func TestPanelLoginProtectsManagementAndRevokesSessions(t *testing.T) {
	s, _ := newAdminTestServer(t)
	s.config.EnablePprof = true
	s.setupRoutes()
	web := startTestServer(s.engine)
	defer web.Close()
	call := func(method, path, body string, cookie *http.Cookie, status int) *http.Response {
		t.Helper()
		r := panelRequest(t, web, method, path, body, cookie)
		if r.StatusCode != status {
			data, _ := io.ReadAll(r.Body)
			t.Fatalf("%s %s: %d %s", method, path, r.StatusCode, data)
		}
		return r
	}
	for _, path := range []string{"/api/admin/api-tokens", "/api/admin/usage/assets/private/image", "/debug/pprof/"} {
		call("GET", path, "", nil, 401)
	}
	for _, path := range []string{"/__reload", "/__shutdown"} {
		call("POST", path, "", nil, 401)
	}
	call("POST", "/api/auth/login", `{"password":"short"}`, nil, 400)
	r := call("POST", "/api/auth/login", `{"password":"test-password-123"}`, nil, 200)
	if len(r.Cookies()) != 1 {
		t.Fatal("session cookie missing")
	}
	first := r.Cookies()[0]
	if !first.HttpOnly || first.SameSite != http.SameSiteStrictMode || first.MaxAge != 86400 || first.Path != "/" || first.Domain != "" || len(first.Value) != 64 {
		t.Fatalf("unsafe session cookie: %+v", first)
	}
	if r.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("login can be cached")
	}
	call("GET", "/api/admin/api-tokens", "", first, 200)
	call("GET", "/v1/models", "", first, 401) // A panel session is not an inference Key.
	var hash string
	if found, err := s.store.GetSetting(t.Context(), storage.PanelPasswordSetting, &hash); err != nil || !found || strings.Contains(hash, "test-password") || !checkPanelPassword(hash, "test-password-123") {
		t.Fatal("password was not hashed", err)
	}
	if saved, err := s.store.SavePanelPassword(t.Context(), "", hash); err != nil || saved {
		t.Fatal("setup overwrote an existing password", err)
	}
	call("POST", "/api/auth/login", `{"password":"wrong-password"}`, nil, 400)
	r = call("POST", "/api/auth/login", `{"password":"test-password-123"}`, first, 200)
	second := r.Cookies()[0]
	if first.Value == second.Value {
		t.Fatal("session was not rotated")
	}
	call("GET", "/api/admin/api-tokens", "", first, 401)
	call("PUT", "/api/auth/password", `{"currentPassword":"test-password-123","password":"new-password-456"}`, second, 200)
	call("GET", "/api/admin/api-tokens", "", second, 401)
	s.panel.mu.Lock()
	s.panel.attempts = nil
	s.panel.mu.Unlock()
	call("POST", "/api/auth/login", `{"password":"test-password-123"}`, nil, 400)
	r = call("POST", "/api/auth/login", `{"password":"new-password-456"}`, nil, 200)
	third := r.Cookies()[0]
	call("POST", "/api/auth/logout", "", third, 200)
	call("GET", "/api/admin/api-tokens", "", third, 401)

	// Passwords survive a new Server; in-memory sessions do not.
	fresh := New(s.config)
	if fresh.startupErr != nil {
		t.Fatal(fresh.startupErr)
	}
	t.Cleanup(fresh.doShutdown)
	fresh.engine = newTestEngine(t)
	fresh.setupRoutes()
	other := startTestServer(fresh.engine)
	defer other.Close()
	r = panelRequest(t, other, "GET", "/api/auth/session", "", second)
	var status struct {
		Data struct{ Authenticated, SetupRequired bool }
	}
	if err := json.NewDecoder(r.Body).Decode(&status); err != nil || status.Data.Authenticated || status.Data.SetupRequired {
		t.Fatal("restart lost the password or accepted an old session", err)
	}
	if r := panelRequest(t, other, "POST", "/api/auth/login", `{"password":"new-password-456"}`, nil); r.StatusCode != 200 {
		t.Fatal("persisted password rejected", r.StatusCode)
	}
	if err := fresh.store.ResetPanelPassword(t.Context()); err != nil {
		t.Fatal(err)
	}
	r = panelRequest(t, other, "GET", "/api/auth/session", "", second)
	if err := json.NewDecoder(r.Body).Decode(&status); err != nil || !status.Data.SetupRequired || status.Data.Authenticated {
		t.Fatal("local reset did not revoke authentication", err)
	}
}

func TestPanelAuthRejectsCrossSiteRemoteSetupExpiredAndCorruptState(t *testing.T) {
	s, _ := newAdminTestServer(t)
	s.setupRoutes()
	for _, tc := range []struct {
		origin, header string
		remote         bool
	}{
		{"http://evil.example", "1", false}, {"http://localhost:8765", "", false}, {"http://localhost:8765", "1", true},
	} {
		req := httptest.NewRequest("POST", "http://localhost:8765/api/auth/login", strings.NewReader(`{"password":"test-password-123"}`))
		req.RemoteAddr = "127.0.0.1:1234"
		if tc.remote {
			req.RemoteAddr = "192.168.1.9:1234"
		}
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("X-Columba-Panel", tc.header)
		rec := httptest.NewRecorder()
		serveTestHTTP(s.engine, rec, req)
		if rec.Code != 403 {
			t.Fatal("unsafe setup accepted", tc, rec.Code)
		}
	}
	hash, err := hashPanelPassword("test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.SetSetting(t.Context(), storage.PanelPasswordSetting, hash); err != nil {
		t.Fatal(err)
	}
	s.panel.sessions = map[string]panelSession{"expired": {hash, time.Now().Add(-time.Second)}}
	web := startTestServer(s.engine)
	defer web.Close()
	if r := panelRequest(t, web, "GET", "/api/admin/api-tokens", "", &http.Cookie{Name: panelCookie, Value: "expired"}); r.StatusCode != 401 {
		t.Fatal("expired session accepted")
	}
	if err := s.store.SetSetting(t.Context(), storage.PanelPasswordSetting, "corrupt"); err != nil {
		t.Fatal(err)
	}
	if r := panelRequest(t, web, "GET", "/api/auth/session", "", nil); r.StatusCode != 503 {
		t.Fatal("corruption reopened setup", r.StatusCode)
	}
	for range 5 {
		panelRequest(t, web, "POST", "/api/auth/login", `{"password":"wrong"}`, nil)
	}
	r := panelRequest(t, web, "POST", "/api/auth/login", `{"password":"wrong"}`, nil)
	if r.StatusCode != 429 || r.Header.Get("Retry-After") == "" {
		t.Fatal("password attempts not throttled", r.StatusCode)
	}
}
