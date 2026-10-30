package server

import (
	"columba.local/backend/config"
	"context"
	"github.com/cloudwego/hertz/pkg/app"
	"net/http"
	"testing"
)

func TestListenerOnlyAllowsLoopback(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1", "0.0.0.0", "::", "192.168.1.2", "evil.example"} {
		cfg := &config.Config{Host: host}
		want := host == "localhost" || host == "127.0.0.1" || host == "::1"
		if (cfg.ValidateLocalHost() == nil) != want {
			t.Fatalf("unexpected listener validation for %q", host)
		}
	}
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
