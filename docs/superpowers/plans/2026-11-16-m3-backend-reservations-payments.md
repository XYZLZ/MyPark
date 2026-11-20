# M3 Track A — Backend Reservations and Payments Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go side of M3:
- the published M3 contract;
- reservations with an atomic reservable cap;
- the reservation fee charged through a `PaymentGateway` port with a simulated adapter (sync charge, `202` fallback, reconcile job);
- cancellations and refunds processed by the worker with backoff;
- the reservation QR at the entry barrier, pay station and exit, including the physically-full cancel and the stranded check-in restore (M3B-60);
- the worker jobs that expire, remind, notify and refund;
- the `reservation_arrivals` and `no_show` simulator scenarios driven by seeded simulated drivers.

Each part ships with the tests that prove the M3 exit criteria on the backend.

**Architecture:**
- New features follow architecture §4.1: pure domain files, `service.go`, `ports.go`, `http.go`, and `postgres/` over the shared sqlc `dbgen`. Features talk only through exported interfaces.
- Imports point one way: `reservations` → `payments`, `gate`, `alerts`, `notifications`, `availability`. `payments` never imports `reservations`. The callbacks it needs (`OutcomeApplier`, `RefundNotifier`) are defined in `payments` and implemented by `reservations` (resolved gap 1).
- Every write runs in one short transaction carried by `ctx` (`database.TxRunner`). Locks follow the single M3B-14 order. Gateway calls run outside any transaction.
- `reservations.Holds` replaces `availability.NoReservations`, `reservations.VehicleChecker` replaces `vehicles.NoReservations`, and `reservations.GateAccess` implements the new `gate.Reservations` port. The gate keeps owning `stays` and the cash/zero `parking_fee` rows (M3B-28).
- The simulator gains a driver client over the public driver API and never touches the database (M3B-44).

**Tech Stack:** Go 1.27, stdlib `net/http` ServeMux, pgx/v5, sqlc, goose, PostgreSQL 18 (`tstzrange`, GiST via `btree_gist`), go-playground/validator v10, `crypto/hmac`, `crypto/rand`, `math/rand/v2`, `github.com/google/uuid`, **`rsc.io/qr`** (the only new module, M3B-43), openapi-typescript in `packages/contracts`.

**Spec:** `docs/superpowers/specs/2026-11-16-m3-backend-reservations-payments-design.md`, cited below as "design §N". Executors keep it open: this plan points to its SQL, tables, outcome tables and payload shapes instead of copying them. §2.1 decisions are binding, and the §2.2 calls are approved. Also binding: `docs/spec.md`, `docs/architecture.md`, `docs/policies.md`.

## Global Constraints

- **Skills.** Before any Go work, load `.claude/skills/golang-patterns/SKILL.md`, `.claude/skills/golang-pro/SKILL.md` and `C:\Users\benja\.claude\skills\go-testing\SKILL.md`. Before any migration or SQL, load `.claude/skills/supabase-postgres-best-practices/SKILL.md`. Where a skill conflicts with the design, spec, architecture or policies, the documents win.
- **Go.** Go 1.27, module `github.com/XYZLZ/MyPark/apps/server`. Use modern Go (`errors.AsType`, `t.Context()`, `wg.Go`, `range` over int, `min`/`max`, `slices`/`maps`). Nonces, QR nonces and gateway refs come from `crypto/rand` only. **One new module only:** `rsc.io/qr` (S4.3), which must pass `govulncheck` and the security checklist.
- **Time.** Business time comes from `clock.Clock`. Every business timestamp is written as the `@now` query parameter. `now()` appears only in `created_at` defaults (M2 D3). Fee payment rows write `created_at = @now` explicitly (M3B-53). Job bodies take `now`, so tests drive them with `testsupport.FakeClock`.
- **Money.** Integer centavos (`bigint *_cents`), DOP implied. Refunds are always the full fee in M3.
- **Transactions.**
  - Driver-API and device transactions run `set local lock_timeout = '1s'` and `set local statement_timeout = '2s'` (M3B-55). A timeout answers `500 internal.unexpected` and stores nothing.
  - No network I/O inside a transaction.
  - Lock order (M3B-14, design §8.1): `lot_occupancy` → `reservations` → `reservation_slot_loads` (ascending `slot_start`) → `stays` → `payments` → `refunds` → `users` → inserts (`reconciliation_alerts`, `outbox_messages`, `audit_log`) → `device_events`. `vehicles` is only ever locked `FOR SHARE`, and before `reservations`. The exceptions are the exit pass and the late entry pass (`lot_occupancy` → `stays` → `reservations`, design §8.1), which run under the lot lock.
  - Every hold release or restore runs in the same transaction as its status change.
- **Notifications.** A transaction that changes effective holds **at `$now`** takes `lot_occupancy FOR UPDATE` first, then bumps `version` and runs `pg_notify('lot_changed', …)` (M3B-13). Whether the window contains `$now` is decided before locking. Expiry jobs never bump.
- **Secrets.** Card numbers, CVCs, QR tokens, `QR_HMAC_KEY` and simulated-driver passwords never appear in logs, error bodies, `validation.failed` details, `device_events` (QR redacted to `sha256:<12 hex>`), or stored rows. `payments.Card` prints `card(****1234)`. Only brand and last four are stored (FR-PAY-9).
- **Retention.** `delete` and `truncate` are revoked from `mypark_app` on `reservations`, `refunds`, `reconciliation_alerts` and `sim_gateway_charges`, and they stay revoked on `payments`.
- **Data access.**
  - sqlc queries live in `internal/infrastructure/database/queries/<feature>.sql`: `reservations.sql`, `payments.sql`, `paymentsim.sql` and `alerts.sql` are new, and `gate.sql`, `outbox.sql` and `vehicles.sql` are extended. The generated `dbgen` code is committed.
  - Each task adds the queries it needs (resolved gap 5).
- **Contract.**
  - The contract comes first. New request schemas have `additionalProperties: false`. Every error is a catalog code.
  - Every new code gets `errors.<domain>.<reason>` in es-DO and en in **both** `apps/web/src/i18n/messages/` and `apps/mobile/src/lib/i18n/locales/` (`npm run check:i18n`). Codes are never removed.
  - Any change after S1 needs both developers' approval (policies §2.2).
- **Configuration.**
  - No `.env` files. Compose uses inline `${VAR:-dev-default}`.
  - Accessors sit over unexported fields, and `String`/`GoString`/`LogValue` redact.
  - The `QR_HMAC_KEY` dev value is refused in production.
- **Authorization.** Every new route is `Roles: driver` behind the legal gate (not `LegalExempt`). Another driver's reservation or payment, a malformed id and an unknown id all give the same `404` (M3B-33).
- **Testing.**
  - TDD, test first, everything with `-race`. Use the injected clock, seeded randomness and an injected `sleep` in `paymentsim`. No `time.Sleep` assertions (policies §2.3).
  - Integration tests use real Postgres through `testsupport.NewTestDB`, connected as `mypark_app`.
  - After every reservation integration test, `testsupport.AssertSlotInvariant` (S3.2) runs: for every slot row, `held` equals the count of `pending_payment`/`confirmed` reservations whose window covers it. After every gate test, `testsupport.AssertOccupancyInvariant` runs.
- **Go commands** run from `apps/server` in Docker (CLAUDE.md). Define this once per shell and use it in every step:

  ```bash
  docker run -d --name mypark-pg-test -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:18   # once
  # Mounts the repo root: errcodes go:generate writes ../../packages/contracts and config tests read deploy/compose.
  gox() { MSYS_NO_PATHCONV=1 docker run --rm -v "$(cd ../.. && pwd -W)":/repo -w /repo/apps/server \
    -v mypark-gomod:/go/pkg/mod -v mypark-gocache:/root/.cache/go-build \
    -e TEST_DATABASE_URL='postgres://postgres:test@host.docker.internal:55432/postgres?sslmode=disable' \
    golang:1.27 "$@"; }
  ```

  - Tests: `gox go test -race ./...`.
  - sqlc: `gox go tool sqlc generate -f internal/infrastructure/database/sqlc.yaml`, and `gox go tool sqlc diff -f internal/infrastructure/database/sqlc.yaml`.
  - Error-code generation: `gox go generate ./internal/domain/errcodes/...`.
  - Lint: `MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)":/src -w /src golangci/golangci-lint:v2.15.0 golangci-lint run ./...`.
- **JS.** Use npm only. `npm run gen:contracts` regenerates `packages/contracts/src/openapi.d.ts`. To add a dependency, edit `package.json` and then run `npm install`. No new JS dependency is planned.
- **Commits.**
  - Conventional Commits, scopes `server|contracts|deploy|docs|ci`. **No Co-Authored-By and no AI attribution of any kind.**
  - Work-unit commits are ≤ 400 changed lines. Generated `dbgen`, `openapi.d.ts`, `error-codes.ts`, `go.sum` and `package-lock.json` don't count toward the budget; `openapi.yaml` does.
- **Sensitive tasks** (marked **Sensitive**) get the PR template's "Security checklist" plus an automated risk review (`review-risk` or `/security-review`). Task-scoped reviews of S3.x, S4.x, S5.x and S6.x run on opus (design §16).

## Review Focus

1. **Arrival times in other offsets or with sub-second precision.** Examples: `2026-12-01T11:02:30.5-04:00` or `2026-12-01T15:00:00.000Z`. Expected: normalized to the same UTC 5-minute boundary (`15:05:00Z` and `15:00:00Z`) on quote and create. Never `422`, and never a window off by an offset. Test: S3.1 `TestNormalizeArrival_OffsetsAndFractions`; S3.2 `TestQuote_OffsetArrivalNormalized`.
2. **Card fields typed the way people type them.** Examples: `4242 4242 4242 4242` (spaces, as M3B-11 prints it), `4242-4242-…`, `exp_year: 28`, `exp_month: 13`, a 5-digit CVC, numbers sent as JSON strings. Expected: spaces and hyphens are stripped before validation (plan decision 6). A two-digit year, a bad month or a bad CVC gives `422 validation.failed` naming only the field and rule. No value is echoed, and nothing is stored or charged. Test: S4.1 `TestNormalizeCardNumber`, `TestCardRequest_Validation`; S4.2 `TestPay_CardJunkIs422WithoutEcho`.
3. **One `Idempotency-Key` reused across two reservations.** A client generates one key per screen, not per reservation. Expected: the key is scoped to the reservation (M3B-7), so the second reservation gets its own attempt and charge, not `409 idempotency.key_reused`. Test: S4.2 `TestPay_SameKeyDifferentReservations`.
4. **Cancel racing the QR check-in.** The driver taps cancel while the barrier reads the QR, inside the window. Expected: exactly one wins. Either the reservation is cancelled with a refund and the gate answers `deny gate.credential_invalid`, or it is checked in and cancel answers `409 reservation.invalid_state`. No deadlock, no 500, and the slot invariant holds. Test: S6.1 `TestGateQR_CancelRacesCheckIn`.
5. **Garbage ids, cursors and QR values.** Examples: a malformed uuid, an unknown uuid or another driver's id on every `{id}` route; a tampered `cursor`; `scope=foo`; a QR token with trailing whitespace or a newline (copy-paste into the console). Expected: identical `404` bodies (minus `request_id`) for the ids. `422 validation.failed` for the cursor and the scope. For the QR token: `deny gate.credential_invalid` at the device, and `422` at the control API. Never a 500. Test: S3.3 `TestReservations_GarbageIDsAre404`, `TestListReservations_BadCursorOrScope`; S5.3 `TestPayments_GarbageIDsAre404`; S6.1 `TestGateQR_WhitespaceTokenInvalid`.

---

## File Structure

```
apps/server/
├─ api/openapi.yaml                                         # S1
├─ cmd/api/{main.go, openapi_test.go, authz_matrix_test.go, logleak_test.go, fixtures_test.go,
│           reservations_test.go, cap_test.go, pay_test.go, cancel_test.go, refunds_test.go,
│           receipts_test.go, gate_qr_test.go, gate_qr_void_test.go, holds_test.go, jobs_test.go,
│           scenario_test.go, scenario_m3_test.go, acceptance_m3_test.go}
├─ cmd/worker/{main.go, main_test.go}                       # S4.3, S5.2, S6.2, S7.x
├─ cmd/seed/{main.go, main_test.go}                         # S8.1
├─ internal/config/{config.go, dev.go, worker.go, seed.go, config_m3_test.go}   # S2.2, S8.1
├─ internal/domain/errcodes/errcodes.go                     # S1
├─ internal/domain/apperr/apperr.go                         # S4.2 KindPaymentRequired
├─ internal/devicefleet/{drivers.go, drivers_test.go}       # S8.1
├─ internal/features/
│  ├─ reservations/{domain.go, state.go, qr.go, qrdecide.go, ports.go, service.go, create.go, read.go,
│  │                patch.go, pay.go, apply.go, cancel.go, holds.go, vehiclechecker.go, gateaccess.go,
│  │                jobs.go, http.go, postgres/repo.go}
│  ├─ payments/{card.go, gateway.go, backoff.go, replay.go, ports.go, fees.go, reconcile.go,
│  │            refunds.go, receipts.go, http.go, postgres/repo.go}
│  ├─ alerts/{alerts.go, postgres/recorder.go}               # S5.1
│  ├─ gate/{ports.go, entry.go, entry_qr.go, pass.go, paystation.go, exit.go, state.go, void.go}   # S6.x
│  ├─ notifications/{notifications.go, templates.go, qrimage.go, dispatcher.go,
│  │                 templates/{en,es-DO}/reservation_*.{html,txt}, refund_succeeded.*}
│  ├─ auth/seed_driver.go, vehicles/seed.go                  # S8.1
├─ internal/infrastructure/
│  ├─ paymentsim/{paymentsim.go, paymentsim_test.go}         # S4.1
│  ├─ email/smtp.go                                          # S4.3 inline images
│  ├─ ratelimit/rules.go                                     # S3.2, S4.2
│  └─ database/{migrations/00010_reservations.sql, migrations/00011_payments_refunds.sql,
│               queries/{reservations,payments,paymentsim,alerts,gate,outbox}.sql, dbgen/, grants_m3_test.go}
├─ internal/simulator/{driver.go, drivers_pool.go, reservations.go, car.go, scenarios.go, control.go, runner.go}  # S8.x
├─ internal/testsupport/{reservations.go, slots.go}
└─ scripts/covergate.sh                                      # S9.1
packages/contracts/src/{error-codes.ts, openapi.d.ts, m3.types.test.ts, fixtures/{index.ts,m3.ts}}
apps/web/src/i18n/messages/{en,es-DO}.json   apps/mobile/src/lib/i18n/locales/{en,es-DO}.json
deploy/compose/docker-compose.yml   .github/workflows/ci-server.yml
docs/{spec.md, architecture.md, policies.md}   CLAUDE.md
```

**Slicing.** The design's nine slices keep their ids. S1 stays one task with two work-unit commits, because it is the handoff. S2, S3, S4, S5, S6, S7, S8 and S9 are split into `.1`/`.2`/`.3` sub-slices to stay within 400 lines. The marker pattern `^(S|C)[0-9.]+$` allows these ids. That gives 22 tasks.

---

## Task S1: Contract handoff — error codes, OpenAPI paths, pending markers, fixtures

Design slice **S1** (M3B-4, M3B-47, design §14). **Sensitive (payments contract).** Two work-unit commits: about 250 lines (codes and translations), then about 400 lines (`openapi.yaml`, tests, fixtures). It ends with both developers' approval, so Track B can start.

**Files:**
- Modify: `apps/server/internal/domain/errcodes/{errcodes.go,errcodes_test.go}`, `packages/contracts/src/error-codes.ts` (generated), `apps/web/src/i18n/messages/{en,es-DO}.json`, `apps/mobile/src/lib/i18n/locales/{en,es-DO}.json`
- Modify: `apps/server/api/openapi.yaml`, `apps/server/cmd/api/openapi_test.go`, `packages/contracts/src/{openapi.d.ts,fixtures/index.ts}`
- Create: `packages/contracts/src/m3.types.test.ts`, `packages/contracts/src/fixtures/m3.ts`

