# M1 Track A — Backend Auth + Vehicles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go side of M1: one response envelope for every endpoint, the published M1 contract, accounts, sessions with rotating refresh tokens, role and ownership authorization, vehicles, the email outbox delivered to Mailpit, the append-only audit log, rate limits across instances, the first-admin seed, and the M0 follow-ups. Each part is covered by the tests that prove the M1 exit criteria on the backend.

**Architecture:**
- Features follow architecture §4.1: domain files, `service.go`, `ports.go`, `http.go` and a `postgres/` adapter over sqlc `dbgen`. Features talk to each other only through exported use-case interfaces.
- `router.Router` requires a `Policy` per route and builds a per-route chain: ClientIP → IP rate limit → Authn → role gate → legal gate → user rate limit → handler.
- Every use case that writes runs inside `database.TxRunner`. Writes that must survive a failing request commit first and return the error after the commit.

**Tech Stack:** Go 1.27, stdlib `net/http` ServeMux, pgx/v5, sqlc, goose, PostgreSQL 18, go-playground/validator v10, `golang.org/x/crypto/argon2`, `github.com/golang-jwt/jwt/v5`, `github.com/google/uuid`, `net/smtp`, Mailpit `axllent/mailpit:v1.33.0`, openapi-typescript and zod 4 in `packages/contracts`.

**Spec:** `docs/superpowers/specs/2026-11-07-m1-backend-auth-vehicles-design.md` (referred to below as "spec §N"). Executors keep it open. This plan points to its SQL and column tables instead of copying them.

## Global Constraints

- Go 1.27, module `github.com/XYZLZ/MyPark/apps/server`. Use modern Go throughout:
  - `errors.AsType`, `t.Context()`, `sync.OnceValue`, `range` over int, `min`/`max`, `slices`/`maps`, `cmp.Or`, `wg.Go`, `math/rand/v2`.
  - Never use `math/rand` for secrets. Codes and tokens come from `crypto/rand`.
- HTTP uses stdlib `ServeMux` method+path patterns only, with no third-party router. Global chain: `Recover → RequestID → AccessLog → SecureHeaders → CORS → JSONErrors(mux)`. Everything else is per route (spec §4.4, OC-2).
- **Response envelope (spec §6.0, user decision):**
  - Success: `{"data":…, "meta":{"request_id"}}`.
  - List: `{"data":[…], "meta":{"request_id","page":{"limit","next_cursor"}}}`.
  - Error: `{"error":{"code","message","details"}, "meta":{"request_id"}}`. `details` is always an object.
  - `204` has no body. `meta.request_id` equals the `X-Request-Id` header.
  - Handlers write only through `response.OK`, `response.List`, `response.NoContent` or `httpx.WriteError`, never through `json.Encoder`.
- The spec §6.1 **Success** column is the `data` payload. Lists put the array directly in `data`, so there is no `items` key. Vehicles send `meta.page = {limit: MAX_VEHICLES_PER_USER, next_cursor: null}`.
- Data access:
  - pgx/v5 with sqlc. Queries live in `internal/infrastructure/database/queries/<feature>.sql` and generated code in `dbgen` (committed).
  - Regenerate with `go tool sqlc generate -f internal/infrastructure/database/sqlc.yaml`. CI runs `sqlc diff`.
  - sqlc maps `uuid` columns to `github.com/google/uuid.UUID` through an `overrides` entry in `sqlc.yaml`.
- **Time:** every time comparison in a query takes `@now` from `clock.Clock`. That covers code expiry, refresh expiry, session absolute expiry, `date_bin` rate windows, outbox `available_at` and purge cut-offs. `now()` appears only in column defaults. This is what lets tests use `testsupport.FakeClock` with no sleeps (spec §15).
- Migrations are goose and **forward-only**. They run as `mypark_migrator`, and the app runs as `mypark_app`. Files are `00002_db_privileges.sql`, `00003_identity.sql`, `00004_audit_outbox.sql`, `00005_vehicles.sql` and `00006_rate_limits.sql`. Follow `supabase-postgres-best-practices`: identity PKs, `public_id uuid default uuidv7()`, `text` + `check`, `timestamptz`, indexed FKs, partial indexes.
- Validation uses `go-playground/validator/v10` through `internal/infrastructure/validation`. Failures return `422 validation.failed` with `details.fields[{field,rule,param}]`, and the value is never echoed.
- Error codes:
  - New codes go into `internal/domain/errcodes/errcodes.go`.
  - `go generate ./internal/domain/errcodes/...` exports them to `packages/contracts/src/error-codes.ts`.
  - `npm run gen -w packages/contracts` regenerates `src/openapi.d.ts`.
  - CI fails on drift in either file. Codes are never removed.
- Names are always `first_name` (1–60) and `last_name` (1–80, holds both surnames), never `full_name`. This applies everywhere: DB, DTOs, validation, anonymization, audit field names, emails and seed env.
- Configuration:
  - No `.env` files. Compose uses inline `${VAR:-dev-default}`.
  - Config accessors sit over unexported fields. `String()`, `GoString()` and `LogValue()` are redacted.
  - The known dev defaults for `JWT_SIGNING_KEY`, `AUTH_CODE_PEPPER` and `SEED_ADMIN_PASSWORD` are refused when `APP_ENV=production`.
- Secrets never leak:
  - Passwords, codes and refresh tokens in DTOs are `secret.String`.
  - Logs and audit rows hold no emails, names, phones, plates, codes, tokens or hashes.
  - `apperr` messages are fixed English strings.
- Authorization is deny-by-default. The role is never read from client input. Use cases take `authz.Principal` as an explicit parameter. Driver-path repository methods always take the owner id, and "not found" and "not owned" both return 404.
- Testing:
  - TDD, test first. All tests run with `-race`.
  - Integration tests use real Postgres through `internal/testsupport`, connected as `mypark_app`.
  - No DB mocks for SQL behavior. No `time.Sleep` assertions.
- **Integration test env** (Git Bash, repeated in steps as `$IT`): `docker run -d --name mypark-pg-test -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:18`, then prefix commands with `TEST_DATABASE_URL='postgres://postgres:test@localhost:55432/postgres?sslmode=disable'`. Mailpit tests also need `docker run -d --name mypark-mailpit-test -p 51025:1025 -p 58025:8025 axllent/mailpit:v1.33.0` with `TEST_SMTP_ADDR=localhost:51025 TEST_MAILPIT_API=http://localhost:58025`.
- npm, never pnpm. On this Windows machine `npm install <pkg>` fails with ERESOLVE: edit `package.json` and run `npm install`. No new JS dependency is planned in M1 Track A.
- New Go dependencies, with the one-line PR justification each needs (policies §2.6):
  - `github.com/golang-jwt/jwt/v5`: maintained EdDSA JWT with strict parser options (OC-7).
  - `github.com/google/uuid` (already in `go.sum`): parse, format and generate v7 ids.
  - `golang.org/x/crypto`: promoted from indirect to direct for argon2.
- Commits use Conventional Commits with scopes `server|web|mobile|contracts|deploy|docs|ci`. **No `Co-Authored-By`, no AI attribution.** PRs are ≤ 400 changed lines, chained. Generated files (`dbgen`, `openapi.d.ts`, `error-codes.ts`) don't count toward the budget.
- Commands must run in Git Bash on Windows and in Linux CI. Every Go command runs from `apps/server`.

## Review Focus

1. **Blank or empty optional strings that pass the validator but violate a DB CHECK.** Examples: `phone: ""`, or `first_name: "   "`. Expected: 422 `validation.failed` for blank names, and `phone: ""` treated as "no phone" (null), never 500. Tests: A13 `TestRegister_BlankNamesAndEmptyPhone`, A20 `TestPatchMe_EmptyPhoneClears`.
2. **Email case and whitespace variants** (`" Ana@Example.COM "`). Register, login, forgot, resend and every email-keyed rate-limit bucket must treat them as the same address. Otherwise a variant gets a fresh rate-limit bucket or a duplicate pending account. Tests: A13 `TestNormalizeEmail`, A22 `TestLoginEmailRule_SharesBucketAcrossCaseVariants`.
3. **Empty, `null` or non-object JSON bodies** on POST/PATCH/DELETE (`""`, `null`, `[]`, `{}garbage`). Expected: 400 `http.invalid_json`, not 500 or a nil-pointer panic. Test: A6 `TestDecodeJSON_RejectsNullEmptyArrayAndTrailing`.
4. **Garbage pagination input** on `GET /v1/admin/admins` (`cursor=%%%`, `limit=0`, `limit=1000`, `limit=abc`). Expected: 422 `validation.failed` with field `cursor`/`limit`, not 500. Test: A21 `TestListAdmins_BadCursorAndLimit`.
5. **A refresh token that is not valid base64url or has the wrong length.** Expected: 401 `auth.refresh_invalid` without a DB error or 500. Test: A16 `TestRefresh_MalformedTokenIsInvalid`.

---

## File Structure

```
apps/server/
├─ api/openapi.yaml                                  # A1 envelope components, A2 every M1 endpoint
├─ cmd/api/{main.go,deps.go}                         # newHandler(deps) wiring grows per task; A23 matrix/log-leak/acceptance tests
├─ cmd/worker/main.go                                # A18 jobs runner, LoadWorker
├─ cmd/seed/main.go                                  # A11 legal docs, A21 first admin
├─ internal/config/{config.go,worker.go,dev.go}      # A9 auth/jwt/proxy/limits, F1; A18 worker
├─ internal/domain/{errcodes,apperr,authz,secret}/   # A2, A6
├─ internal/features/
│  ├─ health/handler.go                              # A1 envelope
│  ├─ legal/{legal.go,service.go,ports.go,http.go,seed.go,postgres/}           # A11
│  ├─ audit/{audit.go,postgres/}                     # A12
│  ├─ notifications/{notifications.go,templates.go,dispatcher.go,templates/<locale>/*,postgres/}  # A12, A18
│  ├─ auth/{email.go,password.go,code.go,refresh.go,session.go,service*.go,authenticator.go,accounts.go,ports.go,http.go,postgres/}  # A13–A17, A21
│  ├─ users/{users.go,service.go,admins.go,ports.go,http.go,postgres/}         # A20, A21
│  └─ vehicles/{plate.go,limit.go,service.go,ports.go,http.go,postgres/}       # A19
├─ internal/infrastructure/
│  ├─ clock/clock.go  hashing/{argon2.go,codes.go,common.go,common_passwords.txt}  jwt/jwt.go     # A6, A8, A9
│  ├─ database/{tx.go, migrations/0000{2..6}_*.sql, queries/{legal,auth,users,audit,outbox,vehicles,ratelimit}.sql, dbgen/}
│  ├─ ratelimit/{ratelimit.go,rules.go,postgres.go}  email/smtp.go  jobs/jobs.go                  # A7, A22, A18
│  └─ validation/validation.go                       # A8 password_common rule
├─ internal/transport/http/
│  ├─ reqctx/reqctx.go                               # A1 (F4): request id + LogFields
│  ├─ response/response.go                           # A1: OK[T], List[T], NoContent, Error, Meta, Page
│  ├─ httpx/httpx.go                                 # A6: DecodeJSON, WriteError, Nullable[T]
│  ├─ router/router.go                               # A7: Policy, Handle, Routes
│  ├─ authn/authn.go                                 # A15: bearer, role gate, legal gate
│  └─ middleware/{securehdr.go,cors.go,clientip.go,ratelimit.go, recover.go, accesslog.go, notfound.go}  # A4, A22
├─ internal/testsupport/{pgtest.go,clock.go,mailpit.go}   # A3, A6, A18
└─ scripts/covergate.sh                              # A23
packages/contracts/src/{envelope.ts,envelope.test.ts,health.ts,health.test.ts,openapi.d.ts,error-codes.ts,m1.types.test.ts,index.ts}
apps/web/src/features/health/HealthStatus.test.tsx, apps/mobile/src/services/httpClient.test.ts   # A1 fixtures
deploy/compose/docker-compose.yml   .github/workflows/{ci-server.yml,ci-js.yml}
```

---

## Task A1: Baseline response envelope (+ F4 reqctx) — server, contracts, web, mobile

Every later handler depends on this task, so it lands first. It changes the M0 error shape: `request_id` moves from `error` to `meta`.

**Files:**
- Create: `apps/server/internal/transport/http/reqctx/reqctx.go`, `reqctx_test.go`; `packages/contracts/src/envelope.ts`, `envelope.test.ts`
- Modify: `apps/server/internal/transport/http/response/{response.go,response_test.go}`, `middleware/{requestid.go,requestid_test.go,recover.go,accesslog.go,notfound.go}`, `httpserver/httpserver.go` (delete `response.RequestIDFunc = …`), `internal/features/health/{handler.go,handler_test.go}`, `cmd/api/main_test.go`, `api/openapi.yaml` (components + health operations), `packages/contracts/src/{health.ts,health.test.ts,health.types.test.ts,index.ts,openapi.d.ts}`, `apps/web/src/features/health/HealthStatus.test.tsx`, `apps/mobile/src/services/httpClient.test.ts`

**Interfaces:**
- Consumes: M0 `middleware.RequestID`, `response.Error`, `validation.Error`, `toHealthState`.
- Produces (Go):
  - `reqctx.WithRequestID(ctx context.Context, id string) context.Context`, `reqctx.RequestID(ctx context.Context) string`.
  - `type reqctx.LogFields struct{ UserPublicID string }`, `reqctx.WithLogFields(ctx) (context.Context, *LogFields)`, `reqctx.Fields(ctx) *LogFields` (nil-safe). `middleware.RequestIDFrom` is deleted, and callers use `reqctx.RequestID`.
  - `type response.Page struct{ Limit int \`json:"limit"\`; NextCursor *string \`json:"next_cursor"\` }`, and `type response.Meta struct{ RequestID string \`json:"request_id"\`; Page *Page \`json:"page,omitempty"\` }`.
  - `response.OK[T any](w http.ResponseWriter, r *http.Request, status int, data T)`, `response.List[T any](w http.ResponseWriter, r *http.Request, items []T, page Page)` (a nil `items` encodes as `[]`), `response.NoContent(w http.ResponseWriter)`.
  - `response.Error(w, r, status int, code errcodes.Code, msg string, details map[string]any)` keeps its signature and writes `{"error":{code,message,details},"meta":{request_id}}`.
  - `response.ValidationError` is unchanged in signature. `type ErrorBody` drops `RequestID`. `type ErrorEnvelope struct{ Error ErrorBody; Meta Meta }`. `response.JSON` is removed (unexported `writeJSON` only).
- Produces (TS, `@mypark/contracts`):
  - Types: `Meta`, `Page`, `ApiSuccess<T>`, `ApiList<T>`, `ErrorEnvelope`.
  - `class ApiError extends Error { code: ErrorCode | string; details: Record<string, unknown>; requestId: string; status?: number }`.
  - `parseEnvelope<S extends z.ZodType>(schema: S): (body: unknown, status?: number) => z.infer<S>`. It returns `data` when `data` parses with `schema`. It throws `ApiError` built from `error` when an error envelope parses. Anything else throws `ApiError` with code `internal.unexpected` and message `malformed envelope`.
  - Schemas: `metaSchema`, `errorEnvelopeSchema`, `successEnvelope(schema)`, `listEnvelope(schema)`.
  - `toHealthState(input: HealthProbeResult)` keeps its signature and reads the new shapes.

- [ ] **Step 1: Write the failing Go tests**

