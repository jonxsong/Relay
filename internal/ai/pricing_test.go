package ai

import "testing"

func TestPricingCatalogComputesConfiguredMicrousd(t *testing.T) {
	catalog := &PricingCatalog{Version: "prices-1", Models: map[string]ModelPrice{
		"test-model": {InputMicrousdPerMillion: 2_000_000, OutputMicrousdPerMillion: 8_000_000},
	}}
	cost, version := catalog.Cost("test-model", 250_000, 50_000)
	if cost == nil || *cost != 900_000 || version == nil || *version != "prices-1" {
		t.Fatalf("unexpected cost=%v version=%v", cost, version)
	}
}

func TestPricingCatalogLeavesUnknownModelCostUnknown(t *testing.T) {
	catalog := &PricingCatalog{Version: "prices-1", Models: map[string]ModelPrice{}}
	if cost, version := catalog.Cost("unknown", 1, 1); cost != nil || version != nil {
		t.Fatalf("unknown model cost=%v version=%v", cost, version)
	}
}
