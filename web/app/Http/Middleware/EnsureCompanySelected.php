<?php

namespace App\Http\Middleware;

use Closure;
use Illuminate\Http\Request;
use Symfony\Component\HttpFoundation\Response;

/**
 * A user may belong to more than one company (BranchLedger_System_Documentation
 * section 4: "Company ... Customer tenant and financial reporting boundary").
 * Every authenticated action needs exactly one current company in scope, so
 * this redirects to the picker until one is chosen.
 */
class EnsureCompanySelected
{
    public function handle(Request $request, Closure $next): Response
    {
        if (! $request->session()->has('current_company_id')) {
            if ($request->expectsJson() || $request->is('api/*')) {
                return response()->json(['error_code' => 'no_company_selected'], 409);
            }

            return redirect()->route('companies.select');
        }

        return $next($request);
    }
}