```go
// reqctx_test.go
func TestRequestID_RoundTrip(t *testing.T)        // WithRequestID(ctx,"r1") → RequestID == "r1"; empty ctx → ""
func TestLogFields_PointerSharedThroughContext(t *testing.T) // set UserPublicID via Fields(ctx) after WithLogFields → visible through the returned pointer
// response_test.go
func TestOK_WrapsDataAndMeta(t *testing.T)        // OK(w,r,201,map{"a":1}) → 201, body == {"data":{"a":1},"meta":{"request_id":"r1"}}, no "error" key
func TestList_EmptyIsArrayWithPage(t *testing.T)  // List[int](w,r,nil,Page{Limit:5}) → data == [], meta.page == {"limit":5,"next_cursor":null}
func TestError_RequestIDInMeta(t *testing.T)      // error.request_id absent; meta.request_id == "r1"; details == {} when nil
func TestNoContent_EmptyBody(t *testing.T)        // 204, len(body)==0, no Content-Type
func TestValidationError_Envelope(t *testing.T)   // 422, error.code "validation.failed", error.details.fields[0] == {field,rule,param}
// health handler_test.go (update existing)
func TestHealthz_OK(t *testing.T)   // data == {"status":"ok","service":"api","version":"test"}, meta.request_id non-empty
func TestReadyz_Ready(t *testing.T) // data.checks.database == "ok"
func TestReadyz_DBDown(t *testing.T) // 503, error.code "health.not_ready", error.details.database "fail", meta.request_id set
// middleware tests (update/extend)
func TestUnknownRoute_ReturnsEnvelope(t *testing.T)    // 404 error.code "http.not_found", meta.request_id == X-Request-Id header
func TestMethodNotAllowed_ReturnsEnvelope(t *testing.T) // 405 "http.method_not_allowed", Allow header kept
func TestRecover_PanicEnvelopeHasMetaRequestID(t *testing.T)
func TestEnvelope_ParallelHandlersNoRace(t *testing.T) // 50 parallel httptest requests through httpserver.NewHandler each see their own meta.request_id (go test -race; replaces the RequestIDFunc race, F4)
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd apps/server && go test -race ./internal/transport/http/... ./internal/features/health/...`
Expected: FAIL (undefined: reqctx, response.OK, response.List).

- [ ] **Step 3: Implement** `reqctx`, the new `response` helpers, and the migration of `RequestID`, `Recover`, `AccessLog`, `JSONErrors` and `health` to them. Delete `response.RequestIDFunc` and its assignment in `httpserver.NewHandler`. `RequestID` also calls `reqctx.WithLogFields`.
- [ ] **Step 4: Run Go tests** — `go test -race ./... && go vet ./...` (plus `$IT go test -race ./cmd/api/...` after updating `main_test.go` to read `data.status` / `error.code` + `meta.request_id`). Expected: all `ok`.
- [ ] **Step 5: Update `api/openapi.yaml`**:
  - Add `components.schemas`: `Meta` (`request_id` required, `page` optional `$ref Page`), `Page` (`limit` integer, `next_cursor` string|null), `ErrorBody` (`code`, `message`, `details` object required, `additionalProperties: false`), and `ErrorEnvelope` (`error` + `meta`, both required).
  - Health responses use `allOf: [{type: object, required:[data, meta], properties:{data:{$ref: Health}, meta:{$ref: Meta}}}]`. This is the `DataEnvelope` pattern every later operation copies.
- [ ] **Step 6: Write the failing TS tests**

```ts
// envelope.test.ts
test('parseEnvelope returns data for a success envelope')        // parseEnvelope(z.object({a:z.number()}))({data:{a:1},meta:{request_id:'r'}}) → {a:1}
test('parseEnvelope throws ApiError with code, details, requestId') // {error:{code:'auth.forbidden',message:'x',details:{}},meta:{request_id:'r'}} → ApiError code 'auth.forbidden', requestId 'r'
test('parseEnvelope rejects a body with both data and error')    // throws ApiError code 'internal.unexpected'
test('parseEnvelope rejects data that fails the schema')         // throws ApiError code 'internal.unexpected', message 'malformed envelope'
test('listEnvelope requires meta.page with nullable next_cursor')
// health.test.ts — update every fixture: 200 body {data:{status:'ready',checks:{…}},meta:{request_id:'r'}};
// 503 body {error:{code:'health.not_ready',message:'x',details:{}},meta:{request_id:'r'}}. Keep all 9 existing cases, plus:
test('toHealthState maps the old M0 error shape (request_id inside error) to unavailable')
```

- [ ] **Step 7: Run to verify they fail** — `npm test -w packages/contracts`. Expected: FAIL (`parseEnvelope` not exported; old fixtures no longer match the updated schemas once Step 8 lands).
- [ ] **Step 8: Implement** `envelope.ts`, export it from `index.ts`, rewrite `health.ts` so `toHealthState` uses `parseEnvelope(readinessSchema)`, and regenerate with `npm run gen -w packages/contracts`. Update the web `HealthStatus.test.tsx` and mobile `httpClient.test.ts` fixtures to the new 503 shape. Their callers (`apps/web/src/lib/api/server.ts`, `apps/mobile/src/services/httpClient.ts`) keep calling `toHealthState` unchanged.
- [ ] **Step 9: Run all JS checks** — `npm test -w packages/contracts && npm run typecheck -w packages/contracts && npm test -w apps/web && npm test -w apps/mobile`. Expected: all pass. Then `git diff --exit-code packages/contracts/src/openapi.d.ts` after a second `npm run gen` prints nothing.
- [ ] **Step 10: Commit** (two commits, same PR)

```bash
git add apps/server
git commit -m "refactor(server)!: wrap every response in the baseline envelope"
git add packages/contracts apps/web/src/features/health apps/mobile/src/services
git commit -m "refactor(contracts)!: add envelope types and parseEnvelope, adapt health clients"
```

---

## Task A2: M1 contract — OpenAPI, error codes, regenerated contracts (handoff)

This is the Track B handoff, so both developers approve it. The hand-written `openapi.yaml` will exceed 400 lines. Mark the PR `size:exception` (one coherent contract, not splittable without breaking the handoff).

**Files:**
- Modify: `apps/server/api/openapi.yaml`, `apps/server/internal/domain/errcodes/{errcodes.go,errcodes_test.go}`, `packages/contracts/src/{error-codes.ts,openapi.d.ts}` (generated)
- Create: `packages/contracts/src/m1.types.test.ts`

**Interfaces:**
- Consumes: A1 components (`Meta`, `Page`, `ErrorEnvelope`, the `DataEnvelope` pattern).
- Produces:
  - Go constants, with names exactly as below. Later tasks use them.
    - Auth: `AuthUnauthenticated`, `AuthSessionRevoked`, `AuthRefreshInvalid`, `AuthInvalidCredentials`, `AuthEmailNotVerified`, `AuthAccountSuspended`, `AuthForbidden`, `AuthPasswordIncorrect`, `AuthCodeInvalid`.
    - Legal and user: `LegalAcceptanceRequired`, `LegalVersionOutdated`, `UserEmailTaken`.
    - Vehicle: `VehicleNotFound`, `VehiclePlateTaken`, `VehicleLimitReached`, `VehicleInUse`.
    - Other: `RateLimitExceeded`, `HTTPInvalidJSON`, `HTTPPayloadTooLarge`, `HTTPUnsupportedMediaType`.
    - Their string values are exactly the spec §6.3 codes, e.g. `"auth.code_invalid"`, `"rate_limit.exceeded"`.
  - OpenAPI `operationId`s, which Track B and A23 rely on:
    - Legal and auth: `getLegalCurrent`, `register`, `verifyEmail`, `resendVerification`, `login`, `refreshSession`, `logout`, `logoutAll`, `forgotPassword`, `resetPassword`, `acceptInvitation`.
    - Me: `getMe`, `updateMe`, `deleteMe`, `acceptLegal`.
    - Vehicles: `listVehicles`, `createVehicle`, `getVehicle`, `updateVehicle`, `deleteVehicle`.
    - Admin: `listAdmins`, `createAdmin`.
  - Schemas: `User`, `UserLegal`, `TokenResponse`, `Vehicle`, `AdminUser`, `LegalCurrent`, `LegalDocument`, `AcceptedStatus`, and one request schema per body operation, named `<OperationId>Request`.
  - `securitySchemes.bearerAuth` (http bearer, JWT). Public operations have `security: []`.
- Contract decisions to encode:
  - Register returns 202 with `data: {status:"verification_pending"}`. Resend and forgot return 202 with `data: {status:"accepted"}`. Every 204 has no `content`.
  - `listVehicles` and `listAdmins` return `data: Vehicle[]` / `AdminUser[]` with `meta.page`.
  - `listAdmins` query: `limit` integer 1–100 (default 50) and `cursor` (opaque string).
  - `createAdmin` returns 201 (new invite or takeover of a pending driver) or 200 (reissue to a pending invitee, OC-39).
  - Every bearer operation lists 401 (`auth.unauthenticated`, `auth.session_revoked`) and 403 (`auth.forbidden`, `auth.account_suspended`, plus `legal.acceptance_required` on non-exempt driver operations).
  - Rate-limited operations list 429 with a `Retry-After` header.
  - Body operations list 400, 413, 415 and 422.
  - Each error response has one `example` with its code.

- [ ] **Step 1: Write the failing tests**

```go
// errcodes_test.go
func TestCatalog_M1Codes(t *testing.T) // every one of the 20 spec §6.3 codes is in All(); table of {Const, "literal"} pairs asserts the exact string
func TestCatalog_FormatAndUnique(t *testing.T) // existing format regex holds for all; no duplicates
```

```ts
// m1.types.test.ts (vitest + expectTypeOf)
test('every M1 path and method exists')   // expectTypeOf<paths['/v1/auth/login']['post']>().not.toBeNever(); …one line per spec §6.1 row (22)
test('ERROR_CODES includes every M1 code') // expect(ERROR_CODES).toEqual(expect.arrayContaining([...20 codes]))
test('TokenResponse has first_name and last_name on user, never full_name')
```

- [ ] **Step 2: Run to verify they fail** — `go test ./internal/domain/errcodes/...` → FAIL (undefined: errcodes.AuthUnauthenticated). `npm test -w packages/contracts` → FAIL.
- [ ] **Step 3: Implement** the constants and catalog entries. Write every M1 operation into `openapi.yaml`, following spec §6.1–§6.3 field rules for request schemas: lengths, `format: email`, the `^[0-9]{6}$` code pattern, `phone` `type: [string, "null"]` on PATCH, and `client_kind` enum.
- [ ] **Step 4: Regenerate and verify**

Run: `go generate ./internal/domain/errcodes/... && cd ../.. && npm run gen -w packages/contracts && npm test -w packages/contracts && npm run typecheck -w packages/contracts`
Expected: tests pass. A second run of both generators leaves `git diff --exit-code packages/contracts/src` clean.

- [ ] **Step 5: Commit**

```bash
git add apps/server/api/openapi.yaml apps/server/internal/domain/errcodes packages/contracts/src
git commit -m "feat(contracts): publish m1 auth, users, vehicles and admin api contract"
```

---

## Task A3: M0 follow-ups — test DBs as production roles (F9, F6) and DB privileges (F5)

This has to land before any M1 table exists (spec F9: "Land before S3").

**Files:**
- Modify: `apps/server/internal/testsupport/pgtest.go`, `apps/server/internal/infrastructure/database/migrate_test.go` (tests that re-run `MigrateUp` use `.Migrator`)
- Create: `apps/server/internal/infrastructure/database/migrations/00002_db_privileges.sql`, `apps/server/internal/infrastructure/database/privileges_test.go`, `apps/server/internal/testsupport/pgtest_test.go`

**Interfaces:**
- Consumes: `database.NewPool`, `database.MigrateUp`.
- Produces:
  - `type testsupport.TestDB struct{ App, Migrator, Super *pgxpool.Pool; AppURL string }`.
  - `testsupport.NewTestDB(t *testing.T) *TestDB`: creates the DB with `owner mypark_migrator` and runs goose on the `Migrator` pool. `App` connects with runtime param `role=mypark_app`, and `Migrator` with `role=mypark_migrator`. Both are set through `ConnConfig.RuntimeParams`. If the server rejects the startup param, use `options=-c role=…`.
  - `testsupport.NewDB(t) *pgxpool.Pool` now returns `NewTestDB(t).App`.
  - `testsupport.NewDBURL(t) string` returns `AppURL`, which carries the role in its query.
  - `testsupport.NewEmptyDB(t) *pgxpool.Pool` is unchanged in shape (superuser pool, DB owned by `mypark_migrator`). The drop cleanup is registered right after `create database` and the pool close after it (LIFO, F6).

- [ ] **Step 1: Write the failing tests**

