// Package inventory implements the moving weighted-average costing policy
// described in BranchLedger_System_Documentation section 5.5 ("Use a reviewed
// moving weighted-average inventory method for the initial grocery module")
// and the restaurant recipe consumption rules in section 5.7.
package inventory

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// Position is a product's running stock balance at a branch: quantity on hand
// and its total valuation, from which the moving weighted-average unit cost is
// derived as Value / Quantity.
type Position struct {
	Quantity decimal.Decimal
	Value    decimal.Decimal
}

// UnitCost returns the current moving weighted-average cost per unit. A zero
// quantity has no defined unit cost; callers must check Quantity first.
func (p Position) UnitCost() decimal.Decimal {
	if p.Quantity.IsZero() {
		return decimal.Zero
	}
	return p.Value.Div(p.Quantity).Round(4)
}

// Receive adds incoming stock at its purchase cost, folding it into the moving
// average. Matches the architecture example: ten units at 5 and ten at 7 give a
// cost pool of 120 for 20 units, average 6.
func (p Position) Receive(quantity, unitCost decimal.Decimal) (Position, error) {
	if quantity.LessThanOrEqual(decimal.Zero) {
		return Position{}, fmt.Errorf("inventory: receive quantity must be positive, got %s", quantity)
	}
	if unitCost.LessThan(decimal.Zero) {
		return Position{}, fmt.Errorf("inventory: receive unit cost must not be negative, got %s", unitCost)
	}
	addedValue := quantity.Mul(unitCost)
	return Position{
		Quantity: p.Quantity.Add(quantity),
		Value:    p.Value.Add(addedValue),
	}, nil
}

// Issue removes stock (sale, waste, recipe consumption, transfer out) at the
// current weighted-average cost and returns the updated position plus the cost
// of the issued quantity to post as cost of sales / stock loss / etc. Matches
// the architecture example: selling four of twenty units at average cost 6
// produces cost of sales 24 and remaining valuation 96.
func (p Position) Issue(quantity decimal.Decimal) (updated Position, issuedCost decimal.Decimal, err error) {
	if quantity.LessThanOrEqual(decimal.Zero) {
		return Position{}, decimal.Zero, fmt.Errorf("inventory: issue quantity must be positive, got %s", quantity)
	}
	if quantity.GreaterThan(p.Quantity) {
		return Position{}, decimal.Zero, fmt.Errorf("%w: have %s, requested %s", ErrInsufficientStock, p.Quantity, quantity)
	}
	unitCost := p.UnitCost()
	issuedCost = quantity.Mul(unitCost).Round(2)
	updated = Position{
		Quantity: p.Quantity.Sub(quantity),
		Value:    p.Value.Sub(issuedCost),
	}
	return updated, issuedCost, nil
}

// ErrInsufficientStock is returned by Issue when the requested quantity exceeds
// what is on hand. Per the architecture, insufficient stock "becomes review
// item", not a silent negative balance — callers surface this to an approval
// queue rather than posting.
var ErrInsufficientStock = fmt.Errorf("inventory: insufficient stock")

// CountAdjustment compares a physical count against the current position and
// returns the signed quantity delta and its valuation at current unit cost, for
// a stock-count reconciliation movement (section 5.7, "Stock count").
func CountAdjustment(current Position, countedQuantity decimal.Decimal) (deltaQuantity, deltaValue decimal.Decimal) {
	deltaQuantity = countedQuantity.Sub(current.Quantity)
	deltaValue = deltaQuantity.Mul(current.UnitCost()).Round(2)
	return deltaQuantity, deltaValue
}
