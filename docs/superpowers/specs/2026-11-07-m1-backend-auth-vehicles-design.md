# M1 Track A — Backend Auth + Vehicles: Design

**Status:** approved by the user on 2026-11-07, after the builder check (OC-38/39 come from it) · **Date:** 2026-11-07 · **Milestone:** M1 (Nov 16 – Nov 20) · **Track:** A (Go backend only)

This document designs the Go side of M1: accounts, sessions, roles and authorization, vehicles, the email outbox, the audit log, rate limits, the first-admin seed, and the M0 follow-ups. **Authentication, authorization and roles are the center of gravity.** Every other part is shaped so it cannot weaken them.

> **How to read this.** §1–§3 explain *why* and what is fixed. §4–§13 are the design. §14 covers the M0 follow-ups. §15–§17 cover testing, build order and what is left to the builder. Decisions the user made are labeled **(user)**. Decisions this document makes are labeled **(our call)** and collected in §2.2 for review.
>
> **Sources:** `docs/spec.md` (§2, §3.1, §3.2, §3.11, §4, §8 M1, §10), `docs/architecture.md` (§4, §5, §10, §11, §14, §16, §17), `docs/policies.md` (§1.8, §2.3–§2.7), the M0 plan `docs/superpowers/plans/2026-11-06-m0-foundations.md`, and the code under `apps/server`.

---

## 1. Intent

### 1.1 Why

M1 is the first milestone with users. Everything later depends on its identity model: reservations (M3), payments, admin tools (M4) and the device API (M2) all check who the caller is and what they may touch. Mistakes here compound, and they are hard to retrofit. So M1 builds a small, strict and heavily tested core:

- **Authentication.** Accounts, verification, passwords, sessions and tokens.
- **Authorization.** Roles checked per route, plus ownership checked in every use case. Reservations will reuse both in M3.
- **First owned resource.** Vehicles, which also prove the ownership model.

### 1.2 Goals

| # | Goal | Verified by |
|---|---|---|
| G1 | A driver can register, verify by emailed code, log in, refresh and log out on both client kinds (web through the BFF, mobile) | End-to-end backend acceptance test (§15.6) |
| G2 | Refresh tokens rotate on every use. Reusing a rotated token revokes the whole family and is audited | `TestRefresh_ReuseRevokesFamily` and a concurrency test |
| G3 | Authorization is deny-by-default. Every route declares a policy. A driver gets 403 on `/v1/admin/*`. Another driver's resource answers 404 | Authorization matrix test generated from the route registry |
| G4 | Vehicle CRUD with plate normalization, system-wide plate uniqueness among active vehicles, and the 5-vehicle limit | Unit and integration tests, including races |
| G5 | Emails go through a transactional outbox, are delivered to Mailpit by the worker, and are rendered in es-DO or en | Mailpit integration test |
| G6 | Security events and admin actions are written to an append-only audit log in the same transaction as the action | Integration tests, plus a grant test |
| G7 | Login, registration and code requests are rate-limited across API instances, with 429 and `Retry-After` | Integration tests |
| G8 | The M1 contract (OpenAPI and error codes) is published **before** implementation, so Track B can build against it | Contract PR merged first (§16) |

### 1.3 Non-goals (M1)

- Web BFF, cookies, CSRF and screens; mobile secure storage and screens (Track B).
- Reservations, payments and the device API. M1 only builds the authorization that reservations will reuse.
- Email change, password change while logged in (use reset), MFA and social login.
- Admin user management beyond creating and listing admins. Suspend, reactivate and search drivers are M4 (FR-ADMIN-8).
- Audit log read endpoint (FR-ADMIN-10, M4). M1 only writes.
- Employee cards. They are a credential, never a role (user).
- Strict unknown-field rejection (`additionalProperties: false` on requests, zod `strictObject`). Planned before M2.

### 1.4 Anti-goals (failure even if it "works")

| Anti-goal | Guard (section) |
|---|---|
| Tokens, passwords, codes or full emails appear in logs, error messages or audit rows | Secret types, log-leak test (§7.9, §15.4) |
| Register, forgot-password or resend responses reveal whether an email has an account | Identical responses, a notice email instead of an error (§7.7) |
| Login is measurably faster for unknown emails | Dummy argon2id verify (§7.7) |
| A rotated refresh token is accepted, or its reuse goes undetected because the error path rolled back | Reuse detection that commits before it fails (§7.4, §4.5) |
| A driver can read, edit or delete another driver's vehicle, or tell from the response that it exists | Owner-scoped queries, 404 (§8.3) |
| A route ships without an authorization decision | Registration fails without a policy, and a matrix test covers every route (§8.1) |
| Logout, password reset or account deletion leaves a valid access token working for up to 15 min | Session check on every request (§7.3) |
| The app role can rewrite or delete audit history | Revoked grants, grant test (§5.6) |
| Concurrent requests push a driver past 5 vehicles, or register one plate twice | Row lock, partial unique index (§9) |
| Anyone becomes admin through self-registration or mass assignment | Role is never read from client input (§8.4) |

---

## 2. Decisions

### 2.1 Made by the user (2026-11-07)

1. M1 is split. **Track A (Go backend) is built first and fully**, then Track B (web BFF and screens, mobile secure storage and screens). This document covers Track A only.
2. **Contract first.** `apps/server/api/openapi.yaml` with every M1 endpoint and error code is published first, so `packages/contracts` can be regenerated. Track B builds against it later.
3. Top concern: authentication, authorization and roles must be solid. This document is threat-minded and test-heavy.
4. App roles are `driver` and `admin` only. An employee card holder has a credential, not a role. Anonymous means no account. Admins never self-register. The first admin is created by `cmd/seed`, and later admins by an existing admin (FR-AUTH-7).
5. Authorization has two layers: a role check in middleware (a driver calling `/v1/admin/*` gets `403 auth.forbidden`) and an ownership check in every use case.
6. Reservations are **not** in M1 (they are M3). M1 builds the role and ownership authorization they will reuse.
7. The database schema is approved. A person's name is stored as two required fields, `first_name` and `last_name`, never a single `full_name`, in `users` and everything that depends on it (API bodies, validation, anonymization, audit field names, emails). `last_name` holds both surnames (es-DO convention).
8. After reviewing the design and the schema, the user said to continue to the plan. The calls in §2.2 stand unless the user changes one.
9. **Baseline response envelope for every backend response** (§6.0), including `/healthz` and `/readyz`. Success is `{data, meta}`, errors are `{error, meta}`, and `meta.request_id` is always present. Lists put pagination in `meta.page`. The only exception is `204`, which has no body.

### 2.2 Our calls (review these)

| # | Decision | Section |
|---|---|---|
| OC-1 | Accessing another user's resource by id returns **404, not 403**, so ids can't be probed. A malformed id is also a 404 | §8.3 |
| OC-2 | Auth, role checks and rate limits are **per-route wrappers applied at registration**, not global chain links, because `AccessLog` relies on `r.Pattern` set on the request the mux receives | §4.4 |
| OC-3 | New `auth_sessions` table: a session is a refresh-token family, and the JWT `sid` is its `public_id`. `refresh_tokens.session_id` replaces `family_id` (refines architecture §11.1) | §5.3 |
| OC-4 | **Stateful session check on every authenticated request** (one indexed query): revoked sessions, suspended or deleted users, and role changes take effect immediately, not after the 15 min token expiry | §7.3 |
| OC-5 | Strict reuse detection with **no grace window**. Clients must single-flight refresh. This is a Track B requirement for the BFF too | §7.4 |
| OC-6 | Writes that must survive a failing request (family revocation, code attempt increments, failed-login audit) commit first. The error is returned **after** the commit | §4.5 |
| OC-7 | JWT library `github.com/golang-jwt/jwt/v5`, pinned to `EdDSA` with issuer, audience and `exp` required, instead of hand-rolled JWT code | §7.2 |
| OC-8 | Dev JWT key: an inline compose default (a dev-only Ed25519 seed, `kid=dev-1`), same pattern as the dev DB passwords. Config **refuses the known dev key when `APP_ENV=production`**. Rotation uses `kid` and a list of extra verify keys | §7.2 |
| OC-9 | Verification, reset and invite codes are stored as **HMAC-SHA256 with a server pepper** (`AUTH_CODE_PEPPER`), not bare SHA-256, because 10^6 codes reverse instantly offline | §7.5 |
| OC-10 | The outbox payload holds the plaintext code until delivery, then the code is **scrubbed** (on `sent` or `dead`). Codes expire in 15 min, so no payload encryption in M1 | §11.3 |
| OC-11 | All code failures (wrong, expired, consumed, too many attempts, unknown email) return one code, `auth.code_invalid`, so the endpoint can't confirm that an account exists | §7.5 |
| OC-12 | Register with an existing email returns the same `202`. An `active` owner receives an "account already exists" email. A `pending_verification` account is **overwritten** by the new submission (new password, new code), which limits email squatting | §7.6 |
| OC-13 | New endpoint `POST /v1/auth/verify/resend` (missing from architecture §4.4). Verify returns `204`, and the client then logs in | §6 |
| OC-14 | argon2id runs behind a **semaphore of 4 concurrent hashes** (64 MiB each), so a login burst can't exhaust memory | §7.1 |
| OC-15 | Legal re-acceptance: login succeeds and returns `legal_acceptance_required`. Until the user accepts, the middleware returns `403 legal.acceptance_required` on every driver route except an allow-list. **Admins are exempt** | §8.5 |
| OC-16 | Vehicles are **driver-only**. An admin calling `/v1/vehicles` gets `403 auth.forbidden`. `GET/PATCH /v1/me` serve both roles. `DELETE /v1/me` is driver-only | §8.2 |
| OC-17 | `DELETE /v1/me` requires the current password. A wrong password returns `403 auth.password_incorrect`, not a 401, so clients don't loop on refresh | §6 |
| OC-18 | Account deletion anonymizes **immediately**, in the same transaction (meets the "within 30 days" rule), soft-deletes the vehicles and revokes all sessions | §7.8 |
| OC-19 | Admin invitations: an admin creates an admin (`pending_verification`, no password). The invitee gets a 6-digit `admin_invite` code (**48 h** TTL, 5 attempts) and sets a password with `POST /v1/auth/invitations/accept`. No promotion of existing drivers in M1 | §13.2 |
| OC-20 | First admin: `cmd/seed` reads `SEED_ADMIN_EMAIL`, `SEED_ADMIN_PASSWORD` and `SEED_ADMIN_NAME`, with inline compose dev defaults. It is **insert-if-no-admin-exists**, never overwrites, and refuses the dev default password in production | §13.1 |
| OC-21 | Rate-limit storage is a Postgres **UNLOGGED** fixed-window counter table with hashed keys, behind a `Limiter` port. It is correct across instances. M2 may add an in-memory adapter for the device API | §10 |
| OC-22 | Client IP comes from `X-Forwarded-For` **only** when the TCP peer is in `TRUSTED_PROXY_CIDRS`. The web BFF must forward the browser IP (Track B contract item) | §10.3 |
| OC-23 | Extra limits beyond policies §2.7: login 20/hour per email (any IP), code emails 5/hour per email, refresh 60/min per IP, vehicle writes 20/min per user | §10.1 |
| OC-24 | The outbox is **at-least-once** with a lease (`sending` plus `available_at`). The SMTP `Message-ID` is derived from the dedupe key, so a rare duplicate is recognizable | §11.3 |
| OC-25 | Transactions are carried in `context` by a `TxRunner`. The outbox `Enqueue` and audit `Record` adapters **fail if no transaction is present**, so they can't run outside the action's transaction | §4.5 |
| OC-26 | Audit rows store ids, action and changed field names. They never store PII values or secrets. `occurred_at` replaces the column name `at` | §12 |
| OC-27 | New `internal/domain/apperr` (error kind → HTTP status in transport) and `internal/domain/authz` (`Principal`, `Role`) packages | §4.1 |
| OC-28 | New HTTP error codes: `http.invalid_json` 400, `http.payload_too_large` 413, `http.unsupported_media_type` 415. Request bodies are capped at 64 KiB | §6.3 |
| OC-29 | `vehicle.in_use` is defined now. FR-VEH-4 is checked through a `ReservationChecker` port whose M1 adapter always returns "no active reservation". M3 replaces it | §9 |
| OC-30 | Plate rule after normalization: `^[A-Z0-9]{2,10}$`. Make, model and color are free text | §9 |
| OC-31 | Sessions: the refresh token TTL is 30 days, sliding on rotation. A session also has an **absolute cap of 90 days** | §7.4 |
| OC-32 | Security headers on every API response: `X-Content-Type-Options: nosniff`, `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'` | §4.4 |
| OC-33 | Replace the mutable `response.RequestIDFunc` with a leaf context package shared by middleware and response | §14 |
| OC-34 | Mailpit runs as a CI service, and the SMTP integration test is required when `CI=true` (same pattern as `TEST_DATABASE_URL`) | §15.6 |
| OC-35 | The "≥ 90% branch coverage" rule is measured by **statement coverage** (Go has no branch coverage) on the auth and vehicles domain files, with a CI gate | §15.7 |
| OC-36 | A password reset also enqueues a "your password was changed" email | §7.6 |
| OC-37 | Unverified-account purge and a per-user session cap are **deferred** (noted risk) | §16.3 |
| OC-38 | The public pending-account flows (register overwrite, resend, verify) apply **only to `role='driver'`**. A pending admin invitee is treated like an existing account (no overwrite, no email, identical response), and verify/resend for an invitee behave like an unknown email. An invitee can become active **only** through `invitations/accept`, which accepts only `role='admin'` rows in `pending_verification`. This closes the "register over an invite → active admin" escalation path | §7.6, §13.2 |
| OC-39 | Re-inviting: `POST /v1/admin/admins` for an email that is a **pending admin invitee** reissues a fresh `admin_invite` code (consuming the old live codes) and returns `200` with the existing `AdminUser`, audit `admin.invitation_reissued`. For an email held by a **pending (never verified) driver**, the invite takes over the row: role → `admin`, password cleared, driver codes consumed, audit `admin.created` with `replaced_pending_driver=true`. Unverified accounts never proved ownership, so they cannot block an invite. An **active or suspended** account → `409 user.email_taken` | §13.2 |

