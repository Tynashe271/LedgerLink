package inventory

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// RecipeLine is one ingredient requirement for a single serving, versioned
// per BranchLedger_System_Documentation section 5.7 ("Version ingredient
// quantities and serving yield").
type RecipeLine struct {
	IngredientProductID uuid.UUID
	QuantityPerServing   decimal.Decimal
}

// IngredientUsage is the quantity of one ingredient implied by selling a
// number of portions under a specific recipe version.
type IngredientUsage struct {
	IngredientProductID uuid.UUID
	Quantity              decimal.Decimal
}

// ExpectedUsage expands a recipe at the given version into ingredient
// quantities for the number of portions sold. Matches the architecture's
// restaurant example: 0.20 kg rice and 0.15 kg chicken per portion, ten
// portions imply 2 kg rice and 1.5 kg chicken.
//
// This is an *expected* movement, not a replacement for physical stock counts:
// "Expected ingredient use does not replace physical stock counts" (User
// Manual, Stock and branch transfers).
func ExpectedUsage(lines []RecipeLine, portionsSold decimal.Decimal) []IngredientUsage {
	usage := make([]IngredientUsage, 0, len(lines))
	for _, line := range lines {
		usage = append(usage, IngredientUsage{
			IngredientProductID: line.IngredientProductID,
			Quantity:              line.QuantityPerServing.Mul(portionsSold),
		})
	}
	return usage
}

// ExpectedCost prices an ingredient usage list against each ingredient's
// current weighted-average unit cost. Matches the architecture example:
// rice at 1.50/kg and chicken at 4/kg for the ten-portion usage above gives
// an expected cost of 9.
func ExpectedCost(usage []IngredientUsage, unitCosts map[uuid.UUID]decimal.Decimal) decimal.Decimal {
	total := decimal.Zero
	for _, u := range usage {
		cost, ok := unitCosts[u.IngredientProductID]
		if !ok {
			continue
		}
		total = total.Add(u.Quantity.Mul(cost))
	}
	return total.Round(2)
}
