<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

// The `users` table itself is created by BranchLedger's own migration
// (database/migrations/0001_init_schema.up.sql) and is authoritative,
// Go-owned company data — per the architecture, Laravel owns "Session
// metadata only". This migration only adds the Laravel-specific tables
// (password reset tokens, sessions) that Laravel is responsible for.
return new class extends Migration
{
    public function up(): void
    {
        Schema::create('password_reset_tokens', function (Blueprint $table) {
            $table->string('email')->primary();
            $table->string('token');
            $table->timestamp('created_at')->nullable();
        });

        Schema::create('sessions', function (Blueprint $table) {
            $table->string('id')->primary();
            // Go's users.id is a uuid, not an auto-incrementing bigint, so
            // this cannot use foreignId().
            $table->uuid('user_id')->nullable()->index();
            $table->string('ip_address', 45)->nullable();
            $table->text('user_agent')->nullable();
            $table->longText('payload');
            $table->integer('last_activity')->index();
        });
    }

    public function down(): void
    {
        Schema::dropIfExists('password_reset_tokens');
        Schema::dropIfExists('sessions');
    }
};