**Interfaces:**
- Consumes: `errcodes.Code`, `catalog`, `All()`; the M2 `loadOperations`, `pendingSliceID` and `TestOpenAPI_NoPendingOperations` in `cmd/api/openapi_test.go`.
- Produces:
  - **Go constants** (values from design §14.2):
    - `ReservationNotFound`, `ReservationCapacityReached`, `ReservationActiveExists`, `ReservationBlockedNoShows`, `ReservationLotClosed`, `ReservationNoSpaceNow`
    - `ReservationArrivalInPast`, `ReservationBeyondHorizon`, `ReservationInvalidState`, `ReservationPaymentExpired`, `ReservationVehicleChangeClosed`, `ReservationTooEarly`, `ReservationWindowClosed`
    - `PaymentDeclined`, `PaymentInProgress`, `PaymentNotFound`, `IdempotencyKeyReused`
    - 17 in total.
  - **Operations and their `x-mypark-pending` markers.** Each marker is the sub-task that serves the operation and removes the marker:

    | operationId | Route | Marker |
    |---|---|---|
    | `quoteReservation` | `POST /v1/reservations/quote` | `S3.2` |
    | `createReservation` | `POST /v1/reservations` | `S3.2` |
    | `listReservations` | `GET /v1/reservations` | `S3.3` |
    | `getReservation` | `GET /v1/reservations/{id}` | `S3.3` |
    | `changeReservationVehicle` | `PATCH /v1/reservations/{id}` | `S3.3` |
    | `payReservation` | `POST /v1/reservations/{id}/pay` | `S4.2` |
    | `getReservationQr` | `GET /v1/reservations/{id}/qr` | `S4.2` |
    | `cancelReservation` | `POST /v1/reservations/{id}/cancel` | `S5.1` |
    | `listPayments` | `GET /v1/payments` | `S5.3` |
    | `getPayment` | `GET /v1/payments/{id}` | `S5.3` |

  - **Schemas** (design §7):
    - `Reservation` (§7.1, with nested `ReservationVehicle`, `HoldWindow`, `LatestPayment`, `ReservationRefund`, `ReservationStay`) and `ReservationList`
    - `QuoteRequest`, `Quote`, `CreateReservationRequest`, `ChangeVehicleRequest`
    - `PayRequest` (`card_number` string, `exp_month` 1–12, `exp_year` 2000–2099, `cvc` string) and `PayPending` (`{reservation, payment: {id, status: pending}}`)
    - `ReservationQr` (`token`, `entry_valid_from`, `entry_valid_until`; the description says the token keeps working at the pay station and exit while `checked_in`)
    - `Receipt` (§7.10) and `ReceiptList`
  - **Headers and responses on pay.** `Idempotency-Key` is a required header parameter with pattern `^[A-Za-z0-9_-]{8,64}$`. Responses are `200`, `202` (with `Retry-After`), `402`, `409`, `422` and `429`, plus the `Idempotent-Replayed` response header. The description lists the three test cards of M3B-11 and says that spaces in `card_number` are ignored (plan decision 6).
  - **Device API.** The `DeviceEventResult.reason` description gains `reservation.too_early` and `reservation.window_closed`.
  - **Control API.** `InjectCarRequest.credential` enum gains `reservation_qr`. It also gains `qr` (string, pattern `^[A-Za-z0-9_-]{86}$`), which is required only with `reservation_qr`; the description says so (M3B-46).
  - **Go test.** In `openapi_test.go`, `var m3PendingAllowlist = map[string]bool{"S3.2": true, "S3.3": true, "S4.2": true, "S5.1": true, "S5.3": true}`. `TestOpenAPI_NoPendingOperations` skips markers in it, and S9.1 deletes it.
  - **Fixtures** in `fixtures/m3.ts`, typed `satisfies components['schemas'][...]` (no zod, resolved gap 7):
    - `reservationPendingFixture`, `reservationConfirmedFixture`, `reservationCheckedInFixture`, `reservationCancelledRefundPendingFixture`
    - `quoteFixture`, `payPendingFixture`, `payDeclinedErrorFixture`, `reservationQrFixture`, `receiptFixture`

- [ ] **Step 1: Write the failing tests (codes)**

```go
// errcodes_test.go
func TestCatalog_M3Codes(t *testing.T) // table of 17 {Const, literal}, e.g. {ReservationCapacityReached,"reservation.capacity_reached"}, {PaymentDeclined,"payment.declined"}, {IdempotencyKeyReused,"idempotency.key_reused"}; each is in All()
```

```ts
// m3.types.test.ts
test('ERROR_CODES includes every M3 code') // arrayContaining the 17 literals
```

- [ ] **Step 2: Run to verify they fail** — `cd apps/server && gox go test ./internal/domain/errcodes/...` → FAIL (`undefined: errcodes.ReservationNotFound`). Then `npm test -w packages/contracts` → FAIL.
- [ ] **Step 3: Implement** the constants and catalog entries. Add `errors.reservation.*`, `errors.payment.*` and `errors.idempotency.key_reused` to all four locale files. es-DO uses neutral professional Spanish.
- [ ] **Step 4: Verify** — `gox go generate ./internal/domain/errcodes/... && gox go test ./internal/domain/errcodes/...`, then `cd ../.. && npm test -w packages/contracts && npm run check:i18n && npm run typecheck -w apps/web && npm run typecheck -w apps/mobile`. Expected: all pass, and `check:i18n` reports parity.
- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/domain/errcodes packages/contracts/src/error-codes.ts packages/contracts/src/m3.types.test.ts apps/web/src/i18n apps/mobile/src/lib/i18n
git commit -m "feat(contracts): add m3 reservation and payment error codes with translations"
```

- [ ] **Step 6: Write the failing contract tests (paths)**

```ts
// m3.types.test.ts
test('every M3 driver path exists') // expectTypeOf<paths['/v1/reservations/quote']['post']>().not.toBeNever(); … the ten operations of the table
test('pay requires the Idempotency-Key header') // expectTypeOf<paths['/v1/reservations/{id}/pay']['post']['parameters']['header']>().toEqualTypeOf<{'Idempotency-Key': string}>()
test('Reservation.status has the seven states') // 'pending_payment'|'confirmed'|'checked_in'|'completed'|'cancelled'|'expired_unpaid'|'expired_no_show'
test('InjectCarRequest.credential accepts reservation_qr')
test('fixtures parse as their schema types') // the nine fixtures compile under satisfies; reservationConfirmedFixture.hold_window.ends_at > starts_at
```

```go
// openapi_test.go
func TestOpenAPI_M3PendingMarkersMatchAllowlist(t *testing.T) // each of the ten M3 ops carries its table marker OR none (the serving task removes it); every marker present is in m3PendingAllowlist
```

- [ ] **Step 7: Run to verify they fail** — `npm test -w packages/contracts` → FAIL (`paths['/v1/reservations/quote']` is never). Then `gox go test ./cmd/api/ -run OpenAPI` → FAIL.
- [ ] **Step 8: Implement** the YAML, the allowlist and the fixtures. Export `fixtures/m3.ts` from `fixtures/index.ts`.
- [ ] **Step 9: Verify** — `gox go test -race ./cmd/api/ -run OpenAPI`, then `cd ../.. && npm run gen:contracts && npm test -w packages/contracts && npm run typecheck -w packages/contracts && npm test -w apps/web && npm test -w apps/mobile`. Expected: pass. A second `npm run gen:contracts` leaves `git diff --exit-code packages/contracts/src/openapi.d.ts` clean.
- [ ] **Step 10: Commit and hand off**

```bash
git add apps/server/api/openapi.yaml apps/server/cmd/api/openapi_test.go packages/contracts/src
git commit -m "feat(contracts): publish m3 reservations, pay, qr, cancel and receipts contract"
```

Stop here. Ask the user for both developers' approval of the contract before S2 (M3B-4).

---

## Task S2.1: Schema — migrations 00010 and 00011, grants and schema tests

Design slice **S2** (design §6). **Sensitive (migration).** Budget: about 380 lines.

**Files:**
- Create: `internal/infrastructure/database/migrations/{00010_reservations.sql,00011_payments_refunds.sql}`, `internal/infrastructure/database/grants_m3_test.go`, `internal/infrastructure/database/reservations_schema_test.go`

**Interfaces:**
- Consumes: migrations 00001–00009. `btree_gist` is enabled in 00001.
- Produces:
  - Exactly the tables, columns, constraints and indexes of design §6.1 and §6.2: `reservations`, `reservation_slot_loads`, `users.reservation_blocked_until`, `stays.reservation_id` with `stays_reservation_id_check` and `stays_reservation_key`, the `payments` columns and indexes, `refunds`, `sim_gateway_charges`, `reconciliation_alerts`, and the swapped `outbox_messages_template_check`.
  - The constraint name is `outbox_messages_template_check`, the Postgres default for an inline column check, so the drop works (design §18).
  - `refunds.next_attempt_at` has no default; the service writes `@now`.

- [ ] **Step 1: Write the failing tests**

```go
func TestGrants_M3Tables(t *testing.T) // as mypark_app: select/insert/update allowed on reservations, reservation_slot_loads, refunds, sim_gateway_charges, reconciliation_alerts;
                                       // delete on reservation_slot_loads allowed; delete AND truncate denied (42501) on reservations, payments, refunds, sim_gateway_charges, reconciliation_alerts
func TestSchema_OneLiveFeePayment(t *testing.T) // second pending reservation_fee payment for one reservation → 23505 payments_reservation_live_key; a failed one beside a pending one → ok
func TestSchema_OneLiveRefund(t *testing.T) // second pending refund for one payment → 23505 refunds_payment_live_key; a failed refund then a pending one → ok
func TestSchema_OneActiveReservationPerUser(t *testing.T) // second pending_payment for one user → 23505 reservations_user_active_key; checked_in + pending_payment → ok
func TestSchema_ReservationChecks(t *testing.T) // table: window not '[)' → 23514; confirmed without qr_nonce → 23514; cancelled without cancelled_by → 23514; refund_reason on confirmed → 23514; slot_start not 5-min aligned → 23514
func TestSchema_StayReservationLink(t *testing.T) // credential_kind 'reservation' without reservation_id → 23514; second stay for one reservation → 23505 stays_reservation_key
func TestSchema_M2RowsStillValid(t *testing.T) // an M2 cash parking_fee row inserts unchanged; a parking_fee with reservation_id → 23514 payments_kind_target_check
func TestSchema_OutboxTemplates(t *testing.T) // insert each of the 5 new template names → ok; 'nope' → 23514
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./internal/infrastructure/database/ -run 'Grants_M3|Schema_'` → FAIL (`relation "reservations" does not exist`).
- [ ] **Step 3: Implement** both migrations, copying design §6.1 and §6.2. In `refunds`, `sim_gateway_charges` and `reconciliation_alerts`, write the column tables as SQL with the listed checks and indexes. `revoke delete, truncate` on the four new tables.
- [ ] **Step 4: Run** — `gox go test -race ./internal/infrastructure/database/...`, then `gox go test -race ./...` (M2 suites still pass on the new schema). Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/infrastructure/database
git commit -m "feat(server): add reservations, refunds, alerts and simulated gateway schema"
```

---

## Task S2.2: Configuration — QR key, payment and simulated gateway settings

Design slice **S2** (config part, design §5.2 and §9.5, M3B-5, M3B-21, M3B-54). **Sensitive (secrets).** Budget: about 250 lines.

**Files:**
- Modify: `internal/config/{config.go,dev.go,worker.go}`, `deploy/compose/docker-compose.yml` (api and worker env)
- Create: `internal/config/config_m3_test.go`

**Interfaces:**
- Consumes: the `config.Load`/`LoadWorker` patterns, `devBytes` and `loadSecretVar`.
- Produces (identical accessors on `config.Config` and `config.WorkerConfig`):
  - `QRHMACKey() []byte`: env `QR_HMAC_KEY`, base64, ≥ 32 bytes after decoding. The dev default is `DevQRHMACKey` in `dev.go` (32 random bytes, generated once). It is refused when `APP_ENV=production`.
  - `PaymentChargeTimeout() time.Duration`: `PAYMENT_CHARGE_TIMEOUT`, default `5s`, range 1–10 s.
  - `SimGatewayLatency() time.Duration`: `SIM_GATEWAY_LATENCY`, default `300ms`, range 0–5 s.
  - `SimGatewayTimeoutDelay() time.Duration`: `SIM_GATEWAY_TIMEOUT_DELAY`, default `30s`, range 1–120 s.
  - `PaymentGateway() string`: `PAYMENT_GATEWAY`, the only value is `simulated`. It defaults to `simulated` outside production; production fails when it is unset.
  - `LogValue` prints `qr_hmac_key: [redacted]`.

- [ ] **Step 1: Write the failing tests**

```go
func TestLoad_M3Defaults(t *testing.T) // dev env: QRHMACKey()==devBytes(DevQRHMACKey), PaymentChargeTimeout()==5s, SimGatewayLatency()==300ms, SimGatewayTimeoutDelay()==30s, PaymentGateway()=="simulated"
func TestLoad_M3Validation(t *testing.T) // table: QR_HMAC_KEY 31 bytes → error naming QR_HMAC_KEY; PAYMENT_CHARGE_TIMEOUT=11s → error; =500ms → error; PAYMENT_GATEWAY=stripe → error
func TestLoad_M3Production(t *testing.T) // APP_ENV=production with dev QR key → "QR_HMAC_KEY must not be the development default in production"; PAYMENT_GATEWAY unset → error; set to simulated → ok
func TestLoadWorker_M3(t *testing.T) // the same five accessors on WorkerConfig with the same defaults and the same production refusals
func TestConfig_LogValueRedactsQRKey(t *testing.T) // fmt.Sprint(cfg), %#v and slog JSON never contain the base64 key
```

