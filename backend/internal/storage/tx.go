package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/ledgerlink/branchledger/backend/internal/accounting"
	"github.com/ledgerlink/branchledger/backend/internal/inventory"
)

// txImpl implements accounting.Tx against one open pgx transaction.
type txImpl struct {
	tx pgx.Tx
}

func (t *txImpl) FindOperation(ctx context.Context, companyID, operationID uuid.UUID) (accounting.OperationRecord, bool, error) {
	row := t.tx.QueryRow(ctx, `
		SELECT company_id, operation_id, branch_id, device_id, actor_user_id, command_type,
		       payload_hash, status, coalesce(result_record_type, ''), result_record_id,
		       coalesce(result_version, 0), coalesce(error_code, ''), client_submitted_at
		FROM operations WHERE company_id = $1 AND operation_id = $2`, companyID, operationID)

	var rec accounting.OperationRecord
	var resultRecordID *uuid.UUID
	err := row.Scan(&rec.CompanyID, &rec.OperationID, &rec.BranchID, &rec.DeviceID, &rec.ActorUserID,
		&rec.CommandType, &rec.PayloadHash, &rec.Status, &rec.ResultRecordType, &resultRecordID,
		&rec.ResultVersion, &rec.ErrorCode, &rec.ClientSubmittedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return accounting.OperationRecord{}, false, nil
	}
	if err != nil {
		return accounting.OperationRecord{}, false, err
	}
	if resultRecordID != nil {
		rec.ResultRecordID = *resultRecordID
	}
	return rec, true, nil
}

