# M2 Track A — Backend Availability, Gates and Simulator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go side of M2:
- the published M2 contract;
- the HMAC-signed, idempotent device API;
- the gate flows (entry, barrier pass, pay station, exit) around one authoritative occupancy counter;
- the fee calculator;
- public lot availability over REST and SSE;
- the deterministic simulator with its control API.

Each part ships with the tests that prove the M2 exit criteria on the backend.

**Architecture:**
- Features follow architecture §4.1 (domain files, `service.go`, `ports.go`, `http.go`, `postgres/` over sqlc `dbgen`), and features talk only through exported interfaces.
- Dependencies point one way: `devices` → `gate` → `tariffs`/`availability`. Event and result types live in `gate`, and `authz.Device` lives in `domain/authz`.
- `devices` authenticates the device, decodes the event and opens one transaction. In that transaction, `gate.Handle` takes its locks in the M2C-29 order and the `device_events` insert runs last. `pg_notify` fires on commit.
- One Postgres LISTEN connection feeds an in-process hub. The hub coalesces pushes per lot and fans them out to SSE subscribers.
- The simulator is a seeded discrete-event engine that reaches the API only through the signed device API.

**Tech Stack:** Go 1.27, stdlib `net/http` ServeMux, pgx/v5, sqlc, goose, PostgreSQL 18, go-playground/validator v10, `golang.org/x/crypto/nacl/secretbox`, `crypto/hmac`, `math/rand/v2`, `github.com/google/uuid`, openapi-typescript and zod 4 in `packages/contracts`.

**Spec:** `docs/superpowers/specs/2026-11-13-m2-backend-availability-gates-simulator-design.md` (cited below as "design §N"). Executors keep it open, because this plan points to its SQL, tables and payload shapes instead of copying them. Also binding: `docs/spec.md`, `docs/architecture.md`, `docs/policies.md`.

## Global Constraints

- **Skills.** Before any Go work, load `.claude/skills/golang-patterns/SKILL.md`, `.claude/skills/golang-pro/SKILL.md` and `C:\Users\benja\.claude\skills\go-testing\SKILL.md`. Before any migration or SQL, load `.claude/skills/supabase-postgres-best-practices/SKILL.md`. Where a skill conflicts with the spec, architecture or policies, the spec, architecture and policies win.
- **Go.** Go 1.27, module `github.com/XYZLZ/MyPark/apps/server`. Use modern Go (`errors.AsType`, `t.Context()`, `wg.Go`, `range` over int, `min`/`max`, `slices`/`maps`, `math/rand/v2`). Secrets, ticket codes and nonces come from `crypto/rand` only. **No new Go module in M2** (design §3): `secretbox` is in `golang.org/x/crypto` (already direct), and `math/rand/v2` and `container/heap` are stdlib.
- **HTTP.** Use stdlib ServeMux only. The global chain stays `Recover → RequestID → AccessLog → SecureHeaders → CORS → JSONErrors`, and everything else is per route through `router.Router`. Handlers write only through `response.OK`, `response.List`, `response.NoContent` or `httpx.WriteError`. SSE writes only through `transport/http/sse`.
- **Envelope.** Every device and simulator response uses the baseline envelope (`{"data":…,"meta":{"request_id"}}` / `{"error":{code,message,details},"meta":{request_id}}`).
  - Device business outcomes are `200` with `data.result` ∈ `ack|open|deny|rejected` and `reason` (M2C-20).
  - SSE `data:` lines are bare objects, not wrapped in the envelope (design §11.3).
- **Data access.**
  - sqlc queries live in `internal/infrastructure/database/queries/<feature>.sql`, and the generated `dbgen` code is committed.
  - Regenerate with `go tool sqlc generate -f internal/infrastructure/database/sqlc.yaml`. CI runs `sqlc diff`.
- **Time.**
  - The server clock decides all business time (M2C-13). Every business timestamp is written as `@now` from `clock.Clock`, and `now()` appears only in `created_at`/`received_at` defaults.
  - Fees use whole seconds and integer centavos (`bigint *_cents`), DOP implied (M2C-36).
- **Migrations.**
  - Goose, forward-only, run as `mypark_migrator`. The app runs as `mypark_app`.
  - Files are `00007_tariffs_policies.sql` (S2.2), `00008_devices.sql` (S3) and `00009_stays_payments.sql` (S6), merged **in number order**.
  - Follow architecture §11 conventions: identity PKs, `public_id uuid default uuidv7()`, `text` + `check`, `timestamptz`, every FK indexed, partial indexes for hot subsets.
- **Device transactions.**
  - One event is one short transaction with `set local statement_timeout = '2s'` and `set local lock_timeout = '1s'`, and no network I/O inside it.
  - Lock order is always `lot_occupancy FOR UPDATE → stays → payments → device_events insert` (M2C-29).
  - Pay-station and exit-credential flows never take the lot lock.
- **Validation.**
  - Every new M2 **request** schema has `additionalProperties: false`.
  - Go decodes device payloads and control bodies with unknown-field rejection at every level, which gives `422 validation.failed` with rule `unknown_field` (M2C-52).
  - Values are never echoed back.
- **Error codes.**
  - New codes go in `internal/domain/errcodes/errcodes.go`.
  - `go generate ./internal/domain/errcodes/...` regenerates `packages/contracts/src/error-codes.ts`, and `npm run gen -w packages/contracts` regenerates `src/openapi.d.ts`.
  - Every code gets `errors.<domain>.<reason>` in es-DO and en in **both** `apps/web/src/i18n/messages/` and `apps/mobile/src/lib/i18n/locales/` in the same PR (`npm run check:i18n`).
  - Codes are never removed.
- **Configuration.**
  - No `.env` files. Compose uses inline `${VAR:-dev-default}`.
  - Accessors sit over unexported fields, and `String`/`GoString`/`LogValue` redact.
  - The committed dev values of `SECRETBOX_KEY`, `CARD_UID_PEPPER`, `SIMULATOR_FLEET_SECRET` and `SIMULATOR_CONTROL_TOKEN` are refused when `APP_ENV=production`.
- **Secrets never leak.** Device secrets, the fleet secret, keys, peppers, ticket codes and card UIDs never appear in logs, error bodies, `device_events.payload` (redacted to `sha256:<12 hex>`) or `device_events.response` (ticket codes sealed). Decrypted secrets are `secret.Bytes`.
- **Authorization.**
  - Deny by default. `router.Policy` is exactly one of `Public`, `Roles` or `Device` (M2C-48).
  - HMAC proves identity. The kind → event matrix decides what a device may do, and every `gate` query takes `device.LotID` (M2C-21).
- **Testing.**
  - TDD, test first, everything with `-race`. Use an injected clock and seeded randomness, with no `time.Sleep` assertions.
  - Integration tests use real Postgres through `testsupport.NewTestDB`, connected as `mypark_app`.
  - The invariant `occupied = count(stays where status in ('active','paid'))` is asserted after every gate integration test, using `testsupport.AssertOccupancyInvariant` (from S8).
- **Integration test env** (Git Bash; repeated in steps as `$IT`): `docker run -d --name mypark-pg-test -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:18`, then prefix commands with `TEST_DATABASE_URL='postgres://postgres:test@localhost:55432/postgres?sslmode=disable'`.
- **JS.** Use npm, never pnpm. On this Windows machine `npm install <pkg>` fails with ERESOLVE, so edit `package.json` and run `npm install`. No new JS dependency is planned.
- **Commits.** Conventional Commits with scopes `server|web|mobile|contracts|deploy|docs|ci`. **No Co-Authored-By and no AI attribution of any kind**, in commits and PR descriptions alike.
- **PR size.** PRs are ≤ 400 changed lines, chained. Generated files (`dbgen`, `openapi.d.ts`, `error-codes.ts`) are excluded, and `openapi.yaml` is included.
- **Sensitive slices.** Tasks marked **Sensitive** get the "Security checklist" from `.github/pull_request_template.md` completed in the PR, plus an automated risk review (`review-risk` or `/security-review`) linked in the PR (policies §2.2, M2C-12).
- **Commands.** Everything runs in Git Bash on Windows and in Linux CI. Every Go command runs from `apps/server`.

## Review Focus

1. **SSE streams that live longer than the server's `WriteTimeout`.** `httpserver.New` sets `WriteTimeout: 15 * time.Second`, so without a per-write deadline every stream is cut after 15 s and every client reconnects in lockstep. Expected: a stream stays open while heartbeats flow. Test: S11.2 `TestStream_OutlivesServerWriteTimeout`.
2. **Ticket codes and card UIDs typed in another form.** Examples: a lowercase code, hyphens or spaces, or the Crockford aliases `I`/`L`/`O`. Expected: they resolve to the same stay, because the server normalizes before hashing. Tests: S7 `TestNormalizeTicketCode`, `TestNormalizeCardUID`; S9.1 `TestScan_NormalizedTicketFindsStay`.
3. **Nested junk in device payloads.** Examples: unknown fields inside `payload` or `payload.credential`, `payload: null`, or `amount_cents` as `5000.5`, `"5000"`, `1e20`, `0` or `-1`. Expected: `400 http.invalid_json` or `422 validation.failed`, nothing stored, never 500. `httpx.DecodeJSON` does not reject unknown fields, so devices use their own strict decoder. Test: S5.1 `TestDecodeEvent_NestedJunk`.
4. **Garbage lot ids on public routes.** Examples: a slug (`/v1/lots/zona-colonial`), a malformed uuid, or an unknown uuid, on `/v1/lots/{id}`, `/availability`, `/spaces`, `/tariff` and `/availability/stream`. Expected: `404 lot.not_found` with identical bodies (minus `request_id`), never 500 or 422. Test: S10 `TestLots_GarbageIDIs404`.
5. **Garbage stream parameters.** Examples: `Last-Event-ID` set to `abc`, `-1` or `99999999999999999999`, or `include=foo` / `include=spaces,spaces`. Expected: `200`, with the availability snapshot as the first event (`spaces` only for exactly `include=spaces`). Test: S11.2 `TestStream_GarbageLastEventIDAndInclude`.

---

## File Structure

```
apps/server/
├─ api/openapi.yaml                                  # C1.2 lots/SSE, C2.1 device API, C2.2 simulator control
├─ cmd/api/{main.go, openapi_test.go, authz_matrix_test.go, logleak_test.go,
│           device_events_test.go, stream_test.go, scenario_test.go, acceptance_m2_test.go}
├─ cmd/worker/main.go                                # S8 void job, S14.1 purge job
├─ cmd/seed/main.go                                  # S2.2 tariff/policy versions, S3 fleet devices, S6 cards
├─ cmd/simulator/{main.go, openapi_test.go}          # S13.2
├─ internal/config/{config.go, dev.go, seed.go, simulator.go}   # S1.1, S3, S13.2
├─ internal/domain/{authz/authz.go, errcodes/errcodes.go, secret/secret.go}
├─ internal/devicefleet/fleet.go                     # S1.2 manifest + derivation
├─ internal/devicesig/{devicesig.go, testdata/golden.json}       # S4 canonical string + HMAC
├─ internal/features/
│  ├─ tariffs/{calc.go, tariffs.go, service.go, seed.go, http.go, postgres/}   # S2.1, S2.2
│  ├─ devices/{auth.go, envelope.go, matrix.go, redact.go, seed.go, purge.go, http.go, ports.go, postgres/}  # S3–S5.2
│  ├─ gate/{events.go, result.go, codes.go, state.go, service.go, entry.go, pass.go,
│  │        paystation.go, exit.go, sensor.go, void.go, seed.go, ports.go, postgres/}  # S5.1–S9.2
│  ├─ availability/{domain.go, ports.go, service.go, coalescer.go, hub.go, caps.go, http.go, stream.go, postgres/}  # S7, S10, S11
│  ├─ lots/{seed.go, lots.go, service.go, http.go, mapurl.go, postgres/}      # S2.2 (resolver), S10
│  └─ audit/audit.go                                 # S7/S8 new actions
├─ internal/infrastructure/
│  ├─ secretbox/secretbox.go                         # S1.1
│  ├─ ratelimit/{ratelimit.go, rules.go, memory.go, composite.go}   # S1.2, S4
│  ├─ pubsub/listener.go                             # S11.1
│  ├─ clock/clock.go                                 # S11.1 Timers
│  └─ database/{migrations/0000{7,8,9}_*.sql, queries/{tariffs,devices,gate,lots,availability}.sql, dbgen/, grants_m2_test.go}
├─ internal/simulator/{clock.go, engine.go, client.go, transport.go, car.go, scenarios.go,
│                      runner.go, eventlog.go, control.go}   # S12.1–S13.2
├─ internal/testsupport/{clock.go, devices.go, gate.go, lot.go}
├─ internal/transport/http/{router/router.go, middleware/{ratelimit.go,accesslog.go}, reqctx/reqctx.go, sse/sse.go}
└─ scripts/covergate.sh                              # S14.1
packages/contracts/src/{error-codes.ts, openapi.d.ts, health.ts, m2.types.test.ts}
apps/web/src/i18n/messages/{en,es-DO}.json   apps/mobile/src/lib/i18n/locales/{en,es-DO}.json
apps/mobile/src/services/{session.ts,session.test.ts}
deploy/compose/docker-compose.yml   .github/workflows/ci-server.yml
docs/{architecture.md, spec.md, policies.md}, docs/superpowers/specs/2026-11-08-m1-clients-auth-vehicles-design.md
```

**Slicing.** The design's 17 slices are kept in build order (design §16), and the original slice id is visible in every task. Seven slices did not fit in 400 lines, so each was split in two (`.1`/`.2`) and the reason is stated in the task. That gives 27 tasks.

---

## Task C1.1: Contract A, part 1 — every M2 error code, `session.logout_unconfirmed` (L1) and translations

Design slice **C1** (handoff, so both developers approve). It was split from C1.2 because the codes plus four locale files plus the lot paths come to about 600 lines. Budget: ~260 lines.

**Files:**
- Modify: `apps/server/internal/domain/errcodes/{errcodes.go,errcodes_test.go}`, `packages/contracts/src/error-codes.ts` (generated), `apps/web/src/i18n/messages/{en,es-DO}.json`, `apps/mobile/src/lib/i18n/locales/{en,es-DO}.json`
- Create: `packages/contracts/src/m2.types.test.ts`

**Interfaces:**
- Consumes: the existing `errcodes.Code`, `catalog` and `All()`.
- Produces Go constants with exactly these names. Their values are design §7.9.
  - Device: `DeviceUnauthenticated`, `DeviceTimestampOutOfWindow`, `DeviceEventNotAllowed`, `DeviceEventConflict`.
  - Lot: `LotNotFound`, `LotSpaceUnknown`.
  - Gate: `GateLotFull`, `GateLotPhysicallyFull`, `GateLotClosed`, `GateCredentialInvalid`, `GateCredentialInUse`, `GatePaymentRequired`, `GateAmountMismatch`, `GateNothingDue`, `GateStayUnknown`, `GateExitNotAuthorized`, `GateDuplicatePass`.
  - Simulator: `SimulatorUnauthorized`, `SimulatorAlreadyRunning`, `SimulatorNotRunning`, `SimulatorScenarioUnavailable`, `SimulatorCarNotFound`.
  - Session: `SessionLogoutUnconfirmed` (`"session.logout_unconfirmed"`), with the comment `// Returned by the web BFF only.`
  - 23 constants in total.

- [ ] **Step 1: Write the failing tests**

```go
// errcodes_test.go
func TestCatalog_M2Codes(t *testing.T) // table of 23 {Const, literal} pairs, e.g. {DeviceUnauthenticated,"device.unauthenticated"}, {GateLotFull,"gate.lot_full"}, {SimulatorCarNotFound,"simulator.car_not_found"}, {SessionLogoutUnconfirmed,"session.logout_unconfirmed"}; each is in All()
```

```ts
// m2.types.test.ts
test('ERROR_CODES includes every M2 code') // expect(ERROR_CODES).toEqual(expect.arrayContaining([...the 23 literals]))
```

