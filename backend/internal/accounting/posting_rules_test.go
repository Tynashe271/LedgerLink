package accounting

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// d is a small test helper for literal decimals.
func d(s string) decimal.Decimal {
	v, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return v
}

func testAccounts() ChartAccounts {
	return NewChartAccounts("USD", ChartAccounts{
		Cash: uuid.New(), Bank: uuid.New(), AccountsReceivable: uuid.New(),
		AccountsPayable: uuid.New(), SalesRevenue: uuid.New(), SalesReturns: uuid.New(),
		CostOfSales: uuid.New(), Inventory: uuid.New(), OperatingExpense: uuid.New(),
		FixedAsset: uuid.New(), StockLossExpense: uuid.New(), FundsInTransit: uuid.New(),
		OwnerCapital: uuid.New(),
	})
}

// TestPostingCatalogueWorkedExamples reproduces every row of the posting
// catalogue table in BranchLedger_System_Documentation section 5.5, asserting
// each journal balances and lands on the correct accounts and amounts.
func TestPostingCatalogueWorkedExamples(t *testing.T) {
	a := testAccounts()
	rate := d("1")

	t.Run("cash sale 100 at cost 60", func(t *testing.T) {
		j, err := CashSale(uuid.New(), a, d("100"), d("60"), "USD", "USD", rate)
		if err != nil {
			t.Fatalf("CashSale: %v", err)
		}
		if !j.TotalDebits().Equal(d("160")) || !j.TotalCredits().Equal(d("160")) {
			t.Fatalf("expected both sides 160, got debits=%s credits=%s", j.TotalDebits(), j.TotalCredits())
		}
	})

	// A sale line with no product_id (a service, or the dashboard demo form)
	// has no inventory movement: costAmount is zero. CashSale/CreditSale must
	// omit the cost-of-sales legs rather than post a zero-amount pair, which
	// BuildJournal rejects outright (every leg must be strictly positive).
	t.Run("cash sale with zero cost omits cost-of-sales legs", func(t *testing.T) {
		j, err := CashSale(uuid.New(), a, d("25"), decimal.Zero, "USD", "USD", rate)
		if err != nil {
			t.Fatalf("CashSale with zero cost: %v", err)
		}
		if len(j.Lines) != 2 {
			t.Fatalf("expected exactly 2 legs (cash/revenue), got %d", len(j.Lines))
		}
		for _, l := range j.Lines {
			if l.AccountID == a.CostOfSales || l.AccountID == a.Inventory {
				t.Fatalf("zero-cost sale must not touch cost of sales or inventory")
			}
		}
	})

	t.Run("credit sale 200", func(t *testing.T) {
		j, err := CreditSale(uuid.New(), a, d("200"), d("120"), "USD", "USD", rate)
		if err != nil {
			t.Fatalf("CreditSale: %v", err)
		}
		if !j.TotalDebits().Equal(d("320")) {
			t.Fatalf("expected total debits 320, got %s", j.TotalDebits())
		}
	})

	t.Run("customer receipt settles debt without new revenue", func(t *testing.T) {
		j, err := CustomerReceipt(uuid.New(), a, d("200"), "USD", "USD", rate)
		if err != nil {
			t.Fatalf("CustomerReceipt: %v", err)
		}
		for _, l := range j.Lines {
			if l.AccountID == a.SalesRevenue {
				t.Fatalf("customer receipt must never touch sales revenue")
			}
		}
	})

	t.Run("goods bought on credit 150", func(t *testing.T) {
		j, err := CreditPurchase(uuid.New(), a, d("150"), "USD", "USD", rate)
		if err != nil {
			t.Fatalf("CreditPurchase: %v", err)
		}
		if !j.TotalDebits().Equal(d("150")) {
			t.Fatalf("expected 150, got %s", j.TotalDebits())
		}
	})

	t.Run("stock waste 15", func(t *testing.T) {
		j, err := StockWaste(uuid.New(), a, d("15"), "USD", "USD", rate)
		if err != nil {
			t.Fatalf("StockWaste: %v", err)
		}
		if err := j.Validate(); err != nil {
			t.Fatalf("waste journal does not balance: %v", err)
		}
	})

	t.Run("transfer dispatch then receipt nets to zero in transit", func(t *testing.T) {
		dispatch, err := TransferDispatch(uuid.New(), a, d("100"), "USD", "USD", rate)
		if err != nil {
			t.Fatalf("TransferDispatch: %v", err)
		}
		receipt, err := TransferReceipt(uuid.New(), a, d("100"), "USD", "USD", rate)
		if err != nil {
			t.Fatalf("TransferReceipt: %v", err)
		}
		// Funds in transit: debited 100 on dispatch, credited 100 on receipt -> net zero.
		var transitNet decimal.Decimal
		for _, l := range dispatch.Lines {
			if l.AccountID == a.FundsInTransit {
				transitNet = transitNet.Add(l.ReportingAmount)
			}
		}
		for _, l := range receipt.Lines {
			if l.AccountID == a.FundsInTransit {
				transitNet = transitNet.Sub(l.ReportingAmount)
			}
		}
		if !transitNet.IsZero() {
			t.Fatalf("expected funds-in-transit to net to zero after full receipt, got %s", transitNet)
		}
	})
}