func (t *txImpl) RecordOperation(ctx context.Context, rec accounting.OperationRecord) error {
	var resultRecordID *uuid.UUID
	if rec.ResultRecordID != uuid.Nil {
		resultRecordID = &rec.ResultRecordID
	}
	var resultRecordType, errorCode *string
	if rec.ResultRecordType != "" {
		resultRecordType = &rec.ResultRecordType
	}
	if rec.ErrorCode != "" {
		errorCode = &rec.ErrorCode
	}
	_, err := t.tx.Exec(ctx, `
		INSERT INTO operations (company_id, operation_id, branch_id, device_id, actor_user_id,
		                         command_type, payload_hash, status, result_record_type,
		                         result_record_id, result_version, error_code, client_submitted_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		rec.CompanyID, rec.OperationID, rec.BranchID, rec.DeviceID, rec.ActorUserID,
		rec.CommandType, rec.PayloadHash, rec.Status, resultRecordType, resultRecordID,
		nullableInt(rec.ResultVersion), errorCode, rec.ClientSubmittedAt)
	return err
}

func (t *txImpl) GetOpenPeriod(ctx context.Context, companyID uuid.UUID, date time.Time) (accounting.Period, bool, error) {
	row := t.tx.QueryRow(ctx, `
		SELECT id, company_id, starts_on, ends_on, status
		FROM periods
		WHERE company_id = $1 AND status = 'open' AND starts_on <= $2 AND ends_on >= $2`,
		companyID, date)

	var p accounting.Period
	err := row.Scan(&p.ID, &p.CompanyID, &p.StartsOn, &p.EndsOn, &p.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return accounting.Period{}, false, nil
	}
	if err != nil {
		return accounting.Period{}, false, err
	}
	return p, true, nil
}

func (t *txImpl) GetChartAccounts(ctx context.Context, companyID uuid.UUID) (accounting.ChartAccounts, error) {
	rows, err := t.tx.Query(ctx, `SELECT code, id FROM accounts WHERE company_id = $1 AND is_active`, companyID)
	if err != nil {
		return accounting.ChartAccounts{}, err
	}
	defer rows.Close()

	byCode := map[string]uuid.UUID{}
	for rows.Next() {
		var code string
		var id uuid.UUID
		if err := rows.Scan(&code, &id); err != nil {
			return accounting.ChartAccounts{}, err
		}
		byCode[code] = id
	}
	if err := rows.Err(); err != nil {
		return accounting.ChartAccounts{}, err
	}

	var reportingCurrency string
	if err := t.tx.QueryRow(ctx, `SELECT reporting_currency FROM companies WHERE id = $1`, companyID).Scan(&reportingCurrency); err != nil {
		return accounting.ChartAccounts{}, err
	}

	// Standard chart of accounts codes, provisioned at company setup time.
	return accounting.NewChartAccounts(reportingCurrency, accounting.ChartAccounts{
		Cash: byCode["1000"], Bank: byCode["1010"], AccountsReceivable: byCode["1100"],
		AccountsPayable: byCode["2000"], SalesRevenue: byCode["4000"], SalesReturns: byCode["4100"],
		CostOfSales: byCode["5000"], Inventory: byCode["1200"], OperatingExpense: byCode["6000"],
		FixedAsset: byCode["1500"], StockLossExpense: byCode["5100"], FundsInTransit: byCode["1300"],
		OwnerCapital: byCode["3000"],
	}), nil
}

func (t *txImpl) GetStockPosition(ctx context.Context, companyID, branchID, productID uuid.UUID) (inventory.Position, error) {
	row := t.tx.QueryRow(ctx, `
		SELECT running_quantity, running_value FROM stock_movements
		WHERE company_id = $1 AND branch_id = $2 AND product_id = $3
		ORDER BY occurred_at DESC, id DESC LIMIT 1`, companyID, branchID, productID)

	var pos inventory.Position
	err := row.Scan(&pos.Quantity, &pos.Value)
	if errors.Is(err, pgx.ErrNoRows) {
		return inventory.Position{}, nil // no movements yet: zero position
	}
	return pos, err
}

func (t *txImpl) SaveStockMovement(ctx context.Context, m accounting.StockMovementRecord) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO stock_movements (id, company_id, branch_id, product_id, source_transaction_id,
		                              movement_type, quantity_delta, unit_cost, running_quantity,
		                              running_value, reason, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		m.ID, m.CompanyID, m.BranchID, m.ProductID, m.SourceTransactionID, m.MovementType,
		m.QuantityDelta, m.UnitCost, m.RunningQuantity, m.RunningValue, m.Reason, m.CreatedBy)
	return err
}

func (t *txImpl) SaveTransaction(ctx context.Context, rec accounting.TransactionRecord) error {
	// awaiting_approval and rejected transactions have not posted — no
	// posted_at yet; MarkTransactionPosted sets it later if/when they do.
	var postedAtExpr string
	if rec.Status == "posted" {
		postedAtExpr = "now()"
	} else {
		postedAtExpr = "NULL"
	}
	_, err := t.tx.Exec(ctx, `
		INSERT INTO transactions (id, company_id, branch_id, operation_id, document_type,
		                          document_date, currency_code, exchange_rate, counterparty_id,
		                          payment_account_id, source_reference, explanation, status,
		                          revision, created_by, reverses_transaction_id, due_date, posted_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17, `+postedAtExpr+`)`,
		rec.ID, rec.CompanyID, rec.BranchID, rec.OperationID, rec.DocumentType, rec.DocumentDate,
		rec.CurrencyCode, rec.ExchangeRate, rec.CounterpartyID, rec.PaymentAccountID,
		nullableString(rec.SourceReference), nullableString(rec.Explanation), rec.Status, rec.Revision, rec.CreatedBy,
		rec.ReversesTransactionID, rec.DueDate)
	if err != nil {
		return err
	}

	for _, l := range rec.Lines {
		if _, err := t.tx.Exec(ctx, `
			INSERT INTO transaction_lines (transaction_id, line_no, product_id, description,
			                                quantity, unit_price, discount, tax_code, line_net)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			rec.ID, l.LineNo, l.ProductID, nullableString(l.Description), l.Quantity, l.UnitPrice,
			l.Discount, nullableString(l.TaxCode), l.LineNet); err != nil {
			return err
		}
	}
	return nil
}

