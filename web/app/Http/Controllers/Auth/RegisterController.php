<?php

namespace App\Http\Controllers\Auth;

use App\Http\Controllers\Controller;
use App\Models\Branch;
use App\Models\Company;
use App\Models\Membership;
use App\Models\User;
use Illuminate\Http\RedirectResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Hash;
use Illuminate\View\View;

/**
 * Self-service sign-up: System Documentation 6.2's "Company setup" module
 * ("Company name, branch code, category, timezone, financial year,
 * reporting currency, enabled modules"). This is the one place Laravel is
 * allowed to write companies/branches/memberships directly rather than
 * going through Go's internal/settings — every other write to those tables
 * requires an existing membership to sign a delegation token from, and a
 * brand-new company has none yet. Registration is how the very first
 * membership comes to exist.
 */
class RegisterController extends Controller
{
    public function create(): View
    {
        return view('auth.register');
    }

    public function store(Request $request): RedirectResponse
    {
        $validated = $request->validate([
            'full_name' => ['required', 'string', 'max:160'],
            'email' => ['required', 'email', 'max:255', 'unique:users,email'],
            'password' => ['required', 'string', 'min:12', 'confirmed'],
            'company_name' => ['required', 'string', 'max:160'],
            'primary_category' => ['required', 'string', 'max:40'],
            'branch_name' => ['required', 'string', 'max:160'],
            'branch_code' => ['required', 'string', 'max:32'],
            'timezone' => ['required', 'string', 'max:64'],
            'reporting_currency' => ['required', 'string', 'size:3'],
            'financial_year_start' => ['required', 'date'],
        ]);

        [$user, $membership] = DB::transaction(function () use ($validated) {
            $user = User::create([
                'full_name' => $validated['full_name'],
                'email' => $validated['email'],
                'password_hash' => Hash::make($validated['password']),
            ]);

            $company = Company::create([
                'name' => $validated['company_name'],
                'primary_category' => $validated['primary_category'],
                'reporting_currency' => strtoupper($validated['reporting_currency']),
                'timezone' => $validated['timezone'],
                'financial_year_start' => $validated['financial_year_start'],
            ]);

            Branch::create([
                'company_id' => $company->id,
                'name' => $validated['branch_name'],
                'code' => strtoupper($validated['branch_code']),
                'category' => $validated['primary_category'],
                'is_main_branch' => true,
            ]);

            // Without an open period nothing can ever post at all (Go's
            // accounting.Service.Post looks one up by document_date and
            // rejects with period_closed if none covers it) — a full
            // financial year starting from the date just chosen, matching
            // how database/seeds/dev_seed.sql sets up its own one period.
            $yearStart = \Carbon\Carbon::parse($validated['financial_year_start']);
            DB::table('periods')->insert([
                'id' => (string) \Illuminate\Support\Str::uuid(),
                'company_id' => $company->id,
                'starts_on' => $yearStart->toDateString(),
                'ends_on' => $yearStart->copy()->addYear()->subDay()->toDateString(),
                'status' => 'open',
                'created_at' => now(),
            ]);

            // The standard chart of accounts storage.txImpl.GetChartAccounts
            // resolves by code — without these rows every posting rule
            // (CashSale, CreditPurchase, ...) would resolve to a zero UUID.
            // Exactly the codes/types/is_cash_like flags dev_seed.sql seeds
            // for the fixture company, so a real and a demo company behave
            // identically.
            $standardAccounts = [
                ['1000', 'Cash', 'asset', true],
                ['1010', 'Bank', 'asset', true],
                ['1100', 'Accounts Receivable', 'asset', false],
                ['1200', 'Inventory', 'asset', false],
                ['1300', 'Funds In Transit', 'asset', false],
                ['1500', 'Fixed Assets', 'asset', false],
                ['2000', 'Accounts Payable', 'liability', false],
                ['3000', 'Owner Capital', 'equity', false],
                ['4000', 'Sales Revenue', 'income', false],
                ['4100', 'Sales Returns', 'income', false],
                ['5000', 'Cost of Sales', 'expense', false],
                ['5100', 'Stock Loss Expense', 'expense', false],
                ['6000', 'Operating Expense', 'expense', false],
            ];
            DB::table('accounts')->insert(array_map(fn (array $a) => [
                'id' => (string) \Illuminate\Support\Str::uuid(),
                'company_id' => $company->id,
                'code' => $a[0],
                'name' => $a[1],
                'account_type' => $a[2],
                'is_cash_like' => $a[3],
                'is_active' => true,
                'created_at' => now(),
            ], $standardAccounts));

            // The founding owner is the only person who could possibly grant
            // these delegations — there is nobody else in the company yet —
            // so section 4.6's needsDelegation actions for the owner role
            // (approve_spending, reverse_posting, lock_period,
            // change_chart_of_accounts) are granted outright at signup
            // rather than left for an admin step that has no admin to do it.
            // manage_users and export_financial_data need no delegation for
            // Owner per the matrix already.
            $membership = Membership::create([
                'user_id' => $user->id,
                'company_id' => $company->id,
                'role' => 'owner',
                'branch_scope' => '{}',
                'delegations' => '{approve_spending,reverse_posting,lock_period,change_chart_of_accounts}',
                'is_active' => true,
            ]);

            return [$user, $membership];
        });

        Auth::login($user);

        // Session fixation defence, same as LoginController::store.
        $request->session()->regenerate();
        $request->session()->put('current_company_id', $membership->company_id);
        $request->session()->put('current_company_role', $membership->role);

        return redirect()->route('dashboard')->with('status', 'Welcome to BranchLedger — your company is set up.');
    }
}