- [ ] **Step 2: Run to verify they fail** — `gox go test ./internal/config/ -run M3` → FAIL.
- [ ] **Step 3: Implement** the accessors and the compose defaults (`QR_HMAC_KEY: ${QR_HMAC_KEY:-<dev>}` on api and worker, with a `# dev only` comment; `PAYMENT_GATEWAY: ${PAYMENT_GATEWAY:-simulated}`).
- [ ] **Step 4: Run** — `gox go test -race ./internal/config/... ./cmd/...` and `docker compose -f deploy/compose/docker-compose.yml config -q`. Expected: `ok`, and compose valid.
- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/config deploy/compose/docker-compose.yml
git commit -m "feat(server): add qr key, payment timeout and simulated gateway config"
```

---

## Task S3.1: Reservations domain — arrival, window, slots, cap, state machine, Phase 1 guard

Design slice **S3** (pure domain, TDD, design §7.3, §8.3, §10.1, M3B-16 to M3B-18, M3B-48). Budget: about 380 lines.

**Files:**
- Create: `internal/features/reservations/{domain.go,domain_test.go,state.go,state_test.go}`

**Interfaces:**
- Consumes: `errcodes` (S1).
- Produces:
  - `type Policy struct{ VersionID int64; MaxReservablePct, BookingHorizonDays, ArrivalGraceMin, EarlyArrivalMin, PaymentTimeoutMin, MaxActivePerUser, SlotMin, NoShowThreshold, NoShowWindowDays, NoShowBlockDays int }`
  - `func Ceil5(t time.Time) time.Time`: the next 5-minute UTC epoch boundary, or `t` when it is already aligned (sub-seconds count as unaligned). The result is in UTC.
  - `func NormalizeArrival(raw, now time.Time, horizonDays int) (time.Time, errcodes.Code)`. It returns `Ceil5(raw)`. When that is before `Ceil5(now)` it returns `ReservationArrivalInPast`. When it is after `now + horizonDays·24h` it returns `ReservationBeyondHorizon`. Otherwise the code is `""`.
  - `type Window struct{ Start, End time.Time }` (half-open) and `func (w Window) Contains(t time.Time) bool` (`Start ≤ t < End`).
  - `func HoldWindow(arrival time.Time, p Policy) Window` = `[arrival − early, arrival + grace)`.
  - `func Slots(w Window) []time.Time`: the 5-minute starts in `[Start, End)`.
  - `func Cap(maxReservablePct, effectiveCapacity int) int` = `floor(pct × cap / 100)`, and 0 for a non-positive capacity.
  - `func CapReached(held []int, cap int) bool`: any `h ≥ cap`.
  - `func CheckPhase1(p Policy) error` (M3B-48): `ErrPolicyUnsupported` (wrapped with the field) unless `MaxActivePerUser == 1`, `SlotMin == 5`, and `EarlyArrivalMin` and `ArrivalGraceMin` are multiples of 5.
  - `type Status string` with `StatusPendingPayment`, `StatusConfirmed`, `StatusCheckedIn`, `StatusCompleted`, `StatusCancelled`, `StatusExpiredUnpaid` and `StatusExpiredNoShow` (the DB literals).
  - `type Event string` with `EvConfirm`, `EvExpireUnpaid`, `EvLateSuccess`, `EvDriverCancel`, `EvCheckIn`, `EvUnadmitted`, `EvNoShow`, `EvComplete`, `EvEntryVoided` and `EvLateEntryPass`.
  - `func Transition(from Status, ev Event) (Status, error)`: exactly the design §10.1 rows. `EvExpireUnpaid` and `EvLateSuccess` both take `pending_payment → expired_unpaid`. `EvDriverCancel` is allowed from `pending_payment` and `confirmed`. `EvUnadmitted` only from `confirmed`. `EvEntryVoided` takes `checked_in → confirmed`. `EvCheckIn` and `EvLateEntryPass` take `confirmed → checked_in`. Anything else returns `ErrInvalidTransition`.
  - `func (s Status) Active() bool`: `pending_payment` or `confirmed` (FR-RES-3). `func (s Status) HoldsSlots() bool`: the same two (M3B-16).

- [ ] **Step 1: Write the failing tests**

```go
func TestCeil5(t *testing.T) // 15:00:00Z→15:00:00Z; 15:00:00.001Z→15:05Z; 15:04:59Z→15:05Z; 11:02:30-04:00→15:05:00Z (UTC result)
func TestNormalizeArrival(t *testing.T) // now 14:58:10Z, horizon 7: raw 14:58→15:00 ok; raw 14:55→ArrivalInPast; raw == ceil5(now) exactly → ok;
                                        // raw 14:55Z+7d (aligned, ≤ now+7d) → ok; raw 15:00Z+7d (> now+7d) → BeyondHorizon
func TestNormalizeArrival_OffsetsAndFractions(t *testing.T) // Review Focus 1: "2026-12-01T11:02:30.5-04:00" → 2026-12-01T15:05:00Z; "2026-12-01T15:00:00.000Z" → 15:00:00Z
func TestHoldWindow(t *testing.T) // arrival 15:00, early 15, grace 20 → [14:45, 15:20); Contains(14:45)=true, Contains(15:19:59.999)=true, Contains(15:20)=false, Contains(14:44:59)=false
func TestSlots(t *testing.T) // default window → 7 slots 14:45…15:15, each aligned
func TestCap(t *testing.T) // (30,280)→84; (30,279)→83; (30,0)→0; (0,280)→0; (100,280)→280
func TestCapReached(t *testing.T) // held [83,83] cap 84 → false; [83,84] → true; [] → false
func TestCheckPhase1(t *testing.T) // seeded policy ok; MaxActivePerUser 2 → ErrPolicyUnsupported; SlotMin 10 → error; EarlyArrivalMin 12 → error
func TestTransition_FullTable(t *testing.T) // 7 statuses × 10 events: exactly the allowed design §10.1 pairs succeed with the listed target; every other pair → ErrInvalidTransition
```

- [ ] **Step 2: Run to verify they fail** — `gox go test ./internal/features/reservations/` → FAIL (package does not exist).
- [ ] **Step 3: Implement** `domain.go` and `state.go`. The transitions are a `map[Status]map[Event]Status`.
- [ ] **Step 4: Run** — `gox go test -race -cover ./internal/features/reservations/`. Expected: `ok`, with coverage ≥ 90 % on these two files.
- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/features/reservations
git commit -m "feat(server): add reservation domain rules and state machine"
```

---

## Task S3.2: Create and quote — atomic cap, one active reservation, slot invariant

Design slice **S3** (design §7.2, §7.3, §8.3, M3B-15, M3B-16, M3B-20, M3B-35, M3B-38, M3B-49, M3B-55, M3B-57). **Sensitive (concurrency).** Budget: about 400 lines.

**Files:**
- Create: `internal/features/reservations/{ports.go,service.go,create.go,http.go,postgres/repo.go}`, `internal/testsupport/{slots.go,reservations.go}`, `cmd/api/{reservations_test.go,cap_test.go}`
- Modify: `queries/reservations.sql`, `dbgen/*`, `internal/infrastructure/ratelimit/rules.go`, `cmd/api/main.go`, `cmd/api/openapi_test.go` (remove the `S3.2` markers in `openapi.yaml`)

**Interfaces:**
- Consumes: S3.1 domain, `tariffs.Current.AtTime`, `availability.Service.Snapshot` (unlocked public available, M3B-20), `database.TxRunnerPort`, `clock.Clock`.
- Produces:
  - **Rate limit.** `ratelimit.ReservationWriteUser = Rule{Name: "reservation_write_user", Limit: 30, Window: time.Minute, Key: KeyUser, Prefix: "rsv_w:", FailOpen: true}`.
  - **Repo** (in `ports.go`, all on the ctx transaction):
    - `LotForReservation(ctx, lotPublicID uuid.UUID) (Lot, error)` with `Lot{ID int64; PublicID uuid.UUID; Closed bool; Capacity, OutOfService int}` (an unlocked read)
    - `PolicyAt(ctx, lotID int64, now time.Time) (Policy, error)`
    - `VehicleForShare(ctx, userID int64, vehiclePublicID uuid.UUID) (VehicleRef, error)` (`FOR SHARE`, live and owned; `ErrNotFound`)
    - `BlockedUntil(ctx, userID int64) (time.Time, error)`
    - `HasActive(ctx, userID int64) (bool, error)`
    - `Insert(ctx, NewReservation) (Row, error)`, which maps `23505 reservations_user_active_key` to `ErrActiveExists`
    - `EnsureAndLockSlots(ctx, lotID int64, w Window) ([]int, error)` (the design §8.3 insert plus `for update` in ascending order; returns `held`)
    - `IncSlots(ctx, lotID int64, w Window) error` and `DecSlots(ctx, lotID int64, w Window) error`
    - `SlotLoads(ctx, lotID int64, w Window) ([]int, error)` (unlocked, for quote)
  - **Service.** `reservations.Deps{Tx database.TxRunnerPort; Repo Repo; Tariffs tariffs.Current; Avail Availability; Clock clock.Clock; Log *slog.Logger}` (S4.2 and S5.1 add fields) and `NewService(d Deps) *Service`.
  - `type Availability interface{ Snapshot(ctx context.Context, lotID int64) (availability.Snapshot, error) }`
  - `func (s *Service) Quote(ctx context.Context, userID int64, in QuoteInput) (QuoteResult, error)` with `QuoteInput{LotID uuid.UUID; ArrivalAt time.Time; VehicleID *uuid.UUID}` and `QuoteResult{LotID uuid.UUID; ArrivalAt time.Time; Window Window; FeeCents int64; PaymentTimeoutMin int}`.
  - `func (s *Service) Create(ctx context.Context, userID int64, in CreateInput) (View, error)` with `CreateInput{LotID, VehicleID uuid.UUID; ArrivalAt time.Time}`. The checks run in design §7.3 order inside one transaction with the M3B-55 timeouts. `fee_cents = 0` gives a logged `500` (M3B-49), and `CheckPhase1` failing gives a logged `500`.
  - `type View` is the design §7.1 representation (S3.3 fills `vehicle`, `latest_payment`, `refund` and `stay`; here they are `nil` except `vehicle`).
  - **HTTP.** `POST /v1/reservations/quote` and `POST /v1/reservations` (`201`), `Roles: driver`, with the limit `ReservationWriteUser` on create. Quote has no limit (it is read-only).
  - **Test helpers.**
    - `testsupport.AssertSlotInvariant(t testing.TB, db *pgxpool.Pool, lotID int64)`
    - `testsupport.SeedDrivers(t testing.TB, db *TestDB, n int) []Driver` with `Driver{UserID int64; PublicID uuid.UUID; VehicleID uuid.UUID}`: verified, legal versions accepted, one vehicle each
    - `testsupport.HoldSlots(t, db, lotID, w Window, held int)`: sets slot rows directly, for cap edges
  - `cmd/api`: `deps.limiter` is overridable. The cap tests use an allow-all `ratelimit.Limiter` double defined in `fixtures_test.go`.

- [ ] **Step 1: Write the failing tests**

```go
// cmd/api/reservations_test.go (FakeClock at 2026-12-01T13:58:10Z, pilot lot seeded, policy and tariff seeded)
func TestCreate_Success(t *testing.T) // arrival 15:02Z → 201; arrival_at 15:05Z; hold_window 14:50–15:25; status pending_payment; fee_cents == seeded fee; payment_due_at == now+10m; 7 slot rows held 1; AssertSlotInvariant
func TestCreate_ChecksInOrder(t *testing.T) // table, each isolates one check: unknown lot → 404 lot.not_found; other driver's vehicle → 404 vehicle.not_found; blocked_until > now → 409 reservation.blocked_no_shows with details.blocked_until;
                                           // lot closed → 409 reservation.lot_closed; past → 422 reservation.arrival_in_past; now+8d → 422 reservation.beyond_horizon; existing pending → 409 reservation.active_exists;
                                           // arrival in 10 min with public available 0 → 409 reservation.no_space_now; blocked AND closed → blocked_no_shows (order)
func TestCreate_ZeroFeeTariffIs500(t *testing.T) // tariff version with reservation_fee_cents 0 → 500 internal.unexpected; nothing inserted; log has the cause
func TestCreate_OneActiveUnderParallelism(t *testing.T) // one driver, 10 parallel creates (different arrivals) → exactly one 201, nine 409 reservation.active_exists; slot invariant
func TestQuote_ReadOnly(t *testing.T) // valid quote → 200 {arrival_at normalized, hold_window, fee_cents, payment_timeout_min 10}; no reservation row, no slot rows written
func TestQuote_SameErrorsAsCreate(t *testing.T) // arrival_in_past, beyond_horizon, blocked_no_shows, active_exists; capacity_reached from unlocked slots at 84
func TestQuote_OffsetArrivalNormalized(t *testing.T) // Review Focus 1: arrival_at "…T11:02:30.5-04:00" → arrival_at "…T15:05:00Z"
// cmd/api/cap_test.go
func TestReservations_CapAtomicPair(t *testing.T) // all 7 slots of one window held 83 (HoldSlots + 83 matching reservations), two different drivers create in parallel for that arrival
                                                  // → exactly one 201 and one 409 reservation.capacity_reached; every slot held == 84; AssertSlotInvariant
func TestReservations_CapConcurrent200(t *testing.T) // 200 SeedDrivers, allow-all limiter, 200 parallel creates with the same arrival_at → exactly 84 × 201 and 116 × 409 reservation.capacity_reached;
                                                     // no 500 and no deadlock (40P01) in logs; AssertSlotInvariant
func TestReservations_CapRollbackLeavesNoRow(t *testing.T) // at cap, a create → 409; count(reservations) for that driver == 0 (insert rolled back)
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./cmd/api/ -run 'Create_|Quote_|Reservations_Cap'` → FAIL (404 on the routes).
- [ ] **Step 3: Implement** the queries, repo, service, handlers and helpers. Wire the service in `newHandlerWithRoutes`. Remove the `S3.2` markers.
- [ ] **Step 4: Run** — `gox go test -race ./...` and `gox go tool sqlc diff -f internal/infrastructure/database/sqlc.yaml`. Expected: `ok`, and no diff.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add reservation quote and create with an atomic reservable cap"
```

---

## Task S3.3: Read, list, change vehicle — and the Holds and VehicleChecker adapters

Design slice **S3** (design §7.1, §7.4, §7.5, §7.7, §7.11, §8.4, M3B-31, M3B-33, M3B-36, M3B-57). Budget: about 400 lines.

**Files:**
- Create: `internal/features/reservations/{read.go,patch.go,holds.go,vehiclechecker.go}`, `cmd/api/holds_test.go`
- Modify: `queries/reservations.sql`, `dbgen/*`, `internal/features/reservations/{http.go,postgres/repo.go}`, `cmd/api/{main.go,reservations_test.go,authz_matrix_test.go}`, `openapi.yaml` (remove the `S3.3` markers)

**Interfaces:**
- Consumes: S3.2 service and repo; `availability.ReservationHolds`; `vehicles.ReservationChecker`.
- Produces:
  - `func (s *Service) Get(ctx context.Context, userID int64, id uuid.UUID) (View, error)`. Unknown, malformed or other driver → `404 reservation.not_found`.
  - `func (s *Service) List(ctx context.Context, userID int64, q ListQuery) ([]View, string /*next cursor*/, error)` with `ListQuery{Scope string /* "", "active", "past" */; Limit int; Cursor string}`. Keyset on `(arrival_at desc, id desc)`. The cursor is opaque base64url of `arrival_at|public_id`, and a malformed one → `422 validation.failed` (field `cursor`).
  - `func (s *Service) ChangeVehicle(ctx context.Context, userID int64, id, vehicleID uuid.UUID) (View, error)` (design §7.7). Lock the reservation, then the vehicle `FOR SHARE`. The same vehicle → no write.
  - `type Holds struct{…}` with `func NewHolds(pool *pgxpool.Pool) Holds`. It implements `availability.ReservationHolds` through `database.Conn(ctx, pool)` with the design §8.4 queries.
  - `type VehicleChecker struct{…}` with `func NewVehicleChecker(pool *pgxpool.Pool) VehicleChecker`. It implements `vehicles.ReservationChecker` (statuses `pending_payment`, `confirmed`, `checked_in`).
  - **Wiring in `cmd/api/main.go`:** `reservations.NewHolds(pool)` replaces both `availability.NoReservations{}` (in `newAvailabilityService` and in `gate.Deps.Holds`), and `reservations.NewVehicleChecker(pool)` replaces `vehicles.NoReservations{}`.
  - **HTTP.** `GET /v1/reservations`, `GET /v1/reservations/{id}`, and `PATCH /v1/reservations/{id}` (limit `ReservationWriteUser`).

- [ ] **Step 1: Write the failing tests**

```go
func TestGetReservation_Representation(t *testing.T) // pending reservation → every design §7.1 key present; vehicle {id, plate_display, make, model, color}; hold_window.ends_at exclusive; latest_payment null; refund null; stay null
func TestReservations_GarbageIDsAre404(t *testing.T) // Review Focus 5: GET/PATCH /v1/reservations/{x} for x ∈ {"abc", random uuid, another driver's id} → identical 404 reservation.not_found bodies minus request_id
func TestListReservations_ScopesAndPaging(t *testing.T) // 3 past + 1 active (direct SQL) → scope=active → 1; scope=past → 3; limit=2 → 2 + cursor → next page 2; order arrival desc
func TestListReservations_BadCursorOrScope(t *testing.T) // Review Focus 5: scope=foo, limit=0, limit=51, cursor "!!" and a well-formed base64 of garbage → 422 validation.failed naming the field
func TestChangeVehicle(t *testing.T) // confirmed (direct SQL) before arrival → 200 with the new vehicle; same vehicle → 200 and updated_at unchanged; pending_payment → 409 reservation.invalid_state;
                                    // now == arrival_at → 409 reservation.vehicle_change_closed; other driver's vehicle → 404 vehicle.not_found