func (t *txImpl) SaveJournal(ctx context.Context, periodID uuid.UUID, j accounting.Journal) (uuid.UUID, error) {
	journalID := uuid.New()
	var companyID uuid.UUID
	if err := t.tx.QueryRow(ctx, `SELECT company_id FROM periods WHERE id = $1`, periodID).Scan(&companyID); err != nil {
		return uuid.Nil, err
	}

	// branch_id and posted_by travel with the source transaction; look them up
	// so journals carry the same scoping fields transactions do.
	var branchID, postedBy uuid.UUID
	if err := t.tx.QueryRow(ctx, `SELECT branch_id, created_by FROM transactions WHERE id = $1`, j.SourceTransactionID).
		Scan(&branchID, &postedBy); err != nil {
		return uuid.Nil, err
	}

	if _, err := t.tx.Exec(ctx, `
		INSERT INTO journals (id, company_id, branch_id, source_transaction_id, period_id,
		                       posted_by, reporting_currency)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		journalID, companyID, branchID, j.SourceTransactionID, periodID, postedBy, j.ReportingCurrency); err != nil {
		return uuid.Nil, err
	}

	for i, l := range j.Lines {
		if _, err := t.tx.Exec(ctx, `
			INSERT INTO journal_lines (journal_id, company_id, account_id, side, original_amount,
			                            reporting_amount, line_no)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			journalID, companyID, l.AccountID, string(l.Side), l.OriginalAmount, l.ReportingAmount, i+1); err != nil {
			return uuid.Nil, err
		}
	}
	return journalID, nil
}

func (t *txImpl) RecordAuditEvent(ctx context.Context, companyID, actorUserID uuid.UUID, eventType, recordType string, recordID uuid.UUID, details map[string]any) error {
	payload, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO audit_events (company_id, actor_user_id, event_type, record_type, record_id, details)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		companyID, actorUserID, eventType, nullableString(recordType), recordID, payload)
	return err
}

