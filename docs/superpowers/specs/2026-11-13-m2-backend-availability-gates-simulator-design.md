# M2 Track A — Backend Availability, Gates and Simulator: Design

**Status:** draft — pending user approval · **Date:** 2026-11-13 · **Milestone:** M2 (Nov 23 – Nov 27) · **Track:** A (Go backend, simulator, API contract)

This document designs the Go side of M2: the device API with per-device HMAC keys, the gate flows (entry, pay station, exit), the occupancy counter, live availability over SSE, the per-space sensor map, the pay-station fee calculator, and the simulator with its control API. **The gate count and the money are the center of gravity.** Every other part is shaped so it cannot let the count drift or let a car leave without the right charge.

> **How to read this.** §1–§3 explain *why* and what is fixed. §4–§13 are the design. §14 covers the M1 leftovers. §15–§18 cover testing, build order, risks and what is left to the builder. Decisions are numbered **M2C-n**. M2C-1 to M2C-12 were made by the user and the controller and are binding. The rest are **(our call)** and collected in §2.2 for review. Each decision lists its reason and what it costs if it turns out wrong.
>
> **Sources:** `docs/spec.md` (§3.3, §3.6, §3.7, §4, §5.1, §5.2, §5.4, §8 M2, §10), `docs/architecture.md` (§4.4, §6, §7, §8, §9, §10, §11, §14, §16, §17), `docs/policies.md` (§1.3–§1.5, §1.8, §2.2–§2.10), the M1 backend design `docs/superpowers/specs/2026-11-07-m1-backend-auth-vehicles-design.md`, and the code under `apps/server` at `main`.

---

## 1. Intent

### 1.1 Why

M2 is the first milestone where cars move. The demo promise is "watch the count and the map move live; pay and exit a car". Behind it are three things that are hard to fix later:

- **The gate count must never drift.** `lot_occupancy.occupied` is authoritative (ADR-1). Every change to it happens in one transaction with the stay and the device event, under one lock.
- **Devices are untrusted network clients.** Phase 2 swaps the simulator for real hardware with no server changes (ADR-7). So the device API must be correct on its own: signed, idempotent, scoped to its lot and kind, and rate limited.
- **Money is computed once, on the server, from server time.** The fee calculator is a pure, table-tested function, and the pay station only confirms amounts the server computed.

### 1.2 Goals

| # | Goal | Verified by |
|---|---|---|
| G1 | A device event is authenticated (HMAC), idempotent (event id) and logged in `device_events`. A replay returns the stored response. A bad signature returns 401 | `TestDeviceAuth_*`, `TestDeviceEvents_ReplayReturnsStoredResponse` (§15.2) |
| G2 | Drive-up and employee entries are decided under the `lot_occupancy` lock, counting pending entries. The last space is never given twice | `TestEntry_ConcurrentLastSpace` (§15.2) |
| G3 | Occupancy changes only on `barrier.passed`, and `occupied = count(stays in ('active','paid'))` holds after every scenario | Scenario invariant checks (§15.5) |
| G4 | The fee calculator matches §5.1 for grace, cap, multi-day, lost ticket, overstay and every boundary minute | Table-driven suite (§12.4), ≥ 90% statement coverage |
| G5 | Pay and exit: cash payment marks the stay `paid` with a 15-min exit window; zero-due exits record a RD$0 payment; overstay is billed | Integration tests (§15.2), scenario run |
| G6 | Public availability (REST + SSE) updates on clients < 2 s (p95) after a pass, with 15 s heartbeats and snapshot-first reconnects | `TestAvailability_PassToSSELatency` (§15.2) |
| G7 | `weekday` runs; `fill_to_full` reaches 0 and the dispenser denies `gate.lot_full`; both are deterministic for a seed | CI scenario tests (§15.5) |
| G8 | The M2 contract (OpenAPI paths, error codes, regenerated contracts) is published **before** implementation | Contract PRs merged first (§16) |

### 1.3 Non-goals (M2)

- **Reservations of any kind (M3).** No `reservations` table, no QR tokens, no `QR_HMAC_KEY`, no refunds. Holds and upcoming arrivals are `0` behind a port (M2C-1).
- Admin endpoints: device and employee-card CRUD, lot open/close, space out-of-service, stays list, admin stream, reconciliation (M4). Provisioning is seed-only (M2C-4).
- Reconciliation and the `sensor_drift` alert, occupancy snapshots, analytics (M4).
- Admin "mark ticket lost" (FR-GATE-8 admin path). M2 ships the pay-station path only (M2C-33).
- `/metrics` (M5, M2C-50). Web and mobile screens and the Simulator console UI (Track B).
- The online-payment fields of `payments` (provider, card brand, last four). M3 adds them (M2C-3).

### 1.4 Anti-goals (failure even if it "works")

| Anti-goal | Guard (section) |
|---|---|
| The count drifts: a pass is counted twice, a voided entry is counted, or an exit decrements without a stay | One transaction per event, idempotency, stay state checks, invariant test (§8, §9) |
| Two cars get the last space | `lot_occupancy FOR UPDATE` plus pending entries in the decision (§9.2) |
| A replayed or forged device request changes state | ±60 s window, constant-time HMAC, unique `(device_id, event_id)` (§7) |
| A device acts outside its role or lot (a sensor gateway opens a gate, a pay station in lot A pays a stay in lot B) | Kind → event matrix, lot scoping on every lookup (§7.5) |
| A spoofed `X-Device-Id` drains a real device's rate-limit budget | Per-device limit runs **after** signature verification (§7.6) |
| Ticket codes, card UIDs or device secrets appear in logs, `device_events` or error bodies | Hashing, sealed responses, log-leak test (§7.4, §15.4) |
| The pay station sets the amount | The server recomputes the due amount in the payment transaction (§10.3) |
| The simulator writes to the database | It holds no DB credentials and uses only the device API (ADR-7) |
| A slow SSE client delays everyone, or a stream holds the server open on shutdown | Latest-wins buffer, per-write deadlines, shutdown cancel (§11.4) |

---

## 2. Decisions

### 2.1 Made by the user and the controller (2026-11-13, binding)

