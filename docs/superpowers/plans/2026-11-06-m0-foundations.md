# M0 — Foundations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On a clean clone, `docker compose up` brings up a healthy Go API, worker, simulator, Postgres 18 and Mailpit with a migrated and seeded lot. Web and mobile scaffolds show the API's health in es-DO and en, and CI gates every merge to a protected `main`.

**Architecture:** Monorepo `github.com/XYZLZ/MyPark`. Go module `apps/server` (stdlib `net/http`, pgx/v5, sqlc, goose, slog). npm workspaces for `apps/web` (Next.js 16), `apps/mobile` (Expo SDK 58) and `packages/contracts` (TS types generated from `apps/server/api/openapi.yaml`). Two tracks run in parallel. Track B depends on Track A only through the OpenAPI health contract (Task A2).

**Tech Stack:** Go 1.25, pgx/v5, sqlc, goose, PostgreSQL 18, Docker Compose, Mailpit; Next.js 16.4.5, React/React DOM 19.3.0, Tailwind CSS 4.3.5, next-intl, TanStack Query 5, zustand 5, zod 4, vitest; Expo ~58.0.23, expo-router ~58.0.30, react-native 0.88.1, Uniwind 1.13.2, i18next + react-i18next + expo-localization, jest-expo; openapi-typescript; GitHub Actions.

**Spec:** `docs/spec.md` §8 M0 (scope items 0–8 + exit criteria); `docs/architecture.md` §1, §2.1, §3, §4.2–4.3, §11.3, §12–§16; `docs/policies.md` §2.

## Global Constraints

- Package manager **npm** with workspaces (`apps/web`, `apps/mobile`, `packages/contracts`). One root `package-lock.json`. Never pnpm/yarn.
- Exact pins: `next` 16.4.5; `react`/`react-dom` **19.3.0** in both apps (root `overrides` too); `expo` ~58.0.23; `expo-router` ~58.0.30; `react-native` 0.88.1; `uniwind` 1.13.2; `tailwindcss` 4.3.5; `zustand` 5.1.0; `@tanstack/react-query` 5.106.1; Go 1.25; image `postgres:18`.
- Other Expo packages are installed with `npx expo install <pkg>`, never with a hand-picked version.
- TypeScript: **the same version in both apps**, chosen by Task B1 Step 1. Do not assume 7.x.
- Go module path: `github.com/XYZLZ/MyPark/apps/server`.
- Ports (dev): api 8080, worker health **8091**, simulator 8090, web 3000, postgres 5432, mailpit 1025/8025, Expo Metro 8081.
- Error envelope: `{"error":{"code","message","details","request_id"}}`. Codes are `<domain>.<reason>` snake_case and are the contract.
- Locales `es-DO` (default) and `en`. No hardcoded user-facing strings. Key parity is enforced.
- Brand: display name "MyPark · Banreservas Zona Colonial". Brand color tokens are placeholders marked `TODO(brand)`.
- DB conventions: `bigint generated always as identity` PKs, `public_id uuid default uuidv7()`, `text` + `check`, `timestamptz`, indexed FKs, lowercase snake_case. Roles `mypark_migrator` (DDL) and `mypark_app` (DML only).
- Commits: Conventional Commits with scopes `server|web|mobile|contracts|deploy|docs|ci`. **No `Co-Authored-By` or any AI attribution.**
- Commands must run in Git Bash on Windows and in Linux CI: POSIX shell, forward slashes, no PowerShell syntax.
- Scope: M0 only. No auth, vehicles, availability, SSE or simulator scenarios.

## Review Focus

1. **The API is down or unreachable when web/mobile render.** The user expects a localized "API: unavailable" state, not a crash or blank page. Tests: B3 `renders unavailable when fetch rejects`, B4 `shows unavailable on network error`.
2. **The DB is reachable but migrations are behind the binary.** `/readyz` must return 503 `health.not_ready`, not 200. Test: A4 `TestReadyz_MigrationsBehind`.
3. **Seed run twice** (compose restart). It must not duplicate the lot or spaces. Test: A4 `TestSeed_Idempotent`.
4. **Unsupported locale in the URL or from the device** (`/fr`, `fr-FR`). It must fall back to es-DO, not 500 or show untranslated keys. Tests: B3 `resolveLocale falls back to es-DO`, B4 `pickLanguage falls back to es-DO`.
5. **A required env var is missing at boot.** The process must exit non-zero with a message naming the variable, not start half-configured. Test: A1 `TestLoad_MissingDatabaseURL`.

---

## File Structure

```
MyPark/
├─ package.json                      # B1: workspaces, overrides(react 19.3.0), root scripts
├─ .gitignore .editorconfig README.md .nvmrc
├─ scripts/
│  ├─ check-single-react.mjs (+ .test.mjs)   # B5: one react version in npm ls
│  └─ check-i18n-parity.mjs (+ .test.mjs)    # B3: es-DO/en key parity
├─ .github/
│  ├─ pull_request_template.md       # T0: security checklist (policies §2.2)
│  └─ workflows/ci-server.yml        # A5: job "server"
│     workflows/ci-js.yml            # B5: jobs "web", "mobile", "contracts"
├─ deploy/compose/
│  ├─ docker-compose.yml             # A5
│  ├─ .env.example                   # A5
│  └─ postgres/init/01-roles.sh      # A4: creates mypark_migrator / mypark_app
├─ apps/server/
│  ├─ go.mod                         # A1 (tool deps: sqlc)
│  ├─ api/openapi.yaml               # A2: /healthz, /readyz, ErrorEnvelope
│  ├─ cmd/{api,worker,simulator,migrate,seed}/main.go
│  ├─ internal/config/config.go
│  ├─ internal/domain/errcodes/{errcodes.go,gen/main.go}
│  ├─ internal/transport/http/response/response.go
│  ├─ internal/transport/http/middleware/{requestid.go,recover.go,accesslog.go}
│  ├─ internal/features/health/{handler.go,ports.go}
│  ├─ internal/infrastructure/database/{pool.go,migrate.go,sqlc.yaml}
│  ├─ internal/infrastructure/database/migrations/00001_baseline.sql
│  ├─ internal/infrastructure/database/queries/lots.sql
│  ├─ internal/infrastructure/database/dbgen/   # sqlc output (committed)
│  ├─ internal/features/lots/seed.go
│  ├─ internal/testsupport/pgtest.go
│  ├─ Dockerfile  .env.example
├─ packages/contracts/
│  ├─ package.json  tsconfig.json
│  ├─ src/{index.ts,openapi.d.ts(generated),error-codes.ts(generated),health.ts}
│  └─ src/health.test.ts
├─ apps/web/                         # B3 (cognifield client layout)
│  ├─ package.json next.config.ts tsconfig.json vitest.config.ts postcss.config.mjs .env.example
│  └─ src/{app/[locale]/{(public),(driver),(admin)}, app/layout.tsx, components/, design/tokens.css,
│          features/health/, i18n/{routing.ts,request.ts,messages/{es-DO,en}.json}, lib/api/server.ts,
│          providers/query-provider.tsx, store/ui.ts, proxy.ts}
└─ apps/mobile/                      # B4 (nexVial-app layout)
   ├─ package.json app.json metro.config.js tsconfig.json eslint.config.js eas.json AGENTS.md .env.example
   └─ src/{app/{_layout.tsx,index.tsx,(auth)/_layout.tsx}, components/, design/tokens.ts, global.css,
           features/health/, lib/i18n/{index.ts,locales/{es-DO,en}.json}, services/httpClient.ts,
           store/settings.ts, types/}
```

