package reporting

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

func TestTrialBalanceTotalsAlwaysReconcile(t *testing.T) {
	store := &fakeStore{
		currency: "USD",
		trialBalanceLines: []TrialBalanceLine{
			{AccountCode: "1000", AccountName: "Cash", AccountType: "asset", Debit: d("500")},
			{AccountCode: "4000", AccountName: "Sales Revenue", AccountType: "income", Credit: d("300")},
			{AccountCode: "6000", AccountName: "Operating Expense", AccountType: "expense", Debit: d("50")},
			{AccountCode: "3000", AccountName: "Owner Capital", AccountType: "equity", Credit: d("250")},
		},
	}
	svc := NewService(store)
	scope := tenancy.Scope{CompanyID: uuid.New(), BranchScope: nil}

	result, err := svc.TrialBalance(context.Background(), scope, nil, time.Now())
	if err != nil {
		t.Fatalf("TrialBalance: %v", err)
	}
	if !result.TotalDebits.Equal(d("550")) {
		t.Fatalf("total debits: want 550, got %s", result.TotalDebits)
	}
	if !result.TotalCredits.Equal(d("550")) {
		t.Fatalf("total credits: want 550, got %s", result.TotalCredits)
	}
	if !result.TotalDebits.Equal(result.TotalCredits) {
		t.Fatalf("trial balance does not reconcile: debits %s != credits %s", result.TotalDebits, result.TotalCredits)
	}
	if result.PostingBasis != "posted" {
		t.Fatalf("posting basis: want \"posted\", got %q", result.PostingBasis)
	}
}

func TestTrialBalanceRejectsBranchOutsideScope(t *testing.T) {
	store := &fakeStore{currency: "USD"}
	svc := NewService(store)
	scope := tenancy.Scope{CompanyID: uuid.New(), BranchScope: []uuid.UUID{uuid.New()}}
	outsideBranch := uuid.New()

	if _, err := svc.TrialBalance(context.Background(), scope, &outsideBranch, time.Now()); err == nil {
		t.Fatal("expected an error for a branch outside the caller's scope")
	}
}

func TestAgeingBucketsByDaysPastDue(t *testing.T) {
	asOf := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{
		currency: "USD",
		ageingSourceRows: []OverdueDebtAlert{
			{CustomerName: "Not yet due", OutstandingAmount: d("10"), OldestDueDate: asOf.AddDate(0, 0, 5)},
			{CustomerName: "Due today", OutstandingAmount: d("20"), OldestDueDate: asOf},
			{CustomerName: "15 days late", OutstandingAmount: d("30"), OldestDueDate: asOf.AddDate(0, 0, -15)},
			{CustomerName: "45 days late", OutstandingAmount: d("40"), OldestDueDate: asOf.AddDate(0, 0, -45)},
			{CustomerName: "75 days late", OutstandingAmount: d("50"), OldestDueDate: asOf.AddDate(0, 0, -75)},
			{CustomerName: "120 days late", OutstandingAmount: d("60"), OldestDueDate: asOf.AddDate(0, 0, -120)},
		},
	}
	svc := NewService(store)
	scope := tenancy.Scope{CompanyID: uuid.New()}

	result, err := svc.Ageing(context.Background(), scope, nil, asOf)
	if err != nil {
		t.Fatalf("Ageing: %v", err)
	}
	if !result.TotalOutstanding.Equal(d("210")) {
		t.Fatalf("total outstanding: want 210, got %s", result.TotalOutstanding)
	}

	byName := map[string]AgeingRow{}
	for _, r := range result.Rows {
		byName[r.CustomerName] = r
	}

	cases := []struct {
		name   string
		bucket func(AgeingRow) string
		want   string
	}{
		{"Not yet due", func(r AgeingRow) string { return r.Current.String() }, "10"},
		{"Due today", func(r AgeingRow) string { return r.Current.String() }, "20"},
		{"15 days late", func(r AgeingRow) string { return r.Days1To30.String() }, "30"},
		{"45 days late", func(r AgeingRow) string { return r.Days31To60.String() }, "40"},
		{"75 days late", func(r AgeingRow) string { return r.Days61To90.String() }, "50"},
		{"120 days late", func(r AgeingRow) string { return r.DaysOver90.String() }, "60"},
	}
	for _, c := range cases {
		got := c.bucket(byName[c.name])
		if got != c.want {
			t.Errorf("%s: want bucket amount %s, got %s (row=%+v)", c.name, c.want, got, byName[c.name])
		}
	}
}
