# M2 Track B — Web and Mobile Clients: Live Availability, Lot Info, Simulator Console: Design

**Status:** draft — pending user approval · **Date:** 2026-11-15 · **Milestone:** M2 · **Track:** B (`apps/web`, `apps/mobile`, `packages/contracts`) · **Visual reference:** the approved M1 mockup [`2026-11-08-m1-clients-mockup.html`](./2026-11-08-m1-clients-mockup.html) (v3). There is no M2 mockup; the screens below are designed inside that system.

This document designs the client side of M2: the live availability count and the per-space sensor map, the lot information block (address, hours, rates), and a basic Simulator console for admins on the web. The backend for all of it is done and frozen (M2 backend design §11, §13.3, §20).

**The three decisions that shape everything else:**

1. **Each client reads the public SSE stream straight from the Go API.** Web uses the browser's `EventSource` against a runtime-configured public API origin. Mobile uses the `expo/fetch` streaming body it already has, with a small SSE parser. No new dependency, no BFF in the path (§5).
2. **The clients own reconnection, not the transport.** `EventSource` gives up for good on any HTTP error (429, 404). So both clients close on error and run the same backoff, freshness and snapshot rules, defined once as pure code in `packages/contracts` (§5.4–§5.6).
3. **The console talks to the simulator only through its own admin-guarded web route.** That route adds `X-Simulator-Token` on the server, checks the admin role on every request, and allows only the seven control operations (§7).

> **How to read this.** §1–§3 are why and what is fixed. §4 lists screens and states. §5–§7 are the mechanics: stream, data flow, console. §8–§11 cover shared code, design, i18n and accessibility. §12–§14 cover testing, build order and the builder's checks. §15 lists changes against the spec and architecture. Every decision is numbered **M2B-n** (so it doesn't collide with M1's CC-n or the backend's M2C-n) and collected in §2. M1 client calls CC-1…CC-27 still apply unless an M2B decision says otherwise.
>
> **Sources:** `docs/spec.md` (FR-AVAIL, FR-SIM, FR-BRAND, FR-I18N, NFR-PERF-1, NFR-AVAIL-1, NFR-A11Y-1, §8 M2, §10), `docs/architecture.md`, `docs/policies.md`, `apps/server/api/openapi.yaml`, `packages/contracts`, the M2 backend design, the M1 clients design, the existing client code, and the pinned Next.js 16 docs in `node_modules/next/dist/docs/`. Framework behavior stated below was checked against those sources or is listed as a builder check in §14.

---

## 1. Intent

### 1.1 Why

The backend counts cars, streams the count, and runs scenarios, but nobody can see any of it. The first M2 exit criterion ("availability on web and mobile updates in < 2 s") and the M2 demo ("start `weekday` from the console and watch the count and lot map move live") are both marked *needs Track B*. M2 also sets the live-data pattern that M3 (reservations) and M4 (admin dashboard stream) will copy: snapshot first, version guard, stale handling, reconnect.

### 1.2 Goals

| # | Goal | Verified by |
|---|---|---|
| G1 | Anyone, signed in or not, sees the live count, status, last update, address, map link, hours and rates on web and mobile (FR-AVAIL-1) | Screen tests, demo script |
| G2 | A pushed frame reaches the screen without a refresh, inside the same render pass (FR-AVAIL-2, NFR-PERF-1 client side) | The "frame to screen" tests in §12.2 |
| G3 | Losing the connection keeps the last values with a "stale — last updated hh:mm" label and reconnects with backoff (FR-AVAIL-5, NFR-AVAIL-1) | Fake-timer tests for 45 s, backoff, 429, 404 |
| G4 | Both clients show the 280-space sensor map without relying on color, and screen readers hear one summary, not 280 cells | Screen tests, NVDA and TalkBack pass |
| G5 | An admin can run the M2 demo from the web console: start and stop scenarios, inject and release cars, toggle sensor faults, and read the event and denial log | Console tests, demo script |
| G6 | The simulator token never reaches the browser, and a driver cannot reach the console or its route | BFF tests (§12.1) |
| G7 | Every new string exists in es-DO and en; every new color pair passes AA | `check:i18n`, `check:tokens` in CI |

### 1.3 Non-goals (M2)

- **Upcoming reservations count (FR-AVAIL-6)**: hidden until M3 (M2B-4).
- A console on mobile (FR-SIM-6 says admin web). An admin event stream or dashboard (M4, M2C-46).
- A geographic map widget or a map library. The "map link" is the server-built Google Maps URL.
- Push notifications, offline caching across app restarts, `netinfo`.
- Playwright and device e2e (M5). Script-level CSP (M5, CC-20).
- Official brand assets. New tokens stay `TODO(brand)`.

### 1.4 Anti-goals (failure even if it "works")

| Anti-goal | Guard |
|---|---|
| The count goes backwards because an old frame arrives late | Version guard: drop `availability` with `version ≤` last seen (M2B-12) |
| A dropped connection shows old numbers as if they were live | Stale state on any error and after 45 s of silence (M2B-13) |
| `EventSource` stops forever after one 429 and the page silently freezes | Clients close on every error and run their own backoff (M2B-11) |
| The simulator token reaches the browser | Token read only in the server route from `SIMULATOR_CONTROL_TOKEN`, never `NEXT_PUBLIC_*`; response test (M2B-27) |
| A driver drives the simulator | Role check on every console request, not only in the page layout (M2B-27) |
| Screen readers announce every 250 ms push or 280 cells | One throttled announcer; the grid is one summary element (M2B-22) |
| The count is "fixed up" from the map cells | The count comes only from `availability`; the map never feeds it (M2B-15) |
| The home page breaks when the API is down | The lot section degrades to an error panel; Log in and Sign up still work (M2B-17) |
| A stray query parameter turns off the map | The stream URL is built by one function with exactly `include=spaces` (M2B-9) |
| Hardcoded strings or color-only meaning | Keys in both locales; shapes and text for every state (§9, §11) |

---

## 2. Decisions

Columns: **By** is `user` for decisions made by the user on 2026-11-15, `ctrl` for calls made by the M2 controller and confirmed by this design, `ours` for the rest. **Cost if wrong** is what a reversal would cost later.

### 2.1 Made by the user and the controller

| # | By | Decision | Why | Cost if wrong | § |
|---|---|---|---|---|---|
| M2B-1 | user | **Placement (option A).** The web public home shows the live count to everyone, signed in or not. Mobile shows it as the first screen before login (Welcome) and as a **Lot** tab after login | FR-AVAIL-1 says "anyone"; spec §10.2 #2 lets anonymous visitors see availability; architecture §12 already calls `(public)` "home (availability)" | Low. The live block is one feature component; moving it is a route change | §4 |
| M2B-2 | user | **Transport (option 1).** Each client connects directly to the API's public SSE stream: web `EventSource`, mobile `expo/fetch` streaming body plus a small SSE line parser, no new dependency. The console goes through its own admin-guarded web BFF route that adds `X-Simulator-Token` server-side and keeps a deny-by-default allow-list | Matches architecture §5 ("Public SSE availability can be read directly from Go because it is anonymous") and backend design §11.5. A BFF stream proxy would collapse every browser into the web container's IP for the SSE caps | Medium. Needs the public API origin, CORS for the web origin and, from M5, CSP `connect-src`. Switching to a BFF stream later is one route handler plus a URL change | §5, §7 |
| M2B-3 | ctrl | **Both clients show the per-space sensor map** under the count, from the `spaces` events of the same stream (`include=spaces`, exact query). Public data only: code and state. Accessible: shape, outline and pattern, not color alone; screen readers get one summary, not 280 announcements | Spec §8 M2 scope and demo name "the lot map" on web and mobile; M2C-7 made the map public | Low. Hiding it is one prop; the stream works without `include=spaces` | §4.3, §9.3 |
| M2B-4 | ctrl | **FR-AVAIL-6 (upcoming reservations in 2 h) is hidden until M3.** The schema keeps `upcoming_reservations_2h`; no UI renders it | M1 rule 2.1 #3: no "coming soon" placeholders. The value is always 0 in M2 (backend §19), so showing it would be misleading | Low. M3 adds one line to the count card | §4.3 |
| M2B-5 | ctrl | **Stale and offline (FR-AVAIL-5).** 45 s without any frame, or any connection error, means stale: keep the last values, show "Stale — last updated hh:mm" and a reconnecting state. Backoff 1 s → 30 s with jitter. Honor 429 `Retry-After` where the transport exposes it (mobile); on web, where `EventSource` cannot see the status, fall back to a REST snapshot plus a 10 s floor. Snapshot first; drop versions ≤ last seen | FR-AVAIL-5 and spec §10.2 #29 | Medium. The rules live in one pure module (M2B-10), so tuning numbers is a one-line change; the shape is hard to change once both clients depend on it | §5.4–§5.6 |
| M2B-6 | ctrl | **Console scope (admin web only).** Scenario start and stop (speed, seed, start time, drain), inject a car (ticket or employee card, held option, dwell), leave a car (lost-ticket option), sensor fault toggle, live event and denial log polled from `/control/events?after=`, status panel. Strings in both locales; errors translated through the shared catalog, including the new validation rules | FR-SIM-5, FR-SIM-6, FR-GATE-10 (met by the simulator log, M2C-54), the M2 demo | Low. Each control is an independent form | §4.4, §7 |

