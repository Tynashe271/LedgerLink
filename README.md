# BranchLedger

A multi-branch accounting and business-monitoring platform: a company runs a
main branch plus any number of sub-branches, and every owner/manager view
rolls activity up across all of them rather than showing one branch in
isolation. Covers daily transactions (sales, purchases, expenses, transfers),
stock (receiving, sale, waste, stock counts), approvals, daily close, and a
manager dashboard, with offline-first capture on the browser side.

Full functional and architectural specs: [`docs/specs/`](docs/specs/)
(`BranchLedger_Architecture.pdf`, `BranchLedger_System_Documentation.pdf`,
`BranchLedger_User_Manual.pdf`).

## Architecture

```
Browser --> nginx (TLS) --> Laravel (sessions, CSRF, /api gateway) --> Go API --> PostgreSQL
```

- **`backend/`** — Go accounting API: posting rules, double-entry journals,
  weighted-average stock costing, sync push/pull, reporting. Owns every
  business rule; nothing posts a transaction except through it.
- **`web/`** — Laravel app: login, company/branch selection, the daily-
  operations screens (Blade + vanilla JS, offline outbox via IndexedDB), and
  a same-origin `/api/*` gateway that proxies authenticated requests through
  to the Go API.
- **`database/`** — SQL migrations and the local dev seed.
- **`ops/deploy/`** — local dev stack scripts (`dev-up.sh` / `dev-down.sh`)
  and nginx config.

## Prerequisites

- Go 1.27+
- PHP 8.3+ and Composer
- Node.js (optional — only needed if you touch `web/resources/css`; nothing
  currently requires a Vite build to run the app)
- [Podman](https://podman.io/) (or adjust `ops/deploy/dev-up.sh` for Docker)
  for local Postgres and nginx containers

## First-time setup

```sh
# Backend dependencies
cd backend && go mod download && cd ..

# Laravel dependencies
cd web && composer install && cp .env.example .env && php artisan key:generate && cd ..
```

`web/.env.example` is the unmodified Laravel scaffold (sqlite, no delegation
secret) — add these to `web/.env` before running anything:

```
DB_CONNECTION=pgsql
DB_HOST=127.0.0.1
DB_PORT=5442
DB_DATABASE=branchledger
DB_USERNAME=branchledger
DB_PASSWORD=branchledger_dev

DELEGATION_SECRET=<copy the default value from the DELEGATION_SECRET line near the top of ops/deploy/dev-up.sh>
```

`web/.env`'s `DELEGATION_SECRET` must exactly match what the Go API is
started with — `ops/deploy/dev-up.sh` exports a dev-only placeholder for the
Go side unless you override it (see **Secrets** below). The DB values above
match the Postgres container `dev-up.sh` starts.

Bring up Postgres once, then apply the schema and demo seed:

```sh
ops/deploy/dev-up.sh   # starts Postgres, Go API, Laravel, nginx

podman exec branchledger-postgres psql -U branchledger -d branchledger -f database/migrations/0001_init_schema.up.sql
podman exec branchledger-postgres psql -U branchledger -d branchledger -f database/migrations/0002_workflow_extensions.up.sql
podman exec branchledger-postgres psql -U branchledger -d branchledger -f database/migrations/0003_membership_delegations.up.sql
podman exec branchledger-postgres psql -U branchledger -d branchledger -f database/migrations/0004_products_customers_tracking.up.sql
podman exec branchledger-postgres psql -U branchledger -d branchledger -f database/migrations/0005_reconciliation.up.sql
podman exec branchledger-postgres psql -U branchledger -d branchledger -f database/migrations/0006_company_tax_registration.up.sql
podman exec branchledger-postgres psql -U branchledger -d branchledger -f database/seeds/dev_seed.sql

cd web && php artisan migrate   # Laravel's own tables: sessions, cache, jobs
```

## Running the dev stack

```sh
ops/deploy/dev-up.sh    # Postgres (podman) + Go API (:8099) + Laravel (:8000) + nginx
```

Open **https://127.0.0.1:8443** (self-signed cert — your browser will warn
once) or **http://127.0.0.1:8000** directly against Laravel. Use `127.0.0.1`,
not `localhost` — see `ops/deploy/README.md` for why.

Seeded demo login: `owner@alpha.example` / `BranchLedger!2026` (company
"Alpha Traders", a main branch and a sub-branch). This is a throwaway dev
credential — see `database/seeds/dev_seed.sql` before using it anywhere but a
disposable local database.

```sh
ops/deploy/dev-down.sh  # stops everything; Postgres data survives
```

More detail on the stack's topology: [`ops/deploy/README.md`](ops/deploy/README.md).

## Running tests

```sh
cd backend && go test ./...

cd web && php artisan test
```

## Secrets

`DELEGATION_SECRET` (hex, ≥32 bytes) must match between the Go API's
environment and Laravel's `.env` — it's the HMAC key for the delegated-
identity token (`backend/internal/auth/delegation.go`). Generate a real one
per environment with `openssl rand -hex 32`; never commit one. The value in
`ops/deploy/dev-up.sh` (and the one to put in `web/.env` for local dev) is a
throwaway placeholder only.
