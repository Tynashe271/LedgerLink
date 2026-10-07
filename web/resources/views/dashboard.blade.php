@extends('layouts.app')
@section('title', $company->name . ' · BranchLedger')
@section('body')
<h1>Manager overview</h1>
<p class="page-subtitle">
    Posted-journal totals only &mdash; never provisional/offline-pending records
    (<code>GET /api/v1/dashboard</code>).
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
        <button id="dashboard-refresh-btn" type="button" class="secondary">Refresh</button>
    </div>
    <div id="dashboard-meta" class="muted"></div>
    <div id="dashboard-stale-warning"></div>
</div>

<div id="alerts-panel"></div>

<div class="panel">
    <h2>Profit &amp; loss, this period</h2>
    <div id="pl-stats" class="grid grid-stats"></div>
</div>

<div class="panel">
    <h2>Balances, as of today</h2>
    <div id="balance-stats" class="grid grid-stats"></div>
</div>

<div class="grid grid-2">
    <div class="panel">
        <h2>Branch comparison</h2>
        <div id="branch-bars"><p class="muted">Select "All branches" to compare.</p></div>
    </div>
    <div class="panel">
        <h2>Where spending occurs</h2>
        <div id="expense-bars"><p class="muted">No expenses posted in this period.</p></div>
    </div>
</div>

<div class="panel">
    <h2>Revenue trend</h2>
    <div id="trend-chart"></div>
</div>

<div class="panel">
    <h2>Branch freshness</h2>
    <div id="branch-freshness"></div>
</div>

<script>
const csrfToken = document.querySelector('meta[name="csrf-token"]').content;

async function apiFetch(path, options = {}) {
    const headers = Object.assign({
        'Content-Type': 'application/json',
        'X-CSRF-TOKEN': csrfToken,
        'Accept': 'application/json',
    }, options.headers || {});
    const res = await fetch('/api/' + path, Object.assign({}, options, { headers, credentials: 'same-origin' }));
    const body = await res.json().catch(() => ({}));
    return { status: res.status, body };
}

function money(v) {
    const n = Number(v);
    return (n < 0 ? '-$' + Math.abs(n).toFixed(2) : '$' + n.toFixed(2));
}

function statTile(label, value, cls) {
    return `<div class="stat-tile">
        <div class="label">${label}</div>
        <div class="value ${cls || ''}">${value}</div>
    </div>`;
}

function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
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

function renderBars(containerId, items, labelFn, valueFn) {
    const el = document.getElementById(containerId);
    if (!items.length) return;
    const max = Math.max(...items.map(i => Math.abs(valueFn(i))), 1);
    el.innerHTML = items.map(i => {
        const v = valueFn(i);
        const pct = Math.max(2, Math.abs(v) / max * 100);
        return `<div class="bar-row">
            <div class="bar-label">${escapeHtml(labelFn(i))}</div>
            <div class="bar-track"><div class="bar-fill ${v < 0 ? 'neg' : ''}" style="width:${pct}%"></div></div>
            <div class="bar-value">${money(v)}</div>
        </div>`;
    }).join('');
}

function renderTrend(points) {
    const el = document.getElementById('trend-chart');
    if (!points.length) { el.innerHTML = '<p class="muted">No posted revenue in this period.</p>'; return; }
    const w = 600, h = 120, pad = 10;
    const values = points.map(p => Number(p.revenue));
    const max = Math.max(...values, 1);
    const stepX = points.length > 1 ? (w - pad * 2) / (points.length - 1) : 0;
    const coords = values.map((v, i) => {
        const x = pad + i * stepX;
        const y = h - pad - (v / max) * (h - pad * 2);
        return `${x},${y}`;
    });
    const last = points[points.length - 1];
    el.innerHTML = `
        <svg viewBox="0 0 ${w} ${h}" style="width:100%; height:120px;" role="img" aria-label="Daily revenue trend">
            <polyline fill="none" stroke="#2b5fd9" stroke-width="2" points="${coords.join(' ')}" />
        </svg>
        <table>
            <thead><tr><th>Date</th><th>Revenue</th></tr></thead>
            <tbody>${points.slice(-7).map(p => `<tr><td>${p.date}</td><td>${money(p.revenue)}</td></tr>`).join('')}</tbody>
        </table>
        <p class="muted">Showing last 7 of ${points.length} day(s). Latest: ${last.date} &mdash; ${money(last.revenue)}.</p>`;
}