### 2.2 Our calls (review these)

| # | Decision | Why | Cost if wrong | § |
|---|---|---|---|---|
| M2B-7 | **Lot discovery:** `GET /v1/lots`, use the first item. The web RSC and the mobile query both do this; the id is never hardcoded. A `lot.not_found` on any later lot call triggers one re-discovery (web: `router.refresh()`; mobile: invalidate `['lots']`) | Lot ids are UUIDs created by the seed; a re-seeded database changes them. Phase 1 has one lot (spec §10.2 #15) | Low. A lot picker in Phase 2 replaces "first item" | §6 |
| M2B-8 | **Contracts gain an `m2/` folder** (`packages/contracts/src/m2/{lot,availability,spaces,tariff,simulator}.ts`): strict zod response schemas, the three SSE payload schemas, console request schemas, and typed fixtures in `src/fixtures/m2.ts`. Each schema is checked against `openapi.d.ts` with `expectTypeOf`, as in CC-8 | One definition for both clients; drift fails at compile time. SSE payloads are bare JSON, so they get their own schemas instead of `parseEnvelope` | Low | §8 |
| M2B-9 | **The SSE frame parser and the stream URL builder live in `packages/contracts/src/sse/`** as pure code: `createSseParser()` takes text chunks and emits `{event, id, data}` frames per the WHATWG rules (field lines, blank-line dispatch, `retry`, comments, CRLF and CR, chunks split anywhere). `streamUrl(origin, lotId)` returns exactly `…/availability/stream?include=spaces` | Mobile needs the parser; web uses it only in tests (EventSource parses natively). A pure module has no I/O, fits contracts like the M1 normalizers, and is tested once. One URL builder stops a cache-buster from turning off `spaces` (backend D9) | Low | §5.2, §8 |
| M2B-10 | **The live-data rules are pure functions in `packages/contracts/src/m2/live.ts`:** `reduceLive(state, frame)` (version guard, spaces full/delta), `FRESH_WINDOW_MS = 45_000`, `nextDelay(attempt, random, floorMs)`. Each app keeps a small connection manager around its own transport | Both clients must behave the same; transports differ too much to share the manager. Pure rules are easy to unit-test with fake clocks | Low | §5.4–§5.6 |
| M2B-11 | **Clients own reconnection.** Web: on any `EventSource` `error`, call `close()` and schedule our own reconnect. If `readyState` was already `CLOSED` (the HTTP response was not a 200 event stream: likely 429 or 404), the next delay has a 10 s floor and a REST snapshot is fetched, which also detects 404. If it was `CONNECTING` (network drop), normal backoff. Never set `withCredentials` | Per the WHATWG HTML spec, a non-200 status or wrong content type *fails* the connection: `readyState` becomes `CLOSED` and the browser never reconnects. Only network drops auto-retry, at the server's fixed `retry: 3000` with no jitter. `cors.go` sends no `Access-Control-Allow-Credentials`, so credentials would break CORS | Low | §5.4 |
| M2B-12 | **Snapshot first, version guard on availability only.** Every connection starts with an `availability` frame. Any `availability` with `version ≤` the last seen is ignored. `spaces` carries no version, so the map state resets on each new connection and ignores deltas until that connection's `full: true` arrives | Matches the server's wire rules (backend §11.3, D9). Prevents a late or replayed frame from moving the count backwards | Low | §5.5 |
| M2B-13 | **Freshness.** Any frame (availability, spaces, heartbeat, even one dropped by the version guard) resets a 45 s timer. A connection error makes the view stale immediately. When the 45 s timer fires, the client also tears the connection down and reconnects, because a half-open TCP connection never raises an error | FR-AVAIL-5: "any event or heartbeat counts as fresh". The active teardown is what makes "nothing for 45 s" recover on its own | Low | §5.5 |
| M2B-14 | **Two times on screen.** When live: "Last change hh:mm" from `availability.updated_at` (the last count change, FR-AVAIL-1). When stale: "Stale — last updated hh:mm" from the client time of the last frame received (or the last successful REST snapshot). Both in `America/Santo_Domingo` | `updated_at` can be hours old in a quiet lot while the data is perfectly fresh. The stale label must answer "how old is what I'm seeing", which only the last frame time answers | Low. Copy change only | §4.3 |
| M2B-15 | **The map and the count are independent.** The count comes only from `availability` (gate counter). The map shows sensor readings. They will differ on purpose: pending entries are not counted for up to 60 s (D8), and sensor faults or `sensor_drift` change cells but not the count. A muted note under the map says the map shows sensor readings; the screen-reader summary says "sensors report …" | Deriving or cross-checking one from the other would hide exactly what the fault toggle is meant to demonstrate | Low | §4.3 |
| M2B-16 | **Web public API origin is a runtime variable `API_PUBLIC_URL`** (server-only name, no `NEXT_PUBLIC_` prefix). The public home's Server Component reads it at request time and passes it as a prop to the client live island. Compose default `http://localhost:8080`. Missing or not an absolute `http(s)` URL: the island renders the server snapshot and a "Live updates unavailable" status, and does not connect | Next 16 inlines `NEXT_PUBLIC_*` at `next build` and freezes it (`environment-variables.md`, "Runtime Environment Variables"); the web image builds once without build args. The home already reads cookies, so it is dynamic and reads the env per request | Low. Switching to a build-time variable is a rename plus a Docker build arg | §5.3 |
| M2B-17 | **The web home renders when the API is down.** The RSC loads lot, availability, spaces and tariff with `Promise.allSettled` (5 s timeout each). Any failure renders the lot section's error panel with a retry; the brand hero, Log in and Sign up always render. Mobile Welcome does the same with query error states | NFR-AVAIL-1; the home is also the login entry point | Low | §4.1, §4.2 |
| M2B-18 | **Web REST fallback goes through the BFF.** Two public GET entries join the allow-list: `lots/:id/availability` and `lots/:id/spaces` (`auth: public`). The client uses them only while the stream is down, at most once per reconnect attempt and never more than once every 10 s | Keeps "the browser never calls Go directly" true for REST, with only the documented SSE exception. No CORS preflight paths to maintain. The calls are rare, so sharing the web container's rate-limit bucket (X1) is acceptable | Low | §5.4, §6.1 |
| M2B-19 | **TanStack Query holds the data; a tiny external store holds the connection state.** Keys: `['lots']`, `['lot', id]`, `['lot', id, 'availability']`, `['lot', id, 'spaces']`, `['lot', id, 'tariff']`. Availability and spaces: `staleTime: Infinity`, no refetch on focus or interval; the stream writes them with `setQueryData` through `reduceLive`. Lot and tariff: `staleTime` 5 min. Connection state (`connecting`, `live`, `stale`, `unavailable`) plus `lastFrameAt` lives in a per-lot store read with `useSyncExternalStore` | Architecture §12 says SSE writes into the Query cache. Connection state is not server data and changes on timers; putting it in Query would cause refetch side effects | Low | §6 |
| M2B-20 | **One connection per lot per app instance.** A module-level manager keyed by `(origin, lotId)` is reference-counted by subscribers and closes 2 s after the last one leaves. React StrictMode double mounts, fast refresh and screen changes reuse the open connection | The SSE caps (30 connects per minute per IP, 20 open per IP) are shared by every tab and device behind one IP; churn would hit them (R1) | Low | §5.4 |
| M2B-21 | **Mobile transport:** `services/sse.ts` calls `expo/fetch` directly (not the M1 `httpClient`, whose 3 s timeout would kill the stream) with `Accept: text/event-stream` and an `AbortController`. A 200 with `text/event-stream` is read with `getReader()` and `TextDecoder` (`{stream: true}`) into the shared parser. Any other status is read as a JSON envelope: 429 waits `max(Retry-After, backoff)`, 404 re-discovers the lot. The app going to the background aborts the stream; returning to the foreground reconnects (fresh snapshot) | Architecture §13 already chose `expo/fetch` streaming. Unlike `EventSource`, `fetch` exposes the status and `Retry-After`. Background streams waste battery and are killed by the OS anyway | Medium. If `expo/fetch` streaming misbehaves on Hermes, fallback is REST polling every 5 s (§14) | §5.2 |
| M2B-22 | **Announcements are separate from the visual count.** The big number is plain text, not a live region. One polite announcer (web `role="status"` element, mobile `announceForAccessibility`) says status changes (open, full, closed; live, stale) at once, and count changes at most every 30 s, only if the number changed. The 280-cell grid is hidden from assistive tech and replaced by one summary element | Pushes arrive up to 4 per second; announcing each would make the page unusable with a screen reader. Throttling only the announcer keeps the visual update instant (G2) | Low | §11 |
| M2B-23 | **Map layout.** 280 cells, in code order, in a CSS grid (web) or a wrapped `View` grid (mobile): 20 columns on wide web screens, 10 on narrow web and on mobile. No row or zone labels (the codes carry no zones: `E001`…`E280`). Free: white cell with a 2 px green outline. Occupied: solid slate fill. Out of service: hatched fill with a "×". Cells are not interactive. A legend under the grid repeats each shape with its text | The seed has no zones, so any grouping would be invented. Three different shapes keep meaning without color (1.4.1) | Low | §9.3 |
| M2B-24 | **Count card content.** The big number is `available`; the line under it is "of {effective_capacity} spaces". When `effective_capacity < capacity`, an extra line says "{n} out of service". When `status` is `closed`, the number is replaced by "Closed". `effective_holds` is not shown (always 0 until M3). No on-screen text about the 60 s pending-entry lag | `effective_capacity` is what the driver can actually get; holds are an M3 concept. The lag is internal and already within the demo's tolerance | Low | §4.3 |
| M2B-25 | **Lot info and rates.** Name, address, "Open in Maps" (`map_url`, new tab with `rel="noopener noreferrer"` and visible "opens in a new tab" text on web; `Linking.openURL` on mobile). Hours: `always_open: true` → "Open 24 hours"; `false` → "Hours vary — check at the lot". Rates: first hour, each additional hour, daily maximum, free exit within {grace} min, {post_payment} min to exit after paying, lost ticket. `reservation_fee_cents` hidden until M3. `latitude`/`longitude` unused | FR-AVAIL-1 asks for address, map link, hours and rates. The contract has no richer hours object; the seed is 24 h | Low | §4.3 |
| M2B-26 | **Money formatting.** One `formatMoney(cents, locale)` per app: `Intl.NumberFormat(locale, {style: 'currency', currency: 'DOP'})` on `cents / 100`, falling back to a locale key `money.dop` ("RD${amount}") if the runtime prints `DOP` instead of `RD$` (§14) | FR-I18N: RD$ with es-DO number formatting. ICU output differs between Node, browsers and Hermes | Low | §10 |
| M2B-27 | **Console route:** `app/api/admin/simulator/[...path]/route.ts` (GET, POST) with `lib/simulator/{allowlist,handler,upstream}.ts`. Per request: allow-list match, **admin role check** (`GET /v1/me` with the access cookie; not admin → 403 `auth.forbidden`, no session → 401 `auth.unauthenticated`), CSRF and Origin check on POST (CC-3), then `fetch(SIMULATOR_INTERNAL_URL + path)` with `X-Simulator-Token` from env and a 5 s timeout. Upstream unreachable → 502 `internal.unexpected` (as M1). `/api/admin/:path*` joins the `proxy.ts` matcher so the access cookie refreshes during a long demo | The control API is another upstream with another credential; mixing it into `/api/v1` would blur two allow-lists. Checking the role per request costs one API call every 2 s while the console is open, which is acceptable for one admin | Medium. A session-level role cache would cut the calls but adds invalidation | §7 |
| M2B-28 | **Console allow-list has typed parameters.** `GET status`, `POST scenarios/start`, `POST scenarios/stop`, `POST cars`, `POST cars/:carId/leave` (`^car-\d{6}$`), `POST sensors/:code/fault` (`^E\d{3}$`), `GET events` with only `after` (a non-negative integer) as query. Everything else → 404 `http.not_found`. A `204` passes through with no body | M1's matcher only knows UUID ids. Unknown query params are dropped, never forwarded | Low | §7.2 |
| M2B-29 | **The console is on when its config is present, not by `APP_ENV`.** If `SIMULATOR_INTERNAL_URL` or `SIMULATOR_CONTROL_TOKEN` is missing, the route returns 404 and the page shows a "Simulator not configured" panel. An upstream 401 `simulator.unauthorized` shows the same panel with the translated code | The demo may run with `APP_ENV=production` for cookie testing. Spec says the console goes away in Phase 2, which is a deploy change, not a flag | Low | §7 |
| M2B-30 | **Polling.** Status and events are polled every 2 s while the page is visible (TanStack Query `refetchInterval`, paused when the document is hidden). The log keeps the last 500 rows in memory, newest at the top | M2C-46 fixed the 2 s cadence; no admin stream in M2 | Low | §7.3 |
| M2B-31 | **Event log cursor, continuous across runs.** The cursor is the highest `seq` seen and is never reset when `status.started_at` changes: `seq` is per simulator process and the 500-entry ring survives runs. It resets to 0 only when the simulator may have restarted, that is, when any status or events poll failed and the next one succeeds (the log is per process, so `seq` restarts at 1). A returned max `seq` below the cursor cannot be observed, because `/control/events?after=` only returns entries above the cursor, so a stale cursor just returns an empty list. Optionally a run separator row is shown when `started_at` changes. If the first returned `seq` is greater than `after + 1`, a "Some events were skipped" row is inserted | `EventLog` lives for the simulator process (`cmd/simulator/main.go`) and is not cleared by Start or Stop. A restart faster than one 2 s poll is not detected and would leave the log quiet until `seq` passes the old cursor (accepted for a basic console) | Low | §7.3 |
| M2B-32 | **Held cars are remembered in memory, with a manual fallback.** A console store (zustand, per provider, as M1) keeps the car ids returned by inject, cleared on a new run. The Leave form also accepts a typed car id (`car-000123`), so a page reload is not a dead end | There is no list-cars endpoint; the log shows `car_id` on events, which the admin can copy | Low | §7.4 |
| M2B-33 | **Seeds keep every digit.** `simulatorStatusSchema` stays numeric, matching OpenAPI, so its type test holds. The console API layer separately extracts the exact digits from the raw response text (`"seed":(\d+)`) as `rawSeed: string` and displays that. The start form takes the seed as a digit string (up to 2^63 − 1), and the request serializer writes those digits into the JSON body text unquoted, so a copied seed replays exactly | `seed` is `int64`; a random seed above 2^53 loses digits in `JSON.parse` and `JSON.stringify`, and replaying a rounded seed gives a different run (FR-SIM-7) | Low | §7.4 |
| M2B-34 | **Unavailable scenarios are hidden.** The scenario picker lists `idle`, `weekday`, `weekend_peak`, `fill_to_full`, `sensor_drift`. `reservation_arrivals` and `no_show` (422 `simulator.scenario_unavailable` in M2) are not offered | M1 rule: no coming-soon placeholders. M3 adds them back | Low | §7.4 |
| M2B-35 | **Event log text.** `at` shows in virtual time, `America/Santo_Domingo`. `reason` uses `errors.<reason>` when the catalog has it, otherwise the raw code in monospace. `device`, `type` and `result` are shown as sent | The log is an admin diagnostic view, and `type`/`result` are free strings in the contract; translating them would need a contract change | Low | §7.3 |
| M2B-36 | **The console page also shows the compact live block** (count, status and map) beside the controls, reusing the web live component | The demo watches the count and the map move while driving the simulator; one window is easier to present | Low | §4.4 |
| M2B-37 | **Mobile routes.** `(auth)/welcome.tsx` (already the anchor) becomes the pre-login lot screen: header with the parking sign and language switch, the shared `LotLive` component, then Log in and Sign up. After login, a third tab `(tabs)/lot/index.tsx` with its own Stack renders the same `LotLive`. Tab order: Vehicles, Lot, Profile. The post-login landing stays Vehicles (M1 #7) | Reuses the existing guards with no new group. Keeping Vehicles as the landing respects a standing user decision | Low. Reordering tabs is one line | §4.2 |
| M2B-38 | **Web nav.** `DriverNav` gains a "Lot" pill linking to the home. The admin landing gets one link: "Simulator console" | Signed-in drivers otherwise have no route back to the live view. The console is M2, so the M1 "no links to M4 features" rule doesn't apply | Low | §4.1 |
| M2B-39 | **CORS and CSP.** Compose `CORS_ALLOWED_ORIGINS` default becomes `http://localhost:${WEB_PORT:-3000},http://127.0.0.1:${WEB_PORT:-3000}` (mirroring `WEB_ALLOWED_ORIGINS`); `web` gets `API_PUBLIC_URL`. No `connect-src` in M2: the CSP is built at build time in `next.config.ts`, while the API origin is a runtime value. M5's per-request CSP (in `proxy.ts`) adds `connect-src 'self' <API_PUBLIC_URL origin>` | An origin missing from CORS makes `EventSource` fail silently. The CORS line is in the `api` service block, so the PR notes it for Track A | Low | §5.3 |
| M2B-40 | **Tokens.** New status tokens (§9.1) in all three token files, every pair added to `check:tokens`. On navy, status colors appear only as text on their tint backgrounds, never directly on navy | AA (FR-BRAND-4). Green on navy measures 2.13:1 | Low | §9 |
| M2B-41 | **Tests stay at the fetch and transport boundary, no MSW** (CC-14 continues). Web: a `FakeEventSource` test double. Mobile: a stubbed `fetchImpl` returning a controllable `ReadableStream`. Jitter takes an injected `random` | Same reasons as CC-14; the stream needs a hand-driven double anyway | Low | §12 |
| M2B-42 | **"< 2 s" on the client is evidenced by a frame-to-screen test on each client** (push one frame, assert the new number in the same `act`, no timers advanced) plus a demo step. The server half is already measured by `TestAvailability_PassToSSELatency` | The client adds only parse and render time; a test that needs no timer proves it adds no deliberate delay. Real end-to-end timing waits for Playwright in M5 | Low | §12.2 |
| M2B-43 | **Slices:** contracts first, then web, then mobile, then the console, on `feat/m2-clients`, each ≤ 400 changed lines (§13) | User order; mobile before console keeps the client pattern work together | Low. The console only depends on contracts and W1 | §13 |
| M2B-44 | **Console follows the simulator's implicit idle session.** Inject while nothing runs is allowed: it starts an implicit idle session (speed 1, real clock), so status then reports `running: true`, scenario `idle`. A later scenario Start takes that session over and keeps its cars (held cars stay held); a Start after an idle run the admin explicitly started gets 409 `simulator.already_running`. Status cannot tell implicit from explicit idle, so the console derives no rules it cannot know: Start is enabled unless a non-idle scenario is running, Inject is always enabled (except while draining), Stop, Leave and Fault follow `running`, and any 409/422 from the API is shown as a translated banner. No contract change | Matches the approved runner behavior (`Runner.Inject`, `activeOrImplicit`, `takeover`). Cost if wrong: an admin sees an occasional 409 banner | Low | §7.2 |
| M2B-45 | **Draining is client memory.** Status has no drain field. The console sets a `draining` flag when "Stop after cars leave" returns 202 and clears it when status reports not running. On reload the flag is lost and the panel shows "running" until the drain finishes. While the flag is set, Start, Inject, Fault and "Stop after cars leave" are disabled and a plain "Stop now" stays enabled (it escalates the drain into a hard stop). No contract change | The simulator returns 202 at once and drains in the background (`StopAsync`); a second drain stop gets 409 `simulator.not_running`, only a hard stop escalates. A lost flag on reload is acceptable for a basic M2 console. Cost if wrong: an admin sees a 409 banner | Low | §7.2 |

**Decision count:** 43 (M2B-1 to M2B-43; 6 user or controller, 37 ours).

---

## 3. Constraints and known conflicts

### 3.1 Constraints

| Area | Constraint (source) |
|---|---|
| Contract | The SSE and control contract is frozen after handoff; changes need both developers' approval (policies §2.2). Track B adds no endpoint and changes no schema |
| Stream wire | `retry: 3000`; first event `availability` on every connect; `id` = version; ≤ 1 push per 250 ms; `heartbeat` every 15 s; `spaces` only when the whole query string is `include=spaces`; `Last-Event-ID` ignored; caps checked before the stream: 429 `rate_limit.exceeded` + `Retry-After: 10` (backend §11.3, §11.5, D9) |
| Control API | Served by the simulator on `:8090`, not the API. `X-Simulator-Token` header. Strict bodies: unknown field → 422 `unknown_field`. `leave` and `fault` return 409 `simulator.not_running` when nothing runs; `cars` starts an implicit idle session instead (M2B-44); `leave` returns `202 {data: {}}` (P2) |
| Web | Next.js 16.4.5, React 19.3.0, next-intl 4.14.9, TanStack Query 5.106.1, zustand 5.1.0, zod 4.6.7, react-hook-form 7.91 |
| Mobile | Expo SDK 58, React Native 0.88.1, expo-router 58, Uniwind 1.13.2; no netinfo, Reanimated, map or SSE library (CC-19) |
| Dependencies | **None added.** |
| Process | ≤ 400 changed lines per PR, conventional commits, no AI attribution, docs updated in the same PR (policies §2.1, §2.2, §2.10). `lib/bff`, `lib/simulator` and `proxy.ts` are sensitive paths: security checklist plus an automated risk review |

### 3.2 Contradictions found (resolved here or flagged)

| # | Conflict | Resolution |
|---|---|---|
| Y1 | **"EventSource retries on its own."** It does only after a network drop; after a 429 or 404 it closes for good | M2B-11 |
| Y2 | **"Direct to Go gives each browser its own IP."** Through a Docker Desktop published port, the API probably sees the bridge gateway (inside `TRUSTED_PROXY_CIDRS`), and browsers send no `X-Forwarded-For`. Locally, every tab and the phone may share one `sse_connect_ip` bucket (30/min) and one 20-stream cap | M2B-20 limits churn; the builder logs which IP the API sees (§14). Phase 2's Caddy fixes it. Risk R1 |
| Y3 | **Architecture says "the browser never calls the Go API directly"** while §5 and backend §11.5 send `EventSource` straight to Go | The public SSE stream is the one documented exception; REST stays behind the BFF (M2B-18). The docs slice states it in one place |
| Y4 | **M1 #4 public home** (name, hours, location, actions) | Superseded by M2B-1; the actions stay |
| Y5 | **M2C-12 says Track B uses MSW mocks**, CC-14 says no MSW | CC-14 wins (M2B-41) |
| Y6 | **Spec FR-AVAIL-6 is in M2 scope** | Hidden until M3 (M2B-4), consistent with backend §19 |
| Y7 | **`seed` is int64**, JavaScript numbers are not | M2B-33 |
| Y8 | **CORS default lists only `http://localhost:3000`**, but the web is also reached at `127.0.0.1:3000` | M2B-39 |

---

## 4. Screens

States legend: **L** loading, **E** empty, **Err** error, **S** stale, **U** live updates unavailable.

### 4.1 Web (`apps/web/src/app/[locale]/…`)

| Screen | Route file | Data source | States |
|---|---|---|---|
| Public home | `(public)/page.tsx` → `features/home/PublicHome` + `features/lot/LotLive` | RSC: `getLots()`, then lot, availability, spaces, tariff in parallel (`allSettled`); `API_PUBLIC_URL`; client island streams | **L**: none on first paint (RSC data); skeleton only if the client re-discovers. **Err**: lot panel with retry, actions still shown. **S**, **U** as §4.3 |
| Driver nav | `(driver)/layout.tsx` → `DriverNav` | — | New "Lot" pill to `/` (M2B-38) |
| Admin landing | `(admin)/admin/page.tsx` | `requireAdmin()` | Adds a "Simulator console" link |
| Simulator console | `(admin)/admin/simulator/page.tsx` → `features/simulator/SimulatorConsole` | `/api/admin/simulator/*` (M2B-27); RSC passes `API_PUBLIC_URL` and the lot snapshot for the compact live block | Not configured, unreachable, idle, running, draining (§4.4) |

Home layout (top to bottom): navy hero with the parking sign, wordmark "MyPark · Banreservas Zona Colonial" and, on its right on wide screens, the **count card** (soft white panel on navy, as the M1 auth panel); then Log in and Sign up (signed out) or a Vehicles link (signed in, CC-24); then on the light surface: **lot map**, **lot info**, **rates**. On narrow screens the count card stacks under the title and the hero keeps the faint bay lines from the M1 `.lot` panel.

### 4.2 Mobile (`apps/mobile/src/app/…`)

| Screen | Route file | Data source | States |
|---|---|---|---|
| Welcome (pre-login lot) | `(auth)/welcome.tsx` (rewrite) | `['lots']`, lot, tariff, availability, spaces queries; stream | Header and actions always render; lot section **L** skeleton, **Err** retry panel, **S**, **U** |
| Lot tab | `(tabs)/lot/_layout.tsx` (Stack), `(tabs)/lot/index.tsx` | Same `LotLive` | Same |
| Tabs | `(tabs)/_layout.tsx` | — | Three tabs: Vehicles, Lot, Profile; landing stays Vehicles (M2B-37) |

Welcome layout: `brand-primary-strong` header band with parking sign, wordmark and the language switch; the count card overlapping the band's bottom edge; Log in and Sign up buttons right under the count card (so they are reachable without scrolling on a small phone); then map, lot info and rates in a `ScreenScroll`. The Lot tab uses the same component without the actions. Pull-to-refresh is not offered: the stream is the refresh.

### 4.3 The live block (both clients)

| Part | Content | Notes |
|---|---|---|
| Count card | Big `available`; "of {effective_capacity} spaces"; "{n} out of service" when relevant; status pill | `closed` replaces the number with "Closed" (M2B-24) |
| Status pill | Icon + text: **Open** (circle), **Full** (filled square), **Closed** (bar) | Text on the status tint; never color alone |
| Freshness line | Live: dot + "Live · last change hh:mm". Stale: clock icon + "Stale — last updated hh:mm" + "Reconnecting…". Unavailable: "Live updates unavailable" + last snapshot time | Times per M2B-14 |
| Map | 280 cells, legend, note "The map shows sensor readings." | Shown when the first `spaces` full list or the REST spaces snapshot is present; until then a static skeleton grid |
| Lot info | Name, address, "Open in Maps", hours | M2B-25 |
| Rates | Six lines, money per M2B-26 | Reservation fee hidden |

State matrix:

| Connection | Data present | Shown |
|---|---|---|
| `connecting` (first time) | Snapshot from RSC/REST | Values, freshness line "Connecting…" (no stale label yet) |
| `connecting` | No snapshot | Skeleton card and grid |
| `live` | Yes | Values, "Live" |
| `stale` (error or 45 s) | Yes | Last values, stale label, reconnecting |
| `stale` | No (first connect failed and REST failed) | Error panel: "Availability is not loading right now" + Retry |
| `unavailable` (web: no `API_PUBLIC_URL`) | Snapshot | Values, "Live updates unavailable" |
| Any | `lot.not_found` | Re-discover once (M2B-7); then the error panel |

Copy never shows a raw code, `message`, or English in es-DO.

### 4.4 Simulator console (web, admin)

Two columns on wide screens (controls left, compact live block and log right), one column on narrow.

| Panel | Content | States |
|---|---|---|
| Status | Scenario name (translated), running/idle badge, seed (`rawSeed`, copy button), speed ×N, virtual time, cars inside, started at, "Lot reached full" when `reached_full` | Not configured (M2B-29), unreachable ("Simulator is not responding", retrying every 2 s), idle, running, draining (running after a drain stop) |
| Start | Scenario select (M2B-34), speed 1–120 (default 30), seed (optional), start time (optional, local `America/Santo_Domingo`, sent as RFC 3339 with `-04:00`) | Enabled unless a non-idle scenario is running or the console is draining (M2B-44, M2B-45); 409 `simulator.already_running`, 422 translated |
| Stop | "Stop" and "Stop after cars leave" (drain) | Disabled while not running; while draining only "Stop now" is enabled (M2B-45); 409 `simulator.not_running` |
| Inject car | Credential (ticket / employee card), card number when employee card (`EMP-001`), "Hold inside until released", dwell minutes (1–600, optional) | Always enabled except while draining (starts an implicit idle session when nothing runs, M2B-44); success adds the car id to Held cars when held; 422 translated |
| Held cars | Car ids with "Leave" and a "lost ticket" checkbox each; a free-text car id field for anything else | Disabled while idle; 404 `simulator.car_not_found` removes the row with a banner |
| Sensor fault | Space code (`E001`–`E280`), fault (stuck occupied, stuck free, silent, clear) | Disabled while idle; 204 → "Fault set on E017" status; 422 `known_space` field error |
| Event log | Table: time, device, type, result, reason, car; newest first, last 500 | Empty: "No events yet. Start a scenario." Gap row (M2B-31) |

Destructive confirmations are not needed: every action is reversible within the simulation (CC-22 still bans modals).

---

## 5. Live stream

### 5.1 Flow

```
Web:     RSC (API_INTERNAL_URL) ── snapshot ──► initialData ─┐
         EventSource(API_PUBLIC_URL …/stream?include=spaces) ─┼─► reduceLive ─► Query cache ─► LotLive
         BFF GET lots/:id/{availability,spaces} (fallback) ───┘        │
                                                                      └─► connection store ─► freshness line, announcer
Mobile:  expo/fetch REST (lots, lot, tariff, availability, spaces) ──┐
         expo/fetch stream → TextDecoder → createSseParser ──────────┴─► reduceLive ─► Query cache ─► LotLive
```

### 5.2 Transports

| | Web | Mobile |
|---|---|---|
| Open | `new EventSource(streamUrl(origin, lotId))`, no credentials | `fetch(streamUrl(…), {headers: {Accept: 'text/event-stream'}, signal})` from `expo/fetch` |
| Parse | Native; listeners for `availability`, `spaces`, `heartbeat` | `createSseParser` over decoded chunks |
| Payload check | Each `data` goes through its zod schema; a failed parse drops the frame and counts as fresh (logged in dev) | Same |
| HTTP errors | Invisible; `readyState === CLOSED` after `error` (M2B-11) | Status and `Retry-After` readable (M2B-21) |
| Close | `close()` | `abort()` |
| Lifecycle | Ref-counted manager; `pagehide` closes, `pageshow` reopens | Ref-counted manager; `AppState` background aborts, active reopens |

### 5.3 Web configuration

| Item | Value |
|---|---|
| `API_PUBLIC_URL` | New `web` env in compose: `${API_PUBLIC_URL:-http://localhost:${API_PORT:-8080}}`. Read by `lib/env/public-api.ts` in Server Components only; validated with `new URL`, protocol `http:` or `https:`, origin only |
| CORS | `api` service `CORS_ALLOWED_ORIGINS` default adds `http://127.0.0.1:${WEB_PORT:-3000}` (M2B-39) |
| CSP | Unchanged in M2; M5 adds `connect-src` per request |
| Phone | Not relevant: mobile uses `EXPO_PUBLIC_API_URL` and needs no CORS |

### 5.4 Reconnect policy (both clients)

| Event | Action |
|---|---|
| Connection opens and the first frame arrives | State `live`; attempt counter resets |
| Network drop (web `error` with `CONNECTING`; mobile reader error or end of stream) | Close; state `stale`; reconnect after `nextDelay(attempt)` |
| HTTP failure (web `error` with `CLOSED`; mobile non-200) | Close; state `stale`; REST snapshot now (web via BFF); next delay floored at 10 s, or `Retry-After` on mobile when larger |
| `lot.not_found` (mobile status, or the web REST snapshot) | Stop; re-discover once (M2B-7) |
| 45 s with no frame | State `stale`; tear down; reconnect after `nextDelay(attempt)` |
| While `stale` | One REST snapshot per attempt, at most every 10 s; a successful snapshot updates values and `lastFrameAt` but the state stays `stale` until the stream is back |

`nextDelay(attempt, random, floorMs = 0) = max(floorMs, min(30 000, 1 000 × 2^attempt) × (0.5 + 0.5 × random()))`. That gives roughly 0.5–1 s, 1–2 s, 2–4 s, … up to 15–30 s.

### 5.5 Reducer rules (`reduceLive`)

| Frame | Rule |
|---|---|
| New connection opened | `spacesReady = false`; keep the last spaces for display |
| `availability` | Ignore if `version ≤ lastVersion`; else replace and set `lastVersion` |
| `spaces` `full: true` | Replace the whole map; `spacesReady = true` |
| `spaces` `full: false` | Ignore unless `spacesReady`; else set each listed code; unknown codes ignored |
| `heartbeat` | No data change |
| Any frame | `lastFrameAt = now`; restart the 45 s timer |

The REST snapshot path writes availability through the same version guard; REST spaces replace the map only while `spacesReady` is false.

### 5.6 Where the code lives

| Module | Location |
|---|---|
| `createSseParser`, `streamUrl` | `packages/contracts/src/sse/` |
| `reduceLive`, `nextDelay`, `FRESH_WINDOW_MS` | `packages/contracts/src/m2/live.ts` |
| Web manager, `useLiveLot(lotId, origin)` | `apps/web/src/features/lot/live/` |
| Mobile `openStream`, manager, `useLiveLot(lotId)` | `apps/mobile/src/services/sse.ts`, `apps/mobile/src/features/lot/live/` |

---

## 6. Data flow and REST

### 6.1 Web

- **Hero first:** the lot section sits in `<Suspense>` with the skeleton, so the hero, Log in and Sign up render even while a hung API runs out its timeouts (M2B-17).
- **First paint:** `(public)/page.tsx` calls `lib/api/server.ts` for `GET /v1/lots`, then `lot`, `availability`, `spaces` and `tariff` in parallel. The results become `initialData` for the Query keys of M2B-19; `initialDataUpdatedAt` is the request time.
- **Client:** `LotLive` (client component) gets `lotId`, `apiOrigin` and the snapshots as props. Presentational parts (`CountCard`, `StatusPill`, `FreshnessLine`, `SpaceMap`, `LotInfo`, `RatesList`) are props-only and live in `features/lot/components/` (container/presentational, architecture §12).
- **BFF:** `lib/bff/allowlist.ts` gains `GET lots/:id/availability` and `GET lots/:id/spaces`, both `auth: public`.
- **Lot gone:** a `lot.not_found` from the BFF calls `router.refresh()` once; if the RSC still finds nothing, the error panel shows.

### 6.2 Mobile

- `features/lot/api.ts`: `getLots`, `getLot`, `getAvailability`, `getSpaces`, `getTariff` through the M1 `http` client and `parseEnvelope`, each with its zod schema.
- `useLotId()` resolves `['lots']` (first item). Then the four queries run in parallel; the stream starts once `lotId` is known.
- The stream's first frame usually wins the race with REST availability; the version guard makes the order irrelevant.

---

## 7. Simulator console

### 7.1 Layers

```
browser ──(cookies, X-CSRF-Token)──► proxy.ts (refresh) ──► /api/admin/simulator/[...path]
                                                              ├─ allow-list (M2B-28)
                                                              ├─ GET /v1/me with bearer → role admin? (M2B-27)
                                                              ├─ CSRF + Origin on POST (CC-3)
                                                              └─ fetch SIMULATOR_INTERNAL_URL + X-Simulator-Token (5 s)
```

### 7.2 Route table

| Method | Client path | Upstream | Body check in BFF |
|---|---|---|---|
| GET | `status` | `/control/status` | — |
| POST | `scenarios/start` | `/control/scenarios/start` | JSON object, ≤ 4 KB; forwarded as is (server is strict) |
| POST | `scenarios/stop` | `/control/scenarios/stop` | Same |
| POST | `cars` | `/control/cars` | Same |
| POST | `cars/:carId/leave` | `/control/cars/{car_id}/leave` | `carId` matches `^car-\d{6}$` |
| POST | `sensors/:code/fault` | `/control/sensors/{code}/fault` | `code` matches `^E\d{3}$` |
| GET | `events` | `/control/events?after=` | Only `after`, integer ≥ 0 |

Responses pass through status, `content-type`, `retry-after` and `x-request-id`, with `Cache-Control: no-store`. The token is never echoed. No streaming, no `PATCH` or `DELETE`.

### 7.3 Polling and log

- `['simulator', 'status']` and `['simulator', 'events']` poll every 2 s while visible (M2B-30). Mutations invalidate status on success.
- The events query uses the cursor of M2B-31 and appends into the console store; the table renders from the store, newest first, capped at 500.
- Reasons are translated where possible (M2B-35). Denials (`result` contains `deny`) get a "Denied" text badge in addition to color.
- The log region is not a live region (it updates every 2 s); a visually hidden status says "{n} new events" at most every 10 s.

### 7.4 Forms

react-hook-form with the contracts request schemas (M1 #19); the start form uses an app-side form schema for the seed string (M2B-33). Server field errors map through `validation.<rule>` (including `known_space`, `pattern`, `integer`, `excluded_unless`, `unknown_field`); other codes go to a banner through `errors.<code>`. The seed is read as a string (M2B-33). Speed is a number field with step 1, not a slider (exact values matter for the demo).

---

## 8. Shared code (`packages/contracts`)

| Adds | Contents |
|---|---|
| `src/m2/lot.ts` | `lotSummarySchema`, `lotSchema` |
| `src/m2/availability.ts` | `availabilitySchema` (REST and SSE), `heartbeatSchema` |
| `src/m2/spaces.ts` | `lotSpacesSchema`, `spacesEventSchema`, `spaceStateSchema` |
| `src/m2/tariff.ts` | `tariffSchema` |
| `src/m2/simulator.ts` | `simulatorStatusSchema` (numeric `seed`, as OpenAPI; exact digits per M2B-33), `startScenarioSchema`, `stopScenarioSchema`, `injectCarSchema` (card only with `employee_card`), `leaveCarSchema`, `sensorFaultSchema`, `simulatorEventSchema`, `CONSOLE_SCENARIOS` |
| `src/m2/live.ts` | `reduceLive`, `nextDelay`, `FRESH_WINDOW_MS`, `LiveState` type |
| `src/sse/parser.ts`, `src/sse/url.ts` | `createSseParser`, `streamUrl` |
| `src/fixtures/m2.ts` | Lot, availability (open, full, closed, reduced capacity), 280-space list, deltas, tariff, simulator status (idle, running, draining), events (allow, deny, gap); each parsed by its schema |
| `src/m2.schemas.types.test.ts` | `expectTypeOf` per schema against `components['schemas']` |

`ValidationRule` already has the M2 rules; nothing to add there.

---

## 9. Design system

Same rules as M1 §9 (impeccable product register, soft UI, one action color, no side-stripes, motion only for state). The count is the one large number on the page: Barlow 700, about 4.5 rem on web and 64 sp on mobile, tabular figures, no count-up animation.

### 9.1 New tokens (all `TODO(brand)`, three files, parity by `check:tokens`)

| Token | Value | Use |
|---|---|---|
| `status-open` | `#1e7a46` | Open text and icon; free-cell outline |
| `status-open-tint` | `#e7f4ec` | Open pill background |
| `status-full` | `#b3261e` | Full text and icon (same value as `danger`, separate meaning) |
| `status-full-tint` | `#fbeae9` | Full pill background |
| `status-closed` | `#4f5d76` | Closed text; occupied-cell fill |
| `status-closed-tint` | `#eceff4` | Closed pill background |
| `status-stale` | `#8a5300` | Stale text and icon |
| `status-stale-tint` | `#fdf1dc` | Stale line background |

### 9.2 Contrast (computed for this design)

| Pair | Ratio | Rule |
|---|---|---|
| `status-open` on `status-open-tint` / surface / sunken | 4.72 / 5.35 / 4.85 | Pass |
| `status-full` on `status-full-tint` / surface | 5.62 / 6.54 | Pass |
| `status-closed` on `status-closed-tint` / surface / sunken | 5.77 / 6.65 / 6.03 | Pass |
| `status-stale` on `status-stale-tint` / surface / sunken | 5.66 / 6.33 / 5.74 | Pass |
| White on `status-open` / `status-closed` | 5.35 / 6.65 | Pass (cell glyphs) |
| `status-open` directly on `brand-primary-strong` | 2.13 | **Never** — use the tint pill (M2B-40) |

All cell outlines and fills are ≥ 3:1 against `surface` (1.4.11).

### 9.3 Space cells

| State | Shape | Fill | Mark |
|---|---|---|---|
| Free | Rounded square, 2 px outline | `surface`, outline `status-open` | none |
| Occupied | Rounded square | Solid `status-closed` | none |
| Out of service | Rounded square, 1 px outline `ink-muted` | Diagonal hatch | "×" |

Web cells are 24–28 px with a 4 px gap; mobile cells fill `(width − gutters) / 10`. The grid never scrolls horizontally at 320 px (WCAG 1.4.10). A one-line legend repeats each shape with text. When `spacesReady` is false after a reconnect, the grid stays visible at 60 % opacity with "Updating map…".

---

## 10. Internationalization

| Namespace | App | Example keys |
|---|---|---|
| `lot` | both | `title`, `address`, `openInMaps`, `opensInNewTab`, `hours.alwaysOpen`, `hours.varies`, `error.title`, `error.retry` |
| `availability` | both | `of`, `outOfService`, `status.open`, `status.full`, `status.closed`, `live`, `lastChange`, `stale`, `reconnecting`, `connecting`, `unavailable`, `announce.count`, `announce.status` |
| `spaces` | both | `legend.free`, `legend.occupied`, `legend.outOfService`, `summary`, `note`, `updating`, `listFree` |
| `tariff` | both | `title`, `firstHour`, `additionalHour`, `dailyCap`, `grace`, `postPayment`, `lostTicket` |
| `money` | both | `dop` (fallback, M2B-26) |
| `nav` | web | `lot` |
| `simulator` | web | `title`, `status.*`, `scenario.idle`, `scenario.weekday`, `scenario.weekend_peak`, `scenario.fill_to_full`, `scenario.sensor_drift`, `start.*`, `stop.*`, `inject.*`, `held.*`, `fault.*`, `fault.kind.*`, `log.*`, `notConfigured`, `unreachable` |
| `tabs` | mobile | `lot` |

`errors.simulator.*`, `errors.lot.*` and `errors.gate.*` already exist in both apps. Copy examples:

| Key | es-DO | en |
|---|---|---|
| `availability.of` | de {total} espacios | of {total} spaces |
| `availability.stale` | Desactualizado — última actualización {time} | Stale — last updated {time} |
| `spaces.summary` | Los sensores reportan {free} libres, {occupied} ocupados y {oos} fuera de servicio. | Sensors report {free} free, {occupied} occupied and {oos} out of service. |
| `spaces.note` | El mapa muestra lo que leen los sensores. | The map shows sensor readings. |
| `simulator.notConfigured` | El simulador no está configurado en este servidor. | The simulator is not configured on this server. |

Times use `America/Santo_Domingo` (FR-I18N-4); numbers use the active locale.

---

## 11. Accessibility

| Requirement | Web (WCAG 2.2 AA) | Mobile |
|---|---|---|
| Live updates | Count is plain text; one `role="status"` announcer: status changes at once, count at most every 30 s (M2B-22) | Same rule via `announceForAccessibility` |
| Map | Grid container `aria-hidden="true"`; a visible summary sentence; a `<details>` "List free spaces" with the codes as text | Grid `accessibilityElementsHidden` + `importantForAccessibility="no-hide-descendants"`; one summary `AppText`; "List free spaces" expands a text list |
| Color alone | Pill icon + text; cell shapes and marks (§9.3); denials labelled in the log | Same |
| Freshness | Stale is text + icon, not a color change | Same |
| Keyboard | All console controls operable; log table has headers and a caption | Hardware keyboard focus order |
| Targets | Controls ≥ 44 px; cells are not interactive, so they are exempt | ≥ 48 dp |
| External link | Visible "opens in a new tab" text | `accessibilityRole="link"`, hint "Opens Maps" |
| Motion | No count animation; the stale fade honors `prefers-reduced-motion` | Reduce motion honored |
| Zoom and type | 200 % zoom, rem sizes; grid reflows to 10 columns | Font scale respected; count capped at 1.5× so it fits |
| Screen-reader pass | NVDA on home and console per slice (policies §2.9) | TalkBack on Welcome and the Lot tab |

---

## 12. Testing

### 12.1 Automated

| Layer | Tooling | Required tests |
|---|---|---|
| Contracts | vitest | `expectTypeOf` per schema; fixtures parse; `createSseParser`: split chunks, CRLF, CR, comments, `retry`, multi-line `data`, no trailing blank line; `streamUrl` is exactly `?include=spaces`; `reduceLive`: version guard, deltas before full ignored, full replaces, heartbeat no-op; `nextDelay` bounds with `random` 0 and 1, floor wins |
| Web, stream | vitest + `FakeEventSource` + fake timers | *45 s silence → stale and reconnect*; *error while CONNECTING → backoff from 1 s*; *error while CLOSED → 10 s floor and one BFF snapshot*; *snapshot 404 → refresh*; *two subscribers share one connection*; *last unsubscribe closes after 2 s*; *no `withCredentials`* |
| Web, screens | Testing Library + fixtures | Home: signed out, signed in, API down (actions still render), closed, full, reduced capacity; map legend and summary; stale label time; announcer throttling; money format |
| Web, BFF | vitest on handlers | Public lot GETs allowed; console: unknown path 404; bad `carId`/`code` 404; extra query dropped; driver 403; no session 401; POST without CSRF 403; token header set upstream and absent from the response; 204 passthrough; upstream down 502; missing config 404 |
| Web, console | Testing Library | Start form validation and 409; held car added and left; fault 204 status text; inject enabled while idle (implicit session); draining flag disables the controls and keeps Stop now; log cursor kept across `started_at` changes and reset after a failed poll recovers; gap row; a seed above 2^53 round-trips exactly (display and start body) |
| Mobile, stream | jest-expo, injected `fetchImpl` with a controllable `ReadableStream` | Chunked frames parsed; 429 waits `Retry-After`; 404 re-discovers; background aborts and foreground reconnects; 45 s stale |
| Mobile, screens | RN Testing Library | Welcome renders actions with the API down; Lot tab; map hidden from accessibility, summary present; stale label |
| Scripts | `node --test` | `check:tokens` new pairs; `check:i18n` new keys |

### 12.2 Frame-to-screen ("< 2 s") evidence (M2B-42)

On each client, one named test: render `LotLive` with a connected fake transport, push an `availability` frame with a higher version and a new `available`, and assert the new number is on screen inside the same `act`, with **no timer advanced**. A second test pushes a `spaces` delta and asserts the cell's state label changes. Together with `TestAvailability_PassToSSELatency` (server p95 < 2 s) this covers the exit criterion until Playwright measures it end to end in M5. The demo script adds a visible check.

### 12.3 Exit criteria

| Exit criterion (spec §8 M2) | Evidence |
|---|---|
| `weekday` runs and web and mobile update in < 2 s | §12.2 tests, server latency test, demo steps 3–5 |
| Demo: start `weekday` from the console, count and map move live; pay and exit a car | Demo steps 2–8 |

### 12.4 Demo script (outline; written in full as `docs/m2-demo-script.md` in the docs slice)

| # | Step | Expect |
|---|---|---|
| 1 | Open the web home signed out and the phone Welcome | Count, status, map, lot info, rates in both languages |
| 2 | Log in as admin, open Simulator console, start `weekday` at speed 30 | Count and map move on both screens within 2 s; log fills |
| 3 | Straight away, inject one **held** car (works while idle too: it starts an implicit idle session, M2B-44) | Car id listed under Held cars; its entry shows in the log |
| 4 | Toggle `stuck_occupied` on a free space | Cell changes; count does not (M2B-15) |
| 5 | Stop the API container for 50 s | Both clients show stale with the last time; restart → live again |
| 6 | About 12 **real** minutes after step 3, leave the held car | It pays RD$50 and exits; count rises. Business time follows the server clock (M2C-13), so leaving earlier exits at RD$0 inside the 10 min grace |
| 7 | Start `fill_to_full` | Status Full; log shows `gate.lot_full` denials |
| 8 | Phone: background the app 1 min, return | Fresh values, no stale label after reconnect |

---

## 13. Build order

Branch `feat/m2-clients`. Each slice is ≤ 400 changed lines (excluding generated files and lockfiles), chained if pushed as PRs, with fresh-context review. Slices touching `lib/bff`, `lib/simulator` or `proxy.ts` get the security checklist and an automated risk review.

| # | Slice | Contents | Approx. lines | Depends on |
|---|---|---|---|---|
| C1 | Contracts: schemas | `m2/` schemas, fixtures, type tests | 350 | — |
| C2 | Contracts: live rules | `sse/` parser and URL, `m2/live.ts`, tests | 300 | C1 |
| W1 | Web foundations | Status tokens in three files + `check:tokens` pairs; `lot`/`availability`/`spaces`/`tariff`/`money` keys (both apps' files for shared namespaces); `formatMoney`; `API_PUBLIC_URL` reader; compose (`API_PUBLIC_URL`, CORS default); BFF public GET entries | 380 | C1 |
| W2 | Web stream | Connection manager, `useLiveLot`, connection store, Query wiring, fallback, tests | 400 | C2, W1 |
| W3 | Web screens | Home rewrite, `LotLive` and presentational parts, announcer, `DriverNav` link, frame-to-screen tests | 400 | W2 |
| M1 | Mobile stream | `services/sse.ts`, manager, `features/lot/api.ts`, `useLotId`, `useLiveLot`, AppState handling, tests | 400 | C2, W1 |
| M2 | Mobile screens | Welcome rewrite, Lot tab, `LotLive` parts, tab label, frame-to-screen tests | 400 | M1 |
| S1 | Console BFF | Route, `lib/simulator/*`, role check, matcher, compose check, tests | 380 | C1 |
| S2 | Console UI 1 | Page, admin landing link, status panel, start and stop forms, `simulator` keys, store | 400 | S1, W3 |
| S3 | Console UI 2 | Inject, held cars, leave, fault, event log, cursor rules, tests | 400 | S2 |
| D1 | Docs + demo | Items below; `docs/m2-demo-script.md`; spec §8 exit boxes after the demo runs | 200 | all |

### Docs to update (policies §2.10)

| Document | Change | Slice |
|---|---|---|
| `architecture.md` §5, §12 | The SSE exception to "browser never calls Go"; `API_PUBLIC_URL`; console route; connection manager | W2, S1 |
| `architecture.md` §13 | Mobile tabs in M2: Vehicles, Lot, Profile; Welcome shows the lot | M2 |
| `deploy/compose/docker-compose.yml` | `web.API_PUBLIC_URL`; `api.CORS_ALLOWED_ORIGINS` default (note for Track A) | W1 |
| `spec.md` §8 M2 | Tick the Track B parts after the demo | D1 |
| `spec.md` §10.2 | Add "M2 client calls M2B-7…M2B-43 accepted" | D1 |
| `docs/m2-demo-script.md` | New | D1 |

---

## 14. Left to the builder (verify, with fallbacks)

| Check | Fallback |
|---|---|
| `expo/fetch` streams `response.body` incrementally on Android in Expo Go, and `abort()` ends the read | REST polling every 5 s with the same state machine (`connecting` never becomes `live`; label "Updates every few seconds") |
| `TextDecoder` with `{stream: true}` exists on Hermes (RN 0.88) | All stream payloads are ASCII JSON; a byte-to-char decoder is enough |
| Hermes `Intl.DateTimeFormat` with `timeZone: 'America/Santo_Domingo'` on Android | Fixed −04:00 offset (the Dominican Republic has no DST), noted in a comment |
| `Intl.NumberFormat('en', {currency: 'DOP'})` output | Locale key `money.dop` (M2B-26) |
| Which client IP the API logs for browser and phone SSE locally (Y2) | Document the shared bucket in the demo script; open fewer tabs |
| `pagehide`/`pageshow` handling with Next's client router | Close on `visibilitychange` hidden after 60 s instead |
| Exact component props, file splits, wording beyond the examples, skeleton shapes | Within the conventions above |

---

## 15. Changes against the spec and architecture

| Source | Says | This design | Status |
|---|---|---|---|
| FR-AVAIL-6 | Show upcoming reservations in 2 h | Hidden until M3 (M2B-4) | Record in spec §10.2 |
| FR-AVAIL-5 | "stale — last updated hh:mm" | The time is the last frame received, not `updated_at` (M2B-14) | Clarification |
| M1 decision 2.1 #4 | Public home without availability | Home shows availability (M2B-1) | Superseded by user decision |
| Architecture §12 | Browser never calls Go directly | Except the public SSE stream (M2B-2, Y3) | Doc fix in W2 |
| Architecture §13 | Five tabs | Three in M2 (M2B-37) | Doc fix in M2 |
| M2C-12 | MSW mocks | Fetch and transport doubles (M2B-41) | As CC-14 |
| FR-SIM-5 "present reservation QR" | Console control | Absent in M2 (backend M2C-1) | Already recorded |

---

## 16. Risks

| # | Risk | Impact | Mitigation |
|---|---|---|---|
| R1 | Locally all clients share one SSE IP bucket (Y2) | Reconnect storms hit 429 during the demo | One connection per lot per app (M2B-20), jittered backoff with a 10 s floor, demo note |
| R2 | `expo/fetch` streaming on Hermes behaves differently from the docs | Mobile never goes live | Builder check first in M1; REST polling fallback (§14) |
| R3 | The API origin differs between the browser and the server (LAN demo, Phase 2 domains) | Web stream fails while RSC works | `API_PUBLIC_URL` is separate from `API_INTERNAL_URL`; the "unavailable" state is visible, not silent |
| R4 | Map and count disagree on screen | Viewers think it's a bug | M2B-15 note under the map; demo step 4 shows it on purpose |
| R5 | Role check per console request adds load | Two polls every 2 s → about 60 `GET /v1/me` per minute per open console (that route has no rate-limit policy) | Acceptable for one admin; a short role cache keyed by a hash of the access cookie is the later option |
| R6 | Mobile slices land before the console | The full demo is not runnable until S3 | Console depends only on C1 and W3; can be pulled ahead if the schedule needs it |
| R7 | The CORS default change edits the `api` service | Cross-track surprise | Called out in the W1 PR; Track A acknowledges |