---

## Task T0: Pre-flight — commit docs and create the remote (manual, both)

**Files:** Create `.github/pull_request_template.md`. Existing: `docs/**`, `.gitignore`.

**Interfaces:** Produces the `origin` remote at `github.com/XYZLZ/MyPark` and `main` with the docs. Every other task branches from here.

- [ ] **Step 1: Write the PR template** with three sections: *Summary* (with spec IDs), *Testing*, and a *Security checklist (sensitive paths: migrations, auth, payments, device signatures, capacity)* with these boxes: authz + ownership checks; input validated; idempotency keys; lock order + no network I/O in transactions; no secrets/PII in logs; migration is forward-only and safe; failure-path tests; automated risk review (`review-risk` / `/security-review`) linked.
- [ ] **Step 2: Commit docs**

```bash
git add .gitignore docs .github/pull_request_template.md
git commit -m "docs: add project spec, architecture, policies and M0 plan"
```

- [ ] **Step 3: Push to the existing remote** (the user runs this; `origin` already points to `https://github.com/XYZLZ/MyPark.git`)

```bash
git remote -v   # expect origin https://github.com/XYZLZ/MyPark.git
git push -u origin main
```

Expected: `https://github.com/XYZLZ/MyPark` shows `docs/` on `main`. Branch protection is applied in Task T1, after the CI job names exist.

---

## Task A1: Go module, config, error codes, response envelope — Track A

**Files:**
- Create: `apps/server/go.mod`, `apps/server/internal/config/config.go`, `apps/server/internal/domain/errcodes/errcodes.go`, `apps/server/internal/transport/http/response/response.go`, `apps/server/.env.example`
- Test: `apps/server/internal/config/config_test.go`, `apps/server/internal/transport/http/response/response_test.go`

**Interfaces:**
- Produces:
  - `config.Load(getenv func(string) string) (config.Config, error)` with `Config{Env string; HTTPAddr string; DatabaseURL string; LogLevel slog.Level; CORSAllowedOrigins []string; DBMaxConns int32}`. Defaults: `HTTPAddr=":8080"`, `LogLevel=info`, `DBMaxConns=10`, `Env="development"`. `DATABASE_URL` is required.
  - `type errcodes.Code string`; constants `InternalUnexpected="internal.unexpected"`, `HTTPNotFound="http.not_found"`, `HTTPMethodNotAllowed="http.method_not_allowed"`, `HealthNotReady="health.not_ready"`; `errcodes.All() []Code` (sorted).
  - `response.JSON(w http.ResponseWriter, status int, v any)`; `response.Error(w http.ResponseWriter, r *http.Request, status int, code errcodes.Code, msg string, details map[string]any)` reads the request ID via `middleware.RequestIDFrom(r.Context())`. To avoid an import cycle, `response` takes the ID through `response.RequestIDFunc` (a package var set at wiring); a default returns `""`.

- [ ] **Step 1: Init the module**

Run: `cd apps/server && go mod init github.com/XYZLZ/MyPark/apps/server && go mod edit -go=1.25`
Expected: `go.mod` with `module github.com/XYZLZ/MyPark/apps/server` and `go 1.25`.

- [ ] **Step 2: Write the failing tests**

```go
func TestLoad_MissingDatabaseURL(t *testing.T) {
    _, err := config.Load(func(string) string { return "" })
    if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") { t.Fatalf("want error naming DATABASE_URL, got %v", err) }
}
func TestLoad_Defaults(t *testing.T) {
    c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
    // assert c.HTTPAddr == ":8080", c.DBMaxConns == 10, c.LogLevel == slog.LevelInfo, c.Env == "development"
}
func TestLoad_InvalidMaxConns(t *testing.T) { /* DB_MAX_CONNS="abc" → error contains "DB_MAX_CONNS" */ }
func TestLoad_CORSOriginsSplit(t *testing.T) { /* CORS_ALLOWED_ORIGINS="http://localhost:3000, http://localhost:8081" → 2 trimmed entries */ }

func TestError_WritesEnvelope(t *testing.T) {
    response.RequestIDFunc = func(context.Context) string { return "req-1" }
    rec := httptest.NewRecorder()
    response.Error(rec, httptest.NewRequest("GET", "/x", nil), 503, errcodes.HealthNotReady, "db down", map[string]any{"database": "fail"})
    // assert rec.Code == 503; Content-Type == "application/json"
    // body.error.code == "health.not_ready"; body.error.request_id == "req-1"; body.error.details.database == "fail"
}
```

- [ ] **Step 3: Run to verify they fail**

Run: `cd apps/server && go test ./internal/config/... ./internal/transport/http/response/...`
Expected: FAIL (undefined: config.Load / response.Error).

- [ ] **Step 4: Implement** `config.Load`, `errcodes`, `response.JSON`/`response.Error`. Errors aggregate with `errors.Join` so all missing vars are reported at once.

- [ ] **Step 5: Run to verify they pass**

Run: `cd apps/server && go test ./internal/... && go vet ./...`
Expected: `ok` for both packages, and vet prints nothing.

- [ ] **Step 6: Commit**

```bash
git add apps/server
git commit -m "feat(server): add config loading, error codes and response envelope"
```

---

