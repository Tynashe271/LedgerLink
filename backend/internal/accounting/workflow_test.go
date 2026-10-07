package accounting

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/inventory"
	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// fakeRepo is an in-memory Repository+Tx good enough to drive Reverse,
// SubmitClose, DecideApproval and ReceiveTransfer end to end, the same way
// reporting.fakeStore lets Service.Dashboard be tested without a database.
// WithTx gives it real rollback semantics (clone, run against the clone,
// swap back only on success) because several of the rejection paths this
// file tests specifically assert that nothing was partially written.
type fakeRepo struct {
	periodOpen     bool
	accounts       ChartAccounts
	stockPositions map[uuid.UUID]inventory.Position    // keyed by productID (single branch/company in tests)
	stockMovements map[uuid.UUID][]StockMovementRecord // keyed by source_transaction_id
	transactions   map[uuid.UUID]TransactionRecord
	journals       map[uuid.UUID]Journal // keyed by source_transaction_id
	approvals      map[uuid.UUID]ApprovalRequestRecord
	approvalLimits map[uuid.UUID]*decimal.Decimal
	transfers      map[uuid.UUID]TransferRecord
	closes         map[string]DailyCloseRecord // keyed by branchID|date|currency -> latest revision
	operations     map[string]OperationRecord  // keyed by companyID|operationID
	rejected       []OperationRecord
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		periodOpen:     true,
		accounts:       testAccounts(),
		stockPositions: map[uuid.UUID]inventory.Position{},
		stockMovements: map[uuid.UUID][]StockMovementRecord{},
		transactions:   map[uuid.UUID]TransactionRecord{},
		journals:       map[uuid.UUID]Journal{},
		approvals:      map[uuid.UUID]ApprovalRequestRecord{},
		approvalLimits: map[uuid.UUID]*decimal.Decimal{},
		transfers:      map[uuid.UUID]TransferRecord{},
		closes:         map[string]DailyCloseRecord{},
		operations:     map[string]OperationRecord{},
	}
}

func (f *fakeRepo) clone() *fakeRepo {
	c := &fakeRepo{
		periodOpen: f.periodOpen, accounts: f.accounts,
		stockPositions: map[uuid.UUID]inventory.Position{}, stockMovements: map[uuid.UUID][]StockMovementRecord{},
		transactions: map[uuid.UUID]TransactionRecord{}, journals: map[uuid.UUID]Journal{},
		approvals: map[uuid.UUID]ApprovalRequestRecord{}, approvalLimits: f.approvalLimits,
		transfers: map[uuid.UUID]TransferRecord{}, closes: map[string]DailyCloseRecord{},
		operations: map[string]OperationRecord{},
	}
	for k, v := range f.stockPositions {
		c.stockPositions[k] = v
	}
	for k, v := range f.stockMovements {
		c.stockMovements[k] = append([]StockMovementRecord{}, v...)
	}
	for k, v := range f.transactions {
		c.transactions[k] = v
	}
	for k, v := range f.journals {
		c.journals[k] = v
	}
	for k, v := range f.approvals {
		c.approvals[k] = v
	}
	for k, v := range f.transfers {
		c.transfers[k] = v
	}
	for k, v := range f.closes {
		c.closes[k] = v
	}
	for k, v := range f.operations {
		c.operations[k] = v
	}
	return c
}

func (f *fakeRepo) WithTx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error {
	clone := f.clone()
	if err := fn(ctx, clone); err != nil {
		return err
	}
	*f = *clone
	return nil
}

func (f *fakeRepo) BranchInCompany(ctx context.Context, companyID, branchID uuid.UUID) (bool, error) {
	return true, nil
}
func (f *fakeRepo) RecordDenialAudit(ctx context.Context, companyID, actorUserID uuid.UUID, eventType, recordType string, recordID uuid.UUID, details map[string]any) error {
	return nil
}
func (f *fakeRepo) RecordRejection(ctx context.Context, rec OperationRecord) error {
	f.rejected = append(f.rejected, rec)
	opKey(f.operations, rec.CompanyID, rec.OperationID, rec)
	return nil
}

