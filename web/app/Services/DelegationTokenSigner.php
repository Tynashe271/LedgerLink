<?php

namespace App\Services;

use Carbon\CarbonImmutable;
use Illuminate\Support\Str;

/**
 * Signs the delegated-identity token Go's internal/auth.Verifier checks on
 * every /api/v1/* request (BranchLedger_Architecture.pdf: "Laravel routes
 * /api requests through an authenticated gateway to Go, with verified
 * identity context and request IDs. Go independently validates the
 * delegated identity and scopes.").
 *
 * Wire format must match backend/internal/auth/delegation.go exactly:
 * base64url(json claims) + "." + base64url(hmac-sha256(secret, json bytes))
 * Go verifies the signature over the raw payload bytes it receives, so field
 * order doesn't matter — only the JSON key names and value shapes do.
 */
class DelegationTokenSigner
{
    public function __construct(
        private readonly string $secretHex,
        private readonly int $ttlSeconds,
    ) {}

    /**
     * @param  array<string>  $branchScope  Branch UUIDs, or [] for unrestricted
     *                                      (company-wide) access.
     * @param  array<string>  $delegations  Explicit per-action grants on top
     *                                      of role (tenancy.Action values —
     *                                      see Membership::delegationsArray).
     */
    public function sign(
        string $userId,
        string $companyId,
        string $role,
        array $branchScope,
        int $accessVersion,
        string $sessionId,
        array $delegations = [],
    ): string {
        $secret = hex2bin($this->secretHex);
        $now = CarbonImmutable::now('UTC');
        $expires = $now->addSeconds($this->ttlSeconds);

        $claims = [
            'user_id' => $userId,
            'company_id' => $companyId,
            'role' => $role,
            'branch_scope' => array_values($branchScope),
            'access_version' => $accessVersion,
            'session_id' => $sessionId,
            'request_id' => (string) Str::uuid(),
            'issued_at' => $this->formatForGo($now),
            'expires_at' => $this->formatForGo($expires),
            'delegations' => array_values($delegations),
        ];

        $payload = json_encode($claims, JSON_UNESCAPED_SLASHES);
        $signature = hash_hmac('sha256', $payload, $secret, true);

        return $this->base64url($payload).'.'.$this->base64url($signature);
    }

    /**
     * Go's time.Time unmarshals any valid RFC3339 string; microsecond
     * precision with a literal "Z" offset parses cleanly.
     */
    private function formatForGo(CarbonImmutable $t): string
    {
        return $t->format('Y-m-d\TH:i:s.u\Z');
    }

    private function base64url(string $raw): string
    {
        return rtrim(strtr(base64_encode($raw), '+/', '-_'), '=');
    }
}
