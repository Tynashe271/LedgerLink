<?php

namespace App\Http\Controllers;

use Illuminate\Http\RedirectResponse;
use Illuminate\Http\Request;
use Illuminate\View\View;

/**
 * Lets a signed-in user pick which company they're acting as. The choice is
 * stored in the Laravel session only (architecture: Laravel's data
 * ownership is "Session metadata only") and is what GatewayController reads
 * to decide which company_id goes into the delegation token.
 */
class CompanyController extends Controller
{
    public function select(Request $request): View
    {
        $memberships = $request->user()->activeMemberships()->with('company')->get();

        return view('companies.select', ['memberships' => $memberships]);
    }

    public function choose(Request $request): RedirectResponse
    {
        $validated = $request->validate([
            'company_id' => ['required', 'uuid'],
        ]);

        $membership = $request->user()->activeMemberships()
            ->where('company_id', $validated['company_id'])
            ->first();

        if (! $membership) {
            return back()->withErrors(['company_id' => 'You do not have access to that company.']);
        }

        $request->session()->put('current_company_id', $membership->company_id);
        $request->session()->put('current_company_role', $membership->role);

        return redirect()->route('dashboard');
    }
}
