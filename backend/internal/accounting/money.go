// Package accounting implements the BranchLedger posting engine: the authoritative
// double-entry rules described in BranchLedger_System_Documentation section 5
// (System Design) and the worked examples in sections 5.5 and 5.12.
package accounting

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// Money is an exact decimal amount in a specific currency. The architecture doc
// requires "positive exact decimal" handling and forbids mixing currencies by
// adding original amounts directly, so every amount is always paired with its
// currency code.
type Money struct {
	Amount   decimal.Decimal
	Currency string // ISO 4217 code, e.g. "USD"
}

// NewMoney builds a Money value, rejecting NaN/Inf-style inputs is handled by the
// caller's decimal parsing; this constructor only normalises scale.
func NewMoney(amount decimal.Decimal, currency string) Money {
	return Money{Amount: amount, Currency: currency}
}

// Zero reports whether the amount is exactly zero.
func (m Money) Zero() bool { return m.Amount.IsZero() }

// Negative reports whether the amount is less than zero.
func (m Money) Negative() bool { return m.Amount.IsNegative() }

// Add returns m + other. Both must share the same currency; mixing currencies by
// addition is explicitly disallowed per the architecture's foreign-currency rule.
func (m Money) Add(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("accounting: cannot add %s to %s directly", other.Currency, m.Currency)
	}
	return Money{Amount: m.Amount.Add(other.Amount), Currency: m.Currency}, nil
}

// Sub returns m - other, same-currency rule as Add.
func (m Money) Sub(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("accounting: cannot subtract %s from %s directly", other.Currency, m.Currency)
	}
	return Money{Amount: m.Amount.Sub(other.Amount), Currency: m.Currency}, nil
}

// ToReporting converts an original-currency amount into the company reporting
// currency using "reporting units per original unit", matching the architecture's
// foreign currency example (100 at rate 2 posts 200 reporting units).
func (m Money) ToReporting(reportingCurrency string, reportingUnitsPerUnit decimal.Decimal) Money {
	if m.Currency == reportingCurrency {
		return m
	}
	return Money{
		Amount:   m.Amount.Mul(reportingUnitsPerUnit).Round(2),
		Currency: reportingCurrency,
	}
}