func (t *txImpl) EnqueueOutboxJob(ctx context.Context, companyID uuid.UUID, jobType, idempotencyKey string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO outbox_jobs (company_id, job_type, payload, idempotency_key)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (company_id, job_type, idempotency_key) DO NOTHING`,
		companyID, jobType, body, idempotencyKey)
	return err
}

func (t *txImpl) RecordSyncChange(ctx context.Context, companyID, branchID uuid.UUID, recordType string, recordID uuid.UUID, version int, kind string) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO sync_changes (company_id, branch_id, record_type, record_id, record_version, change_kind)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		companyID, branchID, recordType, recordID, version, kind)
	return err
}

// --- Reversal ---------------------------------------------------------------

func (t *txImpl) GetPostedTransaction(ctx context.Context, companyID, transactionID uuid.UUID) (accounting.TransactionRecord, accounting.Journal, bool, error) {
	var rec accounting.TransactionRecord
	row := t.tx.QueryRow(ctx, `
		SELECT id, company_id, branch_id, operation_id, document_type, document_date,
		       currency_code, exchange_rate, counterparty_id, payment_account_id,
		       coalesce(source_reference, ''), coalesce(explanation, ''), status, revision, created_by
		FROM transactions WHERE company_id = $1 AND id = $2 AND status = 'posted'`, companyID, transactionID)
	if err := row.Scan(&rec.ID, &rec.CompanyID, &rec.BranchID, &rec.OperationID, &rec.DocumentType, &rec.DocumentDate,
		&rec.CurrencyCode, &rec.ExchangeRate, &rec.CounterpartyID, &rec.PaymentAccountID,
		&rec.SourceReference, &rec.Explanation, &rec.Status, &rec.Revision, &rec.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return accounting.TransactionRecord{}, accounting.Journal{}, false, nil
		}
		return accounting.TransactionRecord{}, accounting.Journal{}, false, err
	}

	var journalID uuid.UUID
	var reportingCurrency string
	if err := t.tx.QueryRow(ctx, `
		SELECT id, reporting_currency FROM journals WHERE source_transaction_id = $1`, transactionID).
		Scan(&journalID, &reportingCurrency); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return accounting.TransactionRecord{}, accounting.Journal{}, false, nil
		}
		return accounting.TransactionRecord{}, accounting.Journal{}, false, err
	}

	rows, err := t.tx.Query(ctx, `
		SELECT account_id, side, original_amount, reporting_amount FROM journal_lines
		WHERE journal_id = $1 ORDER BY line_no`, journalID)
	if err != nil {
		return accounting.TransactionRecord{}, accounting.Journal{}, false, err
	}
	defer rows.Close()

	journal := accounting.Journal{SourceTransactionID: transactionID, ReportingCurrency: reportingCurrency}
	for rows.Next() {
		var l accounting.JournalLine
		var side string
		if err := rows.Scan(&l.AccountID, &side, &l.OriginalAmount, &l.ReportingAmount); err != nil {
			return accounting.TransactionRecord{}, accounting.Journal{}, false, err
		}
		l.Side = accounting.Side(side)
		journal.Lines = append(journal.Lines, l)
	}
	if err := rows.Err(); err != nil {
		return accounting.TransactionRecord{}, accounting.Journal{}, false, err
	}

	return rec, journal, true, nil
}

func (t *txImpl) GetStockMovementsByTransaction(ctx context.Context, companyID, transactionID uuid.UUID) ([]accounting.StockMovementRecord, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT id, company_id, branch_id, product_id, source_transaction_id, movement_type,
		       quantity_delta, unit_cost, running_quantity, running_value, coalesce(reason, ''), created_by
		FROM stock_movements WHERE company_id = $1 AND source_transaction_id = $2`, companyID, transactionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []accounting.StockMovementRecord
	for rows.Next() {
		var m accounting.StockMovementRecord
		if err := rows.Scan(&m.ID, &m.CompanyID, &m.BranchID, &m.ProductID, &m.SourceTransactionID, &m.MovementType,
			&m.QuantityDelta, &m.UnitCost, &m.RunningQuantity, &m.RunningValue, &m.Reason, &m.CreatedBy); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (t *txImpl) MarkTransactionReversed(ctx context.Context, originalTransactionID, reversalTransactionID uuid.UUID) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE transactions SET status = 'reversed', reversed_by_transaction_id = $1 WHERE id = $2`,
		reversalTransactionID, originalTransactionID)
	return err
}

// --- Approval-gated posting --------------------------------------------------

func (t *txImpl) GetApprovalLimit(ctx context.Context, companyID, userID uuid.UUID) (*decimal.Decimal, error) {
	var limit *decimal.Decimal
	err := t.tx.QueryRow(ctx, `
		SELECT approval_limit FROM memberships WHERE company_id = $1 AND user_id = $2`, companyID, userID).Scan(&limit)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return limit, err
}

func (t *txImpl) SaveAwaitingApproval(ctx context.Context, rec accounting.TransactionRecord, approval accounting.ApprovalRequestRecord) (uuid.UUID, uuid.UUID, error) {
	if err := t.SaveTransaction(ctx, rec); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	var approvalID uuid.UUID
	err := t.tx.QueryRow(ctx, `
		INSERT INTO approvals (company_id, transaction_id, requested_by, record_version_at_request)
		VALUES ($1,$2,$3,$4) RETURNING id`,
		approval.CompanyID, rec.ID, approval.RequestedBy, approval.RecordVersionAtRequest).Scan(&approvalID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return rec.ID, approvalID, nil
}

func (t *txImpl) GetApprovalRequest(ctx context.Context, companyID, approvalID uuid.UUID) (accounting.ApprovalRequestRecord, accounting.TransactionRecord, bool, error) {
	var a accounting.ApprovalRequestRecord
	var txID uuid.UUID
	row := t.tx.QueryRow(ctx, `
		SELECT id, company_id, transaction_id, requested_by, record_version_at_request
		FROM approvals WHERE company_id = $1 AND id = $2 AND decision IS NULL`, companyID, approvalID)
	if err := row.Scan(&a.ID, &a.CompanyID, &txID, &a.RequestedBy, &a.RecordVersionAtRequest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return accounting.ApprovalRequestRecord{}, accounting.TransactionRecord{}, false, nil
		}
		return accounting.ApprovalRequestRecord{}, accounting.TransactionRecord{}, false, err
	}
	a.TransactionID = txID

	rec, ok, err := t.getTransactionAnyStatus(ctx, companyID, txID)
	if err != nil || !ok {
		return accounting.ApprovalRequestRecord{}, accounting.TransactionRecord{}, false, err
	}
	return a, rec, true, nil
}

// getTransactionAnyStatus loads a transaction and its lines regardless of
// status, used where the caller has already established the transaction's
// relevance (e.g. via a matching approvals row) and only needs its fields.
func (t *txImpl) getTransactionAnyStatus(ctx context.Context, companyID, transactionID uuid.UUID) (accounting.TransactionRecord, bool, error) {
	var rec accounting.TransactionRecord
	row := t.tx.QueryRow(ctx, `
		SELECT id, company_id, branch_id, operation_id, document_type, document_date,
		       currency_code, exchange_rate, counterparty_id, payment_account_id,
		       coalesce(source_reference, ''), coalesce(explanation, ''), status, revision, created_by
		FROM transactions WHERE company_id = $1 AND id = $2`, companyID, transactionID)
	if err := row.Scan(&rec.ID, &rec.CompanyID, &rec.BranchID, &rec.OperationID, &rec.DocumentType, &rec.DocumentDate,
		&rec.CurrencyCode, &rec.ExchangeRate, &rec.CounterpartyID, &rec.PaymentAccountID,
		&rec.SourceReference, &rec.Explanation, &rec.Status, &rec.Revision, &rec.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return accounting.TransactionRecord{}, false, nil
		}
		return accounting.TransactionRecord{}, false, err
	}

	rows, err := t.tx.Query(ctx, `
		SELECT line_no, product_id, coalesce(description, ''), quantity, unit_price, discount,
		       coalesce(tax_code, ''), line_net
		FROM transaction_lines WHERE transaction_id = $1 ORDER BY line_no`, transactionID)
	if err != nil {
		return accounting.TransactionRecord{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var l accounting.TransactionLineRecord
		if err := rows.Scan(&l.LineNo, &l.ProductID, &l.Description, &l.Quantity, &l.UnitPrice, &l.Discount,
			&l.TaxCode, &l.LineNet); err != nil {
			return accounting.TransactionRecord{}, false, err
		}
		rec.Lines = append(rec.Lines, l)
	}
	return rec, true, rows.Err()
}

func (t *txImpl) RecordApprovalDecision(ctx context.Context, approvalID, decidedBy uuid.UUID, decision, reason string) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE approvals SET decided_by = $1, decided_at = now(), decision = $2, reason = $3 WHERE id = $4`,
		decidedBy, decision, nullableString(reason), approvalID)
	return err
}

