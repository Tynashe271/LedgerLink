<?php

namespace App\Providers;

use App\Services\DelegationTokenSigner;
use Illuminate\Auth\Notifications\ResetPassword;
use Illuminate\Support\ServiceProvider;

class AppServiceProvider extends ServiceProvider
{
    /**
     * Register any application services.
     */
    public function register(): void
    {
        $this->app->singleton(DelegationTokenSigner::class, function () {
            $secret = config('branchledger.delegation_secret');
            abort_unless(is_string($secret) && $secret !== '', 500, 'DELEGATION_SECRET is not configured');

            return new DelegationTokenSigner(
                secretHex: $secret,
                ttlSeconds: config('branchledger.delegation_ttl_seconds'),
            );
        });
    }

    /**
     * Bootstrap any application services.
     */
    public function boot(): void
    {
        // Point password-reset emails at our own Blade route instead of the
        // framework default, keeping the notification itself unremarkable
        // either way (launch control #29: no account-existence signal).
        ResetPassword::createUrlUsing(function ($notifiable, string $token) {
            return url(route('password.reset', [
                'token' => $token,
                'email' => $notifiable->getEmailForPasswordReset(),
            ], false));
        });
    }
}
