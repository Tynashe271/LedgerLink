# BranchLedger local dev stack

Mirrors `BranchLedger_Architecture.pdf`'s component diagram: browser → nginx
(TLS) → Laravel (sessions, CSRF, same-origin `/api` gateway) → Go API
(accounting, sync, posting rules) → PostgreSQL. Workers are not started by
this script yet (`backend/cmd/worker` exists but has no job processing wired
up — see its TODO).

## Topology (local dev only)

| Component  | How it runs here              | Why                                                             |
|------------|--------------------------------|------------------------------------------------------------------|
| PostgreSQL | Podman container                | Matches prod; no native Windows install needed                  |
| Go API     | Native (`go run`)               | Fast edit/rebuild loop; containerizing adds no local value       |
| Laravel    | Native (`php artisan serve`)    | Same reasoning; no Docker/Podman PHP image needed for dev        |
| nginx      | Podman container                 | TLS termination + reverse proxy, as the architecture specifies   |

A real deployment would containerize Go and Laravel too (see the
proposed `backend/cmd/api`, `backend/cmd/worker` and `web/` layout) behind
the same nginx config, on an internal network nginx already expects
(`laravel-app:8000` — see the comment in `nginx/nginx.conf`).

## Bring the stack up

```sh
ops/deploy/dev-up.sh
```

This starts, in order: Postgres (podman, waits for `pg_isready`), the Go API
(native, port 8099), Laravel (native, `php artisan serve`, port 8000), then
nginx (podman, ports 8180→80 and 8443→443, using the self-signed dev cert in
`nginx/certs/`).

Open **https://127.0.0.1:8443** (self-signed — your browser will warn once).
Direct Laravel access for debugging: http://127.0.0.1:8000.

Use `127.0.0.1`, not `localhost`: on this host, curl/browsers try the `::1`
(IPv6) address first, and podman's port-forwarding for these containers has
only been observed to route the IPv4 mapping — the IPv6 attempt times out or
connects to nothing before falling back, which shows up as a hung or aborted
connection. Also note `php artisan serve` must bind `--host=0.0.0.0`
(`dev-up.sh` already does this) — its 127.0.0.1-only default is unreachable
from the nginx container via `host.containers.internal`.

First-time setup (once, before `dev-up.sh`):

```sh
# Database schema + demo seed (company "Alpha Traders", owner@alpha.example)
podman exec branchledger-postgres psql -U branchledger -d branchledger -f database/migrations/0001_init_schema.up.sql
podman exec branchledger-postgres psql -U branchledger -d branchledger -f database/seeds/dev_seed.sql

# Laravel's own tables (sessions, password_reset_tokens, cache, jobs)
cd web && php artisan migrate && php artisan key:generate
```

The seeded owner login is `owner@alpha.example` / `BranchLedger!2026` — change
the seed's bcrypt hash (see `database/seeds/dev_seed.sql`) before using this
anywhere but a throwaway dev database.

## Bring it down

```sh
ops/deploy/dev-down.sh
```

Stops the podman containers and native processes. Postgres is *stopped*, not
removed, so seeded/test data survives a restart.

## Secrets

`DELEGATION_SECRET` (hex, ≥32 bytes) must be identical between the Go API's
environment and Laravel's `.env` — it's the HMAC key for the delegated-identity
token described in `backend/internal/auth/delegation.go`. Generate a new one
per environment with:

```sh
openssl rand -hex 32
```

Never commit a real one; the value in `web/.env` and `dev-up.sh`'s default is
a local-dev placeholder only.
