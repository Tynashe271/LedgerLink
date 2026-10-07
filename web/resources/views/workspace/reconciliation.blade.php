@extends('layouts.app')
@section('title', 'Reconciliation · BranchLedger')
@section('body')
<h1>Reconciliation</h1>
<p class="page-subtitle">
    Match statement items (bank, mobile money or a cash count) against posted ledger entries and resolve what's
    left outstanding (<code>GET /api/v1/reconciliation</code>). Only cash-like accounts are reconciled here.
</p>

<div class="panel">
    <div class="panel-head">
        <div style="display:flex; gap:10px; align-items:center; flex-wrap:wrap;">
            <select id="account-filter" style="width:auto; min-width:160px;"><option value="">Select an account&hellip;</option></select>
            <input id="from-filter" type="date" style="width:auto;">
            <input id="to-filter" type="date" style="width:auto;">
        </div>
        <span style="display:flex; gap:10px;">
            <button id="auto-match-btn" type="button" class="secondary">Auto-match</button>
            <button id="refresh-btn" type="button" class="secondary">Refresh</button>
        </span>
    </div>
    <div id="overview-meta" class="muted"></div>
    <div id="action-result"></div>
</div>

<div class="panel">
    <h2>Add a statement item</h2>
    <div id="add-result"></div>
    <form id="add-form">
        <label for="item_branch_id">Branch</label>
        <select id="item_branch_id" required>
            @foreach ($branches as $branch)
                <option value="{{ $branch->id }}">{{ $branch->name }}{{ $branch->is_main_branch ? ' (main)' : '' }}</option>
            @endforeach
        </select>

        <label for="item_date">Statement date</label>
        <input id="item_date" type="date" required>

        <label for="item_description">Description</label>
        <input id="item_description" type="text" placeholder="e.g. POS settlement, bank charge" required>

        <label for="item_amount">Amount</label>
        <input id="item_amount" type="number" step="0.01" placeholder="e.g. 250.00 or -15.00" required>
        <p class="muted" style="margin-top:4px;">Positive for a deposit/inflow, negative for a withdrawal/outflow.</p>

        <label for="item_reference">Reference</label>
        <input id="item_reference" type="text" placeholder="optional">

        <button type="submit">Add statement item</button>
    </form>
</div>

<div class="grid grid-2">
    <div class="panel">
        <h2>Statement items</h2>
        <table id="items-table">
            <thead><tr><th>Date</th><th>Branch</th><th>Description</th><th>Amount</th><th>Status</th><th>Action</th></tr></thead>
            <tbody></tbody>
        </table>
    </div>
    <div class="panel">
        <h2>Ledger entries</h2>
        <table id="entries-table">
            <thead><tr><th>Date</th><th>Branch</th><th>Type</th><th>Description</th><th>Amount</th><th>Status</th></tr></thead>
            <tbody></tbody>
        </table>
    </div>
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

const today = new Date().toISOString().slice(0, 10);
const defaultFrom = new Date(Date.now() - 30 * 86400000).toISOString().slice(0, 10);
document.getElementById('from-filter').value = defaultFrom;
document.getElementById('to-filter').value = today;
document.getElementById('item_date').value = today;

let lastOverview = null;

async function populateAccounts() {
    const { status, body } = await apiFetch('v1/reconciliation');
    if (status !== 200) return;
    const select = document.getElementById('account-filter');
    const accounts = body.accounts || [];
    select.innerHTML = '<option value="">Select an account&hellip;</option>' +
        accounts.map(a => `<option value="${a.id}">${escapeHtml(a.name)} (${escapeHtml(a.code)})</option>`).join('');
    if (accounts.length) {
        select.value = accounts[0].id;
        loadOverview();
    }
}

function candidatesFor(item, entries) {
    return entries.filter(e => !e.matched && Number(e.amount) === Number(item.amount));
}

