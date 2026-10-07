// The browser half of the synchronisation sequence in
// BranchLedger_Architecture.pdf: "1. Validate the local form and save
// transaction plus outbox atomically ... 3. Send pending operations with
// stable IDs ... 6. Browser records acknowledgement and pulls server
// changes. 7. Apply change batch and cursor atomically."
//
// queueOperation() is step 1 (called by each form instead of posting
// directly). syncNow() is steps 2-7: push whatever is pending, then pull.
// Never implements "exactly once" — operation_id is stable across retries,
// and the server is the idempotency authority (accounting.Service.Post).

import {
    putOutboxRecord, outboxRecordsByStatus, allOutboxRecords, deleteOutboxRecord,
    getMeta, setMeta, appendChanges, recentChanges,
} from './db.js';

const MAX_BATCH_SIZE = 20; // well under the server's MaxBatchSize (100); keeps one push request small on a slow reconnect.
const SYNCED_RETENTION_MS = 24 * 60 * 60 * 1000; // prune synced rows a day later so "recent" views stay useful without growing forever.

function csrfToken() {
    return document.querySelector('meta[name="csrf-token"]').content;
}

async function apiFetch(path, options = {}) {
    const headers = Object.assign({
        'Content-Type': 'application/json', 'X-CSRF-TOKEN': csrfToken(), 'Accept': 'application/json',
    }, options.headers || {});
    const res = await fetch('/api/' + path, Object.assign({}, options, { headers, credentials: 'same-origin' }));
    const body = await res.json().catch(() => ({}));
    return { status: res.status, body };
}

// queueOperation saves a transaction-shaped request (the same body
// POST /api/v1/transactions accepts) to the local outbox as "pending" and
// returns immediately — the User Manual's "A local confirmation means the
// record is on this device. It does not mean head office has received it."
// It then makes a best-effort immediate sync attempt without waiting for it,
// so an online user still sees the result quickly.
export async function queueOperation(payload) {
    const record = {
        operation_id: payload.operation_id,
        branch_id: payload.branch_id,
        document_type: payload.document_type,
        payload,
        status: 'pending',
        created_at: new Date().toISOString(),
        last_attempt_at: null,
        attempts: 0,
        error_code: null,
        server_transaction_id: null,
        server_status: null,
    };
    await putOutboxRecord(record);
    syncNow().catch(() => {}); // best-effort; failures stay visible in the outbox itself
    return record;
}

let syncing = false;

// syncNow pushes pending/failed outbox items, then pulls authorised changes.
// A concurrent call while one is already running is a no-op — the caller's
// own best-effort trigger (queueOperation, reconnect, the interval below)
// doesn't need to queue up, it'll run again on the next natural trigger.
export async function syncNow() {
    if (syncing) return { skipped: true };
    if (!navigator.onLine) return { offline: true };
    syncing = true;
    try {
        const pushResult = await pushPending();
        const pullResult = await pullChanges();
        await refreshReferenceData();
        await setMeta('last_sync_at', new Date().toISOString());
        await pruneOldSynced();
        return { pushResult, pullResult };
    } finally {
        syncing = false;
    }
}

// --- Reference data cache (products, customers) -----------------------------
// User Manual: "Before going offline, confirm ... required forms and product
// lists are available." A product/customer picker that only works online
// defeats the point of the offline outbox for any sale that references one,
// so the last-fetched lists are cached and read back when offline.

async function refreshReferenceData() {
    const [products, customers] = await Promise.all([
        apiFetch('v1/products').then(r => (r.status === 200 && Array.isArray(r.body)) ? r.body : null).catch(() => null),
        apiFetch('v1/customers').then(r => (r.status === 200 && Array.isArray(r.body)) ? r.body : null).catch(() => null),
    ]);
    if (products) await setMeta('products_cache', products);
    if (customers) await setMeta('customers_cache', customers);
}

export async function getCachedProducts() {
    return getMeta('products_cache', []);
}

