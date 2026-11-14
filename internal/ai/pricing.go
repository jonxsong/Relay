package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
)

const tokensPerMillion = int64(1_000_000)

type ModelPrice struct {
	InputMicrousdPerMillion  int64 `json:"input_microusd_per_million"`
	OutputMicrousdPerMillion int64 `json:"output_microusd_per_million"`
}

type PricingCatalog struct {
	Version string                `json:"version"`
	Models  map[string]ModelPrice `json:"models"`
}

func LoadPricingCatalog(path string) (*PricingCatalog, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pricing catalog: %w", err)
	}
	var catalog PricingCatalog
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return nil, fmt.Errorf("decode pricing catalog: %w", err)
	}
	if strings.TrimSpace(catalog.Version) == "" {
		return nil, errors.New("pricing catalog version is required")
	}
	if catalog.Models == nil {
		catalog.Models = map[string]ModelPrice{}
	}
	for model, price := range catalog.Models {
		if strings.TrimSpace(model) == "" || price.InputMicrousdPerMillion < 0 || price.OutputMicrousdPerMillion < 0 {
			return nil, errors.New("pricing catalog contains an invalid model price")
		}
	}
	return &catalog, nil
}

func (c *PricingCatalog) Cost(model string, inputTokens, outputTokens int64) (*int64, *string) {
	if c == nil || inputTokens < 0 || outputTokens < 0 {
		return nil, nil
	}
	price, exists := c.Models[model]
	if !exists {
		return nil, nil
	}
	total := new(big.Int)
	total.Add(total, new(big.Int).Mul(big.NewInt(inputTokens), big.NewInt(price.InputMicrousdPerMillion)))
	total.Add(total, new(big.Int).Mul(big.NewInt(outputTokens), big.NewInt(price.OutputMicrousdPerMillion)))
	total.Quo(total, big.NewInt(tokensPerMillion))
	if !total.IsInt64() {
		return nil, nil
	}
	cost := total.Int64()
	version := c.Version
	return &cost, &version
}
