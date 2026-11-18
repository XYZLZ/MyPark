# M3 Track A — Backend Reservations and Payments: Design

**Status:** draft for review · **Date:** 2026-11-16 · **Milestone:** M3 (Nov 30 – Dec 4) · **Track:** A (Go backend, worker, simulator, API contract)

This document designs the Go side of M3: reservations with an atomic reservable cap, the reservation fee charged through a `PaymentGateway` port with a simulated adapter, refunds processed by the worker, the reservation QR at the entry barrier, pay station and exit, the worker jobs that expire, remind and notify, and the two reservation scenarios of the simulator. **The guarantee and the money are the center of gravity.** A confirmed driver must get in, the cap must hold under concurrency, and no fee may be charged twice or refunded twice or lost.

> **How to read this.** §1–§3 explain *why* and what is fixed. §4 compares the approaches. §5–§14 are the design. §15–§18 cover testing, build order, risks and what is left to the builder. §19 lists changes against the spec and the architecture, §20 the inconsistencies found between them, §21 the open questions. Decisions are numbered **M3B-n**. M3B-1 to M3B-4, M3B-44 and M3B-60 were made by the user and are binding (§2.1). The rest are **(our call)** and collected in §2.2 for review. Each decision lists its reason and what it costs if it turns out wrong.
>
> **Sources:** `docs/spec.md` (§1.5, §3.4 FR-RES, §3.5 FR-PAY, §3.6 FR-GATE-2/5/6, §3.7 FR-SIM-2/3/5, §3.11 FR-NOTIF-1, §4 NFR-TEST-1, §5.1–§5.4, §8 M3, §10), `docs/architecture.md` (§4.3, §4.4, §6, §7, §7.1, §9, §10, §11.1, §11.2), `docs/policies.md` (§1.1, §1.2, §2.2), the M2 backend design `docs/superpowers/specs/2026-11-13-m2-backend-availability-gates-simulator-design.md`, and the code under `apps/server` at `main` (`483920d`).

---

## 1. Intent

### 1.1 Why

M3 turns MyPark from "look before you drive" into "book before you drive". The demo promise is "reserve on mobile, pay, show the QR, the barrier opens, the stay is billed at exit". Behind it are four things that are hard to fix later:

- **The guarantee is the product.** Spec G2: a confirmed reservation is never denied entry inside its window because drive-ups took the spots. The hold must be netted out of public availability before the driver arrives, and the gate must honour the QR under the same lock that admits drive-ups.
- **The cap must hold under concurrency.** Two drivers booking the last reservable slot at the same instant must not both succeed (spec §1.5, FR-RES-4).
- **Money moves through an external system.** The gateway can succeed, decline, or not answer. Every path must end with exactly one charge or none, and every refund must happen exactly once (spec §1.5, FR-PAY-2).
- **Time drives the state.** Unpaid holds expire after 10 min, no-shows after `arrival + 20 min`, holds become effective 15 min before arrival. Everything must run on the server clock and be testable with the fake clock.

### 1.2 Goals

| # | Goal | Verified by |
|---|---|---|
| G1 | 200 parallel reservation attempts with overlapping windows never exceed the cap, and every slot's `held` equals a recount from `reservations` | `TestReservations_CapConcurrent200` (§15.2) |
| G2 | The guarantee scenario (spec §3.4) passes end to end with the simulator: drive-up denied `gate.lot_full`, QR opens | `TestScenario_Guarantee` (§15.5) |
| G3 | Pay: success → `confirmed`; decline → `402 payment.declined`, retry allowed; timeout → `202`, the worker finalizes. A fee is never charged twice for one reservation | Pay integration suite (§15.2) |
| G4 | Cancel before arrival → refund `succeeded` and payment `refunded`; cancel at or after arrival → no refund; no-show → `expired_no_show` | Cancel, refund and job tests (§15.2) |
| G5 | A QR presented while physically full → `gate.lot_physically_full`, the reservation is cancelled `facility_unavailable`, a refund is queued and an alert row is written, all in the device transaction; the refund then succeeds | `TestGateQR_PhysicallyFull` (§15.2) |
| G6 | After 3 no-shows in 30 days a new reservation is rejected `reservation.blocked_no_shows` with the unblock date | `TestNoShow_BlocksAfterThreshold` (§15.2) |
| G7 | Public availability reflects a hold change caused by an API action (confirm, cancel, check-in) within the NFR-PERF-1 budget, and a time-based hold change within one job interval | `TestHolds_ConfirmToSSE`, `TestHoldBoundaryJob` (§15.2) |
| G8 | The M3 contract (OpenAPI paths, error codes, regenerated contracts) is published **before** implementation | Task S1 (§16) |

### 1.3 Non-goals (M3)

- **Lot closure behaviour (FR-RES-15)** and the admin lot close/open endpoints: M4 (M3B-2). M3 only adds the `facility_closed` and `admin_override` refund reasons to the schema.
- **Reconciliation** (the periodic gate/sensor job, `sensor_drift` alerts, admin alert views and resolution): M4 (M3B-2). M3 creates the `reconciliation_alerts` table and writes rows only for the two incidents it owns (§6.2).
- Admin reservation search, admin cancel with refund override, admin refund views (FR-ADMIN-2): M4.
- Online payment of hourly parking fees (spec §1.4). Hourly fees stay cash at the pay station.
- Push notifications (Phase 2). Web and mobile screens (Track B).
- A real payment provider. Only the simulated adapter exists; the port is shaped for a swap.

### 1.4 Anti-goals (failure even if it "works")

| Anti-goal (spec §1.5) | Guard (section) |
|---|---|
| A reserved driver arrives within the window and is denied because drive-ups took the spots | Holds netted out of the drive-up decision under the lot lock; QR skips the public check (§11.1, spec §5.2 rules 1–2) |
| Two concurrent requests push reservations past the cap | Slot-load rows locked in ascending order in one transaction (§8.3, M3B-16) |
| A reservation fee is charged twice | One live fee payment per reservation (partial unique index), the gateway key is the payment row's id, the client `Idempotency-Key` replays the stored outcome (§9.3, M3B-7, M3B-8) |
| A refund is lost | The refund row is written in the cancellation transaction; the worker retries with backoff until `succeeded` or `failed` + alert (§12.5, M3B-29) |
| A refund is duplicated | One live refund per payment (partial unique index), gateway refund key is the refund row's id, the worker is fenced on `attempts` (§12.5) |
| A charge succeeds after the reservation was expired or cancelled | Expiry and cancel never act while a fee payment is `pending` (M3B-10) |
| Card numbers or QR tokens appear in logs, error bodies, `device_events` or the database | Card data is never stored; validation details never echo values; QR values are redacted in `device_events` (M2 D11); log-leak test (§15.4, M3B-12) |
| A cap change cancels confirmed reservations | The cap is computed at check time only; nothing cancels on a capacity change (architecture §7) |
| The worker or a crash leaves a hold counted forever | Every hold release runs in the same transaction as the status change; an invariant test recounts slots (§15.2) |

---

## 2. Decisions

### 2.1 Made by the user (2026-11-16, binding)

| # | Decision | Reason | Cost if wrong | Section |
|---|---|---|---|---|
| M3B-1 | **Scope in for M3 backend:** reservations (quote, create, list, get, PATCH vehicle, pay, cancel, qr); reservable cap via slot counters; `PaymentGateway` port + simulated adapter (`sim_gateway_charges`); receipts (`GET /v1/payments`, `/{id}`); `refunds` table + worker processing with backoff; reservation QR at the entry barrier and pay station (and exit), including the §7.1 physically-full path; the availability holds and vehicle in-use stubs swapped for real adapters; worker jobs (expire `pending_payment` after 10 min, no-show expiry + no-show block, hold-change notify for SSE, refunds, 30-min reminder emails via outbox, confirmation and cancellation/refund emails); simulator scenarios `reservation_arrivals` and `no_show`; control API presenting a reservation QR | The full driver reservation journey plus the gate guarantee, which is what the M3 demo shows | — | all |
| M3B-2 | **Deferred to M4 (admin):** FR-RES-15 lot-closure cancellation, and the reconciliation job and alert behaviour. M3 lays only schema hooks: the refund reason enum includes `facility_closed` and `admin_override`, and the `reconciliation_alerts` table exists and is written in the §7.1 `lot_physically_full` cancellation transaction. The periodic job and the admin views are M4 | Closure needs the admin lot endpoints, which are M4; building the cancellation path without its trigger leaves dead code | Spec §8 M3 lists the closure exit criterion; it moves to M4 (§19, §20 I8) | §6.2, §12 |
| M3B-3 | **Pay semantics:** `POST /v1/reservations/{id}/pay` calls `Charge` synchronously with a bounded deadline. Success → `200` with the confirmed reservation. Decline → `402 payment.declined`; the reservation stays `pending_payment` and the driver may retry until expiry. Timeout or unknown outcome → `202` with the payment `pending`; the worker re-queries by idempotency key and finalizes; the client polls `GET /v1/reservations/{id}`. Card `…9995` exercises the `202` path. The request carries an `Idempotency-Key` | One round trip for the common case; a well-defined fallback when the gateway is slow | The `202` path needs the reconcile job and client polling (§9.4) | §9 |
| M3B-4 | **Delivery:** one branch `feat/m3-backend`, subagent-driven task by task as in M2. Task S1 is the contract handoff (OpenAPI paths + error codes, `npm run gen:contracts`, `x-mypark-pending`) so Track B can start. Then schema, cap + create, gateway + pay, cancel + refunds, gate + QR, worker jobs, simulator scenarios, docs | Same rhythm as M2; Track B unblocked on day 1 | A post-handoff contract change needs both developers' approval (policies §2.2) | §16 |
| M3B-44 | **Simulated drivers** (decided by the user on 2026-11-16, closing former open question Q2): `cmd/seed` with `SEED_SIMULATOR_DRIVERS=true` creates 20 verified drivers (`sim-driver-01@mypark.test` …) with accepted legal versions and one vehicle each; each password is derived as `HMAC-SHA256(SIMULATOR_FLEET_SECRET, "mypark/driver-password/v1/" + email)` (base64url). The simulator logs them in through `POST /v1/auth/login` (`client_kind: mobile`) and uses **only public HTTP APIs**: the driver API to quote, create, pay and fetch the QR, the device API at the gate; never the database. Production refuses the dev secret | `reservation_arrivals` and `no_show` need confirmed reservations, and the simulator has no DB access (ADR-7). FR-SIM-2 is reworded to "only public APIs (device API + driver API), never the database" (§19, closes §20 I11) | Derived passwords expose the simulated accounts if the fleet secret leaks (R7) |  §13.1 |
| M3B-60 | **Stranded check-in** (decided by the user on 2026-11-16). When the void job voids a reservation stay (QR accepted, no entry pass within 60 s), the **same transaction** moves the reservation `checked_in → confirmed` and re-increments its slot loads (restores its hold) **without** a cap check, and bumps the availability version + NOTIFY. From then on normal rules apply: re-presenting inside the window takes the confirmed-reservation path, reusing the existing voided stay (`voided → entry_pending`, since `stays_reservation_key` allows one stay per reservation); physically full on re-presentation → the architecture §7.1 cancel with a full refund (`facility_unavailable`); never returning → the no-show job expires it (fee forfeited, counts toward the no-show block). A void after the window closed still returns the reservation to `confirmed`, and the no-show job picks it up on its next run. Lock order: `lot_occupancy` → `reservations` → `slot_loads` → `stays` (§8.1, §12.7) | Restoring the hold keeps the guarantee for a driver whose barrier pass never registered. Skipping the cap check follows the cap-drop rule: confirmed reservations are never cancelled for cap, and any overflow falls under spec §5.2 rule 4. The rejected alternative is in §4.5 | A restored hold can briefly push held slots above the cap; the gate's rule 2 and the §7.1 path absorb it | §10.1, §11.1, §12.7 |

### 2.2 Our calls (review these)

