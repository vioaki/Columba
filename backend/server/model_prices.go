package server

import (
	"columba.local/backend/internal/billing"
	"columba.local/backend/storage"
	"context"
	"github.com/cloudwego/hertz/pkg/app"
	"time"
)

type priceSnapshot struct {
	Prices       billing.Prices `json:"prices"`
	Provider     string         `json:"provider"`
	CatalogModel string         `json:"catalogModel"`
	Matched      bool           `json:"matched"`
	UpdatedAt    time.Time      `json:"updatedAt"`
	Manual       billing.Prices `json:"manual"`
}

func (s *Server) modelPriceSnapshot(ctx context.Context, source, model string) (storage.ModelPrice, priceSnapshot, error) {
	p, err := s.store.ModelPrice(ctx, source, model)
	snapshot := priceSnapshot{Provider: p.Provider, CatalogModel: p.CatalogModel, Manual: p.Override}
	if err != nil {
		return p, snapshot, err
	}
	if s.catalog != nil {
		s.catalog.mu.RLock()
		if e := s.catalog.exact[exactCatalogKey(p.Provider, p.CatalogModel)]; e != nil {
			snapshot.Prices = e.prices
			snapshot.Matched = true
		}
		snapshot.UpdatedAt = s.catalog.lastSync
		s.catalog.mu.RUnlock()
	}
	snapshot.Prices = snapshot.Prices.Overlay(p.Override)
	return p, snapshot, nil
}
func (s *Server) adminModelPrice(ctx context.Context, c *app.RequestContext) {
	p, snapshot, err := s.modelPriceSnapshot(ctx, c.Query("sourceId"), c.Query("modelId"))
	if err != nil {
		respondFail(ctx, c, 500, "price_failed", err.Error())
		return
	}
	respondOK(ctx, c, map[string]any{"config": p, "snapshot": snapshot})
}
func (s *Server) adminSaveModelPrice(ctx context.Context, c *app.RequestContext) {
	var p storage.ModelPrice
	if err := bindJSON(ctx, c, &p); err != nil {
		respondFail(ctx, c, 400, "invalid_price", err.Error())
		return
	}
	if err := s.store.SaveModelPrice(ctx, p); err != nil {
		respondFail(ctx, c, 400, "invalid_price", err.Error())
		return
	}
	respondOK(ctx, c, p)
}
