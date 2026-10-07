@extends('layouts.app')
@section('title', 'Daily close · BranchLedger')
@section('body')
<h1>Daily close</h1>
<p class="page-subtitle">
    Cash differences remain visible and do not disappear through editing totals (<code>POST /api/v1/closes</code>).
</p>

<div class="panel">
    <h2>Submit today's close</h2>
    <div id="result"></div>
    <form id="close-form">
        <label for="branch_id">Branch</label>
        <select id="branch_id" required>
            @foreach ($branches as $branch)
                <option value="{{ $branch->id }}">{{ $branch->name }}{{ $branch->is_main_branch ? ' (main)' : '' }}</option>
            @endforeach
        </select>

        <label for="close_date">Close date</label>
        <input id="close_date" type="date" required>

        <div class="grid grid-2">
            <div>
                <label for="opening_float">Opening float</label>
                <input id="opening_float" type="number" step="0.01" value="100.00" required>
            </div>
            <div>
                <label for="currency_code">Currency</label>
                <input id="currency_code" type="text" value="USD" maxlength="3" required>
            </div>
        </div>

        <div class="grid grid-2">
            <div>
                <label for="expected_cash">Expected cash</label>
                <input id="expected_cash" type="number" step="0.01" required>
                <p class="muted" style="margin-top:4px;">Opening float plus today's cash receipts and transfers in, less cash payments, refunds and transfers out. Server-side expected-cash calculation from posted movements is not yet wired up — enter it from today's records.</p>
            </div>
            <div>
                <label for="counted_cash">Counted cash</label>
                <input id="counted_cash" type="number" step="0.01" required>
            </div>
        </div>

        <label for="explanation">Explain any difference</label>
        <textarea id="explanation" rows="2"></textarea>

        <button type="submit">Submit close</button>
    </form>
</div>

<script>
const csrfToken = document.querySelector('meta[name="csrf-token"]').content;
function uuidv4() { return crypto.randomUUID(); }
function money(v) { const n = Number(v); return (n < 0 ? '-$' + Math.abs(n).toFixed(2) : '$' + n.toFixed(2)); }

document.getElementById('close_date').value = new Date().toISOString().slice(0, 10);

async function apiFetch(path, options = {}) {
    const headers = Object.assign({ 'Content-Type': 'application/json', 'X-CSRF-TOKEN': csrfToken, 'Accept': 'application/json' }, options.headers || {});
    const res = await fetch('/api/' + path, Object.assign({}, options, { headers, credentials: 'same-origin' }));
    const body = await res.json().catch(() => ({}));
    return { status: res.status, body };
}

document.getElementById('close-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('result');
    resultEl.innerHTML = '';

    const payload = {
        operation_id: uuidv4(),
        branch_id: document.getElementById('branch_id').value,
        close_date: document.getElementById('close_date').value,
        currency_code: document.getElementById('currency_code').value.toUpperCase(),
        opening_float: document.getElementById('opening_float').value,
        expected_cash: document.getElementById('expected_cash').value,
        counted_cash: document.getElementById('counted_cash').value,
        explanation: document.getElementById('explanation').value,
    };

    const { status, body } = await apiFetch('v1/closes', { method: 'POST', body: JSON.stringify(payload) });

    if ((status === 201 || status === 200) && body.accepted) {
        const discrepancy = Number(body.discrepancy);
        const cls = discrepancy === 0 ? 'status' : 'alert-warn';
        resultEl.innerHTML = `<div class="${cls}">Close submitted. Discrepancy: ${money(body.discrepancy)}${discrepancy !== 0 ? ' &mdash; remains visible until reviewed.' : '.'}</div>`;
    } else {
        resultEl.innerHTML = `<div class="errors">Rejected (HTTP ${status}): ${body.error_code || 'unknown error'}</div>`;
    }
});
</script>
@endsection
