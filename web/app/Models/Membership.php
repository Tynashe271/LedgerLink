<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Concerns\HasUuids;
use Illuminate\Database\Eloquent\Model;

/**
 * A user's role and branch scope within one company (section 4.4/4.6 of
 * BranchLedger_System_Documentation). Laravel reads this to decide what to
 * put in the delegation token it signs for Go; it never writes to it
 * outside the admin "manage users" flow (not yet built).
 */
class Membership extends Model
{
    use HasUuids;

    protected $table = 'memberships';

    public $incrementing = false;

    protected $keyType = 'string';

    public $timestamps = false;

    protected $fillable = [
        'user_id',
        'company_id',
        'role',
        'branch_scope',
        'delegations',
        'approval_limit',
        'is_active',
    ];

    protected $casts = [
        'is_active' => 'boolean',
        'approval_limit' => 'decimal:2',
    ];

    public function user()
    {
        return $this->belongsTo(User::class);
    }

    public function company()
    {
        return $this->belongsTo(Company::class);
    }

    /**
     * branch_scope is a Postgres uuid[] column; the pgsql driver returns its
     * array literal as a raw string (e.g. "{}" or "{<uuid>,<uuid>}"), so it
     * needs explicit parsing rather than Eloquent's standard casts.
     *
     * @return array<string>
     */
    public function branchScopeArray(): array
    {
        return $this->parsePgTextArray($this->attributes['branch_scope'] ?? '{}');
    }

    /**
     * Same raw-string-from-pgsql-driver situation as branch_scope, for the
     * explicit per-action grants in tenancy.Can's "needsDelegation" rows
     * (e.g. "reverse_posting", "approve_spending" — the Action string
     * constants in backend/internal/tenancy/permissions.go).
     *
     * @return array<string>
     */
    public function delegationsArray(): array
    {
        return $this->parsePgTextArray($this->attributes['delegations'] ?? '{}');
    }

    /** @return array<string> */
    private function parsePgTextArray(string $raw): array
    {
        $trimmed = trim($raw, '{}');
        if ($trimmed === '') {
            return [];
        }

        return array_map('trim', explode(',', $trimmed));
    }
}
