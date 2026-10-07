@extends('layouts.app')
@section('title', 'Branch overview · BranchLedger')
@section('body')
<h1>Branch overview</h1>
<p class="page-subtitle">
    Today's posted totals for one branch, quick entry into the daily screens, and sync status
    (<code>GET /api/v1/dashboard</code>). For comparing branches or other periods, use Manager overview.
</p>

<div class="panel">
    <div class="panel-head">
        <div style="display:flex; gap:10px; align-items:center; flex-wrap:wrap;">
            @if ($assignedBranchId)
                <strong id="branch-name-label">{{ optional($branches->firstWhere('id', $assignedBranchId))->name }}</strong>
                <input type="hidden" id="branch-filter" value="{{ $assignedBranchId }}">
            @else
                <select id="branch-filter" style="width:auto; min-width:160px;">
                    @foreach ($branches as $branch)
                        <option value="{{ $branch->id }}" {{ $branch->is_main_branch ? 'selected' : '' }}>{{ $branch->name }}{{ $branch->is_main_branch ? ' (main)' : '' }}</option>
                    @endforeach
                </select>
            @endif
            <span class="muted" id="operating-date"></span>
        </div>
        <span style="display:flex; gap:10px;">
            <button id="sync-now-btn" type="button" class="secondary">Sync now</button>
            <button id="refresh-btn" type="button" class="secondary">Refresh</button>
        </span>
    </div>
    <div id="overview-meta" class="muted"></div>
    <div id="sync-result"></div>
</div>

<div id="alerts-panel"></div>

<div class="panel">
    <h2>Today</h2>
    <div id="pl-stats" class="grid grid-stats"></div>
</div>

<div class="panel">
    <h2>Balances, as of today</h2>
    <div id="balance-stats" class="grid grid-stats"></div>
</div>

<div class="panel">
    <h2>Quick entry</h2>
    <div class="grid grid-stats">
        <a href="{{ route('workspace.sales') }}" class="stat-tile" style="text-decoration:none; display:block;"><div class="label">Sales</div></a>
        <a href="{{ route('workspace.expenses') }}" class="stat-tile" style="text-decoration:none; display:block;"><div class="label">Expenses</div></a>
        <a href="{{ route('workspace.purchases') }}" class="stat-tile" style="text-decoration:none; display:block;"><div class="label">Purchases</div></a>
        <a href="{{ route('workspace.transfers') }}" class="stat-tile" style="text-decoration:none; display:block;"><div class="label">Transfers</div></a>
        <a href="{{ route('workspace.stock') }}" class="stat-tile" style="text-decoration:none; display:block;"><div class="label">Stock</div></a>
        <a href="{{ route('workspace.closes') }}" class="stat-tile" style="text-decoration:none; display:block;"><div class="label">Daily close</div></a>
    </div>
</div>

<script type="module">
import { syncNow } from '/js/offline/sync.js';

const csrfToken = document.querySelector('meta[name="csrf-token"]').content;
function money(v) { const n = Number(v); return (n < 0 ? '-$' + Math.abs(n).toFixed(2) : '$' + n.toFixed(2)); }
function escapeHtml(s) { return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }
function statTile(label, value, cls) {
    return `<div class="stat-tile"><div class="label">${label}</div><div class="value ${cls || ''}">${value}</div></div>`;
}

async function apiFetch(path, options = {}) {
    const headers = Object.assign({ 'Content-Type': 'application/json', 'X-CSRF-TOKEN': csrfToken, 'Accept': 'application/json' }, options.headers || {});
    const res = await fetch('/api/' + path, Object.assign({}, options, { headers, credentials: 'same-origin' }));
    const body = await res.json().catch(() => ({}));
    return { status: res.status, body };
}