```go
func TestNewDB_ConnectsAsApp(t *testing.T)       // select current_user on NewDB pool == "mypark_app"
func TestNewDBURL_ConnectsAsApp(t *testing.T)    // pgx.Connect(NewDBURL) → current_user == "mypark_app"
func TestNewTestDB_MigratedByMigrator(t *testing.T) // select tableowner from pg_tables where tablename='lots' == "mypark_migrator"
func TestNewEmptyDB_DropRegisteredBeforePool(t *testing.T) // use a fake-failing pool hook (unexported newPoolFunc var) → after the subtest ends, pg_database has no row for the name
func TestDBPrivileges_AppCanConnect(t *testing.T) // Super: has_database_privilege('mypark_app', current_database(), 'CONNECT') true; also for mypark_migrator
func TestDBPrivileges_PublicCannotConnectOrTemp(t *testing.T) // Super creates role probe_<rand> (cleanup drops it) → CONNECT false, TEMPORARY false
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/testsupport/... ./internal/infrastructure/database/...`. Expected: FAIL (`current_user` is `postgres`; privileges true for probe).
- [ ] **Step 3: Implement** the testsupport changes and `00002_db_privileges.sql` (spec §5.1 verbatim, `-- +goose Up` only).
- [ ] **Step 4: Run the whole server suite** — `$IT go test -race ./...`. Expected: all `ok`. The existing lots and migrate tests pass as `mypark_app`.
- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/testsupport apps/server/internal/infrastructure/database
git commit -m "fix(server): run test migrations as mypark_migrator and revoke public db access"
```

---

## Task A4: HTTP hardening — SecureHeaders, CORS (F7), ClientIP, Recover (F2), AccessLog (F3)

**Files:**
- Create: `apps/server/internal/transport/http/middleware/{securehdr.go,cors.go,clientip.go}` + tests
- Modify: `middleware/{recover.go,accesslog.go}` + tests, `httpserver/httpserver.go` (global chain), `cmd/api/main.go`
- Docs: `docs/architecture.md` §4.2 (global chain and per-route wrappers, OC-2)

**Interfaces:**
- Consumes: `reqctx` (A1).
- Produces:
  - `middleware.SecurityHeaders` (`map[string]string` of the four OC-32 headers) and `middleware.SecureHeaders(next http.Handler) http.Handler`.
  - `middleware.CORS(allowed []string) func(http.Handler) http.Handler`.
  - `middleware.ClientIP(trusted []netip.Prefix) func(http.Handler) http.Handler` and `middleware.ClientIPFrom(ctx context.Context) netip.Addr`. `ClientIP` is a per-route wrapper (A7 inserts it), not global.
  - `httpserver.NewHandler(log *slog.Logger, mux *http.ServeMux, corsOrigins []string) http.Handler` with the order `Recover → RequestID → AccessLog → SecureHeaders → CORS → JSONErrors`.
  - `AccessLog` adds `user_id` from `reqctx.Fields(ctx).UserPublicID` when non-empty.

- [ ] **Step 1: Write the failing tests**

```go
func TestSecureHeaders_SetOnEveryResponse(t *testing.T) // nosniff, Cache-Control "no-store", Referrer-Policy "no-referrer", CSP "default-src 'none'; frame-ancestors 'none'" — on 200, 404 and panic 500
func TestCORS_AllowedOrigin(t *testing.T)    // Origin http://localhost:3000 → ACAO echoes it, Vary: Origin, Expose-Headers "X-Request-Id, Retry-After", no Allow-Credentials
func TestCORS_DisallowedOrigin(t *testing.T) // Origin http://evil.test → no Access-Control-* headers
func TestCORS_PreflightOnPostOnlyRoute(t *testing.T) // OPTIONS + Access-Control-Request-Method: POST on "POST /v1/x" → 204 (not 405), Allow-Methods "GET, POST, PATCH, DELETE", Allow-Headers "Authorization, Content-Type, X-Request-Id", Max-Age "600"
func TestClientIP_UntrustedPeerIgnoresXFF(t *testing.T) // RemoteAddr 203.0.113.9:5, XFF "1.2.3.4" → 203.0.113.9
func TestClientIP_TrustedPeerUsesRightmostUntrusted(t *testing.T) // trusted 172.16.0.0/12, RemoteAddr 172.18.0.5, XFF "1.2.3.4, 198.51.100.7, 172.18.0.9" → 198.51.100.7
func TestClientIP_SpoofedLeftmostIgnored(t *testing.T)  // XFF "6.6.6.6, 198.51.100.7" via trusted peer → 198.51.100.7
func TestClientIP_GarbageXFFFallsBackToPeer(t *testing.T)
func TestRecover_StaleHeadersDropped(t *testing.T) // handler sets Content-Length: 3, Location, Cache-Control: public, then panics → 500 body is valid JSON envelope, Location absent, Cache-Control "no-store", X-Request-Id kept
func TestRecover_ErrAbortHandlerWrapped(t *testing.T) // panic(fmt.Errorf("x: %w", http.ErrAbortHandler)) is re-panicked (errors.Is)
func TestAccessLog_LogsPanickedRequest(t *testing.T) // captured slog: one line with status 500, panicked=true, route pattern
func TestAccessLog_UserIDFromLogFields(t *testing.T) // handler sets reqctx.Fields(ctx).UserPublicID="u1" → log line has user_id "u1"
```

- [ ] **Step 2: Run to verify they fail** — `go test -race ./internal/transport/http/...` → FAIL.
- [ ] **Step 3: Implement**. `Recover` deletes every response header and re-applies `X-Request-Id` and `SecurityHeaders` before it writes the envelope. `AccessLog` logs in a `defer` and re-panics. CORS answers preflights itself, before `JSONErrors`. `cmd/api` passes `cfg.CORSAllowedOrigins()`.
- [ ] **Step 4: Run** — `go test -race ./... && go vet ./...` → all `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server docs/architecture.md
git commit -m "feat(server): add security headers, cors, client ip and harden recover and access log"
```

---

## Task A5: CI supply chain (F8)

**Files:**
- Modify: `.github/workflows/ci-server.yml`, `.github/workflows/ci-js.yml`, `docs/policies.md` §2.6 (remove the "not in CI yet" row)

**Interfaces:** Consumes the existing `server` and `contracts` jobs. Produces the new steps `go mod tidy check`, `govulncheck` and `npm audit`.

- [ ] **Step 1: Add the steps to ci-server** before `go vet`:
  - `go mod tidy && git diff --exit-code go.mod go.sum`
  - `go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...` (pin the latest release tag at execution time. Record it in the PR).
- [ ] **Step 2: Add a step to the ci-js `contracts` job** after install: `npm audit --omit=dev --audit-level=high`.
- [ ] **Step 3: Verify locally** — `cd apps/server && go mod tidy && git diff --exit-code go.mod go.sum && go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...` → `No vulnerabilities found.`; `npm audit --omit=dev --audit-level=high` → exit 0.
- [ ] **Step 4: Commit**

```bash
git add .github/workflows docs/policies.md
git commit -m "ci: add govulncheck, go mod tidy check and npm audit"
```

---

## Task A6: Domain primitives — apperr, authz, secret, clock, httpx

**Files:**
- Create: `apps/server/internal/domain/apperr/apperr.go`, `internal/domain/authz/authz.go`, `internal/domain/secret/secret.go`, `internal/infrastructure/clock/clock.go`, `internal/testsupport/clock.go`, `internal/transport/http/httpx/httpx.go` + `_test.go` for each
- Modify: `apps/server/go.mod` (`github.com/google/uuid` direct)

**Interfaces:**
- Consumes: `errcodes` (A2), `response` (A1), `validation`.
- Produces:
  - **`apperr`:**
    - `type Kind int`. The constants are the spec's seven (`Invalid`, `Unauthenticated`, `Forbidden`, `NotFound`, `Conflict`, `RateLimited`, `Internal`) plus three this plan adds: `BadRequest`, `TooLarge` and `UnsupportedMedia`. They cover the 400, 413 and 415 codes from OC-28, so handlers never pick a status code.
    - `type Error struct{ Code errcodes.Code; Kind Kind; Details map[string]any; Err error }` with `Error()` and `Unwrap()`.
    - Constructors `apperr.New(kind Kind, code errcodes.Code) *Error`, `(*Error).WithDetails(map[string]any) *Error`, `(*Error).Wrap(err error) *Error`.
    - Shorthands `Invalid(code)`, `Unauthenticated(code)`, `Forbidden(code)`, `NotFound(code)`, `Conflict(code)`, and `RateLimited(retryAfter time.Duration) *Error`. `RateLimited` sets `details.retry_after_seconds = ceil(seconds)`, minimum 1.
    - No `net/http` import.
  - **`authz`:**
    - `type Role string` with `RoleDriver Role = "driver"`, `RoleAdmin Role = "admin"`, and `ParseRole(string) (Role, bool)`.
    - `type Principal struct{ UserID int64; UserPublicID uuid.UUID; Role Role; SessionID int64; SessionPublicID uuid.UUID; LegalPending bool }`.
    - `WithPrincipal(ctx, Principal) context.Context`, `PrincipalFrom(ctx) (Principal, bool)`, `(Principal).Is(roles ...Role) bool`.
  - **`secret`:** `type String string`, with `(String).Reveal() string`. `String()`, `GoString()`, `LogValue()` and `MarshalJSON` all return `"[redacted]"`. `UnmarshalJSON` accepts a JSON string.
  - **`clock`:** `type Clock interface{ Now() time.Time }` and `clock.System{}` (UTC).
  - **`testsupport.FakeClock`:** `NewFakeClock(t time.Time) *FakeClock`, `Now()`, `Advance(d time.Duration)`, `Set(t time.Time)`. Mutex-guarded.
  - **`httpx`:**
    - `MaxBodyBytes = 64 << 10`.
    - `DecodeJSON[T any](r *http.Request, v *validation.Validator) (T, error)` returns an `*apperr.Error` with `BadRequest/http.invalid_json`, `TooLarge/http.payload_too_large`, `UnsupportedMedia/http.unsupported_media_type`, or an `Invalid/validation.failed` error carrying `details.fields`.
    - `WriteError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error)` maps kinds 400/401/403/404/409/413/415/422/429. It sets `Retry-After` from `details.retry_after_seconds`. A non-`apperr` error becomes `500 internal.unexpected` and its cause is logged.
    - `type Nullable[T any] struct{ Set, Null bool; Value T }` with `UnmarshalJSON`, used for PATCH "absent vs null" (spec §17 builder choice).
    - `PathUUID(r *http.Request, name string) (uuid.UUID, bool)`.

- [ ] **Step 1: Write the failing tests**

```go
func TestSecret_NeverPrints(t *testing.T) // for %v %+v %#v %s, json.Marshal, slog JSON handler with slog.Any("s", s) and inside a struct → output contains "[redacted]" and not the plaintext "hunter2-secret"
func TestApperr_ErrorsAsAndWrap(t *testing.T) // errors.AsType[*apperr.Error] finds it through fmt.Errorf("%w"); Unwrap returns cause
func TestApperr_RateLimitedRoundsUp(t *testing.T) // 1500ms → details.retry_after_seconds == 2; 0 → 1
func TestPrincipal_ContextRoundTrip(t *testing.T)
func TestFakeClock_AdvanceIsRaceFree(t *testing.T) // 10 goroutines Advance(time.Second) → Now == start+10s
func TestWriteError_KindToStatus(t *testing.T) // table: each Kind → status + error.code; RateLimited → Retry-After "2"
func TestWriteError_UnknownErrorIs500AndLogged(t *testing.T) // errors.New("db down: secret-dsn") → 500 internal.unexpected; body has no "secret-dsn"; log has it
func TestDecodeJSON_RequiresJSONContentType(t *testing.T) // text/plain → 415; "application/json; charset=utf-8" accepted
func TestDecodeJSON_TooLarge(t *testing.T)                // 64 KiB + 1 → 413 http.payload_too_large
func TestDecodeJSON_RejectsNullEmptyArrayAndTrailing(t *testing.T) // bodies "", "null", "[]", "{} x", `{"a":1}{"a":2}`, `{"email":5}` → 400 http.invalid_json   (Review Focus 3)
func TestDecodeJSON_ValidationFieldsUseJSONNames(t *testing.T) // missing email → 422, fields[0] == {field:"email",rule:"required",param:""}
func TestNullable_AbsentNullValue(t *testing.T) // {} → Set=false; {"phone":null} → Set,Null; {"phone":"+18095550101"} → Value
func TestPathUUID_Malformed(t *testing.T) // "abc" → false
```

- [ ] **Step 2: Run to verify they fail** — `go test -race ./internal/domain/... ./internal/infrastructure/clock/... ./internal/transport/http/httpx/... ./internal/testsupport/...` → FAIL (undefined).
- [ ] **Step 3: Implement** all of the above. `DecodeJSON` uses `http.MaxBytesReader`, `json.Decoder`, a `dec.More()` trailing check, and rejects a JSON `null` or non-object top level.
- [ ] **Step 4: Run** — same command → all `ok`. `go mod tidy` leaves `github.com/google/uuid` as a direct requirement.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add apperr, authz, secret, clock and httpx primitives"
```

---

## Task A7: Router with mandatory policies, route registry, rate-limit port

**Files:**
- Create: `apps/server/internal/transport/http/router/router.go`, `router_test.go`; `apps/server/internal/infrastructure/ratelimit/{ratelimit.go,rules.go}`, `ratelimit_test.go`

**Interfaces:**
- Consumes: `authz.Role` (A6), `middleware.ClientIP` (A4).
- Produces:
  - **`ratelimit`:**
    - `type KeyKind int` with `KeyIP`, `KeyUser` and `KeyCustom` (handler-built keys).
    - `type Rule struct{ Name string; Limit int; Window time.Duration; Key KeyKind; Prefix string; FailOpen bool }`.
    - `type Decision struct{ Allowed bool; RetryAfter time.Duration }`.
    - `type Limiter interface{ Allow(ctx context.Context, rule Rule, key string) (Decision, error) }` and `ratelimit.Noop{}`.
    - `rules.go` defines the spec §10.1 rules as exported vars: `LoginIPEmail{5,1m}`, `LoginEmail{20,1h}`, `RegisterIP{3,1m}`, `CodeRequestIP{3,1m}`, `CodeRequestEmail{5,1h}`, `CodeSubmitIP{10,1m}`, `RefreshIP{60,1m}` and `VehicleWriteUser{20,1m, KeyUser, FailOpen:true}`. Prefixes are `login:`, `login_e:`, `reg:`, `code:`, `code_e:`, `codesub:`, `refresh:` and `veh:`. Auth rules have `FailOpen:false`, so they fail closed.
  - **`router`:**
    - `type Policy struct{ Public bool; Roles []authz.Role; LegalExempt bool; Limit *ratelimit.Rule }`.
    - `type Middlewares struct{ ClientIP, Authn, Legal func(http.Handler) http.Handler; Roles func([]authz.Role) func(http.Handler) http.Handler; RateLimit func(ratelimit.Rule) func(http.Handler) http.Handler }`.
    - `router.New(mux *http.ServeMux, mw Middlewares) *Router`, `(*Router).Handle(pattern string, p Policy, h http.HandlerFunc)` and `(*Router).Routes() []Route`, where `type Route struct{ Pattern string; Policy Policy }`, sorted by pattern.
    - `router.DriverOnly`, `router.AdminOnly` and `router.AnyRole` role slices.
  - Chain order (OC-2, plus a fix to the spec): `ClientIP → RateLimit(rule if Key==KeyIP) → Authn → Roles → Legal (unless LegalExempt or Public) → RateLimit(rule if Key==KeyUser) → h`. User-keyed rules (`vehicle_write_user`) need the principal, so they run after Authn, even though spec §4.4 draws RateLimit before Authn.

- [ ] **Step 1: Write the failing tests**

```go
func TestHandle_PanicsWithoutPolicy(t *testing.T)            // Policy{} → panic containing pattern
func TestHandle_PanicsPublicWithRoles(t *testing.T)
func TestHandle_PanicsAdminPathNotAdminOnly(t *testing.T)     // "GET /v1/admin/x" with Roles{driver,admin} → panic
func TestHandle_PanicsWhenRequiredMiddlewareNil(t *testing.T) // Roles set but mw.Authn nil → panic
func TestHandle_PanicsUserRuleOnPublicRoute(t *testing.T)     // KeyUser limit on Public → panic
func TestHandle_ChainOrder(t *testing.T) // recording middlewares → order exactly ["clientip","rl:ip","authn","roles","legal","handler"] / user rule → [..., "legal","rl:user","handler"]; LegalExempt skips "legal"; Public skips authn/roles/legal
func TestHandle_PatternVisibleToAccessLog(t *testing.T) // via httpserver.NewHandler: log route == "POST /v1/x", not "unmatched"
func TestRoutes_RegistryIsCompleteAndSorted(t *testing.T)
func TestRules_MatchSpec(t *testing.T) // table asserts Limit/Window/Prefix/FailOpen for all 8 rules
```

- [ ] **Step 2: Run to verify they fail** — `go test -race ./internal/transport/http/router/... ./internal/infrastructure/ratelimit/...` → FAIL.
- [ ] **Step 3: Implement** `router` and `ratelimit` (types, rules, `Noop`; no adapter yet).
- [ ] **Step 4: Run** — same command → `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/transport/http/router apps/server/internal/infrastructure/ratelimit
git commit -m "feat(server): add policy-enforcing router with route registry and rate-limit rules"
```

---

## Task A8: Hashing — argon2id, code HMAC, common-password rule

**Files:**
- Create: `apps/server/internal/infrastructure/hashing/{argon2.go,codes.go,common.go,common_passwords.txt}` + tests
- Modify: `apps/server/internal/infrastructure/validation/validation.go` (register `password_common`), `go.mod` (`golang.org/x/crypto` direct)

**Interfaces:**
- Consumes: none.
- Produces:
  - `type hashing.Params struct{ Memory uint32; Time uint32; Threads uint8; SaltLen, KeyLen uint32 }`. `hashing.DefaultParams = Params{Memory: 64*1024, Time: 3, Threads: 2, SaltLen: 16, KeyLen: 32}`.
  - `hashing.NewArgon2id(p Params, maxConcurrent int64) *Argon2id`. `cmd/api` uses `maxConcurrent=4` (OC-14).
  - `(*Argon2id).Hash(ctx context.Context, password string) (string, error)` returns a PHC string `$argon2id$v=19$m=65536,t=3,p=2$<b64salt>$<b64hash>` (raw std base64, no padding).
  - `(*Argon2id).Verify(ctx context.Context, phc, password string) (ok, needsRehash bool, err error)` and `(*Argon2id).DummyHash() string`. The dummy is computed once at construction.
  - `type hashing.PasswordHasher interface{ Hash(ctx, string) (string, error); Verify(ctx, phc, pw string) (bool, bool, error); DummyHash() string }`.
  - `hashing.NewCodeHasher(pepper []byte) CodeHasher` and `(CodeHasher).Hash(purpose string, userPublicID uuid.UUID, code string) []byte`, which computes HMAC-SHA256 of `purpose + ":" + id + ":" + code`. `hashing.Equal(a, b []byte) bool` uses `subtle.ConstantTimeCompare`.
  - `hashing.IsCommonPassword(pw string) bool` (case-insensitive). Validator tag `password_common` (fails when the password is common).
- The list is SecLists `Passwords/Common-Credentials/10k-most-common.txt` (MIT, per policies §2.6). Its source and license go in a header comment line, and the loader skips `#` lines.

- [ ] **Step 1: Write the failing tests**

