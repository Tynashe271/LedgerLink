@extends('layouts.app')
@section('title', 'Alerts · BranchLedger')
@section('body')
<h1>Alerts</h1>
<p class="page-subtitle">
    Everything that needs attention across the company &mdash; rejected operations, unapproved cash
    differences, overdue debt, low stock and delayed branch sync (<code>GET /api/v1/dashboard</code>).
</p>

<div class="panel">
    <div class="panel-head">
        <div style="display:flex; gap:10px; align-items:center; flex-wrap:wrap;">
            <select id="branch-filter" style="width:auto; min-width:160px;">
                <option value="">All branches</option>
                @foreach ($branches as $branch)
                    <option value="{{ $branch->id }}">{{ $branch->name }}{{ $branch->is_main_branch ? ' (main)' : '' }}</option>
                @endforeach
            </select>
            <select id="period-filter" style="width:auto;">
                <option value="today">Today</option>
                <option value="week">This week</option>
                <option value="month" selected>This month</option>
            </select>
        </div>
        <button id="alerts-refresh-btn" type="button" class="secondary">Refresh</button>
    </div>
    <div id="alerts-meta" class="muted"></div>
</div>

<div id="alerts-panel"></div>

<script>
const csrfToken = document.querySelector('meta[name="csrf-token"]').content;
function money(v) { const n = Number(v); return (n < 0 ? '-$' + Math.abs(n).toFixed(2) : '$' + n.toFixed(2)); }
function escapeHtml(s) { return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

async function apiFetch(path, options = {}) {
    const headers = Object.assign({ 'Content-Type': 'application/json', 'X-CSRF-TOKEN': csrfToken, 'Accept': 'application/json' }, options.headers || {});
    const res = await fetch('/api/' + path, Object.assign({}, options, { headers, credentials: 'same-origin' }));
    const body = await res.json().catch(() => ({}));
    return { status: res.status, body };
}

function periodRange(period) {
    const today = new Date();
    const to = today.toISOString().slice(0, 10);
    let from;
    if (period === 'today') {
        from = to;
    } else if (period === 'week') {
        const d = new Date(today); d.setDate(d.getDate() - 7);
        from = d.toISOString().slice(0, 10);
    } else {
        from = to.slice(0, 8) + '01';
    }
    return { from, to };
}

async function loadAlerts() {
    const period = document.getElementById('period-filter').value;
    const branchId = document.getElementById('branch-filter').value;
    const { from, to } = periodRange(period);

    let path = `v1/dashboard?from=${from}&to=${to}`;
    if (branchId) path += `&branch_id=${branchId}`;

    const { status, body } = await apiFetch(path);
    const panelEl = document.getElementById('alerts-panel');
    if (status !== 200) {
        document.getElementById('alerts-meta').textContent = `Could not load alerts (HTTP ${status}).`;
        panelEl.innerHTML = '';
        return;
    }

    document.getElementById('alerts-meta').textContent =
        `${body.from} to ${body.to} · generated ${new Date(body.generated_at).toLocaleTimeString()}`;

    // Critical (needs action now) vs informational (worth reviewing) —
    // same split the dashboard used to make inline.
    const critical = [];
    const warnings = [];

    if (body.negative_cash_balance) critical.push('Cash balance is negative. This should not happen in a cash business &mdash; investigate immediately.');
    if (body.negative_bank_balance) critical.push('Bank balance is negative &mdash; investigate immediately.');
    (body.rejected_operation_alerts || []).forEach(r => {
        critical.push(`${escapeHtml(r.branch_name)}: a ${escapeHtml(r.command_type)} was rejected (${escapeHtml(r.error_code)}) at ${new Date(r.occurred_at).toLocaleString()}.`);
    });
    (body.cash_difference_alerts || []).forEach(a => {
        critical.push(`${escapeHtml(a.branch_name)} had a cash difference of ${money(a.discrepancy)} on ${a.close_date} (${a.currency}), not yet approved.`);
    });
    (body.overdue_debt_alerts || []).forEach(o => {
        critical.push(`${escapeHtml(o.customer_name)} owes ${money(o.outstanding_amount)}, overdue since ${o.oldest_due_date}. <a href="/customers">Record receipt</a>.`);
    });

    const staleBranches = (body.branches || []).filter(b => b.stale);
    if (staleBranches.length) warnings.push(`${staleBranches.length} branch(es) have delayed sync updates. <a href="/sync">Open sync centre</a>.`);
    if (body.pending_approvals_count > 0) warnings.push(`${body.pending_approvals_count} request(s) are awaiting approval. <a href="/approvals">Review</a>.`);
    (body.large_transaction_alerts || []).forEach(l => {
        warnings.push(`${escapeHtml(l.branch_name)}: an unusually large ${escapeHtml(l.document_type)} of ${money(l.amount)} was posted on ${l.date}.`);
    });
    (body.low_stock_alerts || []).forEach(s => {
        warnings.push(`${escapeHtml(s.branch_name)}: ${escapeHtml(s.product_name)} is low (${s.quantity} ${escapeHtml(s.unit)} left, reorder point ${s.reorder_point}). <a href="/purchases">Reorder</a>.`);
    });

    if (!critical.length && !warnings.length) {
        panelEl.innerHTML = '<div class="panel"><p class="muted">Nothing needs attention for this filter.</p></div>';
    } else {
        panelEl.innerHTML = `<div class="panel"><h2>Needs attention</h2>` +
            critical.map(a => `<div class="errors">${a}</div>`).join('') +
            warnings.map(a => `<div class="alert-warn">${a}</div>`).join('') +
            `</div>`;
    }
}

document.getElementById('alerts-refresh-btn').addEventListener('click', loadAlerts);
document.getElementById('branch-filter').addEventListener('change', loadAlerts);
document.getElementById('period-filter').addEventListener('change', loadAlerts);
loadAlerts();
</script>
@endsection