---

## 3. Constraints

| Area | Constraint (source) |
|---|---|
| Language and runtime | Go 1.27, module `github.com/XYZLZ/MyPark/apps/server` |
| HTTP | stdlib `net/http` `ServeMux` method+path patterns. No third-party router (architecture §4.2) |
| Data | `pgx/v5` pool, `sqlc` (queries in `internal/infrastructure/database/queries`, output in `dbgen`, committed), `goose` **forward-only** migrations, Postgres 18 (`uuidv7()`, `citext`) |
| Validation | `go-playground/validator/v10` through `internal/infrastructure/validation` (JSON field paths, `422 validation.failed` with `details.fields[{field,rule,param}]`) |
| Response shape | **Every** response body uses the baseline envelope in §6.0 (user decision, §2.1 item 9). Errors are `{"error":{"code","message","details"},"meta":{"request_id"}}`. `request_id` moves from `error` to `meta`, a change to the M0 shape. New codes go into `internal/domain/errcodes/errcodes.go` and are exported to `packages/contracts/src/error-codes.ts` by `go generate`. CI fails on drift |
| Config | `internal/config` accessors over unexported fields. Validated at boot. **No `.env` files.** Compose uses inline `${VAR:-dev-default}` |
| DB roles | `mypark_migrator` (DDL, runs goose) and `mypark_app` (DML only, used by api, worker and seed). Created by `deploy/compose/postgres/init/01-roles.sh`. Default privileges grant `mypark_app` full DML on every new table |
| Email | Mailpit (`axllent/mailpit:v1.33.0`, SMTP 1025, UI/API 8025) in dev |
| Security baseline | policies §2.7 (ASVS L1): argon2id, password ≥ 10 characters checked against a common-password list, 15-min access tokens, rotating refresh tokens, listed rate limits, CORS allow-list, deny-by-default authorization |
| Privacy | policies §1.8: soft delete plus anonymization, retention (tokens and codes purged 30 days after expiry, outbox 90 days, audit 2 years) |
| Process | PRs ≤ 400 changed lines (chained), TDD for domain rules, integration tests on real Postgres, conventional commits with no AI attribution |

---

## 4. Architecture

### 4.1 Package map

New packages are **bold**. Paths are under `apps/server/internal/`.

| Package | Role | Owns tables |
|---|---|---|
| `domain/errcodes` | Error code catalog (extended, §6.3) | — |
| **`domain/apperr`** | `Error{Code, Kind, Details, Err}`. Kinds: `Invalid`, `Unauthenticated`, `Forbidden`, `NotFound`, `Conflict`, `RateLimited`, `Internal`. No `net/http` import | — |
| **`domain/authz`** | `Role` (`driver`, `admin`), `Principal{UserID int64, UserPublicID uuid, Role, SessionID int64, SessionPublicID uuid, LegalPending bool}`, `WithPrincipal` and `PrincipalFrom` context helpers | — |
| **`domain/secret`** | `secret.String`: `String()`, `GoString()`, `LogValue()` and `MarshalJSON` all return `"[redacted]"`. Used for passwords, codes and tokens in DTOs | — |
| **`features/auth`** | Register, verify, resend, login, refresh, logout, logout-all, forgot, reset, invitation accept. `Authenticator` (token → `Principal`). Account credentials and lifecycle | `users` (credential and status columns), `auth_codes`, `auth_sessions`, `refresh_tokens` |
| **`features/users`** | `GET/PATCH/DELETE /v1/me`, legal re-acceptance, admin create and list (calls `auth.Accounts` to issue the invite) | `users` (profile columns) |
| **`features/legal`** | Current terms and privacy versions, acceptance validation | `legal_documents` |
| **`features/vehicles`** | Vehicle CRUD, plate normalization, limit | `vehicles` |
| **`features/audit`** | `Recorder` (M1). The read side is M4 | `audit_log` |
| **`features/notifications`** | Outbox `Enqueue`, `Dispatcher` (worker), email templates (embedded) | `outbox_messages` |
| `features/lots` | Existing (seed) | `lots`, `spaces`, `lot_occupancy` |
| **`infrastructure/hashing`** | argon2id (PHC string, rehash check, semaphore), HMAC code hashing, common-password list (embedded) | — |
| **`infrastructure/jwt`** | Ed25519 signer and verifier with `kid` | — |
| **`infrastructure/ratelimit`** | `Limiter` port, Postgres adapter | `rate_limit_counters` |
| **`infrastructure/email`** | SMTP sender (`net/smtp`, no TLS in dev, STARTTLS when configured) | — |
| **`infrastructure/clock`** | `Clock` (`Now()`). Real implementation here, fake in `testsupport` | — |
| **`infrastructure/jobs`** | A small ticker runner for worker jobs: named, panic-safe, context-bound | — |
| `infrastructure/database` | Adds **`TxRunner`** and `Conn(ctx)` (§4.5). New query files | — |
| **`transport/http/router`** | `Router` wraps `*http.ServeMux` and **requires a `Policy` per route** (§8.1). Also holds the route registry used by tests | — |
| **`transport/http/authn`** | Bearer middleware (per route) built on `auth.Authenticator`, role gate, legal gate | — |
| **`transport/http/httpx`** | `DecodeJSON` (64 KiB cap, content type, `validation`), `WriteError(apperr)`, kind → status | — |
| `transport/http/middleware` | Adds **`CORS`**, **`SecureHeaders`**, **`ClientIP`**, **`RateLimit`** (per route). Fixes from §14 | — |
| **`transport/http/reqctx`** | Leaf package: request ID and the mutable `LogFields` holder (§4.4). Replaces `response.RequestIDFunc` | — |

Each feature follows architecture §4.1: domain files (`*.go`, no `pgx` or `net/http`), `service.go` (use cases), `ports.go`, `http.go` (handlers), and a `postgres/` adapter over `dbgen`. Features call each other only through **exported use-case interfaces**:

| Caller | Callee interface | Use |
|---|---|---|
| `users` | `auth.Accounts` | `CreateInvitedAdmin`, `RevokeAllSessions`, `VerifyPassword` |
| `auth`, `users` | `legal.Current` | Current versions, `ValidateAcceptance(terms, privacy)` |
| `auth`, `users`, `vehicles` | `audit.Recorder` | `Record(ctx, Entry)` inside the caller's transaction |
| `auth`, `users` | `notifications.Notifier` | `Enqueue(ctx, Email)` inside the caller's transaction |
| `vehicles` | `vehicles.ReservationChecker` (port) | M1 adapter: always false. M3: reservations |

