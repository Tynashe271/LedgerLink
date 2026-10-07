@extends('layouts.app')
@section('title', 'Audit log · BranchLedger')
@section('body')
<h1>Audit log</h1>
<p class="page-subtitle">
    Security and posting events — logins, access denials, role changes, exports, postings and reversals
    (<code>GET /api/v1/audit-events</code>). Owner and accountant only; this is never branch-scoped.
</p>

<div class="panel">
    <div class="panel-head">
        <div style="display:flex; gap:10px; align-items:center; flex-wrap:wrap;">
            <input id="from-filter" type="date" style="width:auto;">
            <input id="to-filter" type="date" style="width:auto;">
            <select id="limit-filter" style="width:auto;">
                <option value="50">Last 50</option>
                <option value="100" selected>Last 100</option>
                <option value="200">Last 200</option>
            </select>
        </div>
        <button id="refresh-btn" type="button" class="secondary">Refresh</button>
    </div>
    <div id="result"></div>
    <table id="audit-table">
        <thead><tr><th>When</th><th>Actor</th><th>Event</th><th>Record</th><th>Details</th></tr></thead>
        <tbody></tbody>
    </table>
</div>

<script type="module">
const csrfToken = document.querySelector('meta[name="csrf-token"]').content;
function escapeHtml(s) { return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

async function apiFetch(path, options = {}) {
    const headers = Object.assign({ 'Content-Type': 'application/json', 'X-CSRF-TOKEN': csrfToken, 'Accept': 'application/json' }, options.headers || {});
    const res = await fetch('/api/' + path, Object.assign({}, options, { headers, credentials: 'same-origin' }));
    const body = await res.json().catch(() => ({}));
    return { status: res.status, body };
}

document.getElementById('from-filter').value = new Date(Date.now() - 30 * 86400000).toISOString().slice(0, 10);
document.getElementById('to-filter').value = new Date().toISOString().slice(0, 10);

async function loadAuditLog() {
    const from = document.getElementById('from-filter').value;
    const to = document.getElementById('to-filter').value;
    const limit = document.getElementById('limit-filter').value;
    const resultEl = document.getElementById('result');
    const tbody = document.querySelector('#audit-table tbody');

    const { status, body } = await apiFetch(`v1/audit-events?from=${from}&to=${to}&limit=${limit}`);
    if (status === 403) {
        resultEl.innerHTML = '<div class="errors">Only the owner or accountant can view the audit log.</div>';
        tbody.innerHTML = '<tr class="empty-row"><td colspan="5">Not authorised.</td></tr>';
        return;
    }
    if (status !== 200) {
        resultEl.innerHTML = `<div class="errors">Could not load the audit log (HTTP ${status}).</div>`;
        return;
    }
    resultEl.innerHTML = '';
    if (!body.length) {
        tbody.innerHTML = '<tr class="empty-row"><td colspan="5">No events in range.</td></tr>';
        return;
    }
    tbody.innerHTML = body.map(e => `<tr>
        <td>${new Date(e.server_time).toLocaleString()}</td>
        <td>${escapeHtml(e.actor_name || 'system')}</td>
        <td>${escapeHtml(e.event_type.replace(/_/g, ' '))}</td>
        <td class="muted">${escapeHtml(e.record_type || '')}${e.record_id ? ' ' + escapeHtml(e.record_id.slice(0, 8)) : ''}</td>
        <td class="muted" style="font-family:monospace; font-size:12px;">${escapeHtml(JSON.stringify(e.details || {}))}</td>
    </tr>`).join('');
}

document.getElementById('from-filter').addEventListener('change', loadAuditLog);
document.getElementById('to-filter').addEventListener('change', loadAuditLog);
document.getElementById('limit-filter').addEventListener('change', loadAuditLog);
document.getElementById('refresh-btn').addEventListener('click', loadAuditLog);
loadAuditLog();
</script>
@endsection
