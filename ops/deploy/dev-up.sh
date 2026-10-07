#!/usr/bin/env bash
# Brings up the full local BranchLedger stack for manual testing:
#   Postgres (podman) -> Go API (native) -> Laravel (native) -> nginx (podman)
# Matches the component diagram in BranchLedger_Architecture.pdf, except
# Postgres/nginx run in Podman containers while Go and Laravel run as native
# processes on the dev machine (faster edit/rebuild loop than containerizing
# PHP/Go for local development; ops/deploy would containerize everything for
# a real deployment).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
ROOT="$(pwd)"

DELEGATION_SECRET="${DELEGATION_SECRET:-839dc4ab8928751dbec86faccea77e4d6719ce70e7f353a1d6276f9c46104fba}"
DB_HOST_PORT="${DB_HOST_PORT:-5442}"
GO_API_PORT="${GO_API_PORT:-8099}"
LARAVEL_PORT="${LARAVEL_PORT:-8000}"

echo "==> Starting Postgres (podman)"
if ! podman start branchledger-postgres >/dev/null 2>&1; then
  podman run -d --name branchledger-postgres \
    -e POSTGRES_PASSWORD=branchledger_dev -e POSTGRES_USER=branchledger -e POSTGRES_DB=branchledger \
    -p "${DB_HOST_PORT}:5432" docker.io/library/postgres:17-alpine
fi
until podman exec branchledger-postgres pg_isready -U branchledger >/dev/null 2>&1; do sleep 1; done
echo "    Postgres ready on localhost:${DB_HOST_PORT}"

echo "==> Starting Go API (native)"
( cd backend && \
  DELEGATION_SECRET="$DELEGATION_SECRET" \
  DATABASE_URL="postgres://branchledger:branchledger_dev@localhost:${DB_HOST_PORT}/branchledger?sslmode=disable" \
  API_LISTEN_ADDR=":${GO_API_PORT}" \
  go run ./cmd/api > "$ROOT/ops/deploy/.go-api.log" 2>&1 & )
echo "    Go API starting on :${GO_API_PORT} (log: ops/deploy/.go-api.log)"

echo "==> Starting Laravel (native)"
# --host=0.0.0.0 is required, not cosmetic: artisan serve's default
# 127.0.0.1-only bind is unreachable from the nginx container via
# host.containers.internal, so nginx would proxy_pass into a black hole.
( cd web && php artisan serve --host=0.0.0.0 --port="${LARAVEL_PORT}" > "$ROOT/ops/deploy/.laravel.log" 2>&1 & )
echo "    Laravel starting on :${LARAVEL_PORT} (log: ops/deploy/.laravel.log)"

echo "==> Starting nginx (podman)"
podman rm -f branchledger-nginx >/dev/null 2>&1 || true
# MSYS_NO_PATHCONV is required here: Git Bash's auto path-conversion mangles
# the container-side half of -v SRC:DST mounts (e.g. turns
# "/etc/nginx/nginx.conf" into a Windows path), which breaks podman's option
# parsing.
MSYS_NO_PATHCONV=1 podman run -d --name branchledger-nginx \
  -p 8180:80 -p 8443:443 \
  -v "$ROOT/ops/deploy/nginx/nginx.conf:/etc/nginx/nginx.conf:ro" \
  -v "$ROOT/ops/deploy/nginx/certs:/etc/nginx/certs:ro" \
  docker.io/library/nginx:1.27-alpine
echo "    nginx ready: https://127.0.0.1:8443 (self-signed cert; http://127.0.0.1:8180 redirects)"

echo ""
echo "Stack is up. Direct Laravel: http://127.0.0.1:${LARAVEL_PORT}  |  Via nginx: https://127.0.0.1:8443"
echo "Use 127.0.0.1, not localhost: this host's podman port-forwarding has been"
echo "observed to only route the IPv4 mapping, and curl/browsers try ::1 first."
