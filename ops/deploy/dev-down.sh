#!/usr/bin/env bash
# Stops the native Go/Laravel dev processes and the podman containers started
# by dev-up.sh. Postgres is stopped (not removed) so seeded data survives.
set -uo pipefail

GO_API_PORT="${GO_API_PORT:-8099}"
LARAVEL_PORT="${LARAVEL_PORT:-8000}"

echo "==> Stopping nginx"
podman stop branchledger-nginx >/dev/null 2>&1 || true
podman rm branchledger-nginx >/dev/null 2>&1 || true

echo "==> Stopping Postgres (data preserved)"
podman stop branchledger-postgres >/dev/null 2>&1 || true

echo "==> Stopping native Go API / Laravel processes"
# Processes backgrounded with `&` from Git Bash don't map cleanly onto a
# single killable PID in the Windows process tree, so this stops whatever is
# actually bound to the known ports instead of tracking PID files.
for port in "$GO_API_PORT" "$LARAVEL_PORT"; do
  powershell -NoProfile -Command "
    Get-NetTCPConnection -LocalPort $port -ErrorAction SilentlyContinue |
      ForEach-Object { Stop-Process -Id \$_.OwningProcess -Force -ErrorAction SilentlyContinue }
  " >/dev/null 2>&1 || true
done

echo "Done. Re-run ops/deploy/dev-up.sh to bring everything back."
