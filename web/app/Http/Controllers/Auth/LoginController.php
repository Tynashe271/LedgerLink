<?php

namespace App\Http\Controllers\Auth;

use App\Http\Controllers\Controller;
use App\Models\User;
use Illuminate\Http\RedirectResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\RateLimiter;
use Illuminate\Support\Str;
use Illuminate\View\View;

/**
 * Login/logout. Matches the Version 1.1 launch safeguards addendum's "Safe
 * sign in and account recovery" and "Abuse resistance" sections: generic
 * failure responses, progressive lockout, session rotation on login and
 * revocation on logout.
 */
class LoginController extends Controller
{
    private const MAX_ATTEMPTS = 5;

    private const LOCKOUT_MINUTES = 15;

    public function create(): View
    {
        return view('auth.login');
    }

    public function store(Request $request): RedirectResponse
    {
        $credentials = $request->validate([
            'email' => ['required', 'email'],
            'password' => ['required', 'string'],
        ]);

        // Keyed by email+IP, not email alone, so one attacker spraying many
        // accounts from one address is also throttled.
        $throttleKey = Str::lower($credentials['email']).'|'.$request->ip();

        if (RateLimiter::tooManyAttempts($throttleKey, self::MAX_ATTEMPTS)) {
            return back()->withErrors([
                'email' => 'Too many attempts. Please try again in a few minutes.',
            ])->onlyInput('email');
        }

        $user = User::where('email', $credentials['email'])->first();

        if ($user && $user->locked_until && $user->locked_until->isFuture()) {
            // Same generic message as a plain bad-password failure — a
            // temporary lockout must not be a distinguishable signal that
            // the account exists (launch control #29 + #50).
            RateLimiter::hit($throttleKey, self::LOCKOUT_MINUTES * 60);

            return back()->withErrors(['email' => __('auth.failed')])->onlyInput('email');
        }

        if (! Auth::attempt($credentials, $request->boolean('remember'))) {
            RateLimiter::hit($throttleKey, self::LOCKOUT_MINUTES * 60);
            if ($user) {
                $this->recordFailedAttempt($user);
            }

            return back()->withErrors(['email' => __('auth.failed')])->onlyInput('email');
        }

        RateLimiter::clear($throttleKey);
        $this->clearLockout($user);

        // Session fixation defence: a new session ID is issued at the moment
        // of authentication (addendum: "Rotate session IDs after login").
        $request->session()->regenerate();

        return redirect()->intended(route('companies.select'));
    }

    public function destroy(Request $request): RedirectResponse
    {
        Auth::guard('web')->logout();
        $request->session()->invalidate();
        $request->session()->regenerateToken();

        return redirect()->route('login');
    }

    private function recordFailedAttempt(User $user): void
    {
        $count = $user->failed_login_count + 1;
        $update = ['failed_login_count' => $count];
        if ($count >= self::MAX_ATTEMPTS) {
            $update['locked_until'] = now()->addMinutes(self::LOCKOUT_MINUTES);
        }
        DB::table('users')->where('id', $user->id)->update($update);
    }

    private function clearLockout(?User $user): void
    {
        if (! $user) {
            return;
        }
        DB::table('users')->where('id', $user->id)->update([
            'failed_login_count' => 0,
            'locked_until' => null,
        ]);
    }
}