```go
func TestHash_PHCFormatAndRoundTrip(t *testing.T) // prefix "$argon2id$v=19$m=65536,t=3,p=2$"; Verify(correct) → ok, !needsRehash
func TestVerify_WrongPassword(t *testing.T)
func TestVerify_TamperedSaltOrHash(t *testing.T)  // flip one char in salt, then hash → ok=false, err nil
func TestVerify_MalformedPHC(t *testing.T)        // "$argon2i$…", missing fields → err non-nil
func TestVerify_NeedsRehashOnOldParams(t *testing.T) // hash with Params{Time:1,…} → Verify by DefaultParams hasher → ok, needsRehash
func TestArgon2id_SemaphoreBoundsConcurrency(t *testing.T) // test hook counts in-flight; 16 goroutines with maxConcurrent=4 → peak <= 4
func TestArgon2id_WaitRespectsContext(t *testing.T) // maxConcurrent=1, slot held, ctx canceled → Hash returns context.Canceled
func TestCodeHash_BindsPurposeAndUser(t *testing.T) // same code, different purpose or user → different hashes; same inputs → equal
func TestIsCommonPassword(t *testing.T) // "password123" and "PASSWORD123" true; "Zx9!kelp-orbit" false; list has ≥ 9_000 entries
func TestValidator_PasswordCommonTag(t *testing.T) // struct{P string `json:"password" validate:"required,min=10,max=128,password_common"`}{"1234567890"} → fields[0].rule == "password_common"
```

Tests use `Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}` except the format test, to keep `-race` fast.

- [ ] **Step 2: Run to verify they fail** — `go test -race ./internal/infrastructure/hashing/... ./internal/infrastructure/validation/...` → FAIL.
- [ ] **Step 3: Implement** with `argon2.IDKey`, concurrency gated by a buffered `chan struct{}` of size `maxConcurrent` (select on `ctx.Done()` while waiting), and `//go:embed common_passwords.txt` loaded into a `sync.OnceValue` set.
- [ ] **Step 4: Run** — same command → `ok`. `go mod tidy` makes `golang.org/x/crypto` direct.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add argon2id hasher, code hmac and common-password rule"
```

---

## Task A9: JWT and config additions (+ F1 redaction, compose defaults)

**Files:**
- Create: `apps/server/internal/infrastructure/jwt/jwt.go`, `jwt_test.go`; `apps/server/internal/config/dev.go`
- Modify: `apps/server/internal/config/{config.go,config_test.go}`, `deploy/compose/docker-compose.yml` (api env + header comment fix: remove the `deploy/compose/.env` sentence), `go.mod` (`github.com/golang-jwt/jwt/v5`)

**Interfaces:**
- Consumes: `authz.Role` (A6).
- Produces:
  - **`jwt` package** (it imports `github.com/golang-jwt/jwt/v5` under the alias `gojwt`):
    - `type Claims struct{ Subject, SessionID uuid.UUID; Role authz.Role; IssuedAt, ExpiresAt time.Time }`.
    - `jwt.NewSigner(kid string, seed []byte, c clock.Clock, ttl time.Duration) (*Signer, error)` and `(*Signer).Sign(subject, session uuid.UUID, role authz.Role) (token string, expiresAt time.Time, err error)`.
    - `jwt.NewVerifier(keys map[string]ed25519.PublicKey, c clock.Clock) *Verifier` and `(*Verifier).Verify(token string) (Claims, error)`.
    - Constants: issuer `"mypark-api"`, audience `"mypark"`, leeway 30 s, `AccessTTL = 15*time.Minute`.
    - `jwt.ErrInvalid` is the single sentinel for every verification failure.
  - **Config:**
    - New accessors: `JWTSigningSeed() []byte`, `JWTKeyID() string`, `JWTVerifyKeys() map[string]ed25519.PublicKey` (includes the signing key's public key), `AuthCodePepper() []byte`, `TrustedProxyCIDRs() []netip.Prefix`, `MaxVehiclesPerUser() int`.
    - Env names: `JWT_SIGNING_KEY`, `JWT_KEY_ID`, `JWT_VERIFY_KEYS`, `AUTH_CODE_PEPPER`, `TRUSTED_PROXY_CIDRS`, `MAX_VEHICLES_PER_USER`.
    - Rules: `JWT_SIGNING_KEY` is base64 of exactly 32 bytes. `AUTH_CODE_PEPPER` is base64 of ≥ 32 bytes. `MAX_VEHICLES_PER_USER` is 1–20 (default 5). `TRUSTED_PROXY_CIDRS` is a comma-separated list of CIDRs (default empty).
    - `config.DevJWTSigningKey`, `config.DevAuthCodePepper` and `config.DevSeedAdminPassword` are exported constants in `dev.go`. Generate them once with `openssl rand -base64 32` (the password is a non-common ≥ 10-char phrase) and mark them `// dev only`.
    - `(Config).String()` and `(Config).GoString()` render the redacted `LogValue` group (F1).
  - Compose `api` env: `JWT_SIGNING_KEY: ${JWT_SIGNING_KEY:-<DevJWTSigningKey>}`, `JWT_KEY_ID: ${JWT_KEY_ID:-dev-1}`, `JWT_VERIFY_KEYS: ${JWT_VERIFY_KEYS:-}`, `AUTH_CODE_PEPPER: …`, `TRUSTED_PROXY_CIDRS: ${TRUSTED_PROXY_CIDRS:-172.16.0.0/12}`, `MAX_VEHICLES_PER_USER: ${MAX_VEHICLES_PER_USER:-5}`.

- [ ] **Step 1: Write the failing tests**

```go
// jwt_test.go
func TestSignVerify_RoundTrip(t *testing.T)     // Claims.Subject/SessionID/Role equal; ExpiresAt == fake now + 15m; header kid "dev-1", alg "EdDSA"
func TestVerify_RejectsAlgNone(t *testing.T)
func TestVerify_RejectsHS256WithPublicKeyBytes(t *testing.T)
func TestVerify_RejectsWrongIssuerOrAudience(t *testing.T)
func TestVerify_RejectsExpired(t *testing.T)    // Advance(15m+31s) → ErrInvalid; Advance(15m+29s) → ok (leeway)
func TestVerify_RejectsNotBeforeFuture(t *testing.T)
func TestVerify_RejectsUnknownKid(t *testing.T)
func TestVerify_RejectsMissingSidOrBadRole(t *testing.T)
func TestVerify_AcceptsRotatedOutKey(t *testing.T) // token signed by old key, verifier has old pubkey under "old-1" → ok
// config_test.go
func TestLoad_AuthDefaultsRequired(t *testing.T) // missing JWT_SIGNING_KEY/JWT_KEY_ID/AUTH_CODE_PEPPER → one joined error naming all three, values never echoed
func TestLoad_RejectsShortSeedAndPepper(t *testing.T)
func TestLoad_ProductionRefusesDevKeys(t *testing.T) // APP_ENV=production + DevJWTSigningKey → error naming JWT_SIGNING_KEY; same for AUTH_CODE_PEPPER
func TestLoad_MaxVehiclesRange(t *testing.T)     // "0","21","x" → error; unset → 5
func TestLoad_TrustedProxyCIDRs(t *testing.T)    // "172.16.0.0/12, 10.0.0.0/8" → 2 prefixes; "nope" → error
func TestConfig_StringAndGoStringRedacted(t *testing.T) // for %v %+v %#v %s: no DB password, no seed, no pepper in output
func TestComposeDefaultsMatchDevConstants(t *testing.T) // read ../../../../deploy/compose/docker-compose.yml → contains DevJWTSigningKey and DevAuthCodePepper
```

- [ ] **Step 2: Run to verify they fail** — `go test -race ./internal/infrastructure/jwt/... ./internal/config/...` → FAIL.
- [ ] **Step 3: Implement**. The parser uses `gojwt.WithValidMethods([]string{"EdDSA"})`, `WithIssuer`, `WithAudience`, `WithExpirationRequired()`, `WithLeeway(30*time.Second)` and `WithTimeFunc(clock.Now)`. Keys are chosen by `kid`. `cmd/api/main.go` keeps compiling (existing tests pass the new env vars through a shared `testEnv` helper in `main_test.go`).
- [ ] **Step 4: Run** — `go test -race ./... && go vet ./...` → `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server deploy/compose/docker-compose.yml
git commit -m "feat(server): add ed25519 jwt, auth config and config string redaction"
```

---

## Task A10: Transactions — TxRunner, Conn, MustTx

**Files:**
- Create: `apps/server/internal/infrastructure/database/tx.go`, `tx_test.go`

**Interfaces:**
- Consumes: `testsupport.NewTestDB` (A3).
- Produces:
  - `type database.DBTX interface{ Exec(…); Query(…); QueryRow(…) }`, compatible with `dbgen.DBTX`.
  - `database.NewTxRunner(pool *pgxpool.Pool) *TxRunner` and `(*TxRunner).InTx(ctx context.Context, fn func(ctx context.Context) error) error`. A nested `InTx` reuses the outer tx.
  - `database.Conn(ctx context.Context, pool *pgxpool.Pool) DBTX` returns the tx when present, otherwise the pool.
  - `database.MustTx(ctx context.Context) (pgx.Tx, error)` returns `database.ErrNoTx` without a tx.
  - `type TxRunnerPort interface{ InTx(ctx context.Context, fn func(context.Context) error) error }` is exported for feature ports.

- [ ] **Step 1: Write the failing tests**

```go
func TestInTx_CommitsOnNil(t *testing.T)      // insert into a temp table inside fn → visible after
func TestInTx_RollsBackOnError(t *testing.T)  // fn returns error after insert → row absent, error returned (wrapped)
func TestInTx_RollsBackOnPanic(t *testing.T)  // panic propagates, row absent
func TestInTx_NestedReusesOuter(t *testing.T) // inner InTx error rolls back the whole outer tx
func TestMustTx_ErrorsOutsideTx(t *testing.T) // errors.Is(err, database.ErrNoTx)
func TestConn_PrefersTx(t *testing.T)
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/infrastructure/database/...` → FAIL.
- [ ] **Step 3: Implement** with an unexported context key holding `pgx.Tx`.
- [ ] **Step 4: Run** — same → `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/infrastructure/database
git commit -m "feat(server): add context-carried transaction runner"
```

---

## Task A11: Identity schema, legal documents and `GET /v1/legal/current`

**Files:**
- Create: `apps/server/internal/infrastructure/database/migrations/00003_identity.sql`, `queries/{legal,auth,users}.sql` (the auth/users query files start with what A13–A21 need; later tasks add queries), `internal/features/legal/{legal.go,service.go,ports.go,http.go,seed.go,postgres/repo.go}` + tests, `internal/infrastructure/database/grants_test.go`
- Modify: `sqlc.yaml` (uuid override), `dbgen/*` (generated), `cmd/seed/main.go`, `cmd/seed/main_test.go`, `cmd/api/main.go` (introduce `deps` and `newHandler(d deps) http.Handler`, register legal), `cmd/api/main_test.go`
- Docs: `docs/architecture.md` §11.1 (`auth_sessions`, `refresh_tokens.session_id`)

**Interfaces:**
- Consumes: `TxRunner` (A10), `router` (A7), `response.OK` (A1), `clock` (A6).
- Produces:
  - Tables from spec §5.2–§5.5: `users`, `auth_sessions`, `refresh_tokens`, `auth_codes`, `legal_documents`. Use exactly the spec's columns, checks and indexes, including `users_email_active_key` and `unique (user_id, purpose) where consumed_at is null`.
  - `type legal.Document struct{ Kind, Version string; PublishedAt time.Time; URLEs, URLEn string }` and `type legal.Versions struct{ Terms, Privacy Document }`.
  - `type legal.Current interface{ Versions(ctx context.Context) (Versions, error); ValidateAcceptance(ctx context.Context, terms, privacy string) error }`. `ValidateAcceptance` returns `apperr.Conflict(LegalVersionOutdated)` with `details{terms_version, privacy_version}`.
  - `legal.NewService(repo Repo, c clock.Clock, ttl time.Duration) *Service`. It caches for 60 s by the clock, and `cmd/api` uses `ttl=60*time.Second`.
  - `legal.InitialVersion = "2026-11-16"`. `legal.SeedDocuments(ctx context.Context, db database.DBTX) error` uses `on conflict do nothing`, and the URLs are `http://localhost:3000/es-DO/legal/{terms,privacy}` and `/en/…`.
  - `(*legal.Handler).Register(r *router.Router)` registers `GET /v1/legal/current` with `Policy{Public:true}`.
  - In `cmd/api`, `type deps struct{ log *slog.Logger; cfg config.Config; pool *pgxpool.Pool; clock clock.Clock; … }` gains fields per task. `newHandler(d deps) http.Handler` builds the router and every feature handler. Tests construct `deps` directly.
- sqlc override in `sqlc.yaml`: `overrides: [{db_type: "uuid", go_type: "github.com/google/uuid.UUID"}]`.

- [ ] **Step 1: Write the failing tests**

```go
// grants_test.go (as App)
func TestGrants_AppDMLOnIdentityTables(t *testing.T) // for users, auth_sessions, refresh_tokens, auth_codes, legal_documents: has_table_privilege('mypark_app', t, 'SELECT,INSERT,UPDATE,DELETE') all true
func TestUsers_ChecksRejectBadRows(t *testing.T) // table: role 'root'; status 'gone'; locale 'fr'; first_name ''; last_name 81 chars; phone '809555'; deleted status without deleted_at → SQLSTATE 23514
func TestUsers_EmailUniqueAmongNonDeletedCaseInsensitive(t *testing.T) // 'Ana@x.do' then 'ana@x.do' → 23505; after soft-delete of first → insert ok
func TestAuthCodes_OneLivePerPurpose(t *testing.T) // second live (user,purpose) → 23505
func TestAuthSessions_RevokedReasonPairing(t *testing.T) // revoked_at set without reason → 23514
// legal
func TestCurrent_PicksLatestPublishedNotFuture(t *testing.T) // rows 2026-11-16 and 2027-01-01 (future to fake clock) → Terms.Version "2026-11-16"
func TestCurrent_CacheExpiresWithClock(t *testing.T) // insert newer row; within 60s still old; Advance(61s) → new
func TestValidateAcceptance_Outdated(t *testing.T) // → apperr Conflict legal.version_outdated, details terms_version/privacy_version current
func TestGetLegalCurrent_Envelope(t *testing.T) // via newHandler: 200, data.terms.version "2026-11-16", data.privacy.url_en set, meta.request_id
// cmd/seed
func TestSeed_InsertsLegalDocumentsIdempotently(t *testing.T) // run twice → 2 rows total
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/infrastructure/database/... ./internal/features/legal/... ./cmd/...` → FAIL.
- [ ] **Step 3: Write the migration and queries**, then `go tool sqlc generate -f internal/infrastructure/database/sqlc.yaml` → no errors.
- [ ] **Step 4: Implement** the `legal` feature, `cmd/seed` (lot seed + `legal.SeedDocuments` in one transaction), and `deps`/`newHandler` in `cmd/api`.
- [ ] **Step 5: Run** — `$IT go test -race ./... && go tool sqlc diff -f internal/infrastructure/database/sqlc.yaml` → `ok`, empty diff.
- [ ] **Step 6: Commit**

```bash
git add apps/server docs/architecture.md
git commit -m "feat(server): add identity schema, legal documents and current legal endpoint"
```

---

## Task A12: Audit log and outbox (write side)

**Files:**
- Create: `migrations/00004_audit_outbox.sql`, `queries/{audit,outbox}.sql`, `internal/features/audit/{audit.go,postgres/recorder.go}` + tests, `internal/features/notifications/{notifications.go,postgres/outbox.go}` + tests
- Modify: `dbgen/*`, `grants_test.go`

