package server

import (
	"columba.local/backend/storage"
	"context"
	"errors"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"io"
	"strings"
)

func readBoundedBody(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("request body is too large")
	}
	return body, nil
}

func (s *Server) listModels(ctx context.Context, c *app.RequestContext) {
	models, err := s.nativeModels(ctx)
	if err != nil {
		nativeError(c, 503, "cannot load models")
		return
	}
	items := []utils.H{}
	seen := map[string]bool{}
	for _, m := range models {
		if !seen[m.ID] {
			seen[m.ID] = true
			items = append(items, utils.H{"id": m.ID, "object": "model", "created": 0, "owned_by": "columba"})
		}
	}
	c.JSON(200, utils.H{"object": "list", "data": items})
}
func (s *Server) listGeminiModels(ctx context.Context, c *app.RequestContext) {
	models, err := s.nativeModels(ctx)
	if err != nil {
		nativeError(c, 503, "cannot load models")
		return
	}
	items := []utils.H{}
	for _, m := range models {
		if m.Protocol == "gemini" {
			items = append(items, utils.H{"name": "models/" + strings.TrimPrefix(m.ID, "models/"), "displayName": m.ID, "supportedGenerationMethods": []string{"generateContent", "streamGenerateContent", "countTokens"}})
		}
	}
	c.JSON(200, utils.H{"models": items})
}
func (s *Server) adminModelRouting(ctx context.Context, c *app.RequestContext) {
	models, err := s.nativeModels(ctx)
	if err != nil {
		respondFail(ctx, c, 500, "models_failed", err.Error())
		return
	}
	policies, err := s.store.ListModelRouting(ctx)
	if err != nil {
		respondFail(ctx, c, 500, "routing_failed", err.Error())
		return
	}
	respondOK(ctx, c, utils.H{"items": models, "policies": policies})
}
func (s *Server) adminSaveModelRouting(ctx context.Context, c *app.RequestContext) {
	var p storage.ModelRouting
	if err := bindJSON(ctx, c, &p); err != nil {
		respondFail(ctx, c, 400, "invalid_json", err.Error())
		return
	}
	if err := s.store.SaveModelRouting(ctx, p); err != nil {
		respondFail(ctx, c, 400, "invalid_routing", err.Error())
		return
	}
	s.invalidateRouteCache()
	respondOK(ctx, c, p)
}