## Task A2: OpenAPI health contract + error-code export — Track A (**handoff to Track B**)

**Files:**
- Create: `apps/server/api/openapi.yaml`, `apps/server/internal/domain/errcodes/gen/main.go`
- Modify: `apps/server/internal/domain/errcodes/errcodes.go` (add `//go:generate go run ./gen -out ../../../../../packages/contracts/src/error-codes.ts`)
- Test: `apps/server/internal/domain/errcodes/gen/main_test.go`

**Interfaces:**
- Consumes: `errcodes.All()` (A1).
- Produces (the contract Track B builds on):
  - `GET /healthz` → 200 `Health {status: "ok", service: string, version: string}`
  - `GET /readyz` → 200 `Readiness {status: "ready", checks: {database: "ok"|"fail", migrations: "ok"|"behind"|"fail"}}`, or 503 `ErrorEnvelope` with `code: "health.not_ready"` and `details` = the same `checks` object.
  - `components.schemas.ErrorEnvelope {error: {code: string, message: string, details?: object, request_id: string}}`
  - `packages/contracts/src/error-codes.ts`: `export const ERROR_CODES = [...] as const; export type ErrorCode = typeof ERROR_CODES[number];`
  - `gen.Render(codes []errcodes.Code) []byte`.

- [ ] **Step 1: Write openapi.yaml** (OpenAPI 3.1, `servers: [{url: http://localhost:8080}]`, the two paths and three schemas above, `info.version: 0.1.0`).
- [ ] **Step 2: Write the failing test**

```go
func TestRender_DeterministicSortedConst(t *testing.T) {
    out := string(gen.Render([]errcodes.Code{"http.not_found", "health.not_ready"}))
    // assert strings.HasPrefix(out, "// Code generated by errcodes/gen. DO NOT EDIT.")
    // assert index("health.not_ready") < index("http.not_found")
    // assert contains "export type ErrorCode = typeof ERROR_CODES[number];"
    // assert Render(same input) == out (byte-equal)
}
```

- [ ] **Step 3: Run to verify it fails** — `cd apps/server && go test ./internal/domain/errcodes/...` → FAIL (undefined: gen.Render).
- [ ] **Step 4: Implement `Render`** plus a `main` that writes `-out`. Then run `cd apps/server && go generate ./internal/domain/errcodes/...`.
- [ ] **Step 5: Verify** — `go test ./internal/domain/errcodes/...` PASS. `packages/contracts/src/error-codes.ts` exists and contains `"health.not_ready"`.
- [ ] **Step 6: Commit and notify Track B**

```bash
git add apps/server/api apps/server/internal/domain/errcodes packages/contracts/src/error-codes.ts
git commit -m "feat(contracts): define health OpenAPI contract and export error codes"
```

Open this as its own small PR and merge it first. This is the M0 contract handoff (spec §8 "Team and parallel tracks").

---

## Task A3: HTTP server, middleware, /healthz and /readyz — Track A

**Files:**
- Create: `apps/server/internal/transport/http/middleware/{requestid.go,recover.go,accesslog.go}`, `apps/server/internal/features/health/{ports.go,handler.go}`, `apps/server/cmd/api/main.go`
- Test: `apps/server/internal/transport/http/middleware/requestid_test.go`, `apps/server/internal/features/health/handler_test.go`

**Interfaces:**
- Consumes: `config.Load`, `response.*`, `errcodes.*` (A1); the contract (A2).
- Produces:
  - `middleware.RequestID(next http.Handler) http.Handler`: accepts an inbound `X-Request-Id` (≤ 64 chars, `[A-Za-z0-9-]`), otherwise generates a UUIDv7; sets the response header; stores it in the context. `middleware.RequestIDFrom(ctx) string`.
  - `middleware.Recover(log *slog.Logger) func(http.Handler) http.Handler`: on a panic, logs it and returns 500 `internal.unexpected`.
  - `middleware.AccessLog(log *slog.Logger) func(http.Handler) http.Handler`: fields `request_id, method, route, status, duration_ms`.
  - `health.Pinger interface{ Ping(ctx context.Context) error }`; `health.MigrationStatus interface{ Status(ctx context.Context) (current, expected int64, err error) }`.
  - `health.NewHandler(service, version string, p health.Pinger, m health.MigrationStatus) *health.Handler`; `(*Handler).Register(mux *http.ServeMux)` registers `GET /healthz` and `GET /readyz`.
  - `cmd/api`: `run(ctx context.Context, getenv func(string) string, stdout io.Writer) error`. `main` calls it and exits 1 on error. Graceful shutdown on SIGINT/SIGTERM with a 10 s timeout.

- [ ] **Step 1: Write the failing tests**

```go
func TestRequestID_GeneratesWhenMissing(t *testing.T)   // response header X-Request-Id non-empty, len 36
func TestRequestID_KeepsValidInbound(t *testing.T)      // inbound "abc-123" echoed
func TestRequestID_ReplacesInvalidInbound(t *testing.T) // inbound "<script>" replaced by generated id
func TestHealthz_OK(t *testing.T)                       // 200, body {"status":"ok","service":"api","version":"test"}
func TestReadyz_Ready(t *testing.T)                     // fake pinger nil, migrations (3,3) → 200 checks.database=="ok", checks.migrations=="ok"
func TestReadyz_DBDown(t *testing.T)                    // pinger error → 503, error.code=="health.not_ready", details.database=="fail"
func TestReadyz_MigrationsBehind(t *testing.T)          // migrations (2,3) → 503, details.migrations=="behind"
func TestUnknownRoute_ReturnsEnvelope(t *testing.T)     // GET /nope via full mux → 404 error.code=="http.not_found"
```

- [ ] **Step 2: Run to verify they fail** — `cd apps/server && go test ./internal/...` → FAIL (undefined symbols).
- [ ] **Step 3: Implement** the middleware, the health handler and `cmd/api/run`. The chain order is recover → request ID → access log → mux. A catch-all `/` handler returns `http.not_found`. `cmd/api` wires `database.NewPool` (A4). Until A4 lands, it compiles against the `health` interfaces with a stub that returns an error.
- [ ] **Step 4: Run to verify they pass** — `go test ./internal/... && go vet ./...` → all `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add api entrypoint with request id, recovery and health endpoints"
```

---

## Task A4: Database — roles, baseline migration, sqlc, migrate and seed — Track A

