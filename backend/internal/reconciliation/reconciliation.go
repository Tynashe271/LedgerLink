// Package reconciliation implements the Reconciliation screen (System
// Documentation 5.2: "Match statement items and resolve" / "Matched and
// outstanding entries"; FR06: "Cash and bank reconciliation ... matches and
// unresolved differences are reported"). A statement_item is a line typed in
// from an actual bank/mobile-money statement or cash count; it is matched
// against one posted journal_lines row on the same cash-like account. This
// is a side ledger of matches only — unlike internal/accounting, nothing
// here ever creates, changes or reverses a journal.
package reconciliation

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

var (
	// ErrAccountNotCashLike covers a statement item or match attempted
	// against an account that isn't flagged is_cash_like — reconciliation is
	// scoped to cash/bank/mobile-money accounts only (accounts.is_cash_like,
	// "for close calculations" per its own column comment).
	ErrAccountNotCashLike = errors.New("reconciliation: account is not a cash-like account")
	ErrOutOfScope         = errors.New("reconciliation: branch or account is outside the caller's scope")
	// ErrAlreadyMatched covers matching a statement item or ledger entry
	// that already has a match — Unmatch first.
	ErrAlreadyMatched = errors.New("reconciliation: already matched")
	ErrNotUnmatched   = errors.New("reconciliation: not currently matched")
	// ErrAmountMismatch is returned when a manual match's statement item and
	// ledger entry amounts differ — reconciliation never force-matches
	// mismatched amounts; a real difference stays outstanding until
	// investigated.
	ErrAmountMismatch = errors.New("reconciliation: statement item and ledger entry amounts do not match")
	ErrNotFound       = errors.New("reconciliation: not found")
)

// CashLikeAccount is one of the company's accounts.is_cash_like accounts,
// for the account picker.
type CashLikeAccount struct {
	ID   uuid.UUID
	Code string
	Name string
}

// StatementItem is one line entered from a statement or cash count.
type StatementItem struct {
	ID                   uuid.UUID
	BranchID             uuid.UUID
	BranchName           string
	AccountID            uuid.UUID
	StatementDate        time.Time
	Description          string
	Amount               decimal.Decimal // signed: + inflow, - outflow
	ExternalReference    string
	Status               string // "unmatched" | "matched"
	MatchedJournalLineID *uuid.UUID
	CreatedAt            time.Time
}

// LedgerEntry is one posted journal_lines row on a cash-like account, signed
// from the account's own perspective (an asset account's normal debit
// balance: debit = +, credit = -), the candidate side of a match.
type LedgerEntry struct {
	JournalLineID uuid.UUID
	TransactionID uuid.UUID
	BranchID      uuid.UUID
	BranchName    string
	DocumentType  string
	DocumentDate  time.Time
	Description   string
	Amount        decimal.Decimal
	Matched       bool
}

// AddStatementItemInput is what the HTTP handler gathers before calling
// Service.AddStatementItem.
type AddStatementItemInput struct {
	// ID is client-generated (the same "stable ID" idempotency pattern the
	// rest of the system uses for operation_id), so a retried submit is a
	// no-op rather than a duplicate line.
	ID                uuid.UUID
	BranchID          uuid.UUID
	AccountID         uuid.UUID
	StatementDate     time.Time
	Description       string
	Amount            decimal.Decimal
	ExternalReference string
}

// Store is the persistence this service needs from Postgres.
type Store interface {
	ListCashLikeAccounts(ctx context.Context, companyID uuid.UUID) ([]CashLikeAccount, error)
	// GetCashLikeAccount resolves one account by ID, scoped to the company,
	// returning ok=false if it doesn't exist, isn't this company's, or isn't
	// flagged is_cash_like.
	GetCashLikeAccount(ctx context.Context, companyID, accountID uuid.UUID) (CashLikeAccount, bool, error)

	// InsertStatementItem is an idempotent insert keyed by the
	// client-generated ID (INSERT ... ON CONFLICT (id) DO NOTHING) — a retry
	// of the same ID is a no-op, not a duplicate line.
	InsertStatementItem(ctx context.Context, companyID uuid.UUID, createdBy uuid.UUID, item StatementItem) error
	GetStatementItem(ctx context.Context, companyID, id uuid.UUID) (StatementItem, bool, error)
	ListStatementItems(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID, accountID uuid.UUID, from, to time.Time) ([]StatementItem, error)

	ListLedgerEntries(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID, accountID uuid.UUID, from, to time.Time) ([]LedgerEntry, error)
	GetLedgerEntry(ctx context.Context, companyID, journalLineID uuid.UUID) (LedgerEntry, bool, error)

	// SetMatch links a statement item to a journal line (both must currently
	// be unmatched; the UNIQUE(matched_journal_line_id) constraint is the
	// final backstop against a race matching the same ledger entry twice).
	SetMatch(ctx context.Context, companyID, statementItemID, journalLineID uuid.UUID) error
	ClearMatch(ctx context.Context, companyID, statementItemID uuid.UUID) error
}

type Service struct {
	store Store
}

func NewService(store Store) *Service { return &Service{store: store} }

// CashLikeAccounts lists the company's reconciliation-eligible accounts, for
// the account picker.
func (s *Service) CashLikeAccounts(ctx context.Context, scope tenancy.Scope) ([]CashLikeAccount, error) {
	return s.store.ListCashLikeAccounts(ctx, scope.CompanyID)
}

