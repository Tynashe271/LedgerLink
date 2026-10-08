@extends('layouts.app')
@section('title', $company->name . ' · BranchLedger')
@section('body')
<style>
    /* Categorical slots for the expense-composition donut (dataviz skill's
       validated default palette — fixed order, never cycled, so a 6th
       category never invents a new hue). "Other" folds any categories past
       slot 5 into one neutral bucket rather than seating a 7th+ hue. */
    .viz-donut {
        --series-1: #2a78d6; --series-2: #eb6834; --series-3: #1baf7a;
        --series-4: #eda100; --series-5: #e87ba4; --series-other: #9a988f;
        --donut-gap: #ffffff; /* matches .panel's background so wedges separate */
    }
    .donut-row { display: flex; gap: 20px; align-items: center; flex-wrap: wrap; }
    .donut-row svg { flex-shrink: 0; }
    .donut-row svg path { cursor: pointer; }
    .donut-row svg path:hover, .donut-row svg path:focus { opacity: 0.85; outline: none; }
    .donut-legend { flex: 1; min-width: 220px; border-collapse: collapse; }
    .donut-legend td { padding: 5px 0; font-size: 13px; border: none; }
    .donut-legend .swatch { width: 10px; height: 10px; border-radius: 2px; display: inline-block; margin-right: 8px; flex-shrink: 0; }
    .donut-legend .cat-cell { display: flex; align-items: center; color: var(--text); }
    .donut-legend .num-cell { text-align: right; color: var(--muted); white-space: nowrap; }
    .donut-tooltip {
        position: fixed; background: #16182b; color: #fff; font-size: 12.5px;
        padding: 6px 10px; border-radius: 6px; pointer-events: none; z-index: 20;
        display: none; white-space: nowrap;
    }
    .donut-tooltip strong { font-weight: 700; }
</style>
<div id="donut-tooltip" class="donut-tooltip" role="status"></div>
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
        <div id="expense-composition"><p class="muted">No expenses posted in this period.</p></div>
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

// Part-to-whole composition, <= 6 segments at a glance (dataviz skill
// anti-patterns: "A donut/pie for comparing close values" is wrong, but
// "part-to-whole at a glance, <= 6 segments" is the valid use — which is
// exactly System Documentation 5.8/5.10's "a small doughnut shows positive
// expense categories").
const DONUT_SLOTS = ['--series-1', '--series-2', '--series-3', '--series-4', '--series-5'];

function polarToCartesian(cx, cy, r, angleDeg) {
    const rad = (angleDeg - 90) * Math.PI / 180;
    return { x: cx + r * Math.cos(rad), y: cy + r * Math.sin(rad) };
}

function donutArcPath(cx, cy, rOuter, rInner, startAngle, endAngle) {
    const startOuter = polarToCartesian(cx, cy, rOuter, endAngle);
    const endOuter = polarToCartesian(cx, cy, rOuter, startAngle);
    const startInner = polarToCartesian(cx, cy, rInner, startAngle);
    const endInner = polarToCartesian(cx, cy, rInner, endAngle);
    const largeArc = endAngle - startAngle > 180 ? 1 : 0;
    return `M${startOuter.x},${startOuter.y} A${rOuter},${rOuter} 0 ${largeArc} 0 ${endOuter.x},${endOuter.y} `
         + `L${startInner.x},${startInner.y} A${rInner},${rInner} 0 ${largeArc} 1 ${endInner.x},${endInner.y} Z`;
}