export async function getCachedCustomers() {
    return getMeta('customers_cache', []);
}

async function pushPending() {
    const items = await outboxRecordsByStatus(['pending', 'failed']);
    if (!items.length) return { pushed: 0 };

    const batch = items.slice(0, MAX_BATCH_SIZE);
    for (const r of batch) {
        r.status = 'syncing';
        r.last_attempt_at = new Date().toISOString();
        r.attempts += 1;
        await putOutboxRecord(r);
    }

    const { status, body } = await apiFetch('v1/sync/push', {
        method: 'POST',
        body: JSON.stringify({ items: batch.map(r => r.payload) }),
    });

    if (status !== 200 || !body.Items) {
        // Transport/server failure: every item in this attempt goes back to
        // "failed" (not lost — operation_id is unchanged, the next sync
        // retries the same idempotent request) with a transport error code.
        for (const r of batch) {
            r.status = 'failed';
            r.error_code = 'sync_request_failed';
            await putOutboxRecord(r);
        }
        return { pushed: 0, transportError: true };
    }

    const byOpId = new Map(batch.map(r => [r.operation_id, r]));
    for (const item of body.Items) {
        const r = byOpId.get(item.OperationID);
        if (!r) continue;
        if (item.Accepted) {
            r.status = 'synced';
            r.server_transaction_id = item.TransactionID;
            r.server_status = item.Status;
            r.error_code = null;
        } else if (item.ErrorCode === 'conflict') {
            // A genuine conflict (same operation ID, different content) is
            // not retried automatically — retrying would just get the same
            // conflict every time. It needs a human look.
            r.status = 'rejected';
            r.error_code = 'conflict';
        } else {
            // A durable business rejection (period closed, insufficient
            // stock, ...). The server already recorded it; retrying the same
            // operation_id would just replay the same rejection, so this
            // also stops here rather than looping.
            r.status = 'rejected';
            r.error_code = item.ErrorCode || 'rejected';
        }
        await putOutboxRecord(r);
    }
    return { pushed: batch.length };
}

async function pullChanges() {
    const cursor = await getMeta('cursor', '');
    const { status, body } = await apiFetch(`v1/sync/pull${cursor ? '?cursor=' + encodeURIComponent(cursor) : ''}`);
    if (status !== 200) return { pulled: 0 };
    if (body.Changes && body.Changes.length) {
        await appendChanges(body.Changes);
    }
    if (body.NextCursor) {
        await setMeta('cursor', body.NextCursor);
    }
    return { pulled: (body.Changes || []).length };
}

async function pruneOldSynced() {
    const all = await allOutboxRecords();
    const cutoff = Date.now() - SYNCED_RETENTION_MS;
    for (const r of all) {
        if (r.status === 'synced' && new Date(r.last_attempt_at || r.created_at).getTime() < cutoff) {
            await deleteOutboxRecord(r.operation_id);
        }
    }
}

// --- Status summary for the topbar badge and Sync centre -------------------

export async function outboxSummary() {
    const all = await allOutboxRecords();
    const counts = { pending: 0, syncing: 0, synced: 0, failed: 0, rejected: 0 };
    for (const r of all) counts[r.status] = (counts[r.status] || 0) + 1;
    return {
        counts,
        lastSyncAt: await getMeta('last_sync_at', null),
        failedAndRejected: all.filter(r => r.status === 'failed' || r.status === 'rejected'),
    };
}

export { recentChanges };

// --- Automatic sync ----------------------------------------------------------
// "Automatic synchronisation runs while the app is active" (User Manual) —
// never relies on the Background Sync API, which the architecture explicitly
// calls an unguaranteed enhancement, not the primary mechanism.

export function startAutoSync() {
    window.addEventListener('online', () => syncNow().catch(() => {}));
    setInterval(() => {
        if (document.visibilityState === 'visible') syncNow().catch(() => {});
    }, 30000);
    syncNow().catch(() => {});
}
