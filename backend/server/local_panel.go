package server

import (
	"context"
	"net"
	"net/http"
	"net/url"

	"github.com/cloudwego/hertz/pkg/app"
)

func localAuthority(authority string) bool {
	u, err := url.Parse("http://" + authority)
	if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

// Origin validation and a custom header on writes protect cookie sessions from CSRF.
func (s *Server) localPanelMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		host := string(c.Host())
		allowed := localAuthority(host) && string(c.GetHeader("Sec-Fetch-Site")) != "cross-site"
		if method := string(c.Method()); method != http.MethodGet && method != http.MethodHead {
			allowed = allowed && string(c.GetHeader("X-Columba-Panel")) == "1"
		}
		if origin := string(c.GetHeader("Origin")); origin != "" {
			u, err := url.Parse(origin)
			allowed = allowed && err == nil && u.Scheme == "http" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && localAuthority(u.Host) && (u.Host == host || u.Port() == "5273" || u.Port() == "5274")
		}
		if !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, map[string]string{"error": "management is only available from the local Columba panel"})
			return
		}
		c.Next(ctx)
	}
}
