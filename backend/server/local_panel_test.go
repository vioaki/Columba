package server

import (
	"columba.local/backend/config"
	"context"
	"github.com/cloudwego/hertz/pkg/app"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"columba.local/backend/storage"
)

func TestListenerOnlyAllowsLoopback(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1", "0.0.0.0", "::", "192.168.1.2", "evil.example"} {
		cfg := &config.Config{Host: host}
		want := host == "localhost" || host == "127.0.0.1" || host == "::1"
		if (cfg.ValidatePanelAccess() == nil) != want {
			t.Fatalf("unexpected listener validation for %q", host)
		}
	}
}

func TestNetworkListenerRequiresExplicitOrigins(t *testing.T) {
	for _, tc := range []struct {
		origin string
		valid  bool
	}{
		{"http://192.168.5.48:8765", true}, {"https://columba.example", true},
		{"*", false}, {"http://", false}, {"http://user@host", false},
		{"http://host/path", false}, {"http://host?", false}, {"http://host#", false},
	} {
		cfg := &config.Config{Host: "0.0.0.0", PanelOrigins: []string{tc.origin}}
		if (cfg.ValidatePanelAccess() == nil) != tc.valid {
			t.Fatalf("unexpected panel origin validation for %q", tc.origin)
		}
	}
}

func TestNetworkPanelRequiresPasswordAndRejectsRemoteSetup(t *testing.T) {
	s, _ := newAdminTestServer(t)
	lan, tail := "192.168.5.48:8765", "100.100.245.70:8765"
	s.config.PanelOrigins = []string{"http://" + lan, "http://" + tail}
	s.setupRoutes()
	call := func(method, path, host, origin, body string, cookie *http.Cookie, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		req.RemoteAddr = "192.168.5.9:1234"
		req.Header.Set("Origin", origin)
		req.Header.Set("X-Columba-Panel", "1")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		serveTestHTTP(s.engine, rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s host=%s origin=%s: %d %s", method, path, host, origin, rec.Code, rec.Body)
		}
		return rec
	}
	password := `{"password":"test-password-123"}`
	status := call("GET", "/api/auth/session", lan, "", "", nil, 200)
	if !strings.Contains(status.Body.String(), `"setupAllowed":false`) {
		t.Fatal("remote panel offered initial password setup")
	}
	call("POST", "/api/auth/login", lan, "http://"+lan, password, nil, 403)
	call("POST", "/api/auth/login", "localhost:8765", "http://localhost:8765", password, nil, 403)
	hash, err := hashPanelPassword("test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.SetSetting(t.Context(), storage.PanelPasswordSetting, hash); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{lan, tail} {
		call("GET", "/api/admin/api-tokens", host, "", "", nil, 401)
		login := call("POST", "/api/auth/login", host, "http://"+host, password, nil, 200)
		cookie := login.Result().Cookies()[0]
		call("GET", "/api/admin/api-tokens", host, "", "", cookie, 200)
		call("POST", "/__shutdown", host, "http://"+host, "", cookie, 403)
		call("GET", "/api/admin/api-tokens", "evil.example", "", "", cookie, 403)
		for _, origin := range []string{"http://evil.example", "http://localhost:5273", "https://" + host, "http://" + host + "#"} {
			call("GET", "/api/admin/api-tokens", host, origin, "", cookie, 403)
		}
	}
	call("POST", "/api/auth/login", lan, "http://"+tail, password, nil, 403)
}

func TestLocalPanelAuthorityRejectsRebindingHosts(t *testing.T) {
	for _, host := range []string{"127.0.0.1:8765", "localhost:8765", "[::1]:8765"} {
		if !localAuthority(host) {
			t.Fatalf("local host rejected: %s", host)
		}
	}
	for _, host := range []string{"evil.example", "localhost.evil.example", "user@localhost:8765", "127.0.0.1/path", "192.168.1.2:8765", "", "localhost?x=y"} {
		if localAuthority(host) {
			t.Fatalf("untrusted host accepted: %s", host)
		}
	}
}

func TestLocalPanelAcceptsNativeAndDevelopmentRequests(t *testing.T) {
	s := &Server{}
	e := newTestEngine(t)
	e.GET("/admin", s.localPanelMiddleware(), func(_ context.Context, c *app.RequestContext) { c.Status(204) })
	web := startTestServer(e)
	defer web.Close()
	for _, tc := range []struct {
		origin, site string
		status       int
	}{
		{"", "", 204}, {web.URL, "same-origin", 204}, {"http://localhost:5273", "same-site", 204},
		{"http://evil.example", "cross-site", 403}, {"null", "", 403}, {"http://localhost:9999", "", 403}, {"", "cross-site", 403},
	} {
		request, _ := http.NewRequest("GET", web.URL+"/admin", nil)
		request.Header.Set("Origin", tc.origin)
		request.Header.Set("Sec-Fetch-Site", tc.site)
		response, err := web.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != tc.status {
			t.Fatalf("origin %q site %q: %d", tc.origin, tc.site, response.StatusCode)
		}
	}
}