| # | Decision | Reason | Cost if wrong | Section |
|---|---|---|---|---|
| M2C-1 | **Option A dependency path.** M2 builds only what M2 needs. A `ReservationHolds` port returns `effective_holds = 0` and `upcoming_reservations_2h = 0`. A reservation QR at the gate is denied with a stable code until M3. `reservation_arrivals` and `no_show` scenarios are out of M2 | M3 owns reservations end to end; building tables early would freeze a schema nobody has designed yet | M3 swaps one adapter and enables one credential branch. If the port shape is wrong, M3 changes one interface used in two places | §9.4, §11.1 |
| M2C-2 | Versioned `tariff_versions` and `policy_versions` tables with one default version each, seeded by `cmd/seed`. Stays capture the tariff version at entry. M4 adds admin editing | §5.1 version capture needs a real row to point at from the first stay | If M4 needs more columns, it adds them by migration; existing stays keep their FK | §5.1 |
| M2C-3 | A minimal `payments` table for cash and zero-amount `parking_fee` payments, shaped so M3 extends it (nullable provider columns added later) without rewriting it | FR-GATE-5/6 need a payment record in M2; FR-PAY is M3 | M3 runs an additive migration. A wrong check enum is a constraint swap (M2C-41 puts the final enums in now) | §5.4 |
| M2C-4 | Device and employee-card provisioning is **seed-only** in M2. Device secrets are encrypted at rest with `SECRETBOX_KEY` (compose dev default, no `.env`, no committed secret files). The simulator gets its device keys from compose env, never from the DB in plaintext | No admin CRUD before M4; policies §2.5 forbid secret files | M4 adds admin create/rotate/revoke on the same table. Seeded rows stay valid | §6 |
| M2C-5 | The device rate limit (100/s per device) uses an **in-memory adapter** behind the existing `Limiter` port, with a per-instance caveat. Sensor batching only if one gateway would exceed it in `weekend_peak` | The Postgres limiter writes a row per hit on the hottest path (ADR-12 notes 2× bursts) | With several API instances the effective limit is N × 100/s per device. Acceptable for one instance in Phase 1 | §7.6 |
| M2C-6 | Idempotency: same `event_id` + same body → stored response; same `event_id` + different body → `409` with a stable code. Signature over a canonical string (method, path with query, timestamp, SHA-256 of the raw body); ±60 s skew; bad signature 401; revoked or unknown device 401 without saying which | True idempotency lets the simulator and hardware retry blindly (NFR-AVAIL-1) | A canonicalization mistake breaks every device. Covered by a golden-vector test shared with the simulator client | §7 |
| M2C-7 | Public, unauthenticated availability: REST snapshot + SSE stream fed by LISTEN/NOTIFY fan-out with coalescing (p95 < 2 s), a per-IP connection cap, `Last-Event-ID` tied to an occupancy version. The sensor map is public but carries only space code + state | Anonymous visitors see availability (spec §10.2 #2); the map has no personal data | If the map later needs to be private, it moves behind the admin stream (M4) with no change to the availability payload | §11 |
| M2C-8 | Simulator: a virtual clock with a speed factor and an injectable clock, so CI runs scenarios fast and deterministically (FR-SIM-7). `weekday` and `fill_to_full` are required for the exit criteria; the others are best effort. The control API is compose-internal, protected by a shared secret header, and reached by admins only through the web BFF | Exit criteria need two scenarios; the rest is demo polish | Missing best-effort scenarios slip to M4 without blocking anything | §13 |
| M2C-9 | Worker in M2: void unpassed entries (FR-GATE-4) and the `device_events` retention purge. Reconciliation and the `sensor_drift` alert stay in M4 | Only the void job affects correctness in M2 | None for M2. M4 adds jobs to the same runner | §12.5 |
| M2C-10 | `effective_capacity = capacity − spaces out_of_service`. Admin toggling is M4. `lots.status` is read-only in M2 | §5.2 definition; no admin endpoints yet | None. The formula already reads the live space status | §9.2 |
| M2C-11 | M1 leftovers in the contract slice: register `session.logout_unconfirmed` in the catalog with translations; mobile logout-all treats 401 as unconfirmed like web; update clients design §5.5 and spec #37. Strict zod **response** schemas become non-strict before the first store build (M5), not in M2 | Closes the drift between catalog, web and mobile found in the M1 demo run | Small. The non-strict switch is a known M5 item | §14 |
| M2C-12 | **Contract first.** The first M2 PRs publish the `openapi.yaml` paths (lots, availability, SSE as `text/event-stream`, device API with an HMAC security scheme, simulator control), the error codes and the regenerated contracts, before implementation. Sensitive slices (migrations, device signatures) carry the security checklist and an automated risk review | Track B builds the availability screens and the console against types and MSW mocks while Track A implements | A post-handoff contract change needs both developers' approval (policies §2.2) | §16 |

### 2.2 Our calls (review these)

| # | Decision | Reason | Cost if wrong | Section |
|---|---|---|---|---|
| M2C-13 | **The server clock decides all business time** (`accepted_at`, `entered_at`, `paid_at`, fees). `X-Timestamp` is wall-clock time. The simulator's virtual clock only schedules what the simulator does next | Devices must not be able to set their own durations; the ±60 s window needs wall time | In live compose an accelerated dwell bills **real** elapsed minutes, so most fast-forwarded stays exit inside the 10-min grace at RD$0. The demo's "pay a car" uses a held car (M2C-46). No dev-only server clock knob exists to abuse | §8.4, §13.1 |
| M2C-14 | CI scenario tests run the **production handler** (`newHandler(deps)`) in-process, with **one fake clock shared** by the API, the worker's void function and the simulator engine | The only way to get realistic fees, fast runs and determinism together | If someone wires a real clock into a domain path, the scenario test catches it (durations would be ~0) | §15.5 |
| M2C-15 | **One device endpoint:** `POST /v1/devices/events`. `heartbeat` is an event type stored like any other. `POST /v1/devices/heartbeat` (architecture §4.4) is dropped | One auth path, one idempotency path, full FR-GATE-9 logging. Heartbeats every 60 s from 6 devices are ~8.6k rows a day | Negligible. A separate endpoint can be added later without breaking devices | §7.2 |
| M2C-16 | Canonical string: `METHOD\nREQUEST_URI\nX-Timestamp\nhex(sha256(raw body))`. `REQUEST_URI` is the path plus raw query **exactly as sent** (`r.URL.RequestURI()`). `X-Timestamp` is used verbatim (RFC 3339, UTC, `Z`). Hex is lowercase. Check order: headers → timestamp window → device lookup and signature. Unknown, revoked and bad signature all answer `401 device.unauthenticated`, and an unknown device still computes an HMAC against a dummy key | Byte-exact canonicalization avoids normalization disputes; equal work avoids a timing oracle | A mismatch breaks the simulator immediately and visibly; the golden vector (§15.1) pins it |  §7.3 |
| M2C-17 | A timestamp outside ±60 s answers `401 device.timestamp_out_of_window`, checked **before** the device lookup, so it reveals nothing about the device | Clock drift is the most common real-device fault; a distinct code makes it diagnosable | None found. If it ever leaks something, merge it into `device.unauthenticated` (codes are never removed, only stop being returned) | §7.3 |
| M2C-18 | Requests that fail authentication are **not** stored in `device_events` (there is no trusted `device_id`). They are logged at warn level with the claimed id, the reason and the IP. Scoped deviation from FR-GATE-9 | `device_events.device_id` is a non-null FK; storing attacker input would let anyone fill the table | Forensics for forged requests rely on logs only. Acceptable at ASVS L1 | §7.3 |
| M2C-19 | Idempotency mechanics: `device_events.body_sha256` stores the raw-body hash. A pre-check `select` is the fast path for replays. The `device_events` insert is the **last statement** of the event transaction and carries the response (the table is append-only). A concurrent duplicate blocks on the unique index, gets `23505`, rolls back everything, re-reads the stored row and answers it (same hash) or `409 device.event_conflict` (different hash). Only `200` outcomes are stored; `4xx` outcomes are deterministic and stored nowhere. A replayed answer carries `Idempotent-Replayed: true` | Append-only rules out "insert then update"; hashing the raw body avoids false 409s from JSON key order | Duplicates do the domain work twice before one rolls back. At ≤ 50 events/s that waste is irrelevant | §7.4 |
| M2C-20 | Device responses use the baseline envelope. `data.result` is one of `ack`, `open`, `deny`, `rejected`, with `reason` (an error-catalog code) on `deny` and `rejected`. **Business outcomes are `200`**; HTTP errors are reserved for protocol failures (auth, validation, conflict, rate limit) | FR-GATE-10: a denial is an expected answer the device must display, and it must be idempotent | Refines architecture §8.2 (`decision`/`ack` become one `result` field). The simulator switches on one field | §7.7 |
| M2C-21 | Device **kind → event type** matrix enforced after auth: a device sending a type its kind may not send gets `403 device.event_not_allowed`. Every stay, space and card lookup is scoped to the device's `lot_id` | HMAC proves *who* is calling, not *what* it may do | A too-strict matrix blocks a legitimate flow; it is one table in code with a unit test | §7.5 |
| M2C-22 | Rate limiting: `ratelimit.Rule` gains `Scope` (`Shared` = Postgres, the default; `Local` = the new in-memory fixed-window adapter). Device rules: `device` 100/s per device **after** signature verification; `device_ip` 1000/s per IP **before** auth (cheap flood guard). Both `Local`. A 429 stores nothing, so a retry with the same `event_id` is safe. **No sensor batching**: the busiest device at the maximum speed factor sends ~20 events/s (§7.6) | Per-device keys must not be spendable by unauthenticated callers; batching would add a second payload shape for no gain | Per-instance counters (M2C-5). If real gateways ever exceed 100/s, batching is an additive event type | §7.6 |
| M2C-23 | Device lookup runs **every request** (unique index on `public_id`), no cache. `last_seen_at` is updated at most every 30 s per device | Revocation must be immediate (policies §2.7 per-device revocation); one indexed read is cheap | ~1 extra read per event. Fine at the NFR-PERF-3 target of 50 events/s | §7.3 |
| M2C-24 | Encryption at rest: `golang.org/x/crypto/nacl/secretbox` (the module is already a direct dependency for argon2, no new module). Config `SECRETBOX_KEY` (base64, 32 bytes) and `SECRETBOX_KEY_ID`, plus optional `SECRETBOX_OLD_KEYS` (`kid:base64,…`) for rotation. Rows store `secret_key_id`. `config.Load` refuses the committed dev key when `APP_ENV=production` | Same rotation pattern as the JWT keys (OC-8); policies §2.5 require rotatable secretbox keys | A full re-encryption tool is M4/M5. Until then rotation = keep the old key in `SECRETBOX_OLD_KEYS` and re-run the seed | §6.1 |
| M2C-25 | **Simulator fleet provisioning:** a committed manifest (`internal/devicefleet`) lists the fleet devices with **fixed public ids**, kinds and names, and the employee cards. Each device secret is `HMAC-SHA256(SIMULATOR_FLEET_SECRET, "mypark/device-secret/v1/" + public_id)`; each card UID is derived the same way with a `card-uid` label. `cmd/seed` (with `SEED_SIMULATOR_FLEET=true`) and `cmd/simulator` both read `SIMULATOR_FLEET_SECRET` from compose env. The seed encrypts with `SECRETBOX_KEY`. Production refuses the dev default. **Replaces** the "key file mounted into the simulator" in architecture §8.3 | Policies §2.5 forbid secret files; the simulator must not read the DB; `uuidv7()` ids would be unknown to the simulator | One leaked fleet secret exposes every simulated device. The fleet is demo-only; real hardware (M4) gets random secrets shown once | §6.2 |
| M2C-26 | Credentials at rest: ticket code = 16 Crockford base32 characters (80 bits, `crypto/rand`), stored as SHA-256 (high entropy, no pepper, like refresh tokens). Card UID stored as `HMAC-SHA256(CARD_UID_PEPPER, uid)` (UIDs are short). In `device_events.payload`, credential values are replaced by `sha256:<first 12 hex>`. Response fields that are themselves credentials (`ticket_code`) are stored **sealed** with secretbox under `sealed` and unsealed only to answer a replay | NFR-SEC-3: tokens stored only as hashes; true idempotency still needs the original ticket code on replay | Sealing adds one secretbox call per ticket issue. If `SECRETBOX_KEY` is lost, replays of old ticket events fail (they are past the useful window anyway) | §7.4, §9.3 |
| M2C-27 | A reservation QR at entry or exit is denied with **`gate.credential_invalid`** in M2, not a throwaway M2-only code | Published codes are never removed; M3 needs `gate.credential_invalid` for forged or unknown QRs anyway | The simulator console cannot tell "not supported yet" from "invalid". Acceptable for one milestone | §9.4 |
| M2C-28 | Employee cards: a partial unique index allows **one open stay per card** (anti-passback); a second entry is denied `gate.credential_in_use`. Unknown or revoked cards are denied `gate.credential_invalid`. Every accepted card entry writes an `audit_log` row (`gate.employee_entry`, actor `system`) | Policies §1.4: card stays visible in the audit trail, revoked cards denied at next presentation | A lost card with an open stay blocks the employee until M4's admin force-close. Rare |  §9.5 |
| M2C-29 | **One lock order** for every device transaction: `lot_occupancy` (`FOR UPDATE`) → `stays` → `payments` → `device_events` insert. Pay-station and exit-credential transactions never take the lot lock | Consistent ordering removes deadlocks; keeping payments off the lot lock keeps entry latency flat | A new flow that locks in another order can deadlock; the PR checklist names this order | §8.2 |
| M2C-30 | Pending entries count for decisions only while `accepted_at > now − 60 s`, read under the lot lock. The worker's voiding is cleanup, not a correctness dependency | Availability must not depend on worker latency | None. Old pending rows are ignored by the count before the worker voids them | §9.2, §12.5 |
| M2C-31 | `barrier.passed` rules: entry on `entry_pending` → `active`; entry on a **voided** stay (late pass) → `active` and counted, logged at warn and audited; a second entry pass → `ack` with `reason gate.duplicate_pass`, no change; an unknown `stay_id` → `rejected gate.stay_unknown`, no change; an exit pass needs an exit authorization from the last 60 s, else `rejected gate.exit_not_authorized`, no change. Invariant: `occupied = count(stays where status in ('active','paid'))` | The barrier is the physical truth (ADR-1). Undercounting a car that is inside oversells; decrementing without an authorized exit hides an unpaid exit | Adds `voided → active` to the stay state machine (§19). Tailgating without a stay is not counted in M2; M4 reconciliation surfaces it via sensors | §9.6 |
| M2C-32 | Exit authorization is stored (`stays.exit_authorized_at`) when `exit.credential_presented` opens, and the zero-due decision is made at that moment. On the exit pass, an `active` stay gets the RD$0 payment and moves `active → paid → exited` in one transaction | A car authorized at 9:58 must not be charged because it passed the barrier at 10:02 | One extra column. None otherwise | §10.5 |
| M2C-33 | **Lost ticket (pay station path):** new event `paystation.lost_ticket {stay_id}`. The pay station identifies the stay (in Phase 1 the simulator knows it; at a real facility the attendant looks it up, M4). The server marks `lost_ticket = true`, **replaces the ticket code** (the old one stops working) and returns the replacement code and the amount due. The lost-ticket fee applies even inside the 10-min grace. The admin path is M4 | Keeps exit uniform (always a ticket code) and kills the old ticket; grace is a courtesy for short stays, not a waiver of the penalty | Adds an event type to architecture §8.2. If product wants grace to win, it is one branch in the calculator | §10.4, §12.2 |
| M2C-34 | `paystation.cash_paid.amount_cents` must **equal** the amount the server recomputes in the payment transaction; otherwise `rejected gate.amount_mismatch` with the current amount. A paid stay inside its window answers `rejected gate.nothing_due`. Receipt number = `MP-` + zero-padded 8-digit payment id | The server owns money; the device only confirms | A legitimate change between scan and pay (crossing an hour) costs one rescan |  §10.3 |
| M2C-35 | FR-GATE-7 **overstay is lazy**: evaluated at `paystation.scan` and `exit.credential_presented`. A `paid` stay past its deadline moves `paid → active` in that transaction and keeps `exit_deadline_at`. Whenever `exit_deadline_at` is set and passed (whatever the status), amount due = `max(additional_hour, fee(entered_at → now) − sum(succeeded parking_fee payments))`. A stay can have several payments | "Same tariff rules" means the whole stay is re-priced (cap and blocks included), and the spec floor is one additional-hour unit | A worker job is unnecessary: nothing observes the overstay except the next scan. If product wants "hours since payment" instead, the formula changes in one function (cases O1–O7 in §12.4) | §12.3 |
| M2C-36 | Fee arithmetic in **whole seconds** (`floor` of the server duration) and integer centavos. Employee stays are always 0 | Deterministic, test-friendly, and rounds in the driver's favor | Sub-second differences are invisible to drivers | §12 |
| M2C-37 | Public lot endpoints: `GET /v1/lots` (discovery, the lot `public_id` differs per database), `GET /v1/lots/{id}`, `/availability`, `/spaces`, `/tariff`, and `/availability/stream`. `map_url` is built server-side (coordinates when present, else the address). Status precedence `closed > full > open`. `opening_hours` supports only `{"always_open": true}` in M2 | Both clients need one discovery call and one consistent map link; the lot is 24/7 (FR-RES-14) | `GET /v1/lots` and `/spaces` are additions to architecture §4.4 | §11.1 |
| M2C-38 | SSE semantics: on every connect the server sends the current **availability snapshot first** (and the full spaces snapshot when `include=spaces`). `id:` is the occupancy version and is set **only** on `availability` events; `spaces` events carry no `id:`. `Last-Event-ID` is parsed and logged but the snapshot is always sent. Clients drop any availability event whose version is ≤ the last one applied | Events are snapshots, not deltas, so "resume" is "send the current state"; one id sequence keeps `Last-Event-ID` meaningful | A client that relies on `Last-Event-ID` to skip the snapshot gets one redundant event. Harmless | §11.3 |
| M2C-39 | Fan-out: per lot, **leading + trailing edge** coalescing at 250 ms (≤ 4 pushes/s), **one snapshot query per lot per push** (not per subscriber), the payload serialized once, each subscriber a **latest-wins buffer of 1** for availability and a delta queue with overflow → full resync for spaces. After a listener reconnect, every subscriber gets fresh snapshots | Meets p95 < 2 s with headroom; cost is independent of the client count | A trailing push adds at most 250 ms. Inside the 2 s budget | §11.4 |
| M2C-40 | NOTIFY channels: `lot_changed` (`lot_id:version`) only when the availability inputs change (in M2: entry and exit passes); `space_changed` (`lot_id:code:state`) only when a sensor **changes** the stored state. Sensor events never bump the occupancy version | ADR-1: sensors never gate; repeats from a chatty sensor must not wake every client | None | §8.3 |
| M2C-41 | Final check enums now: `payments.kind in ('reservation_fee','parking_fee')`, `method in ('gateway_sim','cash','none')`, `status in ('pending','succeeded','failed','refunded')`. `stay_id` is `not null` in M2; M3 drops `not null` and adds `reservation_id` with `num_nonnulls(reservation_id, stay_id) = 1`. Multiple payments per stay are allowed; one zero payment per stay (partial unique index) | Avoids constraint swaps in M3 for values already known from architecture §11.1 | M3's change is metadata-only (`drop not null`, add column, add check) | §5.4 |
| M2C-42 | `tariff_versions` and `policy_versions` are **append-only** for the app role (no update, delete, truncate), like `audit_log` | Stays point at the version that priced them; a version must never change under a running stay | M4 adds new versions only; a typo fix is a new version with a later `effective_from` | §5.1 |
| M2C-43 | `device_events` retention (12 months, policies §1.8): a `security definer` function `purge_device_events(batch int)` owned by `mypark_migrator`, `set search_path = ''`, deleting at most `batch` rows with `received_at < now() − interval '12 months'` (the floor is **inside** the function). The app role gets `EXECUTE` only. The worker calls it daily until it deletes fewer than `batch`. `BRIN(received_at)` supports it | The app role has no DELETE on append-only tables; a hard-coded floor means the app cannot erase recent evidence | Monthly partitioning (architecture, Phase 2) replaces it when the table passes 10M rows. The same pattern can close the deferred `audit_log` purge | §5.2, §12.5 |
| M2C-44 | Simulator engine: a **discrete-event scheduler** (priority queue by virtual time, then sequence), one goroutine, `math/rand/v2` PCG seeded from the scenario seed. Real mode maps virtual time to wall time by the speed factor; test mode jumps straight to the next event. Determinism means "same seed + same starting lot state → same device request sequence" | Sequential execution removes scheduling nondeterminism; server responses differ only in random codes, which never feed the RNG | Throughput is bounded by request latency (~200 req/s sequential), well above the capped load | §13.1 |
| M2C-45 | Speed factor range **1–120**, default 30. Scenario parameters in §13.2. `reservation_arrivals` and `no_show` answer `422 simulator.scenario_unavailable` | The cap keeps every device under 100/s even in `fill_to_full` (§7.6) | Demos that want faster than 120× wait longer. Raising the cap needs batching first | §13.2 |
| M2C-46 | Control API (`:8090`): JSON with the baseline envelope, header `X-Simulator-Token` compared in constant time with `SIMULATOR_CONTROL_TOKEN`. Endpoints in §13.3. Injected cars can be **held** (park until told to leave), which the demo uses to show a real non-zero fee. The console polls `GET /control/events` (a ring buffer of the last 500 device outcomes) through the BFF every 2 s | FR-SIM-5/6, FR-GATE-10 without an admin stream in M2; holding solves M2C-13's demo issue honestly | Polling adds ≤ 2 s to the console log (not to availability). M4's admin stream can replace it | §13.3 |
| M2C-47 | Simulator state (cars, ticket codes) is **in memory**. `stop` with `drain: true` pays and exits every car inside. A simulator crash leaves orphan active stays; recovery in M2 is `docker compose down -v` | ADR-7: no DB access; persistence would need a secret-bearing file | Orphans inflate `occupied` until M4's admin force-close. Noted risk R3 | §13.4 |
| M2C-48 | Router: `Policy.Device bool` (exclusive with `Public` and `Roles`) builds `ClientIP → RateLimit(device_ip) → DeviceAuth → RateLimit(device)`. New `authz.Device{ID, PublicID, LotID, Kind}` principal with `WithDevice`/`DeviceFrom`. `DeviceAuth` fills `reqctx.LogFields.DeviceID` (architecture §14 log field). The authz matrix gains device principals | Deny-by-default must cover devices, not just users | Router and matrix changes are mechanical; they land in the device-auth slice | §7.8 |
| M2C-49 | OpenAPI: an HMAC security requirement made of three `apiKey` header schemes (`X-Device-Id`, `X-Timestamp`, `X-Signature`, all required together). Simulator control paths carry a path-level `servers` override and `x-mypark-service: simulator`; the API's route↔OpenAPI parity test skips them, and a parity test in `cmd/simulator` covers them. Event bodies are a `oneOf` discriminated by `type` | One contract file for Track B; parity tests stay exact per binary | None | §16 |
| M2C-50 | `/metrics` is deferred to M5 (perf check). M2 proves NFR-PERF-1 with an integration latency test (pass → SSE event, 200 samples, p95 < 2 s) | The exit criterion needs evidence, not a dashboard | Architecture §14 lists `/metrics` for Phase 1; M5 delivers it |  §15.2 |
| M2C-51 | Outbox payload encryption (deferred in M1 §16.3 "revisit with `SECRETBOX_KEY` in M2") stays **out of Phase 1** | Codes are scrubbed after send and expire in 15 min; encrypting them adds a key dependency to the worker for little gain | Low. Revisit if outbox payloads ever carry long-lived data |  §6.1 |
| M2C-52 | Strictness: every new M2 **request** schema has `additionalProperties: false`, and Go decodes device payloads with unknown-field rejection (`422 validation.failed`, rule `unknown_field`). Response schemas follow the existing contract pattern; the switch to non-strict response parsing is M5 (M2C-11) | Firmware typos must fail loudly; responses must stay additive | A future device firmware that adds fields needs a contract update first. That is the point |  §7.7 |
| M2C-53 | Public read limits (both `Local`): `public_read_ip` 120/min per IP on the lot REST endpoints; `sse_connect_ip` 30/min per IP on stream opens; concurrent streams `SSE_MAX_PER_IP` (default 20) and `SSE_MAX_STREAMS` (default 2000) | Anonymous endpoints need a floor against scraping (policies §1.7) and reconnect storms | Carrier NAT can put many phones behind one IP; 20 streams per IP is generous, and the cap is config so the 500-client perf test (M5) can raise it | §11.5 |
| M2C-54 | **No admin endpoints and no admin stream in M2.** The console reads the simulator's own event log (M2C-46) | FR-GATE-10 is met by the simulator log; M4 builds the admin surface once | The console's view is simulator-centric; a request from a non-simulated device would not appear there until M4 |  §13.3 |
| M2C-55 | `stays` stores no `amount_due_cents` (derived, would go stale). `stays.reservation_id` is added by M3. New columns: `accepted_at`, `voided_at`, `exit_authorized_at` | Derived money is computed at the moment it is needed (§12) | Analytics (M4) sums `payments`, not `stays` |  §5.3 |
| M2C-56 | Sensor updates **never overwrite** `out_of_service` (the update filters it out). An unknown space code answers `rejected lot.space_unknown` | Out-of-service is an admin decision that affects capacity; a sensor must not undo it | The reading of an out-of-service space is dropped. M4 may split sensor state from admin status if it needs both |  §8.3 |

**Count:** 56 decisions (12 binding, 44 our calls).

---

## 3. Constraints

| Area | Constraint (source) |
|---|---|
| Inherited | Everything in M1 §3: Go 1.27, stdlib `ServeMux`, `pgx/v5` + sqlc + goose forward-only, Postgres 18, baseline envelope, `internal/config` accessors, no `.env` files, `mypark_migrator`/`mypark_app` roles |
| Device auth | HMAC-SHA256, ±60 s, constant-time compare, unique `(device_id, event_id)`, per-device revocation (architecture §8.1, policies §2.7) |
| Real time | One LISTEN connection per API instance, ≤ 4 pushes/s per lot, 15 s heartbeat, 45 s client stale rule (architecture §6, policies §1.5) |
| Performance | NFR-PERF-1 p95 < 2 s pass → client; NFR-PERF-2 reads p95 < 200 ms, writes p95 < 400 ms; NFR-PERF-3 ≥ 500 SSE clients and ≥ 50 device events/s on one instance |
| Restart | NFR-AVAIL-2: all state in Postgres; restarting any container never drifts the count |
| Money | Integer centavos (`bigint *_cents`), DOP implied, `America/Santo_Domingo` for display only (spec §5.1, §5.3) |
| Dependencies | Stdlib first. No new Go module in M2 (`secretbox` is in `golang.org/x/crypto`, already direct; `math/rand/v2` is stdlib) |
| Process | PRs ≤ 400 changed lines (generated sqlc output excluded, `openapi.yaml` included), chained; migrations and device signatures are sensitive paths (policies §2.2); TDD for fees, availability and the stay state machine; integration tests on real Postgres; no AI attribution |

---

## 4. Architecture

### 4.1 Package map

New packages are **bold**. Paths are under `apps/server/internal/`.

| Package | Role | Owns tables |
|---|---|---|
| `domain/authz` | Adds `Device{ID, PublicID, LotID, Kind}`, `WithDevice`, `DeviceFrom` | — |
| `domain/errcodes` | M2 codes (§7.9) and `session.logout_unconfirmed` (§14) | — |
| **`infrastructure/secretbox`** | `Sealer` (`Seal(plain) (ciphertext, keyID)`, `Open(ciphertext, keyID)`), keyring from config | — |
| **`infrastructure/pubsub`** | `Listener`: dedicated LISTEN connection (hijacked from the pool), reconnect with backoff, typed callbacks for `lot_changed` and `space_changed`, `Connected()` for `/readyz` | — |
| `infrastructure/ratelimit` | Adds `Rule.Scope`, **`memory.go`** (fixed-window, mutex + map, swept every minute), and a `Composite` that routes by scope | — |
| **`devicefleet`** | The committed simulator fleet manifest and the secret/UID derivation (§6.2). Imported by `cmd/seed` and `simulator` only | — |
| **`features/tariffs`** | Domain: the fee calculator (pure). Service: current version for a lot. HTTP: `GET /v1/lots/{id}/tariff` | `tariff_versions`, `policy_versions` |
| `features/lots` | Adds `GET /v1/lots`, `GET /v1/lots/{id}`, `GET /v1/lots/{id}/spaces`; seed extended (§6.3) | `lots`, `spaces` |
| **`features/availability`** | Domain: `Compute(inputs) Snapshot` and the entry-capacity rule. Ports: `ReservationHolds`. Service: snapshot query. **Hub**: fan-out, coalescing, subscribers. HTTP: REST snapshot and SSE stream | `lot_occupancy` (reads; writes go through `gate`) |
| **`features/devices`** | Device registry reads, `DeviceAuth` middleware, event envelope decode, kind matrix, idempotency store, dispatch to `gate` | `devices`, `device_events` |
| **`features/gate`** | Use cases: entry decision, barrier pass, pay-station scan/lost/cash, exit decision, sensor update, void job. Domain: the stay state machine | `stays`, `employee_cards`, `payments` |
| **`simulator`** | Engine, virtual clock, scenarios, car lifecycle, signing device client, control API | — |
| `transport/http/router` | `Policy.Device` (M2C-48) | — |
| `transport/http/sse` (**new**) | Minimal SSE writer: `Event(name, id, data)`, `Comment`, per-write deadline via `http.ResponseController` | — |

Features talk through exported interfaces:

| Caller | Callee interface | Use |
|---|---|---|
| `devices` | `gate.Handler` | `Handle(ctx, device, event) (Result, error)` inside the event transaction |
| `gate`, `availability` | `availability.ReservationHolds` | `EffectiveHolds(ctx, lotID, now)`, `UpcomingArrivals(ctx, lotID, now, within)`. M2 adapter: `noReservations{}` returns 0 (M2C-1) |
| `gate` | `tariffs.Current` | `AtTime(ctx, lotID, t) (Version, error)` and `ByID` |
| `gate` | `tariffs.Calculate` | Pure function (§12) |
| `gate` | `audit.Recorder` | Employee entries, late passes (M2C-28, M2C-31) |
| `availability` hub | `pubsub.Listener` | Notifications → coalesced pushes |

### 4.2 Composition

| Binary | Adds in M2 |
|---|---|
| `cmd/api` | Config (`SECRETBOX_*`, `CARD_UID_PEPPER`, `SSE_*`, `DEVICE_RATE_LIMIT_PER_S`). Sealer, composite limiter, pubsub listener, availability hub. Routes for lots, availability, SSE and devices. `/readyz` adds `listener` (M2C-39). Shutdown order: stop accepting → `hub.Close()` (ends streams) → `srv.Shutdown` |
| `cmd/worker` | Jobs `void_unpassed_entries` (15 s) and `device_events_purge` (24 h). Each job body is a function taking `now`, so tests call it with the fake clock (M2C-14) |
| `cmd/seed` | After lots and legal: default tariff and policy versions, then (when `SEED_SIMULATOR_FLEET=true`) the fleet devices and employee cards. One transaction |
| `cmd/simulator` | Config, fleet manifest, signing client, engine, control API on `SIMULATOR_ADDR`, `/healthz` and `/readyz` (API reachable) |

### 4.3 Request flow of one device event

```
Recover → RequestID → AccessLog → SecureHeaders → CORS → JSONErrors(mux)
    │
    per route (Policy.Device): ClientIP → RateLimit(device_ip, Local) → DeviceAuth → RateLimit(device, Local) → handler
                                                                           │
handler: decode envelope (16 KiB cap, strict) → kind matrix → replay fast path (select by device_id, event_id)
       → TxRunner.InTx: gate.Handle (locks in M2C-29 order) → insert device_events (last) → pg_notify (fires on commit)
       → on 23505: re-read → same hash: stored response · different hash: 409
```

---

## 5. Data model

Migrations run as `mypark_migrator`, forward-only, with the architecture §11 conventions: identity PKs, `public_id uuid default uuidv7()` on exposed rows, `text` + `check` enums, `timestamptz`, `*_cents bigint`, every FK indexed, partial indexes for hot subsets. Three files, one per sensitive slice: `00007_tariffs_policies.sql`, `00008_devices.sql`, `00009_stays_payments.sql`.

### 5.1 `00007_tariffs_policies`

```sql
create table tariff_versions (
  id                     bigint generated always as identity primary key,
  lot_id                 bigint not null references lots (id),
  effective_from         timestamptz not null,
  first_hour_cents       bigint not null check (first_hour_cents >= 0),
  additional_hour_cents  bigint not null check (additional_hour_cents >= 0),
  exit_grace_min         int    not null check (exit_grace_min between 0 and 120),
  post_payment_exit_min  int    not null check (post_payment_exit_min between 1 and 120),
  daily_cap_cents        bigint not null check (daily_cap_cents >= first_hour_cents),
  lost_ticket_cents      bigint not null check (lost_ticket_cents >= 0),
  reservation_fee_cents  bigint not null check (reservation_fee_cents >= 0),
  created_by             bigint references users (id),
  created_at             timestamptz not null default now(),
  unique (lot_id, effective_from)            -- also the lot_id FK index and the "current version" lookup
);
create index tariff_versions_created_by_idx on tariff_versions (created_by);
```

`policy_versions` mirrors architecture §11.1 (`max_reservable_pct between 0 and 100`, `booking_horizon_days`, `arrival_grace_min`, `early_arrival_min`, `payment_timeout_min`, `max_active_per_user`, `slot_min`, `noshow_block_threshold`, `noshow_window_days`, `noshow_block_days`, all `> 0` where it applies), with the same `unique (lot_id, effective_from)` and `created_by` index. M2 only seeds it.

```sql
revoke update, delete, truncate on tariff_versions, policy_versions from mypark_app;   -- M2C-42
```

**Current version:** `select … where lot_id = $1 and effective_from <= $2 order by effective_from desc limit 1` (served by the unique index). **Seed:** one row each, `effective_from = '2026-11-01T00:00:00-04:00'`, values from spec §5.1/§5.3 (5000, 2500, 10, 15, 40000, 50000, 15000; 30, 7, 20, 15, 10, 1, 5, 3, 30, 7), `on conflict (lot_id, effective_from) do nothing`.

### 5.2 `00008_devices`

`devices`:

| Column | Notes |
|---|---|
| `id` bigint PK, `public_id` uuid unique | Fleet devices use fixed ids (M2C-25). Others default to `uuidv7()` |
| `lot_id` bigint not null → lots | `unique (lot_id, name)` covers the FK |
| `kind` text | `check (kind in ('podium','dispenser','entry_barrier','exit_barrier','pay_station','sensor_gateway'))` |
| `name` text | `check (length(name) between 1 and 60)` |
| `secret_ciphertext` bytea not null, `secret_key_id` text not null | nonce (24 B) ‖ secretbox output of the 32-byte secret (M2C-24) |
| `status` text default `'active'` | `check (status in ('active','revoked'))`, `check ((status = 'revoked') = (revoked_at is not null))` |
| `last_seen_at`, `revoked_at` timestamptz null; `created_at` | |

`device_events` (append-only):

| Column | Notes |
|---|---|
| `id` bigint identity PK | |
| `lot_id` bigint not null → lots | idx `(lot_id, received_at)` (architecture) — also the FK index |
| `device_id` bigint not null → devices | `unique (device_id, event_id)` — also the FK index |
| `event_id` uuid not null | |
| `type` text | `check (type in ('podium.vehicle_detected','entry.credential_presented','barrier.passed','paystation.scan','paystation.lost_ticket','paystation.cash_paid','exit.credential_presented','sensor.space_changed','heartbeat'))` |
| `occurred_at` timestamptz not null | Device claim; informational (M2C-13) |
| `received_at` timestamptz not null default now() | BRIN `(received_at)` for the purge |
| `body_sha256` bytea not null | `check (octet_length(body_sha256) = 32)` (M2C-19) |
| `payload` jsonb not null | Credential values redacted (M2C-26) |
| `response` jsonb not null | The `data` object answered; credential fields sealed |

```sql
revoke update, delete, truncate on device_events from mypark_app;

create function purge_device_events(batch int) returns bigint
language sql security definer set search_path = '' as $$
  with doomed as (
    select id from public.device_events
     where received_at < now() - interval '12 months'
     order by received_at limit least(greatest(batch, 1), 10000)
  )
  delete from public.device_events d using doomed where d.id = doomed.id
  returning 1
$$;   -- wrapped so it returns the count; exact form left to the builder
revoke all on function purge_device_events(int) from public;
grant execute on function purge_device_events(int) to mypark_app;
```

Volume: a weekday is ~6k events plus ~8.6k heartbeats. A year stays far below the 10M-row partitioning threshold.

### 5.3 `00009_stays_payments`

`employee_cards`:

| Column | Notes |
|---|---|
| `id`, `public_id` | |
| `lot_id` → lots | FK idx `(lot_id)` |
| `card_uid_hash` bytea not null unique | HMAC with `CARD_UID_PEPPER` (M2C-26) |
| `holder_name` text, `employee_code` text | `1–80` and `1–20` characters. Seeded with fictitious values |
| `status` text default `'active'` | `check (status in ('active','revoked'))`, with `revoked_at` consistency check |
| `created_by` → users null, `created_at`, `revoked_at` | FK idx `(created_by)` |

`stays`:

| Column | Notes |
|---|---|
| `id`, `public_id` | `public_id` is the `stay_id` devices see |
| `lot_id` → lots | |
| `credential_kind` text | `check (credential_kind in ('ticket','reservation','employee_card'))` (final enum; `reservation` unused until M3) |
| `ticket_code_hash` bytea null | `unique`. `check ((credential_kind = 'ticket') = (ticket_code_hash is not null))` |
| `employee_card_id` bigint null → employee_cards | `check ((credential_kind = 'employee_card') = (employee_card_id is not null))` |
| `status` text | `check (status in ('entry_pending','active','paid','exited','voided'))` |
| `tariff_version_id` bigint not null → tariff_versions | Captured at acceptance (M2C-2). FK idx |
| `accepted_at` timestamptz not null default now() | Credential accepted |
| `entered_at`, `paid_at`, `exit_deadline_at`, `exit_authorized_at`, `exited_at`, `voided_at` | timestamptz null |
| `lost_ticket` boolean not null default false | |
| `updated_at` | |

State consistency checks: `status in ('active','paid','exited') → entered_at is not null`; `status = 'paid' → paid_at and exit_deadline_at not null`; `status = 'exited' → exited_at not null`; `status = 'voided' → voided_at not null`; `lost_ticket → credential_kind = 'ticket'`.

Indexes:

```sql
create index stays_open_idx     on stays (lot_id) where status in ('entry_pending','active','paid');      -- architecture
create index stays_pending_idx  on stays (lot_id, accepted_at) where status = 'entry_pending';            -- decision count (M2C-30), void job
create unique index stays_card_open_key on stays (employee_card_id)
  where status in ('entry_pending','active','paid');                                                       -- anti-passback (M2C-28), also FK idx for open rows
create index stays_employee_card_id_idx on stays (employee_card_id) where employee_card_id is not null;   -- FK idx
create index stays_tariff_version_id_idx on stays (tariff_version_id);                                    -- FK idx
```

### 5.4 `payments` (minimal, M2C-3, M2C-41)

| Column | Notes |
|---|---|
| `id` bigint PK, `public_id` uuid unique | `receipt_no` = `'MP-' || lpad(id::text, 8, '0')` (derived, M2C-34) |
| `kind` text | `check (kind in ('reservation_fee','parking_fee'))`. M2 writes only `parking_fee` |
| `stay_id` bigint **not null** → stays | FK idx `(stay_id)`. M3 drops `not null` and adds `reservation_id` |
| `method` text | `check (method in ('gateway_sim','cash','none'))`. M2 writes `cash` and `none` |
| `amount_cents` bigint not null | `check (amount_cents >= 0)`, `check ((method = 'none') = (amount_cents = 0))` |
| `status` text | `check (status in ('pending','succeeded','failed','refunded'))`. M2 writes `succeeded` |
| `idempotency_key` text not null unique | `cash:<device public_id>:<event_id>` or `zero:<stay public_id>` |
| `device_id` bigint null → devices | The pay station. FK idx |
| `created_at`, `succeeded_at` | |

`create unique index payments_zero_per_stay_key on payments (stay_id) where method = 'none';` — at most one zero payment per stay.

### 5.5 Grants test

`TestGrants_M2Tables` (as `mypark_app`): select/insert on every new table; update on `devices`, `stays`, `employee_cards`, `payments`; **denied** update/delete on `device_events`, `tariff_versions`, `policy_versions`; `purge_device_events` executes, deletes a row back-dated 13 months (inserted by the migrator in the fixture) and leaves a 11-month-old row.

---

## 6. Security: secrets and provisioning

Device authentication is in §7.3–§7.6, credential handling at rest in §7.4, and the anti-goals in §1.4. This section covers keys and provisioning.

### 6.1 Secretbox keyring (M2C-24)

| Env var | Used by | Compose default (dev only) |
|---|---|---|
| `SECRETBOX_KEY` | api, seed | inline dev key (base64, 32 bytes) |
| `SECRETBOX_KEY_ID` | api, seed | `dev-1` |
| `SECRETBOX_OLD_KEYS` | api, seed | empty |
| `CARD_UID_PEPPER` | api, seed | inline dev value |
| `SIMULATOR_FLEET_SECRET` | seed, simulator | inline dev value |
| `SEED_SIMULATOR_FLEET` | seed | `true` |
| `SIMULATOR_CONTROL_TOKEN` | simulator, web (Track B) | inline dev value |

All four secret values are refused by config when `APP_ENV=production` and they equal the committed dev values (same rule as `JWT_SIGNING_KEY`, OC-8). `Config.String`/`GoString`/`LogValue` redact them. Decrypted device secrets live in a `secret.Bytes` type that redacts like `secret.String`.

### 6.2 Simulator fleet (M2C-25)

| Name | Kind | Public id |
|---|---|---|
| `podium-entry-1` | podium | `019a6d00-0000-7000-8000-000000000001` |
| `dispenser-entry-1` | dispenser | `…0002` |
| `barrier-entry-1` | entry_barrier | `…0003` |
| `barrier-exit-1` | exit_barrier | `…0004` |
| `paystation-1` | pay_station | `…0005` |
| `sensors-1` | sensor_gateway | `…0006` (all 280 sensors report through it) |

Employee cards: `EMP-001` … `EMP-030` active, `EMP-031` revoked (to demo the denial). Holder names are fictitious (`Empleado 01`…).

Seed behavior ("ensure fleet"): for each device, insert it if missing; if present and the stored secret (decrypted) differs from the derived one, or the row was sealed with an old key id, **re-seal and update**. So rotating `SECRETBOX_KEY` or `SIMULATOR_FLEET_SECRET` is "change env, re-run seed". Cards are insert-only (`on conflict (card_uid_hash) do nothing`). The seed never logs a secret or a UID.

### 6.3 Lot seed

Unchanged (`zona-colonial`, 280 spaces, insert-only). Coordinates stay `NULL` until confirmed; `map_url` falls back to the address (M2C-37).

---

## 7. Device API

### 7.1 Request

```
POST /v1/devices/events
Content-Type: application/json
X-Device-Id: 019a6d00-0000-7000-8000-000000000002
X-Timestamp: 2026-11-24T14:03:11Z
X-Signature: 3f9c…(64 lowercase hex)

{ "event_id": "019a…", "type": "entry.credential_presented", "occurred_at": "2026-11-24T14:03:10Z",
  "payload": { "kind": "ticket_request" } }
```

| Field | Rule |
|---|---|
| Body | ≤ 16 KiB, `application/json`, unknown fields rejected at every level (M2C-52) |
| `event_id` | `required,uuid` (UUIDv7 recommended; any version accepted) |
| `type` | One of the nine types in §7.2 |
| `occurred_at` | RFC 3339; must be ≤ server now + 60 s. Stored, never used for business time (M2C-13) |
| `payload` | Per type, §7.2 |

### 7.2 Event types

| Type | Kinds allowed (M2C-21) | Payload | `data` on 200 |
|---|---|---|---|
| `podium.vehicle_detected` | podium | `{}` | `{result: "ack"}` |
| `entry.credential_presented` | dispenser (`ticket_request`), entry_barrier (`employee_card`, `reservation_qr`) | `{kind: "ticket_request"\|"employee_card"\|"reservation_qr", value?: string ≤ 512}` (`value` required except for `ticket_request`) | `{result: "open", stay_id, ticket_code?}` or `{result: "deny", reason}` |
| `barrier.passed` | entry_barrier (`entry`), exit_barrier (`exit`) | `{direction: "entry"\|"exit", stay_id: uuid}` | `{result: "ack", reason?: "gate.duplicate_pass"}` or `{result: "rejected", reason}` |
| `paystation.scan` | pay_station | `{credential: {kind: "ticket"\|"employee_card"\|"reservation_qr", value}}` | `{result: "ack", stay_id, status, amount_cents, breakdown}` or `{result: "rejected", reason}` |
| `paystation.lost_ticket` | pay_station | `{stay_id: uuid}` | `{result: "ack", stay_id, ticket_code, amount_cents, breakdown}` or `rejected` |
| `paystation.cash_paid` | pay_station | `{stay_id: uuid, amount_cents: int ≥ 1}` | `{result: "ack", receipt_no, amount_cents, paid_at, exit_deadline}` or `{result: "rejected", reason, amount_cents?}` |
| `exit.credential_presented` | exit_barrier | `{credential: {kind, value}}` | `{result: "open", stay_id}` or `{result: "deny", reason, stay_id?, amount_cents?}` |
| `sensor.space_changed` | sensor_gateway | `{space_code: "E001".."E999", occupied: bool}` | `{result: "ack"}` or `{result: "rejected", reason: "lot.space_unknown"}` |
| `heartbeat` | any | `{}` | `{result: "ack"}` |

`breakdown` = `{kind: "time"|"grace"|"lost_ticket"|"employee"|"overstay"|"paid", duration_seconds, full_blocks, remainder_seconds, paid_cents, tariff_effective_from}`. Every `stay_id` is a stay `public_id` scoped to the device's lot; another lot's stay is `gate.stay_unknown`.

### 7.3 Authentication (M2C-16, M2C-17, M2C-18, M2C-23)

1. `X-Device-Id` (uuid), `X-Timestamp` (strict `time.RFC3339`), `X-Signature` (64 lowercase hex) present and well-formed → else `401 device.unauthenticated`.
2. `|server_now − X-Timestamp| ≤ 60 s` → else `401 device.timestamp_out_of_window`.
3. Read the raw body (16 KiB cap → `413 http.payload_too_large`). Compute `canonical = METHOD + "\n" + RequestURI + "\n" + X-Timestamp + "\n" + hex(sha256(body))`.
4. `select id, public_id, lot_id, kind, status, secret_ciphertext, secret_key_id from devices where public_id = $1`. Missing or `revoked` → compute the HMAC with a fixed dummy key (equal work), then `401 device.unauthenticated`. Otherwise open the secret and compare `hmac.Equal`. Mismatch → `401 device.unauthenticated`.
5. `authz.WithDevice(ctx, d)`, fill `LogFields.DeviceID`, touch `last_seen_at` if older than 30 s (outside the event transaction, best effort), restore the body for the handler.

Failures log `device auth failed` at warn with `claimed_device_id`, `reason` (`headers`, `skew`, `unknown`, `revoked`, `signature`), and the client IP. The response never says which.

### 7.4 Idempotency and storage (M2C-19, M2C-26)

```
pre-check: select response, body_sha256 from device_events where device_id = $d and event_id = $e
  found, same hash  → 200 stored response (unseal), header Idempotent-Replayed: true
  found, other hash → 409 device.event_conflict
not found → InTx {
  result := gate.Handle(...)                       // M2C-29 lock order
  insert into device_events (..., body_sha256, payload_redacted, response_sealed)   // LAST statement
}                                                   // pg_notify issued inside gate.Handle fires on commit
on 23505 (device_events_device_id_event_id_key) → re-run the pre-check outside the failed tx
```

- A retry after more than 60 s re-signs with a new `X-Timestamp`. The body (including `occurred_at`) is unchanged, so it is a replay, not a conflict.
- `4xx` answers (validation, not-allowed, conflict) and `5xx` store nothing. Repeating them gives the same answer.
- Redaction: `payload.value` and `payload.credential.value` become `"sha256:" + first 12 hex of sha256(value)`. Sealing: `response.ticket_code` becomes `response.sealed = {key_id, box}` (base64). The handler unseals before answering a replay.

### 7.5 Kind matrix and lot scoping (M2C-21)

The matrix in §7.2 is a `map[Kind]map[Type]bool` plus the sub-rules (dispenser only `ticket_request`; entry barrier only `employee_card`/`reservation_qr` and `direction: entry`; exit barrier only `direction: exit`). Violations → `403 device.event_not_allowed`. Every query in `gate` takes `device.LotID`.

### 7.6 Rate limits (M2C-5, M2C-22)

| Rule | Limit | Key | Scope | Position |
|---|---|---|---|---|
| `device_ip` | 1000 / 1 s | client IP | Local | before `DeviceAuth` |
| `device` | 100 / 1 s (`DEVICE_RATE_LIMIT_PER_S`) | device public id | Local | after `DeviceAuth` |

`429 rate_limit.exceeded` with `Retry-After` (seconds, at least 1). The simulator retries with the same `event_id` and body, honoring `Retry-After`, at most 5 times with 100 ms × 2ⁿ backoff.

**Batching check.** Busiest device per scenario at the 120× cap:

| Scenario | Peak arrivals (virtual) | Real arrivals/s at 120× | Sensor gateway events/s (2 per car in + out) | Dispenser / barriers events/s |
|---|---|---|---|---|
| `weekday` | 60 / h | 2 | ≤ 4 | ≤ 2 each |
| `weekend_peak` | 90 / h | 3 | ≤ 6 | ≤ 3 each |
| `fill_to_full` | 600 / h | 20 | ≤ 20 | ≤ 20 each |

Every device stays under 100/s with a 5× margin. One event per request; no batching.

### 7.7 Errors on the device route

| HTTP | Code | When |
|---|---|---|
| 401 | `device.unauthenticated` | Missing or malformed headers, unknown or revoked device, bad signature |
| 401 | `device.timestamp_out_of_window` | `X-Timestamp` outside ±60 s |
| 403 | `device.event_not_allowed` | Kind matrix |
| 409 | `device.event_conflict` | Same `event_id`, different body |
| 413 / 415 / 400 | `http.*` | Body cap, content type, invalid JSON |
| 422 | `validation.failed` | Envelope or payload rule, unknown field |
| 429 | `rate_limit.exceeded` | §7.6 |

### 7.8 Router and authorization (M2C-48)

`router.Policy{Device: true}` is valid only on `/v1/devices/` paths and only alone (no `Public`, no `Roles`, no `LegalExempt`); anything else panics at startup. The authz matrix adds three principals for device routes: anonymous (no headers) → 401 `device.unauthenticated`; a valid **user bearer** → 401 `device.unauthenticated` (bearer is ignored); a valid device → 200 for an allowed type, 403 for a disallowed one.

### 7.9 Error codes added to `errcodes`

| Code | HTTP / where | When |
|---|---|---|
| `device.unauthenticated` | 401 | §7.3 |
| `device.timestamp_out_of_window` | 401 | §7.3 |
| `device.event_not_allowed` | 403 | §7.5 |
| `device.event_conflict` | 409 | §7.4 |
| `lot.not_found` | 404 | Unknown or malformed lot id on public routes |
| `lot.space_unknown` | `reason` (200) | Sensor for a code not in the lot |
| `gate.lot_full` | `reason` | Drive-up or employee entry with public available − pending ≤ 0 |
| `gate.lot_physically_full` | `reason` | Defined now for the contract; returned from M3 (FR-GATE-2) |
| `gate.lot_closed` | `reason` | Entry while `lots.status = 'closed'` |
| `gate.credential_invalid` | `reason` | Unknown ticket, unknown or revoked card, any reservation QR in M2 (M2C-27) |
| `gate.credential_in_use` | `reason` | Card already has an open stay |
| `gate.payment_required` | `reason` | Exit with amount due > 0 (`amount_cents` included) |
| `gate.amount_mismatch` | `reason` | Cash amount ≠ server amount |
| `gate.nothing_due` | `reason` | Cash for a stay that owes nothing |
| `gate.stay_unknown` | `reason` | `stay_id` not found in the device's lot, or in a state the event cannot act on |
| `gate.exit_not_authorized` | `reason` | Exit pass without an authorization in the last 60 s |
| `gate.duplicate_pass` | `reason` on `ack` | Second pass in the same direction |
| `simulator.unauthorized` | 401 (simulator) | Missing or wrong `X-Simulator-Token` |
| `simulator.already_running` | 409 | Start while a scenario runs |
| `simulator.not_running` | 409 | Stop with nothing running |
| `simulator.scenario_unavailable` | 422 | `reservation_arrivals`, `no_show` in M2 |
| `simulator.car_not_found` | 404 | Unknown or already-gone car id |
| `session.logout_unconfirmed` | 502 (web BFF only) | §14 |

Every code gets `errors.<code>` in es-DO and en in **both** web and mobile in the same PR, because `scripts/check-i18n-parity.mjs` reads the whole `ERROR_CODES` list (policies §2.8). Device codes are rarely shown, so the copy is short and technical.

---

## 8. Transactions, locks and notifications

### 8.1 Transaction scope

One device event = one short transaction: no network I/O inside, `set local statement_timeout = '2s'`, `set local lock_timeout = '1s'`. A timeout rolls back, answers `500 internal.unexpected` (cause logged) and stores nothing, so the device retries with the same `event_id`.

### 8.2 Lock order (M2C-29)

| Flow | Locks, in order |
|---|---|
| Entry credential | `lot_occupancy` FOR UPDATE → insert `stays` |
| Barrier pass (entry or exit) | `lot_occupancy` FOR UPDATE → `stays` FOR UPDATE → insert `payments` (zero) → insert `device_events` |
| Pay-station scan, lost ticket, cash paid | `stays` FOR UPDATE → insert `payments` → insert `device_events` |
| Exit credential | `stays` FOR UPDATE → insert `device_events` |
| Sensor | update `spaces` (row lock) → insert `device_events` |
| Void job | `stays` (`for update skip locked`, batches of 200) |

### 8.3 Notifications (M2C-40, M2C-56)

- **Occupancy:** `update lot_occupancy set occupied = occupied ± 1, version = version + 1, updated_at = now() where lot_id = $1 returning version`, then `select pg_notify('lot_changed', $lot_id || ':' || $version)` in the same transaction.
- **Sensor:** `update spaces set status = $state, sensor_updated_at = now() where lot_id = $1 and code = $2 and status <> 'out_of_service' and status <> $state returning code`. Only when a row returns: `pg_notify('space_changed', lot_id || ':' || code || ':' || state)`. When nothing returns, the code is checked for existence to tell "unchanged" (`ack`) from "unknown" (`rejected lot.space_unknown`).
- Payloads carry the internal lot id. They never leave the server.

### 8.4 Time

`clock.Clock` is injected into `devices` (window), `gate` (all timestamps written explicitly as `$now`, never `now()` in SQL for business columns), `availability` (heartbeat ticker) and the worker jobs. SQL `now()` stays only in `created_at`/`received_at` defaults.

---

## 9. Entry

### 9.1 Flow

```
podium.vehicle_detected (podium) → ack
entry.credential_presented (dispenser, ticket_request) ──decision under lock──► open {stay_id, ticket_code} | deny {reason}
barrier.passed {entry, stay_id} (entry barrier) ──► occupied +1, version +1, NOTIFY
```

### 9.2 Decision under lock (spec §5.2 rules 1 and 5, M2C-10, M2C-30)

```sql
select o.occupied, l.capacity, l.status from lot_occupancy o join lots l on l.id = o.lot_id
 where o.lot_id = $1 for update of o;
select count(*) from spaces where lot_id = $1 and status = 'out_of_service';
select count(*) from stays  where lot_id = $1 and status = 'entry_pending' and accepted_at > $now - interval '60 seconds';
```

`holds := ReservationHolds.EffectiveHolds(lot, now)` (0 in M2). Domain rule (TDD), `availability.CanAdmitDriveUp`:

| Check | Outcome |
|---|---|
| `lots.status = 'closed'` | deny `gate.lot_closed` |
| `effective_capacity − (occupied + pending) − holds ≤ 0` | deny `gate.lot_full` |
| otherwise | open |

### 9.3 Ticket issue

On open, in the same transaction: tariff version at `$now`, ticket code (M2C-26), insert `stays (credential_kind 'ticket', status 'entry_pending', accepted_at $now, tariff_version_id, ticket_code_hash)`. Response `{result: "open", stay_id, ticket_code}`.

### 9.4 Reservation QR (M2C-1, M2C-27)

`kind: reservation_qr` → `deny gate.credential_invalid` without taking the lot lock. M3 replaces this branch with FR-GATE-2.

### 9.5 Employee card (FR-GATE-3, M2C-28)

Hash the UID → `employee_cards` in the device's lot with `status = 'active'`, else `deny gate.credential_invalid`. Same capacity decision as drive-up. Insert the stay (`credential_kind 'employee_card'`); a violation of `stays_card_open_key` → `deny gate.credential_in_use` (checked by a lookup first, the index is the backstop). Audit `gate.employee_entry` (actor `system`, entity `stay`, `after: {employee_card_id: <card public id>, device_id: <device public id>}`). No ticket code.

### 9.6 Barrier pass (FR-GATE-4, M2C-31)

| Direction | Stay status | Effect | Response |
|---|---|---|---|
| entry | `entry_pending` | `active`, `entered_at = $now`, occupancy +1 | `ack` |
| entry | `voided` | `active`, `entered_at = $now`, `voided_at` kept, occupancy +1, warn log, audit `gate.late_entry_pass`. If it is a card stay and the card already has a newer open stay (`stays_card_open_key`), nothing changes | `ack`, or `rejected gate.credential_in_use` for the card conflict |
| entry | `active`, `paid` | none | `ack`, `reason gate.duplicate_pass` |
| entry | `exited`, not found, other lot | none | `rejected gate.stay_unknown` |
| exit | `paid` or `active`, with `exit_authorized_at ≥ $now − 60 s` | §10.5 | `ack` |
| exit | `exited` | none | `ack`, `reason gate.duplicate_pass` |
| exit | `active`/`paid` without a fresh authorization | none | `rejected gate.exit_not_authorized` |
| exit | `entry_pending`, `voided`, not found | none | `rejected gate.stay_unknown` |

`check (occupied >= 0)` backs the exit decrement. The invariant `occupied = count(active, paid)` is asserted after every integration and scenario test (§15.5).

---

## 10. Pay station and exit

### 10.1 Scan (FR-GATE-5, FR-GATE-7)

Resolve the credential in the device's lot: ticket → `ticket_code_hash`; employee card → the card's open stay; reservation QR → `rejected gate.credential_invalid`. Not found or not `active`/`paid` → `rejected gate.credential_invalid`.

- `paid` and `$now ≤ exit_deadline_at` → `amount_cents 0`, `breakdown.kind "paid"`.
- `paid` and `$now > exit_deadline_at` → `paid → active` (lazy, M2C-35), then price as overstay.
- `active` → price with §12 (`entered_at → $now`, the stay's tariff version, `lost_ticket`, payments so far).

### 10.2 Lost ticket (FR-GATE-8 pay-station path, M2C-33)

`paystation.lost_ticket {stay_id}`: the stay must be a `ticket` stay in `active` or `paid`. Set `lost_ticket = true`, generate a new code and replace `ticket_code_hash`. A `paid` stay moves back to `active` (it owes money again). Price with the lost formula: due = `max(0, fee_lost − paid)`, because the lost fee replaces the time charge for the first block, so a prior hourly payment counts toward it (case L7). Marking an already-lost stay again only rotates the code. Response includes the replacement `ticket_code` (sealed in storage).

### 10.3 Cash paid (M2C-34)

Lock the stay; recompute the due amount with the same `amountDue` as `scan` at `$now` (§12.2). `due = 0` → `rejected gate.nothing_due`. `amount_cents ≠ due` → `rejected gate.amount_mismatch` with `amount_cents: due`. Otherwise insert `payments (parking_fee, cash, succeeded, idempotency_key cash:<device>:<event_id>, device_id)`, set `status 'paid'`, `paid_at = $now`, `exit_deadline_at = $now + post_payment_exit_min` (from the stay's tariff version). Response `{receipt_no, amount_cents, paid_at, exit_deadline}`.

### 10.4 Exit credential (FR-GATE-6)

Resolve the credential as in §10.1 (lazy `paid → active` included) and compute the due amount with the shared `amountDue` (§12.2). Then:

| State | Decision |
|---|---|
| `paid`, inside the window | open; `exit_authorized_at = $now` |
| `active`, amount due 0 (grace or employee) | open; `exit_authorized_at = $now` (the zero decision is taken now, M2C-32) |
| `active`, amount due > 0 | `deny gate.payment_required`, `amount_cents` |
| reservation QR | `deny gate.credential_invalid` |

### 10.5 Exit pass

Under the lot lock: an `active` stay with a fresh authorization → insert `payments (parking_fee, none, 0, succeeded, zero:<stay>)`, `paid_at = exited_at = $now`, status `exited` (the `active → paid → exited` hop happens inside one transaction, spec §10.2 #20). A `paid` stay → `exited_at = $now`, `exited`. Both: occupancy −1, version +1, NOTIFY.

---

## 11. Availability

### 11.1 Endpoints (all `Public`, `security: []`)

| Method and path | Limit | Success `data` | Errors |
|---|---|---|---|
| `GET /v1/lots` | `public_read_ip` | `LotSummary[]` (`meta.page.next_cursor: null`) | — |
| `GET /v1/lots/{id}` | `public_read_ip` | `Lot` | `lot.not_found` 404 |
| `GET /v1/lots/{id}/availability` | `public_read_ip` | `Availability` | `lot.not_found` |
| `GET /v1/lots/{id}/spaces` | `public_read_ip` | `{spaces: [{code, state}]}` (280 items, ordered by code) | `lot.not_found` |
| `GET /v1/lots/{id}/tariff` | `public_read_ip` | `Tariff` | `lot.not_found` |
| `GET /v1/lots/{id}/availability/stream?include=spaces` | `sse_connect_ip` + caps | `text/event-stream` (§11.3) | `lot.not_found` 404, `rate_limit.exceeded` 429 (before the stream starts) |

```jsonc
// LotSummary
{ "id": "uuid", "name": "Parqueo Banreservas Zona Colonial", "slug": "zona-colonial" }
// Lot
{ "id": "uuid", "slug": "…", "name": "…", "address": "…", "latitude": null, "longitude": null,
  "map_url": "https://www.google.com/maps/search/?api=1&query=<encoded>", "timezone": "America/Santo_Domingo",
  "capacity": 280, "opening_hours": { "always_open": true }, "status": "open" }
// Availability (also the SSE availability payload)
{ "lot_id": "uuid", "available": 12, "occupied": 268, "effective_capacity": 280, "capacity": 280,
  "effective_holds": 0, "upcoming_reservations_2h": 0, "status": "open" | "full" | "closed",
  "version": 1042, "updated_at": "2026-11-24T14:03:11Z" }
// Tariff
{ "effective_from": "…", "currency": "DOP", "first_hour_cents": 5000, "additional_hour_cents": 2500,
  "exit_grace_minutes": 10, "post_payment_exit_minutes": 15, "daily_cap_cents": 40000,
  "lost_ticket_cents": 50000, "reservation_fee_cents": 15000 }
```

`updated_at` is `lot_occupancy.updated_at` (the last count change, FR-AVAIL-1 "last update time"). Space `state` is `free`, `occupied` or `out_of_service`. No device ids, no stay ids, no times per space (M2C-7).

### 11.2 Snapshot (domain, TDD)

`availability.Compute(capacity, outOfService, occupied, holds, lotStatus)`:

```
effective_capacity = capacity − out_of_service
available          = max(0, effective_capacity − occupied − holds)
status             = closed if lot closed; else full if available == 0; else open
```

One query reads `lots`, `lot_occupancy` and the out-of-service count; the holds port adds the two reservation numbers. Pending entries do **not** change the displayed numbers (spec §5.2 rule 5).

### 11.3 SSE wire format (M2C-38)

```
HTTP/1.1 200 OK
Content-Type: text/event-stream
Cache-Control: no-store
X-Accel-Buffering: no

retry: 3000

event: availability
id: 1042
data: {"lot_id":"…","available":12,…,"version":1042}

event: spaces                    (only with include=spaces; first one is full, then deltas)
data: {"full":true,"spaces":[{"code":"E001","state":"occupied"},…]}

event: spaces
data: {"full":false,"spaces":[{"code":"E117","state":"free"}]}

event: heartbeat
data: {"as_of":"2026-11-24T14:03:26Z"}
```

Heartbeat every 15 s (fake-clock driven). The JSON envelope does not wrap SSE `data` lines; each `data` is the bare object, documented in OpenAPI as the event payload schemas.

### 11.4 Hub (M2C-39)

```
pubsub.Listener ──lot_changed(lot, v)──► hub.lot[lot].bump(v)
                                          │ leading edge: push now if last push ≥ 250 ms ago
                                          │ trailing edge: else one timer at last+250 ms
                                          ▼
                              snapshot := service.Snapshot(lot)   (one query per lot per push)
                              bytes := encode once
                              for each subscriber: replace its 1-slot buffer (latest wins)
space_changed(lot, code, state) ──► append to each subscriber's delta queue (cap 300); overflow → mark resync (full snapshot next)
listener reconnected            ──► every lot: fresh availability + full spaces to every subscriber
```

Handler loop per stream: `select` over the subscriber channels, the heartbeat ticker, `r.Context().Done()` and `hub.Done()`. Before each write, `http.ResponseController.SetWriteDeadline(now + 10 s)`; on error the stream ends. `AccessLog`'s writer already has `Unwrap`; `Recover` and any other wrapper must keep `Unwrap` so `Flush` reaches the connection (test: a stream through the full production chain receives its first event). `hub.Close()` on shutdown ends every stream before `srv.Shutdown`, so shutdown does not wait out its timeout.

### 11.5 Caps (M2C-53)

A per-IP counter (`map[ip]int` under a mutex) and a global atomic counter, checked before the `200`. Over the cap → `429 rate_limit.exceeded` with `Retry-After: 10`. Released when the handler returns. Browsers (`EventSource`) connect directly to Go, so `CORS_ALLOWED_ORIGINS` must include the web origin and the web CSP `connect-src` must allow the API origin (Track B). Mobile uses `expo/fetch` streaming (no CORS).

### 11.6 Listener

`pubsub.Listener` acquires one pool connection, hijacks it, runs `LISTEN lot_changed; LISTEN space_changed`, and loops on `WaitForNotification`. On error: mark disconnected (`/readyz` → 503 `health.not_ready`, check name `listener`), reconnect with backoff 250 ms → 5 s, then trigger the hub resync. The pool keeps `DB_MAX_CONNS − 1` for requests.

---

## 12. Fee calculator (spec §5.1, TDD)

### 12.1 Inputs

`Tariff{FirstHour, AdditionalHour, DailyCap, LostTicket int64; ExitGrace, PostPaymentExit time.Duration}`, `Stay{Kind, EnteredAt, LostTicket, PaidCents int64, Status, ExitDeadline}`, `now`. All money in centavos, durations in whole seconds `d = floor(now − entered_at)` (M2C-36).

### 12.2 Algorithm

```
blockFee(r) = r == 0 ? 0 : min(cap, first + ceilDiv(max(r − 3600, 0), 3600) × additional)

timeFee(d):
  if d ≤ grace:                   return 0
  full = d / 86400 ; rem = d − full × 86400
  return full × cap + blockFee(rem)

lostFee(d):                       // M2C-33: applies even inside the grace
  if d < 86400:                   return lost
  full = d / 86400 ; rem = d − full × 86400
  return lost + (full − 1) × cap + blockFee(rem)

fee(d) = employee ? 0 : (lost ? lostFee(d) : timeFee(d))

amountDue(stay, now):
  employee                                               → 0
  status paid and now ≤ exit_deadline                     → 0
  exit_deadline is set and now > exit_deadline            → max(additional, fee(d) − paid)   // overstay, M2C-35, any status
  otherwise (active, never paid or lost-on-paid in window) → max(0, fee(d) − paid)
```

The overstay floor depends on the **deadline**, not on the status: the lazy `paid → active` transition leaves `exit_deadline_at` set, so every later evaluation still applies the floor. `paystation.scan`, `paystation.lost_ticket`, `paystation.cash_paid` and `exit.credential_presented` all call this one `amountDue`, so their amounts always agree.

`ceilDiv(a, b) = (a + b − 1) / b` on non-negative integers. Grace applies to the **total** duration only, never to a remainder block.

### 12.3 Defaults used in the table

first 5000, additional 2500, cap 40000, lost 50000, grace 600 s, exit window 15 min.

### 12.4 Test table (the suite must include every row)

| # | Case | Duration | Paid so far | Expected (cents) | Why |
|---|---|---|---|---|---|
| T1 | ticket | 0 s | 0 | 0 | grace |
| T2 | ticket | 9 m 59 s | 0 | 0 | grace |
| T3 | ticket | 10 m 00 s | 0 | 0 | grace boundary is inclusive (≤) |
| T4 | ticket | 10 m 01 s | 0 | 5000 | first hour |
| T5 | ticket | 59 m 59 s | 0 | 5000 | |
| T6 | ticket | 1 h 00 m 00 s | 0 | 5000 | `ceil(0) = 0` |
| T7 | ticket | 1 h 00 m 01 s | 0 | 7500 | one additional unit |
| T8 | ticket | 2 h 00 m 00 s | 0 | 7500 | |
| T9 | ticket | 2 h 00 m 01 s | 0 | 10000 | |
| T10 | ticket | 14 h 00 m 00 s | 0 | 37500 | 5000 + 13 × 2500 |
| T11 | ticket | 14 h 00 m 01 s | 0 | 40000 | reaches the cap exactly |
| T12 | ticket | 15 h 00 m 01 s | 0 | 40000 | capped (42500 → 40000) |
| T13 | ticket | 23 h 59 m 59 s | 0 | 40000 | |
| T14 | ticket | 24 h 00 m 00 s | 0 | 40000 | one full block, remainder 0 |
| T15 | ticket | 24 h 00 m 01 s | 0 | 45000 | remainder of 1 s pays a first hour |
| T16 | ticket | 24 h 09 m 00 s | 0 | 45000 | no grace on a remainder |
| T17 | ticket | 25 h 00 m 01 s | 0 | 47500 | |
| T18 | ticket | 48 h 00 m 00 s | 0 | 80000 | two blocks |
| T19 | ticket | 75 h 00 m 00 s | 0 | 130000 | 3 × 40000 + (5000 + 2 × 2500) |
| L1 | lost | 5 m | 0 | 50000 | lost fee ignores grace |
| L2 | lost | 3 h | 0 | 50000 | replaces the time charge |
| L3 | lost | 23 h 59 m 59 s | 0 | 50000 | above the cap by design |
| L4 | lost | 24 h 00 m 00 s | 0 | 50000 | first block replaced, remainder 0 |
| L5 | lost | 26 h 00 m 00 s | 0 | 57500 | 50000 + blockFee(2 h) |
| L6 | lost | 50 h 00 m 00 s | 0 | 97500 | 50000 + 40000 + 7500 |
| L7 | lost after an hourly payment, inside the window | 1 h 40 m, marked lost (stay back to `active`) | 7500 | 42500 | `fee_lost − paid` |
| E1 | employee | 30 h | 0 | 0 | |
| O1 | overstay | scan at 1 h 50 m, paid at 1 h 30 m | 7500 | 2500 | `fee = 7500`, `7500 − 7500 = 0` → minimum unit |
| O2 | overstay | scan at 3 h 10 m, paid at 1 h 30 m | 7500 | 5000 | `12500 − 7500` |
| O3 | overstay | scan at 23 h, paid at 20 h | 40000 | 2500 | at the cap, minimum unit still applies (spec FR-GATE-7) |
| O4 | inside the window | scan at 1 h 44 m, paid at 1 h 30 m | 7500 | 0 | |
| O5 | window boundary | scan at exactly paid + 15 m 00 s | 7500 | 0 | deadline is inclusive (≤) |
| O6 | lost + overstay | scan at 2 h 20 m, paid 50000 at 2 h | 50000 | 2500 | |
| O7 | second overstay | scan at 2 h 20 m, payments 7500 + 2500, last paid at 1 h 50 m | 10000 | 2500 | `10000 − 10000 = 0` → minimum unit |
| O8 | overstay flow agreement | after O1's scan (stay now `active`, deadline 1 h 45 m kept): `cash_paid 2500` at 1 h 50 m; separately, an exit credential at 1 h 50 m before paying | 7500 | cash → `ack`; exit → `deny gate.payment_required`, 2500 | the floor survives the lazy transition; scan, cash and exit share `amountDue` |

Plus property tests: the result is never negative; for ticket stays `timeFee` is non-decreasing in `d`; `timeFee(d) ≤ ceil(d / 24 h) × cap` for `d > grace`.

### 12.5 Worker jobs (M2C-9, M2C-30, M2C-43)

| Job | Cadence | Mechanism |
|---|---|---|
| `void_unpassed_entries` | 15 s | `update stays set status = 'voided', voided_at = $now, updated_at = $now where id in (select id from stays where status = 'entry_pending' and accepted_at < $now − interval '60 seconds' order by accepted_at limit 200 for update skip locked)`; repeat while 200 rows. No occupancy change, no NOTIFY |
| `device_events_purge` | 24 h | `select purge_device_events(5000)` until it returns < 5000. Logs the count |

---

## 13. Simulator

### 13.1 Engine and clocks (M2C-8, M2C-13, M2C-44)

```go
type Clock interface {             // simulator-local; virtual time
    Now() time.Time
    WaitUntil(ctx context.Context, t time.Time) error   // real: sleeps (t − now)/speed; test: returns at once
}
```

- The engine pops the earliest event `(at, seq)` from a min-heap, sets the virtual time, runs the step (which may call the device API and schedule follow-ups), and repeats. One goroutine; `ctx` cancels it.
- `math/rand/v2.NewPCG(seed, seed^const)`. Every random draw happens in the engine goroutine in a fixed order. Server responses (ticket codes, stay ids) are stored but never feed the RNG.
- The **device client** signs with a separate `signClock` (wall clock in compose; the shared fake clock in CI, M2C-14). It retries per §7.6.
- Virtual start time: the scenario's `start_at` (default today at 07:00 America/Santo_Domingo for `weekday`/`weekend_peak`, now for others).

### 13.2 Scenarios (M2C-45)

| Scenario | M2 | Arrivals (Poisson per virtual hour) | Dwell | Other |
|---|---|---|---|---|
| `idle` | yes | none | — | heartbeats every 60 s per device |
| `weekday` | **required** | 00–06: 2 · 07: 30 · 08: 60 · 09: 45 · 10–11: 30 · 12–13: 40 · 14–16: 30 · 17: 45 · 18: 30 · 19–21: 10 · 22–23: 4 | log-normal, median 90 min, σ 0.8, clipped to [3 min, 10 h] | 30 employee cards arrive 07:30–08:30 (uniform) and stay 8–9 h; lost ticket p = 0.01; late after paying (beyond 15 min) p = 0.03 |
| `weekend_peak` | yes | 00–08: 3 · 09: 40 · 10–16: 90 · 17–19: 50 · 20–23: 15 | median 120 min, σ 0.7 | no employees |
| `fill_to_full` | **required** | 600 / h | no exits | stops arriving after 3 consecutive `gate.lot_full` denials and reports `reached_full: true` |
| `sensor_drift` | best effort | as `weekday` | as `weekday` | 3 random sensors stuck; 10% of sensor events dropped (map ≠ count; the alert is M4) |
| `reservation_arrivals`, `no_show` | no | — | — | `422 simulator.scenario_unavailable` (M2C-1) |

Car lifecycle (FR-SIM-4): `podium.vehicle_detected` → `entry.credential_presented` → on open, after 3–8 s virtual: `barrier.passed entry` → after 20–60 s: `sensor.space_changed occupied` on a random free space (from the simulator's own free set) → dwell → `paystation.scan` (skipped when the scan would be 0 and the car is an employee) → `paystation.cash_paid` with the returned amount → 1–10 min walk → `exit.credential_presented` → `barrier.passed exit` → `sensor.space_changed free`. A deny at any step is logged and the car leaves (entry) or re-pays (exit, `gate.payment_required`).

### 13.3 Control API (M2C-46, M2C-54)

All under `/control`, header `X-Simulator-Token` required (constant-time compare; missing or wrong → `401 simulator.unauthorized`). JSON, baseline envelope, bodies strict.

| Method and path | Body | Success | Errors |
|---|---|---|---|
| `GET /control/status` | — | 200 `{scenario, running, seed, speed, virtual_now, cars_inside, started_at, reached_full}` | — |
| `POST /control/scenarios/start` | `{scenario, seed?: uint64, speed?: 1–120, start_at?: RFC3339}` | 202 status | `simulator.already_running` 409, `simulator.scenario_unavailable` 422 |
| `POST /control/scenarios/stop` | `{drain?: bool}` | 202 status | `simulator.not_running` 409 |
| `POST /control/cars` | `{credential: "ticket"\|"employee_card", card?: "EMP-007", hold?: bool, dwell_minutes?: 1–600}` | 202 `{car_id}` | `validation.failed` |
| `POST /control/cars/{car_id}/leave` | `{lose_ticket?: bool}` | 202 | `simulator.car_not_found` 404 |
| `POST /control/sensors/{code}/fault` | `{fault: "stuck_occupied"\|"stuck_free"\|"silent"\|"none"}` | 204 | `validation.failed` |
| `GET /control/events?after=<seq>` | — | 200 `[{seq, at, device, type, result, reason?, car_id?}]` (≤ 500, ring buffer) | — |

FR-SIM-5 "present a specific reservation QR" is absent in M2 (M2C-1). "Present an employee card" is `POST /control/cars` with `credential: employee_card`. "Mark ticket lost" is `leave` with `lose_ticket: true`. Injected cars run under the current scenario's clock, or the real clock at speed 1 when idle.

**Demo path (M2C-13):** inject one held car at the start of the demo, run `weekday`, and ~12 minutes later call `leave`: it scans (RD$50), pays cash and exits while the count and map move.

### 13.4 Lifecycle and health (M2C-47)

`/healthz` (process), `/readyz` (API `/readyz` answered 200 in the last 30 s). `stop {drain: true}` schedules leave for every car inside and completes when they are gone. A restart forgets cars; their stays stay `active`. The demo script resets with `docker compose down -v`.

---

## 14. M1 leftovers (M2C-11)

| # | Item | Fix | Slice |
|---|---|---|---|
| L1 | `session.logout_unconfirmed` is returned by the web BFF but missing from the catalog | Add `SessionLogoutUnconfirmed` to Go `errcodes` with a comment "returned by the web BFF only"; `go generate`; `errors.session.logout_unconfirmed` in es-DO and en in web and mobile | C1 |
| L2 | Mobile logout-all treats 401 as success, web as unconfirmed | `apps/mobile/src/services/session.ts`: scope `all` + 401 → unconfirmed → `logout_all_failed` notice (strings exist). Update `session.test.ts` | C1b |
| L3 | Docs say 401 on logout-all is success | Clients design §5.5 (table and "Logout and the 401 rule") and spec §10.2 #37: 401 on logout is success; 401 on logout-all is unconfirmed on both clients | C1b |
| L4 | Strict zod response schemas | Note in spec §8 M5 scope: switch response parsing to non-strict before the first store build. No code in M2 | C1b |

---

## 15. Testing strategy

All tests run with `-race`, an injected clock and seeded randomness, with no sleep-based assertions (policies §2.3).

### 15.1 Unit (TDD first)

| Package | Cases |
|---|---|
| `tariffs` | §12.4 table, property tests, tariff validation |
| `availability` | `Compute` (capacity, out of service, holds, closed precedence, floor at 0); `CanAdmitDriveUp` with pending; coalescer with a fake clock (leading edge, trailing edge, ≤ 4/s, bursts collapse to one) |
| `gate` | Stay state machine table: every allowed transition and every rejected one (§9.6, §10); amount-due selection |
| `devices` | **Golden vector**: a fixed secret, method, URI with query, timestamp and body → a fixed signature, shared with the simulator client test; header parsing; window boundaries (±60 s inclusive, 61 s rejected); kind matrix; redaction and sealing round trip |
| `ratelimit` | Memory adapter: window roll with fake clock, concurrent `Allow`, sweep |
| `simulator` | Same seed → identical request trace (types, order, payloads minus server codes) over 2 virtual hours; Poisson rate sanity; scheduler ordering ties by sequence |
| `router` | Panics on `Device` combined with `Public`/`Roles`, or outside `/v1/devices/` |

### 15.2 Integration (real Postgres, as `mypark_app`)

- **Auth:** valid → 200; bad signature → 401; skew 61 s → 401 `device.timestamp_out_of_window`; unknown and revoked devices give byte-identical bodies (minus `request_id`); query string tampering and body tampering both → 401.
- **Idempotency:** replay → same `data`, `Idempotent-Replayed: true`, one `device_events` row, one stay; same id + different body → 409; replay after 90 s with a fresh signature → stored response; **20 concurrent duplicates** → one effect, 19 identical stored answers, no deadlock.
- **`TestEntry_ConcurrentLastSpace`:** occupied 279 → 20 parallel `ticket_request` from the dispenser → exactly one `open`, 19 `deny gate.lot_full`.
- Pending window: an `entry_pending` older than 60 s does not block entry even before the void job runs.
- Pass rules (§9.6 table), lot scoping (a stay from a second lot → `gate.stay_unknown`), kind matrix (sensor sending a credential → 403).
- Pay/exit: cash with the wrong amount → `amount_mismatch`; overstay → `paid → active` and a second payment; zero-due exit writes one `none` payment; lost ticket kills the old code.
- Employee: revoked card denied; second entry → `gate.credential_in_use`; audit row present.
- **`TestAvailability_PassToSSELatency`:** 50 SSE clients through the production handler; 200 passes; p95 from commit to event received < 2 s (evidence for NFR-PERF-1).
- SSE: snapshot first; heartbeat after 15 s of fake time; `include=spaces` full then deltas; caps → 429; listener drop → `/readyz` 503 then resync; shutdown ends streams within 1 s.
- Grants: §5.5. Void job and purge function with the fake clock.

### 15.3 Contract

Every M2 route returns only catalog codes; the OpenAPI parity tests (api and simulator) pass; the authz matrix includes device principals (M2C-48).

### 15.4 Log-leak test (extended)

`TestNoSecretsInLogs` adds: device secrets, the fleet secret, `SECRETBOX_KEY`, `CARD_UID_PEPPER`, ticket codes and card UIDs. The same assertion runs over every `device_events.payload` and `response` row (ticket codes appear only sealed).

### 15.5 Scenario tests in CI (FR-SIM-7, M2C-14)

`apps/server/cmd/simulator` integration tests build the API with `newHandler(deps)` on a migrated DB, share one fake clock with the engine and the worker's void function, and run:

| Test | Asserts |
|---|---|
| `TestScenario_FillToFull` | `available` reaches 0, the next dispenser request is `deny gate.lot_full`, `occupied = 280 = count(active)` |
| `TestScenario_WeekdayInvariants` | 3 virtual hours from 07:00 seed 42: never `occupied > effective_capacity`; after `stop {drain:true}` `occupied = 0`; at every check `occupied = count(active, paid)`; some exits paid cash (non-zero) and some RD$0 |
| `TestScenario_Deterministic` | Two runs with seed 42 on fresh DBs produce identical traces |

### 15.6 Coverage

≥ 90% statement coverage (the OC-35 proxy) on `features/tariffs` domain, `features/availability` domain and the `features/gate` state machine file, enforced by the existing CI coverage step.

### 15.7 M2 exit criteria, backend side

| Exit criterion (spec §8 M2) | Backend evidence |
|---|---|
| `weekday` runs; availability updates < 2 s on clients | `TestScenario_WeekdayInvariants`, `TestAvailability_PassToSSELatency`; client side is Track B |
| `fill_to_full` reaches 0, dispenser denies `gate.lot_full` | `TestScenario_FillToFull` |
| Tariff calculator table suite | §12.4 suite |
| Replayed event is a no-op; bad signature → 401 | Idempotency and auth integration tests |

---

## 16. Build order (chained PRs, ≤ 400 changed lines each)

| # | Slice | Contents | Sensitive | Depends on |
|---|---|---|---|---|
| C1 | **Contract A (handoff)** | `openapi.yaml`: lots, availability, spaces, tariff, SSE (`text/event-stream` with event schemas). All M2 error codes + `session.logout_unconfirmed` in `errcodes`, `go generate`, regenerated `packages/contracts`, translations in web and mobile. Both developers approve | — | — |
| C2 | **Contract B (handoff)** | `openapi.yaml`: device API (`deviceHmac` scheme, `oneOf` events), simulator control paths (`servers` override, `x-mypark-service`), parity-test filter. Both developers approve | device signatures | C1 |
| C1b | M1 leftovers | L2–L4 (§14) | — | C1 |
| S1 | Foundations | `secretbox` keyring, config additions and production refusals, `secret.Bytes`, `devicefleet` manifest + derivation, memory limiter + `Rule.Scope` | secrets | C2 |
| S2 | Tariffs | Migration 00007, sqlc, seed of versions, fee calculator + §12.4 suite, `GET /tariff` | migration | S1 |
| S3 | Devices schema | Migration 00008 (devices, device_events, purge function), grants tests, fleet seed | migration | S1 |
| S4 | Device auth | `authz.Device`, `DeviceAuth`, router `Policy.Device`, authz matrix, rate-limit wiring, golden vector | device signatures | S3 |
| S5 | Event pipeline | Envelope decode, kind matrix, idempotency store, redaction/sealing, `heartbeat`, `podium`, `sensor.space_changed` + `space_changed` NOTIFY | device signatures | S4 |
| S6 | Gate schema | Migration 00009 (cards, stays, payments), sqlc, card seed, grants tests | migration | S3 |
| S7 | Entry | Decision under lock, ticket issue, employee card, QR deny, holds port, concurrency test | — | S5, S6 |
| S8 | Pass and void | Barrier pass rules, occupancy NOTIFY, void job in the worker | — | S7 |
| S9 | Pay and exit | Scan, lost ticket, cash, exit credential, exit pass, zero payment | payments | S8, S2 |
| S10 | Availability REST | Lots, availability, spaces endpoints, `Compute`, limits | — | S6 |
| S11 | Real time | Listener, hub, SSE writer and stream, caps, `/readyz`, shutdown, latency test | — | S10, S8 |
| S12 | Simulator engine | Clock, scheduler, device client (retries), car lifecycle | — | S9 |
| S13 | Scenarios + control | Scenarios, control API, event ring buffer, compose env | — | S12 |
| S14 | Hardening | Scenario CI tests, log-leak extension, coverage gate, purge job, doc sync (§16.1) | — | all |

Every slice with a value in the **Sensitive** column carries the PR-template security checklist and an automated risk review (`review-risk` or `/security-review`) linked in the PR (policies §2.2, M2C-12). S10 can start right after S6 in parallel with S7–S9. Track B integrates against C1/C2 types with MSW mocks, then against S10–S11 mid-week (spec §8).

### 16.1 Docs to update in the same PRs (policies §2.10)

- `architecture.md` §4.4: `GET /v1/lots`, `/spaces`; heartbeat endpoint removed (M2C-15). §6: snapshot-first and `Last-Event-ID` semantics, coalescing details, `include=spaces`. §8.1: canonical string, error codes, unauthenticated requests not stored. §8.2: `result` field, `paystation.lost_ticket`, kind matrix. §8.3: fleet provisioning via env (M2C-25), control API auth. §10: void job and purge function. §11.1: new columns (`accepted_at`, `voided_at`, `exit_authorized_at`, `body_sha256`, `secret_key_id`), no `amount_due_cents`. §14: `SECRETBOX_*`, `CARD_UID_PEPPER`, `SIMULATOR_*`; `/metrics` in M5. §17: ADRs for the local limiter scope and the derived fleet secrets.
- `spec.md` §5.4: `voided → active` on a late pass. §10.2: append the accepted M2 calls and the #37 update (L3).
- `policies.md` §2.7: device and public-read limits (M2C-22, M2C-53).
- `deploy/compose/docker-compose.yml`: new env defaults for api, seed, simulator and web.

---

## 17. Risks and deferred items

| # | Risk or item | Mitigation / target |
|---|---|---|
| R1 | Fast-forwarded scenarios bill real minutes, so most demo stays are free (M2C-13) | Held-car demo path (§13.3); CI uses the shared fake clock for realistic fees |
| R2 | Per-instance rate limits (M2C-5, M2C-22) | One API instance in Phase 1; Postgres scope remains available per rule |
| R3 | Simulator crash orphans active stays (M2C-47) | `stop {drain:true}`; `compose down -v`; admin force-close in M4 |
| R4 | Tailgating (a pass without a stay) is not counted (M2C-31) | Sensors and M4 reconciliation surface it |
| R5 | The derived fleet secret exposes all simulated devices if leaked (M2C-25) | Dev-only, refused in production; real devices get random secrets in M4 |
| R6 | Five-day milestone with 17 slices | C1/C2 on day 1; S2, S3, S10 parallelizable; best-effort scenarios drop first |
| R7 | One fleet sensor gateway for 280 sensors is a single point of failure | Matches the spec's device list; a second gateway is a manifest entry |
| Deferred | Admin device/card CRUD, lost-ticket admin path, admin stream, reconciliation, snapshots | M4 |
| Deferred | `/metrics`, 500-client SSE perf run, non-strict response parsing | M5 |

---

## 18. Left to the builder

- sqlc query names, file splits and Go names inside each feature, within §4.1.
- Exact dev default values (secretbox key, pepper, fleet secret, control token); generate once and mark them dev-only in compose.
- The exact SQL form of `purge_device_events` returning a count (a CTE with `count(*)` over `delete … returning`).
- Whether the memory limiter sweeps on a ticker or lazily on access.
- Delta-queue capacity tuning (start at 300) and the SSE write deadline (start at 10 s).
- Log-normal implementation details (`math/rand/v2` `NormFloat64`), and Poisson thinning vs exponential gaps.
- Translation copy for the new codes (short, technical for `device.*`).

---

## 19. Changes vs spec and architecture

| Source | Original | This design | Decision |
|---|---|---|---|
| Architecture §4.4 | `POST /v1/devices/heartbeat` | Removed; `heartbeat` is an event type | M2C-15 |
| Architecture §4.4 | Lot routes `/{id}`, `/availability`, `/stream`, `/tariff` | Adds `GET /v1/lots` and `GET /v1/lots/{id}/spaces` | M2C-37 |
| Architecture §8.2 | `decision` / `ack` response fields | One `result` field (`ack`/`open`/`deny`/`rejected`) + `reason` | M2C-20 |
| Architecture §8.2 | No lost-ticket event | `paystation.lost_ticket {stay_id}` with a replacement code | M2C-33 |
| Architecture §8.3 | Keys provisioned into a mounted local file | Derived from `SIMULATOR_FLEET_SECRET` env; fixed public ids | M2C-25 |
| Architecture §11.1 | `stays.amount_due_cents`, `stays.reservation_id` | No `amount_due_cents`; `reservation_id` added in M3; new `accepted_at`, `voided_at`, `exit_authorized_at` | M2C-55, M2C-32 |
| Architecture §11.1 | `payments` full shape | Minimal M2 shape with final enums | M2C-3, M2C-41 |
| Architecture §14 | `/metrics` in Phase 1 | M5 | M2C-50 |
| Spec §5.4 | No `voided → active` | Late entry pass reactivates the stay | M2C-31 |
| Spec FR-GATE-8 | Admin or pay station marks lost | Pay station in M2; admin in M4 | M2C-33 |
| Spec FR-GATE-9 | Every device request logged in `device_events` | Every **authenticated** request; failed auth goes to logs | M2C-18 |
| Spec FR-GATE-2, FR-AVAIL-6, FR-SIM-3/5 (reservation parts) | In M2 scope | Placeholders until M3 | M2C-1 |
| M1 design §16.3 | Outbox encryption "revisit in M2" | Out of Phase 1 | M2C-51 |