// TestFinancialFixtureControlTotals reproduces the financial fixture in
// section 5.12: after the first five posting rows (owner capital, inventory
// credit purchase, cash sale, rent expense, supplier payment), the spec
// asserts closing bank 900, cash 170, inventory 180, assets 1,250, supplier
// payable 200, capital 1,000, current profit 50 — and assets must equal
// liabilities plus equity (1,250 = 1,250). This test is AT15 / T15 made
// executable.
func TestFinancialFixtureControlTotals(t *testing.T) {
	a := testAccounts()
	rate := d("1")

	bank := decimal.Zero
	cash := decimal.Zero
	inventory := decimal.Zero
	payable := decimal.Zero
	capital := decimal.Zero
	revenue := decimal.Zero
	costOfSales := decimal.Zero
	rentExpense := decimal.Zero

	// debitIncreases records, per account, whether a debit line increases
	// (true: assets and expenses) or decreases (false: liabilities, equity,
	// income) that account's natural balance.
	debitIncreases := map[uuid.UUID]bool{
		a.Bank: true, a.Cash: true, a.Inventory: true, a.CostOfSales: true, a.OperatingExpense: true,
		a.AccountsPayable: false, a.OwnerCapital: false, a.SalesRevenue: false,
	}
	balances := map[uuid.UUID]*decimal.Decimal{
		a.Bank: &bank, a.Cash: &cash, a.Inventory: &inventory, a.AccountsPayable: &payable,
		a.OwnerCapital: &capital, a.SalesRevenue: &revenue, a.CostOfSales: &costOfSales,
		a.OperatingExpense: &rentExpense,
	}

	applyJournal := func(j Journal) {
		for _, l := range j.Lines {
			balance, tracked := balances[l.AccountID]
			if !tracked {
				continue
			}
			delta := l.ReportingAmount
			if (l.Side == Debit) != debitIncreases[l.AccountID] {
				delta = delta.Neg()
			}
			*balance = balance.Add(delta)
		}
	}

	capitalJ, err := OwnerCapitalInjection(uuid.New(), a, d("1000"), "USD", "USD", rate)
	if err != nil {
		t.Fatalf("capital: %v", err)
	}
	applyJournal(capitalJ)

	purchaseJ, err := CreditPurchase(uuid.New(), a, d("300"), "USD", "USD", rate)
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	applyJournal(purchaseJ)

	saleJ, err := CashSale(uuid.New(), a, d("200"), d("120"), "USD", "USD", rate)
	if err != nil {
		t.Fatalf("sale: %v", err)
	}
	applyJournal(saleJ)

	rentJ, err := CashExpense(uuid.New(), a, d("30"), "USD", "USD", rate)
	if err != nil {
		t.Fatalf("rent: %v", err)
	}
	applyJournal(rentJ)

	paymentJ, err := SupplierPayment(uuid.New(), a, d("100"), "USD", "USD", rate)
	if err != nil {
		t.Fatalf("payment: %v", err)
	}
	applyJournal(paymentJ)

	if !bank.Equal(d("900")) {
		t.Errorf("bank: want 900, got %s", bank)
	}
	if !cash.Equal(d("170")) {
		t.Errorf("cash: want 170, got %s", cash)
	}
	if !inventory.Equal(d("180")) {
		t.Errorf("inventory: want 180, got %s", inventory)
	}
	if !payable.Equal(d("200")) {
		t.Errorf("payable: want 200, got %s", payable)
	}
	if !capital.Equal(d("1000")) {
		t.Errorf("capital: want 1000, got %s", capital)
	}

	assets := bank.Add(cash).Add(inventory)
	profit := revenue.Sub(costOfSales).Sub(rentExpense)
	liabilitiesPlusEquity := payable.Add(capital).Add(profit)

	if !assets.Equal(d("1250")) {
		t.Errorf("assets: want 1250, got %s", assets)
	}
	if !profit.Equal(d("50")) {
		t.Errorf("current profit: want 50, got %s", profit)
	}
	if !assets.Equal(liabilitiesPlusEquity) {
		t.Errorf("balance sheet does not balance: assets %s != liabilities+equity %s", assets, liabilitiesPlusEquity)
	}
}

