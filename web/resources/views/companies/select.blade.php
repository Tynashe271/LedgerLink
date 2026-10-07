@extends('layouts.app')
@section('title', 'Select company · BranchLedger')
@section('body')
<header class="topbar">
    <span class="brand">BranchLedger</span>
    <form method="POST" action="{{ route('logout') }}">@csrf<button type="submit" style="width:auto; margin:0; background:transparent; color:#6b7280; border:1px solid #e2e4ea;">Sign out</button></form>
</header>
<div class="shell" style="max-width: 560px;">
    <h1>Select a company</h1>
    <div class="panel">
        @if ($errors->any())
            <div class="errors">
                @foreach ($errors->all() as $error)
                    <div>{{ $error }}</div>
                @endforeach
            </div>
        @endif

        @forelse ($memberships as $membership)
            <form method="POST" action="{{ route('companies.choose') }}" style="margin-bottom:12px;">
                @csrf
                <input type="hidden" name="company_id" value="{{ $membership->company_id }}">
                <button type="submit" style="text-align:left; display:flex; justify-content:space-between;">
                    <span>{{ $membership->company->name }}</span>
                    <span class="muted" style="color:#d6e0ff;">{{ ucfirst(str_replace('_', ' ', $membership->role)) }}</span>
                </button>
            </form>
        @empty
            <p class="muted">You don't have access to any company yet. Ask your administrator to invite you.</p>
        @endforelse
    </div>
</div>
@endsection
