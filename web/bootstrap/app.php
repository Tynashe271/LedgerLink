<?php

use App\Http\Middleware\EnsureCompanySelected;
use Illuminate\Foundation\Application;
use Illuminate\Foundation\Configuration\Exceptions;
use Illuminate\Foundation\Configuration\Middleware;
use Illuminate\Http\Request;

return Application::configure(basePath: dirname(__DIR__))
    ->withRouting(
        web: __DIR__.'/../routes/web.php',
        commands: __DIR__.'/../routes/console.php',
        health: '/up',
    )
    ->withMiddleware(function (Middleware $middleware): void {
        $middleware->alias([
            'company.selected' => EnsureCompanySelected::class,
        ]);

        // Nginx terminates TLS and sits in front of this process
        // (architecture: "Host a TLS reverse proxy ... in separated
        // deployment units"), so trust its X-Forwarded-* headers for
        // correct scheme/IP detection. In production this should be the
        // proxy's actual fixed IP/CIDR, not a wildcard.
        $middleware->trustProxies(at: '*');
    })
    ->withExceptions(function (Exceptions $exceptions): void {
        $exceptions->shouldRenderJsonWhen(
            fn (Request $request) => $request->is('api/*') || $request->expectsJson(),
        );
    })->create();
