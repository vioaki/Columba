package storage

import (
	"columba.local/backend/internal/billing"
	"context"
	"encoding/json"
	"errors"
)

type ModelPrice struct {
	SourceID     string         `json:"sourceId"`
	ModelID      string         `json:"modelId"`
	Provider     string         `json:"provider"`
	CatalogModel string         `json:"catalogModel"`
	Override     billing.Prices `json:"override"`
}

func modelPriceKey(source, model string) string {
	raw, _ := json.Marshal([]string{source, model})
	return "model-price:" + string(raw)
}
func (s *Store) ModelPrice(ctx context.Context, source, model string) (ModelPrice, error) {
	p := ModelPrice{SourceID: source, ModelID: model}
	_, err := s.GetSetting(ctx, modelPriceKey(source, model), &p)
	return p, err
}
func (s *Store) SaveModelPrice(ctx context.Context, p ModelPrice) error {
	if p.SourceID == "" || p.ModelID == "" || (p.Provider == "") != (p.CatalogModel == "") {
		return errors.New("sourceId, modelId and a complete provider/catalogModel pair are required")
	}
	if err := p.Override.Validate(); err != nil {
		return err
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM models WHERE source_id=? AND id=?`, p.SourceID, p.ModelID).Scan(&exists); err != nil {
		return err
	}
	return s.SetSetting(ctx, modelPriceKey(p.SourceID, p.ModelID), p)
}