**Files:**
- Create: `deploy/compose/postgres/init/01-roles.sh`, `apps/server/internal/infrastructure/database/{pool.go,migrate.go,sqlc.yaml}`, `.../migrations/00001_baseline.sql`, `.../queries/lots.sql`, `.../dbgen/*` (generated), `apps/server/internal/features/lots/seed.go`, `apps/server/internal/testsupport/pgtest.go`, `apps/server/cmd/migrate/main.go`, `apps/server/cmd/seed/main.go`
- Modify: `apps/server/cmd/api/main.go` (real Pinger + MigrationStatus), `apps/server/go.mod` (`go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc@latest`, then pin the resolved version)
- Test: `apps/server/internal/infrastructure/database/migrate_test.go`, `apps/server/internal/features/lots/seed_test.go`

**Interfaces:**
- Consumes: `health.Pinger`, `health.MigrationStatus` (A3).
- Produces:
  - `database.NewPool(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error)`: sets `statement_timeout=5s` and `application_name` via `ConnConfig.RuntimeParams`.
  - `database.MigrationsFS embed.FS`; `database.MigrateUp(ctx context.Context, db *sql.DB) error` (goose, dialect postgres); `database.NewMigrationStatus(db *sql.DB) health.MigrationStatus` (expected = highest embedded version).
  - `lots.Seed(ctx context.Context, q *dbgen.Queries) (lotID int64, err error)`: idempotent.
  - `testsupport.NewDB(t *testing.T) *pgxpool.Pool`: creates a fresh database `mypark_test_<random>` from `TEST_DATABASE_URL`, runs `MigrateUp`, and drops it in `t.Cleanup`. It calls `t.Skip` when `TEST_DATABASE_URL` is unset locally and `t.Fatal` when `CI=true`.
  - sqlc queries: `UpsertLotBySlug :one`, `InsertSpace :exec` (`on conflict (lot_id, code) do nothing`), `CountSpacesByLot :one`, `InitLotOccupancy :exec` (`on conflict do nothing`).
- Schema (`00001_baseline.sql`, goose `-- +goose Up` only; forward-only):
  - `create extension if not exists citext; create extension if not exists btree_gist;`
  - `lots(id bigint identity pk, public_id uuid not null default uuidv7() unique, slug text not null unique, name text not null, address text not null, latitude numeric(9,6), longitude numeric(9,6), timezone text not null default 'America/Santo_Domingo', capacity int not null check (capacity > 0), status text not null default 'open' check (status in ('open','closed')), opening_hours jsonb not null default '{"always_open": true}', created_at timestamptz not null default now(), updated_at timestamptz not null default now())`
  - `spaces(id bigint identity pk, lot_id bigint not null references lots(id), code text not null, kind text not null default 'standard' check (kind in ('standard','accessible')), status text not null default 'free' check (status in ('free','occupied','out_of_service')), sensor_updated_at timestamptz, unique (lot_id, code))` + `create index spaces_lot_id_status_idx on spaces (lot_id, status)`
  - `lot_occupancy(lot_id bigint primary key references lots(id), occupied int not null default 0 check (occupied >= 0), version bigint not null default 0, updated_at timestamptz not null default now())`
  - `revoke all on schema public from public; grant usage on schema public to mypark_app; alter default privileges for role mypark_migrator in schema public grant select, insert, update, delete on tables to mypark_app; alter default privileges ... grant usage, select on sequences to mypark_app;` plus explicit grants on the three tables.
- Seed values: slug `zona-colonial`, name `Parqueo Banreservas Zona Colonial`, address `Calle Arzobispo Meriño No. 386, Zona Colonial, Santo Domingo`, capacity 280, spaces `E001`…`E280`, lat/long `NULL` (see spec gaps).
- `01-roles.sh` (runs as the postgres superuser at container init): creates `mypark_migrator` (LOGIN, owner of `$POSTGRES_DB`) and `mypark_app` (LOGIN, no DDL) with passwords from `MYPARK_MIGRATOR_PASSWORD` / `MYPARK_APP_PASSWORD`. Uses `set -eu` and `psql -v ON_ERROR_STOP=1`.

- [ ] **Step 1: Write the failing tests**

```go
func TestMigrateUp_CreatesBaseline(t *testing.T) {
    db := testsupport.NewDB(t)
    // assert to_regclass('public.lots'), ('public.spaces'), ('public.lot_occupancy') all non-null
}
func TestMigrationStatus_UpToDate(t *testing.T) { /* current == expected == 1 */ }
func TestReadyz_MigrationsBehind_Integration(t *testing.T) { /* fresh DB without MigrateUp → Status returns current 0 < expected 1 */ }
func TestSpaces_StatusCheckRejectsUnknown(t *testing.T) { /* update spaces set status='parked' → SQLSTATE 23514 */ }
func TestSeed_Creates280Spaces(t *testing.T) {
    // lots.Seed → CountSpacesByLot == 280; first code "E001", last "E280"; lot_occupancy row occupied == 0
}
func TestSeed_Idempotent(t *testing.T) { /* Seed twice → same lotID, count still 280, 1 row in lots */ }
```

- [ ] **Step 2: Run to verify they fail**

Run: `docker run -d --name mypark-pg-test -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:18`, then `cd apps/server && TEST_DATABASE_URL=postgres://postgres:test@localhost:55432/postgres?sslmode=disable go test ./internal/infrastructure/database/... ./internal/features/lots/...`
Expected: FAIL (undefined: database.MigrateUp, lots.Seed).

- [ ] **Step 3: Write the migration, queries and `sqlc.yaml`** (engine postgresql, `sql_package: pgx/v5`, out `dbgen`). Then run `cd apps/server && go tool sqlc generate -f internal/infrastructure/database/sqlc.yaml`. Expected: `dbgen/` files generated, no errors.
- [ ] **Step 4: Implement** `NewPool`, `MigrateUp`, `NewMigrationStatus`, `lots.Seed` (in one transaction), `testsupport.NewDB`, `cmd/migrate` (`run` connects with `MIGRATOR_DATABASE_URL`) and `cmd/seed`. Wire the real health ports in `cmd/api`.
- [ ] **Step 5: Run to verify they pass** — same command as Step 2 → all PASS. Then `go tool sqlc diff -f internal/infrastructure/database/sqlc.yaml` prints nothing.
- [ ] **Step 6: Commit**

