<?php

namespace Tests\Feature;

use App\Models\User;
use Illuminate\Foundation\Testing\DatabaseTransactions;
use Illuminate\Support\Facades\Hash;
use Tests\TestCase;

/**
 * Exercises login against the real shared schema (not a Laravel-only
 * fixture): BranchLedger's own tables are schema-owned by Go and applied
 * directly with psql (see database/migrations/0001_init_schema.up.sql), so
 * these tests run inside a DB transaction against that already-migrated
 * schema rather than letting RefreshDatabase try to manage it.
 */
class AuthenticationTest extends TestCase
{
    use DatabaseTransactions;

    private function makeUser(string $password = 'correct-horse-battery-staple'): User
    {
        return User::create([
            'email' => 'test-'.uniqid().'@example.com',
            'password_hash' => Hash::make($password),
            'full_name' => 'Test User',
        ]);
    }

    public function test_user_can_log_in_with_correct_credentials(): void
    {
        $user = $this->makeUser('correct-horse-battery-staple');

        $response = $this->post('/login', [
            'email' => $user->email,
            'password' => 'correct-horse-battery-staple',
        ]);

        $this->assertAuthenticatedAs($user->fresh());
        $response->assertRedirect(route('companies.select'));
    }

    public function test_login_fails_with_wrong_password(): void
    {
        $user = $this->makeUser('correct-horse-battery-staple');

        $response = $this->from('/login')->post('/login', [
            'email' => $user->email,
            'password' => 'totally-wrong',
        ]);

        $this->assertGuest();
        $response->assertRedirect('/login');
        $response->assertSessionHasErrors('email');
    }

    public function test_failed_logins_increment_counter_and_eventually_lock(): void
    {
        $user = $this->makeUser('correct-horse-battery-staple');

        for ($i = 0; $i < 5; $i++) {
            $this->post('/login', ['email' => $user->email, 'password' => 'wrong']);
        }

        $user->refresh();
        $this->assertSame(5, $user->failed_login_count);
        $this->assertNotNull($user->locked_until);
        $this->assertTrue($user->locked_until->isFuture());

        // Even the CORRECT password is rejected while locked — the lockout
        // itself, not just the password check, must block sign-in.
        $response = $this->post('/login', ['email' => $user->email, 'password' => 'correct-horse-battery-staple']);
        $this->assertGuest();
        $response->assertSessionHasErrors('email');
    }

    public function test_successful_login_clears_a_prior_failed_counter(): void
    {
        $user = $this->makeUser('correct-horse-battery-staple');
        $this->post('/login', ['email' => $user->email, 'password' => 'wrong']);

        $this->post('/login', ['email' => $user->email, 'password' => 'correct-horse-battery-staple']);

        $user->refresh();
        $this->assertSame(0, $user->failed_login_count);
        $this->assertNull($user->locked_until);
    }

    public function test_dashboard_requires_authentication(): void
    {
        $this->get('/dashboard')->assertRedirect('/login');
    }

    public function test_dashboard_requires_a_selected_company(): void
    {
        $user = $this->makeUser();
        $this->actingAs($user)->get('/dashboard')->assertRedirect(route('companies.select'));
    }
}
