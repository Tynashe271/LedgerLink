@extends('layouts.app')
@section('title', 'Forgot password · BranchLedger')
@section('body')
<div class="shell" style="max-width: 420px; margin-top: 10vh;">
    <h1 style="text-align:center;">Reset your password</h1>
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

        <p class="muted">Enter your account email and, if it matches an account, we'll send a reset link.</p>

        <form method="POST" action="{{ route('password.email') }}">
            @csrf
            <label for="email">Email</label>
            <input id="email" type="email" name="email" value="{{ old('email') }}" required autofocus>
            <button type="submit">Send reset link</button>
        </form>
        <p class="muted" style="margin-top:16px;"><a href="{{ route('login') }}">Back to sign in</a></p>
    </div>
</div>
@endsection
