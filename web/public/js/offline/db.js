// IndexedDB wrapper for the browser-side outbox and change feed described in
// BranchLedger_Architecture.pdf ("Browser PWA: Cached forms, chart
// interaction, IndexedDB and outbox") and BranchLedger_System_Documentation
// section 5.1 ("Create a UUID for each operation ... and atomically save the
// record with an outbox item").
//
// Lives under web/public/js/offline (served as a plain static file) rather
// than the architecture's proposed web/resources/js/offline: this project's
// Vite/Tailwind pipeline is installed but not wired into the layout (no
// @vite directive, no npm process in ops/deploy/dev-up.sh), and every other
// page here is a plain unbundled <script>. Adding a required `npm run dev`
// process to view a page was judged worse than this one path deviation;
// resources/js/app.js stays the unused Vite entrypoint it already was.

const DB_NAME = 'branchledger';
const DB_VERSION = 1;
export const STORE_OUTBOX = 'outbox';
export const STORE_META = 'meta';
export const STORE_CHANGES = 'changes';

let dbPromise = null;

export function openDB() {
    if (dbPromise) return dbPromise;
    dbPromise = new Promise((resolve, reject) => {
        const req = indexedDB.open(DB_NAME, DB_VERSION);
        req.onupgradeneeded = (e) => {
            const db = e.target.result;
            if (!db.objectStoreNames.contains(STORE_OUTBOX)) {
                const os = db.createObjectStore(STORE_OUTBOX, { keyPath: 'operation_id' });
                os.createIndex('status', 'status');
                os.createIndex('created_at', 'created_at');
            }
            if (!db.objectStoreNames.contains(STORE_META)) {
                db.createObjectStore(STORE_META, { keyPath: 'key' });
            }
            if (!db.objectStoreNames.contains(STORE_CHANGES)) {
                // keyPath matches Go's default (no json tags) capitalised
                // field names on sync.Change: Cursor, BranchID, RecordType,
                // RecordID, RecordVersion, ChangeKind.
                db.createObjectStore(STORE_CHANGES, { keyPath: 'Cursor' });
            }
        };
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error);
    });
    return dbPromise;
}

function tx(db, store, mode) {
    return db.transaction(store, mode).objectStore(store);
}

function reqToPromise(req) {
    return new Promise((resolve, reject) => {
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error);
    });
}

// --- Outbox -------------------------------------------------------------

export async function putOutboxRecord(record) {
    const db = await openDB();
    return reqToPromise(tx(db, STORE_OUTBOX, 'readwrite').put(record));
}

export async function getOutboxRecord(operationId) {
    const db = await openDB();
    return reqToPromise(tx(db, STORE_OUTBOX, 'readonly').get(operationId));
}

export async function allOutboxRecords() {
    const db = await openDB();
    return reqToPromise(tx(db, STORE_OUTBOX, 'readonly').getAll());
}

export async function outboxRecordsByStatus(statuses) {
    const all = await allOutboxRecords();
    return all.filter(r => statuses.includes(r.status));
}

export async function deleteOutboxRecord(operationId) {
    const db = await openDB();
    return reqToPromise(tx(db, STORE_OUTBOX, 'readwrite').delete(operationId));
}

// --- Meta (sync cursor, timestamps) ---------------------------------------

export async function getMeta(key, fallback) {
    const db = await openDB();
    const rec = await reqToPromise(tx(db, STORE_META, 'readonly').get(key));
    return rec ? rec.value : fallback;
}

export async function setMeta(key, value) {
    const db = await openDB();
    return reqToPromise(tx(db, STORE_META, 'readwrite').put({ key, value }));
}

// --- Change feed (pulled from GET /api/v1/sync/pull) -----------------------

export async function appendChanges(changes) {
    const db = await openDB();
    const store = tx(db, STORE_CHANGES, 'readwrite');
    for (const c of changes) {
        store.put(c);
    }
    return new Promise((resolve, reject) => {
        store.transaction.oncomplete = resolve;
        store.transaction.onerror = () => reject(store.transaction.error);
    });
}

export async function recentChanges(limit) {
    const db = await openDB();
    const all = await reqToPromise(tx(db, STORE_CHANGES, 'readonly').getAll());
    return all.sort((a, b) => b.Cursor - a.Cursor).slice(0, limit || 20);
}
