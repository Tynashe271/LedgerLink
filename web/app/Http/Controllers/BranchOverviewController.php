<?php

namespace App\Http\Controllers;

use App\Models\Branch;
use App\Models\Company;
use App\Models\Membership;
use Illuminate\Http\Request;
use Illuminate\View\View;

/**
 * Renders the Branch overview screen (System Documentation 5.2: "Daily
 * totals, quick entry and sync", required state "Assigned branch and
 * operating date") — distinct from the Manager overview (dashboard.php),
 * which is company-wide with a flexible period. This screen is always one
 * branch, always today: the branch_manager/branch_staff roles who use it day
 * to day have exactly one branch in their membership's branch_scope, so
 * there is nothing to pick. A role with no branch restriction (branch_scope
 * empty, e.g. owner/accountant) still gets a picker, defaulting to the main
 * branch, so they can check in on any one branch's day without going
 * through the multi-branch Manager overview.
 */
class BranchOverviewController extends Controller
{
    public function show(Request $request): View
    {
        $companyId = $request->session()->get('current_company_id');
        $company = Company::findOrFail($companyId);
        $branches = Branch::where('company_id', $companyId)->orderByDesc('is_main_branch')->get();

        $membership = Membership::query()
            ->where('user_id', $request->user()->id)
            ->where('company_id', $companyId)
            ->where('is_active', true)
            ->first();
        $branchScope = $membership?->branchScopeArray() ?? [];
        $assignedBranchId = count($branchScope) === 1 ? $branchScope[0] : null;

        return view('branch-overview', [
            'company' => $company,
            'branches' => $branches,
            'role' => $request->session()->get('current_company_role'),
            'assignedBranchId' => $assignedBranchId,
        ]);
    }
}
