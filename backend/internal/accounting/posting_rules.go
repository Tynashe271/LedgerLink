package accounting

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Leg is one account movement before it becomes a JournalLine: an account, a
// side, and an amount in the original transaction currency. BuildJournal
// converts legs into a balanced Journal, computing reporting amounts.
type Leg struct {
	AccountID uuid.UUID
	Side      Side
	Amount    decimal.Decimal
}

// BuildJournal assembles a Journal from legs that are already expected to
// balance in the original currency, converting each to the reporting currency.
// This is the single place every posting rule below funnels through, so the
// balance check in Journal.Validate is always exercised before a caller posts.
func BuildJournal(sourceTxID uuid.UUID, originalCurrency, reportingCurrency string, rate decimal.Decimal, legs []Leg) (Journal, error) {
	if len(legs) == 0 {
		return Journal{}, fmt.Errorf("accounting: cannot build a journal with no legs")
	}

	lines := make([]JournalLine, 0, len(legs))
	for _, leg := range legs {
		if leg.Amount.LessThanOrEqual(decimal.Zero) {
			return Journal{}, fmt.Errorf("accounting: leg on account %s has non-positive amount %s", leg.AccountID, leg.Amount)
		}
		reportingAmount := leg.Amount
		if originalCurrency != reportingCurrency {
			reportingAmount = leg.Amount.Mul(rate).Round(2)
		}
		lines = append(lines, JournalLine{
			AccountID:       leg.AccountID,
			Side:             leg.Side,
			OriginalAmount:   leg.Amount,
			ReportingAmount:  reportingAmount,
		})
	}

	j := Journal{
		SourceTransactionID: sourceTxID,
		ReportingCurrency:    reportingCurrency,
		Lines:                 lines,
	}
	if err := j.Validate(); err != nil {
		return Journal{}, err
	}
	return j, nil
}

// ChartAccounts is the minimal set of resolved account IDs a posting rule needs.
// The caller (the accounting service) resolves these from the company's chart
// of accounts before calling a posting rule; posting rules never look accounts
// up themselves, keeping them pure and independently testable against the
// worked examples in BranchLedger_System_Documentation section 5.5 and 5.12.
type ChartAccounts struct {
	// reportingCurrency is set via NewChartAccounts; it travels with the
	// resolved account set since every account in it belongs to one company
	// with exactly one reporting currency.
	reportingCurrency string

	Cash             uuid.UUID
	Bank              uuid.UUID
	AccountsReceivable uuid.UUID
	AccountsPayable    uuid.UUID
	SalesRevenue        uuid.UUID
	SalesReturns          uuid.UUID
	CostOfSales             uuid.UUID
	Inventory                uuid.UUID
	OperatingExpense           uuid.UUID
	FixedAsset                  uuid.UUID
	StockLossExpense               uuid.UUID
	FundsInTransit                   uuid.UUID
	OwnerCapital                       uuid.UUID
}

// NewChartAccounts builds a ChartAccounts for a company, recording its
// reporting currency so posting rules know which currency a journal's
// reporting-side amounts are in. The storage package's repository
// implementation calls this after resolving account codes to IDs.
func NewChartAccounts(reportingCurrency string, ids ChartAccounts) ChartAccounts {
	ids.reportingCurrency = reportingCurrency
	return ids
}

// CashSale posts "Cash sale" + "Goods consumed by sale" together (architecture
// section 5.5): Dr Cash / Cr Sales revenue for the price, and — only when the
// sale actually moved stock — Dr Cost of sales / Cr Inventory for the
// weighted-average cost. A zero costAmount (a service line, or any sale line
// with no product_id) omits the cost-of-sales legs entirely rather than
// posting a zero-amount pair, which BuildJournal would reject outright since
// every leg must be strictly positive. netAmount and costAmount are both in
// the transaction's original currency.
func CashSale(txID uuid.UUID, accts ChartAccounts, netAmount, costAmount decimal.Decimal, currency, reportingCurrency string, rate decimal.Decimal) (Journal, error) {
	legs := []Leg{
		{AccountID: accts.Cash, Side: Debit, Amount: netAmount},
		{AccountID: accts.SalesRevenue, Side: Credit, Amount: netAmount},
	}
	if costAmount.GreaterThan(decimal.Zero) {
		legs = append(legs,
			Leg{AccountID: accts.CostOfSales, Side: Debit, Amount: costAmount},
			Leg{AccountID: accts.Inventory, Side: Credit, Amount: costAmount},
		)
	}
	return BuildJournal(txID, currency, reportingCurrency, rate, legs)
}

