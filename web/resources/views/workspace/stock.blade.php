@extends('layouts.app')
@section('title', 'Stock · BranchLedger')
@section('body')
<h1>Stock</h1>
<p class="page-subtitle">
    Current on-hand balance and valuation per branch, plus waste and stock-count reconciliation
    (<code>GET /api/v1/stock</code>, <code>POST /api/v1/transactions</code>). Stock counts and waste affect only
    tracked ("stocked") products.
</p>

<div class="grid grid-2">
    <div class="panel">
        <h2>Record waste</h2>
        <div id="waste-result"></div>
        <form id="waste-form">
            <label for="waste_branch_id">Branch</label>
            <select id="waste_branch_id" required>
                @foreach ($branches as $branch)
                    <option value="{{ $branch->id }}">{{ $branch->name }}{{ $branch->is_main_branch ? ' (main)' : '' }}</option>
                @endforeach
            </select>

            <label for="waste_product_id">Product</label>
            <select id="waste_product_id" required>
                <option value="">Select a product&hellip;</option>
            </select>

            <label for="waste_quantity">Quantity wasted</label>
            <input id="waste_quantity" type="number" step="0.001" min="0.001" value="1" required>

            <label for="waste_reason">Reason</label>
            <input id="waste_reason" type="text" placeholder="e.g. Expired, damaged in transit" required>

            <button type="submit">Record waste</button>
        </form>
        <p class="muted" style="margin-top:10px;">Posts Dr Stock loss expense / Cr Inventory at the branch's current weighted-average cost.</p>
    </div>

    <div class="panel">
        <h2>Stock count</h2>
        <div id="count-result"></div>
        <form id="count-form">
            <label for="count_branch_id">Branch</label>
            <select id="count_branch_id" required>
                @foreach ($branches as $branch)
                    <option value="{{ $branch->id }}">{{ $branch->name }}{{ $branch->is_main_branch ? ' (main)' : '' }}</option>
                @endforeach
            </select>

            <label for="count_product_id">Product</label>
            <select id="count_product_id" required>
                <option value="">Select a product&hellip;</option>
            </select>

            <label for="count_quantity">Counted quantity</label>
            <input id="count_quantity" type="number" step="0.001" min="0" value="0" required>
            <p class="muted" style="margin-top:4px;">Compared against the recorded position; a matching count posts nothing.</p>

            <label for="count_note">Note</label>
            <input id="count_note" type="text" placeholder="e.g. Monthly count, 2026-10-07">

            <button type="submit">Submit count</button>
        </form>
    </div>
</div>

<div class="panel">
    <div class="panel-head">
        <h2>Current positions</h2>
        <button id="refresh-btn" type="button" class="secondary">Refresh</button>
    </div>
    <table id="stock-table">
        <thead><tr><th>Branch</th><th>SKU</th><th>Product</th><th>Quantity</th><th>Unit</th><th>Unit cost</th><th>Value</th></tr></thead>
        <tbody></tbody>
    </table>
</div>

<div class="panel">
    <div class="panel-head">
        <h2>Recent adjustments (this device)</h2>
    </div>
    <table id="adjustments-table">
        <thead><tr><th>Branch</th><th>Type</th><th>Product</th><th>Quantity</th><th>Status</th></tr></thead>
        <tbody></tbody>
    </table>
</div>

<script type="module">
import { queueOperation, getCachedProducts } from '/js/offline/sync.js';
import { outboxRecordsByStatus } from '/js/offline/db.js';