> **Ownership of `users` (our call).** `auth` owns identity and credential columns (`email`, `password_hash`, `role`, `status`, `email_verified_at`, `password_changed_at`, `deleted_at`). `users` owns profile columns (`first_name`, `last_name`, `phone`, `locale`, legal acceptance). Each feature has its own sqlc queries for its own columns. Deletion lives in `users`. It anonymizes the profile, then calls `auth.Accounts` to revoke sessions, inside one transaction.

### 4.2 Errors from use case to wire

Use cases return `*apperr.Error`, or a plain error for unexpected failures. `httpx.WriteError` maps the kind to a status: Invalid 422, Unauthenticated 401, Forbidden 403, NotFound 404, Conflict 409, RateLimited 429 (and sets `Retry-After` from `details.retry_after_seconds`). Anything else becomes `500 internal.unexpected`, with the cause logged and never sent. Handlers never pick status codes by hand.

### 4.3 Composition

| Binary | Adds in M1 |
|---|---|
| `cmd/api` | Config (JWT keys, pepper, trusted proxies, limits). Clock, hasher, JWT, TxRunner, limiter. Repositories and services for auth, users, legal, vehicles, audit and notifications (enqueue only). `router.New` with all routes and policies. A pure `newHandler(deps)` so integration tests build the exact production handler |
| `cmd/worker` | A worker config loader (SMTP and DB). The jobs in §10.3. Pool size 4 stays enough |
| `cmd/seed` | After the lot seed: `legal_documents` initial versions, then the first admin (§13.1). One transaction |

### 4.4 HTTP pipeline

The existing chain is `Recover → RequestID → AccessLog → JSONErrors(mux)`. `AccessLog` needs `r.Pattern`, which the mux sets **on the request it receives**. Any global middleware that calls `r.WithContext` between `AccessLog` and the mux would make every route log as `unmatched`. So:

```
Recover → RequestID → AccessLog → SecureHeaders → CORS → JSONErrors(mux)
                                                          │
                       per route (router.Handle):  ClientIP → RateLimit? → Authn? → RoleGate? → LegalGate? → handler
```

