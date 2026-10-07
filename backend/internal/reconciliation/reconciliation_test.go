package reconciliation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// fakeStore is an in-memory Store good enough to drive the service without a
// database, the same way accounting's fakeRepo does for Service.Post.
type fakeStore struct {
	accounts map[uuid.UUID]CashLikeAccount
	items    map[uuid.UUID]StatementItem
	entries  map[uuid.UUID]LedgerEntry
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		accounts: map[uuid.UUID]CashLikeAccount{},
		items:    map[uuid.UUID]StatementItem{},
		entries:  map[uuid.UUID]LedgerEntry{},
	}
}

func (f *fakeStore) ListCashLikeAccounts(ctx context.Context, companyID uuid.UUID) ([]CashLikeAccount, error) {
	var out []CashLikeAccount
	for _, a := range f.accounts {
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeStore) GetCashLikeAccount(ctx context.Context, companyID, accountID uuid.UUID) (CashLikeAccount, bool, error) {
	a, ok := f.accounts[accountID]
	return a, ok, nil
}

func (f *fakeStore) InsertStatementItem(ctx context.Context, companyID, createdBy uuid.UUID, item StatementItem) error {
	if _, exists := f.items[item.ID]; exists {
		return nil // ON CONFLICT (id) DO NOTHING
	}
	f.items[item.ID] = item
	return nil
}

func (f *fakeStore) GetStatementItem(ctx context.Context, companyID, id uuid.UUID) (StatementItem, bool, error) {
	item, ok := f.items[id]
	return item, ok, nil
}

func (f *fakeStore) ListStatementItems(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID, accountID uuid.UUID, from, to time.Time) ([]StatementItem, error) {
	var out []StatementItem
	for _, it := range f.items {
		if it.AccountID == accountID {
			out = append(out, it)
		}
	}
	return out, nil
}

func (f *fakeStore) ListLedgerEntries(ctx context.Context, companyID uuid.UUID, allowedBranches []uuid.UUID, accountID uuid.UUID, from, to time.Time) ([]LedgerEntry, error) {
	var out []LedgerEntry
	for _, e := range f.entries {
		out = append(out, e)
	}
	return out, nil
}

func (f *fakeStore) GetLedgerEntry(ctx context.Context, companyID, journalLineID uuid.UUID) (LedgerEntry, bool, error) {
	e, ok := f.entries[journalLineID]
	return e, ok, nil
}

func (f *fakeStore) SetMatch(ctx context.Context, companyID, statementItemID, journalLineID uuid.UUID) error {
	item, ok := f.items[statementItemID]
	if !ok || item.Status != "unmatched" {
		return ErrAlreadyMatched
	}
	entry, ok := f.entries[journalLineID]
	if !ok || entry.Matched {
		return ErrAlreadyMatched
	}
	item.Status = "matched"
	id := journalLineID
	item.MatchedJournalLineID = &id
	f.items[statementItemID] = item
	entry.Matched = true
	f.entries[journalLineID] = entry
	return nil
}

func (f *fakeStore) ClearMatch(ctx context.Context, companyID, statementItemID uuid.UUID) error {
	item, ok := f.items[statementItemID]
	if !ok || item.Status != "matched" {
		return ErrNotUnmatched
	}
	jlID := *item.MatchedJournalLineID
	item.Status = "unmatched"
	item.MatchedJournalLineID = nil
	f.items[statementItemID] = item
	entry := f.entries[jlID]
	entry.Matched = false
	f.entries[jlID] = entry
	return nil
}

var _ Store = (*fakeStore)(nil)

func d(s string) decimal.Decimal {
	v, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return v
}

func testScope(companyID, branchID uuid.UUID) tenancy.Scope {
	return tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleAccountant, BranchScope: []uuid.UUID{branchID}}
}

func TestAddStatementItemRejectsNonCashLikeAccount(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	companyID, branchID, accountID := uuid.New(), uuid.New(), uuid.New()
	scope := testScope(companyID, branchID)
	// accountID deliberately not registered in store.accounts.

	_, err := svc.AddStatementItem(context.Background(), scope, AddStatementItemInput{
		ID: uuid.New(), BranchID: branchID, AccountID: accountID, StatementDate: time.Now(),
		Description: "Deposit", Amount: d("100"),
	})
	if !errors.Is(err, ErrAccountNotCashLike) {
		t.Fatalf("expected ErrAccountNotCashLike, got %v", err)
	}
}

func TestAddStatementItemIsIdempotentOnRetry(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	companyID, branchID, accountID := uuid.New(), uuid.New(), uuid.New()
	store.accounts[accountID] = CashLikeAccount{ID: accountID, Code: "1000", Name: "Cash"}
	scope := testScope(companyID, branchID)

	in := AddStatementItemInput{
		ID: uuid.New(), BranchID: branchID, AccountID: accountID, StatementDate: time.Now(),
		Description: "Deposit", Amount: d("100"),
	}
	first, err := svc.AddStatementItem(context.Background(), scope, in)
	if err != nil {
		t.Fatalf("first AddStatementItem: %v", err)
	}
	second, err := svc.AddStatementItem(context.Background(), scope, in)
	if err != nil {
		t.Fatalf("retry AddStatementItem: %v", err)
	}
	if second.ID != first.ID || len(store.items) != 1 {
		t.Fatalf("retry created a second row: first=%+v second=%+v items=%d", first, second, len(store.items))
	}
}

