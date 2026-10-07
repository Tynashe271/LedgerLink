<?php

namespace Tests\Feature;

use App\Models\Branch;
use App\Models\Company;
use App\Models\Membership;
use App\Models\User;
use Illuminate\Foundation\Testing\DatabaseTransactions;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Hash;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

/**
 * Exercises the /api/* gateway (GatewayController): that it signs a
 * delegation token derived only from the server-side session (never from a
 * client header — architecture: "Reject client-provided identity headers"),
 * forwards to the configured Go API, and relays the response back.
 */
class GatewayTest extends TestCase
{
    use DatabaseTransactions;

    private function signedInOwner(): array
    {
        $company = Company::create([
            'name' => 'Test Co '.uniqid(),
            'primary_category' => 'retail_wholesale',
            'reporting_currency' => 'USD',
            'financial_year_start' => '2026-01-01',
        ]);
        $branch = Branch::create([
            'company_id' => $company->id,
            'name' => 'Main',
            'code' => 'MAIN-'.uniqid(),
            'category' => 'retail_wholesale',
            'is_main_branch' => true,
        ]);
        $user = User::create([
            'email' => 'gw-'.uniqid().'@example.com',
            'password_hash' => Hash::make('irrelevant-for-this-test'),
            'full_name' => 'Gateway Tester',
        ]);
        DB::statement(
            'INSERT INTO memberships (user_id, company_id, role, branch_scope, is_active) VALUES (?, ?, ?, ?, true)',
            [$user->id, $company->id, 'owner', '{}']
        );

        return [$user, $company, $branch];
    }

    public function test_proxy_forwards_to_go_with_signed_delegation_header_and_no_v1_duplication(): void
    {
        [$user, $company] = $this->signedInOwner();

        Http::fake([
            config('branchledger.go_api_base_url').'/api/v1/transactions' => Http::response(
                ['operation_id' => 'x', 'accepted' => true, 'transaction_id' => 'y', 'version' => 1], 201
            ),
        ]);

        $response = $this->actingAs($user)
            ->withSession(['current_company_id' => $company->id, 'current_company_role' => 'owner'])
            ->postJson('/api/v1/transactions', ['document_type' => 'sale']);

        $response->assertStatus(201);
        $response->assertJson(['accepted' => true]);

        Http::assertSent(function ($request) use ($company, $user) {
            if ($request->url() !== config('branchledger.go_api_base_url').'/api/v1/transactions') {
                return false;
            }
            if (! $request->hasHeader('X-BranchLedger-Delegation')) {
                return false;
            }
            $token = $request->header('X-BranchLedger-Delegation')[0];
            [$payloadB64] = explode('.', $token, 2);
            $claims = json_decode(base64_decode(strtr($payloadB64, '-_', '+/')), true);

            return $claims['company_id'] === $company->id
                && $claims['user_id'] === $user->id
                && $claims['role'] === 'owner';
        });
    }

    public function test_proxy_requires_an_active_membership_in_the_current_company(): void
    {
        [$user, $company] = $this->signedInOwner();

        // Deactivate the membership after it was used to select the company,
        // simulating a revoked user whose session hasn't caught up yet.
        DB::table('memberships')->where('user_id', $user->id)->update(['is_active' => false]);

        $response = $this->actingAs($user)
            ->withSession(['current_company_id' => $company->id])
            ->postJson('/api/v1/transactions', ['document_type' => 'sale']);

        $response->assertStatus(403);
        $response->assertJson(['error_code' => 'no_active_membership']);
    }

    public function test_client_cannot_inject_its_own_delegation_header(): void
    {
        [$user, $company] = $this->signedInOwner();

        $seenToken = null;
        Http::fake(function ($request) use (&$seenToken) {
            $seenToken = $request->header('X-BranchLedger-Delegation')[0] ?? null;

            return Http::response(['accepted' => true], 201);
        });

        $this->actingAs($user)
            ->withSession(['current_company_id' => $company->id])
            ->postJson('/api/v1/transactions', ['document_type' => 'sale'], [
                'X-BranchLedger-Delegation' => 'attacker-forged-token',
            ]);

        $this->assertNotSame('attacker-forged-token', $seenToken);
    }
}