func opKey(m map[string]OperationRecord, companyID, operationID uuid.UUID, rec OperationRecord) {
	m[companyID.String()+"|"+operationID.String()] = rec
}

func (f *fakeRepo) FindOperation(ctx context.Context, companyID, operationID uuid.UUID) (OperationRecord, bool, error) {
	rec, ok := f.operations[companyID.String()+"|"+operationID.String()]
	return rec, ok, nil
}
func (f *fakeRepo) RecordOperation(ctx context.Context, rec OperationRecord) error {
	opKey(f.operations, rec.CompanyID, rec.OperationID, rec)
	return nil
}
func (f *fakeRepo) GetOpenPeriod(ctx context.Context, companyID uuid.UUID, date time.Time) (Period, bool, error) {
	if !f.periodOpen {
		return Period{}, false, nil
	}
	return Period{ID: uuid.New(), CompanyID: companyID, Status: "open"}, true, nil
}
func (f *fakeRepo) GetChartAccounts(ctx context.Context, companyID uuid.UUID) (ChartAccounts, error) {
	return f.accounts, nil
}
func (f *fakeRepo) GetStockPosition(ctx context.Context, companyID, branchID, productID uuid.UUID) (inventory.Position, error) {
	return f.stockPositions[productID], nil
}
func (f *fakeRepo) PurchaseInvoiceExists(ctx context.Context, companyID, counterpartyID uuid.UUID, sourceReference string) (bool, error) {
	for _, t := range f.transactions {
		if t.DocumentType == "purchase" && t.Status == "posted" && t.SourceReference == sourceReference &&
			t.CounterpartyID != nil && *t.CounterpartyID == counterpartyID {
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeRepo) SaveStockMovement(ctx context.Context, m StockMovementRecord) error {
	f.stockPositions[m.ProductID] = inventory.Position{Quantity: m.RunningQuantity, Value: m.RunningValue}
	f.stockMovements[m.SourceTransactionID] = append(f.stockMovements[m.SourceTransactionID], m)
	return nil
}
func (f *fakeRepo) SaveTransaction(ctx context.Context, t TransactionRecord) error {
	f.transactions[t.ID] = t
	return nil
}
func (f *fakeRepo) SaveJournal(ctx context.Context, periodID uuid.UUID, j Journal) (uuid.UUID, error) {
	f.journals[j.SourceTransactionID] = j
	return uuid.New(), nil
}
func (f *fakeRepo) RecordAuditEvent(ctx context.Context, companyID, actorUserID uuid.UUID, eventType, recordType string, recordID uuid.UUID, details map[string]any) error {
	return nil
}
func (f *fakeRepo) EnqueueOutboxJob(ctx context.Context, companyID uuid.UUID, jobType, idempotencyKey string, payload map[string]any) error {
	return nil
}
func (f *fakeRepo) RecordSyncChange(ctx context.Context, companyID, branchID uuid.UUID, recordType string, recordID uuid.UUID, version int, kind string) error {
	return nil
}
func (f *fakeRepo) GetPostedTransaction(ctx context.Context, companyID, transactionID uuid.UUID) (TransactionRecord, Journal, bool, error) {
	t, ok := f.transactions[transactionID]
	if !ok || t.Status != "posted" {
		return TransactionRecord{}, Journal{}, false, nil
	}
	return t, f.journals[transactionID], true, nil
}
func (f *fakeRepo) GetStockMovementsByTransaction(ctx context.Context, companyID, transactionID uuid.UUID) ([]StockMovementRecord, error) {
	return f.stockMovements[transactionID], nil
}
func (f *fakeRepo) MarkTransactionReversed(ctx context.Context, originalTransactionID, reversalTransactionID uuid.UUID) error {
	t := f.transactions[originalTransactionID]
	t.Status = "reversed"
	f.transactions[originalTransactionID] = t
	return nil
}
func (f *fakeRepo) GetApprovalLimit(ctx context.Context, companyID, userID uuid.UUID) (*decimal.Decimal, error) {
	return f.approvalLimits[userID], nil
}
func (f *fakeRepo) SaveAwaitingApproval(ctx context.Context, t TransactionRecord, approval ApprovalRequestRecord) (uuid.UUID, uuid.UUID, error) {
	f.transactions[t.ID] = t
	approvalID := uuid.New()
	approval.ID = approvalID
	approval.TransactionID = t.ID
	f.approvals[approvalID] = approval
	return t.ID, approvalID, nil
}
func (f *fakeRepo) GetApprovalRequest(ctx context.Context, companyID, approvalID uuid.UUID) (ApprovalRequestRecord, TransactionRecord, bool, error) {
	a, ok := f.approvals[approvalID]
	if !ok {
		return ApprovalRequestRecord{}, TransactionRecord{}, false, nil
	}
	return a, f.transactions[a.TransactionID], true, nil
}
func (f *fakeRepo) RecordApprovalDecision(ctx context.Context, approvalID, decidedBy uuid.UUID, decision, reason string) error {
	a := f.approvals[approvalID]
	delete(f.approvals, approvalID) // decided requests are no longer "pending" for GetApprovalRequest
	_ = a
	return nil
}
func (f *fakeRepo) MarkTransactionPosted(ctx context.Context, transactionID uuid.UUID) error {
	t := f.transactions[transactionID]
	t.Status = "posted"
	f.transactions[transactionID] = t
	return nil
}
func (f *fakeRepo) MarkTransactionRejected(ctx context.Context, transactionID uuid.UUID) error {
	t := f.transactions[transactionID]
	t.Status = "rejected"
	f.transactions[transactionID] = t
	return nil
}
func (f *fakeRepo) CreateTransfer(ctx context.Context, rec TransferRecord) (uuid.UUID, error) {
	rec.ID = uuid.New()
	rec.Status = "dispatched"
	f.transfers[rec.ID] = rec
	return rec.ID, nil
}
func (f *fakeRepo) GetTransferForReceipt(ctx context.Context, companyID, transferID uuid.UUID) (TransferRecord, bool, error) {
	t, ok := f.transfers[transferID]
	return t, ok, nil
}
func (f *fakeRepo) UpdateTransferReceipt(ctx context.Context, transferID, receiptTransactionID uuid.UUID, receivedAmount decimal.Decimal, newStatus string) error {
	t := f.transfers[transferID]
	t.ReceivedAmount = t.ReceivedAmount.Add(receivedAmount)
	t.Status = newStatus
	r := receiptTransactionID
	t.ReceiptTransactionID = &r
	f.transfers[transferID] = t
	return nil
}
func (f *fakeRepo) GetLatestClose(ctx context.Context, companyID, branchID uuid.UUID, closeDate time.Time, currencyCode string) (DailyCloseRecord, bool, error) {
	rec, ok := f.closes[closeKey(branchID, closeDate, currencyCode)]
	return rec, ok, nil
}
func (f *fakeRepo) SaveDailyClose(ctx context.Context, rec DailyCloseRecord) (uuid.UUID, error) {
	rec.ID = uuid.New()
	f.closes[closeKey(rec.BranchID, rec.CloseDate, rec.CurrencyCode)] = rec
	return rec.ID, nil
}
func closeKey(branchID uuid.UUID, date time.Time, currency string) string {
	return branchID.String() + "|" + date.Format("2006-01-02") + "|" + currency
}

var _ Repository = (*fakeRepo)(nil)
var _ Tx = (*fakeRepo)(nil)

func postedSaleTx(t *testing.T, svc *Service, scope tenancy.Scope, branchID uuid.UUID) PostResult {
	t.Helper()
	res, err := svc.Post(context.Background(), scope, PostInput{
		OperationID: uuid.New(), BranchID: branchID, DocumentType: "expense",
		DocumentDate: time.Now().UTC(), CurrencyCode: "USD", ExchangeRate: d("1"),
		Lines:             []LineInput{{Description: "Rent", Quantity: d("1"), UnitPrice: d("30")}},
		ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed Post: %v", err)
	}
	return res
}

// --- Reverse -----------------------------------------------------------------

func TestReverseMirrorsOriginalAndIsIdempotent(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	companyID, branchID := uuid.New(), uuid.New()
	scope := tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleAccountant, BranchScope: []uuid.UUID{branchID}}

	posted := postedSaleTx(t, svc, scope, branchID)
	if !posted.Accepted {
		t.Fatalf("seed transaction was not accepted: %+v", posted)
	}

	opID := uuid.New()
	res, err := svc.Reverse(context.Background(), scope, ReverseInput{
		OperationID: opID, TransactionID: posted.TransactionID, BranchID: branchID, Reason: "wrong amount",
		ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("expected reversal to be accepted, got error_code=%s", res.ErrorCode)
	}

	original := repo.transactions[posted.TransactionID]
	if original.Status != "reversed" {
		t.Fatalf("original transaction status = %q, want \"reversed\"", original.Status)
	}
	reversalJournal := repo.journals[res.TransactionID]
	originalJournal := repo.journals[posted.TransactionID]
	if !reversalJournal.TotalDebits().Equal(originalJournal.TotalCredits()) {
		t.Fatalf("reversal debits %s != original credits %s", reversalJournal.TotalDebits(), originalJournal.TotalCredits())
	}

	// Retrying with the same operation ID must not reverse a second time.
	res2, err := svc.Reverse(context.Background(), scope, ReverseInput{
		OperationID: opID, TransactionID: posted.TransactionID, BranchID: branchID, Reason: "wrong amount",
		ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("retry Reverse: %v", err)
	}
	if !res2.AlreadyProcessed || res2.TransactionID != res.TransactionID {
		t.Fatalf("retry did not return the original reversal: %+v", res2)
	}
}

func TestReverseAlreadyReversedTransactionIsRejected(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	companyID, branchID := uuid.New(), uuid.New()
	scope := tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleAccountant, BranchScope: []uuid.UUID{branchID}}

	posted := postedSaleTx(t, svc, scope, branchID)
	if _, err := svc.Reverse(context.Background(), scope, ReverseInput{
		OperationID: uuid.New(), TransactionID: posted.TransactionID, BranchID: branchID, Reason: "first",
		ClientSubmittedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("first reverse: %v", err)
	}

	res, err := svc.Reverse(context.Background(), scope, ReverseInput{
		OperationID: uuid.New(), TransactionID: posted.TransactionID, BranchID: branchID, Reason: "second",
		ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("second reverse: %v", err)
	}
	if res.Accepted || res.ErrorCode != "transaction_not_posted" {
		t.Fatalf("expected transaction_not_posted rejection, got %+v", res)
	}
}

func TestReverseNonAccountantNeedsDelegation(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	companyID, branchID := uuid.New(), uuid.New()
	ownerScope := tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleOwner, BranchScope: []uuid.UUID{branchID}}
	accountantScope := tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleAccountant, BranchScope: []uuid.UUID{branchID}}

	posted := postedSaleTx(t, svc, accountantScope, branchID)

	if _, err := svc.Reverse(context.Background(), ownerScope, ReverseInput{
		OperationID: uuid.New(), TransactionID: posted.TransactionID, BranchID: branchID, Reason: "x",
		ClientSubmittedAt: time.Now().UTC(),
	}); !errors.Is(err, ErrOutOfScope) {
		t.Fatalf("owner without delegation: got err=%v, want ErrOutOfScope", err)
	}

	ownerScope.Delegations = tenancy.Delegations{tenancy.ActionReversePosting: true}
	if _, err := svc.Reverse(context.Background(), ownerScope, ReverseInput{
		OperationID: uuid.New(), TransactionID: posted.TransactionID, BranchID: branchID, Reason: "x",
		ClientSubmittedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("owner with delegation: %v", err)
	}
}

// --- SubmitClose ---------------------------------------------------------------

func TestSubmitCloseComputesDiscrepancyAndBlocksDuplicateRevision(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	companyID, branchID := uuid.New(), uuid.New()
	scope := tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleBranchManager, BranchScope: []uuid.UUID{branchID}}
	closeDate := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)

	res, err := svc.SubmitClose(context.Background(), scope, CloseInput{
		OperationID: uuid.New(), BranchID: branchID, CloseDate: closeDate, CurrencyCode: "USD",
		OpeningFloat: d("100"), ExpectedCash: d("320"), CountedCash: d("315"),
		ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("SubmitClose: %v", err)
	}
	if !res.Accepted || !res.Discrepancy.Equal(d("-5")) {
		t.Fatalf("expected accepted with discrepancy -5, got %+v", res)
	}

	res2, err := svc.SubmitClose(context.Background(), scope, CloseInput{
		OperationID: uuid.New(), BranchID: branchID, CloseDate: closeDate, CurrencyCode: "USD",
		OpeningFloat: d("100"), ExpectedCash: d("320"), CountedCash: d("320"),
		ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("SubmitClose duplicate: %v", err)
	}
	if res2.Accepted || res2.ErrorCode != "close_already_submitted" {
		t.Fatalf("expected close_already_submitted rejection, got %+v", res2)
	}
}

// --- DecideApproval --------------------------------------------------------------

func TestExpenseAboveLimitAwaitsApprovalThenPosts(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	companyID, branchID := uuid.New(), uuid.New()
	staffID := uuid.New()
	limit := d("50")
	repo.approvalLimits[staffID] = &limit

	staffScope := tenancy.Scope{CompanyID: companyID, UserID: staffID, Role: tenancy.RoleStaff, BranchScope: []uuid.UUID{branchID}}
	res, err := svc.Post(context.Background(), staffScope, PostInput{
		OperationID: uuid.New(), BranchID: branchID, DocumentType: "expense",
		DocumentDate: time.Now().UTC(), CurrencyCode: "USD", ExchangeRate: d("1"),
		Lines:             []LineInput{{Description: "Generator repair", Quantity: d("1"), UnitPrice: d("200")}},
		ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Post over-limit expense: %v", err)
	}
	if !res.Accepted || res.Status != "awaiting_approval" {
		t.Fatalf("expected accepted+awaiting_approval, got %+v", res)
	}
	if repo.transactions[res.TransactionID].Status != "awaiting_approval" {
		t.Fatalf("transaction was not saved as awaiting_approval")
	}
	if len(repo.journals) != 0 {
		t.Fatalf("no journal should exist before approval, found %d", len(repo.journals))
	}

	var approvalID uuid.UUID
	for id, a := range repo.approvals {
		if a.TransactionID == res.TransactionID {
			approvalID = id
		}
	}
	if approvalID == uuid.Nil {
		t.Fatalf("no approval request was created")
	}

	// Self-approval must be denied even for an owner.
	ownerScope := tenancy.Scope{CompanyID: companyID, UserID: staffID, Role: tenancy.RoleOwner, BranchScope: []uuid.UUID{branchID},
		Delegations: tenancy.Delegations{tenancy.ActionApproveSpending: true}}
	if selfRes, err := svc.DecideApproval(context.Background(), ownerScope, ApprovalDecisionInput{
		OperationID: uuid.New(), ApprovalID: approvalID, BranchID: branchID, Decision: "approved",
		ExpectedVersion: 1, ClientSubmittedAt: time.Now().UTC(),
	}); err != nil || selfRes.Accepted || selfRes.ErrorCode != "self_approval_denied" {
		t.Fatalf("expected self_approval_denied, got res=%+v err=%v", selfRes, err)
	}

	approverScope := tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleAccountant, BranchScope: []uuid.UUID{branchID},
		Delegations: tenancy.Delegations{tenancy.ActionApproveSpending: true}}
	decided, err := svc.DecideApproval(context.Background(), approverScope, ApprovalDecisionInput{
		OperationID: uuid.New(), ApprovalID: approvalID, BranchID: branchID, Decision: "approved",
		ExpectedVersion: 1, ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("DecideApproval: %v", err)
	}
	if !decided.Accepted || decided.Status != "posted" {
		t.Fatalf("expected posted, got %+v", decided)
	}
	if repo.transactions[res.TransactionID].Status != "posted" {
		t.Fatalf("transaction was not transitioned to posted")
	}
	j, ok := repo.journals[res.TransactionID]
	if !ok || !j.TotalDebits().Equal(d("200")) {
		t.Fatalf("expected a 200 journal to be posted on approval, got %+v ok=%v", j, ok)
	}
}

func TestExpenseAtOrBelowLimitPostsImmediately(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	companyID, branchID := uuid.New(), uuid.New()
	userID := uuid.New()
	limit := d("50")
	repo.approvalLimits[userID] = &limit
	scope := tenancy.Scope{CompanyID: companyID, UserID: userID, Role: tenancy.RoleStaff, BranchScope: []uuid.UUID{branchID}}

	res, err := svc.Post(context.Background(), scope, PostInput{
		OperationID: uuid.New(), BranchID: branchID, DocumentType: "expense",
		DocumentDate: time.Now().UTC(), CurrencyCode: "USD", ExchangeRate: d("1"),
		Lines:             []LineInput{{Description: "Stationery", Quantity: d("1"), UnitPrice: d("50")}},
		ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if res.Status != "posted" {
		t.Fatalf("expected an at-limit expense to post immediately, got status=%q", res.Status)
	}
}

// --- ReceiveTransfer -------------------------------------------------------------

func TestTransferDispatchAndPartialThenFullReceipt(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	companyID := uuid.New()
	senderBranch, receiverBranch := uuid.New(), uuid.New()
	senderScope := tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleBranchManager, BranchScope: []uuid.UUID{senderBranch}}
	receiverScope := tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleBranchManager, BranchScope: []uuid.UUID{receiverBranch}}

	dispatch, err := svc.Post(context.Background(), senderScope, PostInput{
		OperationID: uuid.New(), BranchID: senderBranch, DocumentType: "transfer",
		DocumentDate: time.Now().UTC(), CurrencyCode: "USD", ExchangeRate: d("1"),
		ReceiverBranchID: &receiverBranch, TransferType: "cash",
		Lines:             []LineInput{{Description: "Cash to main", Quantity: d("1"), UnitPrice: d("100")}},
		ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil || !dispatch.Accepted {
		t.Fatalf("dispatch Post: res=%+v err=%v", dispatch, err)
	}

	var transferID uuid.UUID
	for id, tr := range repo.transfers {
		if tr.DispatchTransactionID == dispatch.TransactionID {
			transferID = id
		}
	}
	if transferID == uuid.Nil {
		t.Fatalf("no transfer record was created on dispatch")
	}
	if !repo.transfers[transferID].Amount.Equal(d("100")) {
		t.Fatalf("transfer amount = %s, want 100", repo.transfers[transferID].Amount)
	}

	partial, err := svc.ReceiveTransfer(context.Background(), receiverScope, TransferReceiveInput{
		OperationID: uuid.New(), TransferID: transferID, BranchID: receiverBranch,
		ReceivedAmount: d("60"), ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil || !partial.Accepted || partial.TransferStatus != "partially_received" {
		t.Fatalf("partial receipt: res=%+v err=%v", partial, err)
	}

	// Receiving more than the remaining 40 must be rejected, not silently
	// capped or overpaid.
	over, err := svc.ReceiveTransfer(context.Background(), receiverScope, TransferReceiveInput{
		OperationID: uuid.New(), TransferID: transferID, BranchID: receiverBranch,
		ReceivedAmount: d("50"), ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("over-receipt: %v", err)
	}
	if over.Accepted || over.ErrorCode != "receipt_exceeds_remaining" {
		t.Fatalf("expected receipt_exceeds_remaining, got %+v", over)
	}

	final, err := svc.ReceiveTransfer(context.Background(), receiverScope, TransferReceiveInput{
		OperationID: uuid.New(), TransferID: transferID, BranchID: receiverBranch,
		ReceivedAmount: d("40"), ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil || !final.Accepted || final.TransferStatus != "received" {
		t.Fatalf("final receipt: res=%+v err=%v", final, err)
	}
	if !repo.transfers[transferID].ReceivedAmount.Equal(d("100")) {
		t.Fatalf("cumulative received = %s, want 100", repo.transfers[transferID].ReceivedAmount)
	}
}

// --- Stock adjustment (waste, count) -----------------------------------------

func TestStockAdjustmentWasteReducesStockAndPostsLoss(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	companyID, branchID, productID := uuid.New(), uuid.New(), uuid.New()
	scope := tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleAccountant, BranchScope: []uuid.UUID{branchID}}
	repo.stockPositions[productID] = inventory.Position{Quantity: d("20"), Value: d("120")} // unit cost 6

	res, err := svc.Post(context.Background(), scope, PostInput{
		OperationID: uuid.New(), BranchID: branchID, DocumentType: "stock_adjustment", AdjustmentType: "waste",
		DocumentDate: time.Now().UTC(), CurrencyCode: "USD", ExchangeRate: d("1"), Explanation: "Spoiled stock",
		Lines:             []LineInput{{ProductID: &productID, Description: "Waste", Quantity: d("5"), UnitPrice: d("0")}},
		ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("expected waste to be accepted, got error_code=%s", res.ErrorCode)
	}

	pos := repo.stockPositions[productID]
	if !pos.Quantity.Equal(d("15")) {
		t.Fatalf("stock quantity after waste: want 15, got %s", pos.Quantity)
	}
	j := repo.journals[res.TransactionID]
	if !j.TotalDebits().Equal(d("30")) { // 5 units at unit cost 6
		t.Fatalf("waste journal debits: want 30, got %s", j.TotalDebits())
	}
	if err := j.Validate(); err != nil {
		t.Fatalf("waste journal does not balance: %v", err)
	}
}

func TestStockAdjustmentWasteAboveOnHandIsRejected(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	companyID, branchID, productID := uuid.New(), uuid.New(), uuid.New()
	scope := tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleAccountant, BranchScope: []uuid.UUID{branchID}}
	repo.stockPositions[productID] = inventory.Position{Quantity: d("5"), Value: d("30")}

	res, err := svc.Post(context.Background(), scope, PostInput{
		OperationID: uuid.New(), BranchID: branchID, DocumentType: "stock_adjustment", AdjustmentType: "waste",
		DocumentDate: time.Now().UTC(), CurrencyCode: "USD", ExchangeRate: d("1"), Explanation: "Spoiled stock",
		Lines:             []LineInput{{ProductID: &productID, Description: "Waste", Quantity: d("10"), UnitPrice: d("0")}},
		ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if res.Accepted || res.ErrorCode != "insufficient_stock" {
		t.Fatalf("expected insufficient_stock rejection, got %+v", res)
	}
}

func TestStockAdjustmentCountShortfallDebitsStockLossExpense(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	companyID, branchID, productID := uuid.New(), uuid.New(), uuid.New()
	scope := tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleAccountant, BranchScope: []uuid.UUID{branchID}}
	repo.stockPositions[productID] = inventory.Position{Quantity: d("16"), Value: d("96")} // unit cost 6

	res, err := svc.Post(context.Background(), scope, PostInput{
		OperationID: uuid.New(), BranchID: branchID, DocumentType: "stock_adjustment", AdjustmentType: "count",
		DocumentDate: time.Now().UTC(), CurrencyCode: "USD", ExchangeRate: d("1"), Explanation: "Monthly count",
		Lines:             []LineInput{{ProductID: &productID, Description: "Count", Quantity: d("15"), UnitPrice: d("0")}},
		ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("expected count adjustment to be accepted, got error_code=%s", res.ErrorCode)
	}

	pos := repo.stockPositions[productID]
	if !pos.Quantity.Equal(d("15")) {
		t.Fatalf("stock quantity after count: want 15, got %s", pos.Quantity)
	}
	j := repo.journals[res.TransactionID]
	if !j.TotalDebits().Equal(d("6")) { // 1 unit short at unit cost 6
		t.Fatalf("count shortfall journal debits: want 6, got %s", j.TotalDebits())
	}
	found := false
	for _, l := range j.Lines {
		if l.AccountID == repo.accounts.StockLossExpense {
			found = true
			if l.Side != Debit {
				t.Fatalf("expected a shortfall to debit Stock loss expense, got %s", l.Side)
			}
		}
	}
	if !found {
		t.Fatalf("expected a Stock loss expense leg in the journal")
	}
}

func TestStockAdjustmentCountSurplusCreditsStockLossExpense(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	companyID, branchID, productID := uuid.New(), uuid.New(), uuid.New()
	scope := tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleAccountant, BranchScope: []uuid.UUID{branchID}}
	repo.stockPositions[productID] = inventory.Position{Quantity: d("16"), Value: d("96")} // unit cost 6

	res, err := svc.Post(context.Background(), scope, PostInput{
		OperationID: uuid.New(), BranchID: branchID, DocumentType: "stock_adjustment", AdjustmentType: "count",
		DocumentDate: time.Now().UTC(), CurrencyCode: "USD", ExchangeRate: d("1"), Explanation: "Monthly count",
		Lines:             []LineInput{{ProductID: &productID, Description: "Count", Quantity: d("17"), UnitPrice: d("0")}},
		ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("expected count adjustment to be accepted, got error_code=%s", res.ErrorCode)
	}

	j := repo.journals[res.TransactionID]
	for _, l := range j.Lines {
		if l.AccountID == repo.accounts.StockLossExpense && l.Side != Credit {
			t.Fatalf("expected a surplus to credit Stock loss expense, got %s", l.Side)
		}
	}
}

func TestStockAdjustmentCountMatchingPositionIsRejected(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	companyID, branchID, productID := uuid.New(), uuid.New(), uuid.New()
	scope := tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleAccountant, BranchScope: []uuid.UUID{branchID}}
	repo.stockPositions[productID] = inventory.Position{Quantity: d("10"), Value: d("60")}

	res, err := svc.Post(context.Background(), scope, PostInput{
		OperationID: uuid.New(), BranchID: branchID, DocumentType: "stock_adjustment", AdjustmentType: "count",
		DocumentDate: time.Now().UTC(), CurrencyCode: "USD", ExchangeRate: d("1"), Explanation: "No change",
		Lines:             []LineInput{{ProductID: &productID, Description: "Count", Quantity: d("10"), UnitPrice: d("0")}},
		ClientSubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if res.Accepted || res.ErrorCode != "no_stock_change" {
		t.Fatalf("expected a no_stock_change rejection, got %+v", res)
	}
}

// --- Purchase duplicate-invoice detection -------------------------------------

func TestPurchaseRejectsDuplicateSupplierInvoice(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	companyID, branchID, supplierID := uuid.New(), uuid.New(), uuid.New()
	scope := tenancy.Scope{CompanyID: companyID, UserID: uuid.New(), Role: tenancy.RoleAccountant, BranchScope: []uuid.UUID{branchID}}

	postPurchase := func(reference string) (PostResult, error) {
		return svc.Post(context.Background(), scope, PostInput{
			OperationID: uuid.New(), BranchID: branchID, DocumentType: "purchase",
			DocumentDate: time.Now().UTC(), CurrencyCode: "USD", ExchangeRate: d("1"),
			CounterpartyID: &supplierID, SourceReference: reference,
			Lines:             []LineInput{{Description: "Stock", Quantity: d("1"), UnitPrice: d("100")}},
			ClientSubmittedAt: time.Now().UTC(),
		})
	}

	first, err := postPurchase("INV-100")
	if err != nil || !first.Accepted {
		t.Fatalf("first purchase: res=%+v err=%v", first, err)
	}

	second, err := postPurchase("INV-100")
	if err != nil {
		t.Fatalf("second purchase: %v", err)
	}
	if second.Accepted || second.ErrorCode != "duplicate_supplier_invoice" {
		t.Fatalf("expected duplicate_supplier_invoice rejection, got %+v", second)
	}

	// A different reference, or a different supplier, is not a duplicate.
	differentRef, err := postPurchase("INV-101")
	if err != nil || !differentRef.Accepted {
		t.Fatalf("different reference should post: res=%+v err=%v", differentRef, err)
	}
}