// CreditSale posts "Credit sale": Dr Accounts receivable / Cr Sales revenue,
// plus the matching cost-of-sales movement when the sale moved stock (see
// CashSale), same shape as CashSale but against the receivable account
// instead of cash.
func CreditSale(txID uuid.UUID, accts ChartAccounts, netAmount, costAmount decimal.Decimal, currency, reportingCurrency string, rate decimal.Decimal) (Journal, error) {
	legs := []Leg{
		{AccountID: accts.AccountsReceivable, Side: Debit, Amount: netAmount},
		{AccountID: accts.SalesRevenue, Side: Credit, Amount: netAmount},
	}
	if costAmount.GreaterThan(decimal.Zero) {
		legs = append(legs,
			Leg{AccountID: accts.CostOfSales, Side: Debit, Amount: costAmount},
			Leg{AccountID: accts.Inventory, Side: Credit, Amount: costAmount},
		)
	}
	return BuildJournal(txID, currency, reportingCurrency, rate, legs)
}

// CustomerReceipt posts "Customer receipt": Dr Bank / Cr Accounts receivable.
// It settles debt and must never recognise revenue again (architecture section
// 5.5 credit-sale example).
func CustomerReceipt(txID uuid.UUID, accts ChartAccounts, amount decimal.Decimal, currency, reportingCurrency string, rate decimal.Decimal) (Journal, error) {
	legs := []Leg{
		{AccountID: accts.Bank, Side: Debit, Amount: amount},
		{AccountID: accts.AccountsReceivable, Side: Credit, Amount: amount},
	}
	return BuildJournal(txID, currency, reportingCurrency, rate, legs)
}

// CreditPurchase posts "Goods bought on credit": Dr Inventory / Cr Accounts payable.
func CreditPurchase(txID uuid.UUID, accts ChartAccounts, amount decimal.Decimal, currency, reportingCurrency string, rate decimal.Decimal) (Journal, error) {
	legs := []Leg{
		{AccountID: accts.Inventory, Side: Debit, Amount: amount},
		{AccountID: accts.AccountsPayable, Side: Credit, Amount: amount},
	}
	return BuildJournal(txID, currency, reportingCurrency, rate, legs)
}

// SupplierPayment posts "Supplier payment": Dr Accounts payable / Cr Bank.
func SupplierPayment(txID uuid.UUID, accts ChartAccounts, amount decimal.Decimal, currency, reportingCurrency string, rate decimal.Decimal) (Journal, error) {
	legs := []Leg{
		{AccountID: accts.AccountsPayable, Side: Debit, Amount: amount},
		{AccountID: accts.Bank, Side: Credit, Amount: amount},
	}
	return BuildJournal(txID, currency, reportingCurrency, rate, legs)
}

// CashExpense posts "Cash operating expense": Dr Operating expense / Cr Cash.
func CashExpense(txID uuid.UUID, accts ChartAccounts, amount decimal.Decimal, currency, reportingCurrency string, rate decimal.Decimal) (Journal, error) {
	legs := []Leg{
		{AccountID: accts.OperatingExpense, Side: Debit, Amount: amount},
		{AccountID: accts.Cash, Side: Credit, Amount: amount},
	}
	return BuildJournal(txID, currency, reportingCurrency, rate, legs)
}

// StockWaste posts "Stock waste": Dr Stock loss expense / Cr Inventory.
func StockWaste(txID uuid.UUID, accts ChartAccounts, amount decimal.Decimal, currency, reportingCurrency string, rate decimal.Decimal) (Journal, error) {
	legs := []Leg{
		{AccountID: accts.StockLossExpense, Side: Debit, Amount: amount},
		{AccountID: accts.Inventory, Side: Credit, Amount: amount},
	}
	return BuildJournal(txID, currency, reportingCurrency, rate, legs)
}

