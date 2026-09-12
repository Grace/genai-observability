package pricing

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// Catalog is deliberately versioned. Production deployments should replace the
// sample entries with rates verified for the models and region they actually use.
type Catalog struct {
	Version  string           `json:"version"`
	Currency string           `json:"currency"`
	Unit     string           `json:"unit"`
	Models   map[string]Rates `json:"models"`
}

type Rates struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

type Cost struct {
	USD            float64 `json:"usd"`
	PricingVersion string  `json:"pricing_version"`
}

//go:embed catalog.json
var embeddedCatalog []byte

func DefaultCatalog() (Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(embeddedCatalog, &c); err != nil {
		return Catalog{}, err
	}
	return c, nil
}

func Calculate(c Catalog, model string, inputTokens, outputTokens int64) (Cost, error) {
	r, ok := c.Models[model]
	if !ok {
		return Cost{}, fmt.Errorf("no pricing entry for model %q in catalog %s", model, c.Version)
	}
	usd := (float64(inputTokens)/1_000_000.0)*r.Input + (float64(outputTokens)/1_000_000.0)*r.Output
	return Cost{USD: usd, PricingVersion: c.Version}, nil
}

func ParseCatalogJSON(raw string) (Catalog, error) {
	var c Catalog
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Catalog{}, err
	}
	if c.Version == "" || len(c.Models) == 0 {
		return Catalog{}, fmt.Errorf("pricing catalog requires version and at least one model")
	}
	return c, nil
}
