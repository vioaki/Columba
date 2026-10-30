package server

import (
	"context"
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"

	"columba.local/backend/config"
	"columba.local/backend/protocol/builtin"
	"columba.local/backend/storage"
)

func (s *Server) authMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		token := extractAccessToken(c)
		accessToken, ok := s.findAccessToken(token)
		message := ""
		if !ok {
			message = "Incorrect API key provided"
		} else if accessTokenHasScope(accessToken, storage.TokenScopeAgent) {
			message = "This API key is reserved for AI assistant remote control"
		}
		if message != "" {
			c.Abort()
			// Inference authentication errors use the caller's wire protocol.
			writeProtocolError(ctx, c, inputFormatFromPath(string(c.Path())), &builtin.GatewayError{
				Class: builtin.ErrorClassAuthentication, Message: message,
			})
			return
		}
		c.Set("elysiaKeyName", accessToken.Name)
		c.Set("elysiaKeyHash", shortTokenHash(token))
		c.Set("elysiaAllowedGroups", accessToken.AllowedGroups)
		c.Next(ctx)
	}
}

func accessTokenHasScope(token config.AccessToken, scope string) bool {
	return slices.Contains(token.Scopes, scope)
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

func (s *Server) tokenAllowsGroup(ctx context.Context, c *app.RequestContext, groupName string) bool {
	allowed := c.GetStringSlice("elysiaAllowedGroups")
	return len(allowed) == 0 || slices.Contains(allowed, groupName)
}
