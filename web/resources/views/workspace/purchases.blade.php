@extends('layouts.app')
@section('title', 'Purchases · BranchLedger')
@section('body')
<h1>Purchases</h1>
<p class="page-subtitle">Record goods or services received on credit (<code>POST</code>/<code>GET /api/v1/transactions</code>).</p>

<div class="panel">
    <h2>Record a purchase</h2>
    <div id="result"></div>
    <form id="purchase-form">
        <label for="branch_id">Branch</label>
        <select id="branch_id" required>
            @foreach ($branches as $branch)
                <option value="{{ $branch->id }}">{{ $branch->name }}{{ $branch->is_main_branch ? ' (main)' : '' }}</option>
            @endforeach
        </select>

        <label for="source_reference">Supplier invoice reference</label>
        <input id="source_reference" type="text" placeholder="e.g. INV-1042" required>

        <label for="product_id">Product</label>
        <select id="product_id">
            <option value="">Other / service (no stock received)</option>
        </select>
        <p class="muted" style="margin-top:4px;">Selecting a product receives it into tracked stock at the branch's moving weighted-average cost.</p>

        <label for="description">Goods or services</label>
        <input id="description" type="text" value="Stock replenishment" required>

        <label for="quantity">Quantity</label>
        <input id="quantity" type="number" step="0.001" value="1" required>

        <label for="unit_price">Unit cost (USD)</label>
        <input id="unit_price" type="number" step="0.01" value="100.00" required>

        <button type="submit">Post purchase</button>
    </form>
    <p class="muted" style="margin-top:10px;">
        Duplicate-invoice detection compares supplier and reference server-side; a repeat reference
        for the same supplier is flagged rather than silently re-posted.
    </p>
</div>

<div class="panel">
    <div class="panel-head">
        <h2>Recent purchases</h2>
        <button id="refresh-btn" type="button" class="secondary">Refresh</button>
    </div>
    <table id="purchases-table">
        <thead><tr><th>Date</th><th>Branch</th><th>Amount</th><th>Status</th><th>Reference</th></tr></thead>
        <tbody></tbody>
    </table>
</div>

<script type="module">
import { queueOperation, getCachedProducts } from '/js/offline/sync.js';
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

async function populateProductPicker() {
    const products = await getCachedProducts();
    const select = document.getElementById('product_id');
    select.innerHTML = '<option value="">Other / service (no stock received)</option>' +
        products.map(p => `<option value="${p.id}" data-name="${escapeHtml(p.name)}">${escapeHtml(p.name)} (${escapeHtml(p.sku)})</option>`).join('');
}
document.getElementById('product_id').addEventListener('change', (e) => {
    const opt = e.target.selectedOptions[0];
    if (opt && opt.dataset.name) document.getElementById('description').value = opt.dataset.name;
});
populateProductPicker();

document.getElementById('purchase-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('result');
    const productId = document.getElementById('product_id').value;

    const payload = {
        operation_id: uuidv4(),
        branch_id: document.getElementById('branch_id').value,
        document_type: 'purchase',
        document_date: new Date().toISOString().slice(0, 10),
        currency_code: 'USD',
        exchange_rate: '1',
        source_reference: document.getElementById('source_reference').value,
        lines: [{
            product_id: productId || undefined,
            description: document.getElementById('description').value,
            quantity: document.getElementById('quantity').value,
            unit_price: document.getElementById('unit_price').value,
            discount: '0',
        }],
    };

    await queueOperation(payload);
    resultEl.innerHTML = '<div class="status">Saved locally. Syncing automatically.</div>';
    e.target.reset();
    loadPurchases();
});

async function loadPurchases() {
    const to = new Date().toISOString().slice(0, 10);
    const from = new Date(Date.now() - 30 * 86400000).toISOString().slice(0, 10);
    const { status, body } = await apiFetch(`v1/transactions?document_type=purchase&from=${from}&to=${to}`);
    const serverRows = (status === 200 && Array.isArray(body)) ? body : [];

    const localRows = (await outboxRecordsByStatus(['pending', 'syncing', 'failed', 'rejected']))
        .filter(r => r.document_type === 'purchase')
        .map(r => ({
            document_date: r.payload.document_date, branch_name: '(this device)',
            net_amount: r.payload.lines.reduce((s, l) => s + Number(l.quantity) * Number(l.unit_price) - Number(l.discount || 0), 0),
            status: r.status, source_reference: r.error_code || r.payload.source_reference || '',
        }));

    const tbody = document.querySelector('#purchases-table tbody');
    const rows = [...localRows, ...serverRows];
    if (!rows.length) {
        tbody.innerHTML = '<tr class="empty-row"><td colspan="5">No purchases in the last 30 days.</td></tr>';
        return;
    }
    tbody.innerHTML = rows.map(t => `<tr>
        <td>${t.document_date}</td>
        <td>${escapeHtml(t.branch_name)}</td>
        <td>${money(t.net_amount)}</td>
        <td><span class="badge badge-${t.status}">${t.status.replace('_', ' ')}</span></td>
        <td class="muted">${escapeHtml(t.source_reference || '')}</td>
    </tr>`).join('');
}

document.getElementById('refresh-btn').addEventListener('click', loadPurchases);
loadPurchases();
setInterval(loadPurchases, 5000);
</script>
@endsection
