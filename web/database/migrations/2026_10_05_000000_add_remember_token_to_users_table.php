<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

// Additive-only: "remember me" is a Laravel login convenience, not part of
// Go's authoritative user record, but it has to live on the same row for
// Eloquent's auth guard to manage it. Matches the architecture's
// expand-and-contract migration guidance (never a destructive change to a
// table another service owns).
return new class extends Migration
{
    public function up(): void
    {
        Schema::table('users', function (Blueprint $table) {
            $table->rememberToken()->nullable();
        });
    }

    public function down(): void
    {
        Schema::table('users', function (Blueprint $table) {
            $table->dropColumn('remember_token');
        });
    }
};