func TestVehicleDelete_InUseForEachActiveStatus(t *testing.T) // pending_payment, confirmed, checked_in → DELETE /v1/vehicles/{id} → 409 vehicle.in_use; cancelled → 204
func TestCreate_VehicleDeleteSerialized(t *testing.T) // M3B-31: a side tx holds the vehicle FOR SHARE (as an in-flight create does) → DELETE /v1/vehicles/{id} has not answered after the side tx's insert of a
                                                     // pending_payment reservation; side tx commits → DELETE answers 409 vehicle.in_use (never deletes a vehicle with a pending reservation)
func TestHolds_EffectiveAndUpcoming(t *testing.T) // confirmed windows [14:45,15:20) and [16:45,17:20), pending one in window: EffectiveHolds(15:00)==1, (15:20)==0, (14:44:59)==0; UpcomingArrivals(14:00, 2h)==2; pending never counted
func TestAvailability_NetsHolds(t *testing.T) // one confirmed hold effective → GET /v1/lots/{id}/availability effective_holds 1 and available reduced by 1; upcoming_reservations_2h counts
func TestAuthzMatrix_M3Routes(t *testing.T) // extend the matrix: every new driver route → anonymous 401, admin 403, driver without legal acceptance 403 legal.acceptance_required
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./cmd/api/ -run 'Reservation|Holds|VehicleDelete|Availability_NetsHolds|AuthzMatrix'` → FAIL.
- [ ] **Step 3: Implement** the code, the wiring, and the marker removal.
- [ ] **Step 4: Run** — `gox go test -race ./...`. Expected: `ok`, including the M2 gate tests now running against the real Holds.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add reservation reads and vehicle change, wire real holds and vehicle checks"
```

---

## Task S4.1: Payments domain, gateway port and the simulated adapter

Design slice **S4** (design §9.1, §9.2, §9.5, §12.5 backoff, M3B-6, M3B-7, M3B-9, M3B-11, M3B-29). **Sensitive (payments).** Budget: about 400 lines.

**Files:**
- Create: `internal/features/payments/{card.go,card_test.go,gateway.go,backoff.go,backoff_test.go,replay.go,replay_test.go}`, `internal/infrastructure/paymentsim/{paymentsim.go,paymentsim_test.go}`
- Modify: `queries/paymentsim.sql`, `dbgen/*`, `internal/infrastructure/validation/validation.go` (register `luhn`)

**Interfaces:**
- Consumes: S2.1 `sim_gateway_charges`; `validation.New()`; `errcodes`.
- Produces:
  - The gateway port exactly as design §9.1: `Gateway`, `ChargeRequest`, `ChargeResult`, `RefundRequest`, `RefundResult` and `ErrChargeUnknown`, plus `type ChargeOutcome int` with `ChargeSucceeded` and `ChargeDeclined`.
  - `type Card struct{ Number string; ExpMonth, ExpYear int; CVC string }`. Its `String()` and `LogValue()` return `card(****<last4>)`, and `GoString` does the same.
  - `type PayRequest struct{ CardNumber string \`json:"card_number" validate:"required,numeric,min=13,max=19,luhn"\`; ExpMonth int \`json:"exp_month" validate:"required,min=1,max=12"\`; ExpYear int \`json:"exp_year" validate:"required,min=2000,max=2099"\`; CVC string \`json:"cvc" validate:"required,numeric,min=3,max=4"\` }`
  - `func NormalizeCardNumber(raw string) string`: removes ASCII spaces and hyphens only (plan decision 6). It runs before validation.
  - `func Luhn(number string) bool`, `func Brand(number string) string` (design §9.2 BIN table), and `func ExpiryValid(month, year int, now time.Time) bool` (not before the current month in UTC−4).
  - `func Fingerprint(reservation uuid.UUID, brand, last4 string, expMonth, expYear int) []byte` = `sha256(reservation[:] ‖ brand ‖ "|" ‖ last4 ‖ "|" ‖ fmt.Sprintf("%02d%04d", expMonth, expYear))`. 32 bytes; the CVC and full PAN are excluded.
  - `func DecideReplay(stored, requested []byte) (replay bool, code errcodes.Code)`: equal → `(true, "")`, else `(false, IdempotencyKeyReused)`.
  - `const RefundMaxAttempts = 10` and `func RefundBackoff(attempts int) (wait time.Duration, failed bool)`: `attempts ≥ 10` → `(0, true)`, else `min(10s·2^(attempts−1), 30m)`.
  - **Adapter.** `paymentsim.New(pool *pgxpool.Pool, cfg Config) *Gateway` with `Config{Latency, TimeoutDelay time.Duration; Sleep func(ctx context.Context, d time.Duration) error; Rand io.Reader}`. A nil `Sleep` means a real timer that honours `ctx`. It implements `payments.Gateway` with the design §9.5 table: insert `on conflict (idempotency_key) do nothing` then re-read. `provider_ref` is `sim_ch_` plus 24 base32 characters (refunds use `sim_rf_`).

- [ ] **Step 1: Write the failing tests**

```go
func TestNormalizeCardNumber(t *testing.T) // Review Focus 2: "4242 4242 4242 4242" and "4242-4242-4242-4242" → "4242424242424242"; "4242.4242" unchanged (then fails numeric)
func TestLuhnAndBrand(t *testing.T) // 4242424242424242 visa ok; 4000000000000002 ok; 5555555555554444 mastercard; 2223003122003222 mastercard; 378282246310005 amex; 6011111111111117 other; 4242424242424241 Luhn false
func TestExpiryValid(t *testing.T) // now 2026-12-01T03:00Z (Nov 30 23:00 in SD): 11/2026 ok, 10/2026 not; now 2026-12-01T05:00Z: 11/2026 not, 12/2026 ok
func TestCardRequest_Validation(t *testing.T) // Review Focus 2: exp_year 28 → field exp_year rule min; exp_month 13 → max; cvc "12345" → max; card_number "abc" → numeric;
                                              // every FieldError.Param and message contains none of the input values
func TestCard_NeverPrintsPAN(t *testing.T) // fmt %v, %+v, %#v and slog JSON of Card{Number:"4242424242424242",CVC:"123"} contain "card(****4242)" and neither the PAN nor the CVC
func TestFingerprint(t *testing.T) // stable for equal inputs; changes with brand, last4, exp_month, exp_year and reservation; length 32
func TestDecideReplay(t *testing.T) // equal → (true,""); different → (false, IdempotencyKeyReused)
func TestRefundBackoff(t *testing.T) // attempts 1..9 → 10s,20s,40s,80s,160s,320s,640s,1280s,30m; 10 → failed
// paymentsim_test.go (real DB, Sleep records durations and returns ctx.Err() when ctx is done)
func TestSim_OutcomeTable(t *testing.T) // 4242… → Succeeded after Latency; 4000…0002 → Declined card_declined; 5555555555554444 → Declined card_not_supported; rows recorded with brand/last4 only
func TestSim_TimeoutCard(t *testing.T) // 4000000000009995 with a 50 ms ctx → returns context.DeadlineExceeded; row outcome succeeded, delayed true; Sleep asked for TimeoutDelay
func TestSim_RepeatAndRequery(t *testing.T) // second Charge same key (different card) → first result, same ProviderRef; Charge key-only → recorded result immediately; unknown key-only → ErrChargeUnknown
func TestSim_RefundOnce(t *testing.T) // Refund twice same key → same ProviderRef, one refund on the row; another refund key on the same charge → error; refund of a declined charge → error
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./internal/features/payments/ ./internal/infrastructure/paymentsim/` → FAIL.
- [ ] **Step 3: Implement** the code above.
- [ ] **Step 4: Run** — `gox go test -race -cover ./internal/features/payments/ ./internal/infrastructure/...`. Expected: `ok`, with coverage ≥ 90 % on `card.go`, `backoff.go` and `replay.go`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add payment gateway port, card rules and simulated gateway"
```

---

## Task S4.2: Pay endpoint, Apply, QR codec and `GET /qr`

Design slice **S4** (design §7.6, §7.9, §9.3, §9.4 steps 1–5, §11.2, M3B-3, M3B-7, M3B-8, M3B-10, M3B-13, M3B-21, M3B-32, M3B-38, M3B-50, M3B-56). **Sensitive (payments, QR signing).** Budget: about 400 lines (the 202 and reconcile tests move to S4.3).

**Files:**
- Create: `internal/features/reservations/{qr.go,qr_test.go,pay.go,apply.go}`, `internal/features/payments/{ports.go,fees.go,postgres/repo.go}`, `cmd/api/pay_test.go`, `internal/features/reservations/testdata/qr_golden.json`
- Modify: `queries/{payments,reservations}.sql`, `dbgen/*`, `internal/domain/apperr/apperr.go` (`KindPaymentRequired`), `internal/transport/http/httpx/httpx.go` (402), `ratelimit/rules.go`, `reservations/{service.go,http.go}`, `cmd/api/main.go`, `openapi.yaml` (remove the `S4.2` markers)

**Interfaces:**
- Consumes: S4.1 port and adapter; S3.x service; `notifications.Notifier`; config `QRHMACKey`, `PaymentChargeTimeout`.
- Produces:
  - `apperr.KindPaymentRequired` maps to HTTP 402, and `apperr.PaymentRequired(code errcodes.Code) *Error`.
  - **Rate limit.** `ratelimit.ReservationPayUser = Rule{Name: "reservation_pay_user", Limit: 10, Window: 10 * time.Minute, Key: KeyUser, Prefix: "rsv_pay:"}` (fails closed).
  - **QR codec.** `func EncodeQR(key []byte, publicID uuid.UUID, nonce [16]byte) string` and `func DecodeQR(key []byte, token string) (uuid.UUID, [16]byte, bool)`, as design §11.2: 86 characters, `hmac.Equal`, and anything else rejected.
  - **`payments.Fees`** (all on the ctx transaction):
    - `StartAttempt(ctx, in AttemptInput) (a Attempt, replayed bool, err error)`. `AttemptInput{ReservationID, UserID int64; ReservationPublicID uuid.UUID; AmountCents int64; ClientKey string; Fingerprint []byte; Brand, Last4 string; Now time.Time}`.
      - It generates `public_id` with `uuid.NewV7()` in Go, so `idempotency_key = "rsv-fee:" + public_id` is set in the same insert (resolved gap 4).
      - Its errors are `apperr` `409 idempotency.key_reused` and `409 payment.in_progress`. On `23505`, `payments_reservation_client_key` → re-read and replay, and `payments_reservation_live_key` → `payment.in_progress`.
    - `FeeByID(ctx, paymentID int64) (FeePayment, error)`: unlocked; `reservation_id` is immutable.
    - `Finalize(ctx, paymentID int64, res ChargeResult, now time.Time) (applied bool, err error)`: fenced on `status = 'pending'`.
    - `MarkNotSubmitted(ctx, paymentID int64, now time.Time) (bool, error)`
    - `LiveFee(ctx, reservationID int64) (FeePayment, bool, error)` and `LatestFee(ctx, reservationID int64) (FeePayment, bool, error)`
    - `QueueRefund(ctx, paymentID, amountCents int64, reason string, now time.Time) (Refund, error)`
  - `type Attempt struct{ ID int64; PublicID uuid.UUID; Status, FailureCode, GatewayKey string }` and `type FeePayment struct{ Attempt; ReservationID, AmountCents int64 }`.
  - `type OutcomeApplier interface{ Apply(ctx context.Context, paymentID int64, res ChargeResult) error }`, defined in `payments` and implemented by `*reservations.Service` (resolved gap 1).
  - `func (s *Service) Pay(ctx context.Context, userID int64, id uuid.UUID, key string, req payments.PayRequest) (PayResult, error)` with `PayResult{Code int /*200|202|402*/; Reservation View; PaymentID uuid.UUID; FailureCode string; Replayed bool}`. It runs tx 1, then `Gateway.Charge` under `context.WithTimeout(PaymentChargeTimeout)`, then tx 2 on `context.WithoutCancel` with its own 3 s deadline (M3B-56). Its errors are design §7.6.
  - `func (s *Service) Apply(ctx context.Context, paymentID int64, res payments.ChargeResult) error`: design §9.4 confirm, late and defensive branches in the §8.1 lock order. The confirm branch enqueues `reservation_confirmed` (S4.3 adds the template; until then `notifications.TplReservationConfirmed` is declared in this task) with `Data{"qr_token": …}` and dedupe `reservation_confirmed:<public_id>`.
  - `func (s *Service) QR(ctx context.Context, userID int64, id uuid.UUID) (QRView, error)`: `confirmed`/`checked_in` only, else `409 reservation.invalid_state`.
  - `reservations.Deps` gains `Fees payments.Fees`, `Gateway payments.Gateway`, `Notify notifications.Notifier`, `QRKey []byte`, `ChargeTimeout time.Duration` and `Rand io.Reader`.
  - `cmd/api` `deps` gains `gateway payments.Gateway` (nil → `paymentsim.New`).

- [ ] **Step 1: Write the failing tests**

```go
// qr_test.go
func TestQR_GoldenVector(t *testing.T) // key 0x00..0x1f, id 0190f0e0-0000-7000-8000-000000000001, nonce 0x10..0x1f → token == testdata value (computed once, then frozen); len 86
func TestQR_RoundTripAndTamper(t *testing.T) // round trip ok; flipping any single byte of the decoded 64 bytes → !ok; wrong key → !ok; lengths 85/87 → !ok; "=" padding → !ok; "+" or "/" → !ok; trailing "\n" → !ok
// cmd/api/pay_test.go (paymentsim with a recording Sleep; FakeClock)
func TestPay_Success(t *testing.T) // 4242… → 200 confirmed; payment succeeded with provider_ref, brand visa, last4 4242; qr_nonce set; outbox reservation_confirmed with qr_token; GET /qr → 200 Cache-Control no-store, token decodes to the reservation
func TestPay_ConfirmInsideWindowBumpsVersion(t *testing.T) // window contains now → lot_occupancy.version +1 and one lot_changed; window in the future → version unchanged
func TestPay_DeclineThenRetry(t *testing.T) // 4000…0002 → 402 payment.declined details {payment_id, failure_code card_declined}; status pending_payment; new key + 4242 → 200
func TestPay_ReplaySameKey(t *testing.T) // replay of the 200 → 200 + Idempotent-Replayed true; replay of the 402 → 402 same payment_id + header; still one gateway row per payment
func TestPay_SameKeyDifferentCard(t *testing.T) // same key, other last4 → 409 idempotency.key_reused
func TestPay_SameKeyParallel(t *testing.T) // REQUIRED: gateway double blocks Charge on a channel; 10 parallel pays, one key → exactly one Charge call, one payment row;
                                           // the 9 non-leaders answer 202 with the same payment id and Idempotent-Replayed true; release → leader 200; a later replay → 200 + header
func TestPay_DifferentKeysParallel(t *testing.T) // gateway blocked; 5 parallel pays with 5 keys → one 202-or-200 leader, four 409 payment.in_progress; one live fee payment
func TestPay_SameKeyDifferentReservations(t *testing.T) // Review Focus 3: two drivers' reservations (or one after cancel) with key "k-12345678" → both 200, two payment rows, two gateway rows
func TestPay_InvalidStateAndExpired(t *testing.T) // confirmed → 409 reservation.invalid_state details.status confirmed; now == payment_due_at → 409 reservation.payment_expired
func TestPay_CardJunkIs422WithoutEcho(t *testing.T) // Review Focus 2: spaced PAN → 200; exp_year 28 → 422 field exp_year; missing Idempotency-Key and "short" → 422 field Idempotency-Key; body never contains the PAN, CVC or key
func TestPay_LateSuccessExpiresAndRefunds(t *testing.T) // Apply(success) with now ≥ upper(window) on a pending_payment → expired_unpaid, refund_reason facility_unavailable, slots released, refund pending (full fee), reservation_cancelled enqueued, version unchanged
func TestPay_DefensiveSuccessRefunds(t *testing.T) // Apply(success) on a cancelled reservation's pending payment → payment succeeded + refund pending; reservation unchanged; error logged
func TestPay_OneLiveFeeByIndex(t *testing.T) // direct insert of a second pending fee for the reservation → 23505 (guard holds even if the service is bypassed)
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./internal/features/reservations/ ./cmd/api/ -run 'QR_|Pay_'` → FAIL.
- [ ] **Step 3: Implement** in this order: the codec, `Fees` and its repo, `Pay`/`Apply`, the handlers (`Idempotency-Key` read from the header, `Retry-After: 2` on 202, `Idempotent-Replayed: true` on replays), then the wiring and marker removal. The leader/non-leader behaviour follows from tx 1: a replay of a `pending` attempt is a `202`.
- [ ] **Step 4: Run** — `gox go test -race ./...`. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add reservation pay with idempotent attempts, apply rules and signed qr"
```

