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
		local := localAuthority(host)
		configured := s.config != nil && (s.config.AllowsPanelOrigin("http://"+host) || s.config.AllowsPanelOrigin("https://"+host))
		allowed := (local || configured) && string(c.GetHeader("Sec-Fetch-Site")) != "cross-site"
		if method := string(c.Method()); method != http.MethodGet && method != http.MethodHead {
			allowed = allowed && string(c.GetHeader("X-Columba-Panel")) == "1"
		}
		if origin := string(c.GetHeader("Origin")); origin != "" {
			u, err := url.Parse(origin)
			originAllowed := false
			if err == nil && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && origin == u.Scheme+"://"+u.Host {
				originAllowed = local && u.Scheme == "http" && localAuthority(u.Host) && (u.Host == host || u.Port() == "5273" || u.Port() == "5274")
				originAllowed = originAllowed || configured && u.Host == host && s.config.AllowsPanelOrigin(origin)
			}
			allowed = allowed && originAllowed
		}
		if !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, map[string]string{"error": "management request host or origin is not allowed"})
			return
		}
		c.Next(ctx)
	}
}
