@extends('layouts.app')
@section('title', 'Set a new password · BranchLedger')
@section('body')
<div class="shell" style="max-width: 420px; margin-top: 10vh;">
    <h1 style="text-align:center;">Set a new password</h1>
    <div class="panel">
        @if ($errors->any())
            <div class="errors">
                @foreach ($errors->all() as $error)
                    <div>{{ $error }}</div>
                @endforeach
            </div>
        @endif

        <form method="POST" action="{{ route('password.update') }}">
            @csrf
            <input type="hidden" name="token" value="{{ $token }}">

            <label for="email">Email</label>
            <input id="email" type="email" name="email" value="{{ old('email', $email) }}" required autofocus>

            <label for="password">New password</label>
            <input id="password" type="password" name="password" minlength="12" required>

            <label for="password_confirmation">Confirm new password</label>
            <input id="password_confirmation" type="password" name="password_confirmation" minlength="12" required>

            <button type="submit">Reset password</button>
        </form>
    </div>
</div>
@endsection