**Interfaces:**
- Consumes: `database.MustTx` (A10), `reqctx.RequestID` (A1), `middleware.ClientIPFrom` (A4).
- Produces:
  - Tables `audit_log` and `outbox_messages` per spec §5.6–§5.7, including `revoke update, delete, truncate on audit_log from mypark_app`. The `outbox_messages.template` check list is the spec's five.
  - Audit API:
    - `type audit.ActorKind string` with `ActorUser`, `ActorAdmin`, `ActorSystem`, `ActorAnonymous`.
    - `type audit.Entry struct{ Action string; Actor ActorKind; ActorUserID *int64; EntityType, EntityID string; Before, After map[string]any }`.
    - `type audit.Recorder interface{ Record(ctx context.Context, e Entry) error }`. `audit.NewRecorder() Recorder` fills `ip` and `request_id` from ctx and fails with `database.ErrNoTx` outside a tx.
    - Action constants for every spec §12 row: `audit.AuthRegistered`, … `audit.AdminSeeded`. Two more constants come from OC-39: `audit.AdminInvitationReissued = "admin.invitation_reissued"` and the metadata key `replaced_pending_driver`.
  - Notifications API:
    - `type notifications.Template string` with `TplVerifyEmail`, `TplPasswordReset`, `TplPasswordChanged`, `TplAccountExists`, `TplAdminInvite`.
    - `type notifications.Email struct{ Template Template; Locale string; To string; UserID *int64; Data map[string]string; DedupeKey string }`.
    - `type notifications.Notifier interface{ Enqueue(ctx context.Context, e Email) error; ScrubPendingForUser(ctx context.Context, userID int64) error }`. Both fail outside a tx. Enqueue uses `on conflict (dedupe_key) do nothing`.
    - `ScrubPendingForUser` removes `payload.code` from `pending`/`sending` rows. Account deletion uses it (spec §7.8). The port is missing from spec §4.1, so this plan adds it.

- [ ] **Step 1: Write the failing tests**

```go
func TestGrants_AuditLogAppendOnly(t *testing.T) // as App: insert ok; update/delete/truncate → SQLSTATE 42501 insufficient_privilege
func TestGrants_AppDMLOnOutbox(t *testing.T)
func TestRecord_FailsOutsideTx(t *testing.T)     // errors.Is(err, database.ErrNoTx)
func TestRecord_FillsIPAndRequestID(t *testing.T) // ctx with reqctx id "r1" + client ip 198.51.100.7 → row.ip, row.request_id
func TestRecord_RollsBackWithAction(t *testing.T) // InTx(record; return err) → no row
func TestEnqueue_FailsOutsideTx(t *testing.T)
func TestEnqueue_DedupeKeyIsIdempotent(t *testing.T) // same key twice → 1 row
func TestScrubPendingForUser_RemovesCodeOnly(t *testing.T) // payload {"code":"123456","first_name":"Ana"} → {"first_name":"Ana"}; sent rows untouched
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/audit/... ./internal/features/notifications/... ./internal/infrastructure/database/...` → FAIL.
- [ ] **Step 3: Migration + queries + `sqlc generate`.**
- [ ] **Step 4: Implement** `audit` and `notifications` (enqueue and scrub only).
- [ ] **Step 5: Run** — same command + `sqlc diff` → `ok`.
- [ ] **Step 6: Commit**

```bash
git add apps/server
git commit -m "feat(server): add append-only audit log and transactional email outbox"
```

---

## Task A13: Register, verify, resend (+ OC-38 invitee rules, anti-enumeration)

**Files:**
- Create: `internal/features/auth/{email.go,password.go,code.go,ports.go,service.go,service_register.go,http.go,postgres/repo.go}` + `email_test.go`, `password_test.go`, `code_test.go`, `register_test.go` (integration), `http_test.go`
- Modify: `queries/auth.sql`, `dbgen/*`, `cmd/api/main.go` (wire auth)

**Interfaces:**
- Consumes: `hashing.PasswordHasher`, `hashing.CodeHasher` (A8); `legal.Current` (A11); `audit.Recorder`, `notifications.Notifier` (A12); `TxRunnerPort` (A10); `ratelimit.Limiter` + rules (A7); `clock.Clock`; `apperr`, `httpx`, `secret`.
- Produces:
  - **Domain:**
    - `auth.NormalizeEmail(raw string) string`: trims and lowercases. It is used before validation, lookups and rate-limit keys.
    - `auth.NewCode(r io.Reader) (string, error)`: uniform in `[0, 999999]`, 6 digits zero-padded.
    - Constants: `auth.CodeTTL = 15*time.Minute`, `auth.InviteTTL = 48*time.Hour`, `auth.MaxCodeAttempts = 5`.
    - `type auth.Purpose string` with `PurposeVerifyEmail`, `PurposeResetPassword`, `PurposeAdminInvite`.
  - **Service:**
    - `auth.NewService(d Deps) *Service`, where `type Deps struct{ Tx database.TxRunnerPort; Repo Repo; Hasher hashing.PasswordHasher; Codes hashing.CodeHasher; Legal legal.Current; Audit audit.Recorder; Notify notifications.Notifier; Clock clock.Clock; Rand io.Reader; Signer *jwt.Signer }`. `Rand` defaults to `crypto/rand.Reader`.
    - `(*Service).Register(ctx context.Context, in RegisterInput) error`, `(*Service).Verify(ctx context.Context, email string, code secret.String) error` and `(*Service).Resend(ctx context.Context, email string) error`.
    - `type RegisterInput struct{ Email string; Password secret.String; FirstName, LastName string; Phone *string; Locale string; TermsVersion, PrivacyVersion string }`.
    - The unexported `issueCode(ctx, user userRow, p Purpose, ttl time.Duration) (code string, codeID int64, err error)` consumes the old live code and inserts the new one. `checkCode(ctx, userID int64, publicID uuid.UUID, p Purpose, code string) (ok bool, err error)` uses the commit-first attempts update from spec §7.5 with `@now`.
  - **HTTP:** `auth.NewHandler(s *Service, v *validation.Validator, l ratelimit.Limiter, log *slog.Logger) *Handler` and `(*Handler).Register(r *router.Router)`. Routes and policies:
    - `POST /v1/auth/register` — `Policy{Public:true, Limit:&ratelimit.RegisterIP}`.
    - `POST /v1/auth/verify` — `CodeSubmitIP`.
    - `POST /v1/auth/verify/resend` — `CodeRequestIP`.
    - Handlers check the email rules in the handler after decoding: `CodeRequestEmail` on register and resend.
  - Request DTOs have validate tags exactly as spec §6.2. Names are trimmed before validation. `phone` is trimmed, and `""` becomes `nil` (Review Focus 1).

- [ ] **Step 1: Write the failing unit tests**

```go
func TestNormalizeEmail(t *testing.T) // " Ana@Example.COM " → "ana@example.com"   (Review Focus 2)
func TestNewCode_RangeAndFormat(t *testing.T) // fixed reader bytes → "000000", "999999", "042137"; always len 6, digits only; rejection sampling keeps uniformity (reader yielding 0xFFFFFFFF first is skipped)
func TestPasswordPolicy(t *testing.T) // 9 chars → min; 129 chars → max; "Password123" (common, any case) → password_common; 10-char unicode passphrase counted by runes → ok
```

- [ ] **Step 2: Write the failing integration tests** (`register_test.go`, using the real service on `NewTestDB` with `legal.SeedDocuments`, a FakeClock and the real `hashing` with low params)

```go
func TestRegister_NewEmailCreatesPendingDriverCodeOutboxAudit(t *testing.T) // users row role "driver", status "pending_verification", legal versions = "2026-11-16"; one live verify_email code whose code_hash == CodeHasher.Hash(purpose,user,code-from-outbox); outbox template "verify_email", dedupe "verify_email:<code id>", payload.code 6 digits; audit "auth.registered" actor anonymous, after {"locale":"es-DO"}
func TestRegister_AtomicOnFailureAfterEnqueue(t *testing.T) // Audit fake that errors → no users, auth_codes, outbox rows
func TestRegister_RoleInBodyIgnored(t *testing.T)            // via HTTP with "role":"admin" → users.role "driver"
func TestRegister_OutdatedLegal409(t *testing.T)             // accepted_terms_version "2025-01-01" → 409 legal.version_outdated, details.terms_version "2026-11-16"
func TestRegister_PendingDriverIsOverwritten(t *testing.T)   // second register same email: new password verifies, names replaced, old code consumed, new code live, audit "auth.registration_replaced"
func TestRegister_ActiveOwnerGetsAccountExistsEmail(t *testing.T) // users unchanged; outbox template "account_exists", dedupe "account_exists:<user id>:<yyyymmddhh of fake now>"; twice in same hour → 1 row
func TestRegister_PendingAdminInviteeUntouched(t *testing.T) // OC-38: invitee row (role admin, pending, password null) → unchanged; no outbox row; no code change; response 202 identical
func TestRegister_EveryPathHashesOnce(t *testing.T)          // spy hasher: new, pending, active, invitee → exactly 1 Hash call each
func TestRegister_IdenticalResponses(t *testing.T)           // new / pending / active / invitee via HTTP: status 202, body equal after deleting meta.request_id, data == {"status":"verification_pending"}, same header names
func TestRegister_BlankNamesAndEmptyPhone(t *testing.T)      // first_name "   " → 422 fields[0] {field:"first_name",rule:"required"}; phone "" → 202 and users.phone IS NULL   (Review Focus 1)
func TestVerify_ActivatesDriver(t *testing.T)                // correct code → 204 no body; status "active", email_verified_at == fake now; code consumed; audit "auth.email_verified"
func TestVerify_AttemptsPersistAcrossFailures(t *testing.T)  // 5 wrong → each 422 auth.code_invalid; attempts == 5 in DB; 6th with the right code → 422 auth.code_invalid
func TestVerify_ExpiredCode(t *testing.T)                    // Advance(15m+1s) → 422 auth.code_invalid
func TestVerify_UnknownEmailSameAsWrongCode(t *testing.T)    // bodies equal minus meta.request_id
func TestVerify_InviteeBehavesLikeUnknown(t *testing.T)      // OC-38: verify with the admin_invite code against an invitee → 422; user still pending; no driver code issued
func TestResend_PendingDriverReissues(t *testing.T)          // old code consumed, new outbox row; 202 data {"status":"accepted"}
func TestResend_OthersDoNothing(t *testing.T)                // unknown, active, invitee → 202 identical, no rows written
func TestRegister_EmailRateLimit(t *testing.T)               // fake Limiter denying CodeRequestEmail with RetryAfter 30m → 429 rate_limit.exceeded, Retry-After "1800", no user row, Hash not called
```

- [ ] **Step 3: Run to verify they fail** — `$IT go test -race ./internal/features/auth/...` → FAIL (undefined).
- [ ] **Step 4: Add queries + `sqlc generate`. Implement** the domain files, the service and the handler, then wire `cmd/api` (`ratelimit.Noop{}` until A22).
- [ ] **Step 5: Run** — `$IT go test -race ./internal/features/auth/... ./cmd/... && go test -cover ./internal/features/auth/` → `ok`.
- [ ] **Step 6: Commit**

```bash
git add apps/server
git commit -m "feat(server): add driver registration, email verification and resend"
```

---

## Task A14: Login and session creation (+ timing equalization)

**Files:**
- Create: `internal/features/auth/{session.go,service_login.go}` + `session_test.go`, `login_test.go`
- Modify: `queries/auth.sql`, `dbgen/*`, `auth/http.go`

**Interfaces:**
- Consumes: A13 service and repo, `jwt.Signer` (A9), `legal.Current`.
- Produces:
  - Constants: `auth.RefreshTTL = 30*24*time.Hour`, `auth.SessionAbsoluteTTL = 90*24*time.Hour`.
  - `auth.SuccessorExpiry(now, absolute time.Time) time.Time` returns `min(now+RefreshTTL, absolute)`.
  - `auth.NewRefreshToken(r io.Reader) (token string, hash []byte, err error)`: 32 bytes, base64url without padding, SHA-256.
  - `type auth.ClientKind string` with `ClientWeb`, `ClientMobile`.
  - `type auth.TokenPair struct{ AccessToken string; AccessExpiresIn int; RefreshToken secret.String; RefreshExpiresIn int; LegalAcceptanceRequired bool; User UserView }`.
  - `type auth.UserView struct{ PublicID uuid.UUID; Email, FirstName, LastName string; Phone *string; Role authz.Role; Status, Locale string; EmailVerifiedAt *time.Time; TermsVersion, PrivacyVersion *string; TermsAcceptedAt, PrivacyAcceptedAt *time.Time; CreatedAt time.Time }`. It is shared with `users` (A20) for the `User` JSON shape.
  - `(*Service).Login(ctx context.Context, in LoginInput) (TokenPair, error)` with `type LoginInput struct{ Email string; Password secret.String; ClientKind ClientKind; IP netip.Addr; UserAgent string }`.
  - `auth.ToUserJSON(UserView) any` is the exact spec §6.2 `User` shape.
  - The login and refresh handlers map `TokenPair` to an unexported response struct with plain `string` fields (`RefreshToken.Reveal()`). Never encode `TokenPair` directly: `secret.String` marshals to `"[redacted]"`.
  - Route `POST /v1/auth/login` with `Policy{Public:true}`. The handler checks `LoginIPEmail` (key `ip + "|" + email`) and then `LoginEmail` **before** any hashing or lookup.

- [ ] **Step 1: Write the failing tests**

```go
func TestSuccessorExpiry_SlidingCappedByAbsolute(t *testing.T) // now+30d < abs → now+30d; abs earlier → abs
func TestNewRefreshToken_FormatAndHash(t *testing.T)            // len(token)==43, base64url decodes to 32 bytes, hash == sha256(raw)
func TestLogin_Success(t *testing.T) // 200 data.token_type "Bearer", expires_in 900, refresh_expires_in 2592000, legal_acceptance_required false, data.refresh_token base64url-decodes to 32 bytes (not "[redacted]"), user.first_name/last_name, no "full_name"; one auth_sessions row client_kind "mobile", absolute_expires_at == now+90d; refresh_tokens.expires_at == now+30d; audit "auth.login_succeeded" entity session, after {"client_kind":"mobile"}
func TestLogin_OneVerifyPerPath(t *testing.T) // spy hasher: unknown email, deleted user, invitee with null hash, wrong password, correct password → exactly 1 Verify each; unknown verifies against DummyHash()
func TestLogin_WrongPasswordAndUnknownIdentical(t *testing.T) // both 401 auth.invalid_credentials; bodies equal minus meta.request_id
func TestLogin_NotVerifiedOnlyWithCorrectPassword(t *testing.T) // pending + wrong pw → 401 invalid_credentials; pending + right pw → 403 auth.email_not_verified
func TestLogin_SuspendedOnlyWithCorrectPassword(t *testing.T)   // → 403 auth.account_suspended
func TestLogin_FailedAuditCommitsForKnownEmailOnly(t *testing.T) // wrong pw on known → audit "auth.login_failed" after {"reason":"password"} persists despite 401; unknown email → no audit row
func TestLogin_RehashOnOldParams(t *testing.T) // stored hash with old params → after login password_hash has DefaultParams prefix
func TestLogin_LegalPendingDriver(t *testing.T) // driver accepted "2025-01-01" → 200, legal_acceptance_required true
func TestLogin_RateLimitBeforeHashing(t *testing.T) // fake limiter denies LoginIPEmail → 429, spy Verify count 0
func TestLogin_SameAccountWebAndMobile(t *testing.T) // two logins → two sessions, both refresh tokens valid
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/auth/...` → FAIL.
- [ ] **Step 3: Implement**. Failed-login audit and rehash use the commit-first pattern from spec §4.5: the outcome is decided inside `InTx`, and the error is returned after the commit.
- [ ] **Step 4: Run** — same → `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add login with sessions, rehash and timing equalization"
```

---

## Task A15: Authenticator, bearer/role/legal gates, logout, logout-all

**Files:**
- Create: `internal/features/auth/authenticator.go` + test, `internal/transport/http/authn/authn.go` + test, `internal/features/auth/logout_test.go`
- Modify: `queries/auth.sql`, `dbgen/*`, `auth/http.go`, `cmd/api/main.go` (router `Middlewares` from authn)
- Docs: `docs/architecture.md` §5 (per-request session check, no grace window, BFF single-flight, `X-Forwarded-For` contract), §17 ADR "stateful session check"

