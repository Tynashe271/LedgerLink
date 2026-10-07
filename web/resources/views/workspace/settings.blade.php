@extends('layouts.app')
@section('title', 'Settings · BranchLedger')
@section('body')
<h1>Settings</h1>
<p class="page-subtitle">
    Company, accounts, devices and limits (<code>GET/PUT /api/v1/settings/company</code>,
    <code>GET/POST /api/v1/settings/branches</code>, <code>/accounts</code>, <code>/devices</code>,
    <code>/memberships</code>, <code>/users</code>).
    Every change here is administrative, not a posting, and is itself an authorised, auditable action
    (see the <a href="{{ route('workspace.audit-log') }}">audit log</a>).
</p>

<div class="panel">
    <h2>Company</h2>
    <div id="company-result"></div>
    <form id="company-form">
        <div class="grid grid-2">
            <div>
                <label for="company_name">Name</label>
                <input id="company_name" type="text" required>
            </div>
            <div>
                <label for="company_category">Primary category</label>
                <input id="company_category" type="text" required>
            </div>
        </div>
        <div class="grid grid-2">
            <div>
                <label for="company_timezone">Timezone</label>
                <input id="company_timezone" type="text" required>
            </div>
            <div>
                <label for="company_fy_start">Financial year start</label>
                <input id="company_fy_start" type="date" required>
            </div>
        </div>
        <p class="muted">Reporting currency (<span id="company_currency"></span>) and status (<span id="company_status"></span>) are not editable here &mdash; changing a reporting currency after journals have posted needs a reviewed conversion, not a field edit.</p>
        <button type="submit">Save company profile</button>
    </form>
</div>

<div class="panel">
    <h2>Branches</h2>
    <div id="branch-result"></div>
    <form id="branch-form">
        <div class="grid grid-2">
            <div>
                <label for="branch_name">Name</label>
                <input id="branch_name" type="text" required>
            </div>
            <div>
                <label for="branch_code">Code</label>
                <input id="branch_code" type="text" placeholder="e.g. NORTH" required>
            </div>
        </div>
        <div class="grid grid-2">
            <div>
                <label for="branch_category">Category</label>
                <input id="branch_category" type="text" placeholder="e.g. retail_wholesale" required>
            </div>
            <div>
                <label for="branch_is_main">&nbsp;</label>
                <label style="display:flex; align-items:center; gap:6px; font-weight:normal;">
                    <input id="branch_is_main" type="checkbox" style="width:auto;"> Main branch
                </label>
            </div>
        </div>
        <p class="muted">Adding a new branch only &mdash; exactly one branch may be the main branch, and a company's branches can't be edited here once created.</p>
        <button type="submit">Add branch</button>
    </form>
    <table id="branches-table">
        <thead><tr><th>Name</th><th>Code</th><th>Category</th><th>Main</th><th>Status</th></tr></thead>
        <tbody></tbody>
    </table>
</div>

<div class="panel">
    <h2>Chart of accounts</h2>
    <div id="account-result"></div>
    <form id="account-form">
        <div class="grid grid-2">
            <div>
                <label for="account_code">Code</label>
                <input id="account_code" type="text" placeholder="e.g. 7000" required>
            </div>
            <div>
                <label for="account_name">Name</label>
                <input id="account_name" type="text" placeholder="e.g. Bank Charges" required>
            </div>
        </div>
        <div class="grid grid-2">
            <div>
                <label for="account_type">Type</label>
                <select id="account_type">
                    <option value="asset">Asset</option>
                    <option value="liability">Liability</option>
                    <option value="equity">Equity</option>
                    <option value="income">Income</option>
                    <option value="expense">Expense</option>
                </select>
            </div>
            <div>
                <label for="account_cash_like">&nbsp;</label>
                <label style="display:flex; align-items:center; gap:6px; font-weight:normal;">
                    <input id="account_cash_like" type="checkbox" style="width:auto;"> Cash-like (eligible for reconciliation)
                </label>
            </div>
        </div>
        <p class="muted">Adding a new account only &mdash; an existing account's code or name cannot be changed here once it may already be posted against.</p>
        <button type="submit">Add account</button>
    </form>
    <table id="accounts-table">
        <thead><tr><th>Code</th><th>Name</th><th>Type</th><th>Cash-like</th><th>Active</th></tr></thead>
        <tbody></tbody>
    </table>
