# CLAUDE.md — Obrabi

Guidance for Claude Code sessions working on this repository.

## What this is

Obrabi is a small BI / bookkeeping web app for **Nando**, a mason running
renovation jobs around València. Per job (*obra*) it tracks what Nando pays,
what he charges the client, what the client has already paid and the profit;
the home page shows monthly statistics.

- **Owner / developer: Luca** — writes in Italian; answer him in Italian.
- **End user: Nando**, mostly from a **phone** (installed as a PWA), sometimes a PC.
- **UI language: Valencian** (AVL standard). All user-facing text — including
  API error `message`s — is Valencian. Code, identifiers and comments are
  English. E-mails to Luca (feedback / bug reports) are Italian.
- **Hosting: Luca's on-premise Proxmox** (Docker Compose in a Debian LXC with
  `nesting,keyctl`, or a VM), behind his reverse proxy or the bundled Caddy.

`README.md` (Italian) is the user-facing documentation: features, architecture,
ER diagram, Proxmox deployment, e-mail, backups. Keep it in sync.

## Architecture

Go monorepo (one `go.mod`, Go ≥ 1.26), five microservices, each a Gin HTTP
server with its own binary in `cmd/<service>`. One Docker image, five
containers. PostgreSQL 16 with **one schema and one DB role per service**
(created by `deploy/postgres/init/01-obrabi.sh` when the volume is first
initialised).

| Service | Port | Schema | Responsibility |
|---|---|---|---|
| `gateway` | 8080 | – | Only public service. Serves the SPA embedded from `web/static` (ETag + gzip in memory, `{{VERSION}}` placeholder = content hash), login/logout, session cookie (JWT HS256, minted and verified only here, 60-day sliding TTL, `Secure` auto-detected via TLS / `X-Forwarded-Proto`), reverse-proxies `/api/*` (minus the `/api` prefix) adding `X-User-ID`, `X-Username`, `X-Request-ID`, `X-Internal-Token`; strips those headers and cookies from client requests and blocks `/internal/*`. Login rate limit per IP (10 fails / 15 min) and per username. CSRF: mutating `/api` calls need the `X-Obrabi` header (+ SameSite=Lax). |
| `auth` | 8081 | `auth` | Users & credentials (bcrypt cost 12). Creates `OBRABI_BOOTSTRAP_USERS` (default `nando`) with a random password printed once to the log. CLI: `auth user list | add <user> [name] | reset-password <user>`. |
| `projects` | 8082 | `projects` | Towns, projects, line items, categories, client payments, finish/reopen, PDF (`?variant=intern|client`). CLI: `projects seed-demo <user-id>`. |
| `stats` | 8083 | – | Dashboard analytics; DB role has only `SELECT` on schema `projects` (read side). |
| `feedback` | 8084 | `feedback` | Suggestions/bug reports + error telemetry (dedup by fingerprint, 6 h window). The reports table is also the e-mail outbox; a worker sends via SMTP with backoff and refuses AUTH without STARTTLS. |

Every service binary also answers `<binary> healthcheck` (used by Docker; the
images are distroless, no shell). Internal services reject requests without
the shared `X-Internal-Token` (`httpx.InternalOnly`) and read the user from
`X-User-ID` (`httpx.RequireUser`). Services report errors to `feedback` through
`internal/platform/telemetry` (5xx with `httpx.Internal`, panics via `Recover`).

Docker networks: `backend` is `internal: true` (no Internet: db + services),
`edge` publishes the gateway (and Caddy), `egress` lets only `feedback` reach
SMTP. The bundled Caddy lives in `OBRABI_EDGE_SUBNET`, trusted by default.

## Layout

```
cmd/<service>/main.go        entrypoints, env config, CLI sub-commands
internal/platform/           config (env), database (pgx pool + embedded migrations
                             with advisory lock), httpx (Gin middleware, errors, graceful
                             shutdown, healthcheck), telemetry, logging (slog), money,
                             ratelimit, buildinfo
internal/<service>/          service code; SQL migrations in internal/<service>/migrations/NNN_name.sql
internal/integration/        SQL integration tests (projects + stats) against real PostgreSQL
web/static/                  frontend: index.html, css/app.css, js/ (ES modules), icons/, sw.js, manifest
web/tests/                   frontend unit tests (node --test)
deploy/                      postgres init (roles/schemas), backup.sh, Caddyfile
scripts/                     gen-env.sh (random secrets → .env), dev.sh (local run)
docs/screenshots/            images used by README.md
```

## Domain glossary (Valencian UI ↔ code)