**Interfaces:**
- Consumes: `jwt.Verifier` (A9), `legal.Current`, `authz`, `reqctx.Fields`, `router.Middlewares` (A7).
- Produces:
  - `auth.NewAuthenticator(v *jwt.Verifier, repo Repo, l legal.Current, c clock.Clock) *Authenticator`.
  - `(*Authenticator).Authenticate(ctx context.Context, bearer string) (authz.Principal, error)` follows spec §7.3. It runs exactly one query (`GetSessionPrincipal`), and the absolute expiry is compared with `@now`.
  - `authn.New(a interface{ Authenticate(context.Context, string) (authz.Principal, error) }, log *slog.Logger) *Gates` with the methods `(*Gates).Authn(next) http.Handler`, `(*Gates).Roles(roles []authz.Role) func(http.Handler) http.Handler` and `(*Gates).Legal(next) http.Handler`.
    - `Legal` returns `403 legal.acceptance_required` with `details{terms_version, privacy_version}` from `legal.Current`.
    - `Authn` fills `reqctx.Fields(ctx).UserPublicID`.
  - `(*Service).Logout(ctx context.Context, p authz.Principal) error` and `(*Service).LogoutAll(ctx context.Context, p authz.Principal) (revoked int, err error)` with the audits `auth.logout` (entity session, after `{sessions_revoked:1}`) and `auth.logout_all` (entity user).
  - Routes: `POST /v1/auth/logout` and `POST /v1/auth/logout-all`, both `Policy{Roles: router.AnyRole, LegalExempt: true}` and both 204.

- [ ] **Step 1: Write the failing tests**

```go
func TestAuthenticate_Table(t *testing.T) // rows: valid → Principal{Role driver, LegalPending false}; garbage/expired/alg none/unknown kid → auth.unauthenticated; unknown sid → auth.unauthenticated; revoked session → auth.session_revoked; Advance past absolute_expires_at → auth.session_revoked; suspended → 403 auth.account_suspended; deleted or pending → auth.session_revoked; DB role admin vs claim driver → auth.session_revoked
func TestAuthenticate_LegalPendingDriversOnly(t *testing.T) // outdated driver → LegalPending true; admin with null versions → false
func TestGates_MissingOrMalformedBearer(t *testing.T) // no header, "Basic x", "Bearer " → 401 auth.unauthenticated envelope
func TestGates_RoleDenied(t *testing.T)    // driver principal on Roles{admin} → 403 auth.forbidden
func TestGates_LegalBlocksNonExempt(t *testing.T) // LegalPending driver → 403 legal.acceptance_required details.terms_version "2026-11-16"
func TestGates_AccessLogHasUserID(t *testing.T)
func TestLogout_RevokesOnlyCurrentSession(t *testing.T) // two sessions; logout with A → A revoked_reason "logout"; B's access token still 200 on a probe route; A's access token → 401 auth.session_revoked immediately
func TestLogout_SecondCallWithRevokedTokenIs401(t *testing.T) // second call with the same (now revoked) token → 401 auth.session_revoked (OC-4 session check), DB unchanged
func TestLogoutAll_RevokesEverySession(t *testing.T) // 3 sessions → all revoked "logout_all"; audit after {"sessions_revoked":3}
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/auth/... ./internal/transport/http/authn/...` → FAIL.
- [ ] **Step 3: Implement** and wire the router `Middlewares{ClientIP: middleware.ClientIP(cfg.TrustedProxyCIDRs()), Authn: g.Authn, Roles: g.Roles, Legal: g.Legal, RateLimit: …}` in `cmd/api`.
- [ ] **Step 4: Run** — `$IT go test -race ./...` → `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server docs/architecture.md
git commit -m "feat(server): add per-request session check, role and legal gates, logout"
```

---

## Task A16: Refresh rotation and reuse detection

**Files:**
- Create: `internal/features/auth/{refresh.go,service_refresh.go}` + `refresh_test.go` (unit), `refresh_integration_test.go`
- Modify: `queries/auth.sql`, `dbgen/*`, `auth/http.go`

**Interfaces:**
- Consumes: A14 session helpers, `TxRunnerPort`.
- Produces:
  - `type auth.RefreshSnapshot struct{ Found bool; TokenRotatedAt, SessionRevokedAt *time.Time; TokenExpiresAt, SessionAbsoluteExpiresAt time.Time; UserStatus string }`.
  - `type auth.RefreshOutcome int` with `RefreshOK`, `RefreshInvalid`, `RefreshRevoked`, `RefreshReuse` and `RefreshUserInactive`.
  - `auth.DecideRefresh(s RefreshSnapshot, now time.Time) RefreshOutcome`. It is a pure function in spec §7.4 order: not found → revoked → reuse → expired/absolute → user inactive → ok.
  - `(*Service).Refresh(ctx context.Context, token secret.String, ip netip.Addr) (TokenPair, error)`. It uses `select … for update of t, s` and commit-first, so reuse revokes the family (`reuse_detected`) and audits `auth.refresh_reuse_detected` (actor system, entity session, after `{user_id: <public id>}`) before the 401.
  - User inactive (a gap in the spec): `revoked_reason` has no value for it. For status `deleted`, the session is revoked with `account_deleted`. For `suspended`/`pending`, the service returns `auth.session_revoked` without a write. M4 suspend will revoke sessions itself.
  - Route `POST /v1/auth/refresh` — `Policy{Public:true, Limit:&ratelimit.RefreshIP}`.

- [ ] **Step 1: Write the failing unit test**

```go
func TestDecideRefresh_Table(t *testing.T) // not found→Invalid; revoked→Revoked; rotated→Reuse (even if also expired); expired token→Invalid; absolute passed→Invalid; status suspended→UserInactive; fresh→OK
```

- [ ] **Step 2: Write the failing integration tests**

```go
func TestRefresh_RotatesAndSlides(t *testing.T) // Advance(10d) → new pair; old token rotated_at == now; successor expires_at == now+30d; session last_used_at == now
func TestRefresh_SuccessorCappedByAbsolute(t *testing.T) // Advance(80d) via successive refreshes → successor expires_at == absolute_expires_at
func TestRefresh_ReuseRevokesFamily(t *testing.T) // spec scenario: login → R1 → refresh → R2 → present R1 → 401 auth.session_revoked; revoked_reason "reuse_detected"; audit row present; then R2 → 401 auth.session_revoked; access token from R2 → 401 auth.session_revoked
func TestRefresh_ReuseRevocationSurvivesErrorResponse(t *testing.T) // after the 401, read auth_sessions and audit_log in a new connection: both committed
func TestRefresh_ConcurrentSameToken(t *testing.T) // 20 goroutines (wg.Go) present R1 → successes <= 1; session revoked "reuse_detected"; all return within 10s (no deadlock)
func TestRefresh_ExpiredIsInvalid(t *testing.T)   // Advance(30d+1s) → 401 auth.refresh_invalid
func TestRefresh_AfterAbsoluteCapIsInvalid(t *testing.T)
func TestRefresh_DeletedUserRevokes(t *testing.T) // status deleted → 401 auth.session_revoked, revoked_reason "account_deleted"
func TestRefresh_MalformedTokenIsInvalid(t *testing.T) // "", "!!!", 10 chars, 200 chars → 401 auth.refresh_invalid, no 500   (Review Focus 5)
```

- [ ] **Step 3: Run to verify they fail** — `$IT go test -race ./internal/features/auth/... -run 'Refresh'` → FAIL.
- [ ] **Step 4: Implement** following the spec §4.5 pattern.
- [ ] **Step 5: Run** — `$IT go test -race -count=3 ./internal/features/auth/... -run 'Refresh'` → `ok` all three runs.
- [ ] **Step 6: Commit**

```bash
git add apps/server
git commit -m "feat(server): add refresh rotation with family revocation on reuse"
```

---

## Task A17: Forgot and reset password

**Files:**
- Create: `internal/features/auth/service_reset.go`, `reset_test.go`
- Modify: `queries/auth.sql`, `dbgen/*`, `auth/http.go`

**Interfaces:**
- Consumes: `issueCode`/`checkCode` (A13), session revocation (A15).
- Produces:
  - `(*Service).Forgot(ctx context.Context, email string) error` and `(*Service).Reset(ctx context.Context, email string, code, newPassword secret.String) error`.
  - Unexported `revokeAllSessions(ctx, userID int64, reason string) (int, error)`, which A20 and A21 reuse through `Accounts`.
  - Routes: `POST /v1/auth/password/forgot` (`CodeRequestIP` + `CodeRequestEmail` in the handler, 202 `{status:"accepted"}`) and `POST /v1/auth/password/reset` (`CodeSubmitIP`, 204).

- [ ] **Step 1: Write the failing tests**

```go
func TestForgot_ActiveUserGetsResetCode(t *testing.T) // outbox "password_reset", code purpose reset_password expires now+15m; audit "auth.password_reset_requested" actor anonymous
func TestForgot_UnknownPendingSuspendedDoNothing(t *testing.T) // 202 identical bodies minus meta.request_id; no rows
func TestReset_ChangesPasswordAndRevokesAllSessions(t *testing.T) // 2 sessions; reset → 204; old password fails login, new works; password_changed_at == now; both sessions revoked "password_reset"; previous access tokens → 401 auth.session_revoked immediately
func TestReset_ConsumesOtherLiveCodes(t *testing.T) // live verify/reset codes → consumed
func TestReset_EnqueuesPasswordChanged(t *testing.T) // outbox "password_changed" (OC-36); audit "auth.password_reset" after {"sessions_revoked":2}
func TestReset_WeakPassword422(t *testing.T)       // new_password "password12" → 422 rule password_common; code attempts unchanged
func TestReset_WrongCodePersistsAttempt(t *testing.T) // 422 auth.code_invalid, attempts == 1 after the failing request
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/auth/... -run 'Forgot|Reset'` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — same → `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add forgot and reset password with session revocation"
```

---

## Task A18: Worker delivery — SMTP, templates, dispatcher, purge jobs, Mailpit CI

**Files:**
- Create: `internal/infrastructure/email/smtp.go` + test, `internal/infrastructure/jobs/jobs.go` + test, `internal/features/notifications/{templates.go,dispatcher.go,purge.go}` + tests, `templates/{es-DO,en}/{verify_email,password_reset,password_changed,account_exists,admin_invite}.{subject.txt,txt,html}`, `templates/_brand.{txt,html}`, `internal/config/worker.go` + test, `internal/testsupport/mailpit.go`
- Modify: `queries/outbox.sql`, `queries/auth.sql` (purge), `dbgen/*`, `cmd/worker/main.go` + test, `deploy/compose/docker-compose.yml` (worker SMTP env), `.github/workflows/ci-server.yml` (mailpit service + env)

**Interfaces:**
- Consumes: outbox table (A12), `clock.Clock`.
- Produces:
  - `type email.Message struct{ From, To, Subject, Text, HTML, MessageID string }`, `type email.Sender interface{ Send(ctx context.Context, m Message) error }` and `email.NewSMTP(cfg SMTPConfig) *SMTP`. `SMTPConfig` has `Host string; Port int; Username, Password secret.String; StartTLS bool`. Messages are multipart/alternative.
  - `email.MessageID(dedupeKey string) string` returns `"<" + hex(sha256(dedupeKey))[:32] + "@mypark.local>"` (OC-24).
  - `notifications.Render(t Template, locale string, data map[string]string) (subject, text, html string, err error)` over `embed.FS`.
  - `notifications.NewDispatcher(pool *pgxpool.Pool, s email.Sender, c clock.Clock, from string, log *slog.Logger) *Dispatcher` and `(*Dispatcher).Tick(ctx context.Context) (sent int, err error)`. The lifecycle is claim → send outside any tx → finish, as in spec §11.3. Batch size is 10, the lease is 1 min, and the message goes dead after 8 attempts.
  - `notifications.Backoff(attempts int) time.Duration` returns `min(5s·2^attempts, 15m)`.
  - `jobs.Job{Name string; Every time.Duration; Run func(ctx context.Context) error}` and `jobs.Run(ctx context.Context, log *slog.Logger, js ...Job)`. One goroutine per job, panic-safe, and it stops on ctx.
  - Purge jobs: `notifications.PurgeOutbox` (24 h, `sent`/`dead` older than 90 days) and `auth.Purge` (1 h, spec §11.3 auth row). The rate-limit purge lands in A22.
  - `config.LoadWorker(getenv) (WorkerConfig, error)` reads `DATABASE_URL`, `WORKER_HEALTH_ADDR`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_FROM`, `SMTP_USERNAME`, `SMTP_PASSWORD` and `SMTP_STARTTLS`. Compose defaults are `mailpit`, `1025` and `MyPark <no-reply@mypark.local>`.
  - `testsupport.Mailpit(t) (smtpAddr, apiURL string)` skips locally when unset and fails when `CI=true` (OC-34). `testsupport.MailpitMessages(t, apiURL, to string) []MailpitMessage` reads `GET /api/v1/messages`, and `DELETE /api/v1/messages` clears the inbox at test start.

- [ ] **Step 1: Write the failing tests**

```go
func TestTemplates_ParityAndRender(t *testing.T) // every Template × {es-DO,en} has 3 files; Render with sample {first_name:"Ana",code:"123456"} → subject non-empty, text contains "123456", html escapes "<b>" in first_name, both contain "MyPark · Banreservas Zona Colonial"
func TestTemplates_UnknownLocaleErrors(t *testing.T)
func TestBackoff_Schedule(t *testing.T) // 1→10s, 2→20s, 7→10m40s, 8→15m (cap)
func TestDispatcher_SendsAndScrubs(t *testing.T) // fake Sender; Tick → status "sent", sent_at == now, payload has no "code"; Message-ID == email.MessageID(dedupe)
func TestDispatcher_RetryWithBackoff(t *testing.T) // Sender errors "550 mailbox <x@y>" → status "pending", attempts 1, available_at == now+10s, last_error ≤ 500 chars and has no payload values
func TestDispatcher_DeadAfterMax(t *testing.T)     // 8 failing ticks (Advance each backoff) → "dead", code scrubbed, error log line
func TestDispatcher_LeaseReclaimAfterCrash(t *testing.T) // claim via Tick with a Sender that blocks then ctx cancel → row "sending"; Advance(61s); new Tick sends it
func TestDispatcher_ConcurrentTicksNoDoubleSend(t *testing.T) // 2 dispatchers, 10 rows → each row sent exactly once (skip locked)
func TestJobs_PanicDoesNotKillRunner(t *testing.T)
func TestPurgeOutbox_And_AuthPurge(t *testing.T) // rows at now-91d removed, now-89d kept; codes/tokens 30d past expiry removed
func TestLoadWorker_DefaultsAndRequired(t *testing.T)
func TestDispatcher_DeliversToMailpit(t *testing.T) // spec §15.6: enqueue verify_email es-DO and en → Tick → Mailpit has 2 messages; es-DO subject is the Spanish subject, en the English one; body contains the code; Message-ID set; DB payload scrubbed
```

- [ ] **Step 2: Run to verify they fail** — `$IT TEST_SMTP_ADDR=localhost:51025 TEST_MAILPIT_API=http://localhost:58025 go test -race ./internal/features/notifications/... ./internal/infrastructure/email/... ./internal/infrastructure/jobs/... ./internal/config/... ./cmd/worker/...` → FAIL.
- [ ] **Step 3: Implement**, write es-DO and en copy (codes as plain text, no links), and wire `cmd/worker`. The health server stays up, and the jobs run under the same ctx.
- [ ] **Step 4: CI** — add a `mailpit` service (`axllent/mailpit:v1.33.0`, ports `1025:1025`, `8025:8025`) to the `server` job and `TEST_SMTP_ADDR: localhost:1025`, `TEST_MAILPIT_API: http://localhost:8025` to the Test step env.
- [ ] **Step 5: Run** — same command as Step 2 → `ok`. Then `docker compose -f deploy/compose/docker-compose.yml up -d --build worker` → healthy.
- [ ] **Step 6: Commit**

