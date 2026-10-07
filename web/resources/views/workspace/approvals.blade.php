@extends('layouts.app')
@section('title', 'Approvals · BranchLedger')
@section('body')
<h1>Approvals</h1>
<p class="page-subtitle">
    Inspect the source details and decide within your delegated limit. You cannot approve your own request &mdash;
    this is enforced server-side for every role (<code>GET</code>/<code>POST /api/v1/approvals</code>).
</p>

<div class="panel">
    <div class="panel-head">
        <h2>Pending requests</h2>
        <button id="refresh-btn" type="button" class="secondary">Refresh</button>
    </div>
    <table id="approvals-table">
        <thead><tr><th>Requested</th><th>Branch</th><th>Type</th><th>Amount</th><th>Requested by</th><th>Explanation</th><th>Decision</th></tr></thead>
        <tbody></tbody>
    </table>
</div>

<script>
const csrfToken = document.querySelector('meta[name="csrf-token"]').content;
function uuidv4() { return crypto.randomUUID(); }
function money(v) { const n = Number(v); return (n < 0 ? '-$' + Math.abs(n).toFixed(2) : '$' + n.toFixed(2)); }
function escapeHtml(s) { return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

async function apiFetch(path, options = {}) {
    const headers = Object.assign({ 'Content-Type': 'application/json', 'X-CSRF-TOKEN': csrfToken, 'Accept': 'application/json' }, options.headers || {});
    const res = await fetch('/api/' + path, Object.assign({}, options, { headers, credentials: 'same-origin' }));
    const body = await res.json().catch(() => ({}));
    return { status: res.status, body };
}

async function decide(approvalId, branchId, expectedVersion, decision) {
    const reason = prompt(decision === 'approved' ? 'Approval reason (optional):' : 'Rejection reason:');
    if (decision === 'rejected' && !reason) { alert('A rejection reason is required.'); return; }

    const { status, body } = await apiFetch(`v1/approvals/${approvalId}/decision`, {
        method: 'POST',
        body: JSON.stringify({
            operation_id: uuidv4(), branch_id: branchId, decision, reason: reason || '',
            expected_version: expectedVersion,
        }),
    });
    if (status === 200 && body.accepted) {
        loadApprovals();
    } else if (body.error_code === 'conflict') {
        alert('This request changed since you opened it. Refreshing the list.');
        loadApprovals();
    } else {
        alert(`Rejected (HTTP ${status}): ${body.error_code || 'unknown error'}`);
    }
}
window.decide = decide;

async function loadApprovals() {
    const { status, body } = await apiFetch('v1/approvals');
    const tbody = document.querySelector('#approvals-table tbody');
    if (status !== 200 || !Array.isArray(body) || !body.length) {
        tbody.innerHTML = '<tr class="empty-row"><td colspan="7">No pending approvals.</td></tr>';
        return;
    }
    tbody.innerHTML = body.map(a => `<tr>
        <td>${new Date(a.requested_at).toLocaleString()}</td>
        <td>${escapeHtml(a.branch_name)}</td>
        <td>${escapeHtml(a.document_type)}</td>
        <td>${money(a.net_amount)}</td>
        <td>${escapeHtml(a.requested_by_name)}</td>
        <td class="muted">${escapeHtml(a.explanation || '')}</td>
        <td class="btn-row">
            <button type="button" onclick="decide('${a.approval_id}', '${a.branch_id}', ${a.record_version_at_request}, 'approved')">Approve</button>
            <button type="button" class="danger" onclick="decide('${a.approval_id}', '${a.branch_id}', ${a.record_version_at_request}, 'rejected')">Reject</button>
        </td>
    </tr>`).join('');
}

document.getElementById('refresh-btn').addEventListener('click', loadApprovals);
loadApprovals();
</script>
@endsection
