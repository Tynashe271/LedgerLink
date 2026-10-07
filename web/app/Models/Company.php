<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Concerns\HasUuids;
use Illuminate\Database\Eloquent\Model;

class Company extends Model
{
    use HasUuids;

    protected $table = 'companies';

    public $incrementing = false;

    protected $keyType = 'string';

    protected $fillable = [
        'name',
        'primary_category',
        'secondary_categories',
        'reporting_currency',
        'timezone',
        'financial_year_start',
    ];

    protected $casts = [
        'secondary_categories' => 'array',
        'financial_year_start' => 'date',
    ];

    public function branches()
    {
        return $this->hasMany(Branch::class);
    }

    public function memberships()
    {
        return $this->hasMany(Membership::class);
    }
}