```bash
git add apps/server deploy/compose/docker-compose.yml .github/workflows/ci-server.yml
git commit -m "feat(server): deliver outbox emails via smtp with templates, retries and purge jobs"
```

---

## Task A19: Vehicles (FR-VEH-1…5)

Vehicles come before Me (spec S10 before S9; both depend only on S6). Account deletion in A20 has to anonymize real vehicle rows, so the table must exist first.

**Files:**
- Create: `migrations/00005_vehicles.sql`, `queries/vehicles.sql`, `internal/features/vehicles/{plate.go,limit.go,service.go,ports.go,http.go,postgres/repo.go}` + `plate_test.go`, `limit_test.go`, `service_test.go` (integration), `http_test.go`
- Modify: `dbgen/*`, `cmd/api/main.go`, `grants_test.go`

**Interfaces:**
- Consumes: `authz.Principal`, `TxRunnerPort`, `apperr`, `httpx`, `router.DriverOnly`, `ratelimit.VehicleWriteUser`, `config.MaxVehiclesPerUser`.
- Produces:
  - `vehicles.NormalizePlate(raw string) (normalized, display string, err error)`. It uppercases with `unicode`, drops spaces and `-`, and requires `^[A-Z0-9]{2,10}$`. `display` is the trimmed input. On failure it returns `apperr` Invalid `validation.failed` with field `plate`, rule `plate`.
  - `vehicles.LimitReached(active, max int) bool`.
  - `type vehicles.Vehicle struct{ PublicID uuid.UUID; Plate, PlateNormalized, Make, Model, Color string; CreatedAt, UpdatedAt time.Time }`.
  - `type vehicles.ReservationChecker interface{ HasActive(ctx context.Context, vehicleID int64) (bool, error) }` and `vehicles.NoReservations{}` (always false, OC-29).
  - `vehicles.NewService(tx database.TxRunnerPort, repo Repo, rc ReservationChecker, max int) *Service` with:
    - `List(ctx context.Context, p authz.Principal) ([]Vehicle, error)`
    - `Create(ctx context.Context, p authz.Principal, in CreateInput) (Vehicle, error)`
    - `Get(ctx context.Context, p authz.Principal, id uuid.UUID) (Vehicle, error)`
    - `Update(ctx context.Context, p authz.Principal, id uuid.UUID, in UpdateInput) (Vehicle, error)`
    - `Delete(ctx context.Context, p authz.Principal, id uuid.UUID) error`
    - `EraseOwner(ctx context.Context, ownerID int64) error`, the anonymization from spec §7.8, called by A20 inside its tx.
    - Every method re-asserts `p.Role == driver` and returns `Forbidden(auth.forbidden)` otherwise.
  - Repo methods all take `ownerID`: `ListOwned`, `GetOwned`, `UpdateOwned`, `SoftDeleteOwned`, `CountActiveOwned`, `LockOwner`. No unscoped lookup exists.
  - Routes (policy `Roles: router.DriverOnly`): `GET /v1/vehicles`, `POST /v1/vehicles`, `GET /v1/vehicles/{id}`, `PATCH /v1/vehicles/{id}` and `DELETE /v1/vehicles/{id}`. The POST, PATCH and DELETE routes add `Limit: &ratelimit.VehicleWriteUser`. List responses use `response.List` with `Page{Limit: max}`.

- [ ] **Step 1: Write the failing unit tests**

```go
func TestNormalizePlate(t *testing.T) // "a-123 456"→"A123456"; " ab12 "→"AB12"; "Á123"→err; "A"→err; "ABCDEFGHIJK"→err (11); "A#12"→err; "a 123"→err (non-ASCII space); "ab-12"→display "ab-12"
func FuzzNormalizePlate(f *testing.F) // never panics; ok results always match ^[A-Z0-9]{2,10}$
func TestLimitReached(t *testing.T) // (4,5) false, (5,5) true, (6,5) true
```

- [ ] **Step 2: Write the failing integration tests**

```go
func TestCreate_AndListOwnOnly(t *testing.T)   // 201 data.plate "A-123 456", plate_normalized "A123456"; other driver's list empty
func TestCreate_PlateTakenAcrossAccounts(t *testing.T) // 409 vehicle.plate_taken (also same driver duplicate)
func TestCreate_ReuseDeletedPlate(t *testing.T) // delete then another driver creates same plate → 201
func TestCreate_LimitReached(t *testing.T)     // 6th → 409 vehicle.limit_reached details {"limit":5}
func TestVehicles_ConcurrentCreateRespectsLimit(t *testing.T) // 10 parallel creates, distinct plates → exactly 5 succeed, 5 limit_reached
func TestVehicles_ConcurrentSamePlateTwoUsers(t *testing.T)   // → one 201, one 409 plate_taken
func TestOwnership_OtherDriverGets404Identical(t *testing.T)  // GET/PATCH/DELETE other's id → 404 vehicle.not_found; body equal (minus meta.request_id) to a random uuid and to "not-a-uuid"
func TestUpdate_RenormalizesAndBumpsUpdatedAt(t *testing.T)
func TestUpdate_PlateTaken(t *testing.T)
func TestDelete_InUseFromChecker(t *testing.T) // fake checker true → 409 vehicle.in_use; row unchanged
func TestAdminGets403OnVehicles(t *testing.T)  // admin principal → 403 auth.forbidden (OC-16), via router and direct service call
func TestList_EnvelopePage(t *testing.T)       // meta.page == {"limit":5,"next_cursor":null}
func TestEraseOwner_FreesPlatesAndScrubs(t *testing.T) // plate_normalized "X"+8 hex, make/model/color "-", deleted_at set; plate reusable
```

- [ ] **Step 3: Run to verify they fail** — `$IT go test -race ./internal/features/vehicles/...` → FAIL.
- [ ] **Step 4: Migration (spec §5.8) + queries + `sqlc generate`; implement.** The unique violation on `vehicles_plate_active_key` maps to `VehiclePlateTaken` through `errors.AsType[*pgconn.PgError]` and `ConstraintName`.
- [ ] **Step 5: Run** — same command + `sqlc diff` → `ok`.
- [ ] **Step 6: Commit**

```bash
git add apps/server
git commit -m "feat(server): add owner-scoped vehicle crud with plate uniqueness and limit"
```

---

## Task A20: Me — get, patch, delete (anonymization), legal acceptance

**Files:**
- Create: `internal/features/users/{users.go,service.go,ports.go,http.go,postgres/repo.go}` + tests, `internal/features/auth/accounts.go` + test
- Modify: `queries/{users,auth}.sql`, `dbgen/*`, `cmd/api/main.go`

**Interfaces:**
- Consumes:
  - `auth.UserView` and `auth.ToUserJSON` (A14).
  - Revocation (A15/A17), `vehicles.Service.EraseOwner` (A19) and `notifications.Notifier.ScrubPendingForUser` (A12).
  - `legal.Current`, `audit.Recorder`, `httpx.Nullable`.
- Produces:
  - `type auth.Accounts interface`:
    - `VerifyPassword(ctx context.Context, userID int64, pw secret.String) error` returns `apperr.Forbidden(AuthPasswordIncorrect)` on a mismatch.
    - `RevokeAllSessions(ctx context.Context, userID int64, reason string) (int, error)`.
    - `Erase(ctx context.Context, userID int64) error` handles the auth-owned columns: email `deleted+<public_id>@deleted.invalid`, `password_hash` null, status `deleted`, `deleted_at`. It also revokes sessions with `account_deleted` and consumes live codes.
    - `CreateInvitedAdmin(…)` (added in A21).
    - `auth.NewAccounts(svc *Service) Accounts`.
  - `users.NewService(d Deps) *Service`, where `type Deps struct{ Tx database.TxRunnerPort; Repo Repo; Accounts auth.Accounts; Vehicles interface{ EraseOwner(context.Context, int64) error }; Notify notifications.Notifier; Legal legal.Current; Audit audit.Recorder; Clock clock.Clock }`. Methods:
    - `Get(ctx context.Context, p authz.Principal) (auth.UserView, error)`
    - `Update(ctx context.Context, p authz.Principal, in PatchInput) (auth.UserView, error)`, with `type PatchInput struct{ FirstName, LastName, Locale *string; Phone httpx.Nullable[string] }`
    - `Delete(ctx context.Context, p authz.Principal, password secret.String) error`
    - `AcceptLegal(ctx context.Context, p authz.Principal, terms, privacy string) error`
  - Routes:

    | Route | Policy | Success |
    |---|---|---|
    | `GET /v1/me` | `AnyRole, LegalExempt` | 200 `User` |
    | `PATCH /v1/me` | `AnyRole` | 200 `User` |
    | `DELETE /v1/me` | `DriverOnly, LegalExempt` | 204 |
    | `POST /v1/me/legal-acceptance` | `DriverOnly, LegalExempt` | 204 |

- [ ] **Step 1: Write the failing tests**

```go
func TestGetMe_DriverAndAdmin(t *testing.T) // 200 data.first_name, data.last_name, data.legal.terms_version; admin legal fields null
func TestGetMe_AllowedWhileLegalPending(t *testing.T)
func TestPatchMe_UpdatesAndAuditsFieldNames(t *testing.T) // {first_name:"Ana María", locale:"en"} → audit "user.profile_updated" after {"fields":["first_name","locale"]}; no values in audit
func TestPatchMe_PhoneNullClears(t *testing.T)
func TestPatchMe_EmptyPhoneClears(t *testing.T)  // phone "" → 200 data.phone null   (Review Focus 1)
func TestPatchMe_RoleAndEmailIgnored(t *testing.T) // {"role":"admin","email":"x@y.z"} → unchanged
func TestPatchMe_BlockedWhileLegalPending(t *testing.T) // 403 legal.acceptance_required
func TestDeleteMe_WrongPassword403(t *testing.T) // 403 auth.password_incorrect (not 401), nothing changed
func TestDeleteMe_AnonymizesAndFrees(t *testing.T) // email "deleted+<id>@deleted.invalid", first_name/last_name "deleted", phone null, password_hash null, status deleted; vehicles erased; sessions revoked "account_deleted"; live codes consumed; pending outbox code scrubbed; audit "user.deleted" no PII; same email registers again → 202 + new pending user; old access token → 401 auth.session_revoked
func TestDeleteMe_AllowedWhileLegalPending(t *testing.T)
func TestDeleteMe_Admin403(t *testing.T)          // 403 auth.forbidden
func TestDeleteMe_NullBody400(t *testing.T)       // body "null" → 400 http.invalid_json   (Review Focus 3)
func TestAcceptLegal_ClearsPending(t *testing.T)  // outdated driver → 204; versions + timestamps == now; audit "user.legal_accepted" after {terms_version, privacy_version}; next request to /v1/vehicles → 200
func TestAcceptLegal_Outdated409(t *testing.T)
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/users/... ./internal/features/auth/...` → FAIL.
- [ ] **Step 3: Implement**. `Delete` verifies the password **outside** the transaction (argon2 cost) and then runs one `InTx`: profile anonymization → `Accounts.Erase` → `Vehicles.EraseOwner` → `Notify.ScrubPendingForUser` → audit.
- [ ] **Step 4: Run** — `$IT go test -race ./...` → `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add me profile, legal acceptance and account deletion with anonymization"
```

---

## Task A21: Admin — first-admin seed, create/list admins, invitation accept (OC-19, OC-38, OC-39)

**Files:**
- Create: `internal/features/users/admins.go` + `admins_test.go`, `internal/features/auth/{service_invite.go,seed_admin.go}` + tests
- Modify: `queries/{users,auth}.sql`, `dbgen/*`, `auth/http.go`, `users/http.go`, `cmd/seed/main.go` + test, `internal/config/config.go` (seed config), `deploy/compose/docker-compose.yml` (seed env)

**Interfaces:**
- Consumes: `issueCode`/`checkCode` (A13), `Accounts` (A20), the outbox `admin_invite` template (A18), `hashing.PasswordHasher`.
- Produces:
  - `config.LoadSeedAdmin(getenv) (SeedAdmin, bool, error)` reads `SEED_ADMIN_EMAIL`, `SEED_ADMIN_PASSWORD`, `SEED_ADMIN_FIRST_NAME` and `SEED_ADMIN_LAST_NAME`. `bool` is false when the email is unset (skip with a warning). With `APP_ENV=production` it refuses `DevSeedAdminPassword`. Spec §13.1's single `SEED_ADMIN_NAME` contradicts §2.1 item 7, so the name is split. Compose defaults: `admin@mypark.local`, `DevSeedAdminPassword`, `MyPark`, `Admin`.
  - `auth.SeedFirstAdmin(ctx context.Context, d SeedDeps, in SeedAdmin) (created bool, err error)`. It runs inside the seed tx, is insert-if-no-non-deleted-admin, validates the password with the register policy, and audits `admin.seeded` (actor system).
  - `Accounts.CreateInvitedAdmin(ctx context.Context, actor authz.Principal, in InviteInput) (InviteResult, error)`, with `type InviteInput struct{ Email, FirstName, LastName, Locale string }` and `type InviteResult struct{ User auth.UserView; Created bool }`. It runs in one tx. Branches:
    - **New email:** insert an `admin` user in `pending_verification` with a null hash, then audit `admin.created` (`{role:"admin"}`).
    - **Pending admin invitee:** consume the old live codes, reissue, set `Created=false`, and audit `admin.invitation_reissued`.
    - **Pending driver:** take over the row. Role becomes `admin`, the password and the driver legal versions are cleared, names and locale come from the invite, and driver codes are consumed. Audit `admin.created` with `{role:"admin", replaced_pending_driver:true}`.
    - **Active or suspended account:** return `Conflict(user.email_taken)`.
    - Every branch except the conflict issues an `admin_invite` code (48 h) and enqueues `admin_invite` in the invitee's locale.
  - `(*Service).AcceptInvitation(ctx context.Context, email string, code, password secret.String) error` accepts only `role='admin' and status='pending_verification'` rows. It sets the hash, `active` and `email_verified_at`, then audits `admin.invitation_accepted`.
  - `(*users.Service).CreateAdmin(ctx context.Context, p authz.Principal, in CreateAdminInput) (AdminUser, bool, error)` and `(*users.Service).ListAdmins(ctx context.Context, p authz.Principal, limit int, cursor string) ([]AdminUser, *string, error)`. Pagination is keyset on `(created_at desc, id desc)`, and the cursor is base64url JSON `{"t":"<RFC3339Nano>","id":<int>}`.
  - Routes:
    - `GET /v1/admin/admins` (`AdminOnly`, 200 list).
    - `POST /v1/admin/admins` (`AdminOnly`, 201, or 200 on reissue).
    - `POST /v1/auth/invitations/accept` (`Public`, `CodeSubmitIP`, 204).

- [ ] **Step 1: Write the failing tests**

