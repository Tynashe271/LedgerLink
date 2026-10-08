<?php

namespace App\Http\Controllers;

use App\Models\Branch;
use App\Models\Company;
use Illuminate\Http\Request;
use Illuminate\View\View;

/**
 * Renders the daily-operations screens from the screen catalogue
 * (BranchLedger_System_Documentation section 5.2): Sales, Expenses,
 * Purchases, Transfers, Daily close, Approvals. Each view loads and posts
 * its own data client-side through the /api gateway (GatewayController),
 * the same pattern DashboardController's view already uses — this
 * controller only supplies the company/branch/role context every screen
 * needs for its branch picker and role-aware behaviour.
 */
class WorkspaceController extends Controller
{
    public function alerts(Request $request): View
    {
        return $this->render($request, 'workspace.alerts', 'Alerts');
    }

    public function transactions(Request $request): View
    {
        return $this->render($request, 'workspace.transactions', 'Transactions');
    }

    public function sales(Request $request): View
    {
        return $this->render($request, 'workspace.sales', 'Sales');
    }

    public function expenses(Request $request): View
    {
        return $this->render($request, 'workspace.expenses', 'Expenses');
    }

    public function purchases(Request $request): View
    {
        return $this->render($request, 'workspace.purchases', 'Purchases');
    }

    public function transfers(Request $request): View
    {
        return $this->render($request, 'workspace.transfers', 'Transfers');
    }

    public function closes(Request $request): View
    {
        return $this->render($request, 'workspace.closes', 'Daily close');
    }

    public function approvals(Request $request): View
    {
        return $this->render($request, 'workspace.approvals', 'Approvals');
    }

    public function syncCentre(Request $request): View
    {
        return $this->render($request, 'workspace.sync-centre', 'Sync centre');
    }

    public function products(Request $request): View
    {
        return $this->render($request, 'workspace.products', 'Products');
    }

    public function customers(Request $request): View
    {
        return $this->render($request, 'workspace.customers', 'Customers');
    }

    public function stock(Request $request): View
    {
        return $this->render($request, 'workspace.stock', 'Stock');
    }

    public function reconciliation(Request $request): View
    {
        return $this->render($request, 'workspace.reconciliation', 'Reconciliation');
    }

    public function reports(Request $request): View
    {
        return $this->render($request, 'workspace.reports', 'Reports');
    }

    public function settings(Request $request): View
    {
        return $this->render($request, 'workspace.settings', 'Settings');
    }

    public function auditLog(Request $request): View
    {
        return $this->render($request, 'workspace.audit-log', 'Audit log');
    }

    private function render(Request $request, string $view, string $title): View
    {
        $companyId = $request->session()->get('current_company_id');
        $company = Company::findOrFail($companyId);
        $branches = Branch::where('company_id', $companyId)->orderByDesc('is_main_branch')->get();

        return view($view, [
            'company' => $company,
            'branches' => $branches,
            'role' => $request->session()->get('current_company_role'),
            'title' => $title,
        ]);
    }
}
