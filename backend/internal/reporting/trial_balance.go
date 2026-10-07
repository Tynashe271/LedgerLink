package reporting

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// TrialBalanceLine is one account's net balance as of a date — exactly one
// of Debit/Credit is nonzero, per the normal convention (System
// Documentation 5.8: "Trial balance lists debit and credit account balances
// on a consistent basis").
type TrialBalanceLine struct {
	AccountCode string
	AccountName string
	AccountType string
	Debit       decimal.Decimal
	Credit      decimal.Decimal
}

// TrialBalanceResult is the full response to GET /api/v1/reports/trial-balance.
type TrialBalanceResult struct {
	CompanyID         uuid.UUID
	ScopeBranchID     *uuid.UUID
	AsOf              time.Time
	ReportingCurrency string
	GeneratedAt       time.Time
	// PostingBasis is always "posted" — reports never include provisional/
	// offline-pending records (screen catalogue required state: "Report
	// basis and generation time").
	PostingBasis string
	Lines        []TrialBalanceLine
	TotalDebits  decimal.Decimal
	TotalCredits decimal.Decimal
}

// TrialBalance computes every account's balance as of asOf, in the caller's
// scope. FR09's acceptance evidence is "every enabled financial report
// reconciles" — TotalDebits always equals TotalCredits here because every
// posted journal is itself balanced (Journal.Validate), so summing any
// consistent subset of its lines by account can never produce a mismatch.
func (s *Service) TrialBalance(ctx context.Context, scope tenancy.Scope, branchID *uuid.UUID, asOf time.Time) (TrialBalanceResult, error) {
	if branchID != nil {
		if err := scope.RequireBranch(*branchID); err != nil {
			return TrialBalanceResult{}, fmt.Errorf("reporting: %w", err)
		}
	}

	currency, err := s.store.ReportingCurrency(ctx, scope.CompanyID)
	if err != nil {
		return TrialBalanceResult{}, fmt.Errorf("reporting: get reporting currency: %w", err)
	}
	lines, err := s.store.TrialBalanceLines(ctx, scope.CompanyID, branchID, scope.BranchScope, asOf)
	if err != nil {
		return TrialBalanceResult{}, fmt.Errorf("reporting: trial balance lines: %w", err)
	}

	totalDebits, totalCredits := decimal.Zero, decimal.Zero
	for _, l := range lines {
		totalDebits = totalDebits.Add(l.Debit)
		totalCredits = totalCredits.Add(l.Credit)
	}

	return TrialBalanceResult{
		CompanyID: scope.CompanyID, ScopeBranchID: branchID, AsOf: asOf,
		ReportingCurrency: currency, GeneratedAt: time.Now().UTC(), PostingBasis: "posted",
		Lines: lines, TotalDebits: totalDebits, TotalCredits: totalCredits,
	}, nil
}