</div>

<div class="panel">
    <h2>Devices</h2>
    <div id="device-result"></div>
    <form id="device-form">
        <div class="grid grid-2">
            <div>
                <label for="device_branch_id">Branch</label>
                <select id="device_branch_id" required></select>
            </div>
            <div>
                <label for="device_user_id">User</label>
                <select id="device_user_id" required></select>
            </div>
        </div>
        <div class="grid grid-2">
            <div>
                <label for="device_label">Label</label>
                <input id="device_label" type="text" placeholder="e.g. Till 1 tablet" required>
            </div>
            <div>
                <label for="device_lease_days">Lease (days)</label>
                <input id="device_lease_days" type="number" value="7" min="1" required>
            </div>
        </div>
        <button type="submit">Enroll device</button>
    </form>
    <table id="devices-table">
        <thead><tr><th>Branch</th><th>User</th><th>Label</th><th>Offline writer</th><th>Lease expires</th><th>Last sync</th><th>Status</th><th>Actions</th></tr></thead>
        <tbody></tbody>
    </table>
</div>

<div class="panel">
    <h2>Add a user</h2>
    <div id="user-result"></div>
    <form id="user-form">
        <div class="grid grid-2">
            <div>
                <label for="user_email">Email</label>
                <input id="user_email" type="email" required>
            </div>
            <div>
                <label for="user_full_name">Full name</label>
                <input id="user_full_name" type="text" required>
            </div>
        </div>
        <div class="grid grid-2">
            <div>
                <label for="user_role">Role</label>
                <select id="user_role" required>
                    <option value="owner">Owner</option>
                    <option value="general_manager">General manager</option>
                    <option value="accountant">Accountant</option>
                    <option value="branch_manager">Branch manager</option>
                    <option value="staff" selected>Staff</option>
                </select>
            </div>
            <div>
                <label for="user_approval_limit">Approval limit</label>
                <input id="user_approval_limit" type="number" step="0.01" placeholder="Unlimited">
            </div>
        </div>
        <label for="user_branch_scope">Branches (none selected = all branches)</label>
        <select id="user_branch_scope" multiple style="height:80px;"></select>
        <p class="muted" style="margin-top:4px;">
            The new user has no usable password yet &mdash; they request their own via "Forgot your password?" on the login page.
        </p>
        <button type="submit">Add user</button>
    </form>
</div>

<div class="panel">
    <h2>Users and approval limits</h2>
    <div id="membership-result"></div>
    <table id="memberships-table">
        <thead><tr><th>User</th><th>Role</th><th>Branches</th><th>Approval limit</th><th>Action</th></tr></thead>
        <tbody></tbody>
    </table>
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
function rejected(resultEl, status, body) {
    resultEl.innerHTML = `<div class="errors">Rejected (HTTP ${status}): ${body.error_code || 'unknown error'}</div>`;
}

// --- Company -----------------------------------------------------------------

async function loadCompany() {
    const { status, body } = await apiFetch('v1/settings/company');
    if (status !== 200) { rejected(document.getElementById('company-result'), status, body); return; }
    document.getElementById('company_name').value = body.name;
    document.getElementById('company_category').value = body.primary_category;
    document.getElementById('company_timezone').value = body.timezone;
    document.getElementById('company_fy_start').value = body.financial_year_start;
    document.getElementById('company_currency').textContent = body.reporting_currency;
    document.getElementById('company_status').textContent = body.status;
}

document.getElementById('company-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('company-result');
    const payload = {
        name: document.getElementById('company_name').value,
        primary_category: document.getElementById('company_category').value,
        timezone: document.getElementById('company_timezone').value,
        financial_year_start: document.getElementById('company_fy_start').value,
    };
    const { status, body } = await apiFetch('v1/settings/company', { method: 'PUT', body: JSON.stringify(payload) });
    if (status === 200) { resultEl.innerHTML = '<div class="status">Saved.</div>'; } else { rejected(resultEl, status, body); }
});

// --- Branches --------------------------------------------------------------

let lastBranches = [];

