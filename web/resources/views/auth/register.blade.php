@extends('layouts.app')
@section('title', 'Register your company · BranchLedger')
@section('body')
<div class="shell" style="max-width: 520px; margin: 6vh auto 0;">
    <h1 style="text-align:center;">BranchLedger</h1>
    <p class="page-subtitle" style="text-align:center;">Register your company and its main branch — you'll be its owner.</p>
    <div class="panel">
        @if ($errors->any())
            <div class="errors">
                @foreach ($errors->all() as $error)
                    <div>{{ $error }}</div>
                @endforeach
            </div>
        @endif

        <form method="POST" action="{{ route('register') }}">
            @csrf
            <h2>You</h2>
            <div class="grid grid-2">
                <div>
                    <label for="full_name">Full name</label>
                    <input id="full_name" name="full_name" type="text" value="{{ old('full_name') }}" required autofocus>
                </div>
                <div>
                    <label for="email">Email</label>
                    <input id="email" name="email" type="email" value="{{ old('email') }}" required>
                </div>
            </div>
            <div class="grid grid-2">
                <div>
                    <label for="password">Password</label>
                    <input id="password" name="password" type="password" required minlength="12">
                </div>
                <div>
                    <label for="password_confirmation">Confirm password</label>
                    <input id="password_confirmation" name="password_confirmation" type="password" required minlength="12">
                </div>
            </div>

            <h2 style="margin-top:20px;">Your company</h2>
            <div class="grid grid-2">
                <div>
                    <label for="company_name">Company name</label>
                    <input id="company_name" name="company_name" type="text" value="{{ old('company_name') }}" required>
                </div>
                <div>
                    <label for="primary_category">Primary category</label>
                    <select id="primary_category" name="primary_category">
                        <option value="retail_wholesale" selected>Retail / wholesale</option>
                        <option value="grocery">Grocery</option>
                        <option value="restaurant">Restaurant</option>
                        <option value="service">Service</option>
                        <option value="other">Other</option>
                    </select>
                </div>
            </div>
            <div class="grid grid-2">
                <div>
                    <label for="reporting_currency">Reporting currency</label>
                    <input id="reporting_currency" name="reporting_currency" type="text" value="{{ old('reporting_currency', 'USD') }}" maxlength="3" required style="text-transform:uppercase;">
                </div>
                <div>
                    <label for="timezone">Timezone</label>
                    <input id="timezone" name="timezone" type="text" value="{{ old('timezone', 'Africa/Harare') }}" required>
                </div>
            </div>
            <label for="financial_year_start">Financial year start</label>
            <input id="financial_year_start" name="financial_year_start" type="date" value="{{ old('financial_year_start', now()->startOfYear()->toDateString()) }}" required>

            <h2 style="margin-top:20px;">Your main branch</h2>
            <div class="grid grid-2">
                <div>
                    <label for="branch_name">Branch name</label>
                    <input id="branch_name" name="branch_name" type="text" value="{{ old('branch_name', 'Main Branch') }}" required>
                </div>
                <div>
                    <label for="branch_code">Branch code</label>
                    <input id="branch_code" name="branch_code" type="text" value="{{ old('branch_code', 'MAIN') }}" maxlength="32" required style="text-transform:uppercase;">
                </div>
            </div>

            <button type="submit" style="margin-top:20px;">Create company and sign in</button>
        </form>
        <p class="muted" style="margin-top:16px;">
            Already have an account? <a href="{{ route('login') }}">Sign in</a>
        </p>
    </div>
</div>
@endsection
