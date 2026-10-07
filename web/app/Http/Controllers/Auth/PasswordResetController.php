<?php

namespace App\Http\Controllers\Auth;

use App\Http\Controllers\Controller;
use Illuminate\Auth\Events\PasswordReset;
use Illuminate\Http\RedirectResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Hash;
use Illuminate\Support\Facades\Password;
use Illuminate\View\View;

/**
 * Forgot/reset password. Uses Laravel's own token broker (reset tokens are
 * hashed at rest by the framework) and always returns the same generic
 * response regardless of whether the email matched an account (launch
 * control #29: user enumeration).
 */
class PasswordResetController extends Controller
{
    public function create(): View
    {
        return view('auth.forgot-password');
    }

    public function sendResetLink(Request $request): RedirectResponse
    {
        $request->validate(['email' => ['required', 'email']]);

        // The broker's status is intentionally not branched on in the
        // response — success or "no such user" look identical to the caller.
        Password::broker()->sendResetLink($request->only('email'));

        return back()->with('status', 'If an account exists for that address, a reset link has been sent.');
    }

    public function edit(Request $request): View
    {
        return view('auth.reset-password', [
            'token' => $request->route('token'),
            'email' => $request->query('email'),
        ]);
    }

    public function update(Request $request): RedirectResponse
    {
        $request->validate([
            'token' => ['required'],
            'email' => ['required', 'email'],
            'password' => ['required', 'string', 'min:12', 'confirmed'],
        ]);

        $status = Password::broker()->reset(
            $request->only('email', 'password', 'password_confirmation', 'token'),
            function ($user, string $password) {
                // Our column is password_hash, not Laravel's default
                // "password" — see App\Models\User::getAuthPassword().
                $user->forceFill(['password_hash' => Hash::make($password)])->save();

                // Addendum: "After a password change, previous online
                // sessions must end." Laravel's own session store is the one
                // "online session" this process can revoke directly; the Go
                // side independently stops honouring the old access_version
                // once this bump lands (checked on next online validation).
                DB::table('users')
                    ->where('id', $user->id)
                    ->increment('access_version');

                DB::table('sessions')
                    ->where('user_id', $user->id)
                    ->delete();

                event(new PasswordReset($user));
            }
        );

        if ($status !== Password::PASSWORD_RESET) {
            // Same generic wording whether the token was invalid, expired,
            // or the email didn't match — no distinguishing signal.
            return back()->withErrors(['email' => 'This password reset link is invalid or has expired.']);
        }

        return redirect()->route('login')->with('status', 'Your password has been reset. Please sign in.');
    }
}
