@extends('layouts.app')
@section('title', 'Sync centre · BranchLedger')
@section('body')
<h1>Sync centre</h1>
<p class="page-subtitle">
    Automatic synchronisation runs while this app is open. Use Sync now after reconnecting if you don't want to wait.
    Background processing is not guaranteed once the browser is closed.
</p>

<div class="panel">
    <div class="panel-head">
        <h2>Status</h2>
        <button id="sync-now-btn" type="button">Sync now</button>
    </div>
    <div id="last-sync" class="muted"></div>
    <div class="grid grid-stats" style="margin-top:14px;">
        <div class="stat-tile"><div class="label">Pending</div><div class="value" id="count-pending">0</div></div>
        <div class="stat-tile"><div class="label">Syncing</div><div class="value" id="count-syncing">0</div></div>
        <div class="stat-tile"><div class="label">Synced (recent)</div><div class="value" id="count-synced">0</div></div>
        <div class="stat-tile"><div class="label">Needs attention</div><div class="value" id="count-failed">0</div></div>
    </div>
</div>

<div class="panel">
    <h2>Status meanings</h2>
    <table>
        <thead><tr><th>Status</th><th>Meaning</th><th>Action</th></tr></thead>
        <tbody>
            <tr><td><span class="badge badge-pending">Pending</span></td><td>Saved locally and awaiting upload</td><td>Reconnect and sync runs automatically, or use Sync now</td></tr>
            <tr><td><span class="badge badge-syncing">Syncing</span></td><td>Upload in progress</td><td>Keep this tab open and avoid re-entry</td></tr>
            <tr><td><span class="badge badge-synced">Synced</span></td><td>Central system acknowledged this record</td><td>Check its separate approval/posting state in the relevant screen</td></tr>
            <tr><td><span class="badge badge-failed">Failed</span></td><td>Upload didn't complete (connectivity/server)</td><td>Retried automatically; resolve and escalate if it persists</td></tr>
            <tr><td><span class="badge badge-rejected">Rejected</span></td><td>The server durably declined this operation</td><td>Open the reason below; correct and re-enter if needed</td></tr>
        </tbody>
    </table>
</div>

<div class="panel">
    <h2>Outbox</h2>
    <table id="outbox-table">
        <thead><tr><th>Created</th><th>Type</th><th>Status</th><th>Detail</th><th></th></tr></thead>
        <tbody></tbody>
    </table>
</div>

<div class="panel">
    <h2>Recent changes received</h2>
    <p class="muted">Pulled via <code>GET /api/v1/sync/pull</code>.</p>
    <table id="changes-table">
        <thead><tr><th>Record type</th><th>Record ID</th><th>Version</th><th>Kind</th></tr></thead>
        <tbody></tbody>
    </table>
</div>

<script type="module">
    import { syncNow, outboxSummary, recentChanges } from '/js/offline/sync.js';
    import { allOutboxRecords, deleteOutboxRecord } from '/js/offline/db.js';

    function escapeHtml(s) { return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

    async function render() {
        const { counts, lastSyncAt } = await outboxSummary();
        document.getElementById('last-sync').textContent = lastSyncAt
            ? `Last successful sync: ${new Date(lastSyncAt).toLocaleString()}`
            : 'No successful sync yet on this device.';
        document.getElementById('count-pending').textContent = counts.pending;
        document.getElementById('count-syncing').textContent = counts.syncing;
        document.getElementById('count-synced').textContent = counts.synced;
        document.getElementById('count-failed').textContent = counts.failed + counts.rejected;

        const all = (await allOutboxRecords()).sort((a, b) => new Date(b.created_at) - new Date(a.created_at));
        const tbody = document.querySelector('#outbox-table tbody');
        if (!all.length) {
            tbody.innerHTML = '<tr class="empty-row"><td colspan="5">No local records yet. Record a sale, expense, purchase or transfer to see it here.</td></tr>';
        } else {
            tbody.innerHTML = all.slice(0, 50).map(r => `<tr>
                <td>${new Date(r.created_at).toLocaleString()}</td>
                <td>${escapeHtml(r.document_type)}</td>
                <td><span class="badge badge-${r.status}">${r.status}</span></td>
                <td class="muted">${r.status === 'synced' ? 'Server status: ' + escapeHtml(r.server_status || 'posted') : (r.error_code ? escapeHtml(r.error_code) : '')}</td>
                <td>${r.status === 'rejected' ? `<button type="button" class="secondary" data-dismiss="${r.operation_id}">Dismiss</button>` : ''}</td>
            </tr>`).join('');
        }

        const changes = await recentChanges(20);
        const changesTbody = document.querySelector('#changes-table tbody');
        changesTbody.innerHTML = changes.length
            ? changes.map(c => `<tr><td>${escapeHtml(c.RecordType)}</td><td class="muted">${c.RecordID}</td><td>${c.RecordVersion}</td><td>${escapeHtml(c.ChangeKind)}</td></tr>`).join('')
            : '<tr class="empty-row"><td colspan="4">No changes pulled yet.</td></tr>';
    }

    document.getElementById('sync-now-btn').addEventListener('click', async () => {
        const btn = document.getElementById('sync-now-btn');
        btn.disabled = true; btn.textContent = 'Syncing…';
        await syncNow();
        btn.disabled = false; btn.textContent = 'Sync now';
        render();
    });

    document.querySelector('#outbox-table').addEventListener('click', async (e) => {
        const opId = e.target.getAttribute('data-dismiss');
        if (!opId) return;
        // Dismissing only removes the local outbox row — the server's own
        // durable rejection record (and any genuinely posted duplicate
        // attempt) is untouched; this never retries or undoes anything.
        await deleteOutboxRecord(opId);
        render();
    });

    render();
    setInterval(render, 4000);
</script>
@endsection
