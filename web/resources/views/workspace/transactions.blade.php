@extends('layouts.app')
@section('title', 'Transactions · BranchLedger')
@section('body')
<h1>Transactions</h1>
<p class="page-subtitle">
    Search, view and reverse across every document type (<code>GET /api/v1/transactions</code>,
    <code>POST /api/v1/transactions/{id}/reverse</code>). A reversal posts the mirror-image journal and never
    deletes or edits the original.
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
            <select id="type-filter" style="width:auto;">
                <option value="">All types</option>
                <option value="sale">Sale</option>
                <option value="purchase">Purchase</option>
                <option value="expense">Expense</option>
                <option value="receipt">Receipt</option>
                <option value="payment">Payment</option>
                <option value="transfer">Transfer</option>
                <option value="stock_adjustment">Stock adjustment</option>
                <option value="close">Close</option>
            </select>
            <select id="status-filter" style="width:auto;">
                <option value="">All statuses</option>
                <option value="posted">Posted</option>
                <option value="awaiting_approval">Awaiting approval</option>
                <option value="reversed">Reversed</option>
                <option value="rejected">Rejected</option>
                <option value="draft">Draft</option>
            </select>
            <input id="from-filter" type="date" style="width:auto;">
            <input id="to-filter" type="date" style="width:auto;">
        </div>
        <button id="refresh-btn" type="button" class="secondary">Refresh</button>
    </div>
    <div id="result"></div>
    <table id="transactions-table">
        <thead><tr><th>Date</th><th>Branch</th><th>Type</th><th>Amount</th><th>Status</th><th>Reference</th><th>Explanation</th><th>Action</th></tr></thead>
        <tbody></tbody>
    </table>
</div>

<script type="module">
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

const today = new Date().toISOString().slice(0, 10);
document.getElementById('from-filter').value = new Date(Date.now() - 30 * 86400000).toISOString().slice(0, 10);
document.getElementById('to-filter').value = today;

async function loadTransactions() {
    const branchId = document.getElementById('branch-filter').value;
    const type = document.getElementById('type-filter').value;
    const status = document.getElementById('status-filter').value;
    const from = document.getElementById('from-filter').value;
    const to = document.getElementById('to-filter').value;

    let path = `v1/transactions?from=${from}&to=${to}`;
    if (branchId) path += `&branch_id=${branchId}`;
    if (type) path += `&document_type=${type}`;
    if (status) path += `&status=${status}`;

    const { status: httpStatus, body } = await apiFetch(path);
    const tbody = document.querySelector('#transactions-table tbody');
    if (httpStatus !== 200) {
        tbody.innerHTML = `<tr class="empty-row"><td colspan="8">Could not load transactions (HTTP ${httpStatus}).</td></tr>`;
        return;
    }
    if (!body.length) {
        tbody.innerHTML = '<tr class="empty-row"><td colspan="8">No transactions match these filters.</td></tr>';
        return;
    }
    tbody.innerHTML = body.map(t => `<tr>
        <td>${t.document_date}</td><td>${escapeHtml(t.branch_name)}</td><td>${escapeHtml(t.document_type)}</td>
        <td>${money(t.net_amount)}</td>
        <td><span class="badge badge-${t.status}">${t.status.replace('_', ' ')}</span></td>
        <td class="muted">${escapeHtml(t.source_reference || '')}</td>
        <td class="muted">${escapeHtml(t.explanation || '')}</td>
        <td>${t.status === 'posted' ? `<button type="button" class="secondary" onclick="window.reverseTransaction('${t.id}', '${t.branch_id}')">Reverse</button>` : ''}</td>
    </tr>`).join('');
}

window.reverseTransaction = async (transactionId, branchId) => {
    const reason = prompt('Reason for reversal:');
    if (!reason) return;
    const resultEl = document.getElementById('result');
    const { status, body } = await apiFetch(`v1/transactions/${transactionId}/reverse`, {
        method: 'POST',
        body: JSON.stringify({ operation_id: uuidv4(), branch_id: branchId, reason }),
    });
    if (status === 201 || status === 200) {
        resultEl.innerHTML = '<div class="status">Reversed.</div>';
    } else {
        resultEl.innerHTML = `<div class="errors">Could not reverse (HTTP ${status}): ${body.error_code || 'unknown error'}</div>`;
    }
    loadTransactions();
};

document.getElementById('branch-filter').addEventListener('change', loadTransactions);
document.getElementById('type-filter').addEventListener('change', loadTransactions);
document.getElementById('status-filter').addEventListener('change', loadTransactions);
document.getElementById('from-filter').addEventListener('change', loadTransactions);
document.getElementById('to-filter').addEventListener('change', loadTransactions);
document.getElementById('refresh-btn').addEventListener('click', loadTransactions);
loadTransactions();
</script>
@endsection
