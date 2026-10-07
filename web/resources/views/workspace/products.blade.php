@extends('layouts.app')
@section('title', 'Products · BranchLedger')
@section('body')
<h1>Products</h1>
<p class="page-subtitle">
    Products sold or stocked through Sales and Purchases need to exist here first &mdash; selecting a real
    product on a line is what makes stock actually get tracked and feeds the low-stock alert on the dashboard.
</p>

<div class="panel">
    <h2>Add a product</h2>
    <div id="result"></div>
    <form id="product-form">
        <div class="grid grid-2">
            <div>
                <label for="sku">SKU</label>
                <input id="sku" type="text" placeholder="e.g. MM-10KG" required>
            </div>
            <div>
                <label for="name">Name</label>
                <input id="name" type="text" placeholder="e.g. Mealie Meal 10kg" required>
            </div>
        </div>
        <div class="grid grid-2">
            <div>
                <label for="unit">Unit</label>
                <input id="unit" type="text" placeholder="e.g. bag, kg, each" required>
            </div>
            <div>
                <label for="kind">Kind</label>
                <select id="kind">
                    <option value="stocked">Stocked</option>
                    <option value="service">Service (no stock tracked)</option>
                </select>
            </div>
        </div>
        <label for="reorder_point">Reorder point</label>
        <input id="reorder_point" type="number" step="0.001" value="5">
        <p class="muted" style="margin-top:4px;">The dashboard flags this product once tracked stock at a branch falls to or below this quantity.</p>

        <button type="submit">Add product</button>
    </form>
</div>

<div class="panel">
    <div class="panel-head">
        <h2>Catalogue</h2>
        <button id="refresh-btn" type="button" class="secondary">Refresh</button>
    </div>
    <table id="products-table">
        <thead><tr><th>SKU</th><th>Name</th><th>Unit</th><th>Kind</th><th>Reorder point</th></tr></thead>
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

document.getElementById('product-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('result');
    const payload = {
        sku: document.getElementById('sku').value,
        name: document.getElementById('name').value,
        unit: document.getElementById('unit').value,
        kind: document.getElementById('kind').value,
        reorder_point: document.getElementById('reorder_point').value || '5',
    };
    const { status, body } = await apiFetch('v1/products', { method: 'POST', body: JSON.stringify(payload) });
    if (status === 201) {
        resultEl.innerHTML = '<div class="status">Product added.</div>';
        e.target.reset();
        loadProducts();
    } else {
        resultEl.innerHTML = `<div class="errors">Rejected (HTTP ${status}): ${body.error_code || 'unknown error'}</div>`;
    }
});

async function loadProducts() {
    const { status, body } = await apiFetch('v1/products');
    const tbody = document.querySelector('#products-table tbody');
    if (status !== 200 || !Array.isArray(body) || !body.length) {
        tbody.innerHTML = '<tr class="empty-row"><td colspan="5">No products yet.</td></tr>';
        return;
    }
    tbody.innerHTML = body.map(p => `<tr>
        <td>${escapeHtml(p.sku)}</td>
        <td>${escapeHtml(p.name)}</td>
        <td>${escapeHtml(p.unit)}</td>
        <td>${escapeHtml(p.kind)}</td>
        <td>${p.reorder_point}</td>
    </tr>`).join('');
}

document.getElementById('refresh-btn').addEventListener('click', loadProducts);
loadProducts();
</script>
@endsection