| UI (Valencian) | Code | Notes |
|---|---|---|
| obra / obres | `project` | belongs to one town (N:1); `status` = `active` \| `finished` (*arxiu*) |
| poble | `town` | groups projects; unique per user, case-insensitive (`lower(name)` index) |
| partida | line item | `kind` = `expense` (category required; cost + price) or `labor` (*mà d'obra*: hours + price, optional helper cost, **no category**) |
| categoria de despesa | `category` | created on the fly (upsert by name); can be renamed or merged (`DELETE /categories/:id?merge_into=`) |
| cobrament | `payment` | money received from the client |
| benefici | benefit | `price - cost` |
| pendent de cobrar | pending | `price - collected` (a finished job can still owe money) |
| balanç de caixa | balance | `collected - cost` |

Money is **always integer cents** (`*_cents`, `int64` / JS numbers) — never
floats. Dates are `YYYY-MM-DD` strings in the API and `DATE` columns; "today"
is computed in `OBRABI_TIMEZONE` (Europe/Madrid) in Go or on the device, never
with SQL `CURRENT_DATE`. Statistics attribute revenue/cost/benefit to the
item's `item_date` month and collections to the payment's `paid_on` month.
Month comparisons use the same days of the previous month (month-to-date).

## Conventions

- Every query in `projects`/`stats` filters by `user_id`; there are no
  cross-schema foreign keys.
- Errors: JSON `{"error": "<code>", "message": "<Valencian text>"}` via
  `httpx.Fail/BadRequest/NotFound/Conflict/Internal`. `Internal(c, err)`
  attaches the error so the middleware logs it and reports it.
- Migrations are append-only: add `NNN_name.sql`, never edit an applied one.
- Item/payment mutations return the whole updated project (`{"item"|"payment", "project"}`)
  so the phone needs one round trip.
- Frontend: no framework, no bundler. Build DOM with `h()` from `js/dom.js`
  (never `innerHTML` with data). Strict CSP: no inline scripts, no `style="…"`
  attributes (use the CSSOM / `setStyle`). Mobile-first, touch targets ≥ 44px,
  `inputmode="decimal"` for amounts, 16px inputs (no iOS zoom). Sheets are
  `<dialog>`s that also close with the phone's back button (`ui.openSheet`
  pushes a history entry; `sheet.close()` settles only after the pop).
  `api.js` caches GETs in memory; any mutation clears the cache.
- Charts (`js/charts.js`) follow the data-viz rules: validated palette tokens
  (`--s1…--s5`, `--s-prev`, `--s-context`, `--s-other`), columns ≤ 24px with
  4px rounded ends, hairline grid, tooltip on hover/tap/focus, table view.
  Category colours follow the entity (top-3 all-time turnover → slots 1–3,
  the rest "Altres").
- Valencian copy: *este/esta*, *eixir*, *tindre*, *hui*, *arrere*, inchoatives
  in *-ix* (*afegix*); buttons in the infinitive (*Guardar*, *Afegir*).
- The client PDF (`variant=client`) must never show costs or margins.

## Commands

```sh
make test          # go test ./... + node --test web/tests/*.test.mjs
make lint          # go vet + gofmt check
make build         # binaries into ./bin
make dev           # PostgreSQL in Docker (docker-compose.dev.yml) + services locally
make dev-seed      # demo data for user 1 while `make dev` runs
make up / ps / logs / password / users / reset-password WHO=nando / backup
```

If the local Go is older than 1.26 use `GOTOOLCHAIN=go1.26.8` (downloaded via
the module proxy), e.g. `make test GO="env GOTOOLCHAIN=go1.26.8 go"`.

Integration tests need a throw-away database whose name contains `test`
(schema `projects` is dropped):
`OBRABI_TEST_DATABASE_URL=postgres://…/obrabi_test go test ./internal/integration/`.

Frontend lint used during development: ESLint with `no-undef` and
`no-unused-vars` over `web/static/js` (no config committed; the code has no
build step).

## Things to keep in mind

- Nando's credentials are never committed: the auth service generates them on
  first start (`make password`, or `make reset-password WHO=nando`).
- `.env` holds all secrets (created by `scripts/gen-env.sh`); changing DB
  passwords after the first start also requires `ALTER ROLE` in PostgreSQL.
- The `stats` service depends on the tables created by `projects` migrations
  (compose waits for `projects` to be healthy).
- Telemetry must never block a request nor flood the mailbox.
- The sandbox used to write this code needed its TLS-proxy CA injected to run
  `docker build` (a temporary Dockerfile with `--build-context`); the real
  `Dockerfile` must stay free of such hacks.
