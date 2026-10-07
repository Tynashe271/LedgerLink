<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Concerns\HasUuids;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Foundation\Auth\User as Authenticatable;
use Illuminate\Notifications\Notifiable;

/**
 * Maps to the `users` table owned by BranchLedger's own migration
 * (database/migrations/0001_init_schema.up.sql), not a table Laravel
 * created. Per the architecture, Laravel's role here is "Session metadata
 * only" — it reads this authoritative record to authenticate, but never
 * writes financial or business columns on it.
 */
class User extends Authenticatable
{
    use HasFactory, HasUuids, Notifiable;

    protected $table = 'users';

    // The id column is `uuid default gen_random_uuid()`, not an
    // auto-incrementing integer. HasUuids assigns a client-side UUID before
    // insert so Eloquent knows the key immediately — Eloquent's default
    // insert path only reads back a DB-generated key for incrementing
    // integer columns, so without this $model->id stays null after create().
    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = [
        'email',
        'full_name',
        'password_hash',
    ];

    protected $hidden = [
        'password_hash',
        'remember_token',
    ];

    protected function casts(): array
    {
        return [
            'locked_until' => 'datetime',
            'created_at' => 'datetime',
            'updated_at' => 'datetime',
        ];
    }

    /**
     * The auth guard checks this, not a "password" attribute — our column
     * is password_hash (shared with the Go schema's naming), so the
     * Authenticatable base method is overridden to point at it.
     */
    public function getAuthPassword(): string
    {
        return $this->password_hash;
    }

    public function getAuthPasswordName(): string
    {
        return 'password_hash';
    }

    public function memberships()
    {
        return $this->hasMany(Membership::class);
    }

    public function activeMemberships()
    {
        return $this->memberships()->where('is_active', true);
    }
}