---

## Task S4.3: Confirmation email with the inline QR, `qr_token` scrub, and the payment reconcile job

Design slice **S4** (design §9.4 reconcile, §12.1, §12.6, M3B-10 rules 3–4, M3B-42, M3B-43, M3B-50). **Sensitive (dependency, payments).** Budget: about 380 lines.

**Files:**
- Create: `internal/features/notifications/{qrimage.go,qrimage_test.go,templates/{en,es-DO}/reservation_confirmed.{html,txt}}`, `internal/features/payments/{reconcile.go,reconcile_test.go}`, `cmd/api/pay_pending_test.go`
- Modify: `go.mod`/`go.sum` (`rsc.io/qr`), `internal/infrastructure/email/{smtp.go,smtp_test.go}`, `internal/features/notifications/{notifications.go,templates.go,dispatcher.go,dispatcher_test.go}`, `queries/outbox.sql`, `dbgen/*`, `cmd/worker/{main.go,main_test.go}`

**Interfaces:**
- Consumes: S4.2 `Fees`, `OutcomeApplier`; `paymentsim`; config `PaymentChargeTimeout`.
- Produces:
  - **Email.** `email.Message` gains `Inline []Inline` with `type Inline struct{ ContentID, ContentType string; Data []byte }`. When the list is non-empty, `SMTP.build` emits `multipart/related` (the alternative part plus each image with `Content-ID: <id>` and `Content-Disposition: inline`). This is resolved gap 3.
  - `notifications.TplReservationConfirmed` joins `allNames`. Its payload keys are those of design §12.6.
  - `func QRImage(token string) ([]byte, error)`: a PNG from `rsc.io/qr` at level M.
  - The dispatcher attaches `Inline{ContentID: "qr", ContentType: "image/png"}` when the template is `reservation_confirmed`. The HTML uses `cid:qr`.
  - `MarkOutboxSent` and `MarkOutboxDead` become `payload = payload - 'code' - 'qr_token'`. `ScrubPendingForUser` also removes `qr_token`.
  - **Reconcile job.** `payments.Reconciler{Tx database.TxRunnerPort; Fees Fees; Gateway Gateway; Outcomes OutcomeApplier; ChargeTimeout time.Duration; Log *slog.Logger}` with `func (r *Reconciler) Run(ctx context.Context, now time.Time) (int, error)`.
    - It selects up to 20 `pending` fee payments with `created_at < now − (ChargeTimeout + 10s)`.
    - Per row it calls `Charge` key-only under `ChargeTimeout`, then `Outcomes.Apply` in a transaction.
    - `ErrChargeUnknown` → `MarkNotSubmitted`. Any other error → skip until the next tick.
  - Worker job `payment_reconcile` runs every `15 * time.Second`. `cmd/worker` builds `paymentsim`, `payments` and `reservations` from `WorkerConfig` (QR key and timeouts from S2.2).

- [ ] **Step 1: Write the failing tests**

```go
func TestSMTPBuild_InlineImage(t *testing.T) // Message with one Inline → body has multipart/related, Content-ID: <qr>, Content-Type: image/png, base64 data; without Inline the M1 body is byte-identical to before
func TestQRImage(t *testing.T) // 86-char token → valid PNG (image.Decode ok), square; same token → same bytes
func TestDispatcher_ConfirmedEmailHasQRAndIsScrubbed(t *testing.T) // Mailpit: reservation_confirmed sent in es-DO and en with an inline image/png part; after send payload has no qr_token; MarkOutboxDead path also scrubs
func TestTemplates_ReservationConfirmedRenders(t *testing.T) // both locales render subject/text/html with arrival, window and plate; html contains "cid:qr"; the text body never contains the token
// cmd/api/pay_pending_test.go
func TestPay_TimeoutCardReturns202(t *testing.T) // 4000…9995 with ChargeTimeout 50ms → 202, Retry-After 2, {reservation pending_payment, payment {id, status pending}}; GET reservation latest_payment pending
func TestReconcile_ConfirmsTimeoutCard(t *testing.T) // after the 202, advance FakeClock 61 s, Reconciler.Run → 1; reservation confirmed, payment succeeded, email enqueued once; second Run → 0
func TestReconcile_SkipsYoungPending(t *testing.T) // pending created 5 s ago (ChargeTimeout 5s) → Run → 0 rows touched
func TestReconcile_NotSubmitted(t *testing.T) // pending fee payment with no sim_gateway_charges row → failed, failure_code not_submitted, failed_at == now; reservation still pending_payment
func TestReconcile_RaceWithPayTx2(t *testing.T) // Apply from the handler and from Reconciler on the same payment in parallel → payment finalized once, one confirmed transition, one email (fence on pending)
func TestReconcile_LateResolution(t *testing.T) // timeout card, then FakeClock past upper(window) before Run → expired_unpaid + refund pending (the late rule through the job)
func TestWorkerJobs_IncludeReconcile(t *testing.T) // workerJobs(...) has "payment_reconcile" every 15 s
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./internal/infrastructure/email/ ./internal/features/notifications/... ./internal/features/payments/... ./cmd/...` → FAIL.
- [ ] **Step 3: Implement.** Run `gox go get rsc.io/qr@latest`, then the code above. Copy for both locales lives in the template files. Add `reservations.Service` as the `OutcomeApplier` in the worker.
- [ ] **Step 4: Run** — `gox go test -race ./...`, then `gox sh -c 'go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./...'`. Expected: `ok`, and `No vulnerabilities found`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): send the confirmation email with an inline qr and reconcile pending charges"
```

---

## Task S5.1: Cancel, refund queueing, the alerts recorder and the cancellation email

Design slice **S5** (design §7.8, §10.1 cancel rows, §12.6, M3B-10 rule 2, M3B-13, M3B-30, M3B-34). **Sensitive (payments).** Budget: about 380 lines.

**Files:**
- Create: `internal/features/alerts/{alerts.go,postgres/recorder.go,postgres/recorder_test.go}`, `internal/features/reservations/cancel.go`, `internal/features/notifications/templates/{en,es-DO}/reservation_cancelled.{html,txt}`, `cmd/api/cancel_test.go`
- Modify: `queries/{reservations,payments,alerts}.sql`, `dbgen/*`, `payments/{fees.go,postgres/repo.go}`, `notifications/{notifications.go,templates.go}`, `reservations/{service.go,http.go}`, `cmd/api/main.go`, `openapi.yaml` (remove the `S5.1` marker)

**Interfaces:**
- Consumes: S4.2 `Fees.LiveFee`, `Fees.QueueRefund`, `Notifier`.
- Produces:
  - `type alerts.Kind string` with `KindReservationUnadmitted = "reservation_unadmitted"` and `KindRefundFailed = "refund_failed"`.
  - `type alerts.Recorder interface{ Open(ctx context.Context, lotID int64, kind Kind, details map[string]string, now time.Time) error }` (inside the ctx transaction, `database.MustTx`), with `alertspg.NewRecorder()`.
  - `payments.Fees.QueueRefund` inserts `refunds(status pending, attempts 0, next_attempt_at = now, idempotency_key = "refund:" + public_id)`, with `public_id` generated in Go as in S4.2.
  - `func (s *Service) Cancel(ctx context.Context, userID int64, id uuid.UUID) (View, error)`: exactly the design §7.8 table. It takes `lot_occupancy` first and bumps the version when the reservation is `confirmed` and its window contains `now`.
  - `notifications.TplReservationCancelled` with Data `reservation_id`, `refund_status` (`pending`/`none`), `refund_reason` and `fee_cents`; dedupe `reservation_cancelled:<public_id>`.
  - `reservations.Deps` gains `Alerts alerts.Recorder`.
  - `View.Refund` is filled from the latest refund of the reservation's fee payment.

- [ ] **Step 1: Write the failing tests**

```go
func TestAlerts_Open(t *testing.T) // inside a tx → row with kind, details jsonb, opened_at == now; outside a tx → database.ErrNoTx
func TestCancel_PendingWithoutAttempt(t *testing.T) // → 200 cancelled, cancelled_by driver, refund_reason null, refund null; slots released; AssertSlotInvariant
func TestCancel_PendingWithPendingAttempt(t *testing.T) // pending fee payment → 409 payment.in_progress; nothing changes
func TestCancel_ConfirmedBeforeArrival(t *testing.T) // → 200 refund_reason cancelled_before_arrival, refund {status pending, amount_cents == fee, reason cancelled_before_arrival}; reservation_cancelled refund_status pending; slots released
func TestCancel_ConfirmedAtArrival(t *testing.T) // now == arrival_at → 200 refund_reason none_after_arrival, no refund row, refund_status none
func TestCancel_InsideWindowBumpsVersion(t *testing.T) // window contains now → version +1, one lot_changed; before the window → version unchanged
func TestCancel_Idempotent(t *testing.T) // second cancel → 200 same body (minus request_id), one refund row, one email
func TestCancel_InvalidStates(t *testing.T) // checked_in, completed, expired_unpaid, expired_no_show → 409 reservation.invalid_state details.status
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./internal/features/alerts/... ./cmd/api/ -run 'Alerts|Cancel_'` → FAIL.
- [ ] **Step 3: Implement** the code, the template copy, the wiring and the marker removal.
- [ ] **Step 4: Run** — `gox go test -race ./...`. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add reservation cancel with refund queueing and incident alerts"
```

---

## Task S5.2: Refund processing job and the refund email

Design slice **S5** (design §10.3, §12.5, FR-PAY-5, M3B-29). **Sensitive (payments).** Budget: about 350 lines.

**Files:**
- Create: `internal/features/payments/{refunds.go,refunds_test.go}`, `internal/features/notifications/templates/{en,es-DO}/refund_succeeded.{html,txt}`, `cmd/api/refunds_test.go`
- Modify: `queries/payments.sql`, `dbgen/*`, `notifications/{notifications.go,templates.go}`, `reservations/cancel.go` (implements `RefundNotifier`), `cmd/worker/{main.go,main_test.go}`

**Interfaces:**
- Consumes: S5.1 refunds and alerts; S4.1 `RefundBackoff`, `Gateway.Refund`.
- Produces:
  - `type payments.RefundNotifier interface{ RefundSucceeded(ctx context.Context, r RefundDone) error }` with `RefundDone{RefundPublicID uuid.UUID; ReservationID int64; AmountCents int64}`. It is defined in `payments` and implemented by `*reservations.Service`, which enqueues `refund_succeeded` (dedupe `refund_succeeded:<refund public_id>`).
  - `payments.RefundProcessor{Tx database.TxRunnerPort; Repo Repo; Gateway Gateway; Alerts alerts.Recorder; Notice RefundNotifier; Log *slog.Logger}` with `func (p *RefundProcessor) Run(ctx context.Context, now time.Time) (int, error)`. It follows design §12.5 steps 1–4 exactly: claim (batch 20, `skip locked`, `attempts+1`, a 60 s lease), call `Refund` outside a transaction, then a success or failure transaction fenced on `(status = 'pending', attempts)`.
  - On success the payment goes `succeeded → refunded` with `refunded_at`. On attempt 10 it goes `failed` and an alert `refund_failed` is written with `details {reservation_id, refund_id, payment_id}` (public ids only).
  - `last_error` is truncated to 500 bytes and holds only the error class, never request data.
  - Worker job `refund_processing` runs every `10 * time.Second`.

- [ ] **Step 1: Write the failing tests**

```go
func TestRefunds_SuccessFlow(t *testing.T) // cancel before arrival → Run(now) → refund succeeded, provider_ref, succeeded_at == now; payment refunded, refunded_at; refund_succeeded enqueued once; GET reservation refund.status succeeded
func TestRefunds_BackoffSchedule(t *testing.T) // gateway double fails n times: after each Run, attempts and next_attempt_at == now + RefundBackoff(attempts); a Run before next_attempt_at claims nothing
func TestRefunds_TenFailuresAlert(t *testing.T) // fails 10 times (FakeClock advanced past each next_attempt_at) → status failed, failed_at; reconciliation_alerts refund_failed with public ids only; no email
func TestRefunds_ParallelRunnersOneCallPerAttempt(t *testing.T) // REQUIRED: 5 pending refunds, two Run in parallel → each refund Refund-called exactly once, each succeeded once, 5 emails
func TestRefunds_LeaseExpiryNeverDuplicates(t *testing.T) // REQUIRED: runner A claims (attempts 1) and its Refund blocks; FakeClock +61 s; runner B claims (attempts 2), Refund (same key → same ref), succeeds;
                                                         // A unblocks → its finalize matches 0 rows; exactly one succeeded row, one refunded payment, one email; sim row refunded once
func TestRefunds_NeverTwoLiveRefunds(t *testing.T) // cancel twice concurrently (same driver, two requests) → one refund row (unique partial index or idempotent cancel)
func TestWorkerJobs_IncludeRefunds(t *testing.T) // "refund_processing" every 10 s
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./internal/features/payments/... ./cmd/... -run 'Refunds_|WorkerJobs'` → FAIL.
- [ ] **Step 3: Implement** the processor, the notifier, the template and the worker wiring.
- [ ] **Step 4: Run** — `gox go test -race ./...`. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): process refunds with leased retries, backoff and failure alerts"
```

---

## Task S5.3: Receipts — `GET /v1/payments` and `/{id}`

Design slice **S5** (design §7.10, FR-PAY-8, M3B-27, M3B-33, M3B-37). Budget: about 250 lines.

**Files:**
- Create: `internal/features/payments/{receipts.go,http.go}`, `cmd/api/receipts_test.go`
- Modify: `queries/payments.sql`, `dbgen/*`, `cmd/api/{main.go,authz_matrix_test.go}`, `openapi.yaml` (remove the `S5.3` markers)

**Interfaces:**
- Consumes: S4.2 and S5.1 rows.
- Produces:
  - `func (s *ReceiptService) List(ctx context.Context, userID int64, limit int, cursor string) ([]Receipt, string, error)`: keyset on `(created_at desc, id desc)`.
  - `func (s *ReceiptService) Get(ctx context.Context, userID int64, id uuid.UUID) (Receipt, error)`.
  - `Receipt` is the design §7.10 item. `receipt_no` = `MP-` + `lpad(id, 8)` only for `succeeded`/`refunded`. `refund` is `{id, status, amount_cents, reason, succeeded_at}` or `null`.
  - HTTP routes `GET /v1/payments` and `GET /v1/payments/{id}` (`Roles: driver`).

- [ ] **Step 1: Write the failing tests**

```go
func TestReceipts_ListAndPaging(t *testing.T) // a failed attempt, a succeeded fee, a refunded fee (direct SQL + flows) → 3 items newest first; limit=2 → cursor → 1; failed item receipt_no null, failure_code card_declined
func TestReceipts_RefundEmbedded(t *testing.T) // refunded fee → status refunded, receipt_no "MP-000000NN", refund.status succeeded
func TestReceipts_OwnershipOnly(t *testing.T) // another driver's payments never listed; M2 drive-up cash rows (user_id null) never listed
func TestPayments_GarbageIDsAre404(t *testing.T) // Review Focus 5: "abc", random uuid, another driver's id → identical 404 payment.not_found; bad cursor → 422
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./cmd/api/ -run 'Receipts_|Payments_'` → FAIL.
- [ ] **Step 3: Implement** the code, the wiring, the authz matrix rows and the marker removal.
- [ ] **Step 4: Run** — `gox go test -race ./...` and `gox go test ./cmd/api/ -run OpenAPI`. Expected: `ok`. Every M3 marker is now gone, while the allowlist stays until S9.1.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add driver payment receipts"
```

---

## Task S6.1: QR at the entry barrier — decision, check-in, physically full, re-present

Design slice **S6** (design §11.1, §11.5, M3B-22 to M3B-24, FR-GATE-2, FR-RES-8/9). **Sensitive (device flows, payments).** Budget: about 400 lines.

**Files:**
- Create: `internal/features/reservations/{qrdecide.go,qrdecide_test.go,gateaccess.go}`, `internal/features/gate/entry_qr.go`, `cmd/api/gate_qr_test.go`
- Modify: `internal/features/gate/{ports.go,entry.go,entry_test.go,service.go,postgres/repo.go}`, `queries/{gate,reservations}.sql`, `dbgen/*`, `cmd/api/main.go`

**Interfaces:**
- Consumes: the S4.2 codec, S5.1 alerts, `payments.Fees`, the gate `LockLotOccupancy`/`CountOutOfService`/`CountFreshPending`, and `testsupport.FillLot`.
- Produces:
  - **Pure decision.** `func DecideQR(in QRInput) QROutcome` in `reservations` with `QRInput{Status Status; Window Window; Now time.Time; Occupied, Pending, EffectiveCapacity int; StayStatus string /* "" when none */}`. `QROutcome` is one of `QRCheckIn`, `QRReopen`, `QRRefresh`, `QRPhysicallyFull`, `QRDeny`, with `Reason errcodes.Code`. It implements the design §11.1 table: `QRReopen` is `confirmed` with a `voided` stay (S6.2 relies on it), and `QRRefresh` is `checked_in` with an `entry_pending` stay inside the window.
  - **Gate port** in `gate/ports.go`. `gate` defines it and `reservations.GateAccess` implements it:

```go
type ReservationRef struct{ ID int64; PublicID uuid.UUID }
type ReservationEntry struct {
	ID, UserID, TariffVersionID int64
	PublicID                    uuid.UUID
	Status                      string
	WindowStart, WindowEnd      time.Time
}
type Reservations interface {
	// Resolve verifies the token and finds the reservation in the lot, without locks. Bad token, other lot, unknown or nonce mismatch → ErrNotFound.
	Resolve(ctx context.Context, lotID int64, token string) (ReservationRef, error)
	// LockForEntry locks the reservation FOR UPDATE (after LockLotOccupancy) and re-checks the nonce.
	LockForEntry(ctx context.Context, lotID int64, ref ReservationRef) (ReservationEntry, error)
	CheckIn(ctx context.Context, r ReservationEntry, now time.Time) error          // confirmed → checked_in, slots −1
	CancelUnadmitted(ctx context.Context, r ReservationEntry, now time.Time) error // design §11.5 steps 1–5
	RestoreAfterVoid(ctx context.Context, reservationID int64, now time.Time) (bool, error) // S6.2
	LateEntryPass(ctx context.Context, reservationID int64, now time.Time) (bool, error)    // S6.2
	Complete(ctx context.Context, reservationID int64, now time.Time) error                // S6.3; no-op unless checked_in
}
```

  - **Gate repo additions.**
    - `NewStay.ReservationID int64` (the stay is inserted with `credential_kind 'reservation'`).
    - `ReservationStayForUpdate(ctx, lotID, reservationID int64) (Stay, error)`
    - `ReopenReservationStay(ctx, lotID, stayID int64, now time.Time) error` (`voided → entry_pending`, `accepted_at = now`, `voided_at` kept)
    - `RefreshAccepted(ctx, lotID, stayID int64, now time.Time) error`
    - `Stay` gains `ReservationID, ReservationUserID int64`.
  - `gate.Deps` gains `Reservations Reservations`, and `gate.CanTransition` allows `voided → entry_pending`. The `entry.go` QR branch calls `s.qrEntry` (in `entry_qr.go`) in the design §11.1 lock order. `effective_holds` is never read on this path.

- [ ] **Step 1: Write the failing tests**

```go
// qrdecide_test.go
func TestDecideQR_Table(t *testing.T) // every row of design §11.1: confirmed before lower → Deny reservation.too_early; at upper → Deny reservation.window_closed; inside & occ+pend ≥ eff → PhysicallyFull;
                                      // inside & room & no stay → CheckIn; inside & voided stay → Reopen; checked_in & entry_pending inside → Refresh; checked_in & active/paid → Deny gate.credential_in_use;
                                      // expired_no_show → Deny window_closed; pending_payment, cancelled, completed, expired_unpaid → Deny gate.credential_invalid
