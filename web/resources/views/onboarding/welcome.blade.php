@extends('layouts.app')
@section('title', 'Welcome · BranchLedger')
@section('body')
<h1>Your company is set up</h1>
<p class="page-subtitle">Here are your login details for reference. Download a copy before you continue — this page won't show your password.</p>

<div class="panel">
    <h2>Login details</h2>
    <table>
        <tbody>
            <tr><td class="muted">Login email</td><td>{{ auth()->user()->email }}</td></tr>
            <tr><td class="muted">Company</td><td>{{ $company->name }}</td></tr>
            <tr><td class="muted">Main branch</td><td>{{ $branch->name ?? '' }} ({{ $branch->code ?? '' }})</td></tr>
            <tr><td class="muted">Primary category</td><td>{{ ucfirst(str_replace('_', ' ', $company->primary_category)) }}</td></tr>
            <tr><td class="muted">Reporting currency</td><td>{{ $company->reporting_currency }}</td></tr>
            @if ($company->zimra_tax_number)
                <tr><td class="muted">ZIMRA tax number</td><td>{{ $company->zimra_tax_number }}</td></tr>
            @endif
            <tr><td class="muted">Role</td><td>{{ ucfirst($role ?? '') }}</td></tr>
        </tbody>
    </table>

    <p class="muted" style="margin-top:14px;">
        Your password isn't shown here — only you know it, it was set when you registered.
        Keep the downloaded file private; it identifies which account and company to sign in to.
    </p>

    <div class="btn-row" style="margin-top:16px;">
        <a href="{{ route('onboarding.download') }}"><button type="button">Download login details</button></a>
        <a href="{{ route('dashboard') }}"><button type="button" class="secondary">Continue to dashboard</button></a>
    </div>
</div>
@endsection