// AddStatementItem records one statement/cash-count line in the caller's
// scope, rejecting an account that isn't cash-like.
func (s *Service) AddStatementItem(ctx context.Context, scope tenancy.Scope, in AddStatementItemInput) (StatementItem, error) {
	if err := scope.RequireBranch(in.BranchID); err != nil {
		return StatementItem{}, ErrOutOfScope
	}
	if _, ok, err := s.store.GetCashLikeAccount(ctx, scope.CompanyID, in.AccountID); err != nil {
		return StatementItem{}, err
	} else if !ok {
		return StatementItem{}, ErrAccountNotCashLike
	}

	item := StatementItem{
		ID: in.ID, BranchID: in.BranchID, AccountID: in.AccountID, StatementDate: in.StatementDate,
		Description: in.Description, Amount: in.Amount, ExternalReference: in.ExternalReference,
		Status: "unmatched",
	}
	if err := s.store.InsertStatementItem(ctx, scope.CompanyID, scope.UserID, item); err != nil {
		return StatementItem{}, err
	}
	saved, ok, err := s.store.GetStatementItem(ctx, scope.CompanyID, in.ID)
	if err != nil {
		return StatementItem{}, err
	}
	if !ok {
		return StatementItem{}, ErrNotFound
	}
	return saved, nil
}

// Overview returns both sides of the reconciliation view for one account —
// every statement item and every ledger entry in range, matched and
// outstanding alike (required state: "Matched and outstanding entries").
func (s *Service) Overview(ctx context.Context, scope tenancy.Scope, accountID uuid.UUID, from, to time.Time) ([]StatementItem, []LedgerEntry, error) {
	if _, ok, err := s.store.GetCashLikeAccount(ctx, scope.CompanyID, accountID); err != nil {
		return nil, nil, err
	} else if !ok {
		return nil, nil, ErrAccountNotCashLike
	}
	items, err := s.store.ListStatementItems(ctx, scope.CompanyID, scope.BranchScope, accountID, from, to)
	if err != nil {
		return nil, nil, err
	}
	entries, err := s.store.ListLedgerEntries(ctx, scope.CompanyID, scope.BranchScope, accountID, from, to)
	if err != nil {
		return nil, nil, err
	}
	return items, entries, nil
}

// Match links a statement item to a ledger entry — both must be currently
// unmatched, in the caller's scope, and the same signed amount; a real
// difference is never force-matched, only left outstanding for the user to
// investigate (e.g. record a missing bank fee as an expense).
func (s *Service) Match(ctx context.Context, scope tenancy.Scope, statementItemID, journalLineID uuid.UUID) error {
	item, ok, err := s.store.GetStatementItem(ctx, scope.CompanyID, statementItemID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	if err := scope.RequireBranch(item.BranchID); err != nil {
		return ErrOutOfScope
	}
	if item.Status != "unmatched" {
		return ErrAlreadyMatched
	}

	entry, ok, err := s.store.GetLedgerEntry(ctx, scope.CompanyID, journalLineID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	if err := scope.RequireBranch(entry.BranchID); err != nil {
		return ErrOutOfScope
	}
	if entry.Matched {
		return ErrAlreadyMatched
	}
	if !item.Amount.Equal(entry.Amount) {
		return ErrAmountMismatch
	}

	return s.store.SetMatch(ctx, scope.CompanyID, statementItemID, journalLineID)
}

// Unmatch undoes a match, returning both sides to outstanding.
func (s *Service) Unmatch(ctx context.Context, scope tenancy.Scope, statementItemID uuid.UUID) error {
	item, ok, err := s.store.GetStatementItem(ctx, scope.CompanyID, statementItemID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	if err := scope.RequireBranch(item.BranchID); err != nil {
		return ErrOutOfScope
	}
	if item.Status != "matched" {
		return ErrNotUnmatched
	}
	return s.store.ClearMatch(ctx, scope.CompanyID, statementItemID)
}

// AutoMatch pairs every unmatched statement item in range with an unmatched
// ledger entry of the exact same signed amount on the same account,
// greedily and in statement-item order. It returns how many pairs it made;
// anything left unmatched needs a manual Match or investigation — the
// "resolve" half of "Match statement items and resolve".
func (s *Service) AutoMatch(ctx context.Context, scope tenancy.Scope, accountID uuid.UUID, from, to time.Time) (int, error) {
	items, entries, err := s.Overview(ctx, scope, accountID, from, to)
	if err != nil {
		return 0, err
	}

	// usedEntries tracks ledger entries this pass has already claimed, so
	// two statement items with the same amount don't both grab the one
	// candidate entry before either write lands.
	usedEntries := map[uuid.UUID]bool{}
	matched := 0
	for _, item := range items {
		if item.Status != "unmatched" {
			continue
		}
		for _, entry := range entries {
			if entry.Matched || usedEntries[entry.JournalLineID] {
				continue
			}
			if !entry.Amount.Equal(item.Amount) {
				continue
			}
			if err := s.store.SetMatch(ctx, scope.CompanyID, item.ID, entry.JournalLineID); err != nil {
				return matched, err
			}
			usedEntries[entry.JournalLineID] = true
			matched++
			break
		}
	}
	return matched, nil
}