// StockCountAdjustment posts a stock-count reconciliation's net valuation
// change (section 5.7, "Stock count": "Compare counted and expected stock at
// a defined cut-off"). A negative change — counted stock below the recorded
// position — posts the same Dr Stock loss expense / Cr Inventory entry as
// StockWaste. A positive change (a surplus found on count) posts the mirror
// entry against the same account: the architecture names no separate "stock
// gain" account, so one variance account nets both directions, the usual
// treatment for count-based inventory reconciliation.
func StockCountAdjustment(txID uuid.UUID, accts ChartAccounts, valueChange decimal.Decimal, currency, reportingCurrency string, rate decimal.Decimal) (Journal, error) {
	amount := valueChange.Abs()
	legs := []Leg{
		{AccountID: accts.StockLossExpense, Side: Debit, Amount: amount},
		{AccountID: accts.Inventory, Side: Credit, Amount: amount},
	}
	if valueChange.GreaterThan(decimal.Zero) {
		legs = []Leg{
			{AccountID: accts.Inventory, Side: Debit, Amount: amount},
			{AccountID: accts.StockLossExpense, Side: Credit, Amount: amount},
		}
	}
	return BuildJournal(txID, currency, reportingCurrency, rate, legs)
}

// TransferDispatch posts "Transfer dispatch": Dr Funds in transit / Cr Sender cash.
// Internal transfers never count as company sales (User Manual, Stock and
// branch transfers).
func TransferDispatch(txID uuid.UUID, accts ChartAccounts, amount decimal.Decimal, currency, reportingCurrency string, rate decimal.Decimal) (Journal, error) {
	legs := []Leg{
		{AccountID: accts.FundsInTransit, Side: Debit, Amount: amount},
		{AccountID: accts.Cash, Side: Credit, Amount: amount},
	}
	return BuildJournal(txID, currency, reportingCurrency, rate, legs)
}

// TransferReceipt posts "Transfer receipt": Dr Receiver cash / Cr Funds in transit.
// Consolidated clearing is zero once a transfer is fully received.
func TransferReceipt(txID uuid.UUID, accts ChartAccounts, amount decimal.Decimal, currency, reportingCurrency string, rate decimal.Decimal) (Journal, error) {
	legs := []Leg{
		{AccountID: accts.Cash, Side: Debit, Amount: amount},
		{AccountID: accts.FundsInTransit, Side: Credit, Amount: amount},
	}
	return BuildJournal(txID, currency, reportingCurrency, rate, legs)
}

// CashRefund posts "Cash refund with recovered cost": Dr Sales returns and
// Inventory / Cr Cash and Cost of sales. The refund must link to the original
// sale (User Manual: "Returns must link to the original record where possible").
func CashRefund(txID uuid.UUID, accts ChartAccounts, refundAmount, recoveredCost decimal.Decimal, currency, reportingCurrency string, rate decimal.Decimal) (Journal, error) {
	legs := []Leg{
		{AccountID: accts.SalesReturns, Side: Debit, Amount: refundAmount},
		{AccountID: accts.Inventory, Side: Debit, Amount: recoveredCost},
		{AccountID: accts.Cash, Side: Credit, Amount: refundAmount},
		{AccountID: accts.CostOfSales, Side: Credit, Amount: recoveredCost},
	}
	return BuildJournal(txID, currency, reportingCurrency, rate, legs)
}

// OwnerCapitalInjection posts "Owner capital": Dr Bank / Cr Capital.
func OwnerCapitalInjection(txID uuid.UUID, accts ChartAccounts, amount decimal.Decimal, currency, reportingCurrency string, rate decimal.Decimal) (Journal, error) {
	legs := []Leg{
		{AccountID: accts.Bank, Side: Debit, Amount: amount},
		{AccountID: accts.OwnerCapital, Side: Credit, Amount: amount},
	}
	return BuildJournal(txID, currency, reportingCurrency, rate, legs)
}

// Reversal builds the mirror-image journal of an existing posted journal: every
// debit becomes a credit and vice versa, same amounts. Posted entries remain
// immutable; a reversal is how a correction is recorded (architecture section
// "Accounting transaction boundary").
func Reversal(reversalTxID uuid.UUID, original Journal) (Journal, error) {
	lines := make([]JournalLine, 0, len(original.Lines))
	for _, l := range original.Lines {
		flipped := Debit
		if l.Side == Debit {
			flipped = Credit
		}
		lines = append(lines, JournalLine{
			AccountID:       l.AccountID,
			Side:             flipped,
			OriginalAmount:   l.OriginalAmount,
			ReportingAmount:  l.ReportingAmount,
		})
	}
	j := Journal{
		SourceTransactionID: reversalTxID,
		ReportingCurrency:    original.ReportingCurrency,
		Lines:                 lines,
	}
	if err := j.Validate(); err != nil {
		return Journal{}, err
	}
	return j, nil
}