// cmd/api/gate_qr_test.go (entry_barrier and dispenser TestDevices; reservation confirmed via the pay flow)
func TestGateQR_Guarantee(t *testing.T) // occupied 279, one confirmed hold effective → dispenser ticket_request deny gate.lot_full; GET availability available 0; QR → open {stay_id}; reservation checked_in, slots released; version +1
func TestGateQR_PhysicallyFull(t *testing.T) // occupied 280 → QR → deny gate.lot_physically_full; in the same tx: cancelled system facility_unavailable, slots released, refund pending, alert reservation_unadmitted {reservation_id, refund_id}, reservation_cancelled enqueued, version +1;
                                             // replay of the event → same deny, nothing new; RefundProcessor.Run → refund succeeded
func TestGateQR_PendingEntriesCountForFull(t *testing.T) // occupied 279 + one fresh entry_pending → QR → physically full (M3B-23)
func TestGateQR_TooEarlyAndWindowClosed(t *testing.T) // now < lower → deny reservation.too_early; now == upper → deny reservation.window_closed; expired_no_show → window_closed
func TestGateQR_InvalidTokens(t *testing.T) // forged MAC, other lot's reservation, nonce mismatch (nonce rotated in SQL), cancelled, pending_payment → deny gate.credential_invalid; no lot lock (answers within 1 s while a side tx holds lot_occupancy)
func TestGateQR_WhitespaceTokenInvalid(t *testing.T) // Review Focus 5: token + "\n" and " " + token → deny gate.credential_invalid, never 500; device_events payload holds sha256:<12 hex>, not the token
func TestGateQR_RePresentWhilePending(t *testing.T) // QR → open; QR again 20 s later → open same stay_id, accepted_at refreshed; one stay row
func TestGateQR_CarInsideInUse(t *testing.T) // check in, entry pass → QR → deny gate.credential_in_use
func TestGateQR_CancelRacesCheckIn(t *testing.T) // Review Focus 4: inside the window, POST cancel and the QR event in parallel (20 rounds) → per round exactly one of {cancelled+refund+deny credential_invalid} or {checked_in+409 invalid_state}; no 500; AssertSlotInvariant, AssertOccupancyInvariant
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./internal/features/reservations/ ./cmd/api/ -run 'DecideQR|GateQR_'` → FAIL.
- [ ] **Step 3: Implement** `DecideQR`, `GateAccess` (built from the reservations service with `Fees`, `Alerts`, `Notify` and `QRKey`), the gate port, the repo methods, `qrEntry`, and the wiring of `gate.Deps.Reservations`. `RestoreAfterVoid`, `LateEntryPass` and `Complete` return `errors.ErrUnsupported` until S6.2 and S6.3.
- [ ] **Step 4: Run** — `gox go test -race ./...`. Expected: `ok`, and M2 `TestEntry_ReservationQRDenied` is replaced by the S6.1 tests (delete it).
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): honour reservation qr at the entry barrier with the physically-full cancel"
```

---

## Task S6.2: Stranded check-in — void restore, stay reuse, late entry pass

Design slice **S6** (design §8.1 void and late-pass rows, §10.4, §12.7, **M3B-60 user decision**, M3B-24). **Sensitive (concurrency).** Budget: about 380 lines.

**Files:**
- Create: `cmd/api/gate_qr_void_test.go`
- Modify: `internal/features/gate/{void.go,void_test.go,pass.go,ports.go,postgres/repo.go}`, `internal/features/reservations/gateaccess.go`, `queries/{gate,reservations}.sql`, `dbgen/*`, `cmd/worker/{main.go,main_test.go}`, `cmd/api/scenario_test.go` (the pacer runs both void functions)

**Interfaces:**
- Consumes: S6.1 port and repo.
- Produces:
  - The `VoidUnpassed` SQL gains `and credential_kind <> 'reservation'`. `VoidUnpassedEntries` keeps its signature.
  - `gate.ReservationVoider{Tx database.TxRunnerPort; Repo Repo; Reservations Reservations; Log *slog.Logger}` with `func (v *ReservationVoider) Run(ctx context.Context, db database.DBTX, now time.Time) (int64, error)`. Step 1 is an unlocked candidate select (`ReservationStaysToVoid :many`, limit 200). Then one transaction per stay, in design §12.7 order:
    1. `LockLotOccupancy`.
    2. `Reservations.RestoreAfterVoid`, which locks the reservation, and for `checked_in` ensures and locks the slots and does `+1` with **no** cap check, then `checked_in → confirmed` and `checked_in_at = null`.
    3. `StayForUpdateByID` and a re-check of `entry_pending` and `accepted_at < now − 60 s`. On failure it rolls back with `errSkipVoid`.
    4. `VoidStay`.
    5. `BumpOccupancy(delta 0)` + `NotifyLotChanged`.
  - `GateAccess.RestoreAfterVoid(ctx, reservationID, now) (restored bool, err error)`.
  - `GateAccess.LateEntryPass(ctx, reservationID, now) (checkedIn bool, err error)`: `confirmed` → `checked_in`, `checked_in_at = now`, slots −1. Any other status → `(false, nil)`.
  - `pass.go` `lateEntryPass`: when `stay.ReservationID != 0`, after the stay lock it calls `Reservations.LateEntryPass` (lock order `lot_occupancy → stays → reservations → slot_loads`, design §8.1).
  - Worker job `void_unpassed_entries` runs `VoidUnpassedEntries` and then `ReservationVoider.Run` in the same tick.

- [ ] **Step 1: Write the failing tests**

