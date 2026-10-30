package server

import (
	"columba.local/backend/storage"
	"context"
	"crypto/subtle"
	"strings"
)

func (s *Server) invalidateRouteCache() { s.nativeScheduler.Reset() }
func (s *Server) findAccessToken(token string) (storage.APIToken, bool) {
	if s.store == nil || strings.TrimSpace(token) == "" {
		return storage.APIToken{}, false
	}
	items, err := s.store.ListAPITokens(context.Background())
	if err != nil {
		s.logEvent("error").Err(err).Msg("cannot read calling keys")
		return storage.APIToken{}, false
	}
	for _, item := range items {
		if item.Enabled && subtle.ConstantTimeCompare([]byte(item.Token), []byte(token)) == 1 {
			return item, true
		}
	}
	return storage.APIToken{}, false
}
