@extends('layouts.app')
@section('title', 'Reports · BranchLedger')
@section('body')
<h1>Reports</h1>
<p class="page-subtitle">
    Filter, export and drill down (<code>GET /api/v1/reports/trial-balance</code>,
    <code>GET /api/v1/reports/ageing</code>). Figures are from posted journals only, as of the date shown.
</p>

<div class="panel">
    <div class="panel-head">
        <div style="display:flex; gap:10px; align-items:center; flex-wrap:wrap;">
            <select id="report-type" style="width:auto; min-width:160px;">
                <option value="trial-balance">Trial balance</option>
                <option value="ageing">Ageing (debtors)</option>
            </select>
            <select id="branch-filter" style="width:auto; min-width:160px;">
                <option value="">All branches</option>
                @foreach ($branches as $branch)
                    <option value="{{ $branch->id }}">{{ $branch->name }}{{ $branch->is_main_branch ? ' (main)' : '' }}</option>
                @endforeach
            </select>
            <label for="as-of-filter" class="muted" style="margin:0;">As of</label>
            <input id="as-of-filter" type="date" style="width:auto;">
        </div>
        <span style="display:flex; gap:10px;">
            <button id="export-btn" type="button" class="secondary">Export CSV</button>
            <button id="refresh-btn" type="button" class="secondary">Refresh</button>
        </span>
    </div>
    <div id="report-meta" class="muted"></div>
</div>

<div class="panel">
    <h2 id="report-title">Trial balance</h2>
    <table id="report-table">
        <thead></thead>
        <tbody></tbody>
        <tfoot></tfoot>
    </table>
</div>

<script type="module">
const csrfToken = document.querySelector('meta[name="csrf-token"]').content;
function money(v) { const n = Number(v); return (n < 0 ? '-$' + Math.abs(n).toFixed(2) : '$' + n.toFixed(2)); }
function escapeHtml(s) { return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

async function apiFetch(path, options = {}) {
    const headers = Object.assign({ 'Content-Type': 'application/json', 'X-CSRF-TOKEN': csrfToken, 'Accept': 'application/json' }, options.headers || {});
    const res = await fetch('/api/' + path, Object.assign({}, options, { headers, credentials: 'same-origin' }));
    const body = await res.json().catch(() => ({}));
    return { status: res.status, body };
}

document.getElementById('as-of-filter').value = new Date().toISOString().slice(0, 10);
let lastReport = null; // { type, rows: [...plain arrays for CSV...], headers: [...] }

function downloadCsv(filename, headers, rows) {
    const escape = (v) => {
        const s = String(v ?? '');
        return /[",\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;
    };
    const csv = [headers.map(escape).join(','), ...rows.map(r => r.map(escape).join(','))].join('\r\n');
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = filename;
    document.body.appendChild(a); a.click(); a.remove();
    URL.revokeObjectURL(url);
}

async function loadReport() {
    const type = document.getElementById('report-type').value;
    const branchId = document.getElementById('branch-filter').value;
    const asOf = document.getElementById('as-of-filter').value;
    const thead = document.querySelector('#report-table thead');
    const tbody = document.querySelector('#report-table tbody');
    const tfoot = document.querySelector('#report-table tfoot');

    const path = (type === 'trial-balance' ? 'v1/reports/trial-balance' : 'v1/reports/ageing') +
        `?as_of=${asOf}` + (branchId ? `&branch_id=${branchId}` : '');
    const { status, body } = await apiFetch(path);
    if (status !== 200) {
        document.getElementById('report-meta').textContent = `Could not load report (HTTP ${status}).`;
        return;
    }

    document.getElementById('report-meta').textContent =
        `${body.reporting_currency} · posting basis: ${body.posting_basis} · as of ${body.as_of} · generated ${new Date(body.generated_at).toLocaleString()}`;

    if (type === 'trial-balance') {
        document.getElementById('report-title').textContent = 'Trial balance';
        thead.innerHTML = '<tr><th>Code</th><th>Account</th><th>Type</th><th>Debit</th><th>Credit</th></tr>';
        tbody.innerHTML = body.lines.length ? body.lines.map(l => `<tr>
            <td>${escapeHtml(l.account_code)}</td><td>${escapeHtml(l.account_name)}</td><td>${escapeHtml(l.account_type)}</td>
            <td>${Number(l.debit) !== 0 ? money(l.debit) : ''}</td><td>${Number(l.credit) !== 0 ? money(l.credit) : ''}</td>
        </tr>`).join('') : '<tr class="empty-row"><td colspan="5">No accounts.</td></tr>';
        const balanced = body.total_debits === body.total_credits;
        tfoot.innerHTML = `<tr><th colspan="3">Total${balanced ? '' : ' — DOES NOT RECONCILE'}</th><th>${money(body.total_debits)}</th><th>${money(body.total_credits)}</th></tr>`;

        lastReport = {
            filename: `trial-balance-${body.as_of}.csv`,
            headers: ['Code', 'Account', 'Type', 'Debit', 'Credit'],
            rows: body.lines.map(l => [l.account_code, l.account_name, l.account_type, l.debit, l.credit]),
        };
    } else {
        document.getElementById('report-title').textContent = 'Ageing (debtors)';
        thead.innerHTML = '<tr><th>Customer</th><th>Oldest due</th><th>Current</th><th>1-30</th><th>31-60</th><th>61-90</th><th>90+</th><th>Total</th></tr>';
        tbody.innerHTML = body.rows.length ? body.rows.map(r => `<tr>
            <td>${escapeHtml(r.customer_name)}</td><td>${r.oldest_due_date}</td>
            <td>${money(r.current)}</td><td>${money(r.days_1_to_30)}</td><td>${money(r.days_31_to_60)}</td>
            <td>${money(r.days_61_to_90)}</td><td>${money(r.days_over_90)}</td><td>${money(r.total)}</td>
        </tr>`).join('') : '<tr class="empty-row"><td colspan="8">No outstanding customer balances.</td></tr>';
        tfoot.innerHTML = `<tr><th colspan="7">Total outstanding</th><th>${money(body.total_outstanding)}</th></tr>`;

        lastReport = {
            filename: `ageing-${body.as_of}.csv`,
            headers: ['Customer', 'Oldest due', 'Current', '1-30', '31-60', '61-90', '90+', 'Total'],
            rows: body.rows.map(r => [r.customer_name, r.oldest_due_date, r.current, r.days_1_to_30, r.days_31_to_60, r.days_61_to_90, r.days_over_90, r.total]),
        };
    }
}

document.getElementById('export-btn').addEventListener('click', () => {
    if (!lastReport) return;
    downloadCsv(lastReport.filename, lastReport.headers, lastReport.rows);
});

document.getElementById('report-type').addEventListener('change', loadReport);
document.getElementById('branch-filter').addEventListener('change', loadReport);
document.getElementById('as-of-filter').addEventListener('change', loadReport);
document.getElementById('refresh-btn').addEventListener('click', loadReport);
loadReport();
</script>
@endsection