async function loadDashboard() {
    const period = document.getElementById('period-filter').value;
    const branchId = document.getElementById('branch-filter').value;
    const { from, to } = periodRange(period);

    let path = `v1/dashboard?from=${from}&to=${to}`;
    if (branchId) path += `&branch_id=${branchId}`;

    const { status, body } = await apiFetch(path);
    if (status !== 200) {
        document.getElementById('dashboard-meta').textContent = `Could not load dashboard (HTTP ${status}).`;
        return;
    }

    document.getElementById('dashboard-meta').textContent =
        `${body.from} to ${body.to} · ${body.reporting_currency} · generated ${new Date(body.generated_at).toLocaleTimeString()} · ${body.posting_basis}`;

    const staleBranches = (body.branches || []).filter(b => b.stale);
    document.getElementById('dashboard-stale-warning').innerHTML = staleBranches.length
        ? `<div class="errors" style="margin-top:12px;">${staleBranches.length} branch(es) have not synced recently &mdash; company totals above may be incomplete. A stale branch's zero displayed sales does not mean zero actual sales.</div>`
        : '';

    // Alerts: critical (needs action now) vs informational (worth reviewing).
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

    if (staleBranches.length) warnings.push(`${staleBranches.length} branch(es) have delayed sync updates.`);
    if (body.pending_approvals_count > 0) warnings.push(`${body.pending_approvals_count} request(s) are awaiting approval. <a href="/approvals">Review</a>.`);
    (body.large_transaction_alerts || []).forEach(l => {
        warnings.push(`${escapeHtml(l.branch_name)}: an unusually large ${escapeHtml(l.document_type)} of ${money(l.amount)} was posted on ${l.date}.`);
    });
    (body.low_stock_alerts || []).forEach(s => {
        warnings.push(`${escapeHtml(s.branch_name)}: ${escapeHtml(s.product_name)} is low (${s.quantity} ${escapeHtml(s.unit)} left, reorder point ${s.reorder_point}). <a href="/purchases">Reorder</a>.`);
    });
    (body.overdue_debt_alerts || []).forEach(o => {
        critical.push(`${escapeHtml(o.customer_name)} owes ${money(o.outstanding_amount)}, overdue since ${o.oldest_due_date}. <a href="/customers">Record receipt</a>.`);
    });

    const panelEl = document.getElementById('alerts-panel');
    if (!critical.length && !warnings.length) {
        panelEl.innerHTML = '';
    } else {
        panelEl.innerHTML = `<div class="panel"><h2>Needs attention</h2>` +
            critical.map(a => `<div class="errors">${a}</div>`).join('') +
            warnings.map(a => `<div class="alert-warn">${a}</div>`).join('') +
            `</div>`;
    }

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

    const branchBreakdown = body.branch_breakdown || [];
    const branchBarsEl = document.getElementById('branch-bars');
    if (branchBreakdown.length) {
        renderBars('branch-bars', branchBreakdown, b => b.name, b => Number(b.net_profit));
    } else {
        branchBarsEl.innerHTML = '<p class="muted">Select "All branches" to compare branch performance.</p>';
    }

    const expenseBreakdown = body.expense_breakdown || [];
    const expenseBarsEl = document.getElementById('expense-bars');
    if (expenseBreakdown.length) {
        renderBars('expense-bars', expenseBreakdown, e => e.account_name, e => Number(e.amount));
    } else {
        expenseBarsEl.innerHTML = '<p class="muted">No expenses posted in this period.</p>';
    }

    renderTrend(body.daily_trend || []);

    document.getElementById('branch-freshness').innerHTML =
        '<table><thead><tr><th>Branch</th><th>Last sync</th><th>Status</th></tr></thead><tbody>' +
        (body.branches || []).map(b => `<tr>
            <td>${escapeHtml(b.name)}</td>
            <td>${b.last_sync_at ? new Date(b.last_sync_at).toLocaleString() : 'Never'}</td>
            <td><span class="badge ${b.stale ? 'badge-stale' : 'badge-fresh'}">${b.stale ? 'Stale' : 'Fresh'}</span></td>
        </tr>`).join('') +
        '</tbody></table>';
}

document.getElementById('dashboard-refresh-btn').addEventListener('click', loadDashboard);
document.getElementById('branch-filter').addEventListener('change', loadDashboard);
document.getElementById('period-filter').addEventListener('change', loadDashboard);
loadDashboard();
</script>
@endsection