```bash
git add apps/server deploy/compose/postgres
git commit -m "feat(server): add baseline schema, db roles, migrations and lot seed"
```

---

## Task A5: Worker/simulator stubs, Dockerfile, compose, server CI — Track A

**Files:**
- Create: `apps/server/cmd/worker/main.go`, `apps/server/cmd/simulator/main.go`, `apps/server/Dockerfile`, `apps/server/.dockerignore`, `deploy/compose/docker-compose.yml`, `deploy/compose/.env.example`, `.github/workflows/ci-server.yml`
- Test: `apps/server/cmd/worker/main_test.go`, `apps/server/cmd/simulator/main_test.go`

**Interfaces:**
- Consumes: `health.NewHandler` (A3), `database.*` (A4).
- Produces:
  - `cmd/worker`: `run(ctx, getenv, stdout) error`. Serves `/healthz` + `/readyz` on `WORKER_HEALTH_ADDR` (default `:8091`). No jobs yet.
  - `cmd/simulator`: `run(ctx, getenv, stdout) error`. Serves `/healthz` on `SIMULATOR_ADDR` (default `:8090`) with `service:"simulator"`. No scenarios yet.
  - Dockerfile: multi-stage `golang:1.25` → `gcr.io/distroless/static-debian12:nonroot`. `ARG CMD` selects the binary (`api|worker|simulator|migrate|seed`). `CGO_ENABLED=0`. Version is injected via `-ldflags "-X main.version=$VERSION"`.
  - Compose services: `postgres` (postgres:18, init dir mounted, healthcheck `pg_isready`, volume `pgdata`); `mailpit` (axllent/mailpit, 1025/8025); `migrate` and `seed` (one-shot, `depends_on: postgres: service_healthy`, `restart: "no"`); `api` (8080, `depends_on` migrate+seed `service_completed_successfully`, healthcheck on `/readyz` using the binary's `-healthcheck` flag, since distroless has no curl); `worker` (8091); `simulator` (8090, internal network + published port for dev); `web` (built in B3, port 3000, `API_INTERNAL_URL=http://api:8080`).
  - CI job **`server`** (name is used by branch protection): `services.postgres: postgres:18`; steps: checkout → setup-go 1.25 (cache) → run `deploy/compose/postgres/init/01-roles.sh` via psql → `go vet ./...` → golangci-lint (official action, pinned major) → `go tool sqlc diff` → `go generate ./internal/domain/errcodes/...` + `git diff --exit-code packages/contracts/src/error-codes.ts` → `go run ./cmd/migrate` → `go test -race ./...` with `TEST_DATABASE_URL` and `CI=true`.

- [ ] **Step 1: Write the failing tests**

```go
func TestWorker_HealthzServes(t *testing.T)    // start run() on :0 via WORKER_HEALTH_ADDR=127.0.0.1:0, GET /healthz → 200 service=="worker"; cancel ctx → run returns nil
func TestSimulator_HealthzServes(t *testing.T) // same, service=="simulator"
func TestAPI_HealthcheckFlag(t *testing.T)     // run with args ["-healthcheck"] against httptest server returning 200 → exit nil; 503 → error
```

- [ ] **Step 2: Run to verify they fail** — `cd apps/server && go test ./cmd/...` → FAIL.
- [ ] **Step 3: Implement** the stubs, the `-healthcheck` flag (shared helper `internal/transport/http/healthprobe.Probe(url string) error`), the Dockerfile, compose and the CI workflow.
- [ ] **Step 4: Verify tests** — `go test ./cmd/...` → PASS.
- [ ] **Step 5: Verify compose (Track A services only)**

Run: `cp deploy/compose/.env.example deploy/compose/.env && docker compose -f deploy/compose/docker-compose.yml up -d --build postgres mailpit migrate seed api worker simulator && curl -s localhost:8080/readyz`
Expected: `{"status":"ready","checks":{"database":"ok","migrations":"ok"}}`. `docker compose ps` shows api, worker and simulator `healthy`, and migrate and seed `exited (0)`.

- [ ] **Step 6: Commit**

```bash
git add apps/server deploy/compose .github/workflows/ci-server.yml
git commit -m "build(deploy): add compose stack, server dockerfile and server ci"
```

---

## Task B1: Root npm workspace + TypeScript version decision — Track B

**Files:**
- Create: `package.json`, `.editorconfig`, `.nvmrc`, `README.md`
- Modify: `.gitignore` (add `node_modules/`, `.next/`, `.expo/`, `dist/`, `coverage/`, `.env`, `.env.*`, `!.env.example`)

**Interfaces:**
- Produces:
  - Root `package.json`: `"private": true`, `"workspaces": ["apps/web","apps/mobile","packages/contracts"]`, `"overrides": {"react":"19.3.0","react-dom":"19.3.0"}`, `"engines": {"node": ">=22"}`, scripts `check:react`, `check:i18n`, `test:scripts` (`node --test scripts/`), `gen:contracts` (`npm run gen -w packages/contracts`).
  - Shared decision: `TS_VERSION`, written into `README.md` "Toolchain" and used by B2/B3/B4.

- [ ] **Step 1: Determine the TypeScript version** (in a throwaway folder outside the repo)

```bash
mkdir -p "$TMPDIR/ts-probe" && cd "$TMPDIR/ts-probe"
npx --yes create-next-app@16.4.5 probe-web --ts --tailwind --app --src-dir --use-npm --eslint --no-import-alias --yes
node -p "require('./probe-web/package.json').devDependencies.typescript"
npx --yes create-expo-app@latest probe-mobile --template default@sdk-58 --no-install
node -p "require('./probe-mobile/package.json').devDependencies.typescript"
```

Expected: two version ranges. Pick the **highest version satisfying both** (Expo's range wins on conflict) and pin it exactly, e.g. `5.9.x`/`6.0.x`. Record it in `README.md` as `TypeScript: <x.y.z> (Expo SDK 58 + Next 16.4 compatible)`. Delete the probe folder.

- [ ] **Step 2: Write the root files** (README: purpose, toolchain table, `docker compose` quick path, repo layout, links to docs).
- [ ] **Step 3: Verify** — `npm install` at the root succeeds with no workspaces yet (warns are fine), and `node -p "require('./package.json').overrides.react"` prints `19.3.0`.
- [ ] **Step 4: Commit**

```bash
git add package.json .editorconfig .nvmrc README.md .gitignore
git commit -m "build: add npm workspace root and toolchain readme"
```

---

## Task B2: packages/contracts — generated types + health schema — Track B (depends on A2)

**Files:**
- Create: `packages/contracts/{package.json,tsconfig.json,vitest.config.ts}`, `packages/contracts/src/{index.ts,openapi.d.ts,health.ts}`
- Test: `packages/contracts/src/health.test.ts`

**Interfaces:**
- Consumes: `apps/server/api/openapi.yaml`, `packages/contracts/src/error-codes.ts` (A2).
- Produces (package name `@mypark/contracts`):
  - script `gen`: `openapi-typescript ../../apps/server/api/openapi.yaml -o src/openapi.d.ts`
  - `export type { paths, components } from './openapi'`
  - `export { ERROR_CODES, type ErrorCode } from './error-codes'`
  - `healthSchema` / `readinessSchema` / `errorEnvelopeSchema` (zod 4) and `type ApiHealthState = 'healthy' | 'not_ready' | 'unavailable'`
  - `toHealthState(input: { status: number; body: unknown } | { networkError: true }): ApiHealthState`

- [ ] **Step 1: Write the failing test**

```ts
test('toHealthState maps 200 ready to healthy', () =>
  expect(toHealthState({ status: 200, body: { status: 'ready', checks: { database: 'ok', migrations: 'ok' } } })).toBe('healthy'))
test('toHealthState maps 503 envelope to not_ready', () =>
  expect(toHealthState({ status: 503, body: { error: { code: 'health.not_ready', message: 'x', request_id: 'r' } } })).toBe('not_ready'))
test('toHealthState maps network error to unavailable', () =>
  expect(toHealthState({ networkError: true })).toBe('unavailable'))
test('toHealthState maps malformed 200 body to unavailable', () =>
  expect(toHealthState({ status: 200, body: { foo: 1 } })).toBe('unavailable'))
test('ERROR_CODES contains health.not_ready', () => expect(ERROR_CODES).toContain('health.not_ready'))
```

- [ ] **Step 2: Run to verify it fails** — `npm install && npm test -w packages/contracts` → FAIL (cannot find `./health`).
- [ ] **Step 3: Implement** — install `zod@4.6.7`, `openapi-typescript` (pinned) and `vitest` (pinned) with `-w packages/contracts -E`. Run `npm run gen -w packages/contracts` and write `health.ts`.
- [ ] **Step 4: Verify** — `npm test -w packages/contracts` PASS. `npm run gen -w packages/contracts && git diff --exit-code packages/contracts/src/openapi.d.ts` shows no diff.
- [ ] **Step 5: Commit**

```bash
git add packages/contracts package.json package-lock.json
git commit -m "feat(contracts): add generated api types and health state mapping"
```

---

## Task B3: Web scaffold — Next.js 16, i18n, tokens, health page — Track B (depends on B2)

**Files:**
- Create: `apps/web/**` per the File Structure (route groups `(public)`, `(driver)`, `(admin)` with placeholder `layout.tsx` each), `apps/web/Dockerfile`, `scripts/check-i18n-parity.mjs`, `scripts/check-i18n-parity.test.mjs`
- Modify: `deploy/compose/docker-compose.yml` (`web` service build context)
- Test: `apps/web/src/features/health/HealthStatus.test.tsx`, `apps/web/src/i18n/routing.test.ts`, `scripts/check-i18n-parity.test.mjs`

**Interfaces:**
- Consumes: `@mypark/contracts` (`toHealthState`, `ApiHealthState`).
- Produces:
  - `src/i18n/routing.ts`: `locales = ['es-DO','en'] as const`, `defaultLocale = 'es-DO'`, `resolveLocale(input: string | undefined): Locale`, next-intl `defineRouting({ locales, defaultLocale, localePrefix: 'always' })`.
  - `src/proxy.ts` (Next 16): next-intl middleware only (session refresh comes in M1).
  - `src/lib/api/server.ts`: `getApiHealth(): Promise<ApiHealthState>` (server-only; `fetch(`${process.env.API_INTERNAL_URL}/readyz`, { cache: 'no-store', signal: AbortSignal.timeout(2000) })`).
  - `src/features/health/HealthStatus.tsx`: presentational `HealthStatus({ state }: { state: ApiHealthState })` with `role="status"`. The container is the `[locale]/(public)/page.tsx` server component.
  - `src/providers/query-provider.tsx`: `QueryProvider` (TanStack Query client per browser session). `src/store/ui.ts`: `createUiStore()` zustand vanilla store factory `{ theme: 'system'|'light'|'dark'; setTheme }`, provided via context (per-request, as architecture §12 requires).
  - `src/design/tokens.css`: Tailwind 4 `@theme` with `--color-brand-primary`, `--color-brand-primary-contrast`, `--color-brand-accent`, `--color-brand-accent-contrast`, each with `/* TODO(brand): replace with official Banreservas value */`.
  - Messages `common.appName = "MyPark · Banreservas Zona Colonial"`; `health.healthy` en `"API: healthy"` / es-DO `"API: operativa"`; `health.not_ready` en `"API: starting"` / es-DO `"API: iniciando"`; `health.unavailable` en `"API: unavailable"` / es-DO `"API: no disponible"`.
  - `scripts/check-i18n-parity.mjs`: `export function diffKeys(a: object, b: object): { missingInA: string[]; missingInB: string[] }` plus a CLI over `apps/web/src/i18n/messages` and `apps/mobile/src/lib/i18n/locales`. Exits 1 on any difference.

- [ ] **Step 1: Scaffold** — `npx --yes create-next-app@16.4.5 apps/web --ts --tailwind --app --src-dir --use-npm --eslint --import-alias "@/*" --yes`. Then pin `next@16.4.5 react@19.3.0 react-dom@19.3.0 tailwindcss@4.3.5 typescript@$TS_VERSION` with `npm i -E -w apps/web`, and add `next-intl @tanstack/react-query@5.106.1 zustand@5.1.0 zod@4.6.7 @mypark/contracts@*` plus dev `vitest @testing-library/react @testing-library/jest-dom jsdom @vitejs/plugin-react`. Remove the nested lockfile so only the root `package-lock.json` exists.
- [ ] **Step 2: Write the failing tests**

```ts
// HealthStatus.test.tsx (render inside NextIntlClientProvider with each locale's messages)
test('renders healthy in en', () => { /* state='healthy', locale en → getByRole('status') has text "API: healthy" */ })
test('renders healthy in es-DO', () => { /* → "API: operativa" */ })
test('renders unavailable when fetch rejects', async () => {
  /* vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('fetch failed'))); expect(await getApiHealth()).toBe('unavailable') */ })
// routing.test.ts
test('resolveLocale falls back to es-DO', () => { expect(resolveLocale('fr')).toBe('es-DO'); expect(resolveLocale(undefined)).toBe('es-DO'); expect(resolveLocale('en')).toBe('en') })
// scripts/check-i18n-parity.test.mjs (node:test)
test('diffKeys reports nested missing keys', () => assert.deepEqual(diffKeys({ a: { b: 1, c: 1 } }, { a: { b: 1 } }), { missingInA: [], missingInB: ['a.c'] }))
```

- [ ] **Step 3: Run to verify they fail** — `npm test -w apps/web && node --test scripts/` → FAIL.
- [ ] **Step 4: Implement** the routing, proxy, messages, tokens, header with `common.appName`, `HealthStatus`, `getApiHealth`, providers and store factory. Also add `apps/web/Dockerfile` (Next `output: 'standalone'`, `node:22-alpine`).
- [ ] **Step 5: Verify**

Run: `npm test -w apps/web && node --test scripts/ && npm run lint -w apps/web && npx -w apps/web tsc --noEmit && npm run build -w apps/web`
Expected: all pass; the build prints the route list including `/[locale]`.

- [ ] **Step 6: Verify against the running API** — with A5's stack up, `API_INTERNAL_URL=http://localhost:8080 npm run dev -w apps/web`. `http://localhost:3000/es-DO` shows "API: operativa", `/en` shows "API: healthy", and `/fr` redirects to `/es-DO`.
- [ ] **Step 7: Commit**

```bash
git add apps/web scripts package.json package-lock.json deploy/compose/docker-compose.yml
git commit -m "feat(web): scaffold next.js app with i18n, brand tokens and api health page"
```

---

## Task B4: Mobile scaffold — Expo SDK 58, Uniwind, i18n, health screen — Track B (depends on B2)

**Files:**
- Create: `apps/mobile/**` per the File Structure, `apps/mobile/AGENTS.md` (pointing to `https://docs.expo.dev/versions/v58.0.0/`), `apps/mobile/jest.config.js`
- Test: `apps/mobile/src/features/health/HealthStatus.test.tsx`, `apps/mobile/src/lib/i18n/pickLanguage.test.ts`, `apps/mobile/src/services/httpClient.test.ts`

**Interfaces:**
- Consumes: `@mypark/contracts` (`toHealthState`, `ApiHealthState`).
- Produces:
  - `src/services/httpClient.ts`: `createHttpClient(baseUrl: string, fetchImpl = fetch) → { get(path: string, init?: { timeoutMs?: number }): Promise<{ status: number; body: unknown }> }` (rejects on network errors). `getApiHealth(client): Promise<ApiHealthState>`. Base URL comes from `process.env.EXPO_PUBLIC_API_URL`.
  - `src/lib/i18n/index.ts`: `pickLanguage(deviceTags: string[]): 'es-DO' | 'en'` (first tag whose language is `es` → `es-DO`, `en` → `en`, else `es-DO`), and `initI18n(lang)` with i18next + react-i18next.
  - `src/store/settings.ts`: zustand `useSettingsStore` `{ language: 'es-DO'|'en'|null; setLanguage }` (persistence comes in M1).
  - `src/features/health/HealthStatus.tsx`: presentational `HealthStatus({ state })` with `accessibilityRole="text"` and `accessibilityLiveRegion="polite"`. The container is `src/app/index.tsx` using TanStack Query `useQuery({ queryKey: ['health'], queryFn })`.
  - `src/global.css`: `@import 'tailwindcss'; @import 'uniwind';` + `@theme` with the same four brand tokens as web (`TODO(brand)`). `metro.config.js`: `withUniwindConfig(config, { cssEntryFile: './src/global.css', dtsFile: './src/types/uniwind-types.d.ts' })`.
  - `app.json`: `name: "MyPark · Banreservas Zona Colonial"`, `slug: "mypark"`, `scheme: "mypark"`. `eas.json` profiles `development`, `preview` (Android APK), `production`.
  - Locale files with the same keys and values as the web `common.appName` / `health.*` messages.

- [ ] **Step 1: Scaffold** — `npx --yes create-expo-app@latest apps/mobile --template default@sdk-58 --no-install`. Set `name` to `@mypark/mobile`, then `npm install` at the root. Then `cd apps/mobile && npx expo install expo-router expo-localization expo-secure-store jest-expo` and `npm i -E -w apps/mobile uniwind@1.13.2 tailwindcss@4.3.5 zustand@5.1.0 @tanstack/react-query@5.106.1 zod@4.6.7 i18next react-i18next @mypark/contracts@*` plus dev `@testing-library/react-native typescript@$TS_VERSION`. Confirm `react` is `19.3.0` and `react-native` is `0.88.1` in `apps/mobile/package.json`.
- [ ] **Step 2: Write the failing tests**

```ts
test('pickLanguage falls back to es-DO', () => { expect(pickLanguage(['fr-FR'])).toBe('es-DO'); expect(pickLanguage([])).toBe('es-DO') })
test('pickLanguage maps es-MX to es-DO and en-US to en', () => { expect(pickLanguage(['es-MX'])).toBe('es-DO'); expect(pickLanguage(['en-US'])).toBe('en') })
test('shows unavailable on network error', async () => {
  const client = createHttpClient('http://x', jest.fn().mockRejectedValue(new TypeError('Network request failed')))
  expect(await getApiHealth(client)).toBe('unavailable') })
test('renders healthy in es-DO', () => { /* render with i18n es-DO → getByText('API: operativa') */ })
test('renders healthy in en', () => { /* → getByText('API: healthy') */ })
```

- [ ] **Step 3: Run to verify they fail** — `npm test -w apps/mobile` → FAIL.
- [ ] **Step 4: Implement** the files above. `src/app/_layout.tsx` wraps the app in `QueryClientProvider` + `I18nextProvider` and imports `../global.css`. Add an `(auth)` group placeholder layout.
- [ ] **Step 5: Verify**

Run: `npm test -w apps/mobile && npm run lint -w apps/mobile && npx -w apps/mobile tsc --noEmit && (cd apps/mobile && npx expo install --check)`
Expected: tests pass, no lint/type errors, and expo prints `Dependencies are up to date`.

- [ ] **Step 6: Verify on device** — with A5's stack up, `cd apps/mobile && EXPO_PUBLIC_API_URL=http://<LAN-IP>:8080 npx expo start`. Expo Go / a dev client shows "API: operativa" (device in Spanish) or "API: healthy" (device in English).
- [ ] **Step 7: Commit**

```bash
git add apps/mobile package.json package-lock.json
git commit -m "feat(mobile): scaffold expo sdk 58 app with uniwind, i18n and api health screen"
```

---

## Task B5: JS CI + single-React guard — Track B

**Files:**
- Create: `scripts/check-single-react.mjs`, `scripts/check-single-react.test.mjs`, `.github/workflows/ci-js.yml`

**Interfaces:**
- Consumes: workspace scripts from B2–B4; `scripts/check-i18n-parity.mjs` (B3).
- Produces:
  - `export function collectVersions(tree: object, name: string): string[]` (unique, sorted, walking `npm ls --all --json` output) plus a CLI that runs `npm ls react react-dom --all --json`. It exits 1 unless each of `react` and `react-dom` resolves to exactly `["19.3.0"]`.
  - CI jobs (names used by branch protection): **`contracts`** (`npm ci`, `npm run gen -w packages/contracts`, `git diff --exit-code packages/contracts/src`, `npm test -w packages/contracts`); **`web`** (`npm ci`, `npm run check:react`, `npm run check:i18n`, `node --test scripts/`, lint, `tsc --noEmit`, `npm test -w apps/web`, `npm run build -w apps/web`); **`mobile`** (`npm ci`, `npm run check:react`, lint, `tsc --noEmit`, `npm test -w apps/mobile`, `cd apps/mobile && npx expo install --check`). Runs on `ubuntu-latest`, `actions/setup-node` with the Node version from `.nvmrc` and npm cache.

- [ ] **Step 1: Write the failing test**

```js
test('collectVersions finds nested duplicates', () => {
  const tree = { dependencies: { react: { version: '19.3.0' }, 'apps-mobile': { dependencies: { react: { version: '19.2.3' } } } } }
  assert.deepEqual(collectVersions(tree, 'react'), ['19.2.3', '19.3.0'])
})
test('collectVersions returns empty when absent', () => assert.deepEqual(collectVersions({}, 'react'), []))
```

- [ ] **Step 2: Run to verify it fails** — `node --test scripts/` → FAIL (cannot find export).
- [ ] **Step 3: Implement** the script and `ci-js.yml`. Add `"check:react": "node scripts/check-single-react.mjs"` and `"check:i18n": "node scripts/check-i18n-parity.mjs"` to the root.
- [ ] **Step 4: Verify** — `node --test scripts/ && npm run check:react && npm run check:i18n`. Expected: tests pass, `react 19.3.0 ✓ react-dom 19.3.0 ✓`, `i18n keys in parity`.
- [ ] **Step 5: Commit**

```bash
git add scripts .github/workflows/ci-js.yml package.json
git commit -m "ci: add js ci jobs with single-react and i18n parity guards"
```

---

## Task T1: Branch protection + M0 exit verification (manual, both)

**Files:** none (GitHub settings); `docs/` if any decision changed during M0.

**Interfaces:** Consumes the CI job names `server` (A5) and `contracts`, `web`, `mobile` (B5). Each must have run at least once on a PR so GitHub knows them.

- [ ] **Step 1: Apply branch protection** (the user runs this in Git Bash)

```bash
gh api -X PUT repos/XYZLZ/MyPark/branches/main/protection \
  -H "Accept: application/vnd.github+json" --input - <<'JSON'
{
  "required_status_checks": { "strict": true, "contexts": ["server", "contracts", "web", "mobile"] },
  "enforce_admins": true,
  "required_pull_request_reviews": { "required_approving_review_count": 1, "dismiss_stale_reviews": true },
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false
}
JSON
```

Expected: JSON echo containing `"required_approving_review_count": 1` and the four contexts.

- [ ] **Step 2: Verify that direct pushes are rejected** — `git commit --allow-empty -m "chore: verify protection" && git push origin main`. Expected: `remote: error: GH006: Protected branch update failed`. Then `git reset --hard HEAD~1`.
- [ ] **Step 3: Clean-clone check** — in a fresh folder, clone, then `cp deploy/compose/.env.example deploy/compose/.env && docker compose -f deploy/compose/docker-compose.yml up -d --build`. Expected: all services healthy; `curl -s localhost:8080/readyz` returns ready; `localhost:3000/es-DO` shows "API: operativa" with the header "MyPark · Banreservas Zona Colonial"; Mailpit UI at `localhost:8025`.
- [ ] **Step 4: Tick the spec M0 exit criteria** (spec §8 M0). If something deviated (e.g. the worker port moved to 8091, or CI split into two workflow files), update `docs/architecture.md` §2.1 and spec §8 in a `docs:` PR.

---

## Track dependencies

| Task | Track | Depends on |
|---|---|---|
| T0 | both (manual) | — |
| A1 | A | T0 |
| A2 | A | A1 — **handoff to B2** |
| A3 | A | A1, A2 |
| A4 | A | A3 |
| A5 | A | A4 (B3 adds the `web` service later) |
| B1 | B | T0 |
| B2 | B | B1, A2 |
| B3 | B | B2 (live check needs A5) |
| B4 | B | B2 (live check needs A5) |
| B5 | B | B3, B4 |
| T1 | both (manual) | A5, B5 |

## Self-review notes

- **Spec coverage (M0 items 0–8):** 0 → T0/T1; 1 → B1; 2 → A1/A3; 3 → A4; 4 → A5 (+ web in B3); 5 → B3; 6 → B4; 7 → A2/B2; 8 → A5/B5. Exit criteria → T1.
- **Type consistency:** `ApiHealthState`/`toHealthState` (B2) are used by B3/B4; `health.Pinger`/`MigrationStatus` (A3) are implemented in A4; CI job names are the same in A5, B5 and T1.
- **Deviations to sync into the docs in T1 Step 4:** worker health port 8091 (Metro owns 8081); CI split into `ci-server.yml` + `ci-js.yml` (the spec says `ci.yml`) so the two tracks don't edit one file; DB roles are created by a Postgres init script rather than a migration (a migration can't create roles without superuser).