document.getElementById('operating-date').textContent = new Date().toLocaleDateString(undefined, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' });

async function loadOverview() {
    const branchId = document.getElementById('branch-filter').value;
    const today = new Date().toISOString().slice(0, 10);

    const { status, body } = await apiFetch(`v1/dashboard?branch_id=${branchId}&from=${today}&to=${today}`);
    if (status !== 200) {
        document.getElementById('overview-meta').textContent = `Could not load branch overview (HTTP ${status}).`;
        return;
    }

    const branch = (body.branches || [])[0];
    document.getElementById('overview-meta').textContent =
        `${body.reporting_currency} · generated ${new Date(body.generated_at).toLocaleTimeString()} · ${body.posting_basis}` +
        (branch ? ` · last synced ${branch.last_sync_at ? new Date(branch.last_sync_at).toLocaleString() : 'never'}` : '');

    const critical = [];
    const warnings = [];
    if (body.negative_cash_balance) critical.push('Cash balance is negative. This should not happen in a cash business &mdash; investigate immediately.');
    if (body.negative_bank_balance) critical.push('Bank balance is negative &mdash; investigate immediately.');
    if (branch && branch.stale) warnings.push('This branch has not synced recently &mdash; today\'s totals above may be incomplete.');
    (body.rejected_operation_alerts || []).forEach(r => {
        critical.push(`A ${escapeHtml(r.command_type)} was rejected (${escapeHtml(r.error_code)}) at ${new Date(r.occurred_at).toLocaleString()}.`);
    });
    (body.cash_difference_alerts || []).forEach(a => {
        critical.push(`Cash difference of ${money(a.discrepancy)} on ${a.close_date} (${a.currency}), not yet approved.`);
    });
    if (body.pending_approvals_count > 0) warnings.push(`${body.pending_approvals_count} request(s) are awaiting approval. <a href="/approvals">Review</a>.`);
    (body.large_transaction_alerts || []).forEach(l => {
        warnings.push(`An unusually large ${escapeHtml(l.document_type)} of ${money(l.amount)} was posted on ${l.date}.`);
    });
    (body.low_stock_alerts || []).forEach(s => {
        warnings.push(`${escapeHtml(s.product_name)} is low (${s.quantity} ${escapeHtml(s.unit)} left, reorder point ${s.reorder_point}). <a href="/purchases">Reorder</a>.`);
    });
    (body.overdue_debt_alerts || []).forEach(o => {
        critical.push(`${escapeHtml(o.customer_name)} owes ${money(o.outstanding_amount)}, overdue since ${o.oldest_due_date}. <a href="/customers">Record receipt</a>.`);
    });

    const panelEl = document.getElementById('alerts-panel');
    panelEl.innerHTML = (!critical.length && !warnings.length) ? '' :
        `<div class="panel"><h2>Needs attention</h2>` +
        critical.map(a => `<div class="errors">${a}</div>`).join('') +
        warnings.map(a => `<div class="alert-warn">${a}</div>`).join('') +
        `</div>`;

    document.getElementById('pl-stats').innerHTML = [
        statTile('Revenue', money(body.revenue)),
        statTile('Cost of sales', money(body.cost_of_sales)),
        statTile('Gross profit', money(body.gross_profit)),
        statTile('Operating expenses', money(body.operating_expenses)),
        statTile('Net profit', money(body.net_profit), Number(body.net_profit) < 0 ? 'neg' : 'pos'),
    ].join('');

    document.getElementById('balance-stats').innerHTML = [
        statTile('Cash', money(body.cash_balance)),
        statTile('Bank', money(body.bank_balance)),
        statTile('Receivables', money(body.receivables)),
        statTile('Payables', money(body.payables)),
    ].join('');
}

document.getElementById('refresh-btn').addEventListener('click', loadOverview);
if (document.getElementById('branch-filter').tagName === 'SELECT') {
    document.getElementById('branch-filter').addEventListener('change', loadOverview);
}
document.getElementById('sync-now-btn').addEventListener('click', async () => {
    const resultEl = document.getElementById('sync-result');
    resultEl.innerHTML = '<div class="status">Syncing&hellip;</div>';
    const result = await syncNow();
    resultEl.innerHTML = result.offline ? '<div class="errors">Offline &mdash; cannot sync right now.</div>' : '<div class="status">Sync complete.</div>';
    loadOverview();
});

loadOverview();
setInterval(loadOverview, 15000);
</script>
@endsection
