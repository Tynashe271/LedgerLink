<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <meta name="csrf-token" content="{{ csrf_token() }}">
    <meta name="theme-color" content="#15192e">
    <link rel="manifest" href="/manifest.webmanifest">
    <title>@yield('title', 'BranchLedger')</title>
    <style>
        :root {
            color-scheme: light dark;
            --bg: #f5f6fa; --panel: #ffffff; --text: #16182b; --muted: #6b7280;
            --border: #e4e6ee; --accent: #2b5fd9; --accent-soft: #eef2fd;
            --danger: #c0392b; --danger-soft: #fdecea; --ok: #1f8a4c; --ok-soft: #eafaf1;
            --warn: #b7791f; --warn-soft: #fef6e7;
            --sidebar-bg: #15192e; --sidebar-text: #aeb4cc; --sidebar-text-active: #ffffff;
            --radius: 10px;
        }
        * { box-sizing: border-box; }
        body {
            margin: 0; font-family: system-ui, -apple-system, "Segoe UI", sans-serif;
            background: var(--bg); color: var(--text); font-size: 14px;
        }
        a { color: var(--accent); text-decoration: none; }
        a:hover { text-decoration: underline; }

        /* --- Shell: fixed sidebar + topbar + scrollable content ------------- */
        .app-shell { display: flex; min-height: 100vh; }
        .sidebar {
            width: 220px; flex-shrink: 0; background: var(--sidebar-bg); color: var(--sidebar-text);
            padding: 20px 0; position: sticky; top: 0; height: 100vh; overflow-y: auto;
        }
        .sidebar .brand { font-weight: 700; color: #fff; font-size: 17px; padding: 0 20px 20px; display: block; }
        .sidebar nav a {
            display: flex; align-items: center; gap: 10px; padding: 9px 20px; color: var(--sidebar-text);
            font-size: 13.5px; font-weight: 500; border-left: 3px solid transparent;
        }
        .sidebar nav a:hover { color: #fff; text-decoration: none; background: rgba(255,255,255,0.04); }
        .sidebar nav a.active {
            color: var(--sidebar-text-active); background: rgba(255,255,255,0.07);
            border-left-color: var(--accent);
        }
        .sidebar nav .section-label {
            font-size: 10.5px; text-transform: uppercase; letter-spacing: 0.06em; color: #6b7290;
            padding: 16px 20px 6px; font-weight: 700;
        }
        .main { flex: 1; min-width: 0; display: flex; flex-direction: column; }
        header.topbar {
            display: flex; justify-content: space-between; align-items: center;
            padding: 12px 24px; background: var(--panel); border-bottom: 1px solid var(--border);
            position: sticky; top: 0; z-index: 5;
        }
        header.topbar .ctx { font-size: 13px; color: var(--muted); }
        header.topbar .ctx strong { color: var(--text); }
        .content { padding: 24px; max-width: 1180px; width: 100%; margin: 0 auto; }

        h1 { font-size: 20px; margin: 0 0 4px; }
        h2 { font-size: 15px; margin: 0 0 12px; }
        .page-subtitle { color: var(--muted); font-size: 13px; margin: 0 0 20px; }

        /* --- Panels / cards --------------------------------------------------- */
        .panel {
            background: var(--panel); border: 1px solid var(--border); border-radius: var(--radius);
            padding: 20px; margin-top: 16px;
        }
        .panel > h2:first-child, .panel > .panel-head { margin-top: 0; }
        .panel-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; }
        .grid { display: grid; gap: 12px; }
        .grid-stats { grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); }
        .grid-2 { grid-template-columns: 1fr 1fr; }
        @media (max-width: 860px) { .grid-2 { grid-template-columns: 1fr; } .app-shell { flex-direction: column; } .sidebar { width: 100%; height: auto; position: static; } }

        .stat-tile { background: #fafbfe; border: 1px solid var(--border); border-radius: 8px; padding: 12px 14px; }
        .stat-tile .label { font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--muted); }
        .stat-tile .value { font-size: 19px; font-weight: 700; margin-top: 2px; }
        .stat-tile .value.neg { color: var(--danger); }
        .stat-tile .value.pos { color: var(--ok); }

        /* --- Forms -------------------------------------------------------------- */
        label { display: block; font-size: 13px; margin: 12px 0 4px; color: var(--muted); font-weight: 500; }
        input, select, textarea, button {
            font: inherit; padding: 9px 11px; border-radius: 6px; border: 1px solid var(--border);
            width: 100%; background: #fff; color: var(--text);
        }
        textarea { resize: vertical; }
        button { background: var(--accent); color: white; border: none; cursor: pointer; margin-top: 16px; font-weight: 600; }
        button:hover { opacity: 0.92; }
        button.secondary { background: #fff; color: var(--text); border: 1px solid var(--border); }
        button.danger { background: var(--danger); }
        button[type="button"] { width: auto; }
        .btn-row { display: flex; gap: 8px; }
        .btn-row button { margin-top: 0; }

        /* --- Feedback ------------------------------------------------------------ */
        .errors { background: var(--danger-soft); border: 1px solid var(--danger); color: var(--danger); padding: 10px 14px; border-radius: 6px; margin-bottom: 12px; font-size: 13.5px; }
        .status { background: var(--ok-soft); border: 1px solid var(--ok); color: var(--ok); padding: 10px 14px; border-radius: 6px; margin-bottom: 12px; font-size: 13.5px; }
        .alert-warn { background: var(--warn-soft); border: 1px solid var(--warn); color: var(--warn); padding: 10px 14px; border-radius: 6px; margin-bottom: 10px; font-size: 13.5px; }

        /* --- Tables -------------------------------------------------------------- */
        table { width: 100%; border-collapse: collapse; margin-top: 4px; }
        th, td { text-align: left; padding: 9px 8px; border-bottom: 1px solid var(--border); font-size: 13.5px; }
        th { color: var(--muted); font-weight: 600; font-size: 11.5px; text-transform: uppercase; letter-spacing: 0.03em; }
        tbody tr:hover { background: #fafbfe; }
        .empty-row td { color: var(--muted); text-align: center; padding: 24px 8px; }

        /* --- Badges --------------------------------------------------------------- */
        .badge { display: inline-block; padding: 2px 9px; border-radius: 999px; font-size: 11.5px; font-weight: 600; }
        .badge-posted { background: var(--ok-soft); color: var(--ok); }
        .badge-awaiting_approval { background: var(--warn-soft); color: var(--warn); }
        .badge-reversed { background: #f0f0f3; color: var(--muted); }
        .badge-rejected { background: var(--danger-soft); color: var(--danger); }
        .badge-dispatched { background: var(--warn-soft); color: var(--warn); }
        .badge-partially_received { background: var(--warn-soft); color: var(--warn); }
        .badge-received { background: var(--ok-soft); color: var(--ok); }
        .badge-stale { background: var(--danger-soft); color: var(--danger); }
        .badge-fresh { background: var(--ok-soft); color: var(--ok); }
        .badge-pending { background: var(--warn-soft); color: var(--warn); }
        .badge-syncing { background: var(--accent-soft); color: var(--accent); }
        .badge-synced { background: var(--ok-soft); color: var(--ok); }
        .badge-failed { background: var(--danger-soft); color: var(--danger); }
        .badge-rejected { background: var(--danger-soft); color: var(--danger); }

        #sync-status { display: flex; align-items: center; gap: 6px; font-size: 12.5px; color: var(--muted); cursor: pointer; }
        #sync-status .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--muted); flex-shrink: 0; }
        #sync-status .dot.online { background: var(--ok); }
        #sync-status .dot.offline { background: var(--danger); }
        #sync-status .dot.pending { background: var(--warn); }

        .muted { color: var(--muted); font-size: 12.5px; }
        code { background: #f0f1f6; padding: 1px 5px; border-radius: 4px; font-size: 12px; }

        /* --- Simple SVG chart helpers --------------------------------------------- */
        .bar-row { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; font-size: 13px; }
        .bar-row .bar-label { width: 110px; flex-shrink: 0; text-align: right; color: var(--muted); }
        .bar-row .bar-track { flex: 1; background: #f0f1f6; border-radius: 4px; height: 18px; position: relative; overflow: hidden; }
        .bar-row .bar-fill { height: 100%; background: var(--accent); border-radius: 4px; }
        .bar-row .bar-fill.neg { background: var(--danger); }
        .bar-row .bar-value { width: 90px; flex-shrink: 0; text-align: right; font-weight: 600; }
    </style>
</head>
<body>
<div class="app-shell">
    <aside class="sidebar">
        <span class="brand">BranchLedger</span>
        <nav>
            <a href="{{ route('dashboard') }}" class="{{ request()->routeIs('dashboard') ? 'active' : '' }}">Overview</a>
            <a href="{{ route('branch-overview') }}" class="{{ request()->routeIs('branch-overview') ? 'active' : '' }}">Branch overview</a>
            <div class="section-label">Daily operations</div>
            <a href="{{ route('workspace.sales') }}" class="{{ request()->routeIs('workspace.sales') ? 'active' : '' }}">Sales</a>
            <a href="{{ route('workspace.expenses') }}" class="{{ request()->routeIs('workspace.expenses') ? 'active' : '' }}">Expenses</a>
            <a href="{{ route('workspace.purchases') }}" class="{{ request()->routeIs('workspace.purchases') ? 'active' : '' }}">Purchases</a>
            <a href="{{ route('workspace.transfers') }}" class="{{ request()->routeIs('workspace.transfers') ? 'active' : '' }}">Transfers</a>
            <a href="{{ route('workspace.stock') }}" class="{{ request()->routeIs('workspace.stock') ? 'active' : '' }}">Stock</a>
            <a href="{{ route('workspace.closes') }}" class="{{ request()->routeIs('workspace.closes') ? 'active' : '' }}">Daily close</a>
            <a href="{{ route('workspace.reconciliation') }}" class="{{ request()->routeIs('workspace.reconciliation') ? 'active' : '' }}">Reconciliation</a>
            <a href="{{ route('workspace.reports') }}" class="{{ request()->routeIs('workspace.reports') ? 'active' : '' }}">Reports</a>
            <a href="{{ route('workspace.sync-centre') }}" class="{{ request()->routeIs('workspace.sync-centre') ? 'active' : '' }}">Sync centre</a>
            <div class="section-label">Catalogue</div>
            <a href="{{ route('workspace.products') }}" class="{{ request()->routeIs('workspace.products') ? 'active' : '' }}">Products</a>
            <a href="{{ route('workspace.customers') }}" class="{{ request()->routeIs('workspace.customers') ? 'active' : '' }}">Customers</a>
            @if(($role ?? null) !== 'staff')
                <div class="section-label">Review</div>
                <a href="{{ route('workspace.approvals') }}" class="{{ request()->routeIs('workspace.approvals') ? 'active' : '' }}">Approvals</a>
                <div class="section-label">Admin</div>
                <a href="{{ route('workspace.settings') }}" class="{{ request()->routeIs('workspace.settings') ? 'active' : '' }}">Settings</a>
            @endif
        </nav>
    </aside>
    <div class="main">
        <header class="topbar">
            <span class="ctx">
                <strong>{{ $company->name ?? '' }}</strong> &middot; {{ ucfirst(str_replace('_', ' ', $role ?? '')) }}
            </span>
            <span style="display:flex; align-items:center; gap:16px;">
                <a id="sync-status" href="{{ route('workspace.sync-centre') }}">
                    <span class="dot" id="sync-dot"></span>
                    <span id="sync-label">Checking&hellip;</span>
                </a>
                <form method="POST" action="{{ route('logout') }}" style="display:inline;">@csrf<button type="submit" style="width:auto; background:transparent; color:#6b7280; border:1px solid #e2e4ea; margin-top:0;">Sign out</button></form>
            </span>
        </header>
        <div class="content">
            @yield('body')
        </div>
    </div>
</div>
<script type="module">
    import { outboxSummary, startAutoSync } from '/js/offline/sync.js';

    if ('serviceWorker' in navigator) {
        navigator.serviceWorker.register('/sw.js').catch(() => {});
    }

    function updateStatusDot() {
        const dot = document.getElementById('sync-dot');
        const label = document.getElementById('sync-label');
        outboxSummary().then(({ counts, lastSyncAt }) => {
            const unsynced = counts.pending + counts.syncing + counts.failed + counts.rejected;
            dot.className = 'dot ' + (!navigator.onLine ? 'offline' : unsynced > 0 ? 'pending' : 'online');
            if (!navigator.onLine) {
                label.textContent = unsynced > 0 ? `Offline · ${unsynced} pending` : 'Offline';
            } else if (unsynced > 0) {
                label.textContent = `Syncing · ${unsynced} pending`;
            } else {
                label.textContent = lastSyncAt ? `Synced · ${new Date(lastSyncAt).toLocaleTimeString()}` : 'Not yet synced';
            }
        }).catch(() => { label.textContent = 'Sync status unavailable'; });
    }

    startAutoSync();
    updateStatusDot();
    setInterval(updateStatusDot, 5000);
    window.addEventListener('online', updateStatusDot);
    window.addEventListener('offline', updateStatusDot);
</script>
</body>
</html>
