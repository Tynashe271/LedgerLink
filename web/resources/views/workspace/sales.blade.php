@extends('layouts.app')
@section('title', 'Sales · BranchLedger')
@section('body')
<h1>Sales</h1>
<p class="page-subtitle">Record a sale and review recent sales (<code>POST</code>/<code>GET /api/v1/transactions</code>).</p>

<div class="panel">
    <h2>Record a sale</h2>
    <div id="result"></div>
    <form id="sale-form">
        <label for="branch_id">Branch</label>
        <select id="branch_id" required>
            @foreach ($branches as $branch)
                <option value="{{ $branch->id }}">{{ $branch->name }}{{ $branch->is_main_branch ? ' (main)' : '' }}</option>
            @endforeach
        </select>

        <label for="product_id">Product</label>
        <select id="product_id">
            <option value="">Other / custom item (no stock tracked)</option>
        </select>

        <label for="description">Item or service description</label>
        <input id="description" type="text" value="Bag of Mealie Meal" required>

        <label for="quantity">Quantity</label>
        <input id="quantity" type="number" step="0.001" value="1" required>

        <label for="unit_price">Unit price (USD)</label>
        <input id="unit_price" type="number" step="0.01" value="25.00" required>

        <label for="payment_method">Payment</label>
        <select id="payment_method">
            <option value="cash">Cash sale</option>
            <option value="credit">Credit sale</option>
        </select>

        <div id="credit-fields" style="display:none;">
            <label for="customer_id">Customer</label>
            <select id="customer_id"><option value="">Select a customer</option></select>
            <label for="due_date">Payment due</label>
            <input id="due_date" type="date">
            <p class="muted" style="margin-top:4px;">Credit sales create a customer balance; a later receipt (Customers screen) settles it. Overdue balances appear on the dashboard.</p>
        </div>

        <button type="submit">Post sale</button>
    </form>
</div>

<div class="panel">
    <div class="panel-head">
        <h2>Recent sales</h2>
        <button id="refresh-btn" type="button" class="secondary">Refresh</button>
    </div>
    <table id="sales-table">
        <thead><tr><th>Date</th><th>Branch</th><th>Amount</th><th>Status</th><th>Reference</th></tr></thead>
        <tbody></tbody>
    </table>
</div>

<script type="module">
import { queueOperation, getCachedProducts, getCachedCustomers } from '/js/offline/sync.js';
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

async function populatePickers() {
    const products = await getCachedProducts();
    const productSelect = document.getElementById('product_id');
    productSelect.innerHTML = '<option value="">Other / custom item (no stock tracked)</option>' +
        products.map(p => `<option value="${p.id}" data-name="${escapeHtml(p.name)}">${escapeHtml(p.name)} (${escapeHtml(p.sku)})</option>`).join('');

    const customers = await getCachedCustomers();
    const customerSelect = document.getElementById('customer_id');
    customerSelect.innerHTML = '<option value="">Select a customer</option>' +
        customers.map(c => `<option value="${c.id}">${escapeHtml(c.name)}</option>`).join('');
}

document.getElementById('product_id').addEventListener('change', (e) => {
    const opt = e.target.selectedOptions[0];
    if (opt && opt.dataset.name) document.getElementById('description').value = opt.dataset.name;
});

const paymentMethodEl = document.getElementById('payment_method');
const creditFieldsEl = document.getElementById('credit-fields');
const dueDateEl = document.getElementById('due_date');
paymentMethodEl.addEventListener('change', () => {
    const isCredit = paymentMethodEl.value === 'credit';
    creditFieldsEl.style.display = isCredit ? 'block' : 'none';
    if (isCredit && !dueDateEl.value) {
        const d = new Date(); d.setDate(d.getDate() + 30);
        dueDateEl.value = d.toISOString().slice(0, 10);
    }
});

document.getElementById('sale-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('result');
    const isCredit = paymentMethodEl.value === 'credit';
    const productId = document.getElementById('product_id').value;

    const payload = {
        operation_id: uuidv4(),
        branch_id: document.getElementById('branch_id').value,
        document_type: 'sale',
        document_date: new Date().toISOString().slice(0, 10),
        currency_code: 'USD',
        exchange_rate: '1',
        lines: [{
            product_id: productId || undefined,
            description: document.getElementById('description').value,
            quantity: document.getElementById('quantity').value,
            unit_price: document.getElementById('unit_price').value,
            discount: '0',
        }],
    };
    if (isCredit) {
        const customerId = document.getElementById('customer_id').value;
        if (!customerId) { resultEl.innerHTML = '<div class="errors">Select a customer for a credit sale.</div>'; return; }
        payload.counterparty_id = customerId;
        payload.due_date = dueDateEl.value;
    }

    // Saved to the local outbox first, same as an offline entry would be —
    // this is the only write path the form uses. queueOperation makes a
    // best-effort immediate sync when online, but the result here is always
    // "saved locally," never a server acknowledgement.
    await queueOperation(payload);
    resultEl.innerHTML = '<div class="status">Saved locally. Syncing automatically &mdash; check Sync centre or this list for the outcome.</div>';
    e.target.reset();
    creditFieldsEl.style.display = 'none';
    loadSales();
});

populatePickers();

async function loadSales() {
    const to = new Date().toISOString().slice(0, 10);
    const from = new Date(Date.now() - 30 * 86400000).toISOString().slice(0, 10);
    const { status, body } = await apiFetch(`v1/transactions?document_type=sale&from=${from}&to=${to}`);
    const serverRows = (status === 200 && Array.isArray(body)) ? body : [];

    const localRows = (await outboxRecordsByStatus(['pending', 'syncing', 'failed', 'rejected']))
        .filter(r => r.document_type === 'sale')
        .map(r => ({
            document_date: r.payload.document_date, branch_name: '(this device)',
            net_amount: r.payload.lines.reduce((s, l) => s + Number(l.quantity) * Number(l.unit_price) - Number(l.discount || 0), 0),
            status: r.status, source_reference: r.error_code || '', local: true,
        }));

    const tbody = document.querySelector('#sales-table tbody');
    const rows = [...localRows, ...serverRows];
    if (!rows.length) {
        tbody.innerHTML = '<tr class="empty-row"><td colspan="5">No sales in the last 30 days.</td></tr>';
        return;
    }
    tbody.innerHTML = rows.map(t => `<tr>
        <td>${t.document_date}</td>
        <td>${escapeHtml(t.branch_name)}</td>
        <td>${money(t.net_amount)}</td>
        <td><span class="badge badge-${t.status}">${t.status.replace('_', ' ')}</span></td>
        <td class="muted">${escapeHtml(t.source_reference || t.explanation || '')}</td>
    </tr>`).join('');
}

document.getElementById('refresh-btn').addEventListener('click', loadSales);
loadSales();
setInterval(loadSales, 5000);
</script>
@endsection