```go
func TestVoidUnpassed_SkipsReservationStays(t *testing.T) // stale ticket stay + stale reservation stay → VoidUnpassedEntries voids only the ticket stay
func TestGateQR_StrandedCheckIn(t *testing.T) { // REQUIRED, subtests on the FakeClock:
	// "void restores the hold": QR open, no pass, +61 s, voider.Run → stay voided, reservation confirmed, checked_in_at null, slots +1, version +1 and one lot_changed; AssertSlotInvariant
	// "restore above cap": slots held at 84 when the void runs → held 85, no error (no cap check, M3B-60)
	// "re-present reuses the voided stay": QR again inside the window → open with the SAME stay_id, stay entry_pending, voided_at kept, reservation checked_in, one stay row
	// "re-present while physically full": after the void fill to 280 → QR → deny gate.lot_physically_full, cancelled facility_unavailable, refund pending
	// "never returns": after the void advance past upper → no-show job (S7.1; here: direct SQL check that status stays confirmed until then)
	// "void after the window": QR at upper−30 s, void at upper+31 s → reservation confirmed again; window already closed
}
func TestGateQR_VoidRacesRePresent(t *testing.T) // REQUIRED: stale entry_pending; voider.Run and a QR re-present in parallel (20 rounds) → either refreshed (void re-check rolls back, stay entry_pending, reservation checked_in) or voided then reopened; never two stays; invariants hold
func TestGateQR_LateEntryPassAfterVoid(t *testing.T) // REQUIRED: void, then barrier.passed entry for the voided stay → ack, stay active, reservation checked_in, slots −1, occupancy +1; AssertSlotInvariant
func TestGateQR_LateEntryPassAfterCancel(t *testing.T) // void, driver cancels (refund), late pass → stay active, reservation stays cancelled, slots unchanged; car billed normally
func TestGateQR_VoidRacesLateEntryPass(t *testing.T) // REQUIRED: stale entry_pending; voider.Run and the entry pass in parallel (20 rounds) → exactly one wins per round; final state either (active + checked_in) or (voided + confirmed);
                                                     // when the pass wins after a void, checked_in again; slot and occupancy invariants hold; then the exit pass completes the reservation (after S6.3; here: stay exitable)
func TestCanTransition_VoidedToEntryPending(t *testing.T) // updated full table: voided→entry_pending now allowed
func TestWorkerJobs_VoidRunsBoth(t *testing.T) // the void job body voids one ticket stay and one reservation stay in one tick
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./internal/features/gate/... ./cmd/... -run 'Void|Stranded|LateEntry|CanTransition'` → FAIL.
- [ ] **Step 3: Implement** the voider, the adapter methods, the late-pass branch, the worker wiring and the scenario pacer change.
- [ ] **Step 4: Run** — `gox go test -race ./...`. Expected: `ok`, and the M2 `TestScenario_*` still pass.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): restore the hold of a stranded check-in and reuse its stay"
```

---

## Task S6.3: Pay station, exit and completion with the QR

Design slice **S6** (design §11.3, §11.4, M3B-25, M3B-26, M3B-27, FR-GATE-5/6). **Sensitive (payments).** Budget: about 330 lines.

**Files:**
- Modify: `internal/features/gate/{paystation.go,exit.go,postgres/repo.go}`, `internal/features/reservations/gateaccess.go`, `queries/gate.sql`, `dbgen/*`, `cmd/api/gate_qr_test.go`

**Interfaces:**
- Consumes: the S6.1 `Resolve` and `ReservationStayForUpdate`.
- Produces:
  - The `stayByCredential` and `exitStay` `reservation_qr` branches call `Reservations.Resolve`, then `repo.ReservationStayForUpdate`. A non-inside stay → the M2 errors.
  - `NewPayment.UserID int64` is set from `Stay.ReservationUserID` for cash and zero payments of reservation stays (M3B-27).
  - `exit.go` exit pass: when `stay.ReservationID != 0` it calls `Reservations.Complete(ctx, id, now)` after `SetStayExited`, in the same transaction.
  - `GateAccess.Complete`: `checked_in → completed`, `completed_at = now`. Other states → no-op.
  - Billing reads `stays.entered_at` and the stay's `tariff_version_id`, which S6.1 sets to the reservation's version (M3B-25).

- [ ] **Step 1: Write the failing tests**

```go
func TestGateQR_PayStationAndExit(t *testing.T) // check in, pass, +2h10m → paystation.scan with QR → amount due from entered_at with the reservation's tariff version (not a newer one inserted after booking);
                                                // cash_paid → payment user_id == driver, receipt in GET /v1/payments; exit.credential_presented QR → open; barrier.passed exit → stay exited, reservation completed, completed_at == now
func TestGateQR_ZeroFeeExitSetsUser(t *testing.T) // exit within grace → zero payment with user_id set
func TestGateQR_FeeNotCredited(t *testing.T) // amount due is the full hourly fee; reservation fee not subtracted (FR-PAY-7)
func TestGateQR_LateEntryThenExitCompletes(t *testing.T) // REQUIRED: void → late entry pass → pay → exit pass → reservation completed; slot and occupancy invariants
func TestGateQR_ExitAfterCancelledLatePass(t *testing.T) // late pass of a cancelled reservation's stay, exit → stay exited, reservation stays cancelled (Complete no-op)
func TestGateQR_PayStationUnknownToken(t *testing.T) // forged QR at the pay station and exit → the M2 unknown-credential answers, never 500
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./cmd/api/ -run 'GateQR_'` → FAIL.
- [ ] **Step 3: Implement** the branches and `Complete`.
- [ ] **Step 4: Run** — `gox go test -race ./...`. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): accept the reservation qr at the pay station and exit and complete the stay"
```

---

## Task S7.1: Unpaid expiry, no-show expiry and the no-show block

Design slice **S7** (design §12 expiry rows, §12.2, FR-RES-17/18, M3B-10 rule 1, M3B-40, M3B-58). Budget: about 350 lines.

**Files:**
- Create: `internal/features/reservations/{jobs.go,jobs_test.go}`, `internal/features/notifications/templates/{en,es-DO}/reservation_no_show.{html,txt}`, `cmd/api/jobs_test.go`
- Modify: `queries/reservations.sql`, `dbgen/*`, `notifications/{notifications.go,templates.go}`, `cmd/worker/{main.go,main_test.go}`, `cmd/api/gate_qr_void_test.go` ("never returns" subtest)

**Interfaces:**
- Consumes: S3.x slots, `payments.Fees.LiveFee`, `Notifier`.
- Produces:
  - `type Jobs struct{…}` with `func NewJobs(d Deps) *Jobs`.
  - `func (j *Jobs) ExpireUnpaid(ctx context.Context, now time.Time) (int, error)`: claim (batch 200, `skip locked`, repeat while full), then a per-row re-check after the lock that no fee payment is `pending`. Rows with one are skipped. Otherwise `expired_unpaid`, `expired_at = now` and slots −1.
  - `func (j *Jobs) ExpireNoShows(ctx context.Context, now time.Time) (int, error)`: `confirmed and upper(hold_window) <= now` (batch 200) → `expired_no_show`, slots −1, and the design §12.2 block using the reservation's **captured** policy version. It enqueues `reservation_no_show` with `blocked_until` when set.
  - Worker jobs `expire_unpaid_reservations` and `expire_no_shows` run every `30 * time.Second`.
  - `cmd/worker` `maxConns` stays `8`: M3B-51's target is already in place (resolved gap 8).

- [ ] **Step 1: Write the failing tests**

```go
func TestExpireUnpaid(t *testing.T) // pending with payment_due_at == now → expired_unpaid, expired_at == now, slots released; due_at now+1s → untouched; no version bump
func TestExpireUnpaid_SkipsPendingAttempt(t *testing.T) // pending fee payment exists → untouched; after it fails → next Run expires it
func TestExpireUnpaid_RaceWithPay(t *testing.T) // StartAttempt committed between the claim's snapshot and the lock (hook) → row skipped
func TestExpireNoShows_AtUpperExactly(t *testing.T) // now == upper → expired_no_show; upper − 1s → untouched; slots released; reservation_no_show enqueued
func TestNoShow_BlocksAfterThreshold(t *testing.T) // REQUIRED (G6): two prior expired_no_show within 30 days + the third → users.reservation_blocked_until == now + 7d; create → 409 reservation.blocked_no_shows details.blocked_until; email carries blocked_until
func TestNoShow_WindowAndGreatest(t *testing.T) // a no-show 31 days ago does not count; an existing later block is kept (greatest)
func TestNoShow_CapturedPolicy(t *testing.T) // newer policy version (threshold 2) inserted after booking → the expiry uses the captured threshold 3
func TestJobs_Batches(t *testing.T) // 450 due pending → ExpireUnpaid returns 450
func TestWorkerJobs_IncludeExpiry(t *testing.T) // "expire_unpaid_reservations" and "expire_no_shows" every 30 s; maxConns == 8
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./internal/features/reservations/ ./cmd/... -run 'Expire|NoShow|Jobs_|WorkerJobs'` → FAIL.
- [ ] **Step 3: Implement** the jobs, the template and the wiring.
- [ ] **Step 4: Run** — `gox go test -race ./...`. Expected: `ok`. The S6.2 "never returns" subtest now asserts `expired_no_show` through `ExpireNoShows`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): expire unpaid and no-show reservations and block repeat no-shows"
```

---

## Task S7.2: Hold boundary notify, reminders, and hold changes over SSE

Design slice **S7** (design §8.2, §12.3, §12.4, G7, M3B-13, M3B-39, M3B-41). Budget: about 300 lines.

**Files:**
- Create: `internal/features/notifications/templates/{en,es-DO}/reservation_reminder.{html,txt}`, `cmd/api/holds_sse_test.go`
- Modify: `internal/features/reservations/{jobs.go,jobs_test.go}`, `queries/reservations.sql`, `dbgen/*`, `notifications/{notifications.go,templates.go}`, `cmd/worker/{main.go,main_test.go}`

**Interfaces:**
- Consumes: `Holds.EffectiveHolds`, `dbgen.BumpOccupancy`/`NotifyLotChanged`, `testsupport.ReadSSE`/`NextSSE`.
- Produces:
  - `type HoldBoundary struct{ last map[int64]int; started bool; … }` with `func (b *HoldBoundary) Tick(ctx context.Context, now time.Time) (bumped int, err error)`. It works per lot: on the first tick, or when the value differs from the last notified one, it bumps (delta 0) and NOTIFYs in a transaction that locks only `lot_occupancy`.
  - `func (j *Jobs) Reminders(ctx context.Context, now time.Time) (int, error)`: the design §12.4 query, enqueuing `reservation_reminder` with dedupe `reservation_reminder:<public_id>`.
  - Worker jobs `hold_boundary_notify` (every `30 * time.Second`) and `reservation_reminders` (every `time.Minute`).

- [ ] **Step 1: Write the failing tests**

```go
func TestHoldBoundaryJob(t *testing.T) // REQUIRED (G7): first Tick → 1 bump; no change → 0; FakeClock crosses lower of a confirmed window → 1 bump and one lot_changed; crosses upper → 1 bump; one window ending as another starts → 0
func TestHolds_ConfirmToSSE(t *testing.T) // REQUIRED (G7): SSE client on /v1/lots/{id}/availability/stream; pay confirm inside the window → next availability event has a newer version and effective_holds +1 within 2 s
func TestReminders(t *testing.T) // confirmed at T, arrival T+2h: Run at arrival−31m → 0; at arrival−30m → 1; again → 0 (dedupe); confirmed 20 min before arrival → never
func TestWorkerJobs_IncludeBoundaryAndReminders(t *testing.T) // "hold_boundary_notify" 30 s, "reservation_reminders" 1 min
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./internal/features/reservations/ ./cmd/... -run 'HoldBoundary|Holds_ConfirmToSSE|Reminders|WorkerJobs'` → FAIL.
- [ ] **Step 3: Implement** the code, the template and the wiring.
- [ ] **Step 4: Run** — `gox go test -race ./...`. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): notify hold boundary changes and send reservation reminders"
```

---

## Task S8.1: Simulated drivers — seed, derived passwords, driver client

Design slice **S8** (design §13.1, **M3B-44 user decision**, M3B-59). **Sensitive (derived passwords).** Budget: about 380 lines.

**Files:**
- Create: `internal/devicefleet/{drivers.go,drivers_test.go}`, `internal/features/auth/seed_driver.go`, `internal/features/vehicles/seed.go`, `internal/simulator/{driver.go,driver_test.go,drivers_pool.go,drivers_pool_test.go}`
- Modify: `internal/config/{seed.go,config_seed_test.go}`, `cmd/seed/{main.go,main_test.go}`, `deploy/compose/docker-compose.yml` (seed: `SEED_SIMULATOR_DRIVERS: ${SEED_SIMULATOR_DRIVERS:-true}`)

**Interfaces:**
- Consumes: `SIMULATOR_FLEET_SECRET` (`config.SeedFleet.FleetSecret`, `SimulatorConfig.FleetSecret`), the M1 auth and legal seeds, and the vehicles repo.
- Produces:
  - `const devicefleet.SimDriverCount = 20` and `func SimDriverEmail(n int) string` (`sim-driver-%02d@mypark.test`).
  - `func SimDriverPassword(fleet secret.Bytes, email string) secret.String` = base64url-nopad(`HMAC-SHA256(fleet, "mypark/driver-password/v1/" + email)`).
  - `func SimDriverPlate(n int) string` (`SIM-%03d`).
  - `config.LoadSeedDrivers(getenv) (SeedDrivers, error)` with `Enabled() bool` (`SEED_SIMULATOR_DRIVERS`) and `FleetSecret() secret.Bytes`. The production dev-secret refusal is shared with `SeedFleet`.
  - `auth.SeedDriver(ctx, SeedDeps, SeedDriverInput) (created bool, err error)`: a verified driver with the current legal versions accepted. It is idempotent on email.
  - `vehicles.SeedVehicle(ctx, ownerID int64, plate, make, model, color string, now time.Time) error`: it inserts only when the owner has no live vehicle.
  - `simulator.DriverClient` with `NewDriverClient(c DriverClientConfig) *DriverClient`. `DriverClientConfig{BaseURL string; HTTP *http.Client; Fleet secret.Bytes; Clock …}`. Methods:
    - `Login(ctx, n int) error` (`client_kind: mobile`; a refresh on 401 runs single-flight)
    - `Quote(ctx, n int, arrival time.Time) (Quote, error)`
    - `Create(ctx, n int, arrival time.Time, vehicleID uuid.UUID) (Reservation, error)`
    - `Pay(ctx, n int, reservationID uuid.UUID, key string) (string /*status*/, error)` (card `4242424242424242`)
    - `QR(ctx, n int, reservationID uuid.UUID) (string, error)`
    - `Vehicle(ctx, n int) (uuid.UUID, error)`
  - Tokens live in memory.
  - `simulator.DriverPool{Range [2]int}` with `Next(skip func(n int) bool) (int, bool)`: round-robin within the pool. `PoolArrivals = 1..16` and `PoolNoShow = 17..20` (M3B-59).

- [ ] **Step 1: Write the failing tests**

```go
func TestSimDriverPassword(t *testing.T) // fixed secret + "sim-driver-01@mypark.test" → frozen golden value; 43 chars base64url; different email → different value
func TestLoadSeedDrivers(t *testing.T) // unset → disabled; "true" → enabled; production with the dev fleet secret → error
func TestSeed_SimDriversIdempotent(t *testing.T) // run seed twice with SEED_SIMULATOR_DRIVERS=true → 20 users, 20 vehicles, legal accepted; each can POST /v1/auth/login with the derived password
func TestDriverClient_Flow(t *testing.T) // against newHandler: login → quote → create → pay → QR (86 chars); a 401 mid-flow refreshes once (two parallel calls → one refresh)
func TestDriverClient_NeverLogsSecrets(t *testing.T) // captured simulator logs and errors contain neither the password nor the access/refresh tokens nor the QR token
func TestDriverPool_RoundRobinSkips(t *testing.T) // pool 1..16: Next cycles 1,2,…; skip(blocked or busy) skipped; all skipped → false; pool 17..20 never yields 1..16
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./internal/devicefleet/ ./internal/config/ ./internal/simulator/ ./cmd/seed/` → FAIL.
- [ ] **Step 3: Implement** the code and the compose default.
- [ ] **Step 4: Run** — `gox go test -race ./...`. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server deploy/compose/docker-compose.yml
git commit -m "feat(server): seed simulated drivers and add the simulator driver client"
```

---

## Task S8.2: Scenarios `reservation_arrivals` and `no_show`, QR cars, control API

Design slice **S8** (design §13.2, §13.3, M3B-45, M3B-46, M3B-58). Budget: about 380 lines.

**Files:**
- Create: `internal/simulator/{reservations.go,reservations_test.go}`
- Modify: `internal/simulator/{scenarios.go,scenarios_test.go,car.go,car_test.go,control.go,control_test.go,runner.go,eventlog.go}`, `cmd/simulator/{main.go,openapi_test.go}`

**Interfaces:**
- Consumes: S8.1 `DriverClient` and `DriverPool`; the M2 `FleetSim`, `Engine` and `Runner`.
- Produces:
  - `CredentialKind` gains `CredReservationQR = "reservation_qr"`. `Car` gains `QR string` (never logged; `EventLog` stores `sha256:<12 hex>`). `FleetSim` presents it at the entry barrier, pay station and exit.
  - `ArrivalsPerHour` gains the reserved-car rates: `ReservedPerHour(ScenarioReservationArrivals) == 8` and `ReservedPerHour(ScenarioNoShow) == 4`. Drive-up traffic follows weekday for both.
  - `simulator.Reserver` with `func (r *Reserver) Book(ctx, s Scenario) (car *Car, ok bool)`. It runs quote → create with `arrival_at = Ceil5(server now)` → pay → QR for `reservation_arrivals`, and stops after pay for `no_show`. A driver answering `reservation.blocked_no_shows` or `active_exists` is marked and skipped.
  - RNG draws stay in the engine goroutine. Server values never feed the RNG (FR-SIM-7).
  - `Runner.Start` for these two scenarios checks `DriverClient.Login` on the pool's first driver. A `401` gives `422 simulator.scenario_unavailable`.
  - Control API `injectRequest`: `credential` `oneof=ticket employee_card reservation_qr`, and `QR *string` with `excluded_unless` semantics as `card`, matching `^[A-Za-z0-9_-]{86}$`.

- [ ] **Step 1: Write the failing tests**

```go
func TestCarLifecycle_ReservationQR(t *testing.T) // fake device client trace: entry credential reservation_qr → barrier entry → paystation scan (QR) → cash → exit credential (QR) → barrier exit; log entries show sha256:<12 hex>
func TestReserver_BooksAndPays(t *testing.T) // fake driver API: quote→create(arrival ceil5)→pay→qr for arrivals; no_show stops after pay; blocked driver skipped and marked
func TestScenarios_ReservedRates(t *testing.T) // ReservedPerHour: arrivals 8, no_show 4, weekday 0
func TestControl_InjectReservationQR(t *testing.T) // credential reservation_qr with a valid qr → 202 car_id; missing qr → 422 field qr required; qr with ticket → 422 excluded_unless; qr with "\n" suffix → 422 (Review Focus 5); events never show the token
func TestRunner_ScenarioUnavailableWithoutDrivers(t *testing.T) // login 401 → start reservation_arrivals → 422 simulator.scenario_unavailable; weekday still starts
func TestSimulatorOpenAPI_InjectCarParity(t *testing.T) // the control schema in openapi.yaml matches injectRequest (credential enum, qr pattern)
```

- [ ] **Step 2: Run to verify they fail** — `gox go test -race ./internal/simulator/ ./cmd/simulator/` → FAIL.
- [ ] **Step 3: Implement** the code.
- [ ] **Step 4: Run** — `gox go test -race ./...`. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add reservation arrival and no-show scenarios and qr cars"
```

---

## Task S8.3: Scenario tests in CI — guarantee, arrivals, no-show, determinism

Design slice **S8** (design §15.5, G2, FR-SIM-7, M2C-14). Budget: about 330 lines.

**Files:**
- Create: `apps/server/cmd/api/scenario_m3_test.go`
- Modify: `apps/server/cmd/api/scenario_test.go` (the rig seeds simulated drivers, wires a `DriverClient` over the handler transport, and runs the M3 job bodies on the shared FakeClock: reconcile, refunds, expiry, no-show, boundary and both void functions), `.github/workflows/ci-server.yml` (an M3 named-test step mirroring the M2 one)

**Interfaces:**
- Consumes: `newScenarioRig`, `pacer`, `traceTap`, the S8.2 scenarios, and the S7 jobs.
- Produces: the four tests below. The CI step requires each to report `--- PASS`.

- [ ] **Step 1: Write the tests**

```go
func TestScenario_Guarantee(t *testing.T) // REQUIRED (G2): fill to 279; a simulated driver books (arrival ceil5(now)+15m) and pays; boundary tick → hold effective; next drive-up deny gate.lot_full;
                                          // the QR car enters (open); later pays at the pay station (amount > 0) and exits; reservation completed
func TestScenario_ReservationArrivals(t *testing.T) // seed 42, 2 virtual hours: every reserved car that reached the gate inside its window entered; occupancy and slot invariants after every step; stop {drain:true} → every reservation completed
func TestScenario_NoShow(t *testing.T) // seed 42: no-show cars expire at upper(window) via ExpireNoShows; slots released; effective_holds drops after the boundary tick
func TestScenario_ReservationDeterministic(t *testing.T) // two runs seed 42 on fresh DBs → identical request traces (type, device, path; tokens, ids and QR values excluded)
```

- [ ] **Step 2: Run** — `gox go test -race -count=1 ./cmd/api/ -run 'TestScenario_' -v`. Expected: FAIL until the rig changes land, then PASS. A real failure is a bug in the owning feature: fix it there, not in the test.
- [ ] **Step 3: Implement** the rig changes and the CI step (`for name in TestScenario_Guarantee TestScenario_ReservationArrivals TestScenario_NoShow TestScenario_ReservationDeterministic …`).
- [ ] **Step 4: Run** — `gox go test -race ./...`. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server/cmd/api .github/workflows/ci-server.yml
git commit -m "test(server): add m3 reservation scenario tests to ci"
```