func (t *txImpl) MarkTransactionPosted(ctx context.Context, transactionID uuid.UUID) error {
	_, err := t.tx.Exec(ctx, `UPDATE transactions SET status = 'posted', posted_at = now() WHERE id = $1`, transactionID)
	return err
}

func (t *txImpl) MarkTransactionRejected(ctx context.Context, transactionID uuid.UUID) error {
	_, err := t.tx.Exec(ctx, `UPDATE transactions SET status = 'rejected' WHERE id = $1`, transactionID)
	return err
}

// --- Transfers ---------------------------------------------------------------

func (t *txImpl) CreateTransfer(ctx context.Context, rec accounting.TransferRecord) (uuid.UUID, error) {
	var transferID uuid.UUID
	err := t.tx.QueryRow(ctx, `
		INSERT INTO transfers (company_id, transfer_type, sender_branch_id, receiver_branch_id,
		                        dispatch_transaction_id, status, currency_code, exchange_rate)
		VALUES ($1,$2,$3,$4,$5,'dispatched',$6,$7) RETURNING id`,
		rec.CompanyID, rec.TransferType, rec.SenderBranchID, rec.ReceiverBranchID,
		rec.DispatchTransactionID, rec.CurrencyCode, rec.ExchangeRate).Scan(&transferID)
	if err != nil {
		return uuid.Nil, err
	}
	// Cash transfers carry their amount on a single transfer_lines row with
	// no product (transfer_lines.product_id is nullable for exactly this).
	if _, err := t.tx.Exec(ctx, `
		INSERT INTO transfer_lines (transfer_id, product_id, amount, amount_received)
		VALUES ($1, NULL, $2, 0)`, transferID, rec.Amount); err != nil {
		return uuid.Nil, err
	}
	return transferID, nil
}

