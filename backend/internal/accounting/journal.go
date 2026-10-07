package accounting

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Side is which side of a journal line an amount sits on.
type Side string

const (
	Debit  Side = "debit"
	Credit Side = "credit"
)

// JournalLine is one leg of a balanced posting. OriginalAmount is in the source
// transaction's currency; ReportingAmount is the same movement converted to the
// company reporting currency. Per the architecture, a line is one side only
// (never both debit and credit populated), and amounts are always positive.
type JournalLine struct {
	AccountID        uuid.UUID
	Side              Side
	OriginalAmount    decimal.Decimal
	ReportingAmount   decimal.Decimal
}

// Journal is the authoritative balanced posting for one source transaction.
// Posted journals are immutable; corrections use a linked reversal, never an edit.
type Journal struct {
	SourceTransactionID uuid.UUID
	ReportingCurrency    string
	Lines                 []JournalLine
}

// Validate enforces "every posted journal balances" (FR03): total debits in
// reporting currency must equal total credits in reporting currency, and every
// line must carry a strictly positive amount on exactly one side.
func (j Journal) Validate() error {
	if len(j.Lines) < 2 {
		return fmt.Errorf("accounting: journal must have at least two lines, got %d", len(j.Lines))
	}

	debitTotal := decimal.Zero
	creditTotal := decimal.Zero

	for i, line := range j.Lines {
		if line.OriginalAmount.LessThanOrEqual(decimal.Zero) {
			return fmt.Errorf("accounting: journal line %d has non-positive original amount %s", i, line.OriginalAmount)
		}
		if line.ReportingAmount.LessThanOrEqual(decimal.Zero) {
			return fmt.Errorf("accounting: journal line %d has non-positive reporting amount %s", i, line.ReportingAmount)
		}
		switch line.Side {
		case Debit:
			debitTotal = debitTotal.Add(line.ReportingAmount)
		case Credit:
			creditTotal = creditTotal.Add(line.ReportingAmount)
		default:
			return fmt.Errorf("accounting: journal line %d has invalid side %q", i, line.Side)
		}
	}

	if !debitTotal.Equal(creditTotal) {
		return fmt.Errorf("accounting: journal does not balance: debits %s != credits %s", debitTotal, creditTotal)
	}

	return nil
}

// TotalDebits sums the reporting-currency debit side. Callers typically call
// Validate first; this is provided for control-total assertions (section 5.12).
func (j Journal) TotalDebits() decimal.Decimal {
	total := decimal.Zero
	for _, l := range j.Lines {
		if l.Side == Debit {
			total = total.Add(l.ReportingAmount)
		}
	}
	return total
}

// TotalCredits sums the reporting-currency credit side.
func (j Journal) TotalCredits() decimal.Decimal {
	total := decimal.Zero
	for _, l := range j.Lines {
		if l.Side == Credit {
			total = total.Add(l.ReportingAmount)
		}
	}
	return total
}