---

## Task S9.1: Hardening — re-arm the pending check, log-leak, coverage gate, acceptance, compose

Design slice **S9** (design §15.3, §15.4, §15.6, §15.7, M3B-12, M3B-47, M3B-52). Budget: about 350 lines.

**Files:**
- Create: `apps/server/cmd/api/acceptance_m3_test.go`
- Modify: `cmd/api/{openapi_test.go,logleak_test.go}`, `scripts/covergate.sh`, `.github/workflows/ci-server.yml` (coverage step packages; named step adds `TestNoSecretsInLogs_M3`, `TestM3BackendAcceptance`), `deploy/compose/docker-compose.yml` (verify every M3 default and `# dev only` comment: `QR_HMAC_KEY`, `PAYMENT_GATEWAY`, `PAYMENT_CHARGE_TIMEOUT`, `SIM_GATEWAY_LATENCY`, `SIM_GATEWAY_TIMEOUT_DELAY`, `SEED_SIMULATOR_DRIVERS`)

**Interfaces:**
- Consumes: everything above.
- Produces:
  - `m3PendingAllowlist` is deleted, so `TestOpenAPI_NoPendingOperations` is strict again.
  - **Coverage gate.** The `covergate.sh` domain regex adds `internal/features/reservations/(domain|state|qr|qrdecide)\.go` and `internal/features/payments/(card|backoff|replay)\.go`. The CI coverage line adds `./internal/features/reservations/ ./internal/features/payments/`.
  - `TestM3BackendAcceptance`: one subtest per design §15.7 row, each naming the test that proves it.

- [ ] **Step 1: Write the tests**

```go
func TestOpenAPI_NoPendingOperations(t *testing.T) // unchanged body; passes only once the allowlist is gone and no marker remains
func TestNoSecretsInLogs_M3(t *testing.T) // flows: pay with 4242/0002/9995 and CVC "737", QR fetch, gate QR entry, cancel + refund job, simulated driver login;
                                          // captured logs, every error body, rows of payments, refunds, sim_gateway_charges, device_events (payload+response), outbox_messages after send, and the simulator event log contain none of:
                                          // the three PANs (with and without spaces), "737", QR_HMAC_KEY (base64 and raw hex), any issued QR token, any simulated driver password
func TestM3BackendAcceptance(t *testing.T) { // subtests: "cap holds under 200 parallel" (CapConcurrent200 rerun small: 100 attempts, cap 84 → 84); "guarantee"; "cancel before → refunded, after → none, no-show expired";
	// "decline 402 and timeout 202 → confirmed"; "physically full → cancelled + refund succeeded"; "3 no-shows → blocked"; each failure prints the criterion → test mapping
}
```

- [ ] **Step 2: Run** — `gox go test -race -count=1 ./cmd/api/ -run 'NoPendingOperations|NoSecretsInLogs|M3BackendAcceptance' -v`. Expected: `NoPendingOperations` FAILs while the allowlist exists, then PASSes after Step 3. A leak failure is a bug in the owning feature.
- [ ] **Step 3: Implement** the allowlist removal, the coverage gate, the CI lines and the compose check.
- [ ] **Step 4: Run** — `gox go test -race ./...`, then `gox sh -c 'go test -coverprofile=domain.cover ./internal/features/auth/ ./internal/features/vehicles/ ./internal/infrastructure/hashing/ ./internal/features/tariffs/ ./internal/features/availability/ ./internal/features/gate/ ./internal/features/reservations/ ./internal/features/payments/ && sh scripts/covergate.sh domain.cover 90'`. Expected: `ok` and `domain statement coverage: ≥90.0% OK`.
- [ ] **Step 5: Commit**

```bash
git add apps/server .github/workflows/ci-server.yml deploy/compose/docker-compose.yml
git commit -m "test(server): re-arm the pending-operation check and add m3 leak, coverage and acceptance gates"
```

---

## Task S9.2: Docs sync, CLAUDE.md status, and the full CI-equivalent run

Design slice **S9** (docs, design §16.1, §19, §20). This is the final task. Budget: about 300 lines of docs.

**Files:**
- Modify: `docs/spec.md`, `docs/architecture.md`, `docs/policies.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: everything above, plus the resolved gaps below.
- Produces: documents that agree with the code.

- [ ] **Step 1: Update `docs/spec.md`**
  - §3.7 **FR-SIM-2**: "The simulator uses only public APIs (the device API and the driver API), never the database" (M3B-44). Note the seeded simulated drivers.
  - §3.4 FR-VEH-4: add `pending_payment` (M3B-31).
  - §5.4 state machines:
    - payment: an unknown outcome stays `pending` until re-queried, not `failed`;
    - reservation: `pending_payment → expired_unpaid` with a refund on a late success (M3B-10); **`checked_in → confirmed`** when the void job voids its stay, restoring the hold without a cap check, and `confirmed → checked_in` on re-presentation or a late entry pass (M3B-60);
    - stay: `voided → entry_pending` for reservation stays (M3B-24).
  - §5.2 and the glossary: the half-open hold window (I1).
  - §8 M3: tick the backend exit criteria, citing tests. Move "closing the lot refunds overlapping reservations" to M4 (M3B-2).
  - §10.2: a new entry accepting M3B-5 to M3B-59 (our calls), recording M3B-1 to M3B-4, M3B-44 and M3B-60 as user decisions, and listing the plan's resolved gaps 1–9.
- [ ] **Step 2: Update `docs/architecture.md`** per design §16.1:
  - §4.3: 402.
  - §6: no NOTIFY on expiry (I10).
  - §7: the insert-then-lock create (M3B-15), the half-open window, and "one stay per reservation, ever" (I3).
  - §7.1: the predicate with pending entries (I2).
  - §9: the key-only `Charge` re-query (M3B-9), test cards, the late-success rule, and the callback ports defined in `payments` (resolved gap 1).
  - §10: the job list with cadences and `@now` (I7).
  - §11.1: the new columns and `reconciliation_alerts` kinds (I12).
  - §11.2: `QR_HMAC_KEY` and the rotation note.
  - §14: the new env vars.
  - §17: an ADR for the sync-with-202 pay semantics.
- [ ] **Step 3: Update `docs/policies.md`** §2.7 with `reservation_write_user` 30/min per user (fail open) and `reservation_pay_user` 10 per 10 min per user (fail closed).
- [ ] **Step 4: Update `CLAUDE.md`.** Set "Where we are" to the current date. In the milestone table, mark M3 Track A backend done on `feat/m3-backend` (pending merge) and Track B pending. Replace "no design or plan yet" with the design and plan paths. Add an "M3 leftovers" list: the live `no_show` 25-minute wait (R1), the Track B UI for the decline and timeout paths, and the M4 hooks (closure, reconciliation).
- [ ] **Step 5: Cross-check** design §16.1, §19 (every row) and §20 (I1–I12) line by line against the edited docs. Each row is either reflected or explicitly noted as unchanged.
- [ ] **Step 6: Full verification** (all must pass):
  - `cd apps/server && gox go vet ./... && gox go test -race ./...`
  - `gox go tool sqlc diff -f internal/infrastructure/database/sqlc.yaml`
  - `gox sh -c 'go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./...'`
  - `MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)":/src -w /src golangci/golangci-lint:v2.15.0 golangci-lint run ./...`
  - `gox go generate ./internal/domain/errcodes/... && git diff --exit-code ../../packages/contracts/src`
  - From the repo root, the CLAUDE.md block verbatim:

```bash
npm run check:react && npm run check:i18n && npm run check:layout && npm run check:tokens && npm run test:scripts
npm run typecheck -w packages/contracts && npm run typecheck -w apps/web && npm run typecheck -w apps/mobile
npm run lint -w apps/web && npm run lint -w apps/mobile
npm test -w packages/contracts && npm test -w apps/web && npm test -w apps/mobile
npm run build -w apps/web
(cd apps/mobile && npx expo install --check)
npm audit --omit=dev --audit-level=high
```

  - `npm run gen:contracts && git diff --exit-code packages/contracts/src/openapi.d.ts`
  - Compose smoke:
    - `POSTGRES_PORT=5433 docker compose -f deploy/compose/docker-compose.yml down -v && POSTGRES_PORT=5433 docker compose -f deploy/compose/docker-compose.yml up -d --build`. Expected: every service healthy.
    - Log in `sim-driver-01@mypark.test` with its derived password, then quote, create, pay `4242…`, fetch the QR, and inject it through `POST /control/cars` with `hold: true`. Expected: the barrier opens and the reservation is `checked_in`.
- [ ] **Step 7: Commit**

```bash
git add docs CLAUDE.md
git commit -m "docs: sync spec, architecture, policies and project status with the m3 backend"
```

Then ask the user before any push or merge (CLAUDE.md).

---

## Task dependencies

| Task | Depends on |
|---|---|
| S1 contract | — |
| S2.1 schema | S1 (approved) |
| S2.2 config | S1 |
| S3.1 domain | — (pure; may start after S1) |
| S3.2 create | S2.1, S3.1 |
| S3.3 reads, adapters | S3.2 |
| S4.1 payments domain, sim | S2.1, S2.2 |
| S4.2 pay | S3.3, S4.1 |
| S4.3 email, reconcile | S4.2 |
| S5.1 cancel | S4.2 |
| S5.2 refunds | S5.1 |
| S5.3 receipts | S5.1 |
| S6.1 QR entry | S5.1 (refund + alert) |
| S6.2 stranded check-in | S6.1 |
| S6.3 pay station, exit | S6.2 |
| S7.1 expiry, no-show | S6.3 |
| S7.2 boundary, reminders | S7.1 |
| S8.1 drivers | S3.3 (login + create through the API), S4.2 |
| S8.2 scenarios | S8.1, S6.3 |
| S8.3 scenario CI | S8.2, S7.2, S5.2, S4.3 |
| S9.1 hardening | all above |
| S9.2 docs + full run | S9.1 |

## Self-review notes

- **Spec coverage:**

  | Design section | Tasks |
  |---|---|
  | §1.2 G1 | S3.2 (`CapConcurrent200`) |
  | §1.2 G2 | S6.1, S8.3 |
  | §1.2 G3 | S4.2, S4.3 |
  | §1.2 G4 | S5.1, S5.2, S7.1 |
  | §1.2 G5 | S6.1 |
  | §1.2 G6 | S7.1 |
  | §1.2 G7 | S4.2 (bump), S7.2 |
  | §1.2 G8 | S1 |
  | §2.1 M3B-1 to M3B-4 | whole plan; S1 handoff |
  | §2.1 M3B-44 | S8.1, S9.2 FR-SIM-2 |
  | §2.1 M3B-60 | S6.2, S9.2 |
  | §6.1–§6.3 | S2.1 |
  | §7.1–§7.5, §7.7, §7.11 | S3.2, S3.3 |
  | §7.6, §7.9 | S4.2, S4.3 |
  | §7.8 | S5.1 |
  | §7.10 | S5.3 |
  | §8.1–§8.4 | S3.2, S3.3, S4.2, S5.1, S6.x, S7.2 |
  | §9.1–§9.5 | S4.1–S4.3 |
  | §10.1–§10.4 | S3.1, S5.2, S6.1, S6.2 |
  | §11.1–§11.6 | S6.1–S6.3 (§11.2 codec in S4.2) |
  | §12, §12.1–§12.7 | S4.3, S5.2, S6.2, S7.1, S7.2 |
  | §13.1–§13.3 | S8.1–S8.3, S1 (schema) |
  | §14.1–§14.2 | S1 |
  | §15.1–§15.7 | owning tasks; S9.1 |
  | §16.1 docs | S9.2 (compose in S2.2, S8.1, S9.1) |
  | §19–§20 | S9.2 cross-check |

- **Resolved gaps and plan decisions.** S9.2 records them in spec §10.2.
  1. **Import cycle.** Design §5.1 has `payments` call `reservations.ChargeOutcome` and `reservations.RefundNotice` while `reservations` imports `payments`. The callbacks are `payments.OutcomeApplier` and `payments.RefundNotifier`, defined in `payments` and implemented by `*reservations.Service`.
  2. **Gate port.** Design §5.1's `gate.Reservations` has no restore or late-pass method. It gains `RestoreAfterVoid` and `LateEntryPass`. `Reopen`/`StayForQR` became gate repo methods (`ReopenReservationStay`, `ReservationStayForUpdate`), because stays belong to `gate`.
  3. **Inline QR.** `email.Message` had no attachments, so S4.3 adds `Inline` and `multipart/related`.
  4. **Gateway key at insert.** `idempotency_key = 'rsv-fee:' || public_id` cannot read a column default in the same insert. The payment and refund `public_id` is generated in Go (`uuid.NewV7`).
  5. **sqlc queries per task.** Design §16 puts the queries in S2. Each task adds its own instead, which keeps diffs reviewable and avoids dead queries.
  6. **Card numbers with spaces.** M3B-11 prints the test cards spaced. The server strips ASCII spaces and hyphens before validation; anything else non-numeric is a `422`.
  7. **MSW fixtures.** The zod schemas in `packages/contracts/src/m2/` were written by the clients track, so S1 ships OpenAPI types plus fixtures typed with `satisfies`. Track B adds the m3 zod schemas and may wrap the fixtures.
  8. **Worker pool.** M3B-51 says "4 → 8", but `cmd/worker` already has `maxConns = 8`. Kept, with a test.
  9. **Port name.** Design §5.1 says `notifications.Enqueuer`. The real M1 port is `notifications.Notifier`.
  10. **Same-key concurrency test.** Design §15.2 says "all answers equal", but replays of an in-flight attempt legitimately answer `202`. `TestPay_SameKeyParallel` blocks the gateway and asserts one charge, one row, `202` replays with the same payment id, and the leader's `200`.
  11. **Void signature.** `VoidUnpassedEntries` stays batch-only (now excluding reservation stays). The per-row reservation path is a new `gate.ReservationVoider`, and the worker runs both in one tick.
- **Type consistency.** These names are used unchanged from the task that defines them to every later use: `reservations.Status`, `Event`, `Transition`, `Window`, `Policy`, `View`, `Service.Apply`, `GateAccess`, `Holds`, `VehicleChecker`, `Jobs`, `HoldBoundary`; `payments.Gateway`, `ChargeRequest`, `ChargeResult`, `Card`, `PayRequest`, `Fees`, `Attempt`, `FeePayment`, `OutcomeApplier`, `RefundNotifier`, `RefundDone`, `Reconciler`, `RefundProcessor`; `gate.Reservations`, `ReservationRef`, `ReservationEntry`, `ReservationVoider`; `alerts.Recorder`, `alerts.Kind`; `testsupport.AssertSlotInvariant`, `SeedDrivers`; `devicefleet.SimDriverPassword`. `gate.Deps` and `reservations.Deps` grow additively (S3.2 → S4.2 → S5.1 → S6.1).
- **Required concurrency tests:**
  - cap pair: S3.2 `TestReservations_CapAtomicPair`;
  - double pay with one key: S4.2 `TestPay_SameKeyParallel`;
  - refund never duplicated: S2.1 `TestSchema_OneLiveRefund`, plus S5.2 `TestRefunds_ParallelRunnersOneCallPerAttempt` and `TestRefunds_LeaseExpiryNeverDuplicates`;
  - void restore: S6.2 `TestGateQR_StrandedCheckIn` and `TestGateQR_VoidRacesRePresent`;
  - late entry and exit after the void: S6.2 `TestGateQR_LateEntryPassAfterVoid` and `TestGateQR_VoidRacesLateEntryPass`, plus S6.3 `TestGateQR_LateEntryThenExitCompletes`.
- **Review Focus:** each item has its test in the owning task (S3.1/S3.2, S4.1/S4.2, S4.2, S6.1, S3.3/S5.3/S6.1/S8.2).
- **Proportion:** the plan states signatures, rules, values and test assertions. It points to the design for SQL, tables and outcome tables. The only bodies are the gate port declaration (shared by three tasks) and the CLAUDE.md check block, which is copied verbatim.
