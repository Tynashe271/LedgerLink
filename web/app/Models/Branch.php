<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Concerns\HasUuids;
use Illuminate\Database\Eloquent\Model;

class Branch extends Model
{
    use HasUuids;

    protected $table = 'branches';

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = [
        'company_id',
        'name',
        'code',
        'category',
        'is_main_branch',
    ];

    protected $casts = [
        'is_main_branch' => 'boolean',
    ];

    public function company()
    {
        return $this->belongsTo(Company::class);
    }
}