function uuidv4() { return crypto.randomUUID(); }
function money(v) { const n = Number(v); return (n < 0 ? '-$' + Math.abs(n).toFixed(2) : '$' + n.toFixed(2)); }
function escapeHtml(s) { return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

const csrfToken = document.querySelector('meta[name="csrf-token"]').content;
async function apiFetch(path, options = {}) {
    const headers = Object.assign({ 'Content-Type': 'application/json', 'X-CSRF-TOKEN': csrfToken, 'Accept': 'application/json' }, options.headers || {});
    const res = await fetch('/api/' + path, Object.assign({}, options, { headers, credentials: 'same-origin' }));
    const body = await res.json().catch(() => ({}));
    return { status: res.status, body };
}

async function populateProductPickers() {
    const products = await getCachedProducts();
    const options = '<option value="">Select a product&hellip;</option>' +
        products.filter(p => p.kind === 'stocked').map(p => `<option value="${p.id}" data-name="${escapeHtml(p.name)}">${escapeHtml(p.name)} (${escapeHtml(p.sku)})</option>`).join('');
    document.getElementById('waste_product_id').innerHTML = options;
    document.getElementById('count_product_id').innerHTML = options;
}
populateProductPickers();

document.getElementById('waste-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('waste-result');
    const productSelect = document.getElementById('waste_product_id');
    const productId = productSelect.value;
    if (!productId) {
        resultEl.innerHTML = '<div class="errors">Select a product.</div>';
        return;
    }
    const productName = productSelect.selectedOptions[0].dataset.name;

    const payload = {
        operation_id: uuidv4(),
        branch_id: document.getElementById('waste_branch_id').value,
        document_type: 'stock_adjustment',
        adjustment_type: 'waste',
        document_date: new Date().toISOString().slice(0, 10),
        currency_code: 'USD',
        exchange_rate: '1',
        explanation: document.getElementById('waste_reason').value,
        lines: [{
            product_id: productId, description: 'Waste: ' + productName,
            quantity: document.getElementById('waste_quantity').value, unit_price: '0', discount: '0',
        }],
    };

    await queueOperation(payload);
    resultEl.innerHTML = '<div class="status">Saved locally. Syncing automatically.</div>';
    e.target.reset();
    loadAdjustments();
});

document.getElementById('count-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('count-result');
    const productSelect = document.getElementById('count_product_id');
    const productId = productSelect.value;
    if (!productId) {
        resultEl.innerHTML = '<div class="errors">Select a product.</div>';
        return;
    }
    const productName = productSelect.selectedOptions[0].dataset.name;

    const payload = {
        operation_id: uuidv4(),
        branch_id: document.getElementById('count_branch_id').value,
        document_type: 'stock_adjustment',
        adjustment_type: 'count',
        document_date: new Date().toISOString().slice(0, 10),
        currency_code: 'USD',
        exchange_rate: '1',
        explanation: document.getElementById('count_note').value || 'Stock count',
        lines: [{
            product_id: productId, description: 'Count: ' + productName,
            quantity: document.getElementById('count_quantity').value, unit_price: '0', discount: '0',
        }],
    };

    await queueOperation(payload);
    resultEl.innerHTML = '<div class="status">Saved locally. Syncing automatically. A count matching the recorded position is accepted with no posting.</div>';
    e.target.reset();
    loadAdjustments();
});

async function loadStockPositions() {
    const { status, body } = await apiFetch('v1/stock');
    const tbody = document.querySelector('#stock-table tbody');
    if (status !== 200 || !Array.isArray(body) || !body.length) {
        tbody.innerHTML = '<tr class="empty-row"><td colspan="7">No tracked stock movements yet.</td></tr>';
        return;
    }
    tbody.innerHTML = body.map(s => `<tr>
        <td>${escapeHtml(s.branch_name)}</td>
        <td>${escapeHtml(s.sku)}</td>
        <td>${escapeHtml(s.product_name)}</td>
        <td>${Number(s.quantity).toFixed(3)}</td>
        <td>${escapeHtml(s.unit)}</td>
        <td>${money(s.unit_cost)}</td>
        <td>${money(s.value)}</td>
    </tr>`).join('');
}

async function loadAdjustments() {
    const tbody = document.querySelector('#adjustments-table tbody');
    const branchNames = Object.fromEntries(
        [...document.querySelectorAll('#waste_branch_id option')].map(o => [o.value, o.textContent])
    );
    const rows = (await outboxRecordsByStatus(['pending', 'syncing', 'failed', 'rejected', 'synced']))
        .filter(r => r.document_type === 'stock_adjustment')
        .sort((a, b) => b.created_at.localeCompare(a.created_at))
        .slice(0, 20);
    if (!rows.length) {
        tbody.innerHTML = '<tr class="empty-row"><td colspan="5">No adjustments recorded on this device yet.</td></tr>';
        return;
    }
    tbody.innerHTML = rows.map(r => `<tr>
        <td>${escapeHtml(branchNames[r.branch_id] || '')}</td>
        <td>${escapeHtml(r.payload.adjustment_type)}</td>
        <td>${escapeHtml(r.payload.lines[0].description)}</td>
        <td>${Number(r.payload.lines[0].quantity).toFixed(3)}</td>
        <td><span class="badge badge-${r.status}">${(r.error_code ? r.status + ': ' + r.error_code : r.status).replace('_', ' ')}</span></td>
    </tr>`).join('');
}

document.getElementById('refresh-btn').addEventListener('click', loadStockPositions);
loadStockPositions();
loadAdjustments();
setInterval(() => { loadStockPositions(); loadAdjustments(); }, 5000);
</script>
@endsection