- [ ] **Step 2: Run to verify they fail** — `cd apps/server && go test ./internal/domain/errcodes/...` → FAIL (undefined: errcodes.DeviceUnauthenticated); `npm test -w packages/contracts` → FAIL.
- [ ] **Step 3: Implement** the constants and catalog entries. Add `errors.device.*`, `errors.lot.*`, `errors.gate.*`, `errors.simulator.*` and `errors.session.logout_unconfirmed` (nested keys, the existing `errors.<domain>.<reason>` layout) to all four locale files. Device copy is short and technical. es-DO is neutral professional Spanish.
- [ ] **Step 4: Regenerate and verify** — `cd apps/server && go generate ./internal/domain/errcodes/... && go test ./internal/domain/errcodes/... && cd ../.. && npm test -w packages/contracts && npm run typecheck -w packages/contracts && npm run check:i18n && npm test -w apps/web && npm run typecheck -w apps/web && npm test -w apps/mobile && npm run typecheck -w apps/mobile`. Expected: all pass, and `check:i18n` reports parity.
- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/domain/errcodes packages/contracts/src apps/web/src/i18n apps/mobile/src/lib/i18n
git commit -m "feat(contracts): add m2 error codes and session.logout_unconfirmed with translations"
```

---

## Task C1.2: Contract A, part 2 — public lot paths, SSE, readiness `listener`, pending-operation marker

Design slice **C1** (handoff). Budget: ~340 lines.

**Files:**
- Modify: `apps/server/api/openapi.yaml`, `apps/server/cmd/api/openapi_test.go`, `packages/contracts/src/{health.ts,health.test.ts,openapi.d.ts,m2.types.test.ts}`, `docs/architecture.md` §4.4 (`GET /v1/lots`, `/spaces`)

**Interfaces:**
- Consumes: the M1 components (`Meta`, `ErrorEnvelope`, the DataEnvelope pattern), the C1.1 codes, and `newHandlerWithRoutes`.
- Produces:
  - **Operations** (all `security: []`). Each one carries `x-mypark-pending: <slice id>` until its slice serves it.
    - `listLots` `GET /v1/lots` (S10), returning `data: LotSummary[]` with `meta.page`.
    - `getLot` `GET /v1/lots/{id}` (S10).
    - `getLotAvailability` `GET /v1/lots/{id}/availability` (S10).
    - `getLotSpaces` `GET /v1/lots/{id}/spaces` (S10).
    - `getLotTariff` `GET /v1/lots/{id}/tariff` (S2.2).
    - `streamLotAvailability` `GET /v1/lots/{id}/availability/stream` (S11.2). Query `include` is an enum `[spaces]`, the header `Last-Event-ID` is optional, and the 200 response is `text/event-stream`.
  - **Schemas** (design §11.1/§11.3): `LotSummary`, `Lot`, `Availability`, `SpaceState` (`{code, state: free|occupied|out_of_service}`), `LotSpaces`, `Tariff`, `SpacesEvent` (`{full, spaces}`), `HeartbeatEvent` (`{as_of}`). The SSE response documents the three named events in its description and references the payload schemas.
  - **Errors:** `404 lot.not_found` on every `{id}` route, and `429 rate_limit.exceeded` with `Retry-After`.
  - **Readiness:** `ReadinessChecks` gains an optional `listener: ok|fail`. `readinessChecksSchema` gains `listener: z.enum(['ok','fail']).optional()`. Without it, the S11.2 `/readyz` change breaks the strict zod schema the web health probe uses.
  - **Parity test rule:** `openapi_test.go` skips operations carrying `x-mypark-pending`, and fails with `"%s is served but still marked x-mypark-pending"` when such an operation is served.

- [ ] **Step 1: Write the failing tests**

```go
// openapi_test.go
func TestOpenAPI_PendingOperationsAreNotServed(t *testing.T) // every op with x-mypark-pending is absent from router.Routes(); every pending value matches ^(S|C)[0-9.]+$
```

```ts
// m2.types.test.ts
test('every M2 public lot path exists') // expectTypeOf<paths['/v1/lots']['get']>().not.toBeNever(); …/{id}, /availability, /spaces, /tariff, /availability/stream
test('Availability has the design §11.1 fields') // expectTypeOf<Schemas['Availability']>() has lot_id, available, occupied, effective_capacity, capacity, effective_holds, upcoming_reservations_2h, status, version, updated_at
// health.test.ts
test('readiness accepts an optional listener check') // {data:{status:'ready',checks:{database:'ok',migrations:'ok',listener:'ok'}},meta:{request_id:'r'}} → ready; without listener → ready
```

- [ ] **Step 2: Run to verify they fail** — `cd apps/server && go test ./cmd/api/ -run OpenAPI` → FAIL (`GET /v1/lots is documented in openapi.yaml but not served`); `npm test -w packages/contracts` → FAIL.
- [ ] **Step 3: Implement** the YAML, the pending skip in `openapi_test.go`, and the `health.ts` change. Update architecture §4.4.
- [ ] **Step 4: Run** — `cd apps/server && go test -race ./cmd/api/ -run OpenAPI && cd ../.. && npm run gen -w packages/contracts && npm test -w packages/contracts && npm run typecheck -w packages/contracts && npm test -w apps/web && npm test -w apps/mobile`. Expected: all pass. A second `npm run gen` leaves `git diff --exit-code packages/contracts/src/openapi.d.ts` clean.
- [ ] **Step 5: Commit**

```bash
git add apps/server/api/openapi.yaml apps/server/cmd/api/openapi_test.go packages/contracts/src docs/architecture.md
git commit -m "feat(contracts): publish m2 public lot, availability and sse contract"
```

---

## Task C2.1: Contract B, part 1 — device API

Design slice **C2** (handoff). **Sensitive (device signatures).** It was split from C2.2 because a nine-variant `oneOf` plus seven control paths is more than 500 lines of YAML. Budget: ~360 lines.

**Files:**
- Modify: `apps/server/api/openapi.yaml`, `packages/contracts/src/{openapi.d.ts,m2.types.test.ts}`, `docs/architecture.md` §4.4 (heartbeat endpoint removed, M2C-15) and §8.1–§8.2 (canonical string, error codes, unauthenticated requests not stored, the `result` field, `paystation.lost_ticket`, the kind matrix)

**Interfaces:**
- Consumes: C1.1 codes, C1.2 `x-mypark-pending`.
- Produces:
  - **Security schemes:** `deviceId`, `deviceTimestamp` and `deviceSignature`, three `apiKey` `in: header` schemes for `X-Device-Id`, `X-Timestamp` and `X-Signature`. Together they form the `deviceHmac` requirement `[{deviceId: [], deviceTimestamp: [], deviceSignature: []}]` (M2C-49). The description gives the canonical string from M2C-16 verbatim.
  - **Operation** `postDeviceEvent` `POST /v1/devices/events`, `x-mypark-pending: S5.2`.
    - Request schema `DeviceEvent`: a `oneOf` discriminated by `type`. Each variant is named `DeviceEvent<PascalType>`, e.g. `DeviceEventEntryCredentialPresented` and `DeviceEventHeartbeat`. Each variant uses design §7.1–§7.2 payloads, and every object has `additionalProperties: false`.
    - The envelope fields are `event_id` (uuid), `occurred_at` (date-time) and `type`. `space_code` has `pattern: '^E[0-9]{3}$'` and `amount_cents` is an integer with `minimum: 1`.
    - 200 response: `DeviceEventResult`, one object with `result` (enum `ack|open|deny|rejected`, required) and optional `reason`, `stay_id`, `ticket_code`, `status`, `amount_cents`, `breakdown` (`FeeBreakdown`, design §7.2), `receipt_no`, `paid_at` and `exit_deadline`.
    - Response header `Idempotent-Replayed` (`"true"`, only on replays).
    - Error responses: 401 (`device.unauthenticated`, `device.timestamp_out_of_window`), 403 `device.event_not_allowed`, 409 `device.event_conflict`, 400/413/415, 422 `validation.failed` and 429 with `Retry-After`.

- [ ] **Step 1: Write the failing tests**

```ts
test('postDeviceEvent exists and requires the three HMAC headers') // expectTypeOf<paths['/v1/devices/events']['post']>().not.toBeNever()
test('DeviceEvent is a union discriminated by type with nine variants') // expectTypeOf<Schemas['DeviceEvent']['type']>().toEqualTypeOf<'podium.vehicle_detected'|'entry.credential_presented'|'barrier.passed'|'paystation.scan'|'paystation.lost_ticket'|'paystation.cash_paid'|'exit.credential_presented'|'sensor.space_changed'|'heartbeat'>()
test('DeviceEventResult.result is ack|open|deny|rejected')
```

- [ ] **Step 2: Run to verify they fail** — `npm test -w packages/contracts` → FAIL.
- [ ] **Step 3: Implement** the YAML and update architecture §4.4/§8.1/§8.2.
- [ ] **Step 4: Run** — `cd apps/server && go test ./cmd/api/ -run OpenAPI && cd ../.. && npm run gen -w packages/contracts && npm test -w packages/contracts && npm run typecheck -w packages/contracts`. Expected: pass.
- [ ] **Step 5: Commit**

```bash
git add apps/server/api/openapi.yaml packages/contracts/src docs/architecture.md
git commit -m "feat(contracts): publish m2 hmac-signed device event api"
```

---

## Task C2.2: Contract B, part 2 — simulator control paths and the service filter

Design slice **C2** (handoff). Budget: ~260 lines.

**Files:**
- Modify: `apps/server/api/openapi.yaml`, `apps/server/cmd/api/openapi_test.go`, `packages/contracts/src/{openapi.d.ts,m2.types.test.ts}`

**Interfaces:**
- Consumes: C1.1 simulator codes.
- Produces:
  - **Security scheme** `simulatorToken` (apiKey, header `X-Simulator-Token`).
  - **Path-level settings.** Each `/control/...` path carries `servers: [{url: http://simulator:8090}]` and `x-mypark-service: simulator` (M2C-49).
  - **Operations** (design §13.3):
    - `getSimulatorStatus`, `startSimulatorScenario`, `stopSimulatorScenario`
    - `injectSimulatorCar`, `releaseSimulatorCar`
    - `setSimulatorSensorFault`, `listSimulatorEvents`
  - **Schemas:**
    - `SimulatorStatus`, `StartScenarioRequest` (`scenario` enum of the seven names, `seed` integer ≥ 0, `speed` integer 1–120, `start_at` date-time), `StopScenarioRequest`
    - `InjectCarRequest` (`credential` enum `ticket|employee_card`, `card` pattern `^EMP-[0-9]{3}$`, `hold`, `dwell_minutes` 1–600), `InjectCarResponse` (`car_id`)
    - `LeaveCarRequest`, `SensorFaultRequest` (`fault` enum), `SimulatorEvent`
  - **Parity test rule:** `TestOpenAPI_CoversRegisteredRoutes` skips every path whose `x-mypark-service` is `simulator`.

- [ ] **Step 1: Write the failing tests**

```go
func TestOpenAPI_SimulatorPathsSkippedByAPI(t *testing.T) // parsed doc has ≥ 7 ops under /control/ all with x-mypark-service "simulator"; none appears in the API "documented" set
```

```ts
test('every simulator control path exists') // seven expectTypeOf lines, e.g. paths['/control/scenarios/start']['post']
test('StartScenarioRequest.scenario lists the seven scenario names')
```

- [ ] **Step 2: Run to verify they fail** — `cd apps/server && go test ./cmd/api/ -run OpenAPI` → FAIL (`POST /control/scenarios/start is documented in openapi.yaml but not served`); `npm test -w packages/contracts` → FAIL.
- [ ] **Step 3: Implement** the YAML and the path-level skip.
- [ ] **Step 4: Run** — same commands as C2.1. Expected: pass.
- [ ] **Step 5: Commit**

```bash
git add apps/server/api/openapi.yaml apps/server/cmd/api/openapi_test.go packages/contracts/src
git commit -m "feat(contracts): publish m2 simulator control api"
```

---

## Task C1b: M1 leftovers — mobile logout-all 401 parity and docs (L2–L4)

Design slice **C1b**. Budget: ~80 lines.

**Files:**
- Modify: `apps/mobile/src/services/{session.ts,session.test.ts}`, `docs/superpowers/specs/2026-11-08-m1-clients-auth-vehicles-design.md` §5.5 (the table and "Logout and the 401 rule"), `docs/spec.md` §10.2 #37 and §8 M5 scope (the non-strict response parsing note)

**Interfaces:**
- Consumes: the existing `logout(scope?: 'this' | 'all'): Promise<LogoutOutcome>` and the `logout_all_failed` notice (its strings already exist).
- Produces the behavior change: with `scope === 'all'`, a 401 that survived the refresh attempt sets `outcome = 'failed'`, so `signOutLocal('logout_all_failed')`. With `scope === 'this'`, a 401 is still `'revoked'`.

- [ ] **Step 1: Write the failing tests**

```ts
// session.test.ts
test('logout-all treats a 401 as unconfirmed and shows logout_all_failed') // mock POST /v1/auth/logout-all → 401 auth.session_revoked → outcome 'failed', signOutLocal called with 'logout_all_failed', tokens cleared
test('logout (this device) still treats a 401 as success') // POST /v1/auth/logout → 401 → outcome 'revoked', notice null
```

- [ ] **Step 2: Run to verify they fail** — `npm test -w apps/mobile -- session` → FAIL (outcome 'revoked').
- [ ] **Step 3: Implement** the scope check at `session.ts` line 260. Update the docs:
  - Clients design §5.5: 401 on logout is success, and 401 on logout-all is unconfirmed on both clients.
  - Spec #37: the same correction.
  - Spec §8 M5 scope: "switch zod response parsing to non-strict before the first store build" (L4).
- [ ] **Step 4: Run** — `npm test -w apps/mobile && npm run typecheck -w apps/mobile`. Expected: pass.
- [ ] **Step 5: Commit**

```bash
git add apps/mobile/src/services docs/spec.md docs/superpowers/specs/2026-11-08-m1-clients-auth-vehicles-design.md
git commit -m "fix(mobile): treat a 401 on logout-all as unconfirmed like web"
```

---

## Task S1.1: Secretbox keyring, `secret.Bytes`, API config

Design slice **S1**. **Sensitive (secrets).** It was split from S1.2 (S1 as a whole is about 520 lines). Budget: ~330 lines.

**Files:**
- Create: `apps/server/internal/infrastructure/secretbox/{secretbox.go,secretbox_test.go}`
- Modify: `internal/domain/secret/{secret.go,secret_test.go}`, `internal/config/{config.go,dev.go,config_test.go}`, `deploy/compose/docker-compose.yml` (api env), `docs/architecture.md` §14 (new env vars; `/metrics` moves to M5, M2C-50)

**Interfaces:**
- Consumes: the existing `config.Load` and `decodeKey` patterns.
- Produces:
  - `type secret.Bytes []byte` with `(Bytes).Reveal() []byte`. `String`, `GoString`, `LogValue` and `MarshalJSON` all return `"[redacted]"`.
  - `type secretbox.Key struct{ ID string; Secret [32]byte }`.
  - `secretbox.NewSealer(current Key, old ...Key) (*Sealer, error)`. It errors on an empty or duplicate id.
  - `(*Sealer).Seal(plain []byte) (ciphertext []byte, keyID string, err error)`. The output is a 24-byte random nonce ‖ the secretbox output.
  - `(*Sealer).Open(ciphertext []byte, keyID string) ([]byte, error)` and `(*Sealer).CurrentKeyID() string`.
  - Errors: `secretbox.ErrUnknownKey` and `secretbox.ErrDecrypt`.
  - Config env constants: `EnvSecretboxKey = "SECRETBOX_KEY"`, `EnvSecretboxKeyID = "SECRETBOX_KEY_ID"`, `EnvSecretboxOldKeys = "SECRETBOX_OLD_KEYS"`, `EnvCardUIDPepper = "CARD_UID_PEPPER"`, `EnvSSEMaxPerIP = "SSE_MAX_PER_IP"`, `EnvSSEMaxStreams = "SSE_MAX_STREAMS"`, `EnvDeviceRateLimitPerS = "DEVICE_RATE_LIMIT_PER_S"`.
  - Defaults: `DefaultSSEMaxPerIP = 20`, `DefaultSSEMaxStreams = 2000`, `DefaultDeviceRateLimitPerS = 100`.
  - Dev constants in `dev.go`: `DevSecretboxKey`, `DevCardUIDPepper`, `DevSimulatorFleetSecret`, `DevSimulatorControlToken`, each a fresh base64 32-byte value marked `// dev only`.
  - Accessors: `(Config).SecretboxKeys() (current secretbox.Key, old []secretbox.Key)`, `(Config).CardUIDPepper() []byte`, `(Config).SSEMaxPerIP() int`, `(Config).SSEMaxStreams() int`, `(Config).DeviceRateLimitPerS() int`.
  - Exported parse helper for S3/S13.2: `config.ParseSecretboxKeys(id, key, old string, production bool) (secretbox.Key, []secretbox.Key, error)`.
- Rules:
  - `SECRETBOX_KEY` is required, and is base64 of exactly 32 bytes. `SECRETBOX_KEY_ID` is required.
  - `SECRETBOX_OLD_KEYS` is `kid:base64,…`, may not repeat the current id, and may not contain the dev key in production.
  - `CARD_UID_PEPPER` is required, at least 32 bytes.
  - `DEVICE_RATE_LIMIT_PER_S` must be 1–10000.
  - Production refuses `DevSecretboxKey` and `DevCardUIDPepper`.

- [ ] **Step 1: Write the failing tests**

```go
func TestBytes_NeverPrints(t *testing.T) // %v %+v %#v %s, json.Marshal, slog JSON → "[redacted]", never the plaintext "device-secret-plaintext"
func TestSealer_RoundTrip(t *testing.T) // Seal([]byte("abc")) → keyID == "k1", len == 24+3+16; Open → "abc"
func TestSealer_NoncesDiffer(t *testing.T) // two Seal of the same plaintext → different ciphertexts
func TestSealer_OpensWithOldKey(t *testing.T) // sealed by NewSealer(k1); NewSealer(k2, k1).Open(ct,"k1") → plaintext
func TestSealer_UnknownKeyAndTamper(t *testing.T) // Open(ct,"nope") → ErrUnknownKey; flip one byte → ErrDecrypt
func TestLoad_SecretboxAndPepperRequired(t *testing.T) // missing SECRETBOX_KEY, SECRETBOX_KEY_ID, CARD_UID_PEPPER → one aggregated error naming all three, values never printed
func TestLoad_SecretboxKeyMustBe32Bytes(t *testing.T) // base64 of 31 bytes → error naming SECRETBOX_KEY
func TestLoad_OldKeysParsed(t *testing.T) // SECRETBOX_OLD_KEYS "k0:<b64>" → old[0].ID == "k0"; "dev-1:<b64>" with current id dev-1 → error
func TestLoad_ProductionRefusesM2DevSecrets(t *testing.T) // APP_ENV=production with DevSecretboxKey or DevCardUIDPepper → error per var
func TestLoad_M2Defaults(t *testing.T) // SSEMaxPerIP()==20, SSEMaxStreams()==2000, DeviceRateLimitPerS()==100
func TestConfig_StringRedactsM2Secrets(t *testing.T) // fmt.Sprint(cfg) contains neither the secretbox key nor the pepper
```

- [ ] **Step 2: Run to verify they fail** — `go test -race ./internal/infrastructure/secretbox/... ./internal/domain/secret/... ./internal/config/...` → FAIL (undefined).
- [ ] **Step 3: Implement** with `golang.org/x/crypto/nacl/secretbox.Seal`/`Open` and nonces from `crypto/rand`. Add the api env to compose: `SECRETBOX_KEY: ${SECRETBOX_KEY:-<DevSecretboxKey>}`, `SECRETBOX_KEY_ID: ${SECRETBOX_KEY_ID:-dev-1}`, `SECRETBOX_OLD_KEYS: ${SECRETBOX_OLD_KEYS:-}` and `CARD_UID_PEPPER: ${CARD_UID_PEPPER:-<DevCardUIDPepper>}`, with a dev-only comment.
- [ ] **Step 4: Run** — same command plus `$IT go test -race ./cmd/api/...` (`testDeps`/`testEnv` gain the new vars). Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server deploy/compose/docker-compose.yml docs/architecture.md
git commit -m "feat(server): add secretbox keyring, secret bytes and m2 api config"
```

---

## Task S1.2: Fleet manifest, device kinds, local rate limiter

Design slice **S1**. **Sensitive (secrets: the fleet derivation).** Budget: ~320 lines.

**Files:**
- Create: `apps/server/internal/devicefleet/{fleet.go,fleet_test.go}`, `internal/infrastructure/ratelimit/{memory.go,memory_test.go,composite.go,composite_test.go}`
- Modify: `internal/domain/authz/authz.go` (device kinds), `internal/infrastructure/ratelimit/{ratelimit.go,rules.go,ratelimit_test.go}` (next to the existing `TestRules_MatchSpec`), `cmd/api/main.go` (`deps.limiter` becomes the Composite), `docs/architecture.md` §17 (ADR "local limiter scope", M2C-5/M2C-22)

**Interfaces:**
- Consumes: `secret.Bytes` (S1.1), `clock.Clock`, `ratelimit.Postgres`.
- Produces:
  - `type authz.DeviceKind string` with `DevicePodium="podium"`, `DeviceDispenser="dispenser"`, `DeviceEntryBarrier="entry_barrier"`, `DeviceExitBarrier="exit_barrier"`, `DevicePayStation="pay_station"` and `DeviceSensorGateway="sensor_gateway"`.
  - `devicefleet`:
    - `type Device struct{ Name string; Kind authz.DeviceKind; PublicID uuid.UUID }` and `type Card struct{ Code, Holder string; Revoked bool }`.
    - `Devices() []Device` returns the six from design §6.2 with ids `019a6d00-0000-7000-8000-00000000000{1..6}`. `ByKind(k authz.DeviceKind) Device`.
    - `Cards() []Card`: `EMP-001`…`EMP-030` active with holders `Empleado 01`…`Empleado 30`, and `EMP-031` revoked.
    - `DeviceSecret(fleetSecret secret.Bytes, id uuid.UUID) secret.Bytes` = HMAC-SHA256(fleet, `"mypark/device-secret/v1/" + id.String()`).
    - `CardUID(fleetSecret secret.Bytes, code string) string` = uppercase hex of the first 7 bytes of HMAC-SHA256(fleet, `"mypark/card-uid/v1/" + code`). It is a 14-character, 7-byte NFC-style UID (a plan decision; design §6.2 says only "a `card-uid` label").
  - `ratelimit`:
    - `type Scope int` with **`ScopeShared Scope = iota`** (the zero value, so every M1 rule stays on Postgres) and `ScopeLocal`. `Rule` gains `Scope Scope`.
    - `ratelimit.NewMemory(c clock.Clock) *Memory` implements `Limiter`. It uses fixed windows aligned to the Unix epoch (like `date_bin`) with `RetryAfter` = window end − now. It sweeps expired buckets lazily, at most once per minute of clock time.
    - `ratelimit.NewComposite(shared, local Limiter) Composite` and `(Composite).Allow` routes by `rule.Scope`.
    - Rules: `DeviceIP = Rule{Name:"device_ip", Limit:1000, Window:time.Second, Key:KeyIP, Prefix:"dev_ip:", Scope:ScopeLocal}`, `PublicReadIP = Rule{Name:"public_read_ip", Limit:120, Window:time.Minute, Key:KeyIP, Prefix:"pub:", Scope:ScopeLocal}`, `SSEConnectIP = Rule{Name:"sse_connect_ip", Limit:30, Window:time.Minute, Key:KeyIP, Prefix:"sse:", Scope:ScopeLocal}`.

- [ ] **Step 1: Write the failing tests**

```go
func TestDevices_Manifest(t *testing.T) // 6 devices; names/kinds exactly design §6.2; ids …0001…0006; ByKind(DeviceDispenser).Name == "dispenser-entry-1"
func TestDeviceSecret_GoldenVector(t *testing.T) // fleet = bytes 0x20..0x3f; id 019a6d00-0000-7000-8000-000000000002 → hex "a1d15e3256a68cdee96b287f51a7e82632439e0770627a01a86cb1672461f005"
func TestCardUID_GoldenVector(t *testing.T) // same fleet, "EMP-007" → "DFFE6EADBA0A4C"
func TestCards_Manifest(t *testing.T) // 31 cards; EMP-031 Revoked; holders fictitious "Empleado 07"
func TestMemory_FixedWindowWithFakeClock(t *testing.T) // Rule{Limit:3,Window:1s}: 3 allowed, 4th denied RetryAfter in (0,1s]; Advance to next window → allowed
func TestMemory_ConcurrentAllow(t *testing.T) // 50 goroutines × 1 Allow, Limit 20 → exactly 20 allowed
func TestMemory_SweepDropsExpired(t *testing.T) // 100 keys, Advance 2m, one Allow → internal bucket count == 1 (unexported len accessor in _test)
func TestComposite_RoutesByScope(t *testing.T) // recording limiters: ScopeShared rule → shared only; ScopeLocal → local only
func TestRules_M1RulesStayShared(t *testing.T) // every M1 rule (LoginIPEmail … DeleteAccountUser) has Scope == ScopeShared
func TestRules_M2LocalRules(t *testing.T) // DeviceIP 1000/1s, PublicReadIP 120/1m, SSEConnectIP 30/1m, all ScopeLocal, KeyIP
```

- [ ] **Step 2: Run to verify they fail** — `go test -race ./internal/devicefleet/... ./internal/infrastructure/ratelimit/... ./internal/domain/authz/...` → FAIL.
- [ ] **Step 3: Implement.** `Memory` is a `sync.Mutex` plus `map[string]*bucket`, keyed by `rule.Prefix + key`. `cmd/api` uses `ratelimit.NewComposite(ratelimit.NewPostgres(pool, clk), ratelimit.NewMemory(clk))`. Write the ADR in §17.
- [ ] **Step 4: Run** — same command plus `$IT go test -race ./cmd/api/...`. Expected: `ok` (the M1 rate-limit tests still use Postgres).
- [ ] **Step 5: Commit**

```bash
git add apps/server docs/architecture.md
git commit -m "feat(server): add simulator fleet manifest and local in-memory rate limiter"
```

---

## Task S2.1: Fee calculator — the §12.4 table suite

Design slice **S2**. **Sensitive (payments: it decides every amount).** It was split from S2.2 so the pure money core is reviewed on its own. Budget: ~330 lines, most of them table rows.

**Files:**
- Create: `apps/server/internal/features/tariffs/{tariffs.go,calc.go,calc_test.go}`

**Interfaces:**
- Consumes: none (pure).
- Produces:
  - `type tariffs.Tariff struct{ EffectiveFrom time.Time; FirstHour, AdditionalHour, DailyCap, LostTicket, ReservationFee int64; ExitGrace, PostPaymentExit time.Duration }`.
  - `tariffs.DefaultTariff() Tariff` gives the design §5.1/§12.3 values: 5000, 2500, 40000, 50000, 15000, 10 min, 15 min, with `EffectiveFrom = 2026-11-01T00:00:00-04:00`.
  - `type StayKind string` with `KindTicket="ticket"`, `KindEmployeeCard="employee_card"` and `KindReservation="reservation"`.
  - `type Stay struct{ Kind StayKind; EnteredAt time.Time; Paid bool; LostTicket bool; PaidCents int64; PaidAt time.Time; ExitDeadline time.Time }`.
    - `Paid` means status `paid`.
    - `PaidAt` is the latest succeeded payment's time, and zero means none.
    - `ExitDeadline` stays set after the lazy `paid → active` transition, and zero means never paid.
  - `type BreakdownKind string` with `BreakdownTime="time"`, `BreakdownGrace="grace"`, `BreakdownLostTicket="lost_ticket"`, `BreakdownEmployee="employee"`, `BreakdownOverstay="overstay"` and `BreakdownPaid="paid"`.
  - `type Breakdown struct` with JSON tags matching design §7.2: `Kind BreakdownKind`, `DurationSeconds, FullBlocks, RemainderSeconds int64`, `BilledFrom *time.Time` (`billed_from`, null unless overstay), `PaidCents int64`, `TariffEffectiveFrom time.Time`.
  - `tariffs.AmountDue(t Tariff, s Stay, now time.Time) (int64, Breakdown)`. It is design §12.2 `amountDue` exactly: whole seconds via `floor`, `ceilDiv`, and the overstay branch keyed off `ExitDeadline`, not `Paid` (M2C-35).

- [ ] **Step 1: Write the failing table test** — every design §12.4 row, verbatim, with default tariff, `t0 = 2026-11-24T08:00:00-04:00` and offsets from `t0`:

```go
// helpers (test file): ticket() = Stay{Kind: KindTicket, EnteredAt: t0}; lost() = ticket() with LostTicket true;
// emp() = Stay{Kind: KindEmployeeCard, EnteredAt: t0};
// paid(s, at, cents) sets Paid=true, PaidAt=t0+at, ExitDeadline=t0+at+15m, PaidCents=cents;
// lapsed(s) sets Paid=false and keeps PaidAt/ExitDeadline (lazy paid→active, or marked lost).
// Each row: {id, stay, scanAt (time.ParseDuration), wantCents, wantKind}
func TestAmountDue_Table(t *testing.T) {
	cases := []struct{ id string; s Stay; at string; want int64; kind BreakdownKind }{
		{"T1", ticket(), "0s", 0, BreakdownGrace},
		{"T2", ticket(), "9m59s", 0, BreakdownGrace},
		{"T3", ticket(), "10m0s", 0, BreakdownGrace},          // grace boundary is inclusive (≤)
		{"T4", ticket(), "10m1s", 5000, BreakdownTime},
		{"T5", ticket(), "59m59s", 5000, BreakdownTime},
		{"T6", ticket(), "1h0m0s", 5000, BreakdownTime},       // ceil(0) = 0
		{"T7", ticket(), "1h0m1s", 7500, BreakdownTime},
		{"T8", ticket(), "2h0m0s", 7500, BreakdownTime},
		{"T9", ticket(), "2h0m1s", 10000, BreakdownTime},
		{"T10", ticket(), "14h0m0s", 37500, BreakdownTime},    // 5000 + 13 × 2500
		{"T11", ticket(), "14h0m1s", 40000, BreakdownTime},    // reaches the cap exactly
		{"T12", ticket(), "15h0m1s", 40000, BreakdownTime},    // capped (42500 → 40000)
		{"T13", ticket(), "23h59m59s", 40000, BreakdownTime},
		{"T14", ticket(), "24h0m0s", 40000, BreakdownTime},    // one full block, remainder 0
		{"T15", ticket(), "24h0m1s", 45000, BreakdownTime},    // remainder of 1 s pays a first hour
		{"T16", ticket(), "24h9m0s", 45000, BreakdownTime},    // no grace on a remainder
		{"T17", ticket(), "25h0m1s", 47500, BreakdownTime},
		{"T18", ticket(), "48h0m0s", 80000, BreakdownTime},    // two blocks
		{"T19", ticket(), "75h0m0s", 130000, BreakdownTime},   // 3 × 40000 + (5000 + 2 × 2500)
		{"L1", lost(), "5m", 50000, BreakdownLostTicket},      // lost fee ignores grace
		{"L2", lost(), "3h", 50000, BreakdownLostTicket},      // replaces the time charge
		{"L3", lost(), "23h59m59s", 50000, BreakdownLostTicket}, // above the cap by design
		{"L4", lost(), "24h0m0s", 50000, BreakdownLostTicket}, // first block replaced, remainder 0
		{"L5", lost(), "26h0m0s", 57500, BreakdownLostTicket}, // 50000 + blockFee(2 h)
		{"L6", lost(), "50h0m0s", 97500, BreakdownLostTicket}, // 50000 + 40000 + 7500
		{"L7", lapsed(paid(lost(), "1h30m", 7500)), "1h40m", 42500, BreakdownLostTicket}, // fee_lost − paid, inside the window
		{"E1", emp(), "30h", 0, BreakdownEmployee},
		{"O1", paid(ticket(), "1h30m", 7500), "1h50m", 5000, BreakdownOverstay},   // segment 20 m: blockFee(1200) = 5000 (no grace)
		{"O2", paid(ticket(), "1h30m", 7500), "3h10m", 7500, BreakdownOverstay},   // segment 1 h 40 m: 5000 + 1 × 2500 (RD$75)
		{"O3", paid(ticket(), "20h", 40000), "23h", 10000, BreakdownOverstay},     // segment 3 h: 5000 + 2 × 2500
		{"O4", paid(ticket(), "1h30m", 7500), "1h44m", 0, BreakdownPaid},
		{"O5", paid(ticket(), "1h30m", 7500), "1h45m0s", 0, BreakdownPaid},        // deadline is inclusive (≤)
		{"O6", paid(lost(), "2h", 50000), "2h20m", 5000, BreakdownOverstay},       // segment 20 m; lostFee − paid = 0
		{"O7", paid(ticket(), "1h50m", 12500), "2h20m", 5000, BreakdownOverstay},  // payments 7500 @1h30 + 5000 @1h50; segment 30 m
		{"O8", lapsed(paid(ticket(), "1h30m", 7500)), "1h50m", 5000, BreakdownOverstay}, // calculator half: active, deadline 1h45 kept (flow half in S9.2)
		{"O9", paid(ticket(), "1h30m", 7500), "1h45m1s", 5000, BreakdownOverstay}, // segment 15 m 01 s; floor 2500 does not bind
		{"O10", paid(ticket(), "20h", 40000), "40h", 40000, BreakdownOverstay},    // segment 20 h: 52500 → cap 40000
		{"O11", paid(ticket(), "20h", 40000), "46h", 47500, BreakdownOverstay},    // segment 26 h: 40000 + blockFee(2 h)
		{"O12", paid(ticket(), "1h50m", 12500), "2h45m", 5000, BreakdownOverstay}, // segment 55 m from the second payment
		{"O13", lapsed(paid(lost(), "1h30m", 7500)), "2h", 42500, BreakdownLostTicket}, // max(2500, 5000, 50000 − 7500)
	}
	// for each: got, bd := AmountDue(DefaultTariff(), c.s, t0.Add(at)); assert got == c.want, bd.Kind == c.kind,
	// bd.PaidCents == c.s.PaidCents, bd.TariffEffectiveFrom == DefaultTariff().EffectiveFrom
}
func TestAmountDue_OverstayBilledFrom(t *testing.T) // O1: *bd.BilledFrom == t0+1h30m, DurationSeconds == 1200; O7 and O12: == t0+1h50m; O13 and T7: BilledFrom == nil
func TestAmountDue_BreakdownBlocks(t *testing.T)    // T19: DurationSeconds 270000, FullBlocks 3, RemainderSeconds 10800; O11: FullBlocks 1, RemainderSeconds 7200
func TestAmountDue_Properties(t *testing.T) // d from 0 to 80 h in 37 s steps: result ≥ 0; ticket timeFee non-decreasing in d; for d > grace, result ≤ ceil(d/24h) × cap;
                                            // for every overstay (paid at 1h30, scan at deadline+1s … +30h, step 61 s): result ≥ AdditionalHour
```

- [ ] **Step 2: Run to verify it fails** — `go test -race ./internal/features/tariffs/...` → FAIL (undefined: tariffs.AmountDue).
- [ ] **Step 3: Implement `AmountDue(t Tariff, s Stay, now time.Time) (int64, Breakdown)` in `calc.go`.** Follow design §12.2's pseudocode (`blockFee`, `timeFee`, `lostFee`, `segmentFee`, `overstayDue`) branch for branch. Keep the helpers unexported. Kinds:
  - `grace` when `timeFee` returns 0 because `d ≤ grace`.
  - `lost_ticket` when the lost term wins the overstay `max`.
  - `paid` for "paid and inside the window".
- [ ] **Step 4: Run** — `go test -race -cover ./internal/features/tariffs/...`. Expected: PASS, with `calc.go` coverage ≥ 90%.
- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/features/tariffs
git commit -m "feat(server): add fee calculator with grace, cap, lost ticket and overstay rules"
```

---

## Task S2.2: Tariff and policy versions — migration 00007, seed, `GET /v1/lots/{id}/tariff`

Design slice **S2**. **Sensitive (migration).** Budget: ~380 lines.

**Files:**
- Create: `migrations/00007_tariffs_policies.sql`, `queries/tariffs.sql`, `internal/features/tariffs/{service.go,seed.go,http.go,ports.go,postgres/repo.go}` + tests, `internal/features/lots/{lots.go,postgres/repo.go}`, `internal/testsupport/lot.go`, `internal/infrastructure/database/grants_m2_test.go`
- Modify: `queries/lots.sql` (`GetLotIDByPublicID`), `dbgen/*`, `cmd/seed/main.go`, `cmd/api/{main.go,authz_matrix_test.go}` (matrix entry and `specPolicy` `public`), `api/openapi.yaml` (remove `x-mypark-pending` from `getLotTariff`)

**Interfaces:**
- Consumes: `tariffs.Tariff`/`DefaultTariff` (S2.1), `ratelimit.PublicReadIP` (S1.2), `router.Policy{Public:true}`.
- Produces:
  - **Migration:** design §5.1 verbatim (`tariff_versions`, `policy_versions`, `unique (lot_id, effective_from)`, `created_by` indexes, `revoke update, delete, truncate … from mypark_app`).
  - **Queries:** `GetTariffVersionAt :one` (`where lot_id = @lot_id and effective_from <= @at order by effective_from desc limit 1`), `GetTariffVersionByID :one`, `InsertTariffVersion :exec` and `InsertPolicyVersion :exec` (`on conflict (lot_id, effective_from) do nothing`), and `GetLotIDByPublicID :one`.
  - `type tariffs.Version struct{ ID, LotID int64; Tariff Tariff }`.
  - `type tariffs.Current interface{ AtTime(ctx context.Context, lotID int64, t time.Time) (Version, error); ByID(ctx context.Context, id int64) (Version, error) }`, implemented by `tariffs.NewService(repo Repo) *Service`, with `tariffs.ErrNoVersion`.
  - `tariffs.SeedDefaults(ctx context.Context, q *dbgen.Queries, lotID int64) error` seeds one tariff row (`DefaultTariff`) and one policy row with the design §5.1 values `30, 7, 20, 15, 10, 1, 5, 3, 30, 7`, both with `effective_from 2026-11-01T00:00:00-04:00`.
  - `lots.ErrNotFound`. `type lots.Resolver interface{ LotIDByPublicID(ctx context.Context, id uuid.UUID) (int64, error) }`, implemented by `lotspg.NewRepo(pool *pgxpool.Pool) *Repo`.
  - `tariffs.NewHandler(cur Current, lots lots.Resolver, clk clock.Clock, log *slog.Logger) *Handler` with `Register(r *router.Router)`. It registers `GET /v1/lots/{id}/tariff` with `Policy{Public: true, Limit: &ratelimit.PublicReadIP}` and returns the design §11.1 `Tariff` JSON. A malformed or unknown id gives `404 lot.not_found`.
  - `testsupport.SeedPilotLot(t testing.TB, db *TestDB) (lotID int64)` runs `lots.Seed` + `tariffs.SeedDefaults` on the App pool.
  - `TestGrants_M2Tables` in `grants_m2_test.go`, with one subtest per migration.

- [ ] **Step 1: Write the failing tests**

```go
func TestGrants_M2Tables(t *testing.T) // subtest "tariffs": as mypark_app select+insert ok on tariff_versions, policy_versions; update and delete → 42501
func TestSeedDefaults_Idempotent(t *testing.T) // run twice → one tariff and one policy row; values 5000/2500/10/15/40000/50000/15000
func TestCurrent_AtTimePicksLatestEffective(t *testing.T) // versions at 2026-11-01 and 2026-12-01 → AtTime(2026-11-30) is the first, AtTime(2026-12-01T00:00:00-04:00) the second; before both → ErrNoVersion
func TestGetTariff_Public(t *testing.T) // via newHandler: 200, data == {effective_from, currency:"DOP", first_hour_cents:5000, additional_hour_cents:2500, exit_grace_minutes:10, post_payment_exit_minutes:15, daily_cap_cents:40000, lost_ticket_cents:50000, reservation_fee_cents:15000}
func TestGetTariff_UnknownLot404(t *testing.T) // random uuid and "zona-colonial" → 404 lot.not_found
func TestSeedCommand_SeedsTariff(t *testing.T) // cmd/seed run twice on a migrated DB → exactly one tariff_versions row for the pilot lot
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/tariffs/... ./internal/infrastructure/database/... ./cmd/...` → FAIL.
- [ ] **Step 3: Implement.** Write the migration, the queries and `sqlc generate`, then the service, handler, seed wiring (after `legal.SeedDocuments`), testsupport and the matrix entry. Remove the pending marker.
- [ ] **Step 4: Run** — `$IT go test -race ./...` and `go tool sqlc diff -f internal/infrastructure/database/sqlc.yaml`. Expected: `ok`, no diff.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add versioned tariffs and policies with public tariff endpoint"
```

---

## Task S3: Devices schema — migration 00008, purge function, fleet seed

Design slice **S3**. **Sensitive (migration, secrets).** Budget: ~390 lines.

**Files:**
- Create: `migrations/00008_devices.sql`, `queries/devices.sql`, `internal/features/devices/{seed.go,purge.go,ports.go,postgres/repo.go}` + tests
- Modify: `dbgen/*`, `grants_m2_test.go`, `internal/config/seed.go` (`LoadSeedFleet`), `cmd/seed/main.go`, `deploy/compose/docker-compose.yml` (seed env: `SECRETBOX_*`, `CARD_UID_PEPPER`, `SIMULATOR_FLEET_SECRET`, `SEED_SIMULATOR_FLEET: ${SEED_SIMULATOR_FLEET:-true}`)

**Interfaces:**
- Consumes: `secretbox.Sealer`, `config.ParseSecretboxKeys` (S1.1), `devicefleet` (S1.2).
- Produces:
  - **Migration:** design §5.2 verbatim.
    - `devices`, `device_events`, `brin (received_at)`, and `revoke update, delete, truncate on device_events`.
    - `purge_device_events(batch int) returns bigint` as `security definer`, `set search_path = ''`, written as `with doomed as (…), gone as (delete … returning 1) select count(*) from gone` with the 12-month floor inside the function.
    - `revoke all … from public` and `grant execute … to mypark_app`.
  - **Queries:** `GetDeviceByPublicID :one`, `TouchDeviceLastSeen :exec` (`… where id = @id and (last_seen_at is null or last_seen_at < @now - interval '30 seconds')`), `GetFleetDevice :one`, `InsertFleetDevice :exec`, `UpdateDeviceSecret :exec`, `GetDeviceEvent :one`, `InsertDeviceEvent :exec` and `PurgeDeviceEvents :one` (`select purge_device_events(@batch)`).
  - `config.LoadSeedFleet(getenv func(string) string) (SeedFleet, error)` with accessors `Enabled() bool`, `SecretboxKeys() (secretbox.Key, []secretbox.Key)`, `CardUIDPepper() []byte` and `FleetSecret() secret.Bytes`. Constants: `EnvSeedSimulatorFleet = "SEED_SIMULATOR_FLEET"` and `EnvSimulatorFleetSecret = "SIMULATOR_FLEET_SECRET"`. Production refuses `DevSimulatorFleetSecret`.
  - `devices.SeedFleet(ctx context.Context, q *dbgen.Queries, s *secretbox.Sealer, lotID int64, fleet secret.Bytes) (SeedResult, error)` with `type SeedResult struct{ Inserted, Resealed int }`. It inserts missing devices. It re-seals and updates when the stored secret differs from the derived one or the row's `secret_key_id` ≠ `s.CurrentKeyID()`. It never logs a secret.
  - `devices.PurgeEvents(ctx context.Context, db database.DBTX, batch int32) (int64, error)`.

- [ ] **Step 1: Write the failing tests**

```go
func TestGrants_M2Tables(t *testing.T) // subtest "devices": app select/insert/update on devices; insert device_events ok; update/delete device_events → 42501;
                                       // purge: migrator inserts rows back-dated 13 and 11 months → app PurgeEvents(…, 100) == 1, the 11-month row remains
func TestDevicesSchema_Checks(t *testing.T) // kind "drone" → 23514; status revoked with revoked_at null → 23514; body_sha256 of 31 bytes → 23514; duplicate (device_id,event_id) → 23505 "device_events_device_id_event_id_key"
func TestSeedFleet_InsertsSixDevicesSealed(t *testing.T) // Inserted 6; every row secret_key_id "k1"; Open(ciphertext) == DeviceSecret(fleet, public_id)
func TestSeedFleet_IdempotentAndReseals(t *testing.T) // second run → {0,0}; new Sealer(k2, k1) → Resealed 6, key id "k2"; new fleet secret → Resealed 6
func TestLoadSeedFleet_ProductionRefusesDevFleetSecret(t *testing.T)
func TestSeedCommand_SeedsFleetWhenEnabled(t *testing.T) // SEED_SIMULATOR_FLEET=true → 6 devices; "false" → 0; output contains no secret bytes (base64 or hex)
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/devices/... ./internal/infrastructure/database/... ./internal/config/... ./cmd/seed/...` → FAIL.
- [ ] **Step 3: Implement** the migration, the queries and `sqlc generate`, then config, seed, purge and the compose env.
- [ ] **Step 4: Run** — `$IT go test -race ./...` and `go tool sqlc diff -f internal/infrastructure/database/sqlc.yaml`. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server deploy/compose/docker-compose.yml
git commit -m "feat(server): add device registry, append-only device events and fleet seed"
```

---

## Task S4: Device authentication — canonical HMAC, `DeviceAuth`, `Policy.Device`

Design slice **S4**. **Sensitive (device signatures).** Budget: ~400 lines.

**Files:**
- Create: `apps/server/internal/devicesig/{devicesig.go,devicesig_test.go,testdata/golden.json}`, `internal/features/devices/{auth.go,auth_test.go}`, `internal/testsupport/devices.go`
- Modify: `internal/domain/authz/authz.go`, `internal/transport/http/router/{router.go,router_test.go}`, `internal/infrastructure/ratelimit/{ratelimit.go,rules.go}`, `internal/transport/http/middleware/{ratelimit.go,accesslog.go}` + tests, `internal/transport/http/reqctx/reqctx.go`, `internal/features/devices/{ports.go,postgres/repo.go}`

**Interfaces:**
- Consumes: S3 device queries, `secretbox.Sealer`, `ratelimit.DeviceIP`, `clock.Clock`.
- Produces:
  - `type authz.Device struct{ ID int64; PublicID uuid.UUID; LotID int64; Kind DeviceKind }`, plus `authz.WithDevice(ctx, Device) context.Context` and `authz.DeviceFrom(ctx) (Device, bool)`.
  - `devicesig`:
    - `Canonical(method, requestURI, timestamp string, body []byte) []byte` = `METHOD\nREQUEST_URI\nX-Timestamp\nhex(sha256(body))`, lowercase hex, no trailing newline.
    - `Sign(secret, canonical []byte) string` (64 lowercase hex).
    - `Verify(secret, canonical []byte, sig string) bool` (`hmac.Equal`).
    - `ParseTimestamp(s string) (time.Time, bool)` accepts only `YYYY-MM-DDTHH:MM:SSZ`: strict RFC 3339, `Z`, no fraction.
    - `testdata/golden.json` holds `{secret_hex:"000102…1f", method:"POST", uri:"/v1/devices/events?trace=1", timestamp:"2026-11-24T14:03:11Z", body:"{\"event_id\":\"019a6d00-0000-7000-8000-0000000000aa\",\"type\":\"heartbeat\",\"occurred_at\":\"2026-11-24T14:03:10Z\",\"payload\":{}}", body_sha256:"7881d77120ebab304ff011636f6782176e4c9fb8715453376ca4ea876d8c61e8", signature:"3e65ccf587e080778fe040d5dd123ea3679f0e902b6fe8e6aca681b7bdec14e0"}`. The S12.1 client test reads the same file.
  - `devices.MaxEventBytes = 16 << 10`.
  - `type devices.Record struct{ ID, LotID int64; PublicID uuid.UUID; Kind authz.DeviceKind; Revoked bool; SecretCiphertext []byte; SecretKeyID string }` and `devices.ErrDeviceNotFound`.
  - `type devices.Registry interface{ DeviceByPublicID(ctx context.Context, id uuid.UUID) (Record, error); TouchLastSeen(ctx context.Context, id int64, now time.Time) error }`.
  - `devices.NewAuthenticator(reg Registry, s *secretbox.Sealer, clk clock.Clock, log *slog.Logger) *Authenticator` with `(*Authenticator).Middleware(next http.Handler) http.Handler`, following design §7.3 steps 1–5 in order.
    - Unknown or revoked devices sign against a fixed 32-byte dummy key.
    - It warns with `claimed_device_id`, `reason` ∈ `headers|skew|unknown|revoked|signature` and `client_ip`.
    - It restores `r.Body` from the read bytes.
  - `reqctx.LogFields` gains `DeviceID string`. `AccessLog` logs `device_id` when it is non-empty.
  - `ratelimit.KeyDevice` (a new `KeyKind`) and `ratelimit.DeviceRule(perSecond int) Rule` = `{Name:"device", Limit:perSecond, Window:time.Second, Key:KeyDevice, Prefix:"dev:", Scope:ScopeLocal}`. `bucketKey` keys `KeyDevice` by `authz.DeviceFrom(ctx).PublicID.String()` and errors without a device.
  - `router.Policy` gains `Device bool`, and `router.Middlewares` gains `DeviceAuth func(http.Handler) http.Handler` and `DeviceLimit ratelimit.Rule`.
    - The chain for `Device` is `ClientIP → RateLimit(ratelimit.DeviceIP) → DeviceAuth → RateLimit(mw.DeviceLimit) → h`.
    - `validate` panics when `Device` is combined with `Public`, `Roles`, `LegalExempt` or `Limit`, when the path is not under `/v1/devices/`, or when `DeviceAuth` is nil. The existing "Public or Roles" rule becomes "exactly one of Public, Roles, Device".
  - `testsupport.TestSealer(t testing.TB) *secretbox.Sealer` and `type testsupport.TestDevice struct{ ID, LotID int64; PublicID uuid.UUID; Kind authz.DeviceKind; Secret []byte }`.
    - `testsupport.NewTestDevice(t testing.TB, db *TestDB, s *secretbox.Sealer, lotID int64, kind authz.DeviceKind) TestDevice`.
    - `(TestDevice).Request(t testing.TB, uri string, body []byte, at time.Time) *http.Request` builds a signed `POST`.

- [ ] **Step 1: Write the failing tests**

```go
func TestCanonical_GoldenVector(t *testing.T) // golden.json → body_sha256 and signature match exactly
func TestParseTimestamp(t *testing.T) // "2026-11-24T14:03:11Z" ok; "2026-11-24T14:03:11.123Z", "2026-11-24T10:03:11-04:00", "2026-11-24 14:03:11Z", "" → false
func TestDeviceAuth_Valid(t *testing.T) // signed request → next sees DeviceFrom == {ID, PublicID, LotID, Kind}; LogFields.DeviceID == public id; handler can read the full body
func TestDeviceAuth_MissingOrMalformedHeaders(t *testing.T) // each header absent, signature 63 hex or uppercase, non-uuid id, fractional timestamp → 401 device.unauthenticated
func TestDeviceAuth_WindowBoundaries(t *testing.T) // skew −60 s and +60 s → 200; ±61 s → 401 device.timestamp_out_of_window, Registry never called
func TestDeviceAuth_UnknownAndRevokedIdenticalBodies(t *testing.T) // both 401 device.unauthenticated; bodies equal minus meta.request_id; dummy-key HMAC computed (counting hook)
func TestDeviceAuth_TamperedQueryOrBody(t *testing.T) // sign "/v1/devices/events", send "?x=1" → 401; flip one body byte → 401
func TestDeviceAuth_BodyCap(t *testing.T) // 16 KiB + 1 → 413 http.payload_too_large
func TestDeviceAuth_TouchLastSeenThrottled(t *testing.T) // two requests 10 s apart → last_seen_at set once; 31 s later → updated
func TestDeviceAuth_LogsReasonNotSecret(t *testing.T) // captured slog: reason "signature", claimed_device_id set; no secret hex in output
func TestHandle_DevicePolicyChainOrder(t *testing.T) // recording middlewares → ["clientip","rl:device_ip","deviceauth","rl:device","handler"]
func TestHandle_PanicsOnBadDevicePolicy(t *testing.T) // Device+Public, Device+Roles, Device+Limit, Device on "POST /v1/lots", nil DeviceAuth → panic
func TestRateLimit_DeviceKeyedAfterAuth(t *testing.T) // DeviceRule(2): third request from device A → 429 Retry-After "1"; device B unaffected; spoofed unsigned X-Device-Id of A never spends A's budget
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/devicesig/... ./internal/features/devices/... ./internal/transport/http/... ./internal/infrastructure/ratelimit/...` → FAIL.
- [ ] **Step 3: Implement** the pieces above. `cmd/api` fills `DeviceAuth` and `DeviceLimit: ratelimit.DeviceRule(cfg.DeviceRateLimitPerS())`. No device route is registered yet (S5.2), so the tests use a router built in the test with a stub handler.
- [ ] **Step 4: Run** — `$IT go test -race ./...`. Expected: `ok`. The M1 router tests are updated for the "exactly one of" rule.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add hmac device authentication and device route policy"
```

---

## Task S5.1: Event decoding, kind matrix, redaction and sealing

Design slice **S5**. **Sensitive (device signatures).** It was split from S5.2 because the pure decoding and the transactional pipeline together come to about 650 lines. Budget: ~380 lines.

**Files:**
- Create: `internal/features/gate/{events.go,result.go,codes.go}` + tests, `internal/features/devices/{envelope.go,matrix.go,redact.go}` + tests

**Interfaces:**
- Consumes: `authz.DeviceKind`, `secretbox.Sealer`, `tariffs.Breakdown`, `errcodes` (C1.1).
- Produces:
  - **Event types:** `type gate.EventType string` with the nine constants `EventPodiumDetected="podium.vehicle_detected"`, `EventEntryCredential="entry.credential_presented"`, `EventBarrierPassed="barrier.passed"`, `EventPayScan="paystation.scan"`, `EventLostTicket="paystation.lost_ticket"`, `EventCashPaid="paystation.cash_paid"`, `EventExitCredential="exit.credential_presented"`, `EventSensorChanged="sensor.space_changed"` and `EventHeartbeat="heartbeat"`. Also `gate.AllEventTypes() []EventType`.
  - **Payload structs** (exported fields, JSON tags per design §7.2):
    - `EntryCredential{Kind string; Value string}` with kinds `ticket_request|employee_card|reservation_qr`.
    - `Credential{Kind string; Value string}` with kinds `ticket|employee_card|reservation_qr`.
    - `BarrierPassed{Direction string; StayID uuid.UUID}`.
    - `PayScan{Credential Credential}`, `LostTicket{StayID uuid.UUID}`, `CashPaid{StayID uuid.UUID; AmountCents int64}`, `ExitCredential{Credential Credential}`.
    - `SensorChanged{SpaceCode string; Occupied bool}` and `Empty{}`.
  - `type gate.Event struct{ ID uuid.UUID; Type EventType; OccurredAt time.Time; Payload any }`.
  - **Result:** `type gate.Outcome string` with `OutcomeAck`, `OutcomeOpen`, `OutcomeDeny` and `OutcomeRejected`. `type gate.Result struct` has JSON fields `result`, `reason,omitempty`, `stay_id,omitempty` (`*uuid.UUID`), `ticket_code,omitempty` (`string`), `status,omitempty`, `amount_cents,omitempty` (`*int64`), `breakdown,omitempty` (`*tariffs.Breakdown`), `receipt_no,omitempty`, `paid_at,omitempty` (`*time.Time`) and `exit_deadline,omitempty` (`*time.Time`).
  - **Credential normalization** (`codes.go`, Review Focus 2):
    - `gate.NormalizeTicketCode(s string) (string, bool)`: uppercase, drop `-` and spaces, map `I`/`L`→`1` and `O`→`0`. The result must be exactly 16 Crockford characters.
    - `gate.NormalizeCardUID(s string) (string, bool)`: uppercase hex, drop `:` `-` and spaces, 8–20 hex characters.
  - **Decoding:** `devices.DecodeEvent(raw []byte, now time.Time) (gate.Event, error)`.
    - The envelope and the per-type payload are decoded with `json.Decoder.DisallowUnknownFields`.
    - Rules: `event_id` required and a uuid; `type` one of the nine; `occurred_at` RFC 3339 and ≤ now + 60 s; `payload` required, a JSON object (never `null`); `value` ≤ 512 and required except for `ticket_request`; `space_code` `^E[0-9]{3}$`; `amount_cents` an integer in 1–10_000_000 (a 100 000 RD$ sanity ceiling, plan decision); `direction` `entry|exit`.
    - Errors: malformed JSON or a wrong JSON type → `400 http.invalid_json`. A rule failure or an unknown field → `422 validation.failed` with `details.fields[{field,rule}]`, rule `unknown_field` for unknown keys, never echoing a value.
  - `devices.Allowed(kind authz.DeviceKind, ev gate.Event) bool` implements design §7.2/§7.5, including the sub-rules (dispenser only `ticket_request`; entry barrier only `employee_card`/`reservation_qr` and `direction: entry`; exit barrier only `direction: exit`; `heartbeat` for any kind).
  - `devices.RedactPayload(ev gate.Event) (json.RawMessage, error)` replaces `payload.value` and `payload.credential.value` with `"sha256:" + first 12 hex of sha256(value)`.
  - `devices.SealResult(s *secretbox.Sealer, r gate.Result) (json.RawMessage, error)` replaces a non-empty `ticket_code` with `"sealed":{"key_id","box"}` (box in base64). `devices.OpenResult(s *secretbox.Sealer, stored json.RawMessage) (json.RawMessage, error)` restores `ticket_code`.

- [ ] **Step 1: Write the failing tests**

```go
func TestDecodeEvent_EveryTypeValid(t *testing.T) // one valid body per type → Event.Payload has the concrete struct type
func TestDecodeEvent_EnvelopeRules(t *testing.T) // missing event_id, event_id "abc", type "door.opened", occurred_at now+61s → 422 with field names; occurred_at now+60s and now−3h → ok
func TestDecodeEvent_NestedJunk(t *testing.T) // (Review Focus 3) payload null; {"payload":{"kind":"ticket_request","x":1}}; credential {"kind":"ticket","value":"A","extra":true} → 422 rule unknown_field;
                                              // amount_cents 5000.5, "5000" → 400 http.invalid_json; 1e20, 0, -1 → 422; value of 513 chars → 422; space_code "e001" → 422; never a panic
func TestKindMatrix(t *testing.T) // full table: 6 kinds × 9 types + sub-rules, e.g. (dispenser, entry employee_card) false; (entry_barrier, barrier exit) false; (sensor_gateway, paystation.scan) false; (pay_station, heartbeat) true
func TestRedactPayload(t *testing.T) // credential value "DFFE6EADBA0A4C" → "sha256:a923af4c08c9"; ticket_request without value unchanged
func TestSealResult_RoundTrip(t *testing.T) // Result{ticket_code:"7K3M…"} → stored JSON has no "ticket_code" key and no code substring; OpenResult → identical JSON to json.Marshal(result)
func TestNormalizeTicketCode(t *testing.T) // (Review Focus 2) "7k3m-9qzx-0abc-1def" → "7K3M9QZX0ABC1DEF"; "7K3M9QZXOABCLDEF" → "7K3M9QZX0ABC1DEF"; 15 chars, "U" (not Crockford) → false
func TestNormalizeCardUID(t *testing.T) // "df:fe:6e:ad:ba:0a:4c" → "DFFE6EADBA0A4C"; "xyz" → false
```

- [ ] **Step 2: Run to verify they fail** — `go test -race ./internal/features/gate/... ./internal/features/devices/...` → FAIL.
- [ ] **Step 3: Implement.** Decode the envelope into a struct with `Payload json.RawMessage`, then decode the payload by type with a second strict decoder.
- [ ] **Step 4: Run** — same command. Expected: PASS.
- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/features/gate apps/server/internal/features/devices
git commit -m "feat(server): add strict device event decoding, kind matrix and response sealing"
```

---

## Task S5.2: Event pipeline — idempotent transaction, heartbeat, podium, sensor

Design slice **S5**. **Sensitive (device signatures).** Budget: ~400 lines.

**Files:**
- Create: `internal/features/devices/http.go`, `internal/features/devices/postgres/events.go`, `internal/features/gate/{service.go,sensor.go,ports.go,postgres/repo.go}`, `queries/gate.sql`, `cmd/api/device_events_test.go`
- Modify: `dbgen/*`, `cmd/api/{main.go,authz_matrix_test.go}`, `api/openapi.yaml` (remove the pending marker on `postDeviceEvent`)
- `authz_matrix_test.go` changes:
  - Add the device principals.
  - Add an `expected()` branch for `p.Device`, placed **before** the Roles check, so a user bearer gets 401 and not 403.
  - `TestRegistryMatchesSpecPolicies` sets `got = "device"` when `rt.Policy.Device`, and the `specPolicy` entry for the route is `"device"`.

**Interfaces:**
- Consumes: `DeviceAuth`/`Policy.Device` (S4), `DecodeEvent`/`Allowed`/`RedactPayload`/`SealResult`/`OpenResult` (S5.1), `database.TxRunnerPort`, `MustTx`.
- Produces:
  - `type gate.Handler interface{ Handle(ctx context.Context, d authz.Device, ev Event) (Result, error) }`. It must run inside the transaction carried by ctx.
  - `gate.NewService(d Deps) *Service` with `type gate.Deps struct{ Repo Repo; Clock clock.Clock; Log *slog.Logger }`. Later tasks add fields: `Tariffs tariffs.Current`, `Holds availability.ReservationHolds`, `Audit audit.Recorder`, `CardPepper []byte`, `Rand io.Reader`.
    - It handles `heartbeat` and `podium.vehicle_detected` (`ack`) and `sensor.space_changed` (design §8.3).
    - Dispatch goes through an unexported `(*Service).handlerFor(t EventType)` lookup. Any type or direction not yet handled returns `gate.ErrUnhandledEvent` (500, nothing stored) until its slice lands. S9.2 deletes that error.
  - **Sensor queries:** `UpdateSpaceFromSensor :one` (design §8.3 update, `returning code`), `SpaceExists :one` and `NotifySpaceChanged :exec` (`select pg_notify('space_changed', @payload)`, with payload `lot_id:code:state`).
  - `type devices.Store interface{ FindEvent(ctx context.Context, deviceID int64, eventID uuid.UUID) (StoredEvent, bool, error); InsertEvent(ctx context.Context, e NewEvent) error }`.
    - `type StoredEvent struct{ BodySHA256 []byte; Response json.RawMessage }` and `NewEvent` holds all `device_events` columns.
    - `devices.ErrDuplicateEvent` maps from `23505` on `device_events_device_id_event_id_key`.
  - `devices.NewHandler(d HandlerDeps) *Handler` with `type HandlerDeps struct{ Tx database.TxRunnerPort; Store Store; Gate gate.Handler; Sealer *secretbox.Sealer; Clock clock.Clock; Log *slog.Logger }` and `Register(r *router.Router)`, which registers `POST /v1/devices/events` with `router.Policy{Device: true}`.
  - **Flow** (design §4.3, §7.4):
    1. `DecodeEvent` and `Allowed` (403 `device.event_not_allowed`).
    2. The pre-check: same hash → 200 with the stored response (opened) and `Idempotent-Replayed: true`; different hash → 409 `device.event_conflict`.
    3. `InTx`: `set local statement_timeout = '2s'` and `set local lock_timeout = '1s'`, then `gate.Handle`, then `InsertEvent` **last**.
    4. On `ErrDuplicateEvent`, re-run the pre-check outside the failed transaction.
  - Only 200 outcomes are stored.

- [ ] **Step 1: Write the failing tests**

```go
// cmd/api/device_events_test.go (through newHandler, real DB, fleet-like TestDevice per kind)
func TestDeviceEvents_HeartbeatAck(t *testing.T) // 200 data == {"result":"ack"}; one device_events row with body_sha256 == sha256(raw body), payload redacted
func TestDeviceEvents_ReplayReturnsStoredResponse(t *testing.T) // same body twice → identical data, second has Idempotent-Replayed "true", one row
func TestDeviceEvents_ReplayAfter90sFreshSignature(t *testing.T) // FakeClock +90 s, re-signed same body → stored response
func TestDeviceEvents_SameIDDifferentBody409(t *testing.T) // → 409 device.event_conflict, still one row
func TestDeviceEvents_ConcurrentDuplicates(t *testing.T) // 20 goroutines, same sensor event → 20 × 200 identical data, 1 row, 1 space update, no deadlock (ctx timeout 10 s)
func TestDeviceEvents_KindMatrix403StoresNothing(t *testing.T) // sensor_gateway sends paystation.scan → 403 device.event_not_allowed, 0 rows
func TestDeviceEvents_ValidationStoresNothing(t *testing.T) // 422 → 0 rows; repeat → same 422
func TestSensor_ChangesAndNotifies(t *testing.T) // LISTEN space_changed on a side conn; E001 occupied → ack, spaces.status "occupied", one notification "<lot>:E001:occupied"; repeat (new event_id) → ack, no notification
func TestSensor_UnknownCodeRejected(t *testing.T) // E999 → {"result":"rejected","reason":"lot.space_unknown"}
func TestSensor_NeverOverwritesOutOfService(t *testing.T) // E002 out_of_service; occupied event → ack, status unchanged, no notification
func TestSensor_OccupancyVersionUntouched(t *testing.T) // lot_occupancy.version unchanged after 5 sensor events
// authz_matrix_test.go: device rows — anonymous → 401 device.unauthenticated; valid user bearer → 401 device.unauthenticated; device allowed type → 200; disallowed → 403 device.event_not_allowed
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./cmd/api/... ./internal/features/...` → FAIL.
- [ ] **Step 3: Implement** the pieces above. Wire `newHandlerWithRoutes`: the device handler, `gate.NewService`, `devicespg` store and registry, and the sealer from `cfg.SecretboxKeys()`.
- [ ] **Step 4: Run** — `$IT go test -race ./...` and `go tool sqlc diff -f internal/infrastructure/database/sqlc.yaml`. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add idempotent device event pipeline with sensor updates"
```

---

## Task S6: Gate schema — migration 00009, cards seed

Design slice **S6**. **Sensitive (migration).** Depends on **S2.2 and S3**: `stays.tariff_version_id` references `tariff_versions`, and `payments.device_id` references `devices`. Budget: ~380 lines.

**Files:**
- Create: `migrations/00009_stays_payments.sql`, `internal/features/gate/seed.go` + test
- Modify: `queries/gate.sql`, `dbgen/*`, `grants_m2_test.go`, `cmd/seed/main.go`, `docs/architecture.md` §11.1 (`accepted_at`, `voided_at`, `exit_authorized_at`, `body_sha256` and `secret_key_id`; no `amount_due_cents`; the minimal `payments`)

**Interfaces:**
- Consumes: `devicefleet.Cards`/`CardUID` (S1.2), `config.SeedFleet` (S3).
- Produces:
  - **Migration:** design §5.3/§5.4 verbatim. That is `employee_cards`, `stays` (all checks, including the state-consistency checks), `payments` (final enums per M2C-41) and every listed index (`stays_open_idx`, `stays_pending_idx`, `stays_card_open_key`, the FK indexes, `payments_zero_per_stay_key`).
  - **Queries** (later tasks add to them):
    - `InsertStay :one` (returns `id, public_id`), `GetStayForUpdate :one` (`… where lot_id = @lot_id and public_id = @public_id for update`)
    - `GetStayByTicketHashForUpdate :one`, `GetOpenStayByCardForUpdate :one`
    - `InsertPayment :one` (returns `id`), `PaymentTotals :one` (`sum(amount_cents)`, `max(succeeded_at)` for `status = 'succeeded'`)
    - `GetActiveCardByHash :one` (`… where lot_id = @lot_id and card_uid_hash = @hash and status = 'active'`), `InsertEmployeeCard :exec` (`on conflict (card_uid_hash) do nothing`)
  - `gate.HashCardUID(pepper []byte, uid string) []byte` = HMAC-SHA256(pepper, normalized uid).
  - `gate.SeedCards(ctx context.Context, q *dbgen.Queries, lotID int64, fleet secret.Bytes, pepper []byte) (inserted int, err error)` inserts all 31 cards and revokes `EMP-031` (`status 'revoked'`, `revoked_at = 2026-11-01T00:00:00-04:00`).

- [ ] **Step 1: Write the failing tests**

```go
func TestGrants_M2Tables(t *testing.T) // subtest "gate": as mypark_app select, insert and update succeed on employee_cards, stays and payments (design §5.5)
func TestStaysSchema_StateChecks(t *testing.T) // as app: status 'active' with entered_at null → 23514; 'paid' without exit_deadline_at → 23514; ticket kind without ticket_code_hash → 23514; lost_ticket on employee_card → 23514
func TestStaysSchema_OneOpenStayPerCard(t *testing.T) // two entry_pending stays same card → 23505 stays_card_open_key; first exited → second insert ok
func TestPaymentsSchema(t *testing.T) // method none with amount 100 → 23514; cash with 0 → 23514; two 'none' rows same stay → 23505 payments_zero_per_stay_key; two cash rows ok
func TestSeedCards_IdempotentAndRevoked(t *testing.T) // 31 rows, EMP-031 revoked; second run inserts 0; card_uid_hash == HashCardUID(pepper, CardUID(fleet,"EMP-007"))
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/gate/... ./internal/infrastructure/database/...` → FAIL.
- [ ] **Step 3: Implement** the migration, the queries and `sqlc generate`, the seed and its wiring (after the fleet seed, when `SeedFleet.Enabled()`), and the architecture §11.1 update.
- [ ] **Step 4: Run** — `$IT go test -race ./...` and `go tool sqlc diff -f internal/infrastructure/database/sqlc.yaml`. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server docs/architecture.md
git commit -m "feat(server): add stays, employee cards and minimal payments schema"
```

---

## Task S7: Entry — decision under lock, ticket issue, employee card, QR deny

Design slice **S7**. **Sensitive (capacity).** Budget: ~400 lines.

**Files:**
- Create: `internal/features/availability/{domain.go,domain_test.go,ports.go}`, `internal/features/gate/{entry.go,entry_test.go}`, `cmd/api/entry_test.go`
- Modify: `queries/gate.sql` (`LockLotOccupancy`, `CountOutOfService`, `CountFreshPending`), `dbgen/*`, `internal/features/gate/{service.go,codes.go}`, `internal/features/audit/audit.go`, `cmd/api/main.go`

**Interfaces:**
- Consumes: `tariffs.Current` (S2.2), S6 queries, `gate.NormalizeCardUID`/`HashCardUID`, `audit.Recorder`.
- Produces:
  - `type availability.AdmitInput struct{ LotClosed bool; Capacity, OutOfService, Occupied, Pending, Holds int }` and `availability.CanAdmitDriveUp(in AdmitInput) (bool, errcodes.Code)` (design §9.2: closed → `GateLotClosed`; `effective − (occupied+pending) − holds ≤ 0` → `GateLotFull`).
  - `type availability.ReservationHolds interface{ EffectiveHolds(ctx context.Context, lotID int64, now time.Time) (int, error); UpcomingArrivals(ctx context.Context, lotID int64, now time.Time, within time.Duration) (int, error) }` and `availability.NoReservations{}`, which returns 0, 0 (M2C-1).
  - `gate.NewTicketCode(r io.Reader) (string, error)`: 10 bytes → 16 Crockford base32 characters. `gate.HashTicketCode(code string) []byte` = sha256 of the normalized code.
  - **Queries:**
    - `LockLotOccupancy :one` (design §9.2 first select, `for update of o`)
    - `CountOutOfService :one`
    - `CountFreshPending :one` (`… accepted_at > @now - interval '60 seconds'`)
  - `gate.Deps` gains `Tariffs tariffs.Current`, `Holds availability.ReservationHolds`, `Audit audit.Recorder`, `CardPepper []byte` and `Rand io.Reader`.
  - `audit.GateEmployeeEntry = "gate.employee_entry"` and `audit.GateLateEntryPass = "gate.late_entry_pass"`.

- [ ] **Step 1: Write the failing tests**

```go
func TestCanAdmitDriveUp(t *testing.T) // table: {cap 280, oos 0, occ 279, pend 0} → true; {occ 279, pend 1} → false GateLotFull; {cap 280, oos 2, occ 278} → false; closed with room → false GateLotClosed (closed wins over full); holds 1 at occ 279 → false
func TestNewTicketCode(t *testing.T) // fixed reader bytes 0x00…0x09 → "000G40R40M30E209"; output always 16 chars of the Crockford alphabet; NormalizeTicketCode(code) == code
// cmd/api/entry_test.go (dispenser and entry_barrier TestDevices, FakeClock)
func TestEntry_TicketIssue(t *testing.T) // ticket_request → {result:"open", stay_id, ticket_code (16 chars)}; stay entry_pending, accepted_at == clock now, tariff_version_id == current; ticket_code_hash == sha256(code); device_events.response has no ticket_code
func TestEntry_ConcurrentLastSpace(t *testing.T) // occupied 279 → 20 parallel ticket_request (distinct event ids) → exactly 1 open, 19 deny gate.lot_full
func TestEntry_PendingWindow(t *testing.T) // occupied 279 + one entry_pending accepted 61 s ago (not voided) → next ticket_request opens
func TestEntry_LotClosedDenied(t *testing.T) // lots.status 'closed' → deny gate.lot_closed
func TestEntry_ReservationQRDenied(t *testing.T) // entry_barrier reservation_qr → deny gate.credential_invalid; no lot lock taken (lock held by a side tx does not block: answers within 1 s)
func TestEntry_EmployeeCard(t *testing.T) // EMP-007 UID (lowercase with colons) → open, no ticket_code; audit_log row gate.employee_entry actor system with after.employee_card_id == card public id
func TestEntry_EmployeeCardRevokedOrUnknown(t *testing.T) // EMP-031 → deny gate.credential_invalid; unknown UID → same
func TestEntry_EmployeeCardInUse(t *testing.T) // second entry same card → deny gate.credential_in_use
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/availability/... ./internal/features/gate/... ./cmd/api/...` → FAIL.
- [ ] **Step 3: Implement** `gate` entry handling in the lock order `LockLotOccupancy → counts → InsertStay`. Wire `NoReservations{}`, `cfg.CardUIDPepper()` and `crypto/rand.Reader` in `cmd/api`.
- [ ] **Step 4: Run** — `$IT go test -race ./...`. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add gate entry decisions under the occupancy lock"
```

---

## Task S8: Barrier pass, stay state machine, occupancy NOTIFY, void job

Design slice **S8**. Budget: ~380 lines.

**Files:**
- Create: `internal/features/gate/{state.go,state_test.go,pass.go,void.go,void_test.go}`, `internal/testsupport/gate.go`, `cmd/api/pass_test.go`
- Modify: `queries/gate.sql` (`BumpOccupancy`, `NotifyLotChanged`, `VoidUnpassed`), `dbgen/*`, `cmd/worker/{main.go,main_test.go}`, `docs/spec.md` §5.4 (`voided → active` on a late pass), `docs/architecture.md` §10 (void job)

**Interfaces:**
- Consumes: S7 stays, `audit.GateLateEntryPass`.
- Produces:
  - `type gate.Status string` with `StatusEntryPending`, `StatusActive`, `StatusPaid`, `StatusExited` and `StatusVoided`.
  - `gate.CanTransition(from, to Status) bool`. Allowed: pending→active, pending→voided, voided→active, active→paid, paid→active (lazy overstay or lost ticket), paid→exited. Everything else is rejected, including active→exited (it has to go through paid, design §10.5).
  - **Queries:**
    - `BumpOccupancy :one` (`update lot_occupancy set occupied = occupied + @delta, version = version + 1, updated_at = @now where lot_id = @lot_id returning version`)
    - `NotifyLotChanged :exec` (`pg_notify('lot_changed', lot_id:version)`)
    - `VoidUnpassed :execrows` (design §12.5 with `@now`, limit 200, `for update skip locked`)
  - `gate.VoidUnpassedEntries(ctx context.Context, db database.DBTX, now time.Time) (int64, error)` repeats while 200 rows are voided. Worker job `void_unpassed_entries` runs every `15 * time.Second`.
  - `testsupport.AssertOccupancyInvariant(t testing.TB, db *pgxpool.Pool, lotID int64)` checks `occupied = count(status in ('active','paid'))`.
  - Exit passes are **not** handled here (S9.2): `direction: exit` keeps returning `ErrUnhandledEvent`.

- [ ] **Step 1: Write the failing tests**

```go
func TestCanTransition_FullTable(t *testing.T) // all 25 (from,to) pairs; exactly the 6 allowed are true
// cmd/api/pass_test.go
func TestPass_EntryActivatesAndNotifies(t *testing.T) // entry_pending → ack; status active, entered_at == now; occupied +1; version +1; one lot_changed "<lot>:<version>"; invariant holds
func TestPass_DuplicateEntry(t *testing.T) // second entry pass (new event id) → {result:"ack", reason:"gate.duplicate_pass"}; occupied unchanged
func TestPass_LateEntryOnVoided(t *testing.T) // voided stay → ack, active, voided_at kept, occupied +1; audit gate.late_entry_pass; warn log
func TestPass_LateEntryCardConflict(t *testing.T) // voided card stay + newer open stay same card → rejected gate.credential_in_use, nothing changes
func TestPass_UnknownOrOtherLotStay(t *testing.T) // random stay_id and a second lot's stay → rejected gate.stay_unknown
func TestVoidUnpassed_FakeClock(t *testing.T) // 3 pending at T−61 s, 1 at T−30 s → VoidUnpassedEntries(T) == 3; statuses voided, voided_at == T; occupancy and version unchanged; no NOTIFY
func TestVoidUnpassed_Batches(t *testing.T) // 450 stale pending → returns 450 (3 batches)
func TestWorkerJobs_IncludeVoid(t *testing.T) // workerJobs(...) has "void_unpassed_entries" every 15 s
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/gate/... ./cmd/...` → FAIL.
- [ ] **Step 3: Implement** the entry pass in the lock order `LockLotOccupancy → GetStayForUpdate → BumpOccupancy → NotifyLotChanged`, then the void job, the worker wiring and the docs.
- [ ] **Step 4: Run** — `$IT go test -race ./...`. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server docs
git commit -m "feat(server): count barrier passes, notify occupancy and void unpassed entries"
```

---

## Task S9.1: Pay station — scan, lost ticket, cash

Design slice **S9**. **Sensitive (payments).** It was split from S9.2 because the four money flows plus the exit pass come to about 650 lines. Budget: ~400 lines.

**Files:**
- Create: `internal/features/gate/{paystation.go,amount.go}` + tests, `cmd/api/paystation_test.go`
- Modify: `queries/gate.sql` (`SetStayPaid`, `ReactivateStay`, `MarkStayLost`), `dbgen/*`

**Interfaces:**
- Consumes: `tariffs.AmountDue` (S2.1), `tariffs.Current.ByID` (S2.2), `CanTransition` (S8), S6 payment queries, `NormalizeTicketCode`/`HashTicketCode`.
- Produces:
  - `gate.ReceiptNo(paymentID int64) string`, e.g. `ReceiptNo(42) == "MP-00000042"`.
  - `gate.amountDue(ctx, stay stayRow, now time.Time) (int64, tariffs.Breakdown, error)` (unexported, shared by scan, lost, cash and exit). It builds `tariffs.Stay` from the stay row plus `PaymentTotals`, and loads the stay's tariff version with `ByID`.
  - The lazy `paid → active` transition (M2C-35) is applied by one helper, `reactivateIfLapsed`, before pricing, in the same transaction.
  - **Queries:**
    - `SetStayPaid :exec` (`status 'paid'`, `paid_at`, `exit_deadline_at`)
    - `ReactivateStay :exec` (`paid → active`, keeps `exit_deadline_at`)
    - `MarkStayLost :exec` (`lost_ticket true`, new `ticket_code_hash`, status active)
  - **Flows** per design §10.1–§10.3: scan → `{ack, stay_id, status, amount_cents, breakdown}`; lost → `{ack, stay_id, ticket_code, amount_cents, breakdown}`; cash → `{ack, receipt_no, amount_cents, paid_at, exit_deadline}`. Payments use idempotency key `cash:<device public_id>:<event_id>`.

- [ ] **Step 1: Write the failing tests** — FakeClock, a dispenser/entry/pay_station TestDevice set, and the stay entered at `t0` through the API.

```go
func TestReceiptNo(t *testing.T) // 42 → "MP-00000042"; 123456789 → "MP-123456789"
func TestScan_PricesActiveTicket(t *testing.T) // scan at t0+1h00m01s → amount_cents 7500, breakdown.kind "time"; stay unchanged
func TestScan_NormalizedTicketFindsStay(t *testing.T) // (Review Focus 2) lowercase + hyphenated code, and O→0 / L→1 aliases → same stay_id, 200 ack
func TestScan_UnknownOrReservationRejected(t *testing.T) // unknown code → rejected gate.credential_invalid; reservation_qr → same; a second lot's code → same
func TestCash_PaysAndSetsWindow(t *testing.T) // scan 7500 then cash_paid 7500 → receipt_no "MP-%08d", paid_at == now, exit_deadline == now+15m; stay paid; one payments row cash/succeeded/key "cash:<device>:<event>"
func TestCash_AmountMismatch(t *testing.T) // cash 5000 when due 7500 → rejected gate.amount_mismatch amount_cents 7500; no payment
func TestCash_NothingDue(t *testing.T) // inside grace (t0+5m) → rejected gate.nothing_due; paid stay inside its window → same
func TestOverstay_LazyReactivationAndSecondPayment(t *testing.T) // pay at t0+1h30 → scan at t0+1h50 → stay active, exit_deadline kept, amount 5000 kind "overstay", billed_from == t0+1h30; cash 5000 → paid again, deadline t0+2h05; 2 payments
func TestLostTicket_ReplacesCodeAndPrices(t *testing.T) // lost at t0+3h → ticket_code differs, amount 50000 kind "lost_ticket"; old code scan → rejected gate.credential_invalid; new code scan → 50000
func TestLostTicket_OnPaidStay_L7(t *testing.T) // paid 7500 at t0+1h30, lost at t0+1h40 → stay active, amount 42500
func TestLostTicket_AfterDeadline_O13(t *testing.T) // paid 7500 at t0+1h30, lost at t0+2h → amount 42500 kind "lost_ticket"
func TestLostTicket_EmployeeStayRejected(t *testing.T) // card stay id → rejected gate.stay_unknown
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/gate/... ./cmd/api/...` → FAIL.
- [ ] **Step 3: Implement** in the lock order `GetStay…ForUpdate → PaymentTotals → InsertPayment` (no lot lock). `cmd/api` adds the `Tariffs` dependency.
- [ ] **Step 4: Run** — `$IT go test -race ./...`. Expected: `ok`, and the invariant holds after every test.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add pay-station scan, lost ticket and cash payment with overstay billing"
```

---

## Task S9.2: Exit — credential decision, exit pass, zero payment

Design slice **S9**. **Sensitive (payments).** Budget: ~330 lines.

**Files:**
- Create: `internal/features/gate/exit.go` + test, `cmd/api/exit_test.go`
- Modify: `queries/gate.sql` (`AuthorizeExit`, `SetStayExited`), `dbgen/*`, `internal/features/gate/{pass.go,service.go}`

**Interfaces:**
- Consumes: `amountDue`/`reactivateIfLapsed` (S9.1), `BumpOccupancy`/`NotifyLotChanged` (S8).
- Produces:
  - **Queries:** `AuthorizeExit :exec` (`exit_authorized_at = @now`) and `SetStayExited :exec` (`paid_at` kept or set, `exited_at`, `status 'exited'`).
  - **Exit credential** (design §10.4): `{open, stay_id}`, or `{deny, gate.payment_required, stay_id, amount_cents}`, or `{deny, gate.credential_invalid}`.
  - **Exit pass** (design §9.6, §10.5): under the lot lock.
    - `active` with a fresh authorization: insert payment `(parking_fee, none, 0, succeeded, "zero:<stay public_id>")`, then `active → paid → exited` in one transaction.
    - `paid`: → `exited`.
    - Both: occupancy −1, version +1, NOTIFY.
  - After this task `gate.Service.Handle` has a case for every `gate.AllEventTypes()` entry and `ErrUnhandledEvent` is deleted.

- [ ] **Step 1: Write the failing tests**

```go
func TestHandle_EveryTypeHandled(t *testing.T) // for each AllEventTypes(): (*Service).handlerFor(t) (unexported dispatch lookup) is non-nil; ErrUnhandledEvent is deleted, so the build proves no caller relies on it
func TestExit_PaidInsideWindowOpens(t *testing.T) // paid at t0+1h, exit cred at t0+1h10 → open; exit pass → ack, exited, occupied −1, version +1, NOTIFY; no new payment
func TestExit_GraceZeroPayment(t *testing.T) // exit cred at t0+5m → open; pass at t0+5m30s → one payment method "none" amount 0 key "zero:<stay>", stay exited, paid_at == exited_at
func TestExit_DecisionAtAuthorization(t *testing.T) // M2C-32: exit cred at t0+9m58s → open; pass at t0+10m02s → still RD$0, exited
func TestExit_PaymentRequired(t *testing.T) // exit cred at t0+2h unpaid → deny gate.payment_required amount_cents 7500, stay_id set
func TestExit_O8FlowAgreement(t *testing.T) // paid 7500 at t0+1h30; scan at t0+1h50 (active, deadline kept); exit cred at t0+1h50 → deny gate.payment_required 5000; cash_paid 5000 at t0+1h50 → ack
func TestExitPass_NotAuthorized(t *testing.T) // no exit cred, or authorized 61 s ago → rejected gate.exit_not_authorized; nothing changes
func TestExitPass_DuplicateAndUnknown(t *testing.T) // second exit pass → ack gate.duplicate_pass; entry_pending/voided/unknown stay → rejected gate.stay_unknown
func TestExit_EmployeeCard(t *testing.T) // card stay at t0+30h → exit cred open; pass → zero payment, exited
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/gate/... ./cmd/api/...` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — `$IT go test -race ./...`. Expected: `ok`, and the invariant holds.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add exit decisions, exit pass and zero-amount payments"
```

---

## Task S10: Availability REST — lots, availability, spaces

Design slice **S10**. It can start after S6 in parallel with S7–S9. Budget: ~400 lines.

**Files:**
- Create: `internal/features/lots/{service.go,http.go,mapurl.go}` + tests, `internal/features/availability/{service.go,http.go,postgres/repo.go}` + tests, `queries/availability.sql`
- Modify: `queries/lots.sql`, `dbgen/*`, `internal/features/availability/domain.go` (`Compute`), `cmd/api/{main.go,authz_matrix_test.go}`, `api/openapi.yaml` (remove the four pending markers)

**Interfaces:**
- Consumes: `lots.Resolver` (S2.2), `ReservationHolds` (S7), `ratelimit.PublicReadIP`.
- Produces:
  - `type availability.SnapshotInput struct{ LotPublicID uuid.UUID; Capacity, OutOfService, Occupied, Holds, UpcomingArrivals2h int; LotClosed bool; Version int64; UpdatedAt time.Time }`.
  - `type availability.Snapshot struct{…}`, with the JSON of design §11.1 `Availability`.
  - `availability.Compute(in SnapshotInput) Snapshot` follows design §11.2. The floor is 0, and the status precedence is `closed > full > open`.
  - `availability.NewService(repo Repo, holds ReservationHolds, clk clock.Clock) *Service` with:
    - `(*Service).Snapshot(ctx context.Context, lotID int64) (Snapshot, error)`, one query for lot + occupancy + out-of-service count;
    - `(*Service).Spaces(ctx context.Context, lotID int64) ([]SpaceState, error)`, ordered by code;
    - `type SpaceState struct{ Code string \`json:"code"\`; State string \`json:"state"\` }`.
  - `lots.MapURL(lat, lng *float64, address string) string` returns `https://www.google.com/maps/search/?api=1&query=<url.QueryEscape("lat,lng")>` when both are set, otherwise the escaped address.
  - `lots.NewService(repo) *Service` provides `List(ctx) ([]Summary, error)` and `Get(ctx, id uuid.UUID) (Lot, error)`.
  - **Routes** (all `Policy{Public: true, Limit: &ratelimit.PublicReadIP}`): `GET /v1/lots`, `GET /v1/lots/{id}`, `GET /v1/lots/{id}/availability` and `GET /v1/lots/{id}/spaces`.

- [ ] **Step 1: Write the failing tests**

```go
func TestCompute(t *testing.T) // {cap 280, oos 0, occ 268} → available 12, effective 280, open; {cap 280, oos 3, occ 277} → 0, full; {occ 300} → available 0 (floor); closed with room → closed; holds 5 at occ 270 → 5
func TestMapURL(t *testing.T) // nil coords, address "Calle Arzobispo Meriño No. 386, Zona Colonial, Santo Domingo" → query=Calle+Arzobispo+Meri%C3%B1o+No.+386%2C+Zona+Colonial%2C+Santo+Domingo; lat 18.4735 lng −69.8849 → query=18.4735%2C-69.8849
func TestListLots(t *testing.T) // via newHandler: data == [{id, name:"Parqueo Banreservas Zona Colonial", slug:"zona-colonial"}], meta.page.next_cursor null
func TestGetLot(t *testing.T) // capacity 280, opening_hours {"always_open":true}, timezone "America/Santo_Domingo", status "open", latitude null, map_url by address
func TestAvailability_Snapshot(t *testing.T) // after one entry pass → occupied 1, available 279, version 1, updated_at == pass time, effective_holds 0, upcoming_reservations_2h 0
func TestAvailability_PendingDoesNotChangeDisplay(t *testing.T) // one entry_pending → available still 280
func TestSpaces_280OrderedNoPrivateData(t *testing.T) // 280 items E001…E280; keys exactly {code,state}
func TestLots_GarbageIDIs404(t *testing.T) // (Review Focus 4) "zona-colonial", "123", random uuid on /{id}, /availability, /spaces, /tariff → 404 lot.not_found; bodies equal minus meta.request_id
func TestPublicRead_RateLimited(t *testing.T) // 121st GET in one minute from one IP (FakeClock) → 429 Retry-After
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/lots/... ./internal/features/availability/... ./cmd/api/...` → FAIL.
- [ ] **Step 3: Implement** the service, the handlers and the routes. Add the matrix entries and the `specPolicy` `public` entries.
- [ ] **Step 4: Run** — `$IT go test -race ./...`. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add public lot, availability and space endpoints"
```

---

## Task S11.1: Real-time core — fake timers, LISTEN listener, coalescing hub, SSE writer

Design slice **S11**. It was split from S11.2 because the hub plus the stream endpoint and latency test come to about 750 lines. Budget: ~400 lines.

**Files:**
- Create: `internal/infrastructure/pubsub/{listener.go,listener_test.go}`, `internal/features/availability/{coalescer.go,hub.go}` + tests, `internal/transport/http/sse/{sse.go,sse_test.go}`
- Modify: `internal/infrastructure/clock/clock.go`, `internal/testsupport/{clock.go,clock_test.go}`

**Interfaces:**
- Consumes: `availability.Service.Snapshot`/`Spaces` (S10).
- Produces:
  - **Timers.** `type clock.Timers interface{ AfterFunc(d time.Duration, f func()) Stopper; NewTicker(d time.Duration) Ticker }`, with `type Stopper interface{ Stop() bool }` and `type Ticker interface{ C() <-chan time.Time; Stop() }`. `clock.System` implements it. `*testsupport.FakeClock` implements it too: `Advance` fires every due timer and ticker in time order, synchronously, before returning.
  - **Listener.**
    - `pubsub.New(pool *pgxpool.Pool, log *slog.Logger, h Handlers) *Listener` with `type Handlers struct{ LotChanged func(lotID, version int64); SpaceChanged func(lotID int64, code, state string); Reconnected func() }`.
    - `(*Listener).Run(ctx context.Context) error` acquires and hijacks one connection, runs `LISTEN lot_changed; LISTEN space_changed`, and calls `WaitForNotification` in a loop. On error it reconnects with backoff from 250 ms up to 5 s, then calls `Reconnected`.
    - `(*Listener).Connected() bool`.
    - Malformed payloads are logged at warn and dropped.
  - **Coalescer.** `availability.Coalescer` with `(*Coalescer).Bump(now time.Time) (pushNow bool, trailingAt time.Time)` and `(*Coalescer).Fire(now time.Time) bool`. It does leading + trailing edge at `CoalesceWindow = 250 * time.Millisecond` (M2C-39).
  - **Hub.**
    - `availability.NewHub(src SnapshotSource, t clock.Timers, log *slog.Logger) *Hub`, where `type SnapshotSource interface{ Snapshot(ctx context.Context, lotID int64) (Snapshot, error); Spaces(ctx context.Context, lotID int64) ([]SpaceState, error) }`.
    - `(*Hub).Subscribe(lotID int64, withSpaces bool) (*Subscriber, func())`, plus `LotChanged(lotID, version int64)`, `SpaceChanged(lotID int64, code, state string)`, `Resync()`, `Close()` and `Done() <-chan struct{}`.
    - `(*Subscriber).Availability() <-chan []byte` is a 1-slot, latest-wins channel of payloads encoded once per push. `(*Subscriber).Spaces() <-chan SpacesMsg` with `type SpacesMsg struct{ Full bool \`json:"full"\`; Spaces []SpaceState \`json:"spaces"\` }` has capacity `SpacesQueueCap = 300`. Overflow marks the subscriber for resync, so the next message is `Full`.
  - **SSE writer.** `sse.New(w http.ResponseWriter, writeTimeout time.Duration) (*Writer, error)` sets the design §11.3 headers and errors when the writer cannot flush through `Unwrap`. `(*Writer).Retry(ms int) error`, `(*Writer).Event(name, id string, data []byte) error` and `(*Writer).Comment(text string) error`. Every write calls `http.ResponseController.SetWriteDeadline(now + writeTimeout)` first, then `Flush`. An `errors.Is(err, http.ErrNotSupported)` from `SetWriteDeadline`, as with `httptest.ResponseRecorder`, is non-fatal. Only a writer that cannot `Flush` is fatal.

- [ ] **Step 1: Write the failing tests**

```go
func TestFakeClock_TimersFireOnAdvance(t *testing.T) // AfterFunc(250ms) not fired at +249ms, fired once at +250ms; ticker 15s fires 2× on Advance(30s); Stop prevents firing
func TestCoalescer_LeadingAndTrailing(t *testing.T) // Bump@0 → pushNow; Bump@100ms → trailingAt 250ms; Bump@200ms → no new timer; Fire@250ms → true; Bump@600ms → pushNow
func TestHub_BurstCollapsesToTwoPushes(t *testing.T) // 50 LotChanged within 200 ms of fake time → exactly 2 Snapshot calls (leading + trailing); subscriber reads latest version only
func TestHub_AtMostFourPushesPerSecond(t *testing.T) // LotChanged every 10 ms for 1 s fake → ≤ 4 Snapshot calls
func TestHub_OneQueryPerLotNotPerSubscriber(t *testing.T) // 100 subscribers, one bump → 1 Snapshot call, identical byte slices
func TestHub_SpacesDeltaAndOverflowResync(t *testing.T) // 3 SpaceChanged → 3 deltas Full=false; 301 without reading → next message Full=true with 280 spaces
func TestHub_ResyncSendsFreshSnapshots(t *testing.T) // Resync() → every subscriber gets availability + full spaces
func TestHub_CloseEndsSubscribers(t *testing.T) // Close → Done closed; Subscribe after Close returns a closed subscriber
func TestSSEWriter_Format(t *testing.T) // Event("availability","1042",`{"a":1}`) → "event: availability\nid: 1042\ndata: {\"a\":1}\n\n"; Event with id "" omits the id line; Retry(3000) → "retry: 3000\n\n"; headers Content-Type text/event-stream, Cache-Control no-store, X-Accel-Buffering no
func TestListener_ReceivesAndReconnects(t *testing.T) // $IT: pg_notify('lot_changed','7:42') → LotChanged(7,42); pg_terminate_backend(listener pid) → Connected false then true, Reconnected called once; payload "x" dropped with warn
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/infrastructure/... ./internal/features/availability/... ./internal/transport/http/sse/... ./internal/testsupport/...` → FAIL.
- [ ] **Step 3: Implement.** The hub keeps per-lot state under one mutex. Snapshot queries run outside the mutex, and the hub never blocks on a subscriber.
- [ ] **Step 4: Run** — same command. Expected: `ok`.
- [ ] **Step 5: Commit**

```bash
git add apps/server
git commit -m "feat(server): add listen fan-out hub with coalescing and sse writer"
```

---

## Task S11.2: Availability stream — endpoint, caps, `/readyz`, shutdown, latency evidence

Design slice **S11**. Budget: ~400 lines.

**Files:**
- Create: `internal/features/availability/{stream.go,caps.go}` + tests, `cmd/api/stream_test.go`
- Modify: `internal/features/health/{handler.go,handler_test.go}`, `cmd/api/{main.go,authz_matrix_test.go,envelope_test.go}`, `api/openapi.yaml` (remove the pending marker on `streamLotAvailability`)
- Registry tests: `TestAuthzMatrix` and `TestEveryRouteReturnsEnvelope` call every route through `httptest.NewRecorder` with no deadline, so a stream would never end.
  - The matrix entry for the stream pattern builds its request with a `context.WithTimeout` of 200 ms.
  - The envelope test special-cases that pattern. For a known lot it asserts `Content-Type: text/event-stream` and a first frame of `retry: 3000`. For an unknown lot it asserts the 404 envelope., `docs/architecture.md` §6 (snapshot-first, `Last-Event-ID`, coalescing, `include=spaces`)

**Interfaces:**
- Consumes: the Hub, Listener, `sse.Writer` and Timers (S11.1), `lots.Resolver`, `ratelimit.SSEConnectIP`, `config.SSEMaxPerIP`/`SSEMaxStreams`.
- Produces:
  - `availability.NewStreamCaps(perIP, total int) *StreamCaps` with `(*StreamCaps).Acquire(ip netip.Addr) (release func(), ok bool)`. Over the cap gives `429 rate_limit.exceeded` with `Retry-After: 10`, before any `200`.
  - `availability.NewStreamHandler(d StreamDeps) http.HandlerFunc` with `type StreamDeps struct{ Hub *Hub; Service *Service; Lots lots.Resolver; Caps *StreamCaps; Timers clock.Timers; Clock clock.Clock; Log *slog.Logger }`.
    - Constants: `HeartbeatEvery = 15 * time.Second` and `WriteTimeout = 10 * time.Second`.
    - Wire order: `retry: 3000`, the current availability (`id:` = version), then the full spaces only when the raw query is exactly `include=spaces`, then the loop.
    - `Last-Event-ID` is parsed and logged, never trusted, and never an error.
    - Registered as `GET /v1/lots/{id}/availability/stream` with `Policy{Public: true, Limit: &ratelimit.SSEConnectIP}`.
  - `(*health.Handler).WithCheck(name string, fn func(ctx context.Context) bool) *Handler` adds a check to `/readyz` (`listener: ok|fail`, 503 `health.not_ready` on fail).
  - **cmd/api:**
    - `run` starts `listener.Run` in a goroutine and calls `srv.RegisterOnShutdown(hub.Close)`, so streams end before `Shutdown` waits.
    - The pool keeps `DB_MAX_CONNS − 1` for requests.
    - `deps` gains `timers clock.Timers` (`clock.System{}` in production).

- [ ] **Step 1: Write the failing tests**

```go
func TestStreamCaps(t *testing.T) // perIP 2, total 3: ip A ×2 ok, 3rd A false; B ok; C false (total); release → C ok
// cmd/api/stream_test.go (newHandler, httptest.Server, FakeClock timers)
func TestStream_SnapshotFirst(t *testing.T) // first frames: "retry: 3000", then event availability with id == current version
func TestStream_IncludeSpacesFullThenDeltas(t *testing.T) // ?include=spaces → availability, spaces full (280); sensor event → spaces {"full":false,"spaces":[{"code":"E117","state":"occupied"}]} with no id line
func TestStream_PassPushesAvailability(t *testing.T) // entry pass → next availability event version+1, occupied+1
func TestStream_HeartbeatOnFakeTime(t *testing.T) // Advance(15s) → event heartbeat {"as_of": FakeClock now}
func TestStream_GarbageLastEventIDAndInclude(t *testing.T) // (Review Focus 5) Last-Event-ID "abc", "-1", "99999999999999999999"; include=foo, include=spaces,spaces → 200, first event availability, no spaces event
func TestStream_UnknownLot404BeforeStream(t *testing.T) // 404 lot.not_found JSON envelope, not text/event-stream
func TestStream_CapsReturn429(t *testing.T) // SSE_MAX_PER_IP 1: second concurrent stream → 429 Retry-After "10"
func TestStream_OutlivesServerWriteTimeout(t *testing.T) // (Review Focus 1) server from httpserver.New with WriteTimeout set to 300 ms; stream receives heartbeats at fake +15s,+30s,+45s while ≥ 1 s real time passes (wait on frames with a 5 s guard, no sleeps)
func TestStream_EndsOnShutdown(t *testing.T) // Serve + cancel ctx with an open stream → client sees EOF and Serve returns within 1 s
func TestReadyz_ListenerDown(t *testing.T) // listener disconnected → 503 health.not_ready, details.listener "fail"; reconnected → 200 checks.listener "ok"
func TestAvailability_PassToSSELatency(t *testing.T) // real clock + real hub; 50 SSE clients; 200 entry passes; p95(commit → event received by every client) < 2 s; logs p50/p95 (NFR-PERF-1 evidence)
```

- [ ] **Step 2: Run to verify they fail** — `$IT go test -race ./internal/features/availability/... ./internal/features/health/... ./cmd/api/...` → FAIL.
- [ ] **Step 3: Implement** the stream handler loop: `select` over the availability channel, the spaces channel, `ticker.C()`, `r.Context().Done()` and `hub.Done()`. Any write error ends the stream. Then the caps, `WithCheck`, the cmd wiring and the architecture §6 update.
- [ ] **Step 4: Run** — `$IT go test -race ./...`. Expected: `ok`, with the latency test logging p95 < 2 s.
- [ ] **Step 5: Commit**

```bash
git add apps/server docs/architecture.md
git commit -m "feat(server): stream live availability over sse with caps and readiness"
```

---

## Task S12.1: Simulator engine — virtual clock, scheduler, signing device client

Design slice **S12**. It was split from S12.2 because the engine plus the client plus the car lifecycle come to about 550 lines. Budget: ~380 lines.

**Files:**
- Create: `apps/server/internal/simulator/{clock.go,engine.go,client.go,transport.go}` + tests

**Interfaces:**
- Consumes: `devicesig` (S4) and its `testdata/golden.json`, `devicefleet` (S1.2), `clock.Clock`.
- Produces:
  - **Clocks.**
    - `type simulator.Clock interface{ Now() time.Time; WaitUntil(ctx context.Context, t time.Time) error }`.
    - `simulator.NewRealClock(start time.Time, speed int, wall clock.Clock) *RealClock`. It maps virtual time to wall time at `speed`, and `WaitUntil` sleeps `(t − now)/speed` with ctx cancel.
    - `simulator.NewStepClock(start time.Time) *StepClock`. Its `WaitUntil` jumps to `t` and returns at once.
  - **Engine.**
    - `simulator.NewEngine(c Clock, seed uint64) *Engine` uses `rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))`.
    - `(*Engine).At(t time.Time, step func(ctx context.Context))` schedules on a min-heap keyed by `(at, seq)`.
    - `(*Engine).Run(ctx context.Context) error` is one goroutine. It pops in order, sets the virtual time, runs the step, and returns when the queue is empty or ctx is done.
    - `(*Engine).Now() time.Time` and `(*Engine).Rand() *rand.Rand`.
  - **Device client.**
    - `type simulator.ClientConfig struct{ BaseURL string; Transport http.RoundTripper; SignClock clock.Clock; FleetSecret secret.Bytes; Sleep func(context.Context, time.Duration) error }`, and `simulator.NewDeviceClient(c ClientConfig) *DeviceClient`.
    - `type simulator.Outgoing struct{ EventID uuid.UUID; Type string; OccurredAt time.Time; Payload any }`.
    - `type simulator.Reply struct{ Status int; Result, Reason, StayID, TicketCode, StayStatus string; AmountCents int64; Replayed bool }`.
    - `(*DeviceClient).Send(ctx context.Context, dev devicefleet.Device, ev Outgoing) (Reply, error)`. It marshals once and re-signs on every attempt with a fresh `X-Timestamp` from `SignClock`, formatted `2006-01-02T15:04:05Z`. It retries with the same body on 429 (honoring `Retry-After`, at least 1 s), on 5xx and on transport errors: at most 5 retries, backoff `100ms × 2ⁿ`.
  - `simulator.HandlerTransport(h http.Handler) http.RoundTripper` serves the request in-process (`httptest.NewRecorder`) for the CI scenario tests (M2C-14).

- [ ] **Step 1: Write the failing tests**

```go
func TestEngine_OrderAndTieBreak(t *testing.T) // At(t+2s,A), At(t+1s,B), At(t+1s,C) → run order B, C, A; step scheduling a follow-up at t+1s runs after C
func TestEngine_SameSeedSameDraws(t *testing.T) // two engines seed 42 → first 100 Rand().Float64() equal; seed 43 differs
func TestStepClock_WaitUntilJumps(t *testing.T) // Now()==t after WaitUntil(t) with no wall time passing
func TestRealClock_SpeedMapping(t *testing.T) // FakeClock wall: speed 30, WaitUntil(+30 virtual s) requests a wall wait of 1 s (sleep hook)
func TestClient_SignsGoldenVector(t *testing.T) // golden.json inputs through Send with a capture transport → X-Signature equals golden signature, X-Timestamp verbatim
func TestClient_RetriesSameBody(t *testing.T) // transport answers 429 Retry-After "2", 503, then 200 → 3 attempts, identical bodies, sleeps [2s, 200ms]; 6 × 503 → error after 5 retries
func TestClient_ParsesEnvelope(t *testing.T) // 200 {"data":{"result":"open","stay_id":"…","ticket_code":"…"}} → Reply fields; Idempotent-Replayed true → Replayed
```

- [ ] **Step 2: Run to verify they fail** — `go test -race ./internal/simulator/...` → FAIL.
- [ ] **Step 3: Implement** with `container/heap` and `math/rand/v2`.
- [ ] **Step 4: Run** — same command. Expected: PASS.
- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/simulator
git commit -m "feat(server): add simulator discrete-event engine and signing device client"
```

---

## Task S12.2: Simulator car lifecycle

Design slice **S12**. Budget: ~330 lines.

**Files:**
- Create: `internal/simulator/{car.go,car_test.go,eventlog.go,eventlog_test.go}`

**Interfaces:**
- Consumes: `Engine`, `DeviceClient` (S12.1), `devicefleet`.
- Produces:
  - `type simulator.CredentialKind string` with `CredTicket="ticket"` and `CredEmployeeCard="employee_card"`.
  - `type simulator.Car struct{ ID string; Cred CredentialKind; Card string; Hold bool; Dwell time.Duration; LoseTicket, PayLate bool; StayID, TicketCode, Space string; Inside bool }`. Car ids are `car-%06d` from a sequence.
  - `simulator.NewFleetSim(e *Engine, c *DeviceClient, fleetSecret secret.Bytes, log *EventLog) *FleetSim` with:
    - `(*FleetSim).Arrive(car *Car)` schedules design §13.2 FR-SIM-4 exactly. That is podium → credential → +3–8 s barrier entry → +20–60 s sensor occupied on a random space from the simulator's free set → dwell → scan (skipped for employee cards) → cash with the returned amount → +1–10 min → exit credential → barrier exit → sensor free. A `PayLate` car waits `exit_deadline + 1 min` before the exit credential and re-pays on `gate.payment_required`. A `LoseTicket` car sends `paystation.lost_ticket` before the scan, and a held car parks until `Leave`.
    - `(*FleetSim).Leave(carID string, loseTicket bool) error` returns `ErrCarNotFound` for an unknown or gone car.
    - `(*FleetSim).Inside() int`.
    - `(*FleetSim).SetFault(code string, f Fault) error` with `type Fault string`: `FaultNone`, `FaultStuckOccupied`, `FaultStuckFree`, `FaultSilent`.
  - **Event log.** `simulator.NewEventLog(capacity int) *EventLog` (capacity 500) with `Append(e LogEntry)` and `After(seq int64) []LogEntry`. `type LogEntry struct{ Seq int64 \`json:"seq"\`; At time.Time \`json:"at"\`; Device, Type, Result string; Reason string \`json:"reason,omitempty"\`; CarID string \`json:"car_id,omitempty"\` }`, with JSON tags `device`, `type` and `result`. Ticket codes are never logged.

- [ ] **Step 1: Write the failing tests** — use a scripted fake device API (`http.Handler` answering by type) through `HandlerTransport`, and a `StepClock`:

```go
func TestCar_TicketLifecycleSequence(t *testing.T) // event types in order: podium.vehicle_detected, entry.credential_presented, barrier.passed(entry), sensor.space_changed(true), paystation.scan, paystation.cash_paid, exit.credential_presented, barrier.passed(exit), sensor.space_changed(false); devices match kinds
func TestCar_EmployeeSkipsScan(t *testing.T) // employee card: no paystation.* events; entry via barrier-entry-1 with the derived card UID
func TestCar_EntryDenyLeaves(t *testing.T) // deny gate.lot_full → no further events for that car; log entry reason "gate.lot_full"
func TestCar_ExitPaymentRequiredRepays(t *testing.T) // exit deny gate.payment_required 5000 → cash_paid 5000 → exit again
func TestCar_HeldUntilLeave(t *testing.T) // Hold → stops after sensor occupied; Leave(id,false) → scan…exit; Leave(unknown) → ErrCarNotFound
func TestCar_LostTicket(t *testing.T) // LoseTicket → paystation.lost_ticket before scan, exit uses the replacement code
func TestEventLog_RingAndAfter(t *testing.T) // capacity 3, 5 appends → After(0) returns seq 3,4,5; After(4) → seq 5
```

- [ ] **Step 2: Run to verify they fail** — `go test -race ./internal/simulator/...` → FAIL.
- [ ] **Step 3: Implement.** All randomness comes from `Engine.Rand()` in the engine goroutine.
- [ ] **Step 4: Run** — same command. Expected: PASS.
- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/simulator
git commit -m "feat(server): add simulator car lifecycle and device event log"
```

---

## Task S13.1: Scenarios and the runner

Design slice **S13**. It was split from S13.2 because the scenarios plus the control API and the wiring come to about 650 lines. Budget: ~380 lines.

**Files:**
- Create: `internal/simulator/{scenarios.go,scenarios_test.go,runner.go,runner_test.go}`

**Interfaces:**
- Consumes: `Engine`, `FleetSim`, `EventLog` (S12).
- Produces:
  - `type simulator.Scenario string` with `ScenarioIdle`, `ScenarioWeekday`, `ScenarioWeekendPeak`, `ScenarioFillToFull`, `ScenarioSensorDrift`, `ScenarioReservationArrivals` and `ScenarioNoShow` (the design §13.2 names).
  - `simulator.ArrivalsPerHour(s Scenario, hour int) float64` uses the design §13.2 table exactly. `fill_to_full` is 600 at every hour.
  - Dwell: `simulator.Dwell(r *rand.Rand, s Scenario) time.Duration` is log-normal (weekday median 90 min, σ 0.8; weekend median 120 min, σ 0.7), clipped to [3 min, 10 h].
  - Employees (weekday only): 30 cards arrive uniformly between 07:30 and 08:30 and stay 8–9 h. `LoseTicket` has p = 0.01 and `PayLate` has p = 0.03.
  - `sensor_drift`: 3 random sensors are stuck and 10% of sensor events are dropped. `idle`: a heartbeat every 60 s per device.
  - **Runner.**
    - `type simulator.StartParams struct{ Scenario Scenario; Seed uint64; Speed int; StartAt time.Time }`.
    - `type simulator.Status struct` with the JSON of design §13.3: `scenario`, `running`, `seed`, `speed`, `virtual_now`, `cars_inside`, `started_at`, `reached_full`.
    - `type simulator.RunnerDeps struct{ Client *DeviceClient; FleetSecret secret.Bytes; NewClock func(start time.Time, speed int) Clock; Wall clock.Clock; Log *EventLog }`.
    - `simulator.NewRunner(d RunnerDeps) *Runner` with `Start(ctx context.Context, p StartParams) (Status, error)`, `Stop(drain bool) (Status, error)`, `Status() Status`, `Inject(p InjectParams) (string, error)`, `Leave(carID string, loseTicket bool) error`, `SetFault(code string, f Fault) error` and `Wait()`.
    - `type InjectParams struct{ Cred CredentialKind; Card string; Hold bool; DwellMinutes int }`.
    - Errors: `ErrAlreadyRunning`, `ErrNotRunning`, `ErrScenarioUnavailable` (reservation_arrivals, no_show), `ErrCarNotFound`.
    - Speed defaults to 30 and must be 1–120. `StartAt` defaults to today 07:00 America/Santo_Domingo for weekday and weekend_peak, and to now for the others.
    - fill_to_full stops arriving after 3 consecutive `gate.lot_full` denials and sets `reached_full`.
    - `Stop(true)` schedules `Leave` for every car inside and completes when they are gone.

- [ ] **Step 1: Write the failing tests**

```go
func TestArrivalsPerHour_Table(t *testing.T) // weekday: h0 2, h7 30, h8 60, h9 45, h10 30, h12 40, h14 30, h17 45, h18 30, h19 10, h22 4; weekend_peak: h0 3, h9 40, h10 90, h17 50, h20 15; fill_to_full 600
func TestDwell_ClippedAndMedian(t *testing.T) // 20 000 draws seed 1: all in [3m,10h]; weekday median within 90±6 min
func TestPoisson_RateSanity(t *testing.T) // weekday 08:00 over 200 virtual hours seed 7 → mean arrivals/h within 60±3
func TestRunner_StartStopErrors(t *testing.T) // Start twice → ErrAlreadyRunning; Stop idle → ErrNotRunning; reservation_arrivals, no_show → ErrScenarioUnavailable; speed 0/121 → validation error
func TestRunner_SameSeedSameTrace(t *testing.T) // fake device API + StepClock: two runs weekday seed 42 for 2 virtual hours → identical [(type, device, payload minus server codes)] traces
func TestRunner_FillToFullStopsAfterThreeDenials(t *testing.T) // fake API denies gate.lot_full from the 10th entry → arrivals stop after 3 consecutive denials, Status().ReachedFull true
func TestRunner_DrainEmptiesLot(t *testing.T) // weekday 1 virtual hour then Stop(true) → Status().CarsInside 0, every car's last event is sensor free
```

- [ ] **Step 2: Run to verify they fail** — `go test -race ./internal/simulator/...` → FAIL.
- [ ] **Step 3: Implement.** Use Poisson arrivals with exponential gaps and the per-hour rate (thinning at hour boundaries), all draws in the engine goroutine.
- [ ] **Step 4: Run** — same command. Expected: PASS.
- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/simulator
git commit -m "feat(server): add simulator scenarios and deterministic runner"
```

---

## Task S13.2: Control API, `cmd/simulator` wiring, compose

Design slice **S13**. Budget: ~390 lines.

**Files:**
- Create: `internal/simulator/{control.go,control_test.go}`, `internal/config/{simulator.go,simulator_test.go}`, `cmd/simulator/openapi_test.go`
- Modify: `cmd/simulator/{main.go,main_test.go}`, `deploy/compose/docker-compose.yml` (simulator env; web `SIMULATOR_CONTROL_TOKEN` for Track B)

**Interfaces:**
- Consumes: `Runner` (S13.1), `httpx.DecodeJSON`/`WriteError`, `response.OK`, `secret.String`.
- Produces:
  - `config.LoadSimulator(getenv func(string) string) (SimulatorConfig, error)` with accessors `Addr()`, `APIURL()`, `FleetSecret() secret.Bytes`, `ControlToken() secret.String` and `Env()`.
    - Constants: `EnvSimulatorAPIURL = "SIMULATOR_API_URL"` (default `http://api:8080`) and `EnvSimulatorControlToken = "SIMULATOR_CONTROL_TOKEN"`.
    - The token must be at least 32 characters. Production refuses `DevSimulatorFleetSecret` and `DevSimulatorControlToken`.
  - `simulator.NewControl(r *Runner, log *EventLog, token secret.String, l *slog.Logger) http.Handler` registers the design §13.3 routes under `/control`.
    - `X-Simulator-Token` is compared with `subtle.ConstantTimeCompare`, and a missing or wrong token gives `401 simulator.unauthorized`.
    - Bodies are strict (unknown field → 422).
    - Runner errors map to `409 simulator.already_running` / `simulator.not_running`, `422 simulator.scenario_unavailable` and `404 simulator.car_not_found`.
    - Responses: 202 with the status for start and stop, 202 `{car_id}` for inject, 202 for leave, 204 for a fault, and 200 for `/control/events?after=` (a non-integer `after` gives 422).
  - `cmd/simulator`:
    - `run` serves `/healthz`, `/readyz` (the API's `/readyz` answered 200 in the last 30 s, polled every 10 s) and `/control/...`.
    - The device client signs with `clock.System{}`.
    - `newControlRoutes()` returns the served patterns for the parity test.
    - The package comment drops "M0 runs no scenarios".

- [ ] **Step 1: Write the failing tests**

```go
func TestControl_TokenRequired(t *testing.T) // no header, wrong token → 401 simulator.unauthorized (envelope); right → 200
func TestControl_StartStopFlow(t *testing.T) // start weekday seed 42 speed 120 → 202 data.running true; second start → 409 simulator.already_running; stop {drain:true} → 202; stop again → 409 simulator.not_running
func TestControl_StartValidation(t *testing.T) // speed 0, 121, "fast", 1e3 → 422 validation.failed; unknown field "turbo" → 422; reservation_arrivals → 422 simulator.scenario_unavailable
func TestControl_CarsAndFaults(t *testing.T) // inject {credential:"ticket",hold:true} → 202 car_id "car-000001"; leave unknown → 404 simulator.car_not_found; fault {fault:"stuck_occupied"} on E010 → 204; fault "boom" → 422
func TestControl_EventsAfter(t *testing.T) // after=0 → ≤ 500 entries ascending seq; after=abc → 422; no ticket_code anywhere in body
func TestLoadSimulator(t *testing.T) // defaults; short token → error; production with dev fleet secret or dev token → errors naming each var
func TestOpenAPI_CoversSimulatorRoutes(t *testing.T) // cmd/simulator: ops under x-mypark-service simulator == newControlRoutes() + GET /healthz, GET /readyz
func TestRun_ReadyzTracksAPI(t *testing.T) // fake API /readyz 200 → simulator /readyz 200; API down for 31 s (FakeClock) → 503 health.not_ready
```

- [ ] **Step 2: Run to verify they fail** — `go test -race ./internal/simulator/... ./internal/config/... ./cmd/simulator/...` → FAIL.
- [ ] **Step 3: Implement.** In compose, the simulator gets `SIMULATOR_API_URL: http://api:8080`, `SIMULATOR_FLEET_SECRET: ${SIMULATOR_FLEET_SECRET:-<DevSimulatorFleetSecret>}` and `SIMULATOR_CONTROL_TOKEN: ${SIMULATOR_CONTROL_TOKEN:-<DevSimulatorControlToken>}`, plus `depends_on: api: service_healthy`. Web gets `SIMULATOR_CONTROL_TOKEN` and `SIMULATOR_INTERNAL_URL: http://simulator:8090`.
- [ ] **Step 4: Run** — same command, then `docker compose -f deploy/compose/docker-compose.yml config -q`. Expected: `ok` and no output.
- [ ] **Step 5: Commit**

```bash
git add apps/server deploy/compose/docker-compose.yml
git commit -m "feat(server): add simulator control api and compose wiring"
```

---

## Task S14.1: Hardening — CI scenario tests, log-leak extension, coverage gate, purge job

Design slice **S14**. It was split from S14.2 so the docs and the acceptance evidence get their own reviewable PR. Budget: ~380 lines.

**Files:**
- Create: `apps/server/cmd/api/scenario_test.go`
- Modify: `cmd/api/logleak_test.go`, `scripts/covergate.sh`, `.github/workflows/ci-server.yml` (the coverage step at line 118), `cmd/worker/{main.go,main_test.go}`, `docs/architecture.md` §10 (purge function and job)

**Interfaces:**
- Consumes: `newHandler(deps)`, `simulator.Runner`/`HandlerTransport`/`NewStepClock`, `gate.VoidUnpassedEntries`, `devices.PurgeEvents`, and one `testsupport.FakeClock` shared as the API clock, the simulator `SignClock` and the void job's `now` (M2C-14).
- Produces:
  - Scenario tests live in `cmd/api`, **not** `cmd/simulator` as design §15.5 says, because `newHandler` is unexported in `package main` (resolved gap 1).
  - **Test glue.** `scenarioEnv(t) (*simulator.Runner, *testsupport.FakeClock, *testsupport.TestDB, lotID int64)` seeds the lot, the fleet (sealed with `TestSealer`) and the cards. Every engine step advances the FakeClock to the virtual time, and the void job runs every 15 virtual seconds.
  - Worker job `device_events_purge` runs every `24 * time.Hour`. It calls `devices.PurgeEvents(ctx, pool, 5000)` until it returns < 5000 and logs the count.
  - **Coverage gate.** The `covergate.sh` domain regex adds `internal/features/tariffs/calc\.go`, `internal/features/availability/(domain|coalescer)\.go` and `internal/features/gate/(state|codes)\.go`. The CI `go test -coverprofile` line adds `./internal/features/tariffs/ ./internal/features/availability/ ./internal/features/gate/`.

- [ ] **Step 1: Write the failing tests**

```go
func TestScenario_FillToFull(t *testing.T) // fill_to_full seed 42 → Status().ReachedFull; GET availability available 0; a fresh dispenser ticket_request → deny gate.lot_full; occupied == 280 == count(active)
func TestScenario_WeekdayInvariants(t *testing.T) // weekday 07:00 seed 42, 3 virtual hours; after every engine step: occupied ≤ effective_capacity and AssertOccupancyInvariant; Stop(true) → occupied 0;
                                                  // payments has ≥ 1 cash row with amount > 0 and ≥ 1 "none" row
func TestScenario_Deterministic(t *testing.T) // two runs seed 42 on fresh DBs (2 virtual hours) → identical request traces (type, device, payload minus ticket codes and stay ids)
func TestNoSecretsInLogs(t *testing.T) // extended: run 1 virtual hour of weekday + one lost ticket + one employee card; captured logs, every error body, every device_events.payload and .response row contain none of:
                                       // any fleet device secret (hex and base64), SIMULATOR_FLEET_SECRET, SECRETBOX_KEY, CARD_UID_PEPPER, any issued ticket code, any card UID
func TestWorkerJobs_IncludePurge(t *testing.T) // "device_events_purge" every 24h; its body with 12 001 back-dated rows deletes all in 3 calls
```

- [ ] **Step 2: Run** — `$IT go test -race ./cmd/... -run 'Scenario|NoSecrets|WorkerJobs'`. Expected: FAIL first (the purge job is missing), then PASS after Step 3. Any real failure in a scenario is a bug in the owning feature: fix it there, not in the test.
- [ ] **Step 3: Implement** the purge job and the coverage gate changes, and update architecture §10.
- [ ] **Step 4: Run** — `$IT go test -race ./...`, then `go test -coverprofile=domain.cover ./internal/features/auth/ ./internal/features/vehicles/ ./internal/infrastructure/hashing/ ./internal/features/tariffs/ ./internal/features/availability/ ./internal/features/gate/ && sh scripts/covergate.sh domain.cover 90`. Expected: `ok` and `domain statement coverage: ≥90.0% OK`.
- [ ] **Step 5: Commit**

```bash
git add apps/server .github/workflows/ci-server.yml docs/architecture.md
git commit -m "test(server): add m2 scenario, log-leak and coverage gates with device event purge"
```

---

## Task S14.2: Docs sync and the M2 backend acceptance test

Design slice **S14** (docs, §16.1). This is the final task. Budget: ~300 lines.

**Files:**
- Create: `apps/server/cmd/api/acceptance_m2_test.go`
- Modify: `apps/server/cmd/api/openapi_test.go` (no pending markers may remain); `docs/architecture.md` §8.3 (fleet provisioning via `SIMULATOR_FLEET_SECRET` env and fixed public ids, replacing the mounted key file; control API auth); §17 (ADR "derived fleet secrets", M2C-25); and any §16.1 item not done by an earlier task; `docs/spec.md` §8 M2 (tick the backend exit criteria, citing tests), §10.2 (append the accepted M2C-13…M2C-56 calls and resolved gaps 1–6 below); `docs/policies.md` §2.7 (device 100/s per device after auth, device_ip 1000/s, public_read_ip 120/min, sse_connect_ip 30/min, stream caps 20 per IP / 2000 total, M2C-22, M2C-53); `deploy/compose/docker-compose.yml` (verify every M2 default and dev-only comment)

**Interfaces:**
- Consumes: everything.
- Produces: `TestM2BackendAcceptance`. It has one subtest per spec §8 M2 exit criterion and uses the S14.1 `scenarioEnv`, and it prints the criterion → test mapping on failure.

- [ ] **Step 1: Write the test**

```go
func TestOpenAPI_NoPendingOperations(t *testing.T) // no operation in api/openapi.yaml carries x-mypark-pending
func TestM2BackendAcceptance(t *testing.T) {
	// "weekday runs; availability updates < 2 s": weekday seed 42 for 1 virtual hour with one SSE client on the production handler;
	//   after the last entry/exit pass the client holds an availability event whose version == lot_occupancy.version; latency figure from TestAvailability_PassToSSELatency
	// "fill_to_full reaches 0, dispenser denies gate.lot_full": run to ReachedFull → GET availability available 0; dispenser → deny gate.lot_full
	// "tariff calculator table suite": through the device API with FakeClock — ticket scanned at +10m (grace) → 0; +15h0m1s (cap) → 40000; +75h (multi-day) → 130000; lost at +26h → 57500
	// "replayed event is a no-op; bad signature → 401": replay a cash_paid → same receipt_no, Idempotent-Replayed true, one payment row, occupancy unchanged; flipped signature → 401 device.unauthenticated
}
```

- [ ] **Step 2: Run** — `$IT go test -race ./cmd/api/ -run 'M2BackendAcceptance|NoPendingOperations'`. Expected: PASS. A failure in `NoPendingOperations` names the slice whose marker was left behind.
- [ ] **Step 3: Update the docs** listed above. Cross-check design §16.1 line by line.
- [ ] **Step 4: Full verification**
  - `cd apps/server && $IT go test -race ./... && go vet ./...`
  - `go tool sqlc diff -f internal/infrastructure/database/sqlc.yaml`
  - `go generate ./internal/domain/errcodes/... && git diff --exit-code ../../packages/contracts/src`
  - `cd ../.. && npm run gen -w packages/contracts && git diff --exit-code packages/contracts/src && npm test -w packages/contracts && npm run check:i18n`
  - Compose smoke:
    - `docker compose -f deploy/compose/docker-compose.yml down -v && docker compose -f deploy/compose/docker-compose.yml up -d --build`. Expected: every service healthy.
    - `curl -s localhost:8080/v1/lots`. Expected: one lot.
    - `curl -s -X POST localhost:8090/control/scenarios/start -H "X-Simulator-Token: <DevSimulatorControlToken>" -H 'Content-Type: application/json' -d '{"scenario":"weekday","seed":42}'` returns 202. Then `curl -sN localhost:8080/v1/lots/<id>/availability/stream` shows the version rising.
- [ ] **Step 5: Commit**

```bash
git add apps/server/cmd/api docs deploy/compose/docker-compose.yml
git commit -m "docs: sync architecture, spec and policies with m2 backend and add acceptance test"
```

---

## Task dependencies

| Task | Design slice | Depends on |
|---|---|---|
| C1.1 codes | C1 | — |
| C1.2 lots contract | C1 | C1.1 |
| C2.1 device contract | C2 | C1.2 |
| C2.2 simulator contract | C2 | C2.1 |
| C1b M1 leftovers | C1b | C1.1 |
| S1.1 secretbox, config | S1 | C2.2 |
| S1.2 fleet, local limiter | S1 | S1.1 |
| S2.1 fee calculator | S2 | — (pure; may start day 1) |
| S2.2 tariffs, 00007 | S2 | S1.2, S2.1 |
| S3 devices, 00008 | S3 | S2.2 (migration order), S1.2 |
| S4 device auth | S4 | S3 |
| S5.1 decoding | S5 | S4 |
| S5.2 pipeline | S5 | S5.1 |
| S6 gate schema, 00009 | S6 | S3, **S2.2** |
| S7 entry | S7 | S5.2, S6 |
| S8 pass + void | S8 | S7 |
| S9.1 pay station | S9 | S8, S2.1 |
| S9.2 exit | S9 | S9.1 |
| S10 availability REST | S10 | S6 (parallel with S7–S9) |
| S11.1 hub | S11 | S10 |
| S11.2 stream | S11 | S11.1, S8 |
| S12.1 engine, client | S12 | S4 (golden vector) |
| S12.2 car lifecycle | S12 | S12.1, S9.2 |
| S13.1 scenarios | S13 | S12.2 |
| S13.2 control API | S13 | S13.1 |
| S14.1 hardening | S14 | all above |
| S14.2 docs + acceptance | S14 | S14.1 |

## Self-review notes

- **Spec coverage:**

  | Design section | Tasks |
  |---|---|
  | §1.2 G1 | S4, S5.2 |
  | §1.2 G2 | S7 |
  | §1.2 G3 | S8, S14.1 |
  | §1.2 G4 | S2.1 |
  | §1.2 G5 | S9.1, S9.2 |
  | §1.2 G6 | S11.2 |
  | §1.2 G7 | S13.1, S14.1 |
  | §1.2 G8 | C1.1–C2.2 |
  | §5.1 | S2.2 |
  | §5.2 | S3 |
  | §5.3–§5.4 | S6 |
  | §5.5 | S2.2, S3, S6 (`TestGrants_M2Tables`) |
  | §6.1 | S1.1, S3, S13.2 |
  | §6.2 | S1.2, S3, S6 |
  | §6.3 | S10 (`map_url`) |
  | §7.1–§7.2 | C2.1, S5.1 |
  | §7.3 | S4 |
  | §7.4 | S5.1, S5.2 |
  | §7.5 | S5.1, S7–S9 (lot scoping) |
  | §7.6 | S1.2, S4, S12.1 |
  | §7.7, §7.9 | C1.1, S5.1 |
  | §7.8 | S4, S5.2 |
  | §8.1–§8.2 | S5.2, S7–S9.2 |
  | §8.3 | S5.2, S8 |
  | §8.4 | every gate task (`@now`) |
  | §9.2–§9.5 | S7 |
  | §9.6 | S8, S9.2 |
  | §10.1–§10.3 | S9.1 |
  | §10.4–§10.5 | S9.2 |
  | §11.1–§11.2 | C1.2, S10 |
  | §11.3–§11.6 | S11.1, S11.2 |
  | §12.1–§12.4 | S2.1 |
  | §12.5 | S8 (void), S3 + S14.1 (purge) |
  | §13.1 | S12.1 |
  | §13.2 | S12.2, S13.1 |
  | §13.3 | C2.2, S13.2 |
  | §13.4 | S13.1 (drain), S13.2 (health) |
  | §14 L1 | C1.1 |
  | §14 L2–L4 | C1b |
  | §15.1–§15.5 | owning tasks, S14.1 |
  | §15.6 | S14.1 |
  | §15.7 | S14.2 |
  | §16.1 docs | C1.2, C2.1, S1.1, S1.2, S6, S8, S11.2, S14.1, S14.2 |

- **Resolved gaps and plan decisions** (S14.2 records them in spec §10.2):
  1. **Scenario test location.** The scenario and acceptance tests live in `cmd/api`, not `cmd/simulator`, because `newHandler` is unexported in `package main`. The simulator's own parity test stays in `cmd/simulator`.
  2. **S6 dependencies.** S6 depends on S2.2 as well as S3, because `stays.tariff_version_id` references `tariff_versions`. Migrations merge in number order.
  3. **Card UIDs.** The card-UID label is `"mypark/card-uid/v1/" + code` and the UID is the uppercase hex of the first 7 HMAC bytes. Ticket codes and card UIDs are normalized before hashing (Review Focus 2).
  4. **Cash amount ceiling.** `amount_cents` is capped at 10_000_000 (RD$100 000) to reject absurd values with 422.
  5. **`X-Timestamp` format.** It must be exactly `YYYY-MM-DDTHH:MM:SSZ`. Fractions and offsets are `401 device.unauthenticated` (headers).
  6. **Pending contract operations.** They carry `x-mypark-pending: <slice>`. The parity test skips them until served, and S14.2 asserts that none remain.
  7. **Hub timers.** The hub's timers come from the new `clock.Timers`, and `FakeClock.Advance` fires due timers. The stream ends on shutdown through `srv.RegisterOnShutdown(hub.Close)` rather than a change to `httpserver.Serve`.
  8. **Scope zero value.** `ratelimit.ScopeShared` is the zero value of `Scope`, so the M1 rules keep using Postgres.
  9. **Calculator input.** `tariffs.Stay.Paid bool` stands for "status = paid", so `tariffs` does not import `gate`.
- **Type consistency:**
  - These names are used unchanged from where they are defined to every later use: `gate.Event`, `gate.Result`, `gate.EventType`, `gate.Status`, `tariffs.Tariff`, `tariffs.Stay`, `tariffs.Breakdown`, `tariffs.AmountDue`, `authz.Device`, `authz.DeviceKind`, `devicefleet.Device`, `secretbox.Sealer`, `availability.ReservationHolds`, `availability.Snapshot`, `simulator.Runner`, `testsupport.TestDevice` and `testsupport.AssertOccupancyInvariant`.
  - `gate.Deps` grows additively: S5.2, then S7, then S9.1.
- **Review Focus:** every item has its test in the owning task (S11.2, S7/S9.1, S5.1, S10, S11.2).
- **Proportion:** the plan states signatures, rules and test assertions. The only bodies are the fee table rows, which are the spec's own values, and the golden vectors.