func TestMatchRequiresEqualSignedAmount(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	companyID, branchID, accountID := uuid.New(), uuid.New(), uuid.New()
	scope := testScope(companyID, branchID)

	itemID, lineID := uuid.New(), uuid.New()
	store.items[itemID] = StatementItem{ID: itemID, BranchID: branchID, AccountID: accountID, Amount: d("100"), Status: "unmatched"}
	store.entries[lineID] = LedgerEntry{JournalLineID: lineID, BranchID: branchID, Amount: d("95")}

	err := svc.Match(context.Background(), scope, itemID, lineID)
	if !errors.Is(err, ErrAmountMismatch) {
		t.Fatalf("expected ErrAmountMismatch, got %v", err)
	}
	if store.items[itemID].Status != "unmatched" {
		t.Fatalf("a rejected match must not change status")
	}
}

func TestMatchSucceedsAndUnmatchReverts(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	companyID, branchID, accountID := uuid.New(), uuid.New(), uuid.New()
	scope := testScope(companyID, branchID)

	itemID, lineID := uuid.New(), uuid.New()
	store.items[itemID] = StatementItem{ID: itemID, BranchID: branchID, AccountID: accountID, Amount: d("-50"), Status: "unmatched"}
	store.entries[lineID] = LedgerEntry{JournalLineID: lineID, BranchID: branchID, Amount: d("-50")}

	if err := svc.Match(context.Background(), scope, itemID, lineID); err != nil {
		t.Fatalf("Match: %v", err)
	}
	if store.items[itemID].Status != "matched" || !store.entries[lineID].Matched {
		t.Fatalf("match did not update both sides: item=%+v entry=%+v", store.items[itemID], store.entries[lineID])
	}

	// A second match attempt on the same (now-matched) item is rejected.
	if err := svc.Match(context.Background(), scope, itemID, lineID); !errors.Is(err, ErrAlreadyMatched) {
		t.Fatalf("expected ErrAlreadyMatched on a re-match, got %v", err)
	}

	if err := svc.Unmatch(context.Background(), scope, itemID); err != nil {
		t.Fatalf("Unmatch: %v", err)
	}
	if store.items[itemID].Status != "unmatched" || store.entries[lineID].Matched {
		t.Fatalf("unmatch did not revert both sides: item=%+v entry=%+v", store.items[itemID], store.entries[lineID])
	}
}

func TestAutoMatchPairsOnlyExactAmountsOnceEach(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	companyID, branchID, accountID := uuid.New(), uuid.New(), uuid.New()
	store.accounts[accountID] = CashLikeAccount{ID: accountID, Code: "1000", Name: "Cash"}
	scope := testScope(companyID, branchID)

	// Two unmatched items at 100, two unmatched entries at 100 and one at 75
	// — auto-match should pair both 100s one-to-one and leave the 75
	// unmatched entry and nothing to pair it with.
	item1, item2 := uuid.New(), uuid.New()
	store.items[item1] = StatementItem{ID: item1, BranchID: branchID, AccountID: accountID, Amount: d("100"), Status: "unmatched"}
	store.items[item2] = StatementItem{ID: item2, BranchID: branchID, AccountID: accountID, Amount: d("100"), Status: "unmatched"}
	line1, line2, line3 := uuid.New(), uuid.New(), uuid.New()
	store.entries[line1] = LedgerEntry{JournalLineID: line1, BranchID: branchID, Amount: d("100")}
	store.entries[line2] = LedgerEntry{JournalLineID: line2, BranchID: branchID, Amount: d("100")}
	store.entries[line3] = LedgerEntry{JournalLineID: line3, BranchID: branchID, Amount: d("75")}

	matched, err := svc.AutoMatch(context.Background(), scope, accountID, time.Now().AddDate(0, 0, -1), time.Now())
	if err != nil {
		t.Fatalf("AutoMatch: %v", err)
	}
	if matched != 2 {
		t.Fatalf("expected 2 pairs, got %d", matched)
	}
	if store.items[item1].Status != "matched" || store.items[item2].Status != "matched" {
		t.Fatalf("both 100-amount items should be matched: %+v %+v", store.items[item1], store.items[item2])
	}
	if store.entries[line3].Matched {
		t.Fatalf("the 75-amount entry has no matching item and must stay unmatched")
	}
	usedLines := 0
	for _, l := range []uuid.UUID{line1, line2} {
		if store.entries[l].Matched {
			usedLines++
		}
	}
	if usedLines != 2 {
		t.Fatalf("expected both 100-amount entries matched exactly once, got %d", usedLines)
	}
}