async function loadBranches() {
    const { status, body } = await apiFetch('v1/settings/branches');
    const tbody = document.querySelector('#branches-table tbody');
    if (status !== 200) { tbody.innerHTML = '<tr class="empty-row"><td colspan="5">Could not load branches.</td></tr>'; return; }
    lastBranches = body;
    tbody.innerHTML = body.length ? body.map(b => `<tr>
        <td>${escapeHtml(b.name)}</td><td>${escapeHtml(b.code)}</td><td>${escapeHtml(b.category)}</td>
        <td>${b.is_main_branch ? 'Yes' : ''}</td><td>${escapeHtml(b.status)}</td>
    </tr>`).join('') : '<tr class="empty-row"><td colspan="5">No branches.</td></tr>';

    const branchOptions = body.map(b => `<option value="${b.id}">${escapeHtml(b.name)}${b.is_main_branch ? ' (main)' : ''}</option>`).join('');
    document.getElementById('device_branch_id').innerHTML = branchOptions;
    document.getElementById('user_branch_scope').innerHTML = branchOptions;
}

document.getElementById('branch-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('branch-result');
    const payload = {
        name: document.getElementById('branch_name').value,
        code: document.getElementById('branch_code').value,
        category: document.getElementById('branch_category').value,
        is_main_branch: document.getElementById('branch_is_main').checked,
    };
    const { status, body } = await apiFetch('v1/settings/branches', { method: 'POST', body: JSON.stringify(payload) });
    if (status === 201) {
        resultEl.innerHTML = '<div class="status">Branch added.</div>';
        e.target.reset();
        loadBranches();
    } else {
        rejected(resultEl, status, body);
    }
});

// --- Chart of accounts ---------------------------------------------------

async function loadAccounts() {
    const { status, body } = await apiFetch('v1/settings/accounts');
    const tbody = document.querySelector('#accounts-table tbody');
    if (status !== 200) { tbody.innerHTML = '<tr class="empty-row"><td colspan="5">Could not load accounts.</td></tr>'; return; }
    tbody.innerHTML = body.length ? body.map(a => `<tr>
        <td>${escapeHtml(a.code)}</td><td>${escapeHtml(a.name)}</td><td>${escapeHtml(a.account_type)}</td>
        <td>${a.is_cash_like ? 'Yes' : ''}</td><td>${a.is_active ? 'Yes' : 'No'}</td>
    </tr>`).join('') : '<tr class="empty-row"><td colspan="5">No accounts.</td></tr>';
}

document.getElementById('account-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('account-result');
    const payload = {
        code: document.getElementById('account_code').value,
        name: document.getElementById('account_name').value,
        account_type: document.getElementById('account_type').value,
        is_cash_like: document.getElementById('account_cash_like').checked,
    };
    const { status, body } = await apiFetch('v1/settings/accounts', { method: 'POST', body: JSON.stringify(payload) });
    if (status === 201) {
        resultEl.innerHTML = '<div class="status">Account added.</div>';
        e.target.reset();
        loadAccounts();
    } else {
        rejected(resultEl, status, body);
    }
});

// --- Devices -------------------------------------------------------------

async function loadDevices() {
    const { status, body } = await apiFetch('v1/settings/devices');
    const tbody = document.querySelector('#devices-table tbody');
    if (status !== 200) { tbody.innerHTML = '<tr class="empty-row"><td colspan="8">Could not load devices.</td></tr>'; return; }
    tbody.innerHTML = body.length ? body.map(d => `<tr>
        <td>${escapeHtml(d.branch_name)}</td><td>${escapeHtml(d.user_name)}</td><td>${escapeHtml(d.label)}</td>
        <td>${d.is_offline_writer ? '<span class="badge badge-synced">Offline writer</span>' : ''}</td>
        <td>${new Date(d.lease_expires_at).toLocaleString()}</td>
        <td>${d.last_sync_at ? new Date(d.last_sync_at).toLocaleString() : 'Never'}</td>
        <td>${d.revoked_at ? '<span class="badge badge-rejected">Revoked</span>' : '<span class="badge badge-synced">Active</span>'}</td>
        <td>${d.revoked_at ? '' : `
            ${d.is_offline_writer ? '' : `<button type="button" class="secondary" onclick="window.setOfflineWriter('${d.id}')">Set as offline writer</button>`}
            <button type="button" class="secondary" onclick="window.revokeDevice('${d.id}')">Revoke</button>
        `}</td>
    </tr>`).join('') : '<tr class="empty-row"><td colspan="8">No enrolled devices.</td></tr>';
}

