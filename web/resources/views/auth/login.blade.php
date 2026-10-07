@extends('layouts.app')
@section('title', 'Sign in · BranchLedger')
@section('body')
<div class="shell" style="max-width: 420px; margin-top: 10vh;">
    <h1 style="text-align:center;">BranchLedger</h1>
    <div class="panel">
        @if ($errors->any())
            <div class="errors">
                @foreach ($errors->all() as $error)
                    <div>{{ $error }}</div>
                @endforeach
            </div>
        @endif
        @if (session('status'))
            <div class="status">{{ session('status') }}</div>
        @endif

        <form method="POST" action="{{ route('login') }}">
            @csrf
            <label for="email">Email</label>
            <input id="email" type="email" name="email" value="{{ old('email') }}" required autofocus>

            <label for="password">Password</label>
            <input id="password" type="password" name="password" required>

            <label style="display:flex; align-items:center; gap:8px; margin-top:12px;">
                <input type="checkbox" name="remember" style="width:auto;"> Remember me
            </label>

            <button type="submit">Sign in</button>
        </form>
        <p class="muted" style="margin-top:16px;">
            <a href="{{ route('password.request') }}">Forgot your password?</a>
        </p>
        <p class="muted" style="margin-top:4px;">
            New here? <a href="{{ route('register') }}">Register your company</a>
        </p>
    </div>
</div>
@endsection
