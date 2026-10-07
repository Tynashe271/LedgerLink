<?php

use App\Http\Controllers\Auth\LoginController;
use App\Http\Controllers\Auth\PasswordResetController;
use App\Http\Controllers\BranchOverviewController;
use App\Http\Controllers\CompanyController;
use App\Http\Controllers\DashboardController;
use App\Http\Controllers\GatewayController;
use App\Http\Controllers\WorkspaceController;
use Illuminate\Support\Facades\Route;

Route::redirect('/', '/login');

// --- Guest routes: session + CSRF only, no auth required -------------------
Route::middleware('guest')->group(function () {
    Route::get('/login', [LoginController::class, 'create'])->name('login');
    Route::post('/login', [LoginController::class, 'store']);

    Route::get('/forgot-password', [PasswordResetController::class, 'create'])->name('password.request');
    Route::post('/forgot-password', [PasswordResetController::class, 'sendResetLink'])->name('password.email');

    Route::get('/reset-password/{token}', [PasswordResetController::class, 'edit'])->name('password.reset');
    Route::post('/reset-password', [PasswordResetController::class, 'update'])->name('password.update');
});

// --- Authenticated routes ----------------------------------------------------
Route::middleware('auth')->group(function () {
    Route::post('/logout', [LoginController::class, 'destroy'])->name('logout');

    Route::get('/companies/select', [CompanyController::class, 'select'])->name('companies.select');
    Route::post('/companies/select', [CompanyController::class, 'choose'])->name('companies.choose');

    Route::middleware('company.selected')->group(function () {
        Route::get('/dashboard', [DashboardController::class, 'show'])->name('dashboard');
        Route::get('/branch-overview', [BranchOverviewController::class, 'show'])->name('branch-overview');

        // Daily-operations screens (System Documentation 5.2 screen
        // catalogue). Each just renders its shell; data is loaded and
        // posted client-side through the /api gateway below, the same
        // pattern the dashboard already uses.
        Route::get('/sales', [WorkspaceController::class, 'sales'])->name('workspace.sales');
        Route::get('/expenses', [WorkspaceController::class, 'expenses'])->name('workspace.expenses');
        Route::get('/purchases', [WorkspaceController::class, 'purchases'])->name('workspace.purchases');
        Route::get('/transfers', [WorkspaceController::class, 'transfers'])->name('workspace.transfers');
        Route::get('/closes', [WorkspaceController::class, 'closes'])->name('workspace.closes');
        Route::get('/approvals', [WorkspaceController::class, 'approvals'])->name('workspace.approvals');
        Route::get('/sync', [WorkspaceController::class, 'syncCentre'])->name('workspace.sync-centre');
        Route::get('/products', [WorkspaceController::class, 'products'])->name('workspace.products');
        Route::get('/customers', [WorkspaceController::class, 'customers'])->name('workspace.customers');
        Route::get('/stock', [WorkspaceController::class, 'stock'])->name('workspace.stock');
        Route::get('/reconciliation', [WorkspaceController::class, 'reconciliation'])->name('workspace.reconciliation');
        Route::get('/reports', [WorkspaceController::class, 'reports'])->name('workspace.reports');
        Route::get('/settings', [WorkspaceController::class, 'settings'])->name('workspace.settings');

        // The same-origin API gateway (architecture: "Laravel routes /api
        // requests through an authenticated gateway to Go"). Stays under the
        // 'web' group (via the outer 'auth' middleware here, which itself
        // sits on the default web stack) so session auth and CSRF apply to
        // every state-changing call exactly as they do to a Blade form post.
        Route::any('/api/{path}', [GatewayController::class, 'proxy'])
            ->where('path', '.*')
            ->name('api.gateway');
    });
});