```go
func TestSeedFirstAdmin_CreatesOnce(t *testing.T) // run twice → 1 admin; password_hash byte-equal after 2nd run; audit "admin.seeded" once; status active, email_verified_at set, legal versions null
func TestSeedFirstAdmin_SkipsWhenAnyAdminExists(t *testing.T)
func TestLoadSeedAdmin_UnsetSkips_ProductionRefusesDevPassword(t *testing.T)
func TestCmdSeed_PasswordNeverLogged(t *testing.T) // captured stdout lacks DevSeedAdminPassword
func TestCreateAdmin_NewInvite(t *testing.T) // 201 data.status "pending_verification"; outbox "admin_invite" code expires now+48h; audit "admin.created" actor admin
func TestCreateAdmin_ReissueForPendingInvitee(t *testing.T) // 200 same id; old code consumed, new live; audit "admin.invitation_reissued"
func TestCreateAdmin_TakesOverPendingDriver(t *testing.T) // OC-39: role "admin", password_hash null, driver verify code consumed, names from invite; audit after.replaced_pending_driver == true; the driver's old password no longer logs in
func TestCreateAdmin_ActiveOrSuspended409(t *testing.T) // user.email_taken
func TestAcceptInvitation_ActivatesAdmin(t *testing.T) // 204; login → 200 user.role "admin"; GET /v1/admin/admins → 200
func TestAcceptInvitation_RejectsDriverRows(t *testing.T) // pending driver with a verify code → 422 auth.code_invalid
func TestInviteeEscalationClosed(t *testing.T) // OC-38: invite x@y → register x@y (any password) → verify with any code → resend → login with the registered password: never an active admin; user remains role admin, pending, password_hash null; register response identical; no driver codes or emails issued
func TestListAdmins_KeysetPagination(t *testing.T) // 5 admins, limit 2 → 3 pages, no duplicates, last next_cursor null; meta.page.limit 2
func TestListAdmins_BadCursorAndLimit(t *testing.T) // cursor "%%%" or valid base64 of "{}" → 422 field "cursor"; limit 0, 101, "abc" → 422 field "limit"   (Review Focus 4)
func TestAdminRoutes_DriverGets403(t *testing.T)    // 403 auth.forbidden
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/users/... ./internal/features/auth/... ./cmd/seed/...` → FAIL.
- [ ] **Step 3: Implement**, then wire `cmd/seed` (lot → legal → admin in one tx) and `cmd/api`.
- [ ] **Step 4: Run** — `$IT go test -race ./...` → `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server deploy/compose/docker-compose.yml
git commit -m "feat(server): add first-admin seed, admin invitations and admin listing"
```

---

## Task A22: Rate limits — Postgres limiter, wiring, purge

**Files:**
- Create: `migrations/00006_rate_limits.sql`, `queries/ratelimit.sql`, `internal/infrastructure/ratelimit/postgres.go` + test, `internal/transport/http/middleware/ratelimit.go` + test
- Modify: `dbgen/*`, `cmd/api/main.go` (real limiter replaces `Noop`), `cmd/worker/main.go` (purge job)
- Docs: `docs/policies.md` §2.7 (OC-23 limits), `docs/architecture.md` §17 ADR "Postgres limiter"

**Interfaces:**
- Consumes: `ratelimit.Rule`/`Limiter` (A7), `clock.Clock`, `middleware.ClientIPFrom` (A4), `authz.PrincipalFrom`.
- Produces:
  - `ratelimit.NewPostgres(pool *pgxpool.Pool, c clock.Clock) *Postgres` implements `Limiter`. The key stored is `sha256(rule.Prefix + key)`. The upsert is spec §10.2 with `window_start = date_bin(@window, @now, 'epoch')`, and `RetryAfter = ceil(expires_at − now)`.
  - `ratelimit.Purge(ctx context.Context, db database.DBTX, now time.Time) (int64, error)`. Worker job `rate_limit_purge` runs every 1 min.
  - `middleware.RateLimit(l ratelimit.Limiter, log *slog.Logger) func(ratelimit.Rule) func(http.Handler) http.Handler`. The key is the client IP (`KeyIP`) or `principal.UserPublicID` (`KeyUser`). Denied requests get 429 through `httpx.WriteError`. Limiter errors return 500 `internal.unexpected` when `!rule.FailOpen`, and are logged and allowed when `FailOpen`. Throttling is logged at warn level with the rule name and the hex key hash.

- [ ] **Step 1: Write the failing tests**

```go
func TestPostgres_SixthLoginIn429(t *testing.T) // LoginIPEmail: 5 allowed, 6th Allowed=false RetryAfter in (0,60s]
func TestPostgres_WindowRollsOverWithFakeClock(t *testing.T) // Advance to next window → allowed again
func TestPostgres_TwoInstancesShareCount(t *testing.T) // two NewPostgres on two pools (same DB) alternate → 6th denied
func TestPostgres_KeysAreHashed(t *testing.T) // select key from rate_limit_counters → 32 bytes, does not contain "ana@example.com"
func TestRateLimit_FailClosedOnAuth(t *testing.T) // erroring limiter + CodeSubmitIP → 500 internal.unexpected
func TestRateLimit_FailOpenOnVehicles(t *testing.T) // erroring limiter + VehicleWriteUser → handler runs
func TestRateLimit_429Envelope(t *testing.T) // Retry-After header == details.retry_after_seconds
func TestLoginEmailRule_SharesBucketAcrossCaseVariants(t *testing.T) // via newHandler: 5 logins as "Ana@X.do", " ana@x.do " … → 6th variant 429   (Review Focus 2)
func TestLogin_SixthAttempt429ViaHandler(t *testing.T) // spec §15.2, real limiter + real handler, Retry-After set
func TestPurge_RemovesExpired(t *testing.T)
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/infrastructure/ratelimit/... ./internal/transport/http/middleware/... ./cmd/...` → FAIL.
- [ ] **Step 3: Migration (spec §5.9, `create unlogged table`) + queries + `sqlc generate`; implement and wire.**
- [ ] **Step 4: Run** — `$IT go test -race ./...` → `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server docs
git commit -m "feat(server): add postgres fixed-window rate limiter across instances"
```

---

## Task A23: Hardening — authz matrix, envelope registry test, log-leak, acceptance, coverage gate

**Files:**
- Create: `apps/server/cmd/api/{authz_matrix_test.go,envelope_test.go,logleak_test.go,acceptance_test.go,openapi_test.go,fixtures_test.go}`, `apps/server/scripts/covergate.sh`
- Modify: `.github/workflows/ci-server.yml` (coverage gate step)

**Interfaces:**
- Consumes:
  - `newHandler(deps)` and `router.Routes()`, from the router built inside `newHandler`. Expose it in tests with an unexported `newHandlerWithRoutes(d deps) (http.Handler, []router.Route)`.
  - Every feature, `testsupport.NewTestDB`, `testsupport.Mailpit` and `FakeClock`.
- Produces (test-only):
  - `type matrixEntry struct{ build func(f *fixture) *http.Request; owned bool }` and `var matrix map[string]matrixEntry`, keyed by route pattern.
  - `fixture` holds the principals: anonymous, driver owner, other driver, admin, legal-pending driver, suspended user, revoked session. It also holds an owned vehicle id and token variants (tampered, expired, `alg=none`).
  - `scripts/covergate.sh <coverprofile> <min>` sums statements over the domain files and exits 1 below the minimum, printing the per-file percentages. The domain files are `features/auth/{email,password,code,refresh,session}.go`, `features/vehicles/{plate,limit}.go` and `infrastructure/hashing/*.go`, excluding `_test`.

- [ ] **Step 1: Write the tests**

```go
func TestAuthzMatrix(t *testing.T) // spec §15.5; fails with "route %s has no matrix entry" for any router.Routes() pattern missing from matrix; per principal row asserts status + error.code; other-driver 404 body equals truly-missing 404 body minus meta.request_id
func TestEveryRouteReturnsEnvelope(t *testing.T) // for every route × {anonymous, owner}: non-204 bodies decode to exactly one of data/error, meta.request_id == X-Request-Id; 204 bodies empty; security headers present
func TestOpenAPI_CoversRegisteredRoutes(t *testing.T) // parse api/openapi.yaml (gopkg.in/yaml.v3): set of "METHOD path" equals router.Routes() patterns plus /healthz, /readyz
func TestNoSecretsInLogs(t *testing.T) // spec §15.4: register→verify→login→refresh→reuse→reset→delete via handler with captured JSON slog; output, every audit_log row (as JSON) and every error body contain none of: password, new password, each code, access + refresh tokens, email, email local part, first_name, last_name, plate, JWT_SIGNING_KEY, AUTH_CODE_PEPPER
func TestM1BackendAcceptance(t *testing.T) // spec §15.6: seed → register (mobile) → code from Mailpit → verify → login mobile → POST vehicle → login web (same account) → GET /v1/vehicles shows the same plate → refresh rotates → reuse → 401 auth.session_revoked → seeded admin login → GET /v1/admin/admins 200 → driver GET /v1/admin/admins 403 auth.forbidden; every error code seen is in errcodes.All()
```

- [ ] **Step 2: Run** — `$IT TEST_SMTP_ADDR=localhost:51025 TEST_MAILPIT_API=http://localhost:58025 go test -race ./cmd/api/...`. Expected: PASS. If the matrix finds a route with a wrong policy, fix the policy in the owning feature's `Register`, not the matrix.
- [ ] **Step 3: Coverage gate** — write `covergate.sh` and add this CI step after Test (same env):
  - `go test -coverprofile=domain.cover ./internal/features/auth/ ./internal/features/vehicles/ ./internal/infrastructure/hashing/`
  - `sh scripts/covergate.sh domain.cover 90`

  Locally: same commands → `domain statement coverage: ≥90.0% OK`.
- [ ] **Step 4: Commit**

```bash
git add apps/server/cmd/api apps/server/scripts .github/workflows/ci-server.yml
git commit -m "test(server): add authz matrix, envelope, log-leak and m1 acceptance tests with coverage gate"
```

---

## Task A24: Docs sync and M1 backend exit verification

**Files:**
- Modify: `docs/architecture.md` §4.4 (resend, invitation and admin admins routes, legal gate, envelope) and any §4.2/§5/§11.1/§17 items not yet done by A4/A11/A15/A22; `docs/spec.md` §10.2 (append the accepted OC-1…OC-39 and this plan's resolved gaps); `deploy/compose/docker-compose.yml` (verify every new default: JWT, pepper, trusted proxies, SMTP, seed admin, max vehicles, and the header with no `.env`)

**Interfaces:** Consumes everything. Produces the exit evidence.

- [ ] **Step 1: Update the docs** listed above (spec §16.2).
- [ ] **Step 2: Full local run**:
  - `cd apps/server && $IT TEST_SMTP_ADDR=localhost:51025 TEST_MAILPIT_API=http://localhost:58025 go test -race ./...`
  - `go vet ./...`
  - `go tool sqlc diff -f internal/infrastructure/database/sqlc.yaml`
  - `go generate ./internal/domain/errcodes/... && git diff --exit-code ../../packages/contracts/src`

  Expected: all `ok`, empty diffs.
- [ ] **Step 3: Compose smoke**:
  - `docker compose -f deploy/compose/docker-compose.yml down -v && docker compose -f deploy/compose/docker-compose.yml up -d --build`. Expected: all services healthy.
  - `curl -s -X POST localhost:8080/v1/auth/login -H 'Content-Type: application/json' -d '{"email":"admin@mypark.local","password":"<DevSeedAdminPassword>","client_kind":"web"}'`. Expected: `data.user.role` is `"admin"`.
  - Register a driver with curl. Expected: the verification email is visible at `localhost:8025`.
- [ ] **Step 4: Tick the spec §16.4 table** in the PR description, citing `TestM1BackendAcceptance`, `TestRefresh_ReuseRevokesFamily`, `TestRefresh_ConcurrentSameToken`, `TestRefresh_ReuseRevocationSurvivesErrorResponse`, the vehicle integration tests and `TestAuthzMatrix`.
- [ ] **Step 5: Commit**

```bash
git add docs deploy/compose/docker-compose.yml
git commit -m "docs: sync architecture, policies and spec with m1 backend"
```

---

## Task dependencies

| Task | Spec slice | Depends on |
|---|---|---|
| A1 Envelope | §6.0 | — |
| A2 Contract (handoff) | S1 | A1 |
| A3 Test roles, F5 | S0 (F9, F6, F5) | — |
| A4 HTTP hardening | S0 (F2, F3, F7) | A1 |
| A5 CI supply chain | S0 (F8) | — |
| A6 Primitives | S2 | A1, A2 |
| A7 Router | S2 | A4, A6 |
| A8 Hashing | S2 | — |
| A9 JWT + config | S2 (+F1) | A6 |
| A10 TxRunner | S2 | A3 |
| A11 Identity + legal | S3 | A7, A10 |
| A12 Audit + outbox | S4 | A11 |
| A13 Register/verify/resend | S5 | A8, A9, A12 |
| A14 Login | S5 | A13 |
| A15 Authn + logout | S6 | A14 |
| A16 Refresh | S6 | A15 |
| A17 Forgot/reset | S7 | A16 |
| A18 Worker delivery | S8 | A12 |
| A19 Vehicles | S10 | A15 |
| A20 Me | S9 | A17, A19 |
| A21 Admin | S11 | A18, A20 |
| A22 Rate limits | S12 | A13 (parallel with A16–A21) |
| A23 Hardening | S13 | all |
| A24 Docs + exit | S13 | A23 |

## Self-review notes

- **Spec coverage:**

  | Spec section | Tasks |
  |---|---|
  | §6.0 | A1 |
  | §6.1–§6.3 | A2 (+ each feature task) |
  | §5.1 / F5 | A3 |
  | §5.2–§5.5 | A11 |
  | §5.6–§5.7 | A12 |
  | §5.8 | A19 |
  | §5.9 | A22 |
  | §7.1 | A8 |
  | §7.2 | A9 |
  | §7.3 | A15 |
  | §7.4 | A14, A16 |
  | §7.5 | A13 |
  | §7.6 | A13, A17, A21 |
  | §7.7 | A13, A14, A17 |
  | §7.8 | A20 |
  | §7.9 | A6, A23 |
  | §8.1–§8.2 | A7, A15, A23 |
  | §8.3 | A19 |
  | §8.4 | A13, A20 |
  | §8.5 | A15, A20 |
  | §9 | A19 |
  | §10 | A7, A22 |
  | §11 | A12, A18 |
  | §12 | A12 + owning tasks |
  | §13 | A21 |
  | F1 | A9 |
  | F2, F3, F7 | A4 |
  | F4 | A1 |
  | F6, F9 | A3 |
  | F8 | A5 |
  | §15 | in every task, plus A23 |
  | §16.2 | A4, A11, A15, A22, A24 |
  | §16.4 | A23, A24 |

- **Resolved spec gaps and contradictions:**
  1. **Seed admin name.** `SEED_ADMIN_NAME` became `SEED_ADMIN_FIRST_NAME` + `SEED_ADMIN_LAST_NAME` (user decision 7).
  2. **SQL time.** SQL `now()` became `@now` from `clock.Clock`, so the fake-clock tests are possible.
  3. **User-keyed rate limits.** They run after Authn, since the principal is needed.
  4. **apperr kinds.** `BadRequest`, `TooLarge` and `UnsupportedMedia` were added for OC-28.
  5. **Audit action.** `admin.invitation_reissued` and `replaced_pending_driver` were added to the §12 catalog. The forgot flow audits `auth.password_reset_requested`.
  6. **Refresh for an inactive user.** There is no `revoked_reason` for it. Deleted users use `account_deleted`, and other statuses get a 401 with no write.
  7. **Deletion ports.** Deletion needs `vehicles.EraseOwner` and `Notifier.ScrubPendingForUser`, which are not in the §4.1 interface table. Both were added, and Vehicles is built before Me.
  8. **Lists.** §6.1 `{items: …}` is superseded by §6.0: `data` is the array.
  9. **Recover headers.** Recover re-applies the exported `SecurityHeaders`.
  10. **UUID library.** `github.com/google/uuid` was chosen.
  11. **Logout idempotency.** Spec §6.1 says "204 (idempotent)", but OC-4 checks the session on every request, so a second logout with the same, now revoked, token returns 401 `auth.session_revoked`. Track B's BFF must treat a 401 on logout as success.
- **Type consistency:**
  - `authz.Principal`, `apperr.*`, `ratelimit.Rule` and the `router.Policy` / `Middlewares` names are used unchanged from A6/A7 through A23.
  - `auth.UserView` (A14) backs `users` (A20/A21).
  - `auth.Accounts` gains `CreateInvitedAdmin` in A21 without changing the A20 methods.
  - `testsupport.NewTestDB` (A3) is used by every integration task.
- **Size exceptions:**
  - A2 (`openapi.yaml`) is one coherent contract.
  - A1 touches four workspaces, but most of its changed lines are test fixtures.
  - The feature tasks A13 and A21 may each need two chained PRs: queries plus the service first, then HTTP plus tests.
