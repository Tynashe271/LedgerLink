@extends('layouts.app')
@section('title', 'Expenses · BranchLedger')
@section('body')
<h1>Expenses</h1>
<p class="page-subtitle">
    An expense above your configured approval limit is saved as <span class="badge badge-awaiting_approval">awaiting approval</span>
    and does not reduce confirmed cash until a decision posts it (<code>POST /api/v1/transactions</code>).
</p>

<div class="panel">
    <h2>Record an expense</h2>
    <div id="result"></div>
    <form id="expense-form">
        <label for="branch_id">Branch</label>
        <select id="branch_id" required>
            @foreach ($branches as $branch)
                <option value="{{ $branch->id }}">{{ $branch->name }}{{ $branch->is_main_branch ? ' (main)' : '' }}</option>
            @endforeach
        </select>

        <label for="description">Category / description</label>
        <input id="description" type="text" value="Office supplies" required>

        <label for="amount">Amount (USD)</label>
        <input id="amount" type="number" step="0.01" value="20.00" required>

        <label for="explanation">Explanation</label>
        <textarea id="explanation" rows="2" placeholder="Reason, supplier, receipt reference"></textarea>

        <button type="submit">Submit expense</button>
    </form>
</div>

<div class="panel">
    <div class="panel-head">
        <h2>Recent expenses</h2>
        <button id="refresh-btn" type="button" class="secondary">Refresh</button>
    </div>
    <table id="expenses-table">
        <thead><tr><th>Date</th><th>Branch</th><th>Amount</th><th>Status</th><th>Explanation</th></tr></thead>
        <tbody></tbody>
    </table>
</div>

<script type="module">
import { queueOperation } from '/js/offline/sync.js';
import { outboxRecordsByStatus } from '/js/offline/db.js';

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

document.getElementById('expense-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('result');

    const payload = {
        operation_id: uuidv4(),
        branch_id: document.getElementById('branch_id').value,
        document_type: 'expense',
        document_date: new Date().toISOString().slice(0, 10),
        currency_code: 'USD',
        exchange_rate: '1',
        explanation: document.getElementById('explanation').value,
        lines: [{
            description: document.getElementById('description').value,
            quantity: '1',
            unit_price: document.getElementById('amount').value,
            discount: '0',
        }],
    };

    await queueOperation(payload);
    resultEl.innerHTML = '<div class="status">Saved locally. Syncing automatically &mdash; if this is above your approval limit it will show as "awaiting approval" below once synced.</div>';
    e.target.reset();
    loadExpenses();
});

async function loadExpenses() {
    const to = new Date().toISOString().slice(0, 10);
    const from = new Date(Date.now() - 30 * 86400000).toISOString().slice(0, 10);
    const { status, body } = await apiFetch(`v1/transactions?document_type=expense&from=${from}&to=${to}`);
    const serverRows = (status === 200 && Array.isArray(body)) ? body : [];

    const localRows = (await outboxRecordsByStatus(['pending', 'syncing', 'failed', 'rejected']))
        .filter(r => r.document_type === 'expense')
        .map(r => ({
            document_date: r.payload.document_date, branch_name: '(this device)',
            net_amount: r.payload.lines.reduce((s, l) => s + Number(l.quantity) * Number(l.unit_price) - Number(l.discount || 0), 0),
            status: r.status, explanation: r.error_code || r.payload.explanation || '',
        }));

    const tbody = document.querySelector('#expenses-table tbody');
    const rows = [...localRows, ...serverRows];
    if (!rows.length) {
        tbody.innerHTML = '<tr class="empty-row"><td colspan="5">No expenses in the last 30 days.</td></tr>';
        return;
    }
    tbody.innerHTML = rows.map(t => `<tr>
        <td>${t.document_date}</td>
        <td>${escapeHtml(t.branch_name)}</td>
        <td>${money(t.net_amount)}</td>
        <td><span class="badge badge-${t.status}">${t.status.replace('_', ' ')}</span></td>
        <td class="muted">${escapeHtml(t.explanation || '')}</td>
    </tr>`).join('');
}

document.getElementById('refresh-btn').addEventListener('click', loadExpenses);
loadExpenses();
setInterval(loadExpenses, 5000);
</script>
@endsection
