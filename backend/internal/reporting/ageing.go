package reporting

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// AgeingRow is one customer's outstanding balance, bucketed by how many
// days past its oldest unpaid credit sale's due date `asOf` falls (System
// Documentation 5.8: "Ageing uses invoice due date ... and groups
// outstanding amounts into current, 1 to 30, 31 to 60, 61 to 90 and over 90
// days"). The whole balance lands in one bucket — like OverdueDebtAlerts,
// this approximates which invoice(s) are unpaid rather than allocating
// settlements per invoice (not tracked in this release).
type AgeingRow struct {
	CustomerName     string
	OldestDueDate    time.Time
	OutstandingTotal decimal.Decimal
	Current          decimal.Decimal
	Days1To30        decimal.Decimal
	Days31To60       decimal.Decimal
	Days61To90       decimal.Decimal
	DaysOver90       decimal.Decimal
}

// AgeingResult is the full response to GET /api/v1/reports/ageing.
type AgeingResult struct {
	CompanyID         uuid.UUID
	ScopeBranchID     *uuid.UUID
	AsOf              time.Time
	ReportingCurrency string
	GeneratedAt       time.Time
	PostingBasis      string // always "posted"
	Rows              []AgeingRow
	TotalOutstanding  decimal.Decimal
}

// Ageing computes the debtor ageing schedule as of asOf, in the caller's
// scope.
func (s *Service) Ageing(ctx context.Context, scope tenancy.Scope, branchID *uuid.UUID, asOf time.Time) (AgeingResult, error) {
	if branchID != nil {
		if err := scope.RequireBranch(*branchID); err != nil {
			return AgeingResult{}, fmt.Errorf("reporting: %w", err)
		}
	}

	currency, err := s.store.ReportingCurrency(ctx, scope.CompanyID)
	if err != nil {
		return AgeingResult{}, fmt.Errorf("reporting: get reporting currency: %w", err)
	}
	source, err := s.store.AgeingSourceRows(ctx, scope.CompanyID, branchID, scope.BranchScope, asOf)
	if err != nil {
		return AgeingResult{}, fmt.Errorf("reporting: ageing source rows: %w", err)
	}

	total := decimal.Zero
	rows := make([]AgeingRow, 0, len(source))
	for _, src := range source {
		row := AgeingRow{CustomerName: src.CustomerName, OldestDueDate: src.OldestDueDate, OutstandingTotal: src.OutstandingAmount}
		bucketAgeing(&row, asOf, src.OldestDueDate, src.OutstandingAmount)
		rows = append(rows, row)
		total = total.Add(src.OutstandingAmount)
	}

	return AgeingResult{
		CompanyID: scope.CompanyID, ScopeBranchID: branchID, AsOf: asOf,
		ReportingCurrency: currency, GeneratedAt: time.Now().UTC(), PostingBasis: "posted",
		Rows: rows, TotalOutstanding: total,
	}, nil
}

// bucketAgeing puts the full amount into the one bucket matching how many
// days asOf falls past dueDate: not yet due (including the due date itself)
// is Current, then 1-30, 31-60, 61-90, and over 90.
func bucketAgeing(row *AgeingRow, asOf, dueDate time.Time, amount decimal.Decimal) {
	daysPastDue := int(asOf.Sub(dueDate).Hours() / 24)
	switch {
	case daysPastDue <= 0:
		row.Current = amount
	case daysPastDue <= 30:
		row.Days1To30 = amount
	case daysPastDue <= 60:
		row.Days31To60 = amount
	case daysPastDue <= 90:
		row.Days61To90 = amount
	default:
		row.DaysOver90 = amount
	}
}
