<?php

namespace App\Http\Controllers;

use App\Models\Branch;
use App\Models\Company;
use Illuminate\Http\Request;
use Illuminate\Http\Response;
use Illuminate\View\View;

/**
 * Post-registration confirmation: shows and lets the owner download their
 * login reference (login email, company name, branch code, ...) right
 * after RegisterController::store creates the account, since there is
 * otherwise no record of it anywhere outside their own memory. Never
 * includes the password — it is only ever known to the user and the
 * bcrypt hash (launch control #21), so there is nothing safe to show here
 * beyond a reminder that they set it.
 */
class OnboardingController extends Controller
{
    public function welcome(Request $request): View
    {
        $companyId = $request->session()->get('current_company_id');
        $company = Company::findOrFail($companyId);
        $mainBranch = Branch::where('company_id', $companyId)->where('is_main_branch', true)->first();

        return view('onboarding.welcome', [
            'company' => $company,
            'branch' => $mainBranch,
            'role' => $request->session()->get('current_company_role'),
        ]);
    }

    public function download(Request $request): Response
    {
        $companyId = $request->session()->get('current_company_id');
        $company = Company::findOrFail($companyId);
        $mainBranch = Branch::where('company_id', $companyId)->where('is_main_branch', true)->first();
        $user = $request->user();

        $lines = [
            'BranchLedger login details',
            '===========================',
            '',
            'Login email: '.$user->email,
            'Company: '.$company->name,
            'Branch: '.($mainBranch->name ?? '').' ('.($mainBranch->code ?? '').')',
            'Primary category: '.$company->primary_category,
            'Reporting currency: '.$company->reporting_currency,
        ];

        if ($company->zimra_tax_number) {
            $lines[] = 'ZIMRA tax number: '.$company->zimra_tax_number;
        }

        $lines[] = '';
        $lines[] = 'Your password is not included here — it was set during';
        $lines[] = 'registration and only you know it. Keep this file private;';
        $lines[] = 'it identifies which account and company to sign in to.';
        $lines[] = '';
        $lines[] = 'Generated '.now()->toDayDateTimeString();

        $filename = 'branchledger-login-'.\Illuminate\Support\Str::slug($company->name).'.txt';

        return response(implode("\n", $lines)."\n", 200, [
            'Content-Type' => 'text/plain; charset=UTF-8',
            'Content-Disposition' => 'attachment; filename="'.$filename.'"',
        ]);
    }
}
