package inventory

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal {
	v, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return v
}

// TestWeightedAverageWorkedExample reproduces BranchLedger_System_Documentation
// section 5.5: "Ten units at 5 and ten at 7 give a cost pool of 120 for 20
// units, average 6. Selling four units produces cost of sales 24 and
// remaining valuation 96."
func TestWeightedAverageWorkedExample(t *testing.T) {
	pos := Position{}

	pos, err := pos.Receive(d("10"), d("5"))
	if err != nil {
		t.Fatalf("receive 1: %v", err)
	}
	pos, err = pos.Receive(d("10"), d("7"))
	if err != nil {
		t.Fatalf("receive 2: %v", err)
	}

	if !pos.Value.Equal(d("120")) {
		t.Errorf("cost pool: want 120, got %s", pos.Value)
	}
	if !pos.UnitCost().Equal(d("6")) {
		t.Errorf("average unit cost: want 6, got %s", pos.UnitCost())
	}

	pos, issuedCost, err := pos.Issue(d("4"))
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !issuedCost.Equal(d("24")) {
		t.Errorf("cost of sales: want 24, got %s", issuedCost)
	}
	if !pos.Value.Equal(d("96")) {
		t.Errorf("remaining valuation: want 96, got %s", pos.Value)
	}
	if !pos.Quantity.Equal(d("16")) {
		t.Errorf("remaining quantity: want 16, got %s", pos.Quantity)
	}
}

// TestIssueRejectsInsufficientStock matches the architecture's "Insufficient
// stock becomes review item" rule: Issue must fail rather than go negative.
func TestIssueRejectsInsufficientStock(t *testing.T) {
	pos := Position{Quantity: d("5"), Value: d("25")}
	_, _, err := pos.Issue(d("10"))
	if err == nil {
		t.Fatal("expected ErrInsufficientStock, got nil")
	}
}

func TestCountAdjustment(t *testing.T) {
	pos := Position{Quantity: d("16"), Value: d("96")} // unit cost 6
	deltaQty, deltaVal := CountAdjustment(pos, d("15"))
	if !deltaQty.Equal(d("-1")) {
		t.Errorf("delta quantity: want -1, got %s", deltaQty)
	}
	if !deltaVal.Equal(d("-6")) {
		t.Errorf("delta value: want -6, got %s", deltaVal)
	}
}