document.getElementById('device-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('device-result');
    const payload = {
        branch_id: document.getElementById('device_branch_id').value,
        user_id: document.getElementById('device_user_id').value,
        label: document.getElementById('device_label').value,
        lease_days: Number(document.getElementById('device_lease_days').value),
    };
    const { status, body } = await apiFetch('v1/settings/devices', { method: 'POST', body: JSON.stringify(payload) });
    if (status === 201) {
        resultEl.innerHTML = '<div class="status">Device enrolled.</div>';
        e.target.reset();
        loadDevices();
    } else {
        rejected(resultEl, status, body);
    }
});

window.revokeDevice = async (id) => {
    const resultEl = document.getElementById('device-result');
    const { status, body } = await apiFetch(`v1/settings/devices/${id}/revoke`, { method: 'POST', body: '{}' });
    if (status === 200) { resultEl.innerHTML = '<div class="status">Device revoked.</div>'; } else { rejected(resultEl, status, body); }
    loadDevices();
};
window.setOfflineWriter = async (id) => {
    const resultEl = document.getElementById('device-result');
    const { status, body } = await apiFetch(`v1/settings/devices/${id}/set-offline-writer`, { method: 'POST', body: '{}' });
    if (status === 200) { resultEl.innerHTML = '<div class="status">Offline writer updated.</div>'; } else { rejected(resultEl, status, body); }
    loadDevices();
};

// --- Memberships / approval limits --------------------------------------------

async function loadMemberships() {
    const { status, body } = await apiFetch('v1/settings/memberships');
    const tbody = document.querySelector('#memberships-table tbody');
    if (status !== 200) { tbody.innerHTML = '<tr class="empty-row"><td colspan="5">Could not load users.</td></tr>'; return; }
    tbody.innerHTML = body.length ? body.map(m => `<tr>
        <td>${escapeHtml(m.user_name)}<div class="muted">${escapeHtml(m.user_email)}</div></td>
        <td>${escapeHtml(m.role.replace('_', ' '))}</td>
        <td>${m.branch_names.length ? escapeHtml(m.branch_names.join(', ')) : 'All branches'}</td>
        <td><input id="limit-${m.id}" type="number" step="0.01" style="width:120px;" value="${m.approval_limit ?? ''}" placeholder="Unlimited"></td>
        <td><button type="button" class="secondary" onclick="window.saveLimit('${m.id}')">Save</button></td>
    </tr>`).join('') : '<tr class="empty-row"><td colspan="5">No users.</td></tr>';

    document.getElementById('device_user_id').innerHTML = body.map(m => `<option value="${m.user_id}">${escapeHtml(m.user_name)}</option>`).join('');
}

window.saveLimit = async (id) => {
    const resultEl = document.getElementById('membership-result');
    const raw = document.getElementById(`limit-${id}`).value;
    const { status, body } = await apiFetch(`v1/settings/memberships/${id}/approval-limit`, {
        method: 'POST', body: JSON.stringify({ approval_limit: raw === '' ? null : raw }),
    });
    if (status === 200) { resultEl.innerHTML = '<div class="status">Approval limit saved.</div>'; } else { rejected(resultEl, status, body); }
};

// --- Add a user ------------------------------------------------------------

document.getElementById('user-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const resultEl = document.getElementById('user-result');
    const branchScope = Array.from(document.getElementById('user_branch_scope').selectedOptions).map(o => o.value);
    const limitRaw = document.getElementById('user_approval_limit').value;
    const payload = {
        email: document.getElementById('user_email').value,
        full_name: document.getElementById('user_full_name').value,
        role: document.getElementById('user_role').value,
        branch_scope: branchScope,
        approval_limit: limitRaw === '' ? null : limitRaw,
    };
    const { status, body } = await apiFetch('v1/settings/users', { method: 'POST', body: JSON.stringify(payload) });
    if (status === 201) {
        resultEl.innerHTML = '<div class="status">User added. They can request a password via "Forgot your password?" on the login page.</div>';
        e.target.reset();
        loadMemberships();
    } else {
        rejected(resultEl, status, body);
    }
});

loadCompany();
loadBranches();
loadAccounts();
loadDevices();
loadMemberships();
</script>
@endsection