- **Global** (they don't clone the request): `SecureHeaders` and `CORS`. CORS answers `OPTIONS` preflights itself (204) **before** `JSONErrors`. Otherwise a POST-only route would answer a preflight with 405.
- **Per route** (they may clone, which is safe inside the mux): `ClientIP`, `RateLimit`, `Authn`, the role gate and the legal gate. `router.Handle(pattern, policy, h)` builds this chain from the policy. This **changes architecture §4.2**, which lists rate limit and auth as global links. Update the doc in the same PR (our call, OC-2).
- **Logging the user:** `RequestID` stores a pointer `*reqctx.LogFields` in the context. `Authn` fills `UserPublicID` and `AccessLog` reads it after `next` returns. The access log gains `user_id` (architecture §14) without cloning.
- **CORS (follow-up):** allow-list from `CORS_ALLOWED_ORIGINS`, exact match. Methods `GET, POST, PATCH, DELETE`. Headers `Authorization, Content-Type, X-Request-Id`. Exposes `X-Request-Id, Retry-After`. `Max-Age: 600`. `Vary: Origin`. **Never** `Allow-Credentials`, because the API is bearer-only. A disallowed origin gets no CORS headers.

### 4.5 Transactions (our call, OC-6, OC-25)

`database.TxRunner.InTx(ctx, fn func(ctx context.Context) error) error` begins a `pgx.Tx`, stores it in a context key, and commits when `fn` returns nil. Adapters call `database.Conn(ctx, pool)`, which returns the transaction when one is present and the pool otherwise. `notifications.Enqueue` and `audit.Record` call `database.MustTx(ctx)` and return an error without a transaction, so a transactional enqueue can't silently degrade.

**Commit first, then fail.** Some failing requests must persist writes: family revocation on reuse, the code attempt increment, and failed-login audit. If those use cases returned an error from inside `fn`, the runner would roll back and the evidence would be lost. Pattern:

```go
var outcome refreshOutcome
err := s.tx.InTx(ctx, func(ctx context.Context) error {
    outcome, err = s.rotate(ctx, hash) // returns outcomeReuse after revoking + auditing; err only for infra failures
    return err
})
if err != nil { return TokenPair{}, err }
switch outcome.kind {
case outcomeReuse:   return TokenPair{}, apperr.Unauthenticated(errcodes.AuthSessionRevoked)
case outcomeInvalid: return TokenPair{}, apperr.Unauthenticated(errcodes.AuthRefreshInvalid)
}
```

Named test: `TestRefresh_ReuseRevocationSurvivesErrorResponse` checks the row state **after** the 401.

---

## 5. Data model

The migrations run as `mypark_migrator`, are forward-only, and follow architecture §11 conventions: `bigint generated always as identity` PK, `public_id uuid not null default uuidv7() unique` on exposed rows, `text` + `check` for enums, `timestamptz`, every FK indexed, partial indexes for hot subsets. Suggested files (one per slice; the builder may merge): `00002_db_privileges.sql`, `00003_identity.sql`, `00004_audit_outbox.sql`, `00005_vehicles.sql`, `00006_rate_limits.sql`.

### 5.1 `00002_db_privileges` (M0 follow-up)

```sql
-- +goose Up
do $$ begin
  execute format('revoke connect, temporary on database %I from public', current_database());
  execute format('grant connect on database %I to mypark_app, mypark_migrator', current_database());
end $$;
```

The grant **must** be in the same migration, or the app can't connect. Test: `mypark_app` can connect, and a fresh role without grants cannot.

### 5.2 `users`

| Column | Type | Notes |
|---|---|---|
| `id` | bigint identity PK | |
| `public_id` | uuid unique default uuidv7() | JWT `sub`, API `id` |
| `email` | citext not null | `check (length(email) <= 254)` |
| `password_hash` | text null | PHC argon2id string. Null for invited admins before acceptance and for deleted users |
| `password_changed_at` | timestamptz null | |
| `first_name` | text not null | `check (length(first_name) between 1 and 60)` |
| `last_name` | text not null | `check (length(last_name) between 1 and 80)`. Holds both surnames (es-DO convention) |
| `phone` | text null | E.164, `check (phone ~ '^\+[1-9][0-9]{6,14}$')` |
| `role` | text not null default 'driver' | `check (role in ('driver','admin'))` |
| `status` | text not null default 'pending_verification' | `check (status in ('pending_verification','active','suspended','deleted'))` |
| `locale` | text not null default 'es-DO' | `check (locale in ('es-DO','en'))` |
| `email_verified_at` | timestamptz null | |
| `terms_version`, `privacy_version` | text null | Accepted versions |
| `terms_accepted_at`, `privacy_accepted_at` | timestamptz null | `check ((terms_version is null) = (terms_accepted_at is null))`, same for privacy |
| `created_at`, `updated_at` | timestamptz not null default now() | |
| `deleted_at` | timestamptz null | `check ((status = 'deleted') = (deleted_at is not null))`. This keeps the two deletion markers from architecture §11.1 consistent |

Indexes: `unique (email) where deleted_at is null` (`users_email_active_key`) and `(role) where role = 'admin' and deleted_at is null` (for the seed's "any admin?" check and the admin list). `reservation_blocked_until` is **not** added in M1. M3 adds it.

### 5.3 `auth_sessions` and `refresh_tokens` (OC-3)

`auth_sessions` (one row per login, which is one refresh-token family):

| Column | Notes |
|---|---|
| `id` bigint PK, `public_id` uuid unique | `public_id` is the JWT `sid` |
| `user_id` bigint not null → users | idx `(user_id) where revoked_at is null`, plus the full FK index `(user_id)` |
| `client_kind` text | `check (client_kind in ('web','mobile'))` |
| `created_at`, `last_used_at` | |
| `absolute_expires_at` timestamptz not null | created + 90 days (OC-31) |
| `revoked_at` timestamptz null, `revoked_reason` text null | `check (revoked_reason in ('logout','logout_all','password_reset','reuse_detected','account_deleted'))`, `check ((revoked_at is null) = (revoked_reason is null))` |
| `created_ip` inet null, `user_agent` text null | user agent truncated to 256 characters |

`refresh_tokens`:

| Column | Notes |
|---|---|
| `id` bigint PK | |
| `session_id` bigint not null → auth_sessions | idx `(session_id)` |
| `token_hash` bytea not null | SHA-256 of the 256-bit random token (high entropy, so no pepper is needed). `unique (token_hash)` |
| `created_at`, `expires_at` (created + 30 days, capped by the session's absolute expiry) | idx `(expires_at)` for purge |
| `rotated_at` timestamptz null | Set when the token is used. A non-null value on presentation means **reuse** |

### 5.4 `auth_codes`

| Column | Notes |
|---|---|
| `id` bigint PK, `user_id` → users | FK idx `(user_id)` |
| `purpose` text | `check (purpose in ('verify_email','reset_password','admin_invite'))` |
| `code_hash` bytea not null | HMAC-SHA256(pepper, purpose ‖ user public_id ‖ code) (OC-9) |
| `expires_at` | 15 min. 48 h for `admin_invite` |
| `attempts` smallint not null default 0 | `check (attempts between 0 and 5)` |
| `consumed_at` timestamptz null | Set on success **or** when a newer code replaces it |
| `created_at` | |

`unique (user_id, purpose) where consumed_at is null` means at most one live code per purpose. Issuing a new code first consumes the old one in the same transaction. Also an index on `(expires_at)` for purge.

### 5.5 `legal_documents`

As in architecture §11.1: `id`, `kind` (`check in ('terms','privacy')`), `version` text, `published_at` timestamptz, `url_es`, `url_en`, with `unique (kind, version)` and an index on `(kind, published_at desc)`. The current version is the row with the latest `published_at <= now()` per kind. `cmd/seed` inserts version `2026-11-16` for both kinds (`on conflict do nothing`). The URLs point at web pages that Track B renders.

### 5.6 `audit_log` (append-only)

| Column | Notes |
|---|---|
| `id` bigint identity PK | |
| `occurred_at` timestamptz not null default now() | idx `(occurred_at desc)` |
| `actor_kind` text | `check (actor_kind in ('user','admin','system','anonymous'))` |
| `actor_user_id` bigint null → users | idx `(actor_user_id)` |
| `action` text not null | e.g. `auth.refresh_reuse_detected` (§12) |
| `entity_type` text null, `entity_id` text null | Public ids only. idx `(entity_type, entity_id)` |
| `before`, `after` jsonb null | Non-PII metadata only (OC-26) |
| `ip` inet null, `request_id` text null | |

```sql
revoke update, delete, truncate on audit_log from mypark_app;  -- default privileges granted them
```

Test `TestGrants_AuditLogAppendOnly`: as `mypark_app`, an `update` or `delete` on `audit_log` fails with `insufficient_privilege`. The 2-year retention purge needs a privileged path. That is deferred, see §16.3.

### 5.7 `outbox_messages`

| Column | Notes |
|---|---|
| `id` bigint PK | |
| `kind` text | `check (kind in ('email'))` |
| `template` text | `check (template in ('verify_email','password_reset','password_changed','account_exists','admin_invite'))` |
| `locale` text | `check (locale in ('es-DO','en'))` |
| `to_address` citext not null | |
| `user_id` bigint null → users | FK idx |
| `payload` jsonb not null | Template data. `code` is removed on `sent` or `dead` (OC-10) |
| `dedupe_key` text not null unique | e.g. `verify_email:<auth_code id>` |
| `status` text | `check (status in ('pending','sending','sent','dead'))` |
| `attempts` smallint default 0, `last_error` text null | `last_error` is truncated and never contains the payload |
| `available_at` timestamptz not null default now() | Claim index `(available_at) where status in ('pending','sending')` |
| `created_at`, `sent_at` | |

### 5.8 `vehicles`

| Column | Notes |
|---|---|
| `id` bigint PK, `public_id` uuid unique | |
| `user_id` bigint not null → users | full FK idx `(user_id)` |
| `plate_normalized` text not null | `check (plate_normalized ~ '^[A-Z0-9]{2,10}$')` |
| `plate_display` text not null | `check (length(plate_display) between 2 and 15)` |
| `make`, `model` text | 1–50 characters |
| `color` text | 1–30 characters |
| `created_at`, `updated_at`, `deleted_at` | |

`unique (plate_normalized) where deleted_at is null` (FR-VEH-3), and `(user_id, created_at) where deleted_at is null` for the owner's list.

### 5.9 `rate_limit_counters` (UNLOGGED, OC-21)

`create unlogged table rate_limit_counters (key bytea primary key, window_start timestamptz not null, count int not null, expires_at timestamptz not null)`. Keys are SHA-256 hashes, so no emails or IPs are stored in clear. UNLOGGED is fast and is truncated after a crash, which is acceptable for throttling. There is an index on `(expires_at)` for the purge.

---

## 6. API (contract-first)

All paths are under `/v1`. Bodies are JSON (`Content-Type: application/json` required on bodies, otherwise 415), capped at 64 KiB. Ids are UUIDv7 `public_id` values. Timestamps are RFC 3339 UTC. The OpenAPI file adds a `bearerAuth` (JWT) security scheme. Public operations declare `security: []`.

### 6.0 Baseline response envelope (user decision)

Every response body from the backend has this shape. The **Success** column in §6.1 shows only the `data` payload. The envelope wraps it.

```jsonc
// Single resource (200/201/202)
{ "data": { … }, "meta": { "request_id": "01J…" } }

// List (data is always an array; pagination lives in meta.page)
{ "data": [ … ], "meta": { "request_id": "01J…", "page": { "limit": 50, "next_cursor": "opaque" | null } } }

// Error (every 4xx/5xx, including 404/405/415/429/500 from middleware and Recover)
{ "error": { "code": "domain.reason", "message": "debug-only", "details": { … } }, "meta": { "request_id": "01J…" } }
```

| Rule | Detail |
|---|---|
| Mutually exclusive | A body has `data` **or** `error`, never both. Clients branch on which key is present |
| `meta.request_id` | Always present. Equals the `X-Request-Id` response header. It is no longer inside `error` (a change from M0) |
| `details` | Always an object (`{}` when empty), never `null` |
| Lists | `data` is always an array. Unpaginated lists (vehicles, at most 5) still send `meta.page` with `next_cursor: null` and `limit` equal to the server cap |
| `204` | No body: logout, logout-all, verify, password reset, invitation accept, delete. HTTP forbids a body there |
| Health | `/healthz` and `/readyz` use the same envelope. The current health payload moves under `data` |
| Go | `internal/transport/http/response` exposes generic helpers: `OK[T any](w, r, status int, data T)`, `List[T any](w, r, items []T, page Page)` and `Error(...)`, plus `type Meta struct{RequestID string; Page *Page}` and `type Page struct{Limit int; NextCursor *string}`. Handlers never call `json.Encoder` directly. A test checks that every route in the registry produces either `data` or `error` plus `meta.request_id` |
| Contracts | `packages/contracts` exports `ApiSuccess<T>`, `ApiList<T>`, `ApiError` and `Meta` types, and one zod parser, `parseEnvelope(schema)`, that unwraps `data` or raises a typed `ApiError`. Web and mobile both use it. The health clients (`toHealthState`) are updated in the same change |
| OpenAPI | Reusable `components.schemas`: `Meta`, `Page`, `ErrorBody`, `ErrorEnvelope`, and a `DataEnvelope` pattern (`allOf` with a `data` property per operation). Every operation references them |

### 6.1 Endpoints

| Method and path | Policy | Request (sketch) | Success | Errors (beyond `validation.failed` and `rate_limit.exceeded` where limited) |
|---|---|---|---|---|
| `GET /v1/legal/current` | public | — | 200 `{terms:{version,published_at,url_es,url_en}, privacy:{…}}` | — |
| `POST /v1/auth/register` | public, RL | `{email, password, first_name, last_name, phone?, locale?, accepted_terms_version, accepted_privacy_version}` | **202** `{status:"verification_pending"}`, always the same | `legal.version_outdated` 409 |
| `POST /v1/auth/verify` | public, RL | `{email, code}` | 204 | `auth.code_invalid` 422 |
| `POST /v1/auth/verify/resend` | public, RL | `{email}` | 202, always | — |
| `POST /v1/auth/login` | public, RL | `{email, password, client_kind:"web"\|"mobile"}` | 200 `TokenResponse` | `auth.invalid_credentials` 401, `auth.email_not_verified` 403, `auth.account_suspended` 403 |
| `POST /v1/auth/refresh` | public, RL | `{refresh_token}` | 200 `TokenResponse` | `auth.refresh_invalid` 401, `auth.session_revoked` 401 |
| `POST /v1/auth/logout` | any role | — | 204 (idempotent) | `auth.unauthenticated` 401 |
| `POST /v1/auth/logout-all` | any role | — | 204 | `auth.unauthenticated` 401 |
| `POST /v1/auth/password/forgot` | public, RL | `{email}` | 202, always | — |
| `POST /v1/auth/password/reset` | public, RL | `{email, code, new_password}` | 204 | `auth.code_invalid` 422 |
| `POST /v1/auth/invitations/accept` | public, RL | `{email, code, password}` | 204 | `auth.code_invalid` 422 |
| `GET /v1/me` | driver, admin; legal-exempt | — | 200 `User` | — |
| `PATCH /v1/me` | driver, admin | `{first_name?, last_name?, phone? (null clears), locale?}` | 200 `User` | — |
| `DELETE /v1/me` | driver; legal-exempt | `{password}` | 204 | `auth.password_incorrect` 403 |
| `POST /v1/me/legal-acceptance` | driver; legal-exempt | `{terms_version, privacy_version}` | 204 | `legal.version_outdated` 409 |
| `GET /v1/vehicles` | driver | — | 200 `{items: Vehicle[]}` (≤ 5, no pagination) | — |
| `POST /v1/vehicles` | driver, RL | `{plate, make, model, color}` | 201 `Vehicle` | `vehicle.plate_taken` 409, `vehicle.limit_reached` 409 |
| `GET /v1/vehicles/{id}` | driver, owner | — | 200 `Vehicle` | `vehicle.not_found` 404 |
| `PATCH /v1/vehicles/{id}` | driver, owner, RL | `{plate?, make?, model?, color?}` | 200 `Vehicle` | `vehicle.not_found` 404, `vehicle.plate_taken` 409 |
| `DELETE /v1/vehicles/{id}` | driver, owner, RL | — | 204 | `vehicle.not_found` 404, `vehicle.in_use` 409 (active from M3) |
| `GET /v1/admin/admins` | admin | `?limit=50&cursor=` (keyset) | 200 `{items: AdminUser[], next_cursor}` | — |
| `POST /v1/admin/admins` | admin | `{email, first_name, last_name, locale?}` | 201 `AdminUser` (`status:"pending_verification"`); 200 when re-inviting a pending invitee (OC-39) | `user.email_taken` 409 (active/suspended only) |

Every bearer route can also return `auth.unauthenticated` 401, `auth.session_revoked` 401, `auth.account_suspended` 403, `auth.forbidden` 403 (role), and `legal.acceptance_required` 403 (drivers on non-exempt routes). "RL" means a rate limit applies (§10.1).

### 6.2 Shapes

```jsonc
// TokenResponse
{ "access_token": "<jwt>", "token_type": "Bearer", "expires_in": 900,
  "refresh_token": "<base64url 32 bytes>", "refresh_expires_in": 2592000,
  "legal_acceptance_required": false,
  "user": { /* User */ } }

// User
{ "id": "uuid", "email": "…", "first_name": "…", "last_name": "…", "phone": null, "role": "driver",
  "status": "active", "locale": "es-DO", "email_verified_at": "…",
  "legal": { "terms_version": "2026-11-16", "terms_accepted_at": "…",
             "privacy_version": "2026-11-16", "privacy_accepted_at": "…" },
  "created_at": "…" }

// Vehicle
{ "id": "uuid", "plate": "A-123 456", "plate_normalized": "A123456",
  "make": "Toyota", "model": "Corolla", "color": "Gris", "created_at": "…", "updated_at": "…" }

// AdminUser
{ "id": "uuid", "email": "…", "first_name": "…", "last_name": "…", "status": "active", "locale": "es-DO", "created_at": "…" }
```

Request validation (validator tags, reported by JSON field):

| Field | Rule |
|---|---|
| `email` | `required,email,max=254`. Trimmed. Case-insensitive through `citext` |
| `password`, `new_password` | `required,min=10,max=128` (characters) plus the custom rule `password_common` (embedded list of the 10k most common passwords, compared case-insensitively) |
| `first_name` | `required,min=1,max=60` after trimming |
| `last_name` | `required,min=1,max=80` after trimming |
| `phone` | `omitempty,e164` |
| `locale` | `omitempty,oneof=es-DO en`. Defaults to `es-DO` |
| `client_kind` | `required,oneof=web mobile` |
| `code` | `required,len=6,numeric` |
| `plate` | `required,min=2,max=15`. Normalized (uppercase, spaces and dashes removed), then must match `^[A-Z0-9]{2,10}$`, otherwise rule `plate` |
| `make`, `model` | `required,min=1,max=50` |
| `color` | `required,min=1,max=30` |

Unknown fields are ignored in M1. A `role` field in any body has **no effect** (§8.4).

### 6.3 Error codes added to `errcodes`

| Code | HTTP | When |
|---|---|---|
| `auth.unauthenticated` | 401 | Missing, malformed, expired or badly signed bearer token. Unknown `kid`. Unknown session |
| `auth.session_revoked` | 401 | Session revoked (logout, reset, deletion, reuse), or a rotated refresh token was presented (spec §3.1 scenario) |
| `auth.refresh_invalid` | 401 | Unknown or expired refresh token |
| `auth.invalid_credentials` | 401 | Wrong email or password (one code for both) |
| `auth.email_not_verified` | 403 | Correct password, account `pending_verification` |
| `auth.account_suspended` | 403 | Correct password (login) or valid session (bearer), account `suspended` |
| `auth.forbidden` | 403 | Role not allowed on the route |
| `auth.password_incorrect` | 403 | Re-authentication failed (`DELETE /v1/me`) |
| `auth.code_invalid` | 422 | Any verification, reset or invite code failure (OC-11) |
| `legal.acceptance_required` | 403 | Accepted versions are older than current. `details: {terms_version, privacy_version}` |
| `legal.version_outdated` | 409 | Submitted versions are not current. `details: {terms_version, privacy_version}` |
| `user.email_taken` | 409 | Admin creates an admin with an email already in use (admin-only, so no enumeration concern) |
| `vehicle.not_found` | 404 | Missing, deleted, owned by someone else, or malformed id |
| `vehicle.plate_taken` | 409 | Plate active on any account, mapped from the unique violation |
| `vehicle.limit_reached` | 409 | Already `MAX_VEHICLES_PER_USER` (5) active. `details: {limit}` |
| `vehicle.in_use` | 409 | FR-VEH-4. Defined now, returned from M3 (OC-29) |
| `rate_limit.exceeded` | 429 | §10. `Retry-After` header and `details: {retry_after_seconds}` |
| `http.invalid_json` | 400 | Body is not valid JSON or has the wrong JSON types |
| `http.payload_too_large` | 413 | Body over 64 KiB |
| `http.unsupported_media_type` | 415 | Body without `application/json` |

Track B adds `errors.<code>` keys in es-DO and en for every code (policies §2.8). Codes are never removed once published.

---

## 7. Authentication mechanics

### 7.1 Passwords

- **argon2id** via `golang.org/x/crypto/argon2` (promote it from indirect to direct). Parameters: m=64 MiB, t=3, p=2, 16-byte salt, 32-byte key. The output is a PHC string `$argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>`, so the parameters travel with the hash.
- **Rehash on login** when the stored parameters differ from the current ones (written in the login transaction).
- **Concurrency bound (OC-14):** a weighted semaphore of 4 around hash and verify. Waiting respects the request context. Peak memory ≈ 256 MiB.
- Comparison with `subtle.ConstantTimeCompare`.
- **Dummy hash:** computed once at boot with the current parameters, used for timing equalization (§7.7).

### 7.2 Access tokens (JWT) and keys

- `github.com/golang-jwt/jwt/v5` (OC-7). Signing alg `EdDSA` (Ed25519). The parser uses `WithValidMethods([]string{"EdDSA"})`, `WithIssuer("mypark-api")`, `WithAudience("mypark")`, `WithExpirationRequired()` and `WithLeeway(30*time.Second)`. Keys are chosen by the `kid` header from a map, and an unknown `kid` is rejected.
- Claims: `iss`, `aud`, `sub` (user `public_id`), `sid` (session `public_id`), `role`, `iat`, `nbf`, `exp` (iat + 15 min). No PII in claims.
- **Keys (OC-8):**

| Env var | Meaning | Compose default (dev only) |
|---|---|---|
| `JWT_SIGNING_KEY` | base64 Ed25519 **seed** (32 bytes). Required | an inline dev seed |
| `JWT_KEY_ID` | `kid` for the signing key. Required | `dev-1` |
| `JWT_VERIFY_KEYS` | Optional `kid=base64pubkey,…` for keys being rotated out | empty |

  `config.Load` fails when `APP_ENV=production` and `JWT_SIGNING_KEY` equals the committed dev seed. The same check applies to `AUTH_CODE_PEPPER`. Rotation: deploy the new signing key, keep the old public key in `JWT_VERIFY_KEYS` for ≥ 15 min, then drop it. A restart **does not** log users out, because refresh tokens are opaque rows in the DB.

### 7.3 Authenticating a request (OC-4)

`authn` middleware → `auth.Authenticator.Authenticate(ctx, bearer)`:

1. Parse `Authorization: Bearer <jwt>` and verify it (§7.2). Any failure → `401 auth.unauthenticated`.
2. One query, by `sid` and `sub`: `select s.id, s.revoked_at, s.absolute_expires_at, u.id, u.role, u.status, u.terms_version, u.privacy_version from auth_sessions s join users u on u.id = s.user_id where s.public_id = $sid and u.public_id = $sub`.
3. No row → 401 `auth.unauthenticated`. `revoked_at` set or absolute expiry passed → 401 `auth.session_revoked`. `status = 'suspended'` → 403 `auth.account_suspended`. `status in ('deleted','pending_verification')` → 401 `auth.session_revoked`. DB `role` ≠ claim `role` → 401 `auth.session_revoked` (the DB is authoritative).
4. `LegalPending` = the user's versions ≠ current versions (cached in `legal.Current` for 60 s). Drivers only.
5. `authz.WithPrincipal(ctx, p)`. Fill `reqctx.LogFields.UserPublicID`.

Cost: one PK or unique-index lookup per authenticated request, well inside NFR-PERF-2.

### 7.4 Sessions and refresh rotation (OC-3, OC-5, OC-31)

**Login** (after the checks in §7.7), in one transaction: insert `auth_sessions`, insert `refresh_tokens` (32 bytes from `crypto/rand`, base64url; store SHA-256), audit `auth.login_succeeded`. Then sign the JWT.

**Refresh**, in one transaction, commit-first (§4.5):

```
row := select t.*, s.* from refresh_tokens t join auth_sessions s … where t.token_hash = $1 for update of t, s
no row                                   → outcome invalid          (401 auth.refresh_invalid)
s.revoked_at not null                    → outcome revoked          (401 auth.session_revoked)
t.rotated_at not null  (REUSE)           → update auth_sessions set revoked_at=now, revoked_reason='reuse_detected'
                                           audit auth.refresh_reuse_detected (session id, ip, request_id)
                                           outcome reuse            (401 auth.session_revoked)
t.expires_at <= now or s.absolute_expires_at <= now
                                         → outcome invalid          (401 auth.refresh_invalid)
user status not active                   → revoke session, outcome revoked
else                                     → t.rotated_at = now; insert successor (expires = min(now+30d, absolute));
                                           s.last_used_at = now; outcome ok → new access + refresh
```

- `for update` serializes concurrent refreshes of the same token. The second one sees `rotated_at` and **revokes the family**. That is intended (OC-5). The mobile client single-flights refresh (architecture §5). **The web BFF must also single-flight per session.** Concurrent `proxy.ts` or RSC refreshes with one cookie will trip reuse detection and log the user out. Track B must design for this. Alternative considered: a 10 s grace window that tolerates a second use. It was rejected, because the successor token can't be returned again (only its hash is stored), so the grace would need a second live successor, which weakens detection.
- **Logout**: revoke the session from the principal's `sid` (`logout`). **Logout-all**: revoke every non-revoked session of the user (`logout_all`). Both are idempotent and audited.

### 7.5 Codes (verification, reset, invite)

- Generation: `crypto/rand` uniform in `[0, 999999]`, zero-padded to 6 digits.
- Storage: `code_hash = HMAC-SHA256(AUTH_CODE_PEPPER, purpose ‖ ":" ‖ user_public_id ‖ ":" ‖ code)`. `AUTH_CODE_PEPPER` is a required base64 env var of ≥ 32 bytes (inline dev default, refused in production).
- Issue, in one transaction: `update auth_codes set consumed_at = now() where user_id=$1 and purpose=$2 and consumed_at is null`, insert the new row, enqueue the email with the plaintext code in the payload, and audit.
- Verify, commit-first:

```sql
update auth_codes set attempts = attempts + 1
 where user_id = $1 and purpose = $2 and consumed_at is null and expires_at > now() and attempts < 5
returning id, code_hash, attempts;
```

  No row → `auth.code_invalid`. Constant-time compare. On a match, set `consumed_at` and apply the effect in the **same** transaction. On a mismatch, commit the increment and return `auth.code_invalid`. An unknown email does the same work shape against no row and returns the same code (OC-11).

### 7.6 Flows

| Flow | Transaction contents | Response |
|---|---|---|
| Register (new email) | Validate legal versions = current → hash password → insert user (`driver`, `pending_verification`, legal versions + now) → issue `verify_email` code → enqueue `verify_email` → audit `auth.registered` | 202 |
| Register (email exists, pending admin invitee) | Same as the active branch below, but **no email is sent** (OC-38) | 202, identical |
| Register (email exists, `driver`, `pending_verification`) | Hash → **overwrite** password, name, phone, locale and legal acceptance → reissue code → enqueue → audit `auth.registration_replaced` (OC-12) | 202, identical |
| Register (email exists, active or suspended) | Hash (discarded, keeps timing) → enqueue `account_exists` to the owner in the owner's locale. Dedupe per hour: `account_exists:<user>:<yyyymmddhh>` | 202, identical |
| Verify | Only `role='driver'` rows (an invitee behaves like an unknown email, OC-38). Code check → `status='active'`, `email_verified_at=now` → audit `auth.email_verified` | 204 |
| Resend | If the user exists, is a `driver` and is `pending_verification`: reissue and enqueue. Otherwise nothing | 202, always |
| Login | §7.7 | 200 |
| Forgot | If the user is active: issue `reset_password` and enqueue. Otherwise nothing | 202, always |
| Reset | Code check → hash new password → set hash and `password_changed_at` → **revoke all sessions** (`password_reset`) → consume other live codes → enqueue `password_changed` (OC-36) → audit `auth.password_reset` | 204 |
| Invitation accept | Only `role='admin'` rows in `pending_verification`; anything else behaves like an unknown email (`auth.code_invalid`). Code check (`admin_invite`) → set hash, `active`, `email_verified_at` → audit `admin.invitation_accepted` | 204 |

### 7.7 Anti-enumeration and timing

| Surface | Rule |
|---|---|
| Register | Same status, same body (`{status:"verification_pending"}`) and same headers for new, pending and existing emails. Every path runs one argon2id hash. Only the existing owner learns about the attempt, by email |
| Forgot, resend | Always 202 with the same body. The extra work for an existing user is two small inserts (a few ms). Accepted residual risk: this is statistically weak and rate-limited to 3/min per IP and 5/hour per email |
| Login | Unknown or deleted email → `hasher.Verify(dummyHash, password)`, then `auth.invalid_credentials`. Known email → real verify. `auth.email_not_verified` and `auth.account_suspended` are returned **only after the password matches**. The rate limit is checked **before** hashing |
| Codes | One error code for every failure (§7.5) |
| Vehicles | `vehicle.plate_taken` reveals that a plate is registered somewhere. The spec requires it (FR-VEH-3). Mitigated by the write rate limit. Accepted |

The test asserts behavior, not wall-clock time (§15.3): a spy hasher records exactly one `Verify` call for the unknown, wrong-password and correct-password paths.

### 7.8 Account deletion (FR-AUTH-9, OC-18)

`DELETE /v1/me {password}`: verify the password (`403 auth.password_incorrect` on mismatch). Then, in one transaction:

- Set `email = 'deleted+' || public_id || '@deleted.invalid'`, `first_name = 'deleted'`, `last_name = 'deleted'`, `phone = null`, `password_hash = null`, `status = 'deleted'`, `deleted_at = now()`.
- Soft-delete the vehicles and replace their plates (`plate_normalized = 'X' || <8 uppercase hex chars of the vehicle id>`, `plate_display` the same, make, model and color `'-'`), which frees the plate.
- Revoke all sessions (`account_deleted`), consume live codes, and scrub pending outbox payloads for the user.
- Audit `user.deleted` (actor = the user, no PII).

The email becomes free to register again. Financial rows (M3+) keep the FK to the anonymized user.

### 7.9 Secrets never leak

- DTO fields for passwords, codes and refresh tokens are `secret.String`, so a stray `slog.Any("req", dto)` prints `[redacted]`.
- `AccessLog` logs the route pattern, never the raw path, query or headers (already true).
- `apperr` messages are fixed English strings with no input echoed. Validation `details` carry field, rule and param, never the value.
- The outbox `last_error` holds the SMTP error text only. Payloads are never logged.
- Config `String()` and `GoString()` are redacted (§14).

---

## 8. Authorization

### 8.1 Route policies, deny by default

```go
type Policy struct {
    Public      bool          // no bearer; mutually exclusive with Roles
    Roles       []authz.Role  // allowed roles; non-empty when !Public
    LegalExempt bool          // skips the legal gate (drivers)
    Limit       *ratelimit.Rule
}
r.Handle("POST /v1/vehicles", router.Policy{Roles: driverOnly, Limit: &limits.VehicleWrite}, h.create)
```

- `router.Handle` **panics at startup** if a policy is neither public nor has roles, or if a `/v1/admin/` pattern allows anything other than `admin` (structural guard for the user's role-middleware decision).
- The router keeps a registry of `(pattern, policy)`. `router.Routes()` feeds the authorization matrix test, so a new route without matrix coverage fails CI.

### 8.2 Role matrix (policy level)

| Route group | anonymous | driver | admin |
|---|---|---|---|
| `GET /v1/legal/current`, `/v1/auth/{register,verify,verify/resend,login,refresh,password/*,invitations/accept}` | ✓ | ✓ (no bearer needed) | ✓ |
| `/v1/auth/logout`, `/logout-all` | 401 | ✓ | ✓ |
| `GET/PATCH /v1/me` | 401 | ✓ | ✓ |
| `DELETE /v1/me`, `POST /v1/me/legal-acceptance` | 401 | ✓ | 403 `auth.forbidden` |
| `/v1/vehicles…` | 401 | ✓ (own only) | 403 `auth.forbidden` (OC-16) |
| `/v1/admin/…` | 401 | **403 `auth.forbidden`** | ✓ |

### 8.3 Ownership in every use case (OC-1)

- Use cases take the `authz.Principal` as an **explicit parameter**, e.g. `vehicles.Service.Update(ctx, p authz.Principal, id uuid.UUID, in UpdateInput)`. They re-assert the role (defense in depth) and return `apperr.Forbidden(auth.forbidden)` on a mismatch.
- **Ownership by construction:** driver-path repository methods all take the owner id, so no unscoped lookup exists. Example: `GetOwned(ctx, ownerID, publicID)` → `where public_id = $1 and user_id = $2 and deleted_at is null`. Updates and deletes use the same predicate and check the affected row count.
- Not found and not owned are indistinguishable: `404 vehicle.not_found`. A malformed `{id}` is also a 404 (parsed in the handler).
- M3 reservations reuse this exactly: principal parameter, owner-scoped queries, `reservation.not_found`.

### 8.4 Privilege boundaries

- The role is set only by the server: `register` always inserts `driver`, the seed and invitations insert `admin`. No DTO has a `role` field. Test: register with `"role":"admin"` in the body produces a driver.
- Admins can't delete themselves through `DELETE /v1/me` (driver-only). There is no admin removal in M1, so the system can't end up with no admin.
- Audit: every admin action (`admin.created`) is recorded with actor `admin`.

### 8.5 Legal gate (FR-AUTH-10, OC-15)

- On register, the submitted versions must equal the current ones (`409 legal.version_outdated` otherwise).
- At login and on each request, a driver whose accepted versions differ from the current ones has `LegalPending`. Login still succeeds and returns `legal_acceptance_required: true`.
- With `LegalPending`, non-exempt driver routes return `403 legal.acceptance_required` with the current versions. Exempt routes: `GET /v1/me`, `POST /v1/me/legal-acceptance`, `DELETE /v1/me`, logout and logout-all. Deletion stays possible, because refusing new terms must not block erasure.
- `POST /v1/me/legal-acceptance` stores both versions and timestamps and audits `user.legal_accepted`.
- Admins are exempt (OC-15): they are staff, and the documents are the driver service terms.

---

## 9. Vehicles (FR-VEH-1…5)

- **Normalize** (domain, TDD): uppercase (`unicode`), drop spaces and `-`, then require `^[A-Z0-9]{2,10}$` (OC-30). `plate_display` keeps the trimmed input.
- **Create**, in one transaction: `select id from users where id = $1 for update` (serializes one user's writes) → `count(*) … where user_id=$1 and deleted_at is null` ≥ `MAX_VEHICLES_PER_USER` → `409 vehicle.limit_reached` → insert. A unique violation on `vehicles_plate_active_key` maps to `409 vehicle.plate_taken`, which covers both another account and the driver's own duplicate.
- **Update**: owner-scoped. A plate change re-normalizes and may hit `plate_taken`. `updated_at = now()`.
- **Delete**: owner-scoped. `ReservationChecker.HasActive(ctx, vehicleID)` → `409 vehicle.in_use` (always false in M1). Soft delete (`deleted_at`) frees the plate.
- `MAX_VEHICLES_PER_USER`: config, default 5, range 1–20 (FR-VEH-5 "configurable"). It moves to policy versions in M4 if needed.

---

## 10. Rate limiting (policies §2.7, OC-21…OC-23)

### 10.1 Rules

| Rule | Limit | Key (hashed) | Endpoints |
|---|---|---|---|
| `login_ip_email` | 5 / min | `login:` ip ‖ lower(email) | login |
| `login_email` (our call) | 20 / hour | `login_e:` lower(email) | login |
| `register_ip` | 3 / min | `reg:` ip | register |
| `code_request_ip` | 3 / min | `code:` ip | verify/resend, password/forgot |
| `code_request_email` (our call) | 5 / hour | `code_e:` lower(email) | register, verify/resend, password/forgot |
| `code_submit_ip` (our call) | 10 / min | `codesub:` ip | verify, password/reset, invitations/accept |
| `refresh_ip` (our call) | 60 / min | `refresh:` ip | refresh |
| `vehicle_write_user` (our call) | 20 / min | `veh:` user id | POST, PATCH and DELETE vehicles |

`reservation create 10/min per user` (M3) and `device API 100/s per device` (M2) reuse the same `Limiter` port. Every attempt counts, successful or not, so the behavior doesn't reveal which attempts failed.

### 10.2 Mechanics

```sql
insert into rate_limit_counters as c (key, window_start, count, expires_at)
values ($key, $window_start, 1, $window_start + $window)
on conflict (key) do update set
  count        = case when c.window_start = excluded.window_start then c.count + 1 else 1 end,
  window_start = excluded.window_start,
  expires_at   = excluded.expires_at
returning count, expires_at;
```

`window_start = date_bin($window, now, epoch)`, a fixed window. A boundary burst of up to 2× is accepted. If `count > limit` → `429 rate_limit.exceeded`, `Retry-After: ceil(expires_at − now)` seconds, and `details.retry_after_seconds`. Checks that need the email (login, code requests) run **in the handler after decoding and before any hashing or DB lookup of the user**. IP and user rules run in the per-route `RateLimit` wrapper. A limiter DB error **fails closed** on auth routes (500 `internal.unexpected`, logged) and is logged and ignored on vehicle routes. Throttling is logged at warn level with the rule name and the hashed key. It is not audited (it would flood the audit log).

### 10.3 Client IP (OC-22)

`ClientIP` middleware: if the TCP peer (`r.RemoteAddr`) is inside `TRUSTED_PROXY_CIDRS`, use the right-most address in `X-Forwarded-For` that is not a trusted proxy. Otherwise use the peer. Compose dev default: the `mypark-internal` network range (`172.16.0.0/12`). The default is empty, so outside compose no forwarded header is trusted. **Track B contract item:** the BFF sets `X-Forwarded-For` to the browser IP on every call to Go. Without it, all web users share one IP bucket.

---

## 11. Email outbox and worker (FR-NOTIF-1, FR-NOTIF-2, FR-I18N-5)

### 11.1 Enqueue

`notifications.Notifier.Enqueue(ctx, Email{Template, Locale, To, UserID, Data, DedupeKey})` inserts into `outbox_messages` using the transaction from `ctx` (it errors without one). `on conflict (dedupe_key) do nothing`. The locale is the recipient's `users.locale`.

### 11.2 Templates

Templates are embedded with `embed.FS` under `features/notifications/templates/<locale>/<template>.{subject.txt,txt,html}` and rendered with `text/template` and `html/template`. Every message is multipart (plain text + HTML). The product name and brand line "MyPark · Banreservas Zona Colonial" come from a shared partial. A parity test asserts that both locales have every template file and that each renders with sample data. M1 templates: `verify_email`, `password_reset`, `password_changed`, `account_exists`, `admin_invite`. Codes are shown as plain text with no link, because there are no deep links in M1.

### 11.3 Worker jobs (`cmd/worker`, `infrastructure/jobs`)

| Job | Cadence | Mechanism |
|---|---|---|
| Outbox dispatch | 2 s | **Claim** (short transaction): `update outbox_messages set status='sending', attempts=attempts+1, available_at=now()+interval '1 min' where id in (select id … where status in ('pending','sending') and available_at <= now() order by available_at limit 10 for update skip locked) returning *`. **Send** outside any transaction. **Finish**: `sent` (+ `sent_at`, scrub `payload.code`), or on error `pending` with `available_at = now() + min(5s·2^attempts, 15 min)`. After 8 attempts → `dead` (scrubbed, logged at error level). A crashed worker's row comes back when its lease expires |
| Rate-limit purge | 1 min | `delete from rate_limit_counters where expires_at < now()` |
| Auth purge | 1 h | Delete `refresh_tokens` and `auth_codes` 30 days past expiry. Delete sessions that are revoked or expired and older than 30 days (policies §1.8) |
| Outbox purge | 24 h | Delete `sent` and `dead` rows older than 90 days |

- **SMTP:** `infrastructure/email` over `net/smtp`. `Message-ID: <dedupe_key-hash@mypark.local>` (OC-24).
- **Worker config:** a new `config.LoadWorker` with `DATABASE_URL`, `WORKER_HEALTH_ADDR`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_FROM`, and optional `SMTP_USERNAME`, `SMTP_PASSWORD` and `SMTP_STARTTLS`. Compose defaults: `mailpit:1025` and `MyPark <no-reply@mypark.local>`.
- Delivery semantics are **at-least-once**. Nothing is lost, because the enqueue is transactional. Duplicates are possible only if the worker crashes between the SMTP `DATA` acknowledgement and the `sent` update, and they are recognizable by `Message-ID`.

---

## 12. Audit log (OC-26)

The entry is written by `audit.Recorder.Record` **in the action's transaction**. The fields are filled from `Principal`, `reqctx` (request id) and `ClientIP`.

| Action | Actor | Entity | `after` metadata |
|---|---|---|---|
| `auth.registered`, `auth.registration_replaced` | anonymous | user | `{locale}` |
| `auth.email_verified` | user | user | — |
| `auth.login_succeeded` | user | session | `{client_kind}` |
| `auth.login_failed` | anonymous | user (**known emails only**) | `{reason: password\|not_verified\|suspended}` |
| `auth.refresh_reuse_detected` | system | session | `{user_id}` (public id) |
| `auth.logout`, `auth.logout_all` | user | session / user | `{sessions_revoked}` |
| `auth.password_reset_requested`, `auth.password_reset` | anonymous / user | user | `{sessions_revoked}` |
| `user.profile_updated` | user | user | `{fields: ["first_name","locale"]}` (names only) |
| `user.legal_accepted` | user | user | `{terms_version, privacy_version}` |
| `user.deleted` | user | user | — |
| `admin.created` | admin | user | `{role:"admin"}` |
| `admin.invitation_accepted` | user | user | — |
| `admin.seeded` | system | user | — |

Never stored: passwords, hashes, codes, tokens, emails, names, phones or plates. Vehicle CRUD is not audited in M1 (not a security event, our call). The audit `ip` is the resolved client IP. Retention is two years, and the purge is deferred (§16.3).

---

## 13. Admin (FR-AUTH-7)

### 13.1 First admin (`cmd/seed`, OC-20)

- Env: `SEED_ADMIN_EMAIL`, `SEED_ADMIN_PASSWORD`, `SEED_ADMIN_NAME`, `APP_ENV`. Compose inline dev defaults: `admin@mypark.local`, a dev-only password of ≥ 10 characters that is not on the common list, and `MyPark Admin`.
- Behavior, in the seed transaction: if **any** non-deleted admin exists → do nothing (log `admin seed skipped`). Otherwise validate the password against the same policy as register, hash it, insert an `admin` that is `active` and `email_verified_at = now()`, with no legal versions (exempt), and audit `admin.seeded`.
- If the variables are unset → skip with a warning (not an error). With `APP_ENV=production`, the known dev default password is refused (non-zero exit). The seed never re-hashes or overwrites, so a compose restart is a no-op. The password is never logged.

### 13.2 Later admins (OC-19)

`POST /v1/admin/admins` (admin) → `users.Service.CreateAdmin(p, in)` → `auth.Accounts.CreateInvitedAdmin`. In one transaction: insert an `admin` user (`pending_verification`, `password_hash` null; an email held by an active or suspended account → `409 user.email_taken`; a pending invitee → reissue, `200`; a pending driver → takeover, see OC-39), issue an `admin_invite` code (48 h), enqueue `admin_invite`, audit `admin.created`. The invitee calls `POST /v1/auth/invitations/accept`, then logs in normally. `GET /v1/admin/admins` lists admins with keyset pagination (`created_at desc, id`).

### 13.3 "Admin can log into the admin area, a driver cannot (403)"

Both roles use `POST /v1/auth/login`, and the response carries `user.role`. Go enforces the admin area: every `/v1/admin/*` route returns 403 `auth.forbidden` to drivers. Track B's `(admin)` layout guard checks `GET /v1/me` → `role` server-side, and the admin screens call `/v1/admin/*`.

---

## 14. M0 follow-ups (fix before or within M1)

| # | Item | Current state (code) | Fix |
|---|---|---|---|
| F1 | Config `String()`/`GoString()` redaction | `config.Config` has only `LogValue()`. `fmt.Printf("%v"/"%#v", cfg)` prints the DB URL and, after M1, the JWT seed and pepper | Add `String()` and `GoString()` that render the redacted `LogValue` group. Test with `%v`, `%+v`, `%#v` and `%s` |
| F2 | Recover: abort handling and stale headers | `ErrAbortHandler` is **already** re-panicked (`recover.go`). Headers set by the handler before the panic (Content-Type, Content-Length, Cache-Control, Location) leak into the 500 | Before writing the envelope, delete every header except `X-Request-Id` (and those set by `SecureHeaders`). Use `errors.Is` on an `error` panic value. Test: the handler sets `Content-Length: 3` and panics, and the envelope is still valid JSON |
| F3 | AccessLog for panicked requests | The log line is skipped on panic (`Recover` is outside) | `defer` the log line. On panic: log `status=500, panicked=true`, then re-panic so `Recover` still handles it |
| F4 | `response.RequestIDFunc` set once | Assigned in every `httpserver.NewHandler` call and in tests. Parallel tests race | Move the request-ID context key to the leaf package `transport/http/reqctx` and have `response` read it directly. Delete the package variable (OC-33). `go test -race` covers it |
| F5 | Revoke `PUBLIC` TEMP/CONNECT on the DB | Only `revoke all on schema public from public` | Migration `00002_db_privileges` (§5.1) |
| F6 | testsupport drop cleanup first | `NewEmptyDB` registers the drop only after `NewPool` succeeds, so a pool error leaks the database | Register `t.Cleanup(drop)` right after `create database`. Close the pool in a separate, later-registered cleanup (LIFO: pool closes first) |
| F7 | CORS middleware | `CORS_ALLOWED_ORIGINS` is parsed but unused | §4.4. Tests: allowed origin, disallowed origin, preflight on a POST-only route returns 204 not 405, no `Allow-Credentials` |
| F8 | CI: `npm audit --omit=dev`, `govulncheck`, `go mod tidy` check | Not in CI (policies §2.6 notes this) | `ci-server`: `go run golang.org/x/vuln/cmd/govulncheck@<pinned> ./...` and `go mod tidy && git diff --exit-code go.mod go.sum`. `ci-js`: `npm audit --omit=dev --audit-level=high` |
| F9 (new, found in this design) | Test migrations run as the production role | `testsupport.NewDB` runs goose on the **superuser** pool, so `alter default privileges for role mypark_migrator` never applies in tests. Every M1 table would give `mypark_app` no grants: tests run as `mypark_app` would fail, and grant tests would pass vacuously | Create the test database with `owner mypark_migrator` and run goose under `set role mypark_migrator`, as compose does. On PG15+ the owner owns `public`, and `citext`/`btree_gist` are trusted extensions. Add **positive** grant tests (`mypark_app` can select and insert on every M1 table, including `audit_log`) next to the negative ones. Land before S3 |
| — | Locale regex, localized not-found page | Web-side | **Deferred to Track B** |

Noted only (out of M1): strict `additionalProperties` and zod `strictObject` (before M2), an insert-only lot seed (before M4; the admin seed in §13.1 is already insert-only), SHA-pinned actions and similar supply-chain hardening (M5).

---

## 15. Testing strategy

Follows policies §2.3 and architecture §16. All tests run with `-race`. Time comes from a fake `Clock` (`testsupport.FakeClock`). There are no sleep-based assertions. Randomness (codes, tokens) is injectable where a test needs determinism.

### 15.1 Unit tests (domain, TDD first)

| Package | Cases |
|---|---|
| `vehicles` | Plate normalization table (spaces, dashes, lowercase, unicode, too short or long, symbols); limit decision |
| `auth` | Password policy (length bounds, common list, case); code generation range and format; HMAC code hash binds purpose and user; the refresh **decision function** (invalid, revoked, reuse, expired, absolute cap, ok) as a pure function over a row snapshot and now; session expiry math (sliding vs absolute) |
| `hashing` | PHC encode and parse round-trip; rehash detection; verify rejects a tampered salt or hash; the semaphore bounds concurrency |
| `jwt` | Sign and verify round-trip; rejects `alg=none`, HS256 signed with the public key bytes, wrong `iss` or `aud`, expired, `nbf` in the future (beyond leeway), unknown `kid`, missing `sid` |
| `legal` | Current-version selection with future-dated rows |
| `router` | Panics on a missing policy and on a non-admin `/v1/admin/` policy |
| `middleware` | CORS, ClientIP (trusted vs untrusted peer, spoofed XFF), SecureHeaders, F2–F4 |
| `notifications` | Template parity and rendering per locale; backoff schedule; scrub |

### 15.2 Integration tests (real Postgres, `testsupport.NewDB`)

Run per package with `TEST_DATABASE_URL`, connected **as `mypark_app`** (`set role mypark_app` on pool connections) so grant mistakes surface in tests:

- Grants (after F9): `mypark_app` can select and insert on every M1 table. `audit_log` update and delete are denied. `mypark_app` can connect after F5.
- Register → code row hashed, outbox row with the code, audit row, all in one transaction (forced failure after the enqueue leaves nothing).
- Code attempts: 5 wrong → the 6th is `code_invalid` even with the right code. Attempts persist across the failing requests (commit-first).
- **`TestRefresh_ReuseRevokesFamily`** (spec scenario): login → R1 → refresh → R2 → present R1 → `401 auth.session_revoked`, session `revoked_reason='reuse_detected'`, audit row present. Then R2 → `401 auth.session_revoked`, and the access token from R2 → `401 auth.session_revoked`.
- **`TestRefresh_ConcurrentSameToken`**: 20 goroutines present the same token → at most one success, the family is revoked, no deadlock.
- Password reset revokes every session (the previous access tokens return 401 immediately).
- Logout revokes only the current session. Logout-all revokes all of them.
- Vehicles: plate taken across accounts; reusing a deleted vehicle's plate succeeds; **`TestVehicles_ConcurrentCreateRespectsLimit`** (10 parallel creates → exactly 5); parallel creates of the same plate by two users → one 201, one 409.
- Deletion anonymizes the user, frees the email and the plates, and revokes sessions.
- Seed: run twice → one admin. The password is unchanged on the second run (the hash is byte-equal).
- Rate limiter: the 6th login in a window → 429 with `Retry-After`. The window rolls over with the fake clock. Two limiter instances (two pools) share the count.
- Outbox dispatcher against a fake `Sender`: success, retry with backoff, dead after the max, lease reclaim after a simulated crash, payload scrub.

### 15.3 Anti-enumeration and timing

- Register (new, pending, active email): byte-identical bodies after removing `request_id`, same status, same header set.
- Forgot and resend (existing, unknown): identical responses.
- Login: a spy `Hasher` records exactly one `Verify` for unknown email, deleted user, wrong password and correct password. `email_not_verified` appears only with the correct password.
- Codes: an unknown email and a wrong code give an identical response.

### 15.4 Log-leak test

`TestNoSecretsInLogs` runs the full flow (register → verify → login → refresh → reuse → reset → delete) against the real handler with a captured JSON slog handler. It asserts that the output contains none of: the password, the codes, the access and refresh tokens, the email and its local part, the first and last name, the plate, `JWT_SIGNING_KEY` or `AUTH_CODE_PEPPER`. The same assertion runs over every `audit_log` row and every error response body.

### 15.5 Authorization matrix

`cmd/api` integration test `TestAuthzMatrix`. It builds the production handler (`newHandler(deps)`) on a migrated DB and iterates `router.Routes()`. **The test fails if a route has no matrix entry.** Each entry provides a request builder and an optional owned-resource fixture. Principals:

| Principal | Expectation |
|---|---|
| anonymous | public → normal behavior; protected → 401 `auth.unauthenticated` |
| driver (owner) | driver routes → 2xx; admin routes → 403 `auth.forbidden` |
| other driver | owned resources → 404 `vehicle.not_found` (body identical to a truly missing id) |
| admin | admin routes → 2xx; driver-only routes → 403 `auth.forbidden` |
| driver, legal pending | non-exempt → 403 `legal.acceptance_required`; exempt → 2xx |
| suspended user (valid token) | → 403 `auth.account_suspended` |
| revoked session (valid JWT) | → 401 `auth.session_revoked` |
| tampered or expired or `alg=none` token | → 401 `auth.unauthenticated` |

### 15.6 Mailpit path and backend acceptance

- CI adds a `mailpit` service (`axllent/mailpit:v1.33.0`). Env `TEST_SMTP_ADDR` and `TEST_MAILPIT_API`. Skipped locally when unset, required when `CI=true` (OC-34).
- `TestDispatcher_DeliversToMailpit`: enqueue `verify_email` (es-DO and en) → run one dispatch tick → `GET /api/v1/messages` shows the subject in the right language, the code in the body, `Message-ID` set, and the payload scrubbed in the DB.
- `TestM1BackendAcceptance` (exit criteria, backend side): seed → register (mobile) → read the code from Mailpit → verify → login `mobile` → add a vehicle → login `web` with the same account → list vehicles (same car) → refresh rotation and reuse → the seeded admin logs in → `GET /v1/admin/admins` 200 → the driver gets 403 on the same route. Every error response's `code` is in `errcodes.All()` (Track B proves the translations).

### 15.7 Coverage (OC-35)

The domain files of `features/auth`, `features/vehicles` and `infrastructure/hashing` need ≥ 90% **statement** coverage. Go has no branch coverage, so statement coverage is the agreed proxy, enforced by a CI step over `go test -coverprofile` filtered to those files. Use cases and adapters are covered by integration tests without a numeric gate. HTTP handlers are covered by contract tests (status + code) and the matrix.

---

## 16. Build order and exit verification

### 16.1 Slices (each slice is one or more PRs of ≤ 400 lines, chained)

| # | Slice | Contents | Depends on |
|---|---|---|---|
| S0 | **M0 follow-ups** | F1–F9. F7 (CORS) can land with S3 | — |
| S1 | **Contract (handoff)** | `openapi.yaml`: all endpoints, schemas, `bearerAuth`, error examples. `errcodes` additions plus `go generate`. Track B regenerates `packages/contracts`. Both developers approve | — |
| S2 | Foundations | `apperr`, `authz`, `secret`, `clock`, `reqctx`, `httpx`, `router` with policies, `hashing`, `jwt`, config additions, `TxRunner` | S0 |
| S3 | Identity schema | Migrations 00002–00003, sqlc queries, legal seed | S2 |
| S4 | Outbox + audit write side | Migration 00004, `notifications.Enqueue`, `audit.Recorder`, grant test | S3 |
| S5 | Register, verify, resend, login, legal current | Including the anti-enumeration tests | S4 |
| S6 | Sessions | Authenticator and per-request check, refresh rotation and reuse, logout, logout-all, legal gate | S5 |
| S7 | Forgot and reset | | S6 |
| S8 | Worker delivery | SMTP sender, templates, dispatcher, purge jobs, Mailpit CI service | S4 |
| S9 | Me | Get, patch, delete (anonymization), legal acceptance | S6 |
| S10 | Vehicles | Migration 00005, CRUD, limit, races | S6 |
| S11 | Admin | Seed first admin, create and list admins, invitation accept | S6, S8 |
| S12 | Rate limits | Migration 00006, limiter, ClientIP, rules wired | S5 (can run in parallel with S7–S11) |
| S13 | Hardening | Authz matrix, log-leak test, acceptance test, coverage gate, doc sync (§16.2) | all |

Why audit and outbox come before the auth core (instead of last): register and login write audit rows and outbox messages **inside** their transactions. Building them first avoids retrofitting transaction boundaries.

### 16.2 Docs to update in the same PRs (policies §2.10)

- `architecture.md` §4.2: per-route auth and rate-limit wrappers, and the global chain order.
- `architecture.md` §4.4: resend and invitation endpoints, the admin admins routes, the legal gate.
- `architecture.md` §5: session check per request, no grace window, single-flight requirement for the BFF, `X-Forwarded-For` contract.
- `architecture.md` §11.1: `auth_sessions`, the `refresh_tokens.session_id` change, `audit_log.occurred_at`, `outbox_messages` columns, `rate_limit_counters`.
- `architecture.md` §17: ADRs for the stateful session check and the Postgres limiter.
- `policies.md` §2.7: the extra limits (OC-23). §2.6: remove the "not in CI yet" row.
- `spec.md` §10.2: append this document's accepted our-calls.
- `deploy/compose/docker-compose.yml`: new env defaults (JWT, pepper, trusted proxies, SMTP, seed admin, max vehicles). Also fix the header comment that tells readers to create `deploy/compose/.env`, which contradicts the no-`.env` policy.

### 16.3 Deferred and accepted risks

| Item | Why deferred | Target |
|---|---|---|
| Purge of never-verified accounts (email squatting cleanup) | Low risk with OC-12 overwrite | M5 |
| Per-user cap on active sessions | Rate limits bound growth | M5 |
| `audit_log` 2-year purge (needs a privileged role or `security definer` function, because the app has no delete) | No data is 2 years old in Phase 1 | Phase 2 |
| Outbox payload encryption | Codes are scrubbed after send and expire in 15 min | Revisit with `SECRETBOX_KEY` in M2 |
| Plate enumeration via `vehicle.plate_taken` | Required by FR-VEH-3. Rate-limited | Accepted |

### 16.4 M1 exit criteria, backend side

| Exit criterion (spec §8 M1) | Backend evidence |
|---|---|
| Register → verification email in Mailpit → verify → login on web and mobile | `TestM1BackendAcceptance` (both `client_kind` values, same account) |
| Refresh rotation + reuse detection covered by integration tests | `TestRefresh_ReuseRevokesFamily`, `TestRefresh_ConcurrentSameToken`, `TestRefresh_ReuseRevocationSurvivesErrorResponse` |
| Vehicle CRUD with plate uniqueness. All errors shown in both languages | Vehicle integration tests. Every returned code is in `errcodes` and the generated contract (translations are Track B's) |
| An admin can log into the admin area. A driver cannot (403) | `TestAuthzMatrix` + the acceptance test (seeded admin 200, driver 403 `auth.forbidden` on `/v1/admin/admins`) |

---

## 17. Left to the builder

- Exact sqlc query names, file splits and the Go names inside each feature, within the package map in §4.1.
- Choosing and licensing the common-password list (must be MIT, BSD or similar per policies §2.6) and its size (around 10k entries).
- The exact dev default values (JWT seed, pepper, seed admin password). Generate them once, and mark them dev-only in compose.
- Email copy in es-DO and en (shared with Track B's i18n tone) and the HTML layout.
- Whether `PATCH` bodies use pointer fields or a presence-tracking type for "absent vs null" (`phone: null` clears it).
- Keyset cursor encoding for `GET /v1/admin/admins` (opaque base64 of `(created_at, id)`).
- Whether S0 is one PR or several, and where CORS lands (S0 or S2).
- `govulncheck` invocation style (`go run` with a pinned version or a `tool` directive in `go.mod`).
- The number of workers or goroutines per job. One goroutine per job is enough in M1.