func (t *txImpl) GetTransferForReceipt(ctx context.Context, companyID, transferID uuid.UUID) (accounting.TransferRecord, bool, error) {
	var rec accounting.TransferRecord
	row := t.tx.QueryRow(ctx, `
		SELECT t.id, t.company_id, t.transfer_type, t.sender_branch_id, t.receiver_branch_id,
		       t.dispatch_transaction_id, t.receipt_transaction_id, t.status,
		       coalesce(t.currency_code, ''), t.exchange_rate, l.amount, l.amount_received
		FROM transfers t
		JOIN transfer_lines l ON l.transfer_id = t.id AND l.product_id IS NULL
		WHERE t.company_id = $1 AND t.id = $2`, companyID, transferID)
	if err := row.Scan(&rec.ID, &rec.CompanyID, &rec.TransferType, &rec.SenderBranchID, &rec.ReceiverBranchID,
		&rec.DispatchTransactionID, &rec.ReceiptTransactionID, &rec.Status,
		&rec.CurrencyCode, &rec.ExchangeRate, &rec.Amount, &rec.ReceivedAmount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return accounting.TransferRecord{}, false, nil
		}
		return accounting.TransferRecord{}, false, err
	}
	return rec, true, nil
}

func (t *txImpl) UpdateTransferReceipt(ctx context.Context, transferID, receiptTransactionID uuid.UUID, receivedAmount decimal.Decimal, newStatus string) error {
	if _, err := t.tx.Exec(ctx, `
		UPDATE transfer_lines SET amount_received = amount_received + $1
		WHERE transfer_id = $2 AND product_id IS NULL`, receivedAmount, transferID); err != nil {
		return err
	}
	_, err := t.tx.Exec(ctx, `
		UPDATE transfers SET status = $1, receipt_transaction_id = $2 WHERE id = $3`,
		newStatus, receiptTransactionID, transferID)
	return err
}

// --- Daily close --------------------------------------------------------------

func (t *txImpl) GetLatestClose(ctx context.Context, companyID, branchID uuid.UUID, closeDate time.Time, currencyCode string) (accounting.DailyCloseRecord, bool, error) {
	var rec accounting.DailyCloseRecord
	row := t.tx.QueryRow(ctx, `
		SELECT id, company_id, branch_id, close_date, currency_code, opening_float, expected_cash,
		       counted_cash, discrepancy, coalesce(explanation, ''), status, revision, submitted_by
		FROM daily_closes
		WHERE company_id = $1 AND branch_id = $2 AND close_date = $3 AND currency_code = $4
		ORDER BY revision DESC LIMIT 1`, companyID, branchID, closeDate, currencyCode)
	if err := row.Scan(&rec.ID, &rec.CompanyID, &rec.BranchID, &rec.CloseDate, &rec.CurrencyCode, &rec.OpeningFloat,
		&rec.ExpectedCash, &rec.CountedCash, &rec.Discrepancy, &rec.Explanation, &rec.Status, &rec.Revision, &rec.SubmittedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return accounting.DailyCloseRecord{}, false, nil
		}
		return accounting.DailyCloseRecord{}, false, err
	}
	return rec, true, nil
}

func (t *txImpl) SaveDailyClose(ctx context.Context, rec accounting.DailyCloseRecord) (uuid.UUID, error) {
	var id uuid.UUID
	err := t.tx.QueryRow(ctx, `
		INSERT INTO daily_closes (company_id, branch_id, close_date, currency_code, opening_float,
		                          expected_cash, counted_cash, explanation, status, submitted_by,
		                          submitted_at, revision)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10, now(), $11) RETURNING id`,
		rec.CompanyID, rec.BranchID, rec.CloseDate, rec.CurrencyCode, rec.OpeningFloat, rec.ExpectedCash,
		rec.CountedCash, nullableString(rec.Explanation), rec.Status, rec.SubmittedBy, rec.Revision).Scan(&id)
	return id, err
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullableInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}
