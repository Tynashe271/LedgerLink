<?php

namespace App\Http\Controllers;

use App\Models\Branch;
use App\Models\Company;
use Illuminate\Http\Request;
use Illuminate\View\View;

class DashboardController extends Controller
{
    public function show(Request $request): View
    {
        $companyId = $request->session()->get('current_company_id');
        $company = Company::findOrFail($companyId);
        $branches = Branch::where('company_id', $companyId)->orderByDesc('is_main_branch')->get();

        return view('dashboard', [
            'company' => $company,
            'branches' => $branches,
            'role' => $request->session()->get('current_company_role'),
        ]);
    }
}
