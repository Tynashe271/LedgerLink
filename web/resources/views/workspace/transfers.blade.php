@extends('layouts.app')
@section('title', 'Transfers · BranchLedger')
@section('body')
<h1>Transfers</h1>
<p class="page-subtitle">
    Internal cash transfers between branches. An unreceived transfer remains in transit and is never
    counted as a company sale (<code>POST /api/v1/transactions</code>, <code>POST /api/v1/transfers/{id}/receive</code>).
</p>

<div class="panel">
    <h2>Dispatch a cash transfer</h2>
    <div id="dispatch-result"></div>
    <form id="dispatch-form">
        <label for="sender_branch_id">Sending branch</label>
        <select id="sender_branch_id" required>
            @foreach ($branches as $branch)
                <option value="{{ $branch->id }}">{{ $branch->name }}{{ $branch->is_main_branch ? ' (main)' : '' }}</option>
            @endforeach
        </select>

        <label for="receiver_branch_id">Receiving branch</label>
        <select id="receiver_branch_id" required>
            @foreach ($branches as $branch)
                <option value="{{ $branch->id }}">{{ $branch->name }}{{ $branch->is_main_branch ? ' (main)' : '' }}</option>
            @endforeach
        </select>

        <label for="amount">Amount (USD)</label>
        <input id="amount" type="number" step="0.01" value="50.00" required>

        <label for="explanation">Explanation</label>
        <input id="explanation" type="text" placeholder="e.g. Float top-up">

        <button type="submit">Dispatch</button>
    </form>
</div>

<div class="panel">
    <div class="panel-head">
        <h2>In transit</h2>
        <button id="refresh-btn" type="button" class="secondary">Refresh</button>
    </div>
    <table id="transfers-table">
        <thead><tr><th>Dispatched</th><th>From</th><th>To</th><th>Amount</th><th>Received</th><th>Status</th><th>Receive</th></tr></thead>
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

document.getElementById('dispatch-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('dispatch-result');

    const senderId = document.getElementById('sender_branch_id').value;
    const receiverId = document.getElementById('receiver_branch_id').value;
    if (senderId === receiverId) {
        resultEl.innerHTML = '<div class="errors">Sending and receiving branch must differ.</div>';
        return;
    }

    const payload = {
        operation_id: uuidv4(),
        branch_id: senderId,
        document_type: 'transfer',
        document_date: new Date().toISOString().slice(0, 10),
        currency_code: 'USD',
        exchange_rate: '1',
        receiver_branch_id: receiverId,
        transfer_type: 'cash',
        explanation: document.getElementById('explanation').value,
        lines: [{ description: 'Cash transfer', quantity: '1', unit_price: document.getElementById('amount').value }],
    };

    await queueOperation(payload);
    resultEl.innerHTML = '<div class="status">Saved locally. Syncing automatically.</div>';
    e.target.reset();
    loadTransfers();
});

// Receiving a transfer requires confirming with the central record of what
// was actually dispatched — unlike dispatch, this is not queued offline
// (receive_amount is checked against the server's current remaining
// balance, which an offline device cannot know is still current).
async function receiveTransfer(transferId, receiverBranchId, remaining) {
    if (!navigator.onLine) { alert('Receiving a transfer requires a connection, so the amount can be checked against the current server record.'); return; }
    const amount = prompt(`Amount to confirm received (remaining ${remaining}):`, remaining);
    if (!amount) return;
    const { status, body } = await apiFetch(`v1/transfers/${transferId}/receive`, {
        method: 'POST',
        body: JSON.stringify({ operation_id: uuidv4(), branch_id: receiverBranchId, received_amount: amount }),
    });
    if (status === 201 || status === 200) {
        loadTransfers();
    } else {
        alert(`Rejected (HTTP ${status}): ${body.error_code || 'unknown error'}`);
    }
}
window.receiveTransfer = receiveTransfer;

async function loadTransfers() {
    const { status, body } = await apiFetch('v1/transfers');
    const serverRows = (status === 200 && Array.isArray(body)) ? body : [];

    const localDispatches = (await outboxRecordsByStatus(['pending', 'syncing', 'failed', 'rejected']))
        .filter(r => r.document_type === 'transfer')
        .map(r => ({
            dispatched_at: r.payload.document_date, sender_branch_name: '(this device)',
            receiver_branch_name: '', amount: Number(r.payload.lines[0].unit_price), received_amount: 0,
            status: r.status, local: true,
        }));

    const tbody = document.querySelector('#transfers-table tbody');
    const rows = [...localDispatches, ...serverRows];
    if (!rows.length) {
        tbody.innerHTML = '<tr class="empty-row"><td colspan="7">No transfers currently in transit.</td></tr>';
        return;
    }
    tbody.innerHTML = rows.map(t => `<tr>
        <td>${new Date(t.dispatched_at).toLocaleDateString()}</td>
        <td>${escapeHtml(t.sender_branch_name)}</td>
        <td>${escapeHtml(t.receiver_branch_name)}</td>
        <td>${money(t.amount)}</td>
        <td>${money(t.received_amount)}</td>
        <td><span class="badge badge-${t.status}">${t.status.replace('_', ' ')}</span></td>
        <td>${t.local ? '' : `<button type="button" class="secondary" onclick="receiveTransfer('${t.id}', '${t.receiver_branch_id}', '${t.remaining_amount}')">Receive</button>`}</td>
    </tr>`).join('');
}

document.getElementById('refresh-btn').addEventListener('click', loadTransfers);
loadTransfers();
setInterval(loadTransfers, 5000);
</script>
@endsection