async function loadOverview() {
    const accountId = document.getElementById('account-filter').value;
    const itemsTbody = document.querySelector('#items-table tbody');
    const entriesTbody = document.querySelector('#entries-table tbody');
    if (!accountId) {
        itemsTbody.innerHTML = '<tr class="empty-row"><td colspan="6">Select an account.</td></tr>';
        entriesTbody.innerHTML = '<tr class="empty-row"><td colspan="6">Select an account.</td></tr>';
        return;
    }
    const from = document.getElementById('from-filter').value;
    const to = document.getElementById('to-filter').value;

    const { status, body } = await apiFetch(`v1/reconciliation?account_id=${accountId}&from=${from}&to=${to}`);
    if (status !== 200) {
        document.getElementById('overview-meta').textContent = `Could not load reconciliation (HTTP ${status}).`;
        return;
    }
    lastOverview = body;

    const items = body.statement_items || [];
    const entries = body.ledger_entries || [];
    const outstandingItems = items.filter(i => i.status === 'unmatched').length;
    const outstandingEntries = entries.filter(e => !e.matched).length;
    document.getElementById('overview-meta').textContent =
        `${items.length} statement item(s), ${outstandingItems} outstanding · ${entries.length} ledger entr${entries.length === 1 ? 'y' : 'ies'}, ${outstandingEntries} outstanding`;

    itemsTbody.innerHTML = items.length ? items.map(item => {
        if (item.status === 'matched') {
            return `<tr>
                <td>${item.statement_date}</td><td>${escapeHtml(item.branch_name)}</td>
                <td>${escapeHtml(item.description)}</td><td>${money(item.amount)}</td>
                <td><span class="badge badge-synced">Matched</span></td>
                <td><button type="button" class="secondary" onclick="window.unmatchItem('${item.id}')">Unmatch</button></td>
            </tr>`;
        }
        const candidates = candidatesFor(item, entries);
        const picker = candidates.length
            ? `<select id="pick-${item.id}">${candidates.map(c => `<option value="${c.journal_line_id}">${c.document_date} ${escapeHtml(c.document_type)} ${money(c.amount)}</option>`).join('')}</select>
               <button type="button" onclick="window.matchItem('${item.id}')">Match</button>`
            : '<span class="muted">No candidate with this amount</span>';
        return `<tr>
            <td>${item.statement_date}</td><td>${escapeHtml(item.branch_name)}</td>
            <td>${escapeHtml(item.description)}</td><td>${money(item.amount)}</td>
            <td><span class="badge badge-pending">Outstanding</span></td>
            <td>${picker}</td>
        </tr>`;
    }).join('') : '<tr class="empty-row"><td colspan="6">No statement items in range.</td></tr>';

    entriesTbody.innerHTML = entries.length ? entries.map(e => `<tr>
        <td>${e.document_date}</td><td>${escapeHtml(e.branch_name)}</td><td>${escapeHtml(e.document_type)}</td>
        <td>${escapeHtml(e.description)}</td><td>${money(e.amount)}</td>
        <td><span class="badge ${e.matched ? 'badge-synced' : 'badge-pending'}">${e.matched ? 'Matched' : 'Outstanding'}</span></td>
    </tr>`).join('') : '<tr class="empty-row"><td colspan="6">No ledger entries on this account in range.</td></tr>';
}

window.matchItem = async (itemId) => {
    const picker = document.getElementById(`pick-${itemId}`);
    const resultEl = document.getElementById('action-result');
    const { status, body } = await apiFetch(`v1/reconciliation/items/${itemId}/match`, {
        method: 'POST', body: JSON.stringify({ journal_line_id: picker.value }),
    });
    resultEl.innerHTML = status === 200
        ? '<div class="status">Matched.</div>'
        : `<div class="errors">Could not match (HTTP ${status}): ${body.error_code || 'unknown error'}</div>`;
    loadOverview();
};

window.unmatchItem = async (itemId) => {
    const resultEl = document.getElementById('action-result');
    const { status, body } = await apiFetch(`v1/reconciliation/items/${itemId}/unmatch`, { method: 'POST', body: '{}' });
    resultEl.innerHTML = status === 200
        ? '<div class="status">Unmatched.</div>'
        : `<div class="errors">Could not unmatch (HTTP ${status}): ${body.error_code || 'unknown error'}</div>`;
    loadOverview();
};

document.getElementById('auto-match-btn').addEventListener('click', async () => {
    const accountId = document.getElementById('account-filter').value;
    const resultEl = document.getElementById('action-result');
    if (!accountId) { resultEl.innerHTML = '<div class="errors">Select an account first.</div>'; return; }
    const { status, body } = await apiFetch('v1/reconciliation/auto-match', {
        method: 'POST',
        body: JSON.stringify({ account_id: accountId, from: document.getElementById('from-filter').value, to: document.getElementById('to-filter').value }),
    });
    resultEl.innerHTML = status === 200
        ? `<div class="status">Auto-matched ${body.matched} pair(s).</div>`
        : `<div class="errors">Auto-match failed (HTTP ${status}): ${body.error_code || 'unknown error'}</div>`;
    loadOverview();
});

document.getElementById('add-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('add-result');
    const payload = {
        id: crypto.randomUUID(),
        branch_id: document.getElementById('item_branch_id').value,
        account_id: document.getElementById('account-filter').value,
        statement_date: document.getElementById('item_date').value,
        description: document.getElementById('item_description').value,
        amount: document.getElementById('item_amount').value,
        external_reference: document.getElementById('item_reference').value,
    };
    if (!payload.account_id) {
        resultEl.innerHTML = '<div class="errors">Select an account above first.</div>';
        return;
    }
    const { status, body } = await apiFetch('v1/reconciliation/items', { method: 'POST', body: JSON.stringify(payload) });
    if (status === 201) {
        resultEl.innerHTML = '<div class="status">Statement item added.</div>';
        e.target.reset();
        document.getElementById('item_date').value = today;
        loadOverview();
    } else {
        resultEl.innerHTML = `<div class="errors">Rejected (HTTP ${status}): ${body.error_code || 'unknown error'}</div>`;
    }
});

document.getElementById('account-filter').addEventListener('change', loadOverview);
document.getElementById('from-filter').addEventListener('change', loadOverview);
document.getElementById('to-filter').addEventListener('change', loadOverview);
document.getElementById('refresh-btn').addEventListener('click', loadOverview);
populateAccounts();
</script>
@endsection
