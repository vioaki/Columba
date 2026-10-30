package server

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

func (s *Server) authMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		token := extractAccessToken(c)
		accessToken, ok := s.findAccessToken(token)
		message := ""
		if !ok {
			message = "Incorrect API key provided"
		}
		if message != "" {
			c.Abort()
			// Inference authentication errors use the caller's wire protocol.
			nativeError(c, http.StatusUnauthorized, message)
			return
		}
		c.Set("columbaKeyName", accessToken.Name)
		c.Set("columbaKeyHash", shortTokenHash(token))
		c.Next(ctx)
	}
}

func extractAccessToken(c *app.RequestContext) string {
	r := &c.Request
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	for _, header := range []string{"x-api-key", "x-goog-api-key"} {
		if key := strings.TrimSpace(r.Header.Get(header)); key != "" {
			return key
		}
	}
	if key := strings.TrimSpace(c.Query("key")); key != "" {
		return key
	}
	return ""
}

func (s *Server) loopbackOnly() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !isLoopbackRequest(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, utils.H{
				"error": "endpoint is only available from loopback",
			})
			return
		}
		c.Next(ctx)
	}
}

func isLoopbackRequest(c *app.RequestContext) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(c.RemoteAddr().String()))
	if err != nil {
		host = strings.TrimSpace(c.RemoteAddr().String())
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
