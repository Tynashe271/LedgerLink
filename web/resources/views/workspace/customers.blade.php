@extends('layouts.app')
@section('title', 'Customers · BranchLedger')
@section('body')
<h1>Customers</h1>
<p class="page-subtitle">
    Select a customer on a credit sale so their balance can be tracked, and record a receipt here when they pay &mdash;
    outstanding and overdue balances show up in the dashboard's "Needs attention" panel.
</p>

<div class="panel">
    <h2>Add a customer</h2>
    <div id="result"></div>
    <form id="customer-form">
        <label for="name">Name</label>
        <input id="name" type="text" required>
        <label for="contact">Contact</label>
        <input id="contact" type="text" placeholder="Phone or email">
        <button type="submit">Add customer</button>
    </form>
</div>

<div class="panel">
    <h2>Record a customer receipt</h2>
    <p class="muted">Settles part or all of a customer's outstanding balance (<code>POST /api/v1/transactions</code>, document_type "receipt").</p>
    <div id="receipt-result"></div>
    <form id="receipt-form">
        <label for="branch_id">Branch</label>
        <select id="branch_id" required>
            @foreach ($branches as $branch)
                <option value="{{ $branch->id }}">{{ $branch->name }}{{ $branch->is_main_branch ? ' (main)' : '' }}</option>
            @endforeach
        </select>
        <label for="customer_id">Customer</label>
        <select id="customer_id" required></select>
        <label for="amount">Amount received (USD)</label>
        <input id="amount" type="number" step="0.01" required>
        <button type="submit">Record receipt</button>
    </form>
</div>

<div class="panel">
    <div class="panel-head">
        <h2>Customers</h2>
        <button id="refresh-btn" type="button" class="secondary">Refresh</button>
    </div>
    <table id="customers-table">
        <thead><tr><th>Name</th><th>Contact</th></tr></thead>
        <tbody></tbody>
    </table>
    <p class="muted" style="margin-top:10px;">Per-customer outstanding balances aren't shown here yet &mdash; see the dashboard's overdue-debt alert for customers who owe past their due date.</p>
</div>

<script type="module">
const csrfToken = document.querySelector('meta[name="csrf-token"]').content;
function uuidv4() { return crypto.randomUUID(); }
function escapeHtml(s) { return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

async function apiFetch(path, options = {}) {
    const headers = Object.assign({ 'Content-Type': 'application/json', 'X-CSRF-TOKEN': csrfToken, 'Accept': 'application/json' }, options.headers || {});
    const res = await fetch('/api/' + path, Object.assign({}, options, { headers, credentials: 'same-origin' }));
    const body = await res.json().catch(() => ({}));
    return { status: res.status, body };
}

document.getElementById('customer-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('result');
    const { status, body } = await apiFetch('v1/customers', {
        method: 'POST',
        body: JSON.stringify({ name: document.getElementById('name').value, contact: document.getElementById('contact').value }),
    });
    if (status === 201) {
        resultEl.innerHTML = '<div class="status">Customer added.</div>';
        e.target.reset();
        loadCustomers();
    } else {
        resultEl.innerHTML = `<div class="errors">Rejected (HTTP ${status}): ${body.error_code || 'unknown error'}</div>`;
    }
});

// A receipt settles a specific customer's balance against the current
// server state, so — like transfer receive — this is a direct connected
// call, not queued through the offline outbox.
document.getElementById('receipt-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('receipt-result');
    const payload = {
        operation_id: uuidv4(),
        branch_id: document.getElementById('branch_id').value,
        document_type: 'receipt',
        document_date: new Date().toISOString().slice(0, 10),
        currency_code: 'USD',
        exchange_rate: '1',
        counterparty_id: document.getElementById('customer_id').value,
        lines: [{ description: 'Customer receipt', quantity: '1', unit_price: document.getElementById('amount').value }],
    };
    const { status, body } = await apiFetch('v1/transactions', { method: 'POST', body: JSON.stringify(payload) });
    if (status === 201 || status === 200) {
        resultEl.innerHTML = '<div class="status">Receipt recorded.</div>';
        e.target.reset();
    } else {
        resultEl.innerHTML = `<div class="errors">Rejected (HTTP ${status}): ${body.error_code || 'unknown error'}</div>`;
    }
});

async function loadCustomers() {
    const { status, body } = await apiFetch('v1/customers');
    const tbody = document.querySelector('#customers-table tbody');
    const select = document.getElementById('customer_id');
    if (status !== 200 || !Array.isArray(body) || !body.length) {
        tbody.innerHTML = '<tr class="empty-row"><td colspan="2">No customers yet.</td></tr>';
        select.innerHTML = '<option value="">No customers yet</option>';
        return;
    }
    tbody.innerHTML = body.map(c => `<tr><td>${escapeHtml(c.name)}</td><td>${escapeHtml(c.contact || '')}</td></tr>`).join('');
    select.innerHTML = body.map(c => `<option value="${c.id}">${escapeHtml(c.name)}</option>`).join('');
}

document.getElementById('refresh-btn').addEventListener('click', loadCustomers);
loadCustomers();
</script>
@endsection