function renderExpenseComposition(items) {
    const el = document.getElementById('expense-composition');

    if (!items.length) {
        el.innerHTML = '<p class="muted">No expenses posted in this period.</p>';
        return;
    }

    // Negative amounts (credit-note-style adjustments) aren't a positive
    // composition anymore — spec 5.10: "Negative results use bar or line
    // charts." Fall back to the bar form rather than draw a misleading slice.
    if (items.some(i => Number(i.amount) < 0)) {
        el.innerHTML = '<div id="expense-bars"></div>';
        renderBars('expense-bars', items, i => i.account_name, i => Number(i.amount));
        return;
    }

    const sorted = [...items].sort((a, b) => Number(b.amount) - Number(a.amount));
    const top = sorted.slice(0, 5);
    const rest = sorted.slice(5);
    const segments = top.map((i, idx) => ({
        label: i.account_name,
        amount: Number(i.amount),
        colorVar: `var(${DONUT_SLOTS[idx]})`,
    }));
    if (rest.length) {
        segments.push({
            label: `Other (${rest.length})`,
            amount: rest.reduce((sum, i) => sum + Number(i.amount), 0),
            colorVar: 'var(--series-other)',
        });
    }

    const total = segments.reduce((sum, s) => sum + s.amount, 0);
    if (total <= 0) {
        el.innerHTML = '<p class="muted">No expenses posted in this period.</p>';
        return;
    }

    const cx = 70, cy = 70, rOuter = 68, rInner = 40;
    const gapDeg = segments.length > 1 ? 1.5 : 0;
    let angle = 0;
    const paths = segments.map(s => {
        const sweep = (s.amount / total) * 360;
        const start = angle + gapDeg / 2;
        // A segment that fills (or nearly fills) the whole ring — the only
        // category this period, say — has start≈0 and end≈360. SVG's arc
        // command treats those as the SAME point (sin/cos are periodic), so
        // the path's two ends coincide and it paints nothing. Clamping just
        // short of a full turn keeps start and end a hair apart so the arc
        // actually renders, with no visible gap at normal ring widths.
        const end = Math.min(angle + sweep - gapDeg / 2, start + 359.99);
        angle += sweep;
        const pct = (s.amount / total) * 100;
        const label = `${s.label}: ${money(s.amount)} (${pct.toFixed(1)}%)`;
        return `<path d="${donutArcPath(cx, cy, rOuter, rInner, Math.max(start, 0), Math.max(end, start))}"
            fill="${s.colorVar}" stroke="var(--donut-gap)" stroke-width="2"
            tabindex="0" role="img" aria-label="${escapeHtml(label)}" data-tooltip="${escapeHtml(label)}"></path>`;
    }).join('');

    const legend = segments.map(s => {
        const pct = (s.amount / total) * 100;
        return `<tr>
            <td class="cat-cell"><span class="swatch" style="background:${s.colorVar}"></span>${escapeHtml(s.label)}</td>
            <td class="num-cell">${money(s.amount)} &middot; ${pct.toFixed(1)}%</td>
        </tr>`;
    }).join('');

    el.innerHTML = `
        <div class="donut-row viz-donut">
            <svg viewBox="0 0 140 140" width="140" height="140" role="img" aria-label="Expense composition by category">${paths}</svg>
            <table class="donut-legend"><tbody>${legend}</tbody></table>
        </div>`;

    const tooltip = document.getElementById('donut-tooltip');
    el.querySelectorAll('path[data-tooltip]').forEach(path => {
        const show = (evt) => {
            tooltip.innerHTML = `<strong>${escapeHtml(path.getAttribute('data-tooltip'))}</strong>`;
            tooltip.style.display = 'block';
            const x = evt.clientX !== undefined ? evt.clientX : path.getBoundingClientRect().left;
            const y = evt.clientY !== undefined ? evt.clientY : path.getBoundingClientRect().top;
            tooltip.style.left = (x + 14) + 'px';
            tooltip.style.top = (y + 14) + 'px';
        };
        const hide = () => { tooltip.style.display = 'none'; };
        path.addEventListener('pointermove', show);
        path.addEventListener('pointerenter', show);
        path.addEventListener('pointerleave', hide);
        path.addEventListener('focus', show);
        path.addEventListener('blur', hide);
    });
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

    renderExpenseComposition(body.expense_breakdown || []);

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
