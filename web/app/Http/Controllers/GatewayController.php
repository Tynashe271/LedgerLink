<?php

namespace App\Http\Controllers;

use App\Models\Membership;
use App\Services\DelegationTokenSigner;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Http;
use Illuminate\Support\Facades\Log;
use Symfony\Component\HttpFoundation\Response;

/**
 * The same-origin API gateway described in BranchLedger_Architecture.pdf:
 * the browser only ever talks to Laravel; Laravel independently derives who
 * the caller is from its own verified session (never from a client-supplied
 * header) and signs a fresh, short-lived delegation token for Go on every
 * call. Laravel never writes financial tables or reimplements posting
 * rules — this controller only translates and forwards.
 */
class GatewayController extends Controller
{
    public function __construct(private readonly DelegationTokenSigner $signer) {}

    public function proxy(Request $request, string $path = ''): Response
    {
        $user = $request->user();
        $companyId = $request->session()->get('current_company_id');

        // EnsureCompanySelected middleware should already guarantee both of
        // these, but the gateway re-checks independently rather than
        // trusting an upstream middleware never to regress.
        if (! $user || ! $companyId) {
            return response()->json(['error_code' => 'unauthenticated'], 401);
        }

        $membership = Membership::query()
            ->where('user_id', $user->id)
            ->where('company_id', $companyId)
            ->where('is_active', true)
            ->first();

        if (! $membership) {
            return response()->json(['error_code' => 'no_active_membership'], 403);
        }

        $token = $this->signer->sign(
            userId: (string) $user->id,
            companyId: (string) $companyId,
            role: $membership->role,
            branchScope: $membership->branchScopeArray(),
            accessVersion: (int) $user->access_version,
            sessionId: $request->session()->getId(),
            delegations: $membership->delegationsArray(),
        );

        // The {path} route parameter already captures everything after
        // "/api/" (e.g. "v1/transactions"), so it maps directly onto Go's
        // own "/api/<path>" — prepending another "v1/" here would double it.
        $goBaseUrl = rtrim(config('branchledger.go_api_base_url'), '/');
        $targetUrl = $goBaseUrl.'/api/'.ltrim($path, '/');

        $pendingRequest = Http::withHeaders([
            'X-BranchLedger-Delegation' => $token,
            'Content-Type' => 'application/json',
            'Accept' => 'application/json',
        ])->timeout(30);

        try {
            $upstream = match ($request->method()) {
                'GET' => $pendingRequest->get($targetUrl, $request->query()),
                'POST' => $pendingRequest->post($targetUrl, $request->json()->all()),
                'PUT' => $pendingRequest->put($targetUrl, $request->json()->all()),
                'PATCH' => $pendingRequest->patch($targetUrl, $request->json()->all()),
                'DELETE' => $pendingRequest->delete($targetUrl, $request->json()->all()),
                default => throw new \InvalidArgumentException('unsupported method'),
            };
        } catch (\InvalidArgumentException) {
            return response()->json(['error_code' => 'method_not_allowed'], 405);
        } catch (\Throwable $e) {
            // The upstream error, not the exception message, is what's safe
            // to show a browser (launch control #4: no stack traces).
            Log::error('gateway proxy failed', ['path' => $path, 'error' => $e->getMessage()]);

            return response()->json(['error_code' => 'upstream_unavailable'], 502);
        }

        return response($upstream->body(), $upstream->status())
            ->header('Content-Type', $upstream->header('Content-Type') ?: 'application/json');
    }
}
