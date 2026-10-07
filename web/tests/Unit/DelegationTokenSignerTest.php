<?php

namespace Tests\Unit;

use App\Services\DelegationTokenSigner;
use PHPUnit\Framework\TestCase;

/**
 * Verifies the wire format independently of Go: Go's
 * internal/auth.Verifier (backend/internal/auth/delegation.go) checks the
 * signature over the raw payload bytes it receives, so what matters here is
 * that (a) the signature is a correct HMAC-SHA256 over the exact payload
 * bytes, (b) both halves are unpadded base64url, and (c) the JSON carries
 * the field names and shapes Go's Claims struct expects.
 */
class DelegationTokenSignerTest extends TestCase
{
    private const SECRET_HEX = '839dc4ab8928751dbec86faccea77e4d6719ce70e7f353a1d6276f9c46104fba';

    public function test_token_has_two_base64url_parts_and_valid_signature(): void
    {
        $signer = new DelegationTokenSigner(self::SECRET_HEX, ttlSeconds: 60);

        $token = $signer->sign(
            userId: '33333333-3333-3333-3333-333333333333',
            companyId: '11111111-1111-1111-1111-111111111111',
            role: 'owner',
            branchScope: [],
            accessVersion: 1,
            sessionId: 'test-session',
        );

        [$payloadB64, $sigB64] = explode('.', $token, 2);

        // base64url: no '+', '/' or '=' padding characters.
        $this->assertDoesNotMatchRegularExpression('/[+\/=]/', $payloadB64);
        $this->assertDoesNotMatchRegularExpression('/[+\/=]/', $sigB64);

        $payload = base64_decode(strtr($payloadB64, '-_', '+/'));
        $expectedSig = hash_hmac('sha256', $payload, hex2bin(self::SECRET_HEX), true);
        $actualSig = base64_decode(strtr($sigB64, '-_', '+/'));

        $this->assertSame($expectedSig, $actualSig, 'signature must be HMAC-SHA256 over the exact payload bytes');

        $claims = json_decode($payload, true);
        $this->assertSame('33333333-3333-3333-3333-333333333333', $claims['user_id']);
        $this->assertSame('11111111-1111-1111-1111-111111111111', $claims['company_id']);
        $this->assertSame('owner', $claims['role']);
        $this->assertSame([], $claims['branch_scope']);
        $this->assertSame(1, $claims['access_version']);
        $this->assertSame('test-session', $claims['session_id']);
        $this->assertNotEmpty($claims['request_id']);
        $this->assertMatchesRegularExpression('/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/', $claims['issued_at']);
        $this->assertMatchesRegularExpression('/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/', $claims['expires_at']);
    }

    public function test_each_call_mints_a_fresh_request_id(): void
    {
        $signer = new DelegationTokenSigner(self::SECRET_HEX, ttlSeconds: 60);
        $sign = fn () => $signer->sign('u', 'c', 'owner', [], 1, 's');

        [$a] = explode('.', $sign());
        [$b] = explode('.', $sign());

        $this->assertNotSame($a, $b, 'two signings of identical inputs must still differ (fresh request_id/timestamp)');
    }

    public function test_branch_scope_is_preserved_in_order(): void
    {
        $signer = new DelegationTokenSigner(self::SECRET_HEX, ttlSeconds: 60);
        $branches = ['22222222-2222-2222-2222-222222222222', '44444444-4444-4444-4444-444444444444'];

        $token = $signer->sign('u', 'c', 'branch_manager', $branches, 1, 's');
        [$payloadB64] = explode('.', $token, 2);
        $claims = json_decode(base64_decode(strtr($payloadB64, '-_', '+/')), true);

        $this->assertSame($branches, $claims['branch_scope']);
    }

    public function test_delegations_default_to_empty_and_are_preserved_when_given(): void
    {
        $signer = new DelegationTokenSigner(self::SECRET_HEX, ttlSeconds: 60);

        $withoutDelegations = $signer->sign('u', 'c', 'staff', [], 1, 's');
        [$payloadB64] = explode('.', $withoutDelegations, 2);
        $claims = json_decode(base64_decode(strtr($payloadB64, '-_', '+/')), true);
        $this->assertSame([], $claims['delegations']);

        $grants = ['reverse_posting', 'approve_spending'];
        $withDelegations = $signer->sign('u', 'c', 'owner', [], 1, 's', delegations: $grants);
        [$payloadB64] = explode('.', $withDelegations, 2);
        $claims = json_decode(base64_decode(strtr($payloadB64, '-_', '+/')), true);
        $this->assertSame($grants, $claims['delegations']);
    }
}
