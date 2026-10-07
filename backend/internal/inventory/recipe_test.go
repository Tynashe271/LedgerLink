package inventory

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// TestRestaurantRecipeWorkedExample reproduces section 5.7: one portion uses
// 0.20 kg rice and 0.15 kg chicken; ten portions imply 2 kg rice and 1.5 kg
// chicken; at 1.50/kg and 4/kg the expected cost is 9.
func TestRestaurantRecipeWorkedExample(t *testing.T) {
	rice := uuid.New()
	chicken := uuid.New()

	lines := []RecipeLine{
		{IngredientProductID: rice, QuantityPerServing: d("0.20")},
		{IngredientProductID: chicken, QuantityPerServing: d("0.15")},
	}

	usage := ExpectedUsage(lines, d("10"))
	want := map[uuid.UUID]decimal.Decimal{rice: d("2"), chicken: d("1.5")}
	for _, u := range usage {
		if !u.Quantity.Equal(want[u.IngredientProductID]) {
			t.Errorf("usage for %s: want %s, got %s", u.IngredientProductID, want[u.IngredientProductID], u.Quantity)
		}
	}

	cost := ExpectedCost(usage, map[uuid.UUID]decimal.Decimal{rice: d("1.50"), chicken: d("4")})
	if !cost.Equal(d("9")) {
		t.Errorf("expected cost: want 9, got %s", cost)
	}
}