| # | Decision | Reason | Cost if wrong | Section |
|---|---|---|---|---|
| M3B-5 | **Charge deadline 5 s** (`PAYMENT_CHARGE_TIMEOUT`, default `5s`, range 1–10 s). The simulated adapter's normal latency is `SIM_GATEWAY_LATENCY` (default `300ms`) | Inside the 15 s server `WriteTimeout`; long enough for a real provider's p99, short enough that a driver is not left staring at a spinner | A slow real provider pushes more payments into the `202` path. One config value |  §9.3 |
| M3B-6 | **Pay request body** `{card_number, exp_month, exp_year, cvc}`. The number must be 13–19 digits and pass Luhn; the expiry must not be before the current month in `America/Santo_Domingo`. Only brand (from the BIN prefix) and last four are kept. Card fields are never stored, logged, echoed in `validation.failed` details, or put in `device_events` | Spec FR-PAY-3 asks for test card numbers; FR-PAY-9 forbids storing raw card data | A real provider uses client-side tokenization; the body then becomes `{payment_token}` (a contract change in Phase 2) | §9.2 |
| M3B-7 | **`Idempotency-Key` on pay:** required header, `^[A-Za-z0-9_-]{8,64}$`, scoped to the reservation. Stored on the payment row (`client_key`) with a **request fingerprint** = SHA-256 of `reservation public_id ‖ brand ‖ last4 ‖ exp_month ‖ exp_year` (no full card number). Same key + same fingerprint → the **current** outcome of that payment (`200`/`402`/`202`) with `Idempotent-Replayed: true`. Same key + different fingerprint → `409 idempotency.key_reused`. A new attempt after a decline needs a **new** key and creates a new payment row. Missing or malformed header → `422 validation.failed` (field `Idempotency-Key`) | True idempotency lets clients retry blindly on network errors (same pattern as the device API, M2C-19); a fingerprint without the PAN avoids storing an enumerable card hash | A client that reuses its key after a decline gets the stored `402` forever; the contract says "new key per attempt" |  §9.3 |
| M3B-8 | **Double-charge guard:** a partial unique index allows at most one `reservation_fee` payment per reservation in `pending`, `succeeded` or `refunded`; the reservation row is locked `FOR UPDATE` while the attempt row is inserted. A second key while one attempt is `pending` → `409 payment.in_progress`; pay on a reservation that is no longer `pending_payment` → `409 reservation.invalid_state`. The **gateway** idempotency key is server-generated: `rsv-fee:<payment public_id>` | Spec §1.5: never charge twice. The client key protects retries; the server key protects the gateway call | None found |  §6.2, §9.3 |
| M3B-9 | **Re-query through `Charge`:** the port keeps the two methods of architecture §9. A `Charge` with an idempotency key the provider already saw returns the recorded result and does **not** need the card; a key it never saw with no card returns `ErrChargeUnknown`. No `Lookup` method is added | Architecture §9 says the worker re-queries by idempotency key because `Charge` is idempotent; the card is never stored, so a re-query cannot resend it | A real provider without retrieve-by-key needs a third method then. Adapter-local change |  §9.1 |
| M3B-10 | **Unknown outcome rules.** (1) The unpaid-expiry job skips a reservation that has a `pending` fee payment. (2) Cancel answers `409 payment.in_progress` while one is pending. (3) The reconcile job re-queries `pending` fee payments older than `PAYMENT_CHARGE_TIMEOUT + 10 s`. (4) `ErrChargeUnknown` (the API died before calling the gateway) → the payment becomes `failed` with `failure_code = 'not_submitted'`. (5) A success that resolves while `$now < upper(hold_window)` confirms; one that resolves later moves the reservation `pending_payment → expired_unpaid` and queues a full refund with reason `facility_unavailable` | No path can expire or cancel a reservation whose money might still be taken; a late charge is always given back | A gateway outage keeps reservations `pending_payment` (holding slots) until it answers. Risk R3 |  §9.4 |
| M3B-11 | **Simulated card outcomes:** `4242 4242 4242 4242` succeeds; `4000 0000 0000 0002` declines (`card_declined`); `4000 0000 0000 9995` is **recorded as succeeded** but its response is delayed past the deadline (`SIM_GATEWAY_TIMEOUT_DELAY`, default `30s`), so the API answers `202` and the reconcile job later confirms it; **any other** Luhn-valid number declines (`card_not_supported`) | The `202` path ends in `confirmed` so the demo shows the whole loop; refusing unknown numbers keeps real cards out of a simulated gateway (policies §1.7 "abusing the simulated gateway") | Track B needs the three numbers on its pay screen hint (contract docs) |  §9.5 |
| M3B-12 | **Card and QR secrecy:** `TestNoSecretsInLogs` adds the three test PANs, a CVC, `QR_HMAC_KEY` and a generated QR token; the same check runs over `payments`, `sim_gateway_charges`, `device_events` and `outbox_messages` rows after the suite | Spec §1.5, FR-PAY-9, NFR-SEC-3 | None |  §15.4 |
| M3B-13 | **Effective-hold changes reach SSE two ways.** (a) A transaction that changes effective holds **at `$now`** (confirm, driver cancel of a confirmed reservation whose window contains `$now`, QR check-in, physically-full cancel) takes `lot_occupancy FOR UPDATE` **first**, bumps `version` and runs `pg_notify('lot_changed', …)`. Whether the window contains `$now` is read before locking (the window is immutable). (b) Time-based changes (a window opening or closing) are pushed by the `hold_boundary_notify` job every 30 s | The stream drops any push whose version is not newer than the last one sent (M2C-38), so a NOTIFY without a version bump never reaches clients. Locking the lot row first keeps one lock order (M3B-14) | Time-based changes show up to 30 s late on clients. Gate decisions always compute holds live, so correctness does not depend on the job | §8.2, §12.3 |
| M3B-14 | **One lock order** extends M2C-29: `lot_occupancy` → `reservations` → `reservation_slot_loads` (ascending `slot_start`) → `stays` → `payments` → `refunds` → `users` → `reconciliation_alerts` / `outbox_messages` / `audit_log` (inserts) → `device_events` (insert, last). `vehicles` rows are only ever locked `FOR SHARE` by reservation flows, before `reservations` | Every M3 transaction is listed in §8.1 against this order; one order removes deadlocks | A new flow that breaks the order can deadlock; the PR checklist names it | §8.1 |
| M3B-15 | **Create inserts the reservation row before it locks the slots** (architecture §7 locks the slots first). The partial unique index rejects a second active reservation before any slot is touched; a cap failure rolls back the insert | Keeps `reservations → slot loads` in every flow (M3B-14) and turns the one-active rule into a cheap early exit | None; the same statements in another order |  §8.3 |
| M3B-16 | **Cap check:** `cap = floor(max_reservable_pct × effective_capacity / 100)`, with `effective_capacity` read **unlocked** in the create transaction (`lots.capacity` − out-of-service spaces). Slots are `[lower, upper)` of the hold window in 5-min steps (7 slots by default). `pending_payment` and `confirmed` reservations hold slots; expiry, cancel, no-show, check-in and the physically-full cancel release them in the same transaction | Architecture §7. A stale capacity read can only be off by a concurrent out-of-service toggle, which is M4 and never cancels anyway | None found |  §8.3 |
| M3B-17 | **Arrival normalization:** the server rounds `arrival_at` **up** to the next 5-minute boundary (UTC epoch; Santo Domingo is UTC−4 with no DST, so the boundaries coincide). After rounding it must be ≥ `ceil5($now)` (`422 reservation.arrival_in_past`) and ≤ `$now + booking_horizon_days` (`422 reservation.beyond_horizon`). The normalized value is returned by quote and create | FR-RES-1 ("rounded up to the next 5 minutes"); aligned arrivals keep every window on slot boundaries | A client that shows the raw time must show the returned one instead |  §7.2 |
| M3B-18 | **Hold window stored half-open:** `hold_window = tstzrange(arrival − early_arrival_min, arrival + arrival_grace_min, '[)')`, from the reservation's captured policy version. The QR is accepted when `lower ≤ $now < upper`; the no-show job acts when `$now ≥ upper` | Matches architecture §7 slot accounting; the closed form of spec §5.2 differs by one instant (§20 I1) | A QR presented at exactly `arrival + 20 min` is late. One instant |  §6.1, §11.1 |
| M3B-19 | **`reservations.payment_due_at`** = `$now + payment_timeout_min` is stored at create. The expiry job and the pay endpoint read it; responses expose it so clients can show a countdown | Indexable expiry query; the captured policy version decides the timeout once | One column |  §6.1 |
| M3B-20 | **FR-RES-13 check** (arrival < 15 min away, so the hold is effective at creation): require `public available ≥ 1` from an **unlocked** availability snapshot at `$now` | The spec rule as written; an exact check would need the lot lock in every create | Two simultaneous "now" bookings can both pass with 1 available. The gate's rule 2 and the physically-full path (§11.1) cover the overflow |  §7.3 |
| M3B-21 | **QR token** per architecture §11.2: `base64url_nopad(public_id[16] ‖ qr_nonce[16] ‖ HMAC-SHA256(QR_HMAC_KEY, public_id ‖ qr_nonce)[32])`, 86 characters. `qr_nonce` is 16 random bytes generated at confirmation. `QR_HMAC_KEY` (base64, ≥ 32 bytes) has an inline compose dev default and is refused in production at that value. **One key**, no key id: rotating it invalidates every outstanding QR, and clients re-fetch `GET /qr` | Opaque, unforgeable, offline-friendly (FR-RES-7). A key id would lengthen the code for a rotation that Phase 1 never does | An offline QR stops working after a rotation until the app is online again. Acceptable for Phase 1 | §11.2 |
| M3B-22 | **QR at the entry barrier: outcome table** (§11.1). New gate reason `reservation.window_closed` for a QR presented at or after `upper(hold_window)` (still `confirmed`, or already `expired_no_show`). Unknown, forged, cancelled, completed, expired-unpaid or `pending_payment` → `gate.credential_invalid`. A second car on a checked-in reservation whose stay is inside → `gate.credential_in_use` | The driver needs to tell "too late" from "invalid"; reusing existing codes elsewhere keeps the catalog small | One new code |  §11.1 |
| M3B-23 | **Physically-full predicate:** `occupied + pending_entries(60 s) ≥ effective_capacity`, under the `lot_occupancy` lock | Spec §5.2 rule 5 counts pending entries for every decision, and architecture §7 states the occupancy invariant with pending entries; the three wordings of rule 2 are reconciled in §20 I2 | None |  §11.1 |
| M3B-24 | **Re-presenting a QR after check-in:** inside the window, if the reservation is `checked_in` and its stay is still `entry_pending` (inside the 60 s), the gate answers `open` with the same `stay_id` and refreshes `accepted_at`. Once the void job has voided the stay, the reservation is `confirmed` again (M3B-60) and a re-presentation takes the confirmed path, reusing the voided stay via `voided → entry_pending` (new stay transition, reservation stays only). `stays_reservation_key` still allows **one** stay per reservation | A barrier glitch must not strand a guaranteed driver outside (spec G2); one stay per reservation keeps billing and completion simple | Adds one stay transition (§10.4) |  §11.1 |
| M3B-25 | **A reservation stay is billed from `stays.entered_at`** (the entry barrier pass), with the reservation's tariff version | Spec §5.1 says "the check-in time"; check-in (credential accepted) and the pass are seconds apart, and M2 bills every stay from the pass. Rounds in the driver's favour (§20 I6) | Up to 60 s of difference, never a different hour except at a boundary |  §11.3 |
| M3B-26 | **The exit pass completes the reservation** (`checked_in → completed`, `completed_at = $now`) in the same transaction that moves the stay to `exited` | One transaction, no worker needed | None |  §11.4 |
| M3B-27 | **`payments.user_id`** is set on reservation fees and on cash/zero `parking_fee` payments of reservation stays, so receipts (FR-PAY-8) show both. Drive-up and card stays keep `user_id` null | A driver's receipts include the hourly fee paid on their reserved stay | One extra lookup in the gate payment insert |  §6.2, §7.10 |
| M3B-28 | **Payments ownership:** `features/payments` owns reservation-fee rows, refunds, receipts and the gateway port. The M2 cash and zero `parking_fee` writes stay in `features/gate` | Moving the M2 writes would churn tested code for no behaviour change | Two writers of one table, each owning a `kind`; documented in §5.1 |  §5.1 |
| M3B-29 | **Refund processing:** claim `pending` rows with `next_attempt_at ≤ $now` (`for update skip locked`, batch 20), set `attempts = attempts + 1`, `next_attempt_at = $now + 60 s` (a lease), commit, call `Refund` outside any transaction, then finalize fenced on `(status = 'pending', attempts)`. Backoff after a failure: `10 s × 2^(attempts−1)`, capped at 30 min. After **10** attempts → `failed` + a `reconciliation_alerts` row `refund_failed` | FR-PAY-5, spec §5.4 refund machine; same lease and fence pattern as the outbox dispatcher (M1) | 10 attempts span about 2.5 h before an alert. Config constants |  §12.5 |
| M3B-30 | **`reconciliation_alerts` kinds in M3:** `reservation_unadmitted` (physically-full cancel) and `refund_failed`. M4 adds its kinds with a constraint swap. `details` carries public ids only | Architecture names the table but no kinds; freezing M4's kinds now would guess at an undesigned feature | M4 runs one `drop/add constraint` |  §6.2 |
| M3B-31 | **Vehicle in-use adapter** counts `pending_payment`, `confirmed` and `checked_in` reservations. Create and PATCH lock the chosen vehicle `FOR SHARE`, so a concurrent delete (which takes `FOR UPDATE`, M1) is serialized | FR-VEH-4 lists `confirmed` and `checked_in`; a vehicle deleted during the 10-min payment window would confirm a reservation on a deleted car (§20 I4) | A driver must cancel an unpaid reservation before deleting its vehicle |  §7.11 |
| M3B-32 | **HTTP 402** for `payment.declined` | The standard status for a refused payment; clients branch on it without parsing codes | Adds 402 to the architecture §4.3 mapping (§19) |  §14 |
| M3B-33 | **Another driver's reservation or payment is `404`** (`reservation.not_found`, `payment.not_found`), the same as a malformed id | No existence oracle (architecture §5 ownership check) | None |  §7 |
| M3B-34 | **Cancel is idempotent for the driver:** cancelling a reservation the driver already cancelled answers `200` with its current state; any other non-cancellable state → `409 reservation.invalid_state` | A client that lost the response can retry safely | None |  §7.8 |
| M3B-35 | **Quote** runs every create check **read-only** (no locks, no writes) and returns the normalized arrival, window, fee and payment timeout, or the same error create would give. `vehicle_id` is optional on quote | The driver sees the fee and any blocker before confirming (FR-RES-1); one validation path | A quote can pass and the create then hit `capacity_reached`. Expected |  §7.2 |
| M3B-36 | **List:** `GET /v1/reservations?scope=active\|past` (default both), keyset on `(arrival_at desc, id desc)`, `limit` 1–50 (default 20). `active` = `pending_payment`, `confirmed`, `checked_in` | Architecture §4.4 keyset rule; the mobile "My reservations" screen shows the two groups | None |  §7.5 |
| M3B-37 | **Receipts** list every payment row with `user_id = me` (all statuses), keyset on `(created_at desc, id desc)`. `receipt_no` (`MP-` + 8 digits, M2C-34) only for `succeeded` and `refunded`; a refund summary is embedded | FR-PAY-8; failed attempts are useful history for "why was I not charged" | None |  §7.10 |
| M3B-38 | **Rate limits** (Shared scope): `reservation_write_user` 30/min per user on create, PATCH and cancel (fail open, like vehicles); `reservation_pay_user` 10 per 10 min per user on pay (**fail closed**); `reservation_read_user` is not added (reads are cheap and owner-scoped) | Policies §1.7 forbid reservation bots and gateway abuse | A legitimate driver who mistypes a card 10 times waits 10 min |  §7 |
| M3B-39 | **`hold_boundary_notify` job** (30 s): per lot, compute `effective_holds($now)` and compare with the last value it notified (in memory); on a difference, or on its first tick after start, bump `lot_occupancy.version` and NOTIFY | Architecture §6 and §10 (30 s scan). Comparing counts skips ticks where one hold ends as another starts, which changes nothing visible | Several worker replicas may bump twice; harmless (a newer snapshot) |  §12.3 |
| M3B-40 | **No-show block** runs inside the no-show expiry transaction, with the values of the expired reservation's **captured** policy version: count the user's `expired_no_show` rows with `expired_at > $now − noshow_window_days`; at ≥ threshold set `users.reservation_blocked_until = greatest(existing, $now + noshow_block_days)` | FR-RES-17, FR-RES-18; architecture §10 "on each `expired_no_show`" | A policy change between booking and expiry is ignored for that block. Rare and conservative |  §12.2 |
| M3B-41 | **Reminder job** (1 min): enqueue `reservation_reminder` for `confirmed` reservations with `arrival_at ∈ ($now, $now + 30 min]` and `confirmed_at ≤ arrival_at − 30 min`, deduplicated by `reservation_reminder:<public_id>` | FR-NOTIF-1; a reservation booked less than 30 min ahead already has its confirmation email | None |  §12.4 |
| M3B-42 | **Emails:** new outbox templates `reservation_confirmed` (with QR), `reservation_reminder`, `reservation_cancelled` (states the refund: pending, none, or not applicable), `reservation_no_show`, `refund_succeeded`. The confirmation payload carries `qr_token`, which `MarkOutboxSent` and `MarkOutboxDead` scrub like `code` | FR-NOTIF-1 and policies §1.2 ("the driver … receives an email" on refund). A QR token is long-lived, so the M2C-51 rationale (codes expire in 15 min) does not cover it; scrubbing does | No email on refund failure (the admin alert covers it) |  §12.6 |
| M3B-43 | **QR image in the confirmation email** is a PNG generated by `rsc.io/qr` (new Go module, pure Go, no transitive dependencies), attached inline | FR-NOTIF-1 "confirmed (with QR)"; the standard library has no QR encoder. Closed as our call on 2026-11-16 (former Q1) | One new dependency to vet (govulncheck, security checklist) |  §12.6 |
| M3B-45 | **Scenario timing in live compose:** reserved cars book `arrival_at = ceil5(server now)`, so their window is already open when they reach the gate (the server clock is real time, M2C-13). `no_show` cars never arrive; in live compose they expire after up to 25 real minutes. CI runs both on the shared fake clock (M2C-14) | Windows are server time; a virtual clock at 30× cannot move them | The live no-show demo needs a 25-min wait. Risk R1 |  §13.2 |
| M3B-46 | **Control API:** `POST /control/cars` accepts `credential: "reservation_qr"` with a required `qr` field (the token string, 86 chars); the car presents it at the entry barrier, the pay station and the exit. The event log and the simulator logs show `sha256:<12 hex>` instead of the token | FR-SIM-5 "present a specific reservation QR"; the demo copies the token from the driver's web reservation page | A token pasted into the console is visible to the admin operating it. Demo only |  §13.3 |
| M3B-47 | **`x-mypark-pending` markers use slice ids `S1`–`S9`** (the existing pattern `^(S\|C)[0-9.]+$`). Task S1 adds a temporary allowlist of M3 slice ids to `TestOpenAPI_NoPendingOperations` so the branch stays green; task S9 removes the allowlist, re-arming the strict check | The test fails on any pending marker today; without the allowlist S1 turns the suite red | None |  §16 |
| M3B-48 | **`max_active_per_user` and `slot_min` are fixed in Phase 1.** The DB index enforces one active reservation per user, and slot rows are 5-minute aligned. The reservation service refuses to run with a policy version whose `max_active_per_user ≠ 1`, `slot_min ≠ 5`, or whose `early_arrival_min` / `arrival_grace_min` are not multiples of 5 (a `500` with a logged cause; M4's admin form must validate the same) | Both columns exist on `policy_versions` but architecture's index and slot table hard-code the values (§20 I9) | M4 must keep the form validation or migrate the index and slots |  §6.1 |
| M3B-49 | **A zero reservation fee is not supported in M3.** The service refuses a tariff version with `reservation_fee_cents = 0` at create (`500`, logged). The schema keeps `>= 0` | A zero fee would need a gateway-less confirm path that nothing in Phase 1 uses | M4 adds the path or validates `> 0` in the admin form |  §7.3 |
| M3B-50 | **Pay `202` body** = `{reservation, payment: {id, status: "pending"}}` with `Retry-After: 2`. Clients poll `GET /v1/reservations/{id}` every 2 s for up to 60 s, then show "still processing" and keep the reservation screen | One polling target; the reservation already carries `latest_payment` | None |  §7.6 |
| M3B-51 | **Worker pool size** rises from 4 to 8 connections (constant in `cmd/worker`) | Six new jobs, two of which hold a connection during a gateway call's surrounding transactions | None |  §5.2 |
| M3B-52 | **Coverage:** ≥ 90 % statement coverage (the OC-35 proxy for NFR-TEST-1 branch coverage) on the reservations domain (state machine, cap rule, arrival and window rules, QR codec) and the payments domain (refund backoff, fingerprint, sim outcome table) | NFR-TEST-1 names the reservation cap and state machines | None |  §15.6 |
| M3B-53 | **Fee payment rows write `created_at = @now`** explicitly (the column keeps its `default now()` for the M2 cash and zero rows); `payments_pending_fee_idx` and the reconcile age filter read it | The reconcile filter must run on the fake clock, and business time never comes from SQL `now()` (M2 D3) | None |  §6.2, §9.4 |
| M3B-54 | **`PAYMENT_GATEWAY`** config, only value `simulated` (default in compose); `APP_ENV=production` requires it to be set explicitly | Phase 1 has one adapter; the flag makes running the simulated gateway in production a visible choice, not a default | None |  §9.5 |
| M3B-55 | **Driver-API transactions** (create, PATCH, pay tx 1 and tx 2, cancel) set `lock_timeout = '1s'` and `statement_timeout = '2s'`, as device transactions do (M2 §8.1). A timeout answers `500 internal.unexpected` and changes nothing | They contend on the same rows as the gate (`lot_occupancy`, slots); a stuck lock must fail fast | A burst of 200 creates may see a few 500s under heavy contention; the concurrency test (§15.2) shows whether 1 s is enough |  §8.1 |
| M3B-56 | **Pay tx 2 runs on a context detached from the request** (`context.WithoutCancel` plus its own 3 s deadline) | A client that disconnects after the gateway answered must not leave the result unwritten; the reconcile job remains the backstop | None |  §9.3 |
| M3B-57 | **Small API behaviours:** PATCH with the vehicle already on the reservation answers `200` without a write; quote reports `reservation.capacity_reached` from an **unlocked** slot read (advisory; create re-checks under lock) | Idempotent PATCH for retries; the driver learns a window is full before paying | A quote can say "available" and the create fail, or the reverse. Expected |  §7.2, §7.7 |
| M3B-58 | **Job batch sizes and scenario rates:** reconcile and refund batches 20 (each row makes a gateway call), expiry and no-show batches 200 (pure SQL, like the M2 void job); `reservation_arrivals` 8 reserved cars per virtual hour, `no_show` 4 per virtual hour | Gateway-bound batches stay short so a lease never expires mid-batch; scenario rates stay well under the cap and the simulated driver pools | Tunable constants (§18) |  §12, §13.2 |
| M3B-59 | **Simulated drivers are split by scenario:** drivers 01–16 serve `reservation_arrivals` and the control API; drivers 17–20 serve only `no_show` | Three no-shows block a driver for 7 days (M3B-40); a shared pool would let one `no_show` run block every driver and stop `reservation_arrivals` | `no_show` runs out of drivers after about 12 no-shows in 30 days; `compose down -v` resets (R5) |  §13.1 |

**Count:** 60 decisions (6 binding: M3B-1 to M3B-4, M3B-44, M3B-60; 54 our calls).

---

## 3. Constraints

| Area | Constraint (source) |
|---|---|
| Inherited | Everything in M1 §3 and M2 §3: Go 1.27, stdlib `ServeMux`, `pgx/v5` + sqlc + goose forward-only, Postgres 18, baseline envelope, `internal/config` accessors with validator, no `.env` files, `mypark_migrator` / `mypark_app` roles |
| Time | `clock.Clock` is injected everywhere. Business timestamps are written as the `@now` query parameter, **never** SQL `now()`; `now()` stays only in `created_at` defaults (M2 D3). Jobs take `now` so tests drive them with `FakeClock` |
| Data | Integer centavos (`bigint *_cents`, DOP implied); `text` + `check` enums; identity PKs and `public_id uuid default uuidv7()`; every FK indexed; partial indexes for hot subsets (architecture §11) |
| Retention | Financial records are kept 5 years (policies §1.8): `delete` and `truncate` are revoked from `mypark_app` on `reservations`, `payments` (M2), `refunds`, `reconciliation_alerts` and `sim_gateway_charges` |
| Transactions | No network I/O inside a transaction. `Charge` and `Refund` run outside; locks are held for milliseconds (architecture §7 step 5, §11.3) |
| Contract | Contract first (spec §8): OpenAPI paths and error codes merged before implementation; `npm run gen:contracts`; every new code gets `errors.<code>` in es-DO and en in web and mobile (`check:i18n` parity) |
| Security | Card data never stored or logged (FR-PAY-9); QR tokens are bearer credentials; secrets come from env with refused dev values in production (policies §2.5) |
| Dependencies | Stdlib first. One new Go module, `rsc.io/qr` (M3B-43), with the security checklist |
| Process | Work-unit commits of ≤ 400 changed lines each (generated sqlc output and `package-lock.json` excluded, `openapi.yaml` included), chained on one branch (M3B-4, policies §2.2); migrations, payments and QR signing are sensitive paths with an automated risk review; TDD for the cap, the state machines, the QR codec and the refund backoff; integration tests on real Postgres as `mypark_app`; no AI attribution |

---

## 4. Approaches considered

### 4.1 Pay response: synchronous with a `202` fallback (chosen) vs always `202`

| | **A. Synchronous charge, `202` on timeout (chosen, M3B-3)** | B. Always `202`, worker charges |
|---|---|---|
| Common case | One request: the driver sees "confirmed" at once | Every payment needs polling, even a 300 ms success |
| Decline UX | `402` in the same request; the form stays filled for a retry | The client polls until `failed`, then reopens the form |
| Moving parts | API calls the gateway; a reconcile job covers the unknown case | A charge job, a queue state, polling everywhere |
| Failure surface | The API process holds a request up to 5 s | No long request, but card data must reach the worker, i.e. be stored somewhere (FR-PAY-9 conflict) |
| Cost | The `202` branch must be built and tested anyway | Simpler API, worse UX, and a card-data storage problem |

B would need the card details to survive until the worker runs, which FR-PAY-9 forbids for raw data. A keeps the card in memory for one call.

### 4.2 Delivery: one branch (chosen) vs two stacked branches

| | **One branch `feat/m3-backend` (chosen, M3B-4)** | Two stacked branches (reservations, then payments + gate) |
|---|---|---|
| Review | Work-unit commits ≤ 400 lines, task-scoped reviews, one final whole-branch review, as in M2 | Two final reviews; the second rebases on fixes from the first |
| Integration risk | Pay, cancel and gate share the lock order and the hold notifications; one branch tests them together | The first branch ships reservations that cannot be paid, which no client can use |
| Track B | Contract in S1 either way | Same |

### 4.3 Scope split: M3 builds the driver journey, M4 builds admin-triggered flows (chosen)

| | **M3 = driver + gate + worker; M4 = closure, reconciliation, admin views (chosen, M3B-1/2)** | Everything in spec §8 M3 now |
|---|---|---|
| FR-RES-15 | Schema hooks only (`facility_closed`, alerts table) | Needs `POST /v1/admin/lot/close`, which is an admin endpoint with no admin area yet |
| Reconciliation | One alert row per incident, no job | A periodic job and alert lifecycle with nothing to show them |
| Exit criteria | The closure criterion moves to M4 (§19) | All criteria in one week, with admin plumbing built twice |

### 4.4 Effective-hold notification (M3B-13)

| | **Lock `lot_occupancy` first in the few transactions that change holds now, plus a 30 s boundary job (chosen)** | Only the boundary job bumps the version | Bump in any order |
|---|---|---|---|
| Latency for confirm/cancel/check-in | < 2 s (same path as a gate pass) | Up to 30 s | < 2 s |
| Deadlock risk | None: one order (M3B-14) | None | `reservations → lot_occupancy` vs QR entry's `lot_occupancy → reservations` deadlocks |
| Cost | A per-lot serialization point for a few writes per minute | Nothing | — |

### 4.5 Stranded check-in (M3B-60, user decision)

| | **Void restores the hold: `checked_in → confirmed` in the void transaction (chosen)** | Refund at window close |
|---|---|---|
| Driver whose pass never registered | Re-presents the QR and enters under the normal confirmed rules | Gets the fee back but has no hold for the rest of the window |
| Moving parts | None new: the existing void job and the existing confirmed path | A new job that finds checked-in reservations without an entered stay at window close |
| Abuse | A driver who accepts the QR and drives off is a no-show (fee forfeited, counts toward the block) | A driver can accept the QR, drive off and collect a refund |
| Genuine barrier failure | M4 admin refund override (`admin_override`) | Automatic |

Rejected: refund at window close adds a job and rewards driving off after a check-in; an M4 admin refund override covers genuine barrier failures.

---

## 5. Architecture

### 5.1 Package map

New packages are **bold**. Paths are under `apps/server/internal/`.

| Package | Role | Owns tables |
|---|---|---|
| **`features/reservations`** | Domain: reservation state machine, arrival normalization, hold window, cap rule, QR codec (pure, TDD). Service: quote, create, list, get, change vehicle, pay orchestration, cancel, QR; the job bodies for unpaid expiry, no-show expiry + block, hold boundary, reminders. Adapters: `Holds` (implements `availability.ReservationHolds`), `VehicleChecker` (implements `vehicles.ReservationChecker`), `GateAccess` (implements `gate.Reservations`). HTTP handlers | `reservations`, `reservation_slot_loads`, `users.reservation_blocked_until` (writes) |
| **`features/payments`** | Port `Gateway` (`Charge`, `Refund`). Service: fee attempts (start, finalize), refund queueing and processing, receipts. HTTP: `GET /v1/payments`, `/{id}`. Job bodies: payment reconcile, refund processing | `payments` (`reservation_fee` rows), `refunds` |
| **`infrastructure/paymentsim`** | The simulated adapter: test-card outcome table, latency and timeout delay, persistence | `sim_gateway_charges` |
| **`features/alerts`** | `Recorder.Open(ctx, lotID, kind, details)` inside the caller's transaction. No reads in M3 | `reconciliation_alerts` |
| `features/gate` | Reservation QR branch at entry, pay station and exit through the `gate.Reservations` port; completion on the exit pass; `voided → entry_pending` for reservation stays; `user_id` on reservation-stay payments | `stays`, `employee_cards`, `payments` (`parking_fee` rows, M3B-28) |
| `features/availability` | Unchanged code; `cmd/api` wires `reservations.Holds` instead of `NoReservations` | — |
| `features/vehicles` | Unchanged code; wiring swaps `NoReservations` for `reservations.VehicleChecker` | — |
| `features/notifications` | Five templates (M3B-42), `qr_token` scrub, inline QR image | `outbox_messages` |
| `simulator` | Driver client (login, quote, create, pay, QR), `reservation_arrivals`, `no_show`, QR cars, control API change | — |
| `domain/errcodes` | M3 codes (§14) | — |

Feature interfaces (features call each other through exported use-case interfaces, architecture §4.1):

| Caller | Callee interface | Use |
|---|---|---|
| `reservations` | `payments.Fees` | `StartAttempt(ctx, AttemptInput) (Attempt, Replay, error)`, `Finalize(ctx, paymentID, ChargeResult) error`, `QueueRefund(ctx, paymentID, reason) error`, `LiveFeePayment(ctx, reservationID)` — all inside the caller's transaction (context-carried tx) |
| `reservations` | `payments.Gateway` | `Charge` outside any transaction |
| `payments` (reconcile job) | `reservations.ChargeOutcome` | `Apply(ctx, paymentID, ChargeResult)`: the same confirm / late-success logic the pay endpoint uses (§9.4) |
| `payments` (refund job) | `reservations.RefundNotice` | `RefundSucceeded(ctx, refund)`: enqueue `refund_succeeded` email |
| `gate` | `gate.Reservations` (port defined in `gate`, implemented by `reservations.GateAccess`) | `Resolve(ctx, lotID, token) (Ref, error)` (signature check, no lock); `CheckIn`, `Reopen`, `CancelUnadmitted`, `StayForQR`, `Complete`, all inside the device transaction |
| `gate`, `availability` | `availability.ReservationHolds` | `EffectiveHolds`, `UpcomingArrivals` — implemented by `reservations.Holds`, which reads through the context transaction when one is present (the gate calls it under the lot lock) |
| `reservations`, `payments` | `alerts.Recorder` | `reservation_unadmitted`, `refund_failed` |
| `reservations`, `payments` | `notifications.Enqueuer` | outbox emails (M1 port) |

### 5.2 Composition

| Binary | Adds in M3 |
|---|---|
| `cmd/api` | Config `QR_HMAC_KEY`, `PAYMENT_CHARGE_TIMEOUT`, `SIM_GATEWAY_LATENCY`, `SIM_GATEWAY_TIMEOUT_DELAY`. `paymentsim.New(pool, clock, cfg)`, payments service, reservations service; `reservations.Holds` into availability and gate; `reservations.VehicleChecker` into vehicles; `reservations.GateAccess` into gate. Routes §7 |
| `cmd/worker` | Same config and adapters. Jobs §12. Pool 8 (M3B-51) |
| `cmd/seed` | `SEED_SIMULATOR_DRIVERS=true`: 20 drivers + vehicles (M3B-44), in the seed transaction, idempotent on email |
| `cmd/simulator` | Driver client, two scenarios, QR cars (§13) |
| Compose | Dev defaults for `QR_HMAC_KEY` (api, worker), `SEED_SIMULATOR_DRIVERS=true` (seed), simulator unchanged secrets |

---

## 6. Data model

Migrations run as `mypark_migrator`, forward-only, with the architecture §11 conventions. Two files, both in task S2: `00010_reservations.sql` and `00011_payments_refunds.sql`.

### 6.1 `00010_reservations`

```sql
alter table users add column reservation_blocked_until timestamptz;   -- FR-RES-17, architecture §11.1

create table reservations (
    id                bigint generated always as identity primary key,
    public_id         uuid not null unique default uuidv7(),
    user_id           bigint not null references users (id),
    vehicle_id        bigint not null references vehicles (id),
    lot_id            bigint not null references lots (id),
    arrival_at        timestamptz not null,                     -- 5-min aligned by the service (M3B-17)
    hold_window       tstzrange not null,                       -- '[)' (M3B-18)
    status            text not null check (status in ('pending_payment','confirmed','checked_in','completed',
                                                      'cancelled','expired_unpaid','expired_no_show')),
    fee_cents         bigint not null check (fee_cents >= 0),
    tariff_version_id bigint not null references tariff_versions (id),   -- captured at booking (FR-RES-18)
    policy_version_id bigint not null references policy_versions (id),
    qr_nonce          bytea check (octet_length(qr_nonce) = 16),        -- set at confirmation (M3B-21)
    payment_due_at    timestamptz not null,                     -- M3B-19
    created_at        timestamptz not null default now(),
    updated_at        timestamptz not null,
    confirmed_at      timestamptz,
    cancelled_at      timestamptz,
    cancelled_by      text check (cancelled_by in ('driver','admin','system')),
    refund_reason     text check (refund_reason in ('cancelled_before_arrival','none_after_arrival',
                                                    'facility_unavailable','facility_closed','admin_override')),
    checked_in_at     timestamptz,
    completed_at      timestamptz,
    expired_at        timestamptz,
    constraint reservations_window_check check (
        lower_inc(hold_window) and not upper_inc(hold_window)
        and lower(hold_window) < arrival_at and arrival_at < upper(hold_window)),
    constraint reservations_confirmed_check check (
        status not in ('confirmed','checked_in','completed') or (confirmed_at is not null and qr_nonce is not null)),
    constraint reservations_cancelled_check check (
        (status = 'cancelled') = (cancelled_at is not null and cancelled_by is not null)),
    constraint reservations_checked_in_check check (status not in ('checked_in','completed') or checked_in_at is not null),
    constraint reservations_completed_check check (status <> 'completed' or completed_at is not null),
    constraint reservations_expired_check check (
        (status in ('expired_unpaid','expired_no_show')) = (expired_at is not null)),
    -- A refund reason exists only on a cancellation or a late-success expiry (M3B-10).
    constraint reservations_refund_reason_status_check check (
        refund_reason is null or status in ('cancelled','expired_unpaid'))
);

-- FR-RES-3: one active reservation per driver (M3B-48).
create unique index reservations_user_active_key on reservations (user_id)
    where status in ('pending_payment','confirmed');
-- "My reservations" keyset (M3B-36); also the user_id FK index.
create index reservations_user_arrival_idx on reservations (user_id, arrival_at desc, id desc);
-- Architecture: lookups by lot and status; also the lot_id FK index. Reminders, upcoming arrivals.
create index reservations_lot_status_arrival_idx on reservations (lot_id, status, arrival_at);
-- Effective holds at a point in time (needs btree_gist, enabled in 00001).
create index reservations_confirmed_window_idx on reservations using gist (lot_id, hold_window)
    where status = 'confirmed';
-- Unpaid expiry job.
create index reservations_payment_due_idx on reservations (payment_due_at) where status = 'pending_payment';
-- No-show job.
create index reservations_window_end_idx on reservations ((upper(hold_window))) where status = 'confirmed';
-- No-show block count.
create index reservations_no_show_idx on reservations (user_id, expired_at) where status = 'expired_no_show';
-- FK indexes.
create index reservations_vehicle_id_idx on reservations (vehicle_id);
create index reservations_tariff_version_id_idx on reservations (tariff_version_id);
create index reservations_policy_version_id_idx on reservations (policy_version_id);

create table reservation_slot_loads (
    lot_id     bigint not null references lots (id),
    slot_start timestamptz not null,
    held       int not null default 0 check (held >= 0),
    primary key (lot_id, slot_start),                       -- also the lot_id FK index
    constraint reservation_slot_loads_aligned_check check (extract(epoch from slot_start)::bigint % 300 = 0)
);

alter table stays add column reservation_id bigint references reservations (id);
alter table stays add constraint stays_reservation_id_check
    check ((credential_kind = 'reservation') = (reservation_id is not null));
-- One stay per reservation, ever (M3B-24; architecture §7 "one open stay per reservation", §20 I3).
create unique index stays_reservation_key on stays (reservation_id) where reservation_id is not null;

revoke delete, truncate on reservations from mypark_app;
```

`users.reservation_blocked_until` is written only by the no-show job (M3B-40).

### 6.2 `00011_payments_refunds`

**`payments`** (M2C-41 announced this change):

```sql
alter table payments alter column stay_id drop not null;
alter table payments
    add column reservation_id      bigint references reservations (id),
    add column user_id             bigint references users (id),                    -- M3B-27
    add column client_key          text check (client_key ~ '^[A-Za-z0-9_-]{8,64}$'), -- Idempotency-Key (M3B-7)
    add column request_fingerprint bytea check (octet_length(request_fingerprint) = 32),
    add column provider_ref        text check (length(provider_ref) between 1 and 100),
    add column card_brand          text check (card_brand in ('visa','mastercard','amex','other')),
    add column card_last4          text check (card_last4 ~ '^[0-9]{4}$'),
    add column failure_code        text check (failure_code in ('card_declined','card_not_supported','not_submitted')),
    add column failed_at           timestamptz,
    add column refunded_at         timestamptz,
    add constraint payments_target_check      check (num_nonnulls(reservation_id, stay_id) = 1),
    add constraint payments_kind_target_check check ((kind = 'reservation_fee') = (reservation_id is not null)),
    add constraint payments_gateway_check     check ((method = 'gateway_sim') = (kind = 'reservation_fee')),
    add constraint payments_fee_fields_check  check (kind <> 'reservation_fee' or (user_id is not null
        and client_key is not null and request_fingerprint is not null
        and card_brand is not null and card_last4 is not null)),
    add constraint payments_failed_check      check ((status = 'failed') = (failed_at is not null and failure_code is not null)),
    add constraint payments_refunded_check    check ((status = 'refunded') = (refunded_at is not null)),
    add constraint payments_provider_check    check (status not in ('succeeded','refunded') or kind <> 'reservation_fee'
        or provider_ref is not null);

-- No double charge (M3B-8).
create unique index payments_reservation_live_key on payments (reservation_id)
    where kind = 'reservation_fee' and status in ('pending','succeeded','refunded');
-- Idempotency-Key scope (M3B-7); also the reservation_id FK index.
create unique index payments_reservation_client_key on payments (reservation_id, client_key)
    where reservation_id is not null;
-- Receipts keyset (M3B-37); also the user_id FK index.
create index payments_user_created_idx on payments (user_id, created_at desc, id desc) where user_id is not null;
-- Reconcile job (M3B-10).
create index payments_pending_fee_idx on payments (created_at) where kind = 'reservation_fee' and status = 'pending';
```

Existing M2 rows satisfy every new check (`parking_fee`, `stay_id` set, `cash`/`none`, `succeeded`).

**`refunds`** (architecture §9, §11.1):

| Column | Notes |
|---|---|
| `id` bigint identity PK, `public_id` uuid unique | |
| `payment_id` bigint not null → payments | idx `(payment_id)` (FK) |
| `amount_cents` bigint not null | `check (amount_cents > 0)`. Always the full fee in M3 |
| `reason` text not null | `check (reason in ('cancelled_before_arrival','facility_unavailable','facility_closed','admin_override'))` |
| `status` text not null default `'pending'` | `check (status in ('pending','succeeded','failed'))` |
| `idempotency_key` text not null unique | `refund:<public_id>` — the gateway refund key |
| `attempts` smallint not null default 0 | `check (attempts >= 0)` |
| `next_attempt_at` timestamptz not null | `$now` at insert; lease and backoff (M3B-29) |
| `last_error` text null | `check (length(last_error) <= 500)`; never holds card data |
| `provider_ref` text null | |
| `created_at` default now(), `succeeded_at`, `failed_at` | `check ((status = 'succeeded') = (succeeded_at is not null))`, `check ((status = 'failed') = (failed_at is not null))`, `check (status <> 'succeeded' or provider_ref is not null)` |

```sql
create unique index refunds_payment_live_key on refunds (payment_id) where status in ('pending','succeeded');  -- no double refund
create index refunds_due_idx on refunds (next_attempt_at) where status = 'pending';
create index refunds_payment_id_idx on refunds (payment_id);
revoke delete, truncate on refunds from mypark_app;
```

**`sim_gateway_charges`** (architecture §9; owned by `infrastructure/paymentsim`):

| Column | Notes |
|---|---|
| `id` bigint identity PK | |
| `idempotency_key` text not null unique | The gateway charge key `rsv-fee:<payment public_id>` |
| `amount_cents` bigint not null `> 0`, `currency` text not null default `'DOP'` `check (currency = 'DOP')` | |
| `outcome` text not null | `check (outcome in ('succeeded','declined'))` |
| `decline_code` text null | `check (decline_code in ('card_declined','card_not_supported'))`, `check ((outcome = 'declined') = (decline_code is not null))` |
| `card_brand`, `card_last4` text not null | Same checks as `payments` |
| `provider_ref` text not null unique | `sim_ch_` + 24 random base32 characters |
| `delayed` boolean not null default false | The timeout card (M3B-11) |
| `created_at` default now() | |
| `refund_idempotency_key` text null unique, `refund_provider_ref` text null unique, `refunded_at` timestamptz null | Full refunds only, at most one per charge. `check ((refund_idempotency_key is null) = (refunded_at is null))` |

`revoke delete, truncate on sim_gateway_charges from mypark_app;`

**`reconciliation_alerts`** (architecture §11.1, minimal for M3, M3B-30):

| Column | Notes |
|---|---|
| `id` bigint identity PK, `public_id` uuid unique | |
| `lot_id` bigint not null → lots | idx `(lot_id)` (FK) and idx `(lot_id) where resolved_at is null` (architecture) |
| `kind` text not null | `check (kind in ('reservation_unadmitted','refund_failed'))`; M4 swaps the constraint |
| `details` jsonb not null | Public ids only: `{reservation_id, refund_id?, payment_id?}` |
| `opened_at` timestamptz not null | `$now` |
| `resolved_at` timestamptz null, `resolved_by` bigint null → users, `resolution_note` text null | M4 writes them. idx `(resolved_by) where resolved_by is not null` (FK) |

`revoke delete, truncate on reconciliation_alerts from mypark_app;`

**`outbox_messages`** — swap the template check (the builder confirms the generated constraint name):

```sql
alter table outbox_messages drop constraint outbox_messages_template_check;
alter table outbox_messages add constraint outbox_messages_template_check check (template in
    ('verify_email','password_reset','password_changed','account_exists','admin_invite',
     'reservation_confirmed','reservation_reminder','reservation_cancelled','reservation_no_show','refund_succeeded'));
```

### 6.3 Grants test

`TestGrants_M3Tables` (as `mypark_app`): select/insert/update on `reservations`, `reservation_slot_loads`, `refunds`, `sim_gateway_charges`, `reconciliation_alerts`; delete on `reservation_slot_loads` allowed (unused); **denied** delete and truncate on `reservations`, `payments`, `refunds`, `sim_gateway_charges`, `reconciliation_alerts`. A schema test inserts a second `pending` fee payment for one reservation and expects `23505` (M3B-8), and a second live refund for one payment and expects `23505`.

---

## 7. Endpoints

All routes are `Roles: driver` (bearer), pass the legal gate (not `LegalExempt`) and use the baseline envelope. Ids in paths and bodies are public UUIDs; a malformed id is the same `404` as an unknown one. Request schemas have `additionalProperties: false` (M2C-52). Writes run in one short transaction unless stated.

### 7.1 Reservation representation

```json
{ "id": "…", "lot_id": "…",
  "vehicle": { "id": "…", "plate_display": "A123456", "make": "Toyota", "model": "Corolla", "color": "Gris" },
  "arrival_at": "2026-12-01T15:00:00Z",
  "hold_window": { "starts_at": "2026-12-01T14:45:00Z", "ends_at": "2026-12-01T15:20:00Z" },
  "status": "confirmed", "fee_cents": 15000, "payment_due_at": "…",
  "created_at": "…", "confirmed_at": "…", "cancelled_at": null, "cancelled_by": null, "refund_reason": null,
  "checked_in_at": null, "completed_at": null, "expired_at": null,
  "latest_payment": { "id": "…", "status": "succeeded", "failure_code": null },
  "refund": null,
  "stay": null }
```

- `ends_at` is exclusive (M3B-18).
- `refund`: `{id, status, amount_cents, reason, succeeded_at}` or `null`.
- `stay` (checked-in and completed reservations): `{entered_at, exited_at, parking_paid_cents}` or `null` (FR-RES-12 "stay summary").
- `latest_payment` is the newest fee attempt, so a client sees `pending` (the 202 path) or `failed` with its `failure_code`.

### 7.2 `POST /v1/reservations/quote` (M3B-35)

Body `{lot_id, arrival_at, vehicle_id?}`. Runs §7.3 checks 1–8 read-only. **200** `{lot_id, arrival_at (normalized), hold_window, fee_cents, payment_timeout_min}`. Errors: the same codes as create; `reservation.capacity_reached` comes from an unlocked slot read (M3B-57).

### 7.3 `POST /v1/reservations`

Body `{lot_id, vehicle_id, arrival_at}`. Rate limit `reservation_write_user`. **201** reservation.

Checks, in order (errors in §14):

| # | Check | Error |
|---|---|---|
| 1 | Lot exists | `404 lot.not_found` |
| 2 | Vehicle owned and not deleted (locked `FOR SHARE` in the transaction, M3B-31) | `404 vehicle.not_found` |
| 3 | `users.reservation_blocked_until > $now` | `409 reservation.blocked_no_shows`, `details.blocked_until` |
| 4 | `lots.status = 'closed'` (FR-RES-14) | `409 reservation.lot_closed` |
| 5 | Normalize arrival (M3B-17) | `422 reservation.arrival_in_past` / `422 reservation.beyond_horizon` |
| 6 | Load tariff and policy versions at `$now`; validate the Phase 1 limits (M3B-48, M3B-49) | `500 internal.unexpected` (logged) |
| 7 | Active reservation exists (fast pre-check; the index is the guarantee) | `409 reservation.active_exists` |
| 8 | `lower(hold_window) ≤ $now` (FR-RES-13) and public available < 1 (M3B-20) | `409 reservation.no_space_now` |
| 9 | Insert reservation (`pending_payment`, `payment_due_at`, captured versions, `fee_cents = reservation_fee_cents`); a `23505` on `reservations_user_active_key` | `409 reservation.active_exists` |
| 10 | Slot loads: ensure rows, lock ascending, any `held ≥ cap` → roll back (§8.3) | `409 reservation.capacity_reached` |
| 11 | Increment the 7 slots; commit | — |

No NOTIFY: a `pending_payment` hold does not change public availability.

### 7.4 `GET /v1/reservations/{id}`

**200** reservation (§7.1). `404 reservation.not_found` for another driver's id (M3B-33).

### 7.5 `GET /v1/reservations`

`?scope=active|past&limit=1..50&cursor=` (M3B-36). **200** list with `meta.page`. `422 validation.failed` for a bad scope or cursor.

### 7.6 `POST /v1/reservations/{id}/pay` (M3B-3, M3B-5 to M3B-11)

Header `Idempotency-Key`. Body `{card_number, exp_month, exp_year, cvc}`. Rate limit `reservation_pay_user` (fail closed). Flow in §9.3.

| Outcome | Status | Body |
|---|---|---|
| Charge succeeded (or replay of a succeeded attempt) | `200` | reservation (`confirmed`) |
| Gateway timeout / unknown (or replay of a pending attempt) | `202`, `Retry-After: 2` | `{reservation, payment: {id, status: "pending"}}` (M3B-50) |
| Declined (or replay of a failed attempt) | `402 payment.declined` | `details: {payment_id, failure_code}` |
| Another attempt pending | `409 payment.in_progress` | — |
| Reservation not `pending_payment` (already paid, cancelled, expired) | `409 reservation.invalid_state` | `details.status` |
| `$now ≥ payment_due_at` | `409 reservation.payment_expired` | — |
| Same key, different card | `409 idempotency.key_reused` | — |
| Missing/malformed key, bad card fields | `422 validation.failed` | field names and rules only, never values |

A replayed answer carries `Idempotent-Replayed: true`.

### 7.7 `PATCH /v1/reservations/{id}` (FR-RES-16)

Body `{vehicle_id}`. Rate limit `reservation_write_user`. Lock the reservation, then the new vehicle `FOR SHARE`. Rules: status `confirmed`, else `409 reservation.invalid_state`; `$now < arrival_at`, else `409 reservation.vehicle_change_closed`; vehicle owned and not deleted, else `404 vehicle.not_found`. Same vehicle → `200` without a write (M3B-57). **200** reservation. Fee, window and QR are unchanged.

### 7.8 `POST /v1/reservations/{id}/cancel` (FR-RES-11, M3B-34)

No body. Rate limit `reservation_write_user`.

| Current status | Condition | Effect | Response |
|---|---|---|---|
| `pending_payment` | a fee payment is `pending` | none | `409 payment.in_progress` |
| `pending_payment` | otherwise | `cancelled`, `cancelled_by driver`, release slots. No refund, no `refund_reason` | `200` |
| `confirmed` | `$now < arrival_at` | `cancelled`, `refund_reason cancelled_before_arrival`, release slots, queue refund (full fee), email | `200` with `refund.status = pending` |
| `confirmed` | `$now ≥ arrival_at` | `cancelled`, `refund_reason none_after_arrival`, release slots, email | `200` |
| `cancelled` by this driver | — | none | `200` (idempotent) |
| anything else | — | none | `409 reservation.invalid_state` |

If the hold window contains `$now`, the transaction takes `lot_occupancy` first and bumps the version (M3B-13).

### 7.9 `GET /v1/reservations/{id}/qr` (FR-RES-7)

**200** `{token, entry_valid_from, entry_valid_until}` with `Cache-Control: no-store`, for `confirmed` and `checked_in` reservations. The two times are the hold window and bound **entry only**; the schema description states that the same token keeps working at the pay station and exit while the reservation is `checked_in` (checked-in drivers need it hours later), so clients must not hide the QR after `entry_valid_until` once checked in **(our call: names that cannot be misread as the token's expiry)**. Other states → `409 reservation.invalid_state`. The token is never logged (§15.4).

### 7.10 `GET /v1/payments`, `GET /v1/payments/{id}` (FR-PAY-8, M3B-37)

List: `?limit=1..50&cursor=`, keyset `(created_at desc, id desc)`, `user_id = me`. Item: `{id, receipt_no, kind, method, amount_cents, status, card_brand, card_last4, failure_code, reservation_id, created_at, succeeded_at, refunded_at, refund}`. `receipt_no` is `null` unless `succeeded` or `refunded`. `GET /{id}` → the item or `404 payment.not_found`.

### 7.11 Vehicle in-use adapter (FR-VEH-4, M3B-31)

`reservations.VehicleChecker.HasActive(ctx, vehicleID)`: `exists (select 1 from reservations where vehicle_id = $1 and status in ('pending_payment','confirmed','checked_in'))`, through the context transaction. `DELETE /v1/vehicles/{id}` already answers `409 vehicle.in_use` when it returns true (M1).

---

## 8. Transactions, locks and notifications

### 8.1 Lock order (M3B-14)

`lot_occupancy` → `reservations` → `reservation_slot_loads` (ascending `slot_start`) → `stays` → `payments` → `refunds` → `users` → inserts (`reconciliation_alerts`, `outbox_messages`, `audit_log`) → `device_events` (last). `vehicles` only `FOR SHARE`, before `reservations`.

| Flow | Locks, in order |
|---|---|
| Create | `vehicles` share → insert `reservations` → `slot_loads` (asc) |
| PATCH vehicle | `reservations` → `vehicles` share (share locks never wait on each other; vehicle writers never touch reservations) |
| Pay, start attempt (tx 1) | `reservations` → insert `payments` |
| Pay, finalize (tx 2) / reconcile | [`lot_occupancy` if the window contains `$now`] → `reservations` → [`slot_loads` on the late path] → `payments` → [insert `refunds`] → outbox |
| Cancel | [`lot_occupancy` if the window contains `$now`] → `reservations` → `slot_loads` → `payments` (read live fee) → insert `refunds` → outbox |
| Unpaid expiry job | `reservations` (`skip locked`) → `slot_loads` |
| No-show job | `reservations` (`skip locked`) → `slot_loads` → `users` → outbox |
| Void job, reservation stay (M3B-60) | `lot_occupancy` → `reservations` → `slot_loads` (asc) → `stays` (re-checked `entry_pending` and older than 60 s) → version bump. Per row, not the M2 batch claim, because the M2 claim locks `stays` first |
| Late entry pass of a voided reservation stay | `lot_occupancy` → `stays` → `reservations` → `slot_loads`, under the lot lock like the exit pass (§12.7) |
| Boundary job | `lot_occupancy` (update version) |
| QR entry (device) | `lot_occupancy` → `reservations` → `slot_loads` → insert/update `stays` → [`payments` read → insert `refunds` → insert alert → outbox] → `device_events` |
| Pay station / exit credential with QR | `stays` (M2) — the reservation is read, not locked |
| Exit pass of a reservation stay | `lot_occupancy` → `stays` → `payments` (zero) → `reservations` (complete) → `device_events`. The only `stays → reservations` step; it runs under the lot lock, as does every flow that locks a checked-in reservation, so no cycle exists |
| Refund job | claim `refunds` (`skip locked`); finalize `payments` → `refunds` → outbox / alert |

Every transaction sets `lock_timeout = '1s'` and `statement_timeout = '2s'` (M2 §8.1 pattern, M3B-55); a timeout answers `500 internal.unexpected` and stores nothing.

### 8.2 Notifications (M3B-13, M3B-39)

- The version bump is the M2 statement: `update lot_occupancy set version = version + 1, updated_at = $now where lot_id = $1 returning version`, then `pg_notify('lot_changed', lot_id || ':' || version)` in the same transaction.
- API-caused hold changes at `$now`: confirm inside the window, cancel of a confirmed reservation whose window contains `$now`, QR check-in, physically-full cancel. A late-success expiry (M3B-10) never bumps: the reservation was never `confirmed`, so its hold never counted.
- Time-based changes: `hold_boundary_notify` (§12.3).
- The no-show and unpaid-expiry jobs never bump: a no-show acts at `$now ≥ upper`, when the hold already stopped counting; `pending_payment` never counted.

### 8.3 Cap transaction (architecture §7, M3B-15, M3B-16)

```sql
-- inside the create transaction, after the reservation insert
insert into reservation_slot_loads (lot_id, slot_start, held)
select @lot_id, s, 0 from generate_series(@w_start, @w_end - interval '5 minutes', interval '5 minutes') s
on conflict do nothing;
select slot_start, held from reservation_slot_loads
 where lot_id = @lot_id and slot_start >= @w_start and slot_start < @w_end
 order by slot_start for update;
-- any held >= @cap → rollback, 409 reservation.capacity_reached
update reservation_slot_loads set held = held + 1
 where lot_id = @lot_id and slot_start >= @w_start and slot_start < @w_end;
```

Release is the same range with `held = held - 1` (`check (held >= 0)` catches double releases). `@cap = floor(max_reservable_pct × effective_capacity / 100)` with `effective_capacity` from an unlocked read in the same transaction.

### 8.4 Holds adapter (`availability.ReservationHolds`)

- `EffectiveHolds(lot, now)`: `count(*) from reservations where lot_id = $1 and status = 'confirmed' and hold_window @> $now::timestamptz` (GiST index). Through the context transaction when present, so the gate reads it under the lot lock.
- `UpcomingArrivals(lot, now, within)`: `count(*) where lot_id = $1 and status = 'confirmed' and arrival_at > $now and arrival_at <= $now + within`. FR-AVAIL-6 (the M2 clients hid it until M3; Track B un-hides it).

---

## 9. Payments

### 9.1 Port (architecture §9, M3B-9)

```go
type Gateway interface {
    Charge(ctx context.Context, req ChargeRequest) (ChargeResult, error)
    Refund(ctx context.Context, req RefundRequest) (RefundResult, error)
}

type ChargeRequest struct {
    IdempotencyKey string // "rsv-fee:<payment public_id>", required
    AmountCents    int64
    Card           *Card  // nil on a re-query
}
type ChargeResult struct {
    Outcome     ChargeOutcome // Succeeded | Declined
    DeclineCode string        // card_declined | card_not_supported
    ProviderRef string
}
type RefundRequest struct {
    IdempotencyKey string // "refund:<refund public_id>"
    ChargeKey      string // the charge's idempotency key
    AmountCents    int64
}
type RefundResult struct{ ProviderRef string }

var ErrChargeUnknown = errors.New("payments: charge key never seen")   // re-query of a key never submitted
```

Errors other than `ErrChargeUnknown` (timeouts, `context.DeadlineExceeded`, transport failures) mean "outcome unknown". `Card` carries the number, expiry and CVC in memory only; its `String`/`LogValue` methods print `card(****1234)`.

### 9.2 Card handling (M3B-6)

- Validation with `validator/v10` custom rules `luhn`, `card_expiry` (against `$now` in `America/Santo_Domingo`), `numeric` lengths. `FieldError.Param` never contains input.
- Brand by BIN: `4` → visa; `51–55`, `2221–2720` → mastercard; `34`, `37` → amex; else other.
- The fingerprint (M3B-7) is computed before the card leaves the handler; the card struct is passed by pointer to the gateway call and dropped.

### 9.3 Pay flow

```
tx 1  lock reservation (owner, FOR UPDATE)
      ├─ payment with (reservation, client_key) exists?
      │     same fingerprint → replay its current outcome (200 / 402 / 202)          ─► return
      │     different        → 409 idempotency.key_reused                             ─► return
      ├─ status ≠ pending_payment → 409 reservation.invalid_state
      ├─ $now ≥ payment_due_at   → 409 reservation.payment_expired
      ├─ a fee payment is pending → 409 payment.in_progress
      └─ insert payments (kind reservation_fee, method gateway_sim, status pending, amount = fee_cents, created_at = @now,
                          user_id, reservation_id, client_key, request_fingerprint, brand, last4,
                          idempotency_key = 'rsv-fee:' || public_id)
         (a concurrent same-key insert gets 23505 on payments_reservation_client_key → re-read → replay;
          a concurrent other-key insert gets 23505 on payments_reservation_live_key → 409 payment.in_progress)
commit
call  Gateway.Charge(ctx with PAYMENT_CHARGE_TIMEOUT, key, amount, card)
tx 2  Apply(paymentID, result):
      ├─ succeeded → §9.4 confirm or late-success rule
      ├─ declined  → payment failed (failure_code, failed_at); reservation unchanged     ─► 402
      └─ unknown   → no write                                                            ─► 202
```

`Apply` is fenced on `payments.status = 'pending'`: if the reconcile job finalized first, tx 2 reads the final state and answers from it. The handler uses a context detached from the request for tx 2 (M3B-56); the reconcile job is the backstop if the process dies.

### 9.4 Confirm, late success and reconcile (M3B-10)

`Apply` with a success, in order (lock order §8.1):

1. If the window contains `$now`, lock `lot_occupancy`.
2. Lock the reservation.
3. **Confirm** — the reservation is `pending_payment` and `$now < upper(hold_window)`: lock the payment → `succeeded`, `succeeded_at = $now`, `provider_ref`; reservation → `confirmed`, `confirmed_at`, new `qr_nonce`; enqueue `reservation_confirmed` (payload `qr_token`, dedupe `reservation_confirmed:<public_id>`); bump the version when step 1 locked.
4. **Late** — the reservation is `pending_payment` and `$now ≥ upper(hold_window)`: reservation → `expired_unpaid`, `expired_at = $now`, `refund_reason = facility_unavailable`; release slots; lock the payment → `succeeded`; insert a refund (full fee, `facility_unavailable`); enqueue `reservation_cancelled`. Logged at warn.
5. **Defensive** — the reservation is in any other state (the guards of M3B-10 make this unreachable, but money must never be kept by accident) **(our call: a success is always either honoured or refunded)**: lock the payment → `succeeded`; insert a refund (full fee, `facility_unavailable`); no reservation transition; logged at error.

The steps follow the lock order of §8.1 (`reservations → slot_loads → payments → refunds`).

The **reconcile job** (§12.1) selects `pending` fee payments with `created_at < $now − (PAYMENT_CHARGE_TIMEOUT + 10 s)` (fee payment rows write `created_at = @now` explicitly, M3B-53), calls `Charge` with the key and no card, and runs `Apply`. `ErrChargeUnknown` → `failed`, `failure_code = 'not_submitted'`. Any other error → retried on the next tick.

### 9.5 Simulated adapter (M3B-11)

| Card | Recorded outcome | Response |
|---|---|---|
| `4242424242424242` | succeeded | after `SIM_GATEWAY_LATENCY` |
| `4000000000000002` | declined `card_declined` | after latency |
| `4000000000009995` | succeeded, `delayed = true` | waits `SIM_GATEWAY_TIMEOUT_DELAY` (30 s) or until `ctx` ends, then returns `ctx.Err()` |
| any other Luhn-valid | declined `card_not_supported` | after latency |
| re-query (card nil) | — | the recorded result at once, or `ErrChargeUnknown` |

- `Charge` inserts its row with `on conflict (idempotency_key) do nothing` and re-reads, so a repeat returns the first result.
- `Refund` sets the refund columns once (`where refund_idempotency_key is null or refund_idempotency_key = $key`) and returns the same `provider_ref` on a repeat. A refund of a declined or unknown charge returns an error.
- The adapter takes an injected `sleep(ctx, d)` so tests never wait (§15).
- Config refuses `APP_ENV=production` with the simulated adapter unless `PAYMENT_GATEWAY=simulated` is set explicitly (M3B-54).

---

## 10. State machines

### 10.1 Reservation (spec §5.4, M3 transitions)

| From | To | Trigger | Side effects in the same transaction |
|---|---|---|---|
| — | `pending_payment` | create | slots +1 |
| `pending_payment` | `confirmed` | charge succeeded inside the window | `qr_nonce`, email, version bump if effective |
| `pending_payment` | `expired_unpaid` | `$now ≥ payment_due_at` and no pending payment (job) | slots −1 |
| `pending_payment` | `expired_unpaid` | charge succeeded after `upper(hold_window)` (M3B-10) | slots −1, refund `facility_unavailable`, email |
| `pending_payment` | `cancelled` | driver cancels, no pending payment | slots −1 |
| `confirmed` | `checked_in` | valid QR inside the window, room under rule 2 | slots −1, stay `entry_pending`, version bump |
| `confirmed` | `cancelled` | driver cancels | slots −1, refund before arrival, email, version bump if effective |
| `confirmed` | `cancelled` | QR while physically full (`facility_unavailable`, `cancelled_by system`) | slots −1, refund, alert, email, version bump |
| `confirmed` | `expired_no_show` | `$now ≥ upper(hold_window)` (job) | slots −1, no-show block check, email |
| `checked_in` | `completed` | exit pass of its stay | — |
| `checked_in` | `confirmed` | the void job voids its stay (no entry pass within 60 s, M3B-60) | slots +1 **without** a cap check, version bump |
| `confirmed` | `checked_in` | late entry pass of its voided stay (§12.7) | slots −1, version bump |
| `confirmed` | `cancelled` (`facility_closed`) | lot closed | **M4** (M3B-2) |

Any other transition is rejected by the domain function `reservations.Transition(from, event) (to, error)` and covered by the table test (§15.1).

### 10.2 Payment (reservation fee)

`pending → succeeded` (charge succeeded) · `pending → failed` (declined, or `not_submitted`) · `succeeded → refunded` (its refund succeeded). An unknown outcome is **not** a transition: the row stays `pending` until the reconcile job gets an answer (§20 I5).

### 10.3 Refund (spec §5.4)

`pending → pending` (failed attempt: `attempts++`, `next_attempt_at` backoff) · `pending → succeeded` (payment → `refunded`, `refunded_at`; email) · `pending → failed` (attempt 10 failed: alert `refund_failed`).

### 10.4 Stay (M2 plus one transition)

`voided → entry_pending`: a reservation stay re-used when its reservation, back to `confirmed` after the void (M3B-60), is checked in again by its QR inside the window (M3B-24). Ticket and card stays never take it. The M2 `voided → active` late pass applies to reservation stays too (§12.7).

---

## 11. Gate integration

### 11.1 QR at the entry barrier (FR-GATE-2, FR-RES-8/9, architecture §7.1)

Replaces the M2 branch in `entry.go` (`KindReservationQR` → `deny gate.credential_invalid`).

1. **Before any lock:** decode and verify the token (constant-time HMAC). Malformed or bad MAC → `deny gate.credential_invalid`. Look the reservation up by `public_id` in the device's lot; not found or `qr_nonce` mismatch → `deny gate.credential_invalid`.
2. Lock `lot_occupancy`, then the reservation (`FOR UPDATE`); re-check the nonce and read the status.
3. `lots.status = 'closed'` → `deny gate.lot_closed`.
4. Decide:

| Reservation status | Condition | Outcome |
|---|---|---|
| `confirmed` | `$now < lower(window)` | `deny reservation.too_early` (FR-RES-9) |
| `confirmed` | `$now ≥ upper(window)` | `deny reservation.window_closed` (M3B-22) |
| `confirmed` | inside, `occupied + pending ≥ effective_capacity` (M3B-23) | §11.5 physically full → `deny gate.lot_physically_full` |
| `confirmed` | inside, room | `checked_in` (`checked_in_at = $now`), release slots, insert stay — or, when a voided stay exists from an earlier check-in (M3B-60), move it `voided → entry_pending` with `accepted_at = $now` (`voided_at` kept) instead of inserting — (`credential_kind 'reservation'`, `reservation_id`, `tariff_version_id` = the reservation's, `entry_pending`, `accepted_at = $now`), version bump → `open {stay_id}` |
| `checked_in` | stay `entry_pending`, inside the window | refresh `accepted_at` → `open {stay_id}` (M3B-24) |
| `checked_in` | stay `active` or `paid` | `deny gate.credential_in_use` |
| `expired_no_show` | — | `deny reservation.window_closed` |
| any other | — | `deny gate.credential_invalid` |

A stranded check-in is never answered here as `checked_in` + `voided`: the void transaction returns the reservation to `confirmed` (M3B-60), so the rows above apply, including the physically-full cancel.

Rule 2 (spec §5.2): the QR skips the public-availability check. `effective_holds` is not read on this path.

### 11.2 QR codec (architecture §11.2, M3B-21)

Pure functions in `reservations` (TDD, golden vector): `EncodeQR(key, publicID, nonce) string`, `DecodeQR(key, token) (publicID, nonce, ok)`. Decode rejects any length other than 86, any non-base64url character, padding, and a bad MAC (`hmac.Equal`). In `device_events.payload` the value is already redacted as `sha256:<12 hex>` (M2 D11).

### 11.3 Pay station (FR-GATE-5)

`stayByCredential` gains the `reservation_qr` branch: decode → reservation in the lot → `StayByReservationForUpdate` where the stay is open. Then the M2 flow unchanged: amount due from `entered_at` with the reservation's tariff version (M3B-25), cash, overstay. The fee is not credited (FR-PAY-7). The cash and zero payment inserts set `user_id` from the reservation (M3B-27).

### 11.4 Exit (FR-GATE-6)

`exit.credential_presented` with a QR uses the same lookup. The exit pass (`barrier.passed exit`) of a stay with `reservation_id` also runs `reservations.Complete` (`checked_in → completed`, `completed_at = $now`) in the pass transaction (M3B-26).

### 11.5 Physically full (FR-GATE-2, spec §5.2 rule 4, architecture §7.1)

In the device transaction, under the lot lock already held:

1. Reservation `confirmed → cancelled`, `cancelled_by system`, `cancelled_at = $now`, `refund_reason facility_unavailable`.
2. Release its slots.
3. Read the live fee payment (`succeeded`); insert a refund (full fee, `facility_unavailable`, `next_attempt_at = $now`).
4. Insert `reconciliation_alerts` (`reservation_unadmitted`, `details {reservation_id, refund_id}`).
5. Enqueue `reservation_cancelled` (refund pending).
6. Bump the version (the hold disappears).
7. Answer `deny gate.lot_physically_full`; `device_events` stores it, so a replay returns the same answer without repeating steps 1–6.

### 11.6 Kind matrix

No change: `reservation_qr` is already allowed for the entry barrier, the pay station (`credential`) and the exit barrier.

---

## 12. Worker jobs

Each job body is a function taking `now` (from the injected clock) so tests drive it with the fake clock (M2C-14). Batches claim rows with `for update skip locked`; several replicas are safe.

| Job | Cadence | Body |
|---|---|---|
| `payment_reconcile` | 15 s | §9.4. Batch 20 (M3B-58) |
| `expire_unpaid_reservations` | 30 s | Claim `status = 'pending_payment' and payment_due_at <= $now` (batch 200, `skip locked`, repeat while full); then, **after the row lock is held**, re-check in a separate statement that no fee payment is `pending` and skip the row if one is (the claim's subselect sees a pre-lock snapshot, so a pay attempt committed in between would otherwise be missed); → `expired_unpaid`, `expired_at = $now`, release slots |
| `expire_no_shows` | 30 s | `status = 'confirmed' and upper(hold_window) <= $now`, batch 200 → `expired_no_show`, release slots, no-show block (M3B-40), enqueue `reservation_no_show` |
| `hold_boundary_notify` | 30 s | §12.3 |
| `refund_processing` | 10 s | §12.5. Batch 20 (M3B-58) |
| `reservation_reminders` | 1 min | §12.4 |
| `void_unpassed_entries` (M2, changed) | 15 s | The M2 batch now excludes `credential_kind = 'reservation'`; reservation stays are voided per row with the reservation step (§12.7, M3B-60) |

### 12.1 Payment reconcile

As §9.4. One gateway call per row, outside any transaction, with `PAYMENT_CHARGE_TIMEOUT`.

### 12.2 No-show block (FR-RES-17, M3B-40)

```sql
select count(*) from reservations
 where user_id = @user_id and status = 'expired_no_show' and expired_at > @now - make_interval(days => @window_days);
-- count >= threshold:
update users set reservation_blocked_until = greatest(coalesce(reservation_blocked_until, @now), @now + make_interval(days => @block_days))
 where id = @user_id;
```

The count includes the row just expired (same transaction).

### 12.3 Hold boundary notify (M3B-39)

Per lot (one in Phase 1): `EffectiveHolds(lot, $now)`; if it differs from the job's last notified value, or the job has not notified since the process started, bump the version and NOTIFY in a short transaction (`lot_occupancy` only).

### 12.4 Reminders (M3B-41)

```sql
select … from reservations
 where status = 'confirmed' and arrival_at > @now and arrival_at <= @now + interval '30 minutes'
   and confirmed_at <= arrival_at - interval '30 minutes';
```

Enqueue `reservation_reminder` with dedupe `reservation_reminder:<public_id>` (`on conflict do nothing`).

### 12.5 Refund processing (FR-PAY-5, M3B-29)

1. Claim tx: `update refunds set attempts = attempts + 1, next_attempt_at = @now + interval '60 seconds' where id in (select id from refunds where status = 'pending' and next_attempt_at <= @now order by next_attempt_at limit 20 for update skip locked) returning …`.
2. For each, outside a transaction: `Gateway.Refund(key = idempotency_key, charge key, amount)`.
3. Success tx (fenced on `status = 'pending' and attempts = @attempts`): refund `succeeded`, `provider_ref`, `succeeded_at`; payment `succeeded → refunded`, `refunded_at`; enqueue `refund_succeeded` (dedupe `refund_succeeded:<refund public_id>`).
4. Failure tx (same fence): `last_error` (truncated, never card data); `attempts < 10` → `next_attempt_at = @now + min(10 s × 2^(attempts−1), 30 min)`; `attempts = 10` → `failed`, `failed_at`, alert `refund_failed`.

A crash between steps 2 and 3 leaves the lease to expire; the next claim calls `Refund` again with the same key, which returns the first result.

### 12.6 Emails (FR-NOTIF-1, M3B-42, M3B-43)

| Template | Enqueued by | Payload (besides locale data) |
|---|---|---|
| `reservation_confirmed` | confirm (§9.4) | `reservation_id`, `arrival_at`, `window`, `plate_display`, `fee_cents`, `qr_token` (scrubbed after send) |
| `reservation_reminder` | §12.4 | `reservation_id`, `arrival_at`, `window` |
| `reservation_cancelled` | cancel, physically full, late success | `reservation_id`, `refund_status` (`pending` / `none`), `refund_reason`, `fee_cents` |
| `reservation_no_show` | §12 no-show | `reservation_id`, `arrival_at`, `blocked_until?` |
| `refund_succeeded` | §12.5 | `reservation_id`, `amount_cents` |

Emails render in the user's `locale` (FR-I18N-5). The QR is attached as an inline PNG generated at send time from `qr_token`. `MarkOutboxSent` and `MarkOutboxDead` remove `qr_token` as they remove `code`.


### 12.7 Void of a reservation stay (M3B-60, user decision)

1. Select candidate reservation stays (`status = 'entry_pending' and accepted_at < $now − 60 s and credential_kind = 'reservation'`) without locking.
2. Per stay, one transaction in the §8.1 order: lock `lot_occupancy`; lock the reservation; if it is `checked_in`, increment its slot loads (ensure rows, lock ascending, **no** cap check); lock the stay and re-check it is still `entry_pending` and older than 60 s (else roll back, the car passed or was re-opened); stay → `voided`, `voided_at = $now`; reservation `checked_in → confirmed` (`checked_in_at` cleared); bump the version and NOTIFY (the hold counts again while its window contains `$now`; outside it the bump is harmless).
3. A reservation in any other state (cancelled or expired meanwhile) only has its stay voided, as in M2.

**Late entry pass** of a voided reservation stay (M2C-31 `voided → active`): under the lot lock, if its reservation is `confirmed` it moves `confirmed → checked_in` (`checked_in_at = $now`) and its slots are released, with a version bump; if the reservation was cancelled or expired meanwhile, only the stay changes (the car is inside and is billed normally).

If the void happens after `upper(hold_window)`, the reservation returns to `confirmed` and the next `expire_no_shows` run expires it (fee forfeited, counted for the block).

---

## 13. Simulator

### 13.1 Driver client (M3B-44)

`internal/simulator` gains a small driver client over the public driver API: login (`client_kind: mobile`), refresh on 401 (single flight), quote, create, pay, get QR. Credentials are derived from `SIMULATOR_FLEET_SECRET` (§2.2), matching `cmd/seed`. Tokens live in memory. Drivers 01–16 serve `reservation_arrivals` and QR cars, drivers 17–20 serve `no_show` only (M3B-59). Within its pool a scenario uses drivers round-robin; a driver blocked by no-shows (`reservation.blocked_no_shows`) or holding an active reservation is skipped. Missing drivers (login `401`) → `reservation_arrivals` and `no_show` answer `422 simulator.scenario_unavailable` at start.

### 13.2 Scenarios (M3B-45)

| Scenario | Drive-up traffic | Reserved cars | Notes |
|---|---|---|---|
| `reservation_arrivals` | as `weekday` | 8 per virtual hour (Poisson, M3B-58). Each: quote → create with `arrival_at = ceil5(server now)` → pay `4242…` → QR → after 1–5 virtual min presents the QR → normal lifecycle with the QR at the pay station and exit | A denial (`reservation.too_early`, `gate.lot_physically_full`, …) is logged and the car leaves |
| `no_show` | as `weekday` | 4 per virtual hour (M3B-58) book and pay but never arrive | The worker expires them when `upper(window)` passes in server time (25 real minutes in live compose) |

Determinism (FR-SIM-7): the RNG draws stay in the engine goroutine; server responses (reservation ids, tokens) never feed it, as in M2C-44.

### 13.3 Control API (M3B-46)

| Method and path | Change |
|---|---|
| `POST /control/cars` | `credential` gains `reservation_qr`; `qr` is required with it and excluded otherwise (`excluded_unless`), pattern `^[A-Za-z0-9_-]{86}$`. The car presents it at the entry barrier, pays with it at the pay station (`hold` and `dwell_minutes` as for tickets), and exits with it |
| `POST /control/scenarios/start` | `reservation_arrivals` and `no_show` no longer answer `422` (they do when the simulated drivers are missing, §13.1) |
| `GET /control/events` | Entries for QR cars show `credential: "reservation_qr"`; the token never appears |

**Demo path (spec §8 M3):** the driver reserves on mobile and pays; on web the reservation page shows the QR and its code (Track B); the operator pastes the code into the console's "Present reservation QR" with `hold: true`; the barrier opens; later `leave` pays the hourly fee at the pay station and exits.

---

## 14. Contract and error codes

### 14.1 OpenAPI (task S1)

| Operation | `x-mypark-pending` | Notes |
|---|---|---|
| `POST /v1/reservations/quote` | `S3` | |
| `POST /v1/reservations`, `GET /v1/reservations`, `GET /v1/reservations/{id}`, `PATCH /v1/reservations/{id}` | `S3` | |
| `POST /v1/reservations/{id}/pay` | `S4` | `Idempotency-Key` header parameter (required); responses `200`, `202`, `402`, `409`, `422`, `429`; `Idempotent-Replayed` and `Retry-After` response headers. The description lists the three test cards (M3B-11) |
| `GET /v1/reservations/{id}/qr` | `S4` | `Cache-Control: no-store` |
| `POST /v1/reservations/{id}/cancel` | `S5` | |
| `GET /v1/payments`, `GET /v1/payments/{id}` | `S5` | |
| `POST /v1/devices/events` | — | Documented already; the `deny` reason enum gains `reservation.too_early` and `reservation.window_closed` (descriptive change only) |
| Simulator `POST /control/cars` | — | `credential` enum + `qr` field (S8 implements; the schema change ships in S1) |

All new request schemas are strict. Fixtures for MSW (Track B) cover each success shape and the `202` and `402` bodies. `npm run gen:contracts` regenerates `packages/contracts`.

### 14.2 Error codes added to `errcodes`

| Code | HTTP / where | When |
|---|---|---|
| `reservation.not_found` | 404 | Unknown, malformed or another driver's reservation (M3B-33) |
| `reservation.capacity_reached` | 409 | Cap reached in any slot of the window (spec scenario "cap is atomic") |
| `reservation.active_exists` | 409 | The driver already has a `pending_payment` or `confirmed` reservation (FR-RES-3) |
| `reservation.blocked_no_shows` | 409 | No-show block active; `details.blocked_until` (FR-RES-17) |
| `reservation.lot_closed` | 409 | Lot `closed` at create (FR-RES-14) |
| `reservation.no_space_now` | 409 | Arrival < 15 min away and public available < 1 (FR-RES-13) |
| `reservation.arrival_in_past` | 422 | Normalized arrival before `ceil5(now)` |
| `reservation.beyond_horizon` | 422 | Arrival after `now + booking_horizon_days` |
| `reservation.invalid_state` | 409 | Pay, PATCH, cancel or QR in a state that does not allow it; `details.status` |
| `reservation.payment_expired` | 409 | Pay at or after `payment_due_at` |
| `reservation.vehicle_change_closed` | 409 | PATCH at or after `arrival_at` (FR-RES-16) |
| `reservation.too_early` | `reason` (device, 200) | QR before `lower(window)` (FR-RES-9) |
| `reservation.window_closed` | `reason` (device, 200) | QR at or after `upper(window)`, or the reservation expired as a no-show (M3B-22) |
| `payment.declined` | 402 | Charge declined; `details {payment_id, failure_code}` |
| `payment.in_progress` | 409 | Another fee attempt is `pending`; cancel during a pending attempt |
| `payment.not_found` | 404 | Unknown or another driver's payment |
| `idempotency.key_reused` | 409 | Same `Idempotency-Key`, different request |

`gate.lot_physically_full` (defined in M2) is now returned. `simulator.scenario_unavailable` stays in the catalog and is returned only when the simulated drivers are missing. Every new code gets `errors.<code>` in es-DO and en in web **and** mobile in S1 (`check:i18n` parity reads the whole catalog).

---

## 15. Testing strategy

All tests run with `-race`, the injected clock, seeded randomness and an injected `sleep` in the simulated gateway, with no sleep-based assertions (policies §2.3).

### 15.1 Unit (TDD first)

| Package | Cases |
|---|---|
| `reservations` domain | Arrival normalization (aligned, unaligned, past, exactly `ceil5(now)`, horizon edge); window bounds `[lower, upper)` at every boundary instant; slot list (7 slots, alignment); cap rule (`held = cap − 1` passes, `= cap` fails, `effective_capacity` 280/279/0, pct 0 and 100); **state machine table**: every allowed transition of §10.1 and every rejected pair; QR outcome table of §11.1 as a pure decision function; Phase 1 policy guard (M3B-48) |
| QR codec | **Golden vector** (fixed key, id, nonce → fixed token); round trip; every single-byte tamper fails; wrong length, padding, non-base64url, wrong key |
| `payments` domain | Fingerprint stability and sensitivity (brand, last4, expiry change it; CVC does not); BIN → brand table; Luhn and expiry rules; refund backoff schedule (attempts 1–10, cap at 30 min, `failed` at 10); replay decision table (same/different fingerprint × pending/succeeded/failed) |
| `paymentsim` | Outcome table §9.5; repeat charge returns the first result; re-query with nil card; unknown key → `ErrChargeUnknown`; refund once, repeat returns the same ref; refund of a declined charge errors |
| `simulator` | QR car lifecycle trace; driver round-robin skips blocked and busy drivers; same seed → same request trace for both scenarios (server tokens excluded) |

### 15.2 Integration (real Postgres, as `mypark_app`)

- **`TestReservations_CapConcurrent200`** (spec §8 M3 exit criterion): 200 seeded drivers (fixture SQL, one vehicle each), rate limiter replaced by a no-op, 200 parallel creates **all with the same `arrival_at`** (so every window shares every slot), cap 84 → exactly 84 `201` and 116 `409 reservation.capacity_reached`; afterwards **for every slot `held` = the count of `pending_payment`/`confirmed` reservations whose window covers it**, and no deadlock error.
- **`TestReservations_CapAtomicPair`**: the spec scenario (83 overlapping, two at once → one succeeds).
- Create: every check of §7.3 in order; one active per driver under 10 parallel creates by the same driver → one `201`; vehicle delete during create serialized (M3B-31).
- **Pay suite:** success → `confirmed`, QR available, confirmation email enqueued; decline → `402`, retry with a new key succeeds; replay (same key, same card) of each outcome; same key + different card → `409`; 10 parallel calls with one key → one gateway charge, all answers equal; parallel calls with different keys → one `pending`, others `409 payment.in_progress`; `…9995` → `202`, then the reconcile job on the fake clock → `confirmed`; pending row without a gateway row → `failed not_submitted`; late success after the window → `expired_unpaid` + refund + version unchanged; pay after `payment_due_at` → `409`; one live fee payment per reservation enforced by the index.
- **Cancel suite:** pending without attempt; pending with a pending attempt → `409`; confirmed before arrival → refund pending → refund job → refund `succeeded`, payment `refunded`, email; at arrival → `none_after_arrival`, no refund; repeat cancel → `200`; slots released.
- **Refund job:** gateway fake fails n times → backoff schedule on the fake clock; 10 failures → `failed` + `refund_failed` alert; two job runners in parallel → one `Refund` call per attempt (fence), never a second `succeeded`.
- **Gate:** `TestGateQR_Guarantee` (occupied 279, a confirmed hold effective → dispenser `deny gate.lot_full`, availability 0, QR → `open`); `TestGateQR_PhysicallyFull` (all of §11.5 in one transaction, replay returns the stored `deny` and changes nothing, the refund job then succeeds); too early; window closed; forged token; other lot; nonce mismatch; re-present while `entry_pending` (M3B-24); **stranded check-in** (`TestGateQR_StrandedCheckIn`: the void job on the fake clock returns the reservation to `confirmed`, restores its slots above the cap without error and bumps the version; a re-presentation reuses the voided stay; a re-presentation while physically full cancels with a `facility_unavailable` refund; never returning → `expired_no_show`; a void after the window → expired on the next no-show run; a late entry pass after the void → `checked_in`, slots released); car inside → `credential_in_use`; pay station and exit with the QR; exit pass completes the reservation; billing from `entered_at` with the reservation's tariff version.
- **Holds:** `TestHolds_ConfirmToSSE` (an SSE client receives a newer version with `effective_holds + 1` after a confirm inside the window, < 2 s); `TestHoldBoundaryJob` (fake clock crosses `lower` → one bump; no change → no bump); `UpcomingArrivals` counts.
- **Jobs:** unpaid expiry skips a reservation with a pending attempt; no-show at `upper` exactly; `TestNoShow_BlocksAfterThreshold` (third no-show sets `reservation_blocked_until`; create → `409` with `blocked_until`); reminder enqueued once across two ticks and never for a reservation confirmed < 30 min ahead.
- **Vehicles:** `DELETE /v1/vehicles/{id}` → `409 vehicle.in_use` for each active status.
- **Receipts:** keyset paging, ownership `404`, `receipt_no` only on succeeded/refunded.
- Grants and schema (§6.3). **Slot invariant** asserted after every reservation integration test.

### 15.3 Contract

Every M3 route returns only catalog codes; route ↔ OpenAPI parity passes; the authz matrix lists the new driver routes (and admin `403`, anonymous `401`); `TestOpenAPI_NoPendingOperations` passes with the allowlist removed (S9).

### 15.4 Log-leak test (extended, M3B-12)

`TestNoSecretsInLogs` adds the test PANs, a CVC, `QR_HMAC_KEY`, a generated QR token and a simulated driver password; the same assertion scans `payments`, `refunds`, `sim_gateway_charges`, `device_events`, `outbox_messages` (after send) and the simulator event log.

### 15.5 Scenario tests in CI (FR-SIM-7, M2C-14)

In `cmd/api` (M2 D1), with the production handler, one fake clock shared by the API, the worker job bodies and the simulator engine, and the simulated drivers seeded:

| Test | Asserts |
|---|---|
| `TestScenario_Guarantee` | Spec §3.4 guarantee end to end: fill to 279, a simulated driver books and pays, the hold becomes effective (boundary job), the next drive-up is `deny gate.lot_full`, the QR car enters (`open`), and later pays and exits with a non-zero fee |
| `TestScenario_ReservationArrivals` | 2 virtual hours, seed 42: every reserved car that reached the gate inside its window entered; occupancy invariant; slot invariant; reservations end `completed` after `stop {drain: true}` |
| `TestScenario_NoShow` | No-show cars expire at `upper(window)`; their slots are released; the hold disappears from availability after the boundary tick |
| `TestScenario_ReservationDeterministic` | Same seed twice → identical traces |

### 15.6 Coverage (M3B-52)

≥ 90 % statement coverage on the `reservations` domain files (state machine, rules, QR codec) and the `payments` domain files, enforced by the existing CI coverage step.

### 15.7 M3 exit criteria, backend side

| Exit criterion (spec §8 M3) | Backend evidence |
|---|---|
| 200 parallel attempts never exceed the cap | `TestReservations_CapConcurrent200` |
| Guarantee scenario end to end with the simulator | `TestScenario_Guarantee`, `TestGateQR_Guarantee` |
| Cancel before → `refunded`; after → no refund; no-show → `expired_no_show` | Cancel suite, refund job, jobs suite |
| Simulated decline/timeout paths work and are visible in the UI | Pay suite (`402`, `202` → confirmed); *needs Track B* for the UI |
| QR while physically full → `gate.lot_physically_full`, cancelled, refund succeeds | `TestGateQR_PhysicallyFull` + refund job |
| Closing the lot refunds overlapping reservations | **Moved to M4** (M3B-2) |
| 3 no-shows in 30 days → `reservation.blocked_no_shows` | `TestNoShow_BlocksAfterThreshold` |

---

## 16. Build order (one branch, task by task)

Branch `feat/m3-backend`. Each task is implemented by one subagent, reviewed task-scoped (opus for S3, S4, S5, S6: concurrency and money), fixed, and committed as work units of ≤ 400 changed lines (policies §2.2); a task over budget splits into sub-slices (`S3.1`, `S3.2`, …, which the marker pattern allows). A final whole-branch review and one fix wave close the branch. The `x-mypark-pending` marker of an operation is removed by the task that serves it.

| # | Task | Contents | Sensitive | Depends on |
|---|---|---|---|---|
| S1 | **Contract handoff** | §14.1 paths and schemas, §14.2 codes in `errcodes`, `go generate`, `npm run gen:contracts`, MSW fixtures, translations (es-DO, en, web and mobile), allowlist in `TestOpenAPI_NoPendingOperations` (M3B-47). Both developers approve | payments (contract) | — |
| S2 | **Schema** | `00010`, `00011`, sqlc queries, grants and schema tests (§6.3), `QR_HMAC_KEY` config + production refusal | migration | S1 |
| S3 | **Cap + create** | Domain (normalization, window, slots, cap, state machine, Phase 1 guard), quote, create, get, list, PATCH, `Holds` and `VehicleChecker` adapters wired in `cmd/api`, concurrency tests | concurrency | S2 |
| S4 | **Gateway + pay** | `payments.Gateway`, `paymentsim`, fee attempts, pay endpoint, `Apply`, QR codec + `GET /qr`, `reservation_confirmed` template + inline QR (`rsc.io/qr`), version bump on confirm, `payment_reconcile` job | payments, QR signing, dependency | S3 |
| S5 | **Cancel + refunds** | Cancel endpoint, refund queueing, `refund_processing` job, alerts recorder, receipts endpoints, `reservation_cancelled` and `refund_succeeded` templates | payments | S4 |
| S6 | **Gate + QR** | §11 entry, physically full, reopen, stranded check-in in the void job and the late pass (§12.7, M3B-60), pay station, exit, completion, `user_id` on reservation-stay payments | device flows, payments | S5 |
| S7 | **Worker jobs** | Unpaid expiry, no-show + block, hold boundary, reminders, `reservation_reminder` and `reservation_no_show` templates, pool 8 | — | S6 |
| S8 | **Simulator** | Driver seed (M3B-44), driver client, scenarios, control API QR, scenario CI tests | secrets (derived passwords) | S7 |
| S9 | **Hardening + docs** | Remove the pending allowlist, log-leak extension, coverage gate, M3 acceptance test, compose env, docs (§16.1) | — | all |

Track B integrates against S1 types with MSW mocks, then against S3–S5 mid-week (spec §8).

### 16.1 Docs to update in S9 (policies §2.10)

- `architecture.md`: §4.3 adds 402; §7 create order (M3B-15) and the half-open window; §7.1 predicate with pending entries; §9 port re-query contract (M3B-9), test-card outcomes, late-success rule; §10 job list with cadences and `@now`; §11.1 new columns (`payment_due_at`, `completed_at`, payment provider columns, `client_key`, `request_fingerprint`), `reconciliation_alerts` kinds; §11.2 `QR_HMAC_KEY` and rotation note; §14 new env vars; §17 ADR for the sync-with-202 pay semantics.
- `spec.md`: §5.4 payment machine (unknown is not `failed`), stay `voided → entry_pending`, reservation `checked_in → confirmed` and back (M3B-60); FR-SIM-2 wording (M3B-44); §8 M3 exit criteria (closure moved to M4); §10.2 a new entry accepting the our calls among M3B-5 to M3B-59 and recording M3B-44 and M3B-60 as user decisions.
- `policies.md` §2.7: the two reservation rate limits.
- `deploy/compose/docker-compose.yml`: `QR_HMAC_KEY`, `PAYMENT_*`, `SIM_GATEWAY_*`, `SEED_SIMULATOR_DRIVERS`.
- `CLAUDE.md` milestone table.

---

## 17. Risks and deferred items

| # | Risk or item | Mitigation / target |
|---|---|---|
| R1 | Live no-show expiry takes up to 25 real minutes (M3B-45) | The demo shows no-show from a CI run or starts `no_show` before the other demo steps |
| R2 | Time-based hold changes reach clients up to 30 s late (M3B-13) | Gate decisions compute holds live; display only |
| R3 | A gateway that never answers keeps reservations `pending_payment` and their slots held (M3B-10) | Sim gateway always answers; a real provider needs a timeout-to-alert rule in Phase 2 |
| R4 | Rotating `QR_HMAC_KEY` invalidates every outstanding QR, including offline copies (M3B-21) | No rotation in Phase 1; documented |
| R5 | Simulated drivers accumulate no-show blocks: three `no_show` cars per driver block it for 7 real days, so after about 12 no-shows in 30 days the `no_show` pool (4 drivers) is exhausted and that scenario answers `422 simulator.scenario_unavailable` | Separate pools keep `reservation_arrivals` unaffected (M3B-59); blocked drivers are skipped; `compose down -v` resets |
| R6 | `reservation_slot_loads` grows by up to 288 rows a day per lot | Negligible for Phase 1; a purge of past slots is an M5 item |
| R7 | The derived simulated-driver passwords expose those accounts if the fleet secret leaks (M3B-44) | Dev-only, refused in production, fake `.test` addresses |
| R8 | One week, nine tasks, three sensitive | S1 on day 1; S8 scenarios are the first to trim (the control-API QR path alone meets the demo) |
| R9 | **Accepted race:** the no-show block is read unlocked in create (users are last in the lock order), so one create can slip past a block that committed a moment earlier | None; the next create is blocked |
| R10 | **Accepted:** the pay `Idempotency-Key` fingerprint excludes the full card number (M3B-7), so a reused key with another card sharing brand, last four and expiry replays instead of answering `409`. No money moves | None |
| R11 | A late charge after a re-pay (new key) is not auto-refunded, because the live-fee index allows one live fee per reservation | A `refund_failed` alert with `details.reason` `duplicate_charge`, `orphan_charge` or `gateway_inconsistent`; the M4 admin refund settles it |
| R12 | A real gateway adapter must guarantee one refund per charge across refund keys. The simulated gateway enforces it, and a refund requeued after `failed` gets a new key | Adapter contract for Phase 2 |
| R13 | Deploy order: an old worker voids reservation stays without restoring the hold (M3B-60) | Deploy the worker before the API |
| Deferred | FR-RES-15 closure, reconciliation job and views, admin reservation and refund views, admin cancel override | M4 |
| Deferred | Purge of past slot rows, `/metrics` for payment latency | M5 |

---

## 18. Left to the builder

- sqlc query names, file splits and Go names inside each feature, within §5.1.
- The exact generated name of the outbox template constraint (§6.2).
- The `QR_HMAC_KEY` dev default (generate once, mark dev-only in compose).
- Email copy and layout for the five templates (es-DO and en), and the QR PNG size.
- Whether the boundary job keeps its last value per lot in a map or a struct field (one lot in Phase 1).
- Translation copy for the new codes.
- The poisson rates of the two scenarios may be tuned if a 2-hour CI run gets too slow.

---

## 19. Changes vs spec and architecture

| Source | Original | This design | Decision |
|---|---|---|---|
| Spec §8 M3 scope and exit criteria | FR-RES-15 closure and its exit criterion in M3 | M4; schema hooks only | M3B-2 |
| Spec FR-SIM-2 | Simulator uses only the device API | Reworded to "only public APIs (device API + driver API), never the database"; seeded simulated drivers | M3B-44 (user decision) |
| Spec §5.4 payment machine | `pending → failed` on timeout | Unknown stays `pending` until re-queried | M3B-3, M3B-10 |
| Spec §5.4 reservation machine | No late-success path | `pending_payment → expired_unpaid` with a refund when a charge resolves after the window | M3B-10 |
| Spec §5.4 stay machine | No `voided → entry_pending` | Added for reservation stays | M3B-24, M3B-60 |
| Spec §5.4 reservation machine | No way back from `checked_in` | `checked_in → confirmed` when the void job voids its stay (hold restored without a cap check), and `confirmed → checked_in` on a late entry pass | M3B-60 (user decision) |
| Spec FR-VEH-4 | `confirmed` or `checked_in` blocks vehicle removal | Also `pending_payment` | M3B-31 |
| Architecture §4.3 | Status mapping without 402 | `402 payment.declined` | M3B-32 |
| Architecture §7 | Lock slots, then insert the reservation | Insert first, then lock slots | M3B-15 |
| Architecture §9 | `ChargeRequest` shape unspecified | Card optional; a key-only `Charge` is the re-query | M3B-9 |
| Architecture §10 | Job SQL with `now()` | `@now` from the clock | M2 D3 |
| Architecture §11.1 | `reservations`, `payments`, `refunds`, `reconciliation_alerts` columns | Adds `payment_due_at`, `completed_at`, `client_key`, `request_fingerprint`, `failure_code`, `failed_at`, `refunded_at`, `succeeded_at`/`failed_at` on refunds | M3B-7, M3B-19, M3B-26 |

---

## 20. Inconsistencies found

These are conflicts **between** the spec and the architecture (or inside one of them). The design resolves each one as noted, but the source documents should be aligned in S9.

| # | Where | Conflict | This design |
|---|---|---|---|
| I1 | Spec §5.2 and glossary vs architecture §7 | Hold window closed `[arrival − 15, arrival + 20]` vs half-open `[ … )` | Half-open (M3B-18) |
| I2 | Spec FR-GATE-2 and §5.2 rule 2 ("active stays") vs architecture §7.1 (`occupied`) vs spec §5.2 rule 5 and architecture §7 invariants (pending entries count) | Three predicates for "physically full" | `occupied + pending ≥ effective_capacity` (M3B-23). A pending entry that later voids can still cause a guaranteed driver's cancellation and refund: accepted, it matches spec rule 5 |
| I3 | Architecture §7 invariants | Labelled "one **open** stay per reservation" but the predicate `reservation_id is not null` allows one stay **ever** | One stay ever, re-opened on re-presentation (M3B-24) |
| I4 | Spec FR-RES-3 vs FR-VEH-4 | "Active" = `pending_payment`/`confirmed` vs `confirmed`/`checked_in` | Vehicle in-use counts all three (M3B-31) |
| I5 | Spec §5.4 payment machine vs architecture §9 | Timeout → `failed` vs re-query and finalize | Re-query (M3B-3) |
| I6 | Spec §5.1 vs M2 code (`entered_at` set on the pass) | A reservation stay is billed from "the check-in time" | `entered_at` of the pass (M3B-25) |
| I7 | Architecture §10 vs architecture/spec time rule (M2 D3, spec §10.2 #40.3) | Job SQL uses `now()` | `@now` |
| I8 | Spec §8 M3 vs the user's scope decision | Closure behaviour and its exit criterion | M4 (M3B-2) |
| I9 | `policy_versions` (architecture §11.1, migration 00007) vs architecture §7 and §11.1 indexes | `max_active_per_user` and `slot_min` are configurable columns, but the partial unique index hard-codes 1 and slot rows are 5-min | Fixed in Phase 1, guarded in the service (M3B-48) |
| I10 | Architecture §6 | "reservation confirm/cancel/expire … runs `pg_notify`" — an expiry never changes effective holds (pending holds never count; a no-show acts after the window) | No NOTIFY on expiry (§8.2) |
| I11 | Spec FR-SIM-2 vs FR-SIM-3 | "Only the device API" vs scenarios that need confirmed (paid) reservations, which the device API cannot create | **Closed** by the user (M3B-44): FR-SIM-2 becomes "only public APIs, never the database" |
| I12 | Architecture §7.1 vs §11.1 | §7.1 writes a `reconciliation_alerts` "incident", but the table's described purpose and FR-ADMIN-9 are gate/sensor discrepancies and overrun stays | One table for all incidents, kinds per M3B-30 |

---

## 21. Open questions

None open. Closed on 2026-11-16: Q1 (`rsc.io/qr` for the email QR, our call M3B-43), Q2 (simulator as seeded drivers, user decision M3B-44), Q3 (a late-charge refund reuses `facility_unavailable`, our call M3B-10), Q4 (`402` for a decline, our call M3B-32).
