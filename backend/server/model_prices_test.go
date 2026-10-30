package server

import (
	"columba.local/backend/internal/billing"
	"columba.local/backend/storage"
	"encoding/json"
	"testing"
)

func TestPricesUseExactCatalogIdentityAndPreserveOverrides(t *testing.T) {
	s, _ := newAdminTestServer(t)
	seedNative(t, s, "channel", "openai", "https://example.com")
	raw := []byte(`{"vendor":{"models":{"raw-model":{"id":"raw-model","reasoning":true,"cost":{"input":0,"output":2,"cache_read":0.1,"tiers":[{"input":4,"output":8,"tier":{"type":"context","size":272000}}],"context_over_200k":{"input":99}}},"Raw-Model":{"id":"Raw-Model","cost":{"input":7,"output":9}}}}}`)
	d, err := parseCatalogDataset(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.exact) != 2 {
		t.Fatal("boolean reasoning or case-sensitive ID lost", len(d.exact))
	}
	s.catalog.mu.Lock()
	s.catalog.exact = d.exact
	s.catalog.mu.Unlock()
	p := storage.ModelPrice{SourceID: "channel", ModelID: "raw-model", Provider: "vendor", CatalogModel: "raw-model"}
	if err := s.store.SaveModelPrice(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	_, snapshot, err := s.modelPriceSnapshot(t.Context(), "channel", "raw-model")
	if err != nil || !snapshot.Matched || *snapshot.Prices.Input != "0" || snapshot.Prices.Tiers[0].Tier.Size != 272000 {
		t.Fatal(snapshot, err)
	}
	rate := billing.Rate("1.25")
	p.Override.Input = &rate
	if err := s.store.SaveModelPrice(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	s.catalog.mu.Lock()
	s.catalog.exact[exactCatalogKey("vendor", "raw-model")].prices.Output = &rate
	s.catalog.mu.Unlock()
	_, snapshot, err = s.modelPriceSnapshot(t.Context(), "channel", "raw-model")
	if err != nil || *snapshot.Prices.Input != "1.25" || *snapshot.Prices.Tiers[0].Input != "1.25" {
		t.Fatal(snapshot, err)
	}
	p.CatalogModel = "raw-model-latest"
	if err := s.store.SaveModelPrice(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	_, snapshot, err = s.modelPriceSnapshot(t.Context(), "channel", "raw-model")
	if err != nil || snapshot.Matched || snapshot.Prices.Output != nil || *snapshot.Prices.Input != "1.25" {
		t.Fatal(snapshot, err)
	}
}
func TestPriceAPIDecimalValidation(t *testing.T) {
	for _, value := range []string{`-1`, `"NaN"`, `"1/2"`, `"1e3"`} {
		var p billing.Prices
		if err := json.Unmarshal([]byte(`{"input":`+value+`}`), &p); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}
