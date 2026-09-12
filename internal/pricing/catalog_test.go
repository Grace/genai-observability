package pricing

import "testing"

func TestCalculate(t *testing.T) {
	c := Catalog{Version: "v1", Models: map[string]Rates{"m": {Input: 2, Output: 10}}}
	got, err := Calculate(c, "m", 500_000, 100_000)
	if err != nil {
		t.Fatal(err)
	}
	if got.USD != 2.0 {
		t.Fatalf("got %v want 2.0", got.USD)
	}
	if got.PricingVersion != "v1" {
		t.Fatalf("unexpected pricing version %q", got.PricingVersion)
	}
}

func TestUnknownModelFailsClosed(t *testing.T) {
	_, err := Calculate(Catalog{Version: "v1", Models: map[string]Rates{}}, "missing", 1, 1)
	if err == nil {
		t.Fatal("expected missing price to be an error")
	}
}