// TestJournalRejectsUnbalanced ensures BuildJournal refuses an unbalanced leg
// set outright (T05: "Post unbalanced journal" must reject the entire
// transaction).
func TestJournalRejectsUnbalanced(t *testing.T) {
	a := testAccounts()
	_, err := BuildJournal(uuid.New(), "USD", "USD", d("1"), []Leg{
		{AccountID: a.Cash, Side: Debit, Amount: d("100")},
		{AccountID: a.SalesRevenue, Side: Credit, Amount: d("99")},
	})
	if err == nil {
		t.Fatal("expected an error for an unbalanced journal, got nil")
	}
}

// TestForeignCurrencyExample matches the architecture's example: original 100
// at rate 2 posts 200 reporting units.
func TestForeignCurrencyExample(t *testing.T) {
	a := testAccounts()
	j, err := CashSale(uuid.New(), a, d("100"), d("0.01"), "ZAR", "USD", d("2"))
	if err != nil {
		t.Fatalf("CashSale: %v", err)
	}
	for _, l := range j.Lines {
		if l.AccountID == a.Cash {
			if !l.OriginalAmount.Equal(d("100")) {
				t.Errorf("original amount: want 100, got %s", l.OriginalAmount)
			}
			if !l.ReportingAmount.Equal(d("200")) {
				t.Errorf("reporting amount: want 200, got %s", l.ReportingAmount)
			}
		}
	}
}

// TestReversalFlipsSides ensures a reversal mirrors every line, preserving
// amounts but swapping debit/credit, and still balances.
func TestReversalFlipsSides(t *testing.T) {
	a := testAccounts()
	original, err := CashSale(uuid.New(), a, d("100"), d("60"), "USD", "USD", d("1"))
	if err != nil {
		t.Fatalf("CashSale: %v", err)
	}
	rev, err := Reversal(uuid.New(), original)
	if err != nil {
		t.Fatalf("Reversal: %v", err)
	}
	if len(rev.Lines) != len(original.Lines) {
		t.Fatalf("reversal line count mismatch")
	}
	for i, l := range rev.Lines {
		orig := original.Lines[i]
		if l.Side == orig.Side {
			t.Errorf("line %d side not flipped", i)
		}
		if !l.ReportingAmount.Equal(orig.ReportingAmount) {
			t.Errorf("line %d amount changed on reversal", i)
		}
	}
}
