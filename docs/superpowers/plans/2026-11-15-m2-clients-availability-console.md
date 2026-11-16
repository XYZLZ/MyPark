# M2 Track B — Web and Mobile Clients: Live Availability, Lot Info, Simulator Console Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show the live lot count, status, sensor map, lot info and rates to anyone on web and mobile, in the approved Banreservas reference style. Add an admin-only web Simulator console that runs the M2 demo.

**Architecture:**
- `packages/contracts` gains strict M2 schemas, typed fixtures and two pure modules:
  - `sse/`: the frame parser and the stream URL builder.
  - `m2/live.ts`: the reducer, backoff, freshness and announcement rules.
  Both clients run the same rules.
- Each client reads the public SSE stream straight from Go:
  - web through `EventSource`;
  - mobile through the `expo/fetch` streaming body and the shared parser.
- On each client, a ref-counted manager per `(origin, lotId)`:
  - owns reconnection;
  - writes data into the TanStack Query cache;
  - writes connection state into a small external store.
- The console calls the simulator only through `/api/admin/simulator/[...path]`. That route checks the allow-list, the admin role per request and CSRF, then adds `X-Simulator-Token` on the server.

**Tech Stack:** Next.js 16.4.5, React 19.3.0, TypeScript 6.0.3, Tailwind 4.3, next-intl 4.14.9, TanStack Query 5.106.1, zustand 5.1.0, zod 4.6.7, react-hook-form 7.91.0, vitest 5. Expo SDK 58, expo-router 58, React Native 0.88.1, Uniwind 1.13.2, i18next, jest-expo with RN Testing Library 14.

**Spec:** `docs/superpowers/specs/2026-11-15-m2-clients-availability-console-design.md` (cited below as "design §N" and "M2B-n"). The approved mockup `docs/superpowers/specs/2026-11-15-m2-clients-mockup.html` is the visual reference for exact tokens, layout and copy. Executors keep both open. This plan points at their tables instead of copying them. Also binding: `docs/spec.md`, `docs/architecture.md`, `docs/policies.md`, `apps/web/AGENTS.md`, `apps/mobile/AGENTS.md`.

## Global Constraints

- **Skills.** Before working on a task, load the skills that match it. The repo docs (design, spec, architecture, policies, AGENTS.md) override any skill default.
  - Web: `.claude/skills/next-best-practices/SKILL.md`, `.claude/skills/react-best-practices/SKILL.md`, `.claude/skills/composition-patterns/SKILL.md`, `.claude/skills/tailwind-css-patterns/SKILL.md`.
  - Mobile: `.claude/skills/uniwind/SKILL.md` and, under `C:\Users\benja\.claude\plugins\cache\claude-plugins-official\expo\1.13.9\skills\`, `expo-router/SKILL.md`, `expo-native-ui/SKILL.md` and `expo-data-fetching/SKILL.md`.
  - Both: `.claude/skills/accessibility/SKILL.md`.
  - Contracts and forms: `.claude/skills/zod/SKILL.md`, `.claude/skills/typescript-advanced-types/SKILL.md`.
  - Every task: `superpowers:test-driven-development`.
  - Docs (Task 16): `C:\Users\benja\.claude\skills\cognitive-doc-design\SKILL.md`.
- **Versions.** Pinned as in the Tech Stack. Before touching a Next.js API, read the matching guide in `node_modules/next/dist/docs/`, especially `01-app/01-getting-started/15-route-handlers.md` and `01-app/02-guides/environment-variables.md` (`apps/web/AGENTS.md`). Expo docs are pinned to `https://docs.expo.dev/versions/v58.0.0/` (`apps/mobile/AGENTS.md`).
- **Packages.** npm workspaces only, never pnpm or yarn. On this Windows repo `npm install <pkg>` fails with ERESOLVE, so edit `package.json`, then run `npm install` at the root. `(cd apps/mobile && npx expo install --check)` must stay clean.
- **Dependencies.** None added (design §3.1). No icon, SVG, map, SSE, netinfo or animation library.
  - Web icons are inline SVG in TSX.
  - Mobile icons and map shapes are drawn with `View`s.
  - `react-native-safe-area-context` is already a direct dependency.
- **React.** Single copy across the workspace: `npm run check:react`.
- **Tokens.** Same names and values in `apps/web/src/design/tokens.css`, `apps/mobile/src/global.css` and `apps/mobile/src/design/tokens.ts`, enforced by `npm run check:tokens`.
  - Every new color carries `TODO(brand)`.
  - `brand-orange` is decorative only: never text, never a state cell.
  - On navy, status colors appear only as text on their tints (M2B-40).
- **Copy.** Every user-facing string is a key in `es-DO` (the source) and `en`, with key parity (`npm run check:i18n`).
  - Placeholders: next-intl uses `{param}`, i18next uses `{{param}}`.
  - No hardcoded strings, including `aria-label` and `accessibilityLabel`.
  - Times always use `America/Santo_Domingo` and numbers the active locale. Copy never shows a raw code, `message` or English in es-DO (design §4.3).
- **i18n namespaces (plan decision, extends existing names).**
  - Web navigation keys go in the existing `common.nav` namespace, not a new top-level `nav`: `common.nav.home`, `vehicle`, `profile`, `admin`, `simulator`, `logout`, `menu`.
  - Mobile tab keys go in the existing `nav` namespace, not a new `tabs` one: `nav.home`, `nav.vehicles`, `nav.profile`.
  - The shared namespaces `lot`, `availability`, `spaces`, `tariff` and `money` have identical key sets in both apps (Task 6a adds the check).
- **Errors.** Every response goes through `parseEnvelope`, `parseListEnvelope` or `parseNoContent`. Show `errors.<code>` or `validation.<rule>`, falling back to `errors.fallback`. Never show `ApiError.message`.
- **Web state.**
  - Server data lives in RSC props or TanStack Query.
  - Connection state lives in a per-lot external store read with `useSyncExternalStore` (M2B-19).
  - zustand holds client UI only, one store per provider (the `store/ui.ts` pattern).
  - Containers live in `features/`; props-only presentational parts live in `features/<f>/components/` or `components/ui/`.
- **Components.** Explicit variants, not boolean mode props. `ref` is a regular prop, and context is read with `use()`.
- **Mobile.**
  - Routes only in `src/app/`, one screen per file, rendering a container from `src/features/`. No tests or helpers in `src/app/`.
  - Never build Uniwind class names dynamically.
  - Controls are ≥ 48 dp.
  - Every screen scrolls through `ScreenScroll`, and the 16 dp gutter is `SCREEN_GUTTER` (M2B-52).
- **Tests.**
  - TDD, test first.
  - Stub at the fetch or transport boundary with typed fixtures from `@mypark/contracts/fixtures`. No MSW (M2B-41).
  - No sleep-based assertions: use fake timers, an injected `now` and an injected `random`.
  - Web tests that touch `NextRequest` or route handlers start with `// @vitest-environment node`.
  - Text with es-DO times is compared after normalizing whitespace (`a. m.` uses narrow no-break spaces).
- **Commands.** Run from the repo root in Git Bash on Windows and in Linux CI. Quote paths that contain `(` or `[`.
- **Commits.** Conventional Commits with scopes `web|mobile|contracts|deploy|docs`. **No Co-Authored-By and no AI attribution of any kind**, in commits and PR descriptions alike.
- **PR size.** Each task is one PR-sized slice of ≤ 400 changed lines, chained on `feat/m2-clients`. Generated files and `package-lock.json` are excluded from the count.
- **Sensitive tasks.** Tasks 6b (`lib/bff/allowlist.ts`) and 13 (`lib/simulator/*`, `proxy.ts`) need, in their PR:
  - the "Security checklist" from `.github/pull_request_template.md`, completed;
  - an automated risk review (`review-risk` or `/security-review`), linked (policies §2.2, design §3.1).
- **Accessibility pass.** Each UI task ends with a keyboard + NVDA pass on web or a TalkBack pass on the demo phone (policies §2.9, design §11).

## Review Focus

1. **Malformed stream payloads.** Inputs: non-JSON `data`, `version` sent as `"121"`, an unknown space code `E999` in a delta, an unknown event name, a chunk that ends mid-line or between `\r` and `\n`. Expected: the frame is dropped, it still counts as fresh, nothing throws, and the count does not change. Tests:
   - Task 2 *decodeFrame drops malformed data* and *reduceLive ignores unknown codes in a delta*;
   - Task 10 *a malformed frame keeps the stream live*.
2. **A mobile 429 with no usable wait time.** Inputs: `Retry-After: soon`, an HTTP date, `-5` or a missing header, and an HTML body from a proxy. Expected: the state is stale, and the next attempt waits at least the 10 s floor, never `NaN` or 0. Tests:
   - Task 2 *parseRetryAfter rejects anything but non-negative integer seconds*;
   - Task 10 *a 429 with Retry-After soon waits the 10 s floor*.
3. **A bad `API_PUBLIC_URL`.** Inputs: `javascript:alert(1)`, `ftp://api.local`, an empty string, `localhost:8080` without a scheme, `http://user:pw@api:8080`. Expected: the home renders the server snapshot with "Live updates unavailable", and no `EventSource` is created. Tests:
   - Task 6b *readPublicApiOrigin rejects non-http and credentialed values*;
   - Task 7 *a null origin never opens a connection*.
4. **Crafted console paths and queries.** Inputs: `events?after=-1`, `after=1e3`, `after=1&after=2`, `after=99999999999999999999`, `cars/car-000001%2F..%2Fstatus/leave`, `cars/CAR-000001/leave`, `sensors/E1000/fault`, `status?x=1`. Expected:
   - an unknown path or a bad `after` gets `404 http.not_found` with no upstream call;
   - an unknown query parameter is dropped and never forwarded.
   Test: Task 13 *rejects crafted ids and after values without calling upstream*.
5. **A machine time zone that is not Santo Domingo** (CI in UTC, a phone set to Madrid). Expected: `2026-11-15T14:47:00Z` renders as `10:47 a. m.` in es-DO and `10:47 AM` in en, for "Last change" and "Stale — last updated" alike. Tests:
   - Task 6b *formatClock ignores the machine time zone*;
   - Task 11 *formatClock ignores the machine time zone*.
   Both are run with `TZ=Europe/Madrid`.

---

## File Structure

```
packages/contracts/
├─ package.json                                   # T1 "./fixtures" -> src/fixtures/index.ts
└─ src/
   ├─ m2/{lot,availability,spaces,tariff,simulator}.ts       # T1 strict schemas
   ├─ m2/live.ts + live.test.ts                    # T2 reducer, backoff, freshness, figures, announcements, keys
   ├─ sse/{parser,url}.ts + tests                  # T2
   ├─ fixtures/{index,m2}.ts                       # T1 (index re-exports m1 and m2)
   ├─ m2/schemas.test.ts, m2.schemas.types.test.ts # T1
   └─ index.ts                                     # T1, T2 re-exports
scripts/
├─ check-design-tokens.mjs + .test.mjs           # T3 new tokens and pairs
├─ check-i18n-parity.mjs + .test.mjs             # T6a shared-namespace parity
└─ check-mobile-layout.mjs + .test.mjs           # T5 check:layout
apps/web/src/
├─ design/tokens.css  components/ui/Button.tsx   # T3
├─ components/shell/{AppShell,Sidebar,TopBar,NavIcon}.tsx, components/shell/nav-items.ts   # T4
├─ app/[locale]/{(public),(driver),(admin)}/layout.tsx       # T4 shell
├─ lib/env/public-api.ts  lib/format/{money,time}.ts         # T6b
├─ lib/bff/allowlist.ts                          # T6b two public GETs (sensitive)
├─ lib/api/server.ts                             # T8 lot getters
├─ features/lot/
│  ├─ api.ts                                     # T7 BFF snapshot
│  ├─ live/{connection-store,manager,useLiveLot}.ts          # T7
│  ├─ server.ts  LotLive.tsx  LotSection.tsx     # T8
│  └─ components/{HomeHero,StatTiles,CountCard,StatusPill,FreshnessLine,LotInfo,RatesList,LotErrorPanel,LotSkeleton,Announcer}.tsx   # T8
│     components/{SpaceMap,SpaceCell,MapLegend,SpacesSummary,FreeSpacesList,ZoomControls}.tsx   # T9
├─ features/home/PublicHome.tsx                  # T8 rewrite
├─ lib/simulator/{allowlist,upstream,handler}.ts # T13 (sensitive)
├─ app/api/admin/simulator/[...path]/route.ts    # T13
├─ proxy.ts                                      # T13 matcher (sensitive)
├─ app/[locale]/(admin)/admin/simulator/page.tsx # T14
└─ features/simulator/
   ├─ api.ts  seed.ts  controls.ts  store.ts  queries.ts   # T14, T15
   ├─ SimulatorConsole.tsx  StatusPanel.tsx  StartForm.tsx  StopControls.tsx   # T14
   └─ InjectForm.tsx  HeldCars.tsx  FaultForm.tsx  EventLog.tsx  log-cursor.ts   # T15
apps/mobile/src/
├─ design/{tokens.ts,layout.ts}  global.css      # T3, T5
├─ components/ui/{Button,ScreenScroll,WavyHeader,TabIcon,TabLabel}.tsx   # T3, T5
├─ testing/{render.tsx,layout.tsx}               # T5
├─ lib/format/{money,time}.ts                    # T11
├─ services/sse.ts                               # T10
├─ features/lot/
│  ├─ api.ts  live/{connection-store,manager,useLotId,useLiveLot}.ts     # T10
│  ├─ useLotData.ts  useAnnouncer.ts  LotLive.tsx  LotHomeScreen.tsx  components/{CountCard,StatusPill,FreshnessLine,LotInfo,RatesList,LotErrorPanel,LotSkeleton,HeroPlaceholder}.tsx  # T11
│  └─ LotMapScreen.tsx  map-layout.ts  components/{SpaceMap,SpaceCell,MapLegend,SpacesSummary,MapPreviewCard}.tsx   # T12
├─ features/auth/WelcomeScreen.tsx               # T11 rewrite
├─ features/session/root-route.ts                # T12 landing
└─ app/(tabs)/_layout.tsx, app/(tabs)/home/{_layout,index,map}.tsx, app/(auth)/map.tsx   # T11, T12
deploy/compose/docker-compose.yml                # T6b
.github/workflows/ci-js.yml                      # T5 check:layout step
docs/{architecture,spec}.md, docs/m2-demo-script.md, docs/superpowers/specs/2026-11-08-m1-clients-auth-vehicles-design.md   # T4, T7, T12, T13, T16
```

**Slicing.** The 17 tasks (1–5, 6a, 6b, 7–16) follow the design §13 build order: C1, C2, V1, V2, W1–W4, M1–M3, S1–S3, D1. Two slices are split:
- **V2** becomes Tasks 4 (web) and 5 (mobile). The web shell and the mobile shell are independent deliverables, and together they exceed 400 lines once the mobile layout tests are counted. A reviewer can reject one and approve the other.
- **W1** becomes Tasks 6a (copy) and 6b (env, formatting, compose, BFF). The reason is given in Task 6a.

**Plan decisions** (the design leaves these open; Task 16 records them in spec §10.2):
1. **Mobile Inicio route.** It is `(tabs)/home/` with its own Stack, `index.tsx` and `map.tsx`. The design's `(tabs)/index.tsx` would collide with `app/index.tsx` on `/`. `(auth)/map.tsx` serves the pre-login full map.
2. **Admin sidebar items.** Inicio, Administración, Simulador, Cerrar sesión. Mi Vehículo and Perfil are left out for admins, because `requireDriver` redirects admins to `/admin` (OC-16, CC-24).
3. **Shared fixtures entry.** `./fixtures` points at a new `src/fixtures/index.ts` that re-exports `m1` and `m2`. Existing imports keep working.
4. **Shared pure rules in contracts.** Stat figures, space counts, announcement throttling and the Query keys all live in `m2/live.ts`, beside the reducer (M2B-10). Both clients render the same numbers and announce on the same schedule.
5. **No-session console response.** A console request with no access cookie gets `401 auth.unauthenticated`. With only a refresh cookie it gets `503 internal.unexpected`, the same as the M1 BFF (a refresh that failed transiently).
6. **Draining fixture.** There is no "draining" status fixture. Draining is client memory (M2B-45), and status reports `running: true` while it lasts.
7. **When held cars clear.** M2B-32 says "cleared on a new run". The console clears them when status goes from running to not running instead, because a Start that takes over an implicit idle session keeps its cars (M2B-44).

---

## Task 1: Contracts — M2 schemas and fixtures (slice C1, about 350 lines)

**Files:**
- Create: `packages/contracts/src/m2/{lot.ts,availability.ts,spaces.ts,tariff.ts,simulator.ts,schemas.test.ts}`, `packages/contracts/src/m2.schemas.types.test.ts`, `packages/contracts/src/fixtures/{m2.ts,index.ts}`
- Modify: `packages/contracts/src/index.ts` (re-export `./m2/*`), `packages/contracts/package.json` (`"./fixtures": "./src/fixtures/index.ts"`)

**Interfaces:**
- Consumes: `components` from `./openapi`, `parseEnvelope`, `successEnvelope`, and the M1 fixtures helpers `successBody`, `listBody`, `errorBody`, `validationBody`.
- Produces (from `@mypark/contracts`). Every object schema is strict (`z.strictObject`).
  - `lotSummarySchema`, `lotSchema`; types `LotSummary`, `Lot`, `LotStatus = 'open' | 'full' | 'closed'`.
  - `availabilitySchema`, `heartbeatSchema`; types `Availability`, `Heartbeat`.
  - `spaceStateSchema`, `lotSpacesSchema`, `spacesEventSchema`; types `SpaceState`, `SpaceStatus = 'free' | 'occupied' | 'out_of_service'`, `LotSpaces`, `SpacesEvent`.
  - `tariffSchema`; type `Tariff`.
  - Scenario lists:
    - `SIMULATOR_SCENARIOS`: the seven names, `as const`.
    - `CONSOLE_SCENARIOS = ['idle', 'weekday', 'weekend_peak', 'fill_to_full', 'sensor_drift'] as const`, type `ConsoleScenario` (M2B-34).
  - `CAR_ID_PATTERN = /^car-\d{6}$/`, `SPACE_CODE_PATTERN = /^E\d{3}$/` (M2B-28; shared by the console route and the forms).
  - Simulator response schemas:
    - `simulatorStatusSchema`: `scenario` allows `''`, `started_at` is optional and `seed` is numeric (M2B-33).
    - `simulatorEventSchema`, `injectCarResultSchema` (`{car_id}`), `emptyDataSchema = z.strictObject({})` (the `leave` 202 body).
  - Request schemas, with issue messages `rule` or `rule:param`:
    - `startScenarioSchema`: `scenario` ∈ `CONSOLE_SCENARIOS` (`oneof`); `speed` is an integer 1–120 (`min:1`, `max:120`, `integer`); `seed?` is a non-negative integer; `start_at?` is an RFC 3339 string with an offset.
    - `stopScenarioSchema`: `{drain}`.
    - `injectCarSchema`: `card` is required when `credential = 'employee_card'` (`required`), is trimmed to 1–32 characters, and is removed from the output for `ticket`; `dwell_minutes?` is an integer 1–600.
    - `leaveCarSchema`: `{lose_ticket}`.
    - `sensorFaultSchema`: `{fault}`.
  - Types: `SimulatorStatus`, `SimulatorEvent`, `StartScenarioValues`, `InjectCarValues`, `SensorFaultValues`.
  - `@mypark/contracts/fixtures` (all parsed by their schema at module load). Values are the mockup's:
    - Lot: `lotSummaryFixture` and `lotFixture` (`name` "Parqueo Banreservas Zona Colonial", `address` "Calle Arzobispo Meriño No. 386, Zona Colonial, Santo Domingo", `capacity` 280, `opening_hours.always_open` true, `map_url` "https://www.google.com/maps/search/?api=1&query=18.4730,-69.8840").
    - Availability:
      - `availabilityReducedFixture`: `available` 211, `occupied` 66, `effective_capacity` 277, `capacity` 280, `status` open, `version` 120, `updated_at` "2026-11-15T14:47:00Z" (10:47 in Santo Domingo).
      - `availabilityOpenFixture`: effective 280, available 214.
      - `availabilityFullFixture`: available 0 of 277, status full, version 121.
      - `availabilityClosedFixture`: status closed.
    - Spaces:
      - `spacesFixture`: 280 codes `E001`–`E280` in order. `E001`–`E066` occupied, `E278`–`E280` out of service, the rest free.
      - `spacesFullEventFixture`: `{full: true, spaces: spacesFixture.spaces}`.
      - `spacesDeltaFixture`: `{full: false, spaces: [{code: 'E100', state: 'occupied'}]}`.
    - `tariffFixture`: `first_hour_cents` 5000, `additional_hour_cents` 2500, `daily_cap_cents` 40000, `lost_ticket_cents` 50000, `exit_grace_minutes` 10, `post_payment_exit_minutes` 15, `reservation_fee_cents` 0, `currency` "DOP".
    - Simulator:
      - `simulatorStatusIdleFixture`: scenario `''`, `running` false.
      - `simulatorStatusRunningFixture`: `weekday`, seed 42, speed 30, `cars_inside` 66, `started_at` set.
      - `simulatorEventsFixture`: seq 1841 `open`; seq 1845 `deny` `gate.credential_in_use` with `car-000213`; seq 1846 `deny` `gate.lot_full` with `car-000214`.
    - `sseText(event: 'availability' | 'spaces' | 'heartbeat', data: unknown, id?: number): string` returns the wire text `event: …\n[id: …\n]data: <json>\n\n`.

- [ ] **Step 1: Write the failing tests**

```ts
// m2/schemas.test.ts
test('lotSchema parses the fixture and rejects an extra key')       // {...lotFixture, extra: 1} fails
test('availabilitySchema rejects a string version')                // {...availabilityReducedFixture, version: '121'} fails
test('spaceStateSchema rejects an unknown state')                  // state 'parked' fails
test('simulatorStatusSchema accepts the empty scenario without started_at')
test('startScenarioSchema bounds speed')                           // 0 -> 'min:1'; 121 -> 'max:120'; 2.5 -> 'integer'
test('startScenarioSchema offers only console scenarios')          // 'reservation_arrivals' -> issue 'oneof'
test('injectCarSchema requires a card for employee_card')          // path ['card'], message 'required'
test('injectCarSchema drops the card for a ticket')                // output has no 'card' key
test('injectCarSchema bounds dwell minutes')                       // 0 -> 'min:1'; 601 -> 'max:600'
test('car id and space code patterns')                             // 'car-000123' ok; 'car-12', 'CAR-000123' no; 'E017' ok; 'E1000', 'e017' no
test('spacesFixture is E001..E280 in order with 211 free, 66 occupied, 3 out of service')
test('sseText writes event, id and data and ends with a blank line') // sseText('heartbeat', {as_of:'x'}) === 'event: heartbeat\ndata: {"as_of":"x"}\n\n'
// m2.schemas.types.test.ts
expectTypeOf<z.output<typeof lotSchema>>().toEqualTypeOf<Schemas['Lot']>()
// same for LotSummary, Availability, SpaceState, LotSpaces, Tariff, SpacesEvent, HeartbeatEvent,
// SimulatorStatus, SimulatorEvent, InjectCarResponse, StopScenarioRequest, LeaveCarRequest, SensorFaultRequest
expectTypeOf<z.output<typeof startScenarioSchema>>().toExtend<Schemas['StartScenarioRequest']>()
expectTypeOf<z.output<typeof injectCarSchema>>().toExtend<Schemas['InjectCarRequest']>()
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w packages/contracts -- src/m2`
Expected: FAIL (cannot resolve `./lot`, `./simulator`).

- [ ] **Step 3: Implement** the five `m2/` modules, `fixtures/m2.ts`, the `fixtures/index.ts` re-export of `./m1` and `./m2`, the `exports` change and the `index.ts` re-exports. `ValidationRule` already has every M2 rule (design §8), so `validation.ts` does not change.

- [ ] **Step 4: Run tests and typecheck**

Run: `npm test -w packages/contracts && npm run typecheck -w packages/contracts && npm test -w apps/web -- src/features/auth && npm test -w apps/mobile -- src/features/auth`
Expected: all pass. The type tests only bite under `typecheck`. The app runs prove that the `./fixtures` entry still resolves for M1 imports.

- [ ] **Step 5: Commit**

```bash
git add packages/contracts
git commit -m "feat(contracts): add m2 lot, availability, tariff and simulator schemas with fixtures"
```

---

## Task 2: Contracts — SSE parser, stream URL and live rules (slice C2, about 300 lines)

**Files:**
- Create: `packages/contracts/src/sse/{parser.ts,parser.test.ts,url.ts,url.test.ts}`, `packages/contracts/src/m2/{live.ts,live.test.ts}`
- Modify: `packages/contracts/src/index.ts`

**Interfaces:**
- Consumes: Task 1 schemas, types and fixtures.
- Produces (from `@mypark/contracts`):
  - Parser:
    - `type SseFrame = { event: string; id?: string; data: string }`.
    - `createSseParser(): SseParser`, where `type SseParser = { push(chunk: string): SseFrame[]; readonly retryMs: number | null }`.
    - WHATWG rules: field lines, a blank line dispatches, `event` defaults to `message`, an empty data buffer is not dispatched, `:` comments are ignored, `retry` is taken only when it is all digits, multi-line `data` joins with `\n`, CRLF, CR and LF all end a line (a CR at the end of one chunk followed by an LF at the start of the next is one line end), a leading BOM is dropped once, and a frame with no closing blank line is never emitted.
  - `streamUrl(origin: string, lotId: string): string` returns `${origin without trailing /}/v1/lots/${encodeURIComponent(lotId)}/availability/stream?include=spaces` and nothing else (M2B-9).
  - `m2/live.ts` constants: `FRESH_WINDOW_MS = 45_000`, `BACKOFF_BASE_MS = 1_000`, `BACKOFF_MAX_MS = 30_000`, `HTTP_FAILURE_FLOOR_MS = 10_000`, `SNAPSHOT_MIN_INTERVAL_MS = 10_000`, `IDLE_CLOSE_MS = 2_000`, `ANNOUNCE_COUNT_INTERVAL_MS = 30_000`.
  - `type ConnectionStatus = 'connecting' | 'live' | 'stale' | 'unavailable'`.
  - `type LiveState = { availability: Availability | null; spaces: SpaceState[] | null; spacesReady: boolean; lastVersion: number }`.
  - `type LiveFrame = { kind: 'open' } | { kind: 'availability'; data: Availability } | { kind: 'spaces'; data: SpacesEvent } | { kind: 'heartbeat' } | { kind: 'snapshot'; availability?: Availability; spaces?: SpaceState[] }`.
  - `initialLiveState(snapshot?: { availability?: Availability; spaces?: SpaceState[] }): LiveState`. `lastVersion` comes from the snapshot or is −1, and `spacesReady` is false.
  - `reduceLive(state: LiveState, frame: LiveFrame): LiveState`. It returns the **same reference** when nothing changed. The rules are design §5.5.
  - `decodeFrame(frame: SseFrame): LiveFrame | null`. It parses `data` with the schema for `availability`, `spaces` or `heartbeat`. Bad JSON, a failed schema or an unknown event gives `null`, and the managers still count that frame as fresh.
  - `nextDelay(attempt: number, random: () => number, floorMs = 0): number` follows the design §5.4 formula.
  - `parseRetryAfter(header: string | null): number | null` returns milliseconds for non-negative integer seconds and `null` otherwise.
  - `type AvailabilityFigures = { total: number; available: number; occupied: number; outOfService: number; effectiveCapacity: number }`.
  - `availabilityFigures(a: Availability): AvailabilityFigures`. `outOfService = capacity − effective_capacity`, so the map never feeds the figures (M2B-15).
  - `countSpaces(spaces: SpaceState[]): { free: number; occupied: number; outOfService: number }`.
  - Announcements:
    - `type AnnounceInput = { available: number; status: LotStatus; connection: ConnectionStatus }`.
    - `type AnnounceMemo` is opaque.
    - `planAnnouncement(memo: AnnounceMemo | null, input: AnnounceInput, now: number): { say: 'status' | 'connection' | 'count' | null; memo: AnnounceMemo }` (M2B-22).
  - `lotQueryKeys`: `all` = `['lots']`, `lot(id)` = `['lot', id]`, `availability(id)`, `spaces(id)` and `tariff(id)` (M2B-19). All `as const`.

- [ ] **Step 1: Write the failing tests**

```ts
// sse/parser.test.ts
test('parses one frame with event, id and data')            // 'event: availability\nid: 120\ndata: {"a":1}\n\n'
test('chunks split anywhere give the same frames')          // the fixture text fed one character at a time
test('CRLF and bare CR end lines; CR|LF across chunks is one line end') // ['data: x\r', '\n\r\n'] -> exactly 1 frame
test('comment lines are ignored')                           // ': ping\n\n' -> []
test('retry is taken only when all digits')                 // 'retry: 3000\n' -> retryMs 3000; 'retry: 3s\n' -> unchanged
test('multi-line data joins with a newline')                // 'data: a\ndata: b\n\n' -> data 'a\nb'
test('a frame without a closing blank line is not emitted')
test('event defaults to message and empty data is not dispatched')
// sse/url.test.ts
test('streamUrl is exactly include=spaces')                 // ('http://localhost:8080/', 'L1') -> 'http://localhost:8080/v1/lots/L1/availability/stream?include=spaces'
test('streamUrl encodes the lot id')                        // 'a/b' -> '/v1/lots/a%2Fb/'
// m2/live.test.ts
test('availability with a version at or below the last is ignored') // 120 then 120 and 119 -> same reference; 121 replaces
test('deltas before the first full list are ignored')
test('a full spaces list replaces the map and sets spacesReady')
test('a delta after full sets E100 to occupied')
test('reduceLive ignores unknown codes in a delta')          // E999 -> same reference
test('open resets spacesReady and keeps the last spaces')
test('heartbeat returns the same state')
test('snapshot availability passes the version guard; snapshot spaces apply only while not ready')
test('decodeFrame drops malformed data')                     // 'not json', version '121', event 'foo' -> null
test('nextDelay bounds')                                     // (0,()=>0) 500; (0,()=>1) 1000; (5,()=>1) 30000; (10,()=>0) 15000; (0,()=>0,10000) 10000
test('parseRetryAfter rejects anything but non-negative integer seconds') // '10' -> 10000; 'soon','-5','1.5','Wed, 21 Oct 2026 07:28:00 GMT', null -> null
test('availabilityFigures uses capacity minus effective capacity') // reduced fixture -> {total 280, available 211, occupied 66, outOfService 3, effectiveCapacity 277}
test('countSpaces counts the fixture')                       // {free 211, occupied 66, outOfService 3}
test('planAnnouncement: silent on mount, status at once, count at most every 30 s')
// first call -> null; open->full at t+1s -> 'status'; 211->210 at t+2s -> null; 210->209 at t+31s -> 'count';
// same count at t+70s -> null; live->stale -> 'connection'
test('lotQueryKeys builds the design keys')                  // availability('L1') -> ['lot','L1','availability']
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w packages/contracts -- src/sse src/m2/live.test.ts`
Expected: FAIL (modules missing).

- [ ] **Step 3: Implement** `sse/parser.ts`, `sse/url.ts` and `m2/live.ts`, then export them from `index.ts`. The parser keeps a line buffer and a pending-CR flag across `push` calls, so the result does not depend on where a chunk splits.

- [ ] **Step 4: Run tests and typecheck**

Run: `npm test -w packages/contracts && npm run typecheck -w packages/contracts`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add packages/contracts
git commit -m "feat(contracts): add sse parser, stream url and live availability rules"
```

---

## Task 3: Visual foundation — tokens, contrast pairs, navy buttons (slice V1, about 380 lines)

**Files:**
- Modify: `apps/web/src/design/tokens.css`, `apps/mobile/src/global.css`, `apps/mobile/src/design/tokens.ts`, `scripts/check-design-tokens.mjs`, `scripts/check-design-tokens.test.mjs`
- Modify: `apps/web/src/components/ui/Button.tsx`, `apps/web/src/components/ui/Button.test.tsx`, `apps/web/src/components/SiteHeader.tsx`, `apps/web/src/features/auth/link-class.ts`, `apps/web/src/features/vehicles/VehicleRow.tsx` (the focus ring's `--color-brand-primary` becomes `--color-brand-navy`)
- Modify: `apps/mobile/src/components/ui/Button.tsx`, `apps/mobile/src/components/ui/Button.test.tsx`, every `focus:border-brand-primary` under `apps/mobile/src/components/ui/` (becomes `focus:border-brand-navy`)
- Modify: `docs/superpowers/specs/2026-11-08-m1-clients-auth-vehicles-design.md` (one header note: buttons are navy from M2, M2B-47)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - 20 new tokens with the exact values in design §9.1 and §9.1b, in all three files. Each has a `TODO(brand)` comment.
    - Status: `status-open`, `status-open-tint`, `status-full`, `status-full-tint`, `status-closed`, `status-closed-tint`, `status-stale`, `status-stale-tint`.
    - Brand and map: `brand-navy`, `brand-navy-deep`, `brand-orange`, `brand-sky`, `brand-sky-strong`, `nav-active`, `on-navy-muted`, `space-occupied`, `space-occupied-tint`, `lot-concrete`, `lot-lawn`, `lot-tree`.
    - `tokens.ts` uses camelCase (`statusOpen`, `brandNavy`, `spaceOccupiedTint`, `lotConcrete`, …).
    - All are added to `REQUIRED_TOKENS`.
  - `MUST_PASS` additions, as `[fg, bg, min]`:
    - At 4.5:
      - `status-open` on `status-open-tint`, `surface`, `surface-sunken`;
      - `status-full` on `status-full-tint`, `surface`;
      - `status-closed` on `status-closed-tint`, `surface`, `surface-sunken`;
      - `status-stale` on `status-stale-tint`, `surface`, `surface-sunken`;
      - `surface` on `status-open`, `status-closed`;
      - `brand-primary-contrast` on `brand-navy`, `brand-navy-deep`, `nav-active`;
      - `brand-navy` on `brand-sky`, `brand-sky-strong`, `surface`;
      - `on-navy-muted` on `brand-navy`, `brand-navy-deep`.
    - At 3:
      - `space-occupied` on `lot-concrete`, `surface`, `space-occupied-tint`;
      - `status-open` on `lot-concrete`;
      - `ink-muted` on `lot-concrete`.
  - `MUST_FAIL` additions, as `[fg, bg, max]`, so these pairs can never become text:
    - `brand-orange` on `surface`, max 3;
    - `status-open` on `brand-primary-strong`, max 3;
    - `lot-tree` on `lot-lawn`, max 3.
  - Web `Button` variants:
    - `primary`: `bg-brand-navy text-brand-primary-contrast hover:bg-brand-navy-deep active:bg-brand-navy-deep`, ring `2px surface + 3px brand-navy`.
    - `primaryOnDark`: `bg-surface text-brand-navy hover:bg-brand-sky-strong`, with the 3 px surface ring.
    - The other variants keep their look and take the navy ring.
    - `ButtonVariant` is unchanged.
  - Mobile `Button`:
    - `primary`: `bg-brand-navy active:bg-brand-navy-deep focus:border-2 focus:border-brand-navy`, text `text-brand-primary-contrast`, spinner `tokens.brandPrimaryContrast`.
    - `ButtonVariant` is unchanged.
  - `brand-action*` tokens stay defined but no longer style buttons (M2B-47).

- [ ] **Step 1: Write the failing tests**

```js
// scripts/check-design-tokens.test.mjs additions
test('contrastRatio matches the design 9.1b values') // ('#ffffff','#0b3a75') ≈ 11.19; ('#c4610a','#e3e8ef') ≈ 3.37; ('#f39a1e','#ffffff') ≈ 2.22; ('#1e7a46','#163b63') ≈ 2.13
test('checkTokens reports a new token missing from global.css')   // drop --color-space-occupied from the mobile css text
test('checkTokens flags brand-orange if it ever passes as text')  // MUST_FAIL entry present
test('checkTokens passes on the real repo')                       // existing test, now with the new tokens
```

```ts
// apps/web/src/components/ui/Button.test.tsx: SIGNATURE.primary becomes 'bg-brand-navy'
test('primary is navy with a navy focus ring')            // classes contain 'bg-brand-navy' and 'var(--color-brand-navy)'; not 'bg-brand-action'
test('primaryOnDark is a white fill with navy text and the surface ring') // 'bg-surface', 'text-brand-navy', '0_0_0_3px_var(--color-surface)'
// apps/mobile/src/components/ui/Button.test.tsx
test('primary renders on navy')                           // the Pressable's className contains 'bg-brand-navy', not 'bg-brand-action'
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm run test:scripts; npm test -w apps/web -- src/components/ui/Button.test.tsx; npm test -w apps/mobile -- src/components/ui/Button.test.tsx`
Expected: FAIL. The new token is missing, and the buttons still carry `bg-brand-action`.

- [ ] **Step 3: Implement** the tokens, the pair lists and the two `Button` maps, then repoint the focus rings in the files listed. Update any M1 test that asserts `bg-brand-action` or `brand-primary` ring classes. Only class assertions change, no behavior (design §12.1 "M1 restyle").

- [ ] **Step 4: Verify**

Run: `npm run check:tokens && npm run test:scripts && npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web && npm test -w apps/mobile && npm run typecheck -w apps/mobile`
Expected: all pass, and `check:tokens` prints its success line.

- [ ] **Step 5: Accessibility pass.** On web, check with the keyboard that the navy ring is visible on the login and vehicles screens. On the phone, check with TalkBack that the login button's label is announced.

- [ ] **Step 6: Commit**

```bash
git add scripts apps/web apps/mobile docs/superpowers/specs/2026-11-08-m1-clients-auth-vehicles-design.md
git commit -m "feat(web): add m2 status and brand tokens and make buttons navy"
```

---

## Task 4: Web app shell — sidebar, top bar, menu (slice V2, web half, about 380 lines)

**Files:**
- Create: `apps/web/src/components/shell/{AppShell.tsx,Sidebar.tsx,TopBar.tsx,NavIcon.tsx,nav-items.ts,nav-items.test.ts,AppShell.test.tsx}`
- Modify: `apps/web/src/app/[locale]/(public)/layout.tsx`, `apps/web/src/app/[locale]/(driver)/layout.tsx`, `apps/web/src/app/[locale]/(driver)/layout.test.tsx`, `apps/web/src/app/[locale]/(admin)/layout.tsx`, `apps/web/src/app/[locale]/(admin)/layout.test.tsx`, `apps/web/src/features/profile/LogoutButtons.tsx`, `apps/web/src/i18n/messages/{es-DO,en}.json` (`common.nav.home|vehicle|admin|simulator|logout|menu`, `common.location`, `common.greeting`), `docs/superpowers/specs/2026-11-08-m1-clients-auth-vehicles-design.md` (header note: the shell changed, M2B-46)
- Delete: `apps/web/src/features/session/DriverNav.tsx` (replaced by the sidebar)

**Interfaces:**
- Consumes: Task 3 tokens; `Link` and `usePathname` from `@/i18n/navigation`; `LanguageSwitch`, `PersistedLanguageSwitch`, `ParkingSign`.
- Produces:
  - `type ShellVariant = 'guest' | 'driver' | 'admin'`.
  - `type NavKey = 'home' | 'vehicle' | 'profile' | 'admin' | 'simulator'`, `type NavItem = { key: NavKey; href: '/' | '/vehicles' | '/profile' | '/admin' | '/admin/simulator' }`.
  - `navItems(variant: ShellVariant): readonly NavItem[]`:
    - guest: `[home]`;
    - driver: `[home, vehicle, profile]`;
    - admin: `[home, admin, simulator]` (plan decision 2).
  - `currentHref(pathname: string, items: readonly NavItem[]): NavItem['href'] | null`. The longest prefix match wins, and `/` matches only itself.
  - `AppShell({ variant, greetingName, languageSwitch, sidebarFooter, children }: { variant: ShellVariant; greetingName?: string; languageSwitch: ReactNode; sidebarFooter: ReactNode; children: ReactNode })`:
    - It renders a skip link to `#main`, then the navy sidebar (`brand-navy` to `brand-navy-deep`) with the placeholder mark and `navItems`, then the top bar, then `<main id="main">`.
    - The current item uses `nav-active` and `aria-current="page"`.
    - The top bar shows `common.location` with a decorative `brand-orange` underline (`aria-hidden`), the language switch, and `common.greeting` when a name is given.
    - Below `lg`, the sidebar collapses behind a menu button (`aria-expanded`, `aria-controls`, label `common.nav.menu`).
    - Every item is ≥ 44 px.
  - `Sidebar` is the only client part, because it uses `usePathname` and the menu state.
  - `NavIcon({ name }: { name: NavKey | 'logout' })` is an inline SVG with `aria-hidden`.
  - `LogoutButtons({ scope, appearance = 'button' }: { scope: 'this' | 'both'; appearance?: 'button' | 'sidebar' })`. The `sidebar` appearance renders the "Cerrar sesión" item; the logic is unchanged.
  - Layouts:
    - `(public)` reads the session-cookie hint (CC-24): `variant` is `driver` with a signed-in hint, otherwise `guest`. `sidebarFooter` is Log in and Sign up for a guest and the sidebar logout for a signed-in hint.
    - `(driver)`: `variant='driver'` and `greetingName={gate.user.first_name}` when the gate is ready.
    - `(admin)`: `variant='admin'`.
    - The legal, suspended and unavailable bodies render inside the shell with the guest items only.

- [ ] **Step 1: Write the failing tests**

```ts
// nav-items.test.ts
test('navItems per variant')                       // guest ['home']; driver ['home','vehicle','profile']; admin ['home','admin','simulator']
test('currentHref picks the longest match')        // ('/admin/simulator', admin) -> '/admin/simulator'; ('/admin', admin) -> '/admin'
test('home is current only on /')                  // ('/vehicles/new', driver) -> '/vehicles'; ('/', driver) -> '/'
// AppShell.test.tsx (usePathname from testing/navigation)
test('starts with a skip link to main')
test('guest shows Inicio and the Log in and Sign up links')
test('driver marks Mi Vehículo as the current page')   // usePathname '/vehicles/new' -> aria-current='page' on 'Mi Vehículo'
test('admin shows Administración and Simulador, never Mi Vehículo')
test('the menu button toggles the sidebar')            // aria-expanded 'false' -> 'true' after click
test('the orange underline is hidden from assistive tech')
// (driver)/layout.test.tsx and (admin)/layout.test.tsx: replace the DriverNav/header assertions with the shell items
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/web -- src/components/shell "src/app/[locale]/(driver)" "src/app/[locale]/(admin)"`
Expected: FAIL (modules missing).

- [ ] **Step 3: Implement** the shell parts and wire the three layouts. Follow the mockup's sections 1a, 1b and 5 for the sidebar width, item spacing and top bar, using the tokens only. `SiteHeader` stays for `not-found`, `forbidden` and `error`.

- [ ] **Step 4: Verify**

Run: `npm run check:i18n && npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web && npm run build -w apps/web`
Expected: all pass.

- [ ] **Step 5: Accessibility pass.**
  - Keyboard + NVDA on the home, vehicles and admin pages: the skip link works, the current item is announced as the current page, and the menu button reports expanded or collapsed.
  - At 320 px and at 200 % zoom, there is no horizontal scroll.

- [ ] **Step 6: Commit**

```bash
git add apps/web docs/superpowers/specs/2026-11-08-m1-clients-auth-vehicles-design.md
git commit -m "feat(web): add the navy sidebar shell around public, driver and admin pages"
```

---

## Task 5: Mobile shell — gutter, safe areas, wavy header, tab bar, layout guard (slice V2, mobile half, about 400 lines)

**Files:**
- Create: `apps/mobile/src/design/layout.ts`, `apps/mobile/src/design/layout.test.ts`, `apps/mobile/src/components/ui/{WavyHeader.tsx,TabIcon.tsx}`, `apps/mobile/src/testing/layout.tsx`, `apps/mobile/src/features/screens.layout.test.tsx`, `scripts/check-mobile-layout.mjs`, `scripts/check-mobile-layout.test.mjs`
- Modify: `apps/mobile/src/components/ui/{ScreenScroll.tsx,TabLabel.tsx}`, `apps/mobile/src/app/(tabs)/_layout.tsx` (icons, the inset-aware bar style), `apps/mobile/src/app/(tabs)/{vehicles,profile}/_layout.tsx` (header `tokens.brandNavy`), the tab screens that render `ScreenScroll` (pass `edge="tabBar"`), `package.json` (`"check:layout": "node scripts/check-mobile-layout.mjs"`), `.github/workflows/ci-js.yml` (mobile job step `npm run check:layout` after `check:react`)

**Interfaces:**
- Consumes: Task 3 tokens; `useSafeAreaInsets` and `SafeAreaProvider` from `react-native-safe-area-context` (already a direct dependency, design §14).
- Produces:
  - `SCREEN_GUTTER = 16`, `LAYOUT_MIN_WIDTH = 360`, `MAX_FIXED_WIDTH = 328` (`360 − 2 × 16`) in `design/layout.ts`.
  - `screenPadding(insets: EdgeInsets, edge: 'screen' | 'tabBar'): { paddingLeft: number; paddingRight: number; paddingBottom: number }`:
    - left = `16 + insets.left`;
    - right = `16 + insets.right`;
    - bottom = `24 + (edge === 'screen' ? insets.bottom : 0)`.
  - `tabBarStyle(insets: EdgeInsets): ViewStyle`: `height: 64 + insets.bottom`, `paddingBottom: insets.bottom`, `paddingLeft: 16 + insets.left`, `paddingRight: 16 + insets.right`.
  - `ScreenScroll({ children, edge = 'screen' }: { children: ReactNode; edge?: 'screen' | 'tabBar' })`. It applies `screenPadding` as `contentContainerStyle`, with `testID="screen-scroll"`. It is never horizontal.
  - `WavyHeader({ children }: { children: ReactNode })`:
    - full-bleed `brand-navy` with a wave drawn by a rounded `View`;
    - `paddingTop = 16 + insets.top`;
    - content inside the gutter plus the side insets.
  - `TabIcon({ name, focused }: { name: 'home' | 'vehicles' | 'profile'; focused: boolean })`: `View`-drawn, `accessibilityElementsHidden`.
  - `TabLabel({ label, focused })`: the M1 pill is replaced by a 3 dp `brand-navy` underline when focused (M2B-46). Its props are unchanged.
  - `apps/mobile/src/testing/layout.tsx`:
    - `LAYOUT_SIZES = { small: { width: 360, height: 640 }, large: { width: 430, height: 932 } } as const`;
    - `DEVICE_INSETS = { top: 24, bottom: 34, left: 0, right: 0 }`;
    - `renderAtSize(ui: ReactElement, size: keyof typeof LAYOUT_SIZES, opts?: { language?: AppLanguage; queryClient?: QueryClient; insets?: EdgeInsets }): Promise<void>`. It wraps `renderScreen` in `SafeAreaProvider initialMetrics` and sets the window size;
    - `scrollPadding(): { left: number; right: number; bottom: number }` reads `screen-scroll`;
    - `countHorizontalScrolls(): number`.
  - `scripts/check-mobile-layout.mjs`:
    - exports `LAYOUT_ALLOW: readonly string[]` and `findLayoutViolations(files: Array<{ path: string; text: string }>, allow?: readonly string[]): string[]`;
    - the CLI walks `apps/mobile/src`, skipping `*.test.*` and `src/testing/`, and exits 1 on any violation.
    - Rules:
      - a `horizontal` prop on a scroll view or list;
      - a numeric `width`, `minWidth` or `maxWidth` style above 328;
      - `w-[Npx]`, `min-w-[Npx]` above 328, or `w-N` with `N × 4 > 328`;
      - a `src/features/**/*Screen.tsx` that does not import `ScreenScroll`.

- [ ] **Step 1: Write the failing tests**

```js
// scripts/check-mobile-layout.test.mjs
test('flags a horizontal ScrollView')                 // '<ScrollView horizontal>' and 'horizontal={true}'
test('flags fixed widths above 328')                  // 'width: 340', 'w-[340px]', 'w-84' flagged; 'w-80', 'width: 328' pass
test('flags a screen that bypasses ScreenScroll')     // path 'src/features/x/FooScreen.tsx' without the import
test('the allow-list exempts a file')
test('passes on the real repo')                       // findLayoutViolations(readRealFiles()) deep-equals []
```

```ts
// design/layout.test.ts
test('screenPadding adds side insets to the gutter, never replaces it') // ({left: 8, right: 0, top: 0, bottom: 34}, 'screen') -> {24, 16, 58}
test('a tab screen leaves the bottom inset to the tab bar')            // edge 'tabBar' -> paddingBottom 24
test('tabBarStyle pads the home indicator')                            // bottom 34 -> height 98, paddingBottom 34
// features/screens.layout.test.tsx (mock expo-router and @/services/runtime as each screen's own test does)
test.each(SCREENS × ['small', 'large'])('%s at %s keeps the 16 dp gutter and never scrolls sideways')
// SCREENS = LoginScreen, RegisterScreen, VerifyScreen, ForgotPasswordScreen, ResetPasswordScreen, VehiclesScreen, ProfileScreen
// scrollPadding() -> {left: 16, right: 16}; countHorizontalScrolls() === 0
test('side insets widen the gutter')                   // insets {left: 12, right: 12} -> {left: 28, right: 28}
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm run test:scripts; npm test -w apps/mobile -- src/design src/features/screens.layout.test.tsx`
Expected: FAIL (modules missing; `ScreenScroll` has no `testID`).

- [ ] **Step 3: Implement** the layout module, `ScreenScroll`, `WavyHeader`, `TabIcon`, `TabLabel` and the tab bar, then the script, the root script and the CI step. If `check:layout` finds an M1 screen that bypasses `ScreenScroll`, move that screen onto it. Add a path to `LAYOUT_ALLOW` only when the screen has no scrolling content, and give the reason in a comment.

- [ ] **Step 4: Verify**

Run: `npm run check:layout && npm run test:scripts && npm test -w apps/mobile && npm run lint -w apps/mobile && npm run typecheck -w apps/mobile && (cd apps/mobile && npx expo install --check)`
Expected: all pass.

- [ ] **Step 5: Device check (M2B-52).** On a 360 × 640 Android device or emulator and on a large phone, both at font scale 1.3, check Login, Vehicles and Profile:
  - no horizontal scroll;
  - equal 16 dp gutters;
  - nothing clipped;
  - the tab bar sits clear of the gesture bar.
  Record the result in the PR. Also do a TalkBack pass on the tab bar: each tab is announced with its label, and the icons are silent.

- [ ] **Step 6: Commit**

```bash
git add scripts package.json .github/workflows/ci-js.yml apps/mobile
git commit -m "feat(mobile): add screen gutter, safe-area tab bar, wavy header and layout guard"
```

---

## Task 6a: Shared lot copy and the shared-namespace parity check (slice W1, part 1, about 340 lines)

**Why W1 is split.** The design estimates W1 at 300 lines, but its copy alone is about 57 nested keys in four locale files, roughly 280 JSON lines. Add the env reader, the formatting, the compose lines, the BFF entries and their tests, and the slice is over 400. The split also leaves the sensitive `lib/bff` change in a small PR of its own (6b).

**Files:**
- Modify: `apps/web/src/i18n/messages/{es-DO,en}.json`, `apps/mobile/src/lib/i18n/locales/{es-DO,en}.json`, `scripts/{check-i18n-parity.mjs,check-i18n-parity.test.mjs}`

**Interfaces:**
- Consumes: the existing `LOCALE_DIRS`, `checkParity` and `checkCatalog`.
- Produces:
  - `SHARED_NAMESPACES = ['lot', 'availability', 'spaces', 'tariff', 'money']` and `checkSharedNamespaces(dirs?: typeof LOCALE_DIRS): string[]` in `check-i18n-parity.mjs`. The function reports any key in these namespaces that exists in one app's locale file but not the other's, and the CLI runs it after `checkCatalog()`.
  - New keys in both apps. Copy comes from design §10 and the mockup (web copy from mockup 1a and 2, mobile copy from 3 and 4):
    - `lot`: `title`, `info`, `address`, `location`, `openInMaps`, `opensInNewTab`, `hours.label`, `hours.alwaysOpen`, `hours.varies`, `hero.title`, `hero.subtitle`, `hero.photoAlt`, `error.title`, `error.retry`, `unavailable.closedBody`.
    - `availability`: `title`, `total`, `available`, `availableLong`, `occupied`, `of`, `outOfService`, `status.open`, `status.full`, `status.closed`, `live`, `lastChange`, `stale`, `reconnecting`, `connecting`, `unavailable`, `unavailableAt`, `announce.count`, `announce.open`, `announce.full`, `announce.closed`, `announce.live`, `announce.stale`, `announce.stale`.
    - `spaces`: `title`, `sensorsLive`, `legend.free`, `legend.occupied`, `legend.outOfService`, `summary`, `note`, `updating`, `listFree`, `viewFull`, `previewCount`, `zoomIn`, `zoomOut`.
    - `tariff`: `title`, `firstHour`, `additionalHour`, `dailyCap`, `grace`, `postPayment`, `lostTicket`.
    - `money`: `dop` ("RD${amount}" on web, "RD${{amount}}" on mobile).

- [ ] **Step 1: Write the failing tests**

```js
// check-i18n-parity.test.mjs additions
test('checkSharedNamespaces reports a key missing on mobile')  // web has spaces.zoomIn, mobile does not
test('checkSharedNamespaces ignores app-only namespaces')       // web-only common.nav.simulator is not reported
test('checkSharedNamespaces passes on the real repo')
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm run test:scripts`
Expected: FAIL (`checkSharedNamespaces` is not exported).

- [ ] **Step 3: Implement** the check, then add the keys to all four files with the copy in design §10 (es-DO is the source). `availability.stale` is "Desactualizado — última actualización {time}" on web and `{{time}}` on mobile. `availability.lastChange` keeps "Último cambio" (M2B-51).

- [ ] **Step 4: Verify**

Run: `npm run check:i18n && npm run test:scripts && npm test -w apps/web -- src/i18n && npm test -w apps/mobile -- src/lib/i18n`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add apps/web/src/i18n/messages apps/mobile/src/lib/i18n/locales scripts
git commit -m "feat(web): add shared lot, availability, spaces and tariff copy with parity check"
```

---

## Task 6b: Formatting, public API origin, compose, BFF lot GETs (slice W1, part 2, about 200 lines, sensitive path)

**Files:**
- Create: `apps/web/src/lib/env/{public-api.ts,public-api.test.ts}`, `apps/web/src/lib/format/{money.ts,time.ts,format.test.ts}`
- Modify: `apps/web/src/lib/bff/{allowlist.ts,allowlist.test.ts,handler.test.ts}`, `deploy/compose/docker-compose.yml`

**Interfaces:**
- Consumes: `Locale`, `BFF_ENTRIES`, `matchBffRoute`; Task 6a `money.dop`.
- Produces:
  - Two new `BFF_ENTRIES` (M2B-18), with `matchBffRoute` unchanged:
    - `{ method: 'GET', path: 'lots/:id/availability', auth: 'public' }`;
    - `{ method: 'GET', path: 'lots/:id/spaces', auth: 'public' }`.
  - `readPublicApiOrigin(value: string | undefined = process.env.API_PUBLIC_URL): string | null` (`server-only`, M2B-16):
    - It returns `new URL(value).origin` for `http:` or `https:`.
    - It returns `null` when the value is empty or not parseable, uses another protocol, or carries a username or password.
  - `formatMoney(cents: number, locale: Locale, fallback: (amount: string) => string): string` (M2B-26):
    - It formats `Intl.NumberFormat(locale, { style: 'currency', currency: 'DOP' })` on `cents / 100`.
    - When the output contains `DOP`, it returns `fallback(<the number with 2 decimals in the locale>)` instead. `fallback` comes from `money.dop`.
  - `formatClock(at: string | number, locale: Locale): string` uses `Intl.DateTimeFormat(locale, { hour: 'numeric', minute: '2-digit', timeZone: 'America/Santo_Domingo' })`.
  - Compose (M2B-39). The `api` line is a Track A file, so the PR notes it (R7).
    - `web.environment.API_PUBLIC_URL: ${API_PUBLIC_URL:-http://localhost:${API_PORT:-8080}}`.
    - `api.environment.CORS_ALLOWED_ORIGINS: ${CORS_ALLOWED_ORIGINS:-http://localhost:${WEB_PORT:-3000},http://127.0.0.1:${WEB_PORT:-3000}}`.

- [ ] **Step 1: Write the failing tests**

```ts
// allowlist.test.ts additions (UUID = '0b6f6f4e-8f2e-4c8e-9d63-5f1f0a1b2c3d')
test('public lot GETs are allowed without a bearer')   // GET ['lots', UUID, 'availability'] -> {upstreamPath: `/v1/lots/${UUID}/availability`, auth: 'public'}; same for 'spaces'
test('every other lot path stays denied')              // GET lots/UUID/tariff, POST lots/UUID/availability, GET lots/zona-colonial/availability,
                                                       // GET lots/UUID/availability/stream -> null
// handler.test.ts addition
test('a public lot GET forwards no Authorization even with a session') // bffRequest with mp_access -> upstream headers lack Authorization
// public-api.test.ts
test('readPublicApiOrigin keeps only the origin')      // 'http://localhost:8080' -> same; 'https://api.example.do/base/?x=1' -> 'https://api.example.do'
test('readPublicApiOrigin rejects non-http and credentialed values') // 'javascript:alert(1)', 'ftp://api.local', '', undefined, 'localhost:8080', 'http://user:pw@api:8080' -> null
// format.test.ts
test('formatMoney prints RD$ in both locales')         // (5000,'es-DO') and (5000,'en') -> 'RD$50.00'; (40000,'es-DO') -> 'RD$400.00'
test('formatClock ignores the machine time zone')      // ('2026-11-15T14:47:00Z','es-DO') -> '10:47 a. m.'; 'en' -> '10:47 AM' (whitespace normalized)
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/web -- src/lib/bff src/lib/env src/lib/format`
Expected: FAIL (entries missing, modules missing).

- [ ] **Step 3: Implement** the entries, the two `lib/format` modules, `lib/env/public-api.ts` and the compose lines.

- [ ] **Step 4: Verify**

Run: `TZ=Europe/Madrid npm test -w apps/web -- src/lib/format && npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web && docker compose -f deploy/compose/docker-compose.yml config | grep -E "API_PUBLIC_URL|CORS_ALLOWED_ORIGINS"`
Expected: all pass. The compose output shows `API_PUBLIC_URL: http://localhost:8080` and both CORS origins.

- [ ] **Step 5: Security checklist and risk review** (sensitive: `lib/bff`).
  - Complete the PR template's security checklist.
  - Run `review-risk` on the diff and link the result.
  - Confirm that both new entries are GET and `public`, and that an id that is not a UUID never reaches Go.

- [ ] **Step 6: Commit**

```bash
git add apps/web/src/lib deploy/compose/docker-compose.yml
git commit -m "feat(web): add money and time formatting, public api origin and lot bff entries"
```

---

## Task 7: Web stream — connection manager, store, `useLiveLot`, fallback (slice W2, about 400 lines)

**Files:**
- Create: `apps/web/src/features/lot/api.ts`, `apps/web/src/features/lot/live/{connection-store.ts,manager.ts,useLiveLot.ts,manager.test.ts,useLiveLot.test.tsx}`, `apps/web/src/testing/event-source.ts`
- Modify: `docs/architecture.md` §5 and §12 (the public SSE stream is the one exception to "the browser never calls Go"; `API_PUBLIC_URL`; the per-lot connection manager; REST fallback through the BFF)

**Interfaces:**
- Consumes: Task 2 `streamUrl`, `decodeFrame`, `reduceLive`, `initialLiveState`, `nextDelay`, `lotQueryKeys`, the constants and `ConnectionStatus`; Task 6b BFF entries; `apiFetch`, `parseEnvelope`.
- Produces:
  - `fetchLotSnapshot(lotId: string): Promise<{ availability: Availability; spaces: SpaceState[] }>` calls `/api/v1/lots/{id}/availability` and `/spaces` in parallel.
  - `type Connection = { status: ConnectionStatus; lastFrameAt: number | null; spacesReady: boolean }`. `spacesReady` mirrors `LiveState.spacesReady`, for the map's "Updating map…" state.
  - `type ConnectionStore = { get(): Connection; set(patch: Partial<Connection>): void; subscribe(listener: () => void): () => void }`, `createConnectionStore(initial: Connection): ConnectionStore`.
  - `type EventSourceLike = Pick<EventSource, 'readyState' | 'close' | 'addEventListener' | 'removeEventListener'>`.
  - `type LiveDeps = { EventSourceImpl: new (url: string) => EventSourceLike; fetchSnapshot: (lotId: string) => Promise<{ availability: Availability; spaces: SpaceState[] }>; random: () => number; now: () => number; onLotGone: () => void; target: Pick<Window, 'addEventListener' | 'removeEventListener'> }`.
  - `acquireLiveLot(origin: string, lotId: string, queryClient: QueryClient, deps?: Partial<LiveDeps>): { store: ConnectionStore; release(): void }` (M2B-20):
    - There is one connection per `(origin, lotId)`, ref-counted, and it closes `IDLE_CLOSE_MS` after the last release.
    - It seeds `LiveState` from the Query cache.
    - On every change it writes `availability` and `spaces` with `setQueryData` (same reference means no write).
    - It never sets `withCredentials`.
  - `resetLiveLotsForTests(): void` closes and forgets every connection.
  - `useLiveLot(lotId: string, origin: string | null): Connection`:
    - It acquires in an effect and reads the store with `useSyncExternalStore`.
    - With `origin === null`, it returns `{ status: 'unavailable', lastFrameAt: null, spacesReady: false }` and never acquires.
    - `onLotGone` calls `router.refresh()` once per mount (M2B-7).
  - `FakeEventSource` (`testing/event-source.ts`):
    - static `instances`;
    - fields `url`, `withCredentials`, `readyState`, `closed`;
    - methods `open()`, `emit(event, data: unknown | string, id?)` and `fail(readyState: 0 | 2)`.

The manager follows design §5.4 exactly. These are the details the table leaves open:
- the attempt counter resets on the first frame of a connection;
- a REST snapshot runs at most once per attempt and never within `SNAPSHOT_MIN_INTERVAL_MS` of the last;
- a snapshot `lot.not_found` stops the manager and calls `onLotGone`;
- `pagehide` closes, and `pageshow` reopens with `{kind: 'open'}`.

- [ ] **Step 1: Write the failing tests**

```ts
// manager.test.ts (fake timers; deps: FakeEventSource, random () => 1, fetchSnapshot vi.fn, target = new EventTarget())
test('opens one EventSource at the stream url without credentials')  // url 'http://api.test/v1/lots/L1/availability/stream?include=spaces'; withCredentials false
test('the first frame makes it live and writes the cache')           // emit availability v121 -> status 'live'; getQueryData(lotQueryKeys.availability('L1')).version 121
test('a lower version does not move the count')                      // v121 then v119 available 5 -> cache still v121
test('a malformed frame counts as fresh')                            // emit availability 'not json' at 30 s; advance 44_999 -> still 'live'
test('45 s silence goes stale and reconnects')                       // advance 45_000 -> 'stale', instance 0 closed; +1_000 -> instance 1
test('error while CONNECTING backs off from 1 s')                    // fail(0) -> 'stale'; +999 no new instance; +1 -> new
test('error while CLOSED waits 10 s and fetches one BFF snapshot')   // fail(2) -> fetchSnapshot once; +9_999 none; +1 -> new instance
test('a stale snapshot updates values but the state stays stale')   // snapshot v125 -> cache v125, status 'stale'
test('snapshot 404 lot.not_found calls onLotGone once and stops')    // no further instances after +60_000
test('two subscribers share one connection')                         // acquire x2 -> instances.length 1
test('the last release closes after 2 s')                            // +1_999 open; +1 closed
test('pagehide closes and pageshow reopens')
// useLiveLot.test.tsx
test('a null origin never opens a connection')                       // status 'unavailable'; FakeEventSource.instances.length 0
test('StrictMode double mount reuses one connection')
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/web -- src/features/lot/live`
Expected: FAIL (modules missing).

- [ ] **Step 3: Implement** `features/lot/api.ts`, the store, the manager and the hook. Listen for `availability`, `spaces` and `heartbeat` with `addEventListener`. Build an `SseFrame` from each `MessageEvent` and pass it through `decodeFrame`.

- [ ] **Step 4: Run tests**

Run: `npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web`
Expected: all pass.

- [ ] **Step 5: Builder checks (design §14).** Run the web against compose and record the results in the PR:
  - Confirm that `pagehide` and `pageshow` fire as expected with Next's client router. If they do not, close on `visibilitychange` hidden after 60 s instead.
  - Note which client IP the API logs for browser SSE (Y2).

- [ ] **Step 6: Update `docs/architecture.md`** §5 and §12 as listed under Files (design §13 "Docs to update").

- [ ] **Step 7: Commit**

```bash
git add apps/web docs/architecture.md
git commit -m "feat(web): add the availability stream manager with backoff, freshness and bff fallback"
```

---

## Task 8: Web home — hero, stat tiles, live block, lot info and rates (slice W3, about 400 lines)

**Files:**
- Create: `apps/web/src/features/lot/{server.ts,server.test.ts,LotSection.tsx,LotLive.tsx,LotLive.test.tsx}`, `apps/web/src/features/lot/components/{HomeHero,StatTiles,CountCard,StatusPill,FreshnessLine,LotInfo,RatesList,LotErrorPanel,LotSkeleton,Announcer}.tsx`, `apps/web/src/features/lot/components/components.test.tsx`
- Modify: `apps/web/src/lib/api/server.ts`, `apps/web/src/features/home/{PublicHome.tsx,PublicHome.test.tsx}`, `apps/web/src/app/[locale]/(public)/page.tsx`, `apps/web/src/app/[locale]/home-page.test.tsx`

**Interfaces:**
- Consumes:
  - Task 2: `availabilityFigures`, `planAnnouncement`, `lotQueryKeys`.
  - Task 6b: `formatMoney`, `formatClock`, `readPublicApiOrigin`; Task 6a copy keys.
  - Task 7: `useLiveLot`, `Connection`.
  - Task 4: `AppShell`.
  - The existing `serverFetch`, `buttonClass` and `Link`.
- Produces:
  - Getters in `lib/api/server.ts`. All use `{ auth: false, timeoutMs: 5_000 }`:
    - `listLots(): Promise<LotSummary[]>`;
    - `getLot(id: string): Promise<Lot>`;
    - `getAvailability(id: string): Promise<Availability>`;
    - `getSpaces(id: string): Promise<SpaceState[]>`;
    - `getTariff(id: string): Promise<Tariff>`.
  - `type LotSnapshot = { lot: Lot; availability: Availability; spaces: SpaceState[]; tariff: Tariff; fetchedAt: number }`.
  - `loadLotSnapshot(now?: () => number): Promise<LotSnapshot | null>` (M2B-7, M2B-17):
    - It calls `listLots()` and takes the first item.
    - It then loads the four resources with `Promise.allSettled`.
    - It returns `null` for an empty list or any failure.
  - `LotSection({ variant }: { variant: 'home' | 'compact' })` is an async RSC. It renders `LotLive` with `readPublicApiOrigin()`, or `LotErrorPanel` when the snapshot is `null`.
  - `LotLive({ variant, lotId, apiOrigin, snapshot }: { variant: 'home' | 'compact'; lotId: string; apiOrigin: string | null; snapshot: LotSnapshot })` is a client component:
    - It seeds the five Query keys with `initialData` and `initialDataUpdatedAt: snapshot.fetchedAt`. Availability and spaces get `staleTime: Infinity`, `refetchOnWindowFocus: false`. Lot and tariff get `staleTime: 300_000`.
    - The `home` variant renders `StatTiles`, `FreshnessLine`, `LotInfo` and `RatesList`. The `compact` variant renders `CountCard` and `FreshnessLine`.
    - Both variants render `Announcer`.
  - Presentational, props-only components:
    - `StatTiles({ figures, status }: { figures: AvailabilityFigures; status: LotStatus })`. Disponibles is the largest tile.
    - `CountCard({ figures, status }: …)`. A `closed` status replaces the number with `availability.status.closed` and `lot.unavailable.closedBody`.
    - `StatusPill({ status }: { status: LotStatus })`: icon and text on the status tint.
    - `FreshnessLine({ connection, updatedAt, lastFrameAt, snapshotAt }: { connection: ConnectionStatus; updatedAt: string; lastFrameAt: number | null; snapshotAt: number })`. The states and times follow design §4.3 and M2B-14.
    - `LotInfo({ lot }: { lot: Lot })`: the Maps link has `target="_blank"`, `rel="noopener noreferrer"` and the visible `lot.opensInNewTab` text.
    - `RatesList({ tariff }: { tariff: Tariff })`: six lines, with no reservation fee.
    - `LotErrorPanel({ onRetry }: { onRetry?: () => void })`: by default it calls `router.refresh()`.
    - `HomeHero()`: the title, subtitle and photo placeholder (`TODO(brand)`).
    - `LotSkeleton()`.
    - `Announcer({ figures, status, connection }: { figures: AvailabilityFigures; status: LotStatus; connection: ConnectionStatus })`: one `role="status"` element driven by `planAnnouncement` (M2B-22).
  - `PublicHome({ signedIn, notice, lot }: { signedIn: boolean; notice?: HomeNotice; lot: ReactNode })`. The lot section is a slot, and `page.tsx` passes `<Suspense fallback={<LotSkeleton />}><LotSection variant="home" /></Suspense>`. The hero and the actions never wait for the API (design §6.1).

- [ ] **Step 1: Write the failing tests**

```ts
// LotLive.test.tsx (FakeEventSource; fake timers with now = 2026-11-15T14:50:00Z; es-DO)
test('frame to screen: a pushed availability frame updates the count without a timer (M2B-42)')
// render home with the reduced snapshot; act(() => es.open(); es.emit('availability', {...reduced, version: 121, available: 210}))
// -> getByText('210') inside the same act; vi.getTimerCount() unchanged; no advanceTimers call
test('stale keeps the last values and shows the frame time')   // frame at 14:52Z, then fail(0) -> '211', 'Desactualizado — última actualización 10:52 a. m.', 'Reconectando…'
test('a null apiOrigin shows the snapshot and Live updates unavailable')
test('closed replaces the number with Cerrado')
test('full shows Lleno and 0 of 277')
test('reduced capacity shows 3 Fuera de servicio and 280 total')
test('the announcer says a status change at once and a count change at most every 30 s')
// components.test.tsx
test('RatesList shows the six lines in RD$')                    // 'RD$50.00','RD$25.00','RD$400.00','RD$500.00','10 min','15 min'; no reservation fee
test('LotInfo opens Maps in a new tab and says so')             // target '_blank', rel 'noopener noreferrer', '(se abre en una pestaña nueva)'
test('LotInfo hours: always open vs varies')
test('StatusPill carries text, not only color')                 // 'Abierto', 'Lleno', 'Cerrado'
// server.test.ts (stubFetch at the boundary)
test('loadLotSnapshot uses the first lot and loads four resources') // 5 fetches; upstream paths /v1/lots, /v1/lots/{id}, …/availability, …/spaces, …/tariff
test('loadLotSnapshot returns null when the list is empty or any call fails')
// PublicHome.test.tsx
test('the API down still renders the hero, Log in and Sign up')  // lot = <LotErrorPanel /> -> 'La disponibilidad no está cargando ahora' + 'Reintentar'
test('signed in shows the Vehicles link instead')
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/web -- src/features/lot src/features/home "src/app/[locale]/home-page.test.tsx"`
Expected: FAIL (modules missing).

- [ ] **Step 3: Implement** the getters, `loadLotSnapshot`, `LotSection`, `LotLive` and the presentational parts. Rewrite `PublicHome` and `page.tsx`. Follow the mockup 1a, 1b and 2 for the tile layout and the freshness line. The count uses Barlow 700, about 4.5 rem, tabular figures, no animation (design §9). The map region is left for Task 9.

- [ ] **Step 4: Verify**

Run: `npm run check:i18n && npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web && npm run build -w apps/web`
Expected: all pass.

Run: with the compose stack up, `npm run start -w apps/web`, then `curl -s http://localhost:3000/es-DO | grep -c "Disponibles"`. Stop the `api` container and fetch again.
Expected: `1` the first time. With the API down, the page is `200` and still contains "Iniciar sesión".

- [ ] **Step 5: Accessibility pass.**
  - NVDA on the home page: the count is read once, not on every push; the status pill is read as text; the external link announces the new tab.
  - At 200 % zoom, the tiles reflow without horizontal scroll.

- [ ] **Step 6: Commit**

```bash
git add apps/web
git commit -m "feat(web): show live availability, lot info and rates on the public home"
```

---

## Task 9: Web lot map — illustrated map, shapes, legend, summary, zoom (slice W4, about 350 lines)

**Files:**
- Create: `apps/web/src/features/lot/components/{SpaceMap,SpaceCell,MapLegend,SpacesSummary,FreeSpacesList,ZoomControls}.tsx`, `apps/web/src/features/lot/components/SpaceMap.test.tsx`
- Modify: `apps/web/src/features/lot/LotLive.tsx`, `apps/web/src/features/lot/LotLive.test.tsx`

**Interfaces:**
- Consumes: Task 2 `countSpaces`; Task 7 `Connection.spacesReady`; Task 8 `LotLive`.
- Produces:
  - `ZOOM_LEVELS = [1, 1.25, 1.5] as const`, `type ZoomLevel = (typeof ZOOM_LEVELS)[number]`.
  - `SpaceMap({ spaces, ready, zoom }: { spaces: SpaceState[] | null; ready: boolean; zoom: ZoomLevel })`:
    - The illustrated lot follows the mockup: concrete, lawn and trees from the §9.1b tokens, decorative only.
    - The 280 bays are in code order with no zones (M2B-23), 10 columns below `md` and 20 from `md`.
    - The grid has `aria-hidden="true"`.
    - `spaces === null` renders a static skeleton grid. `ready === false` with spaces renders at 60 % opacity with `spaces.updating` (design §9.3).
    - At zoom above 1, the map frame scrolls inside itself (`overflow-auto`). The page never scrolls horizontally at 320 px.
  - `SpaceCell({ code, state }: { code: string; state: SpaceStatus })`:
    - It renders `data-code` and `data-state`.
    - Free: a 2 px `status-open` outline. Occupied: a car shape (`data-shape="car"`) in `space-occupied`. Out of service: a hatch with "×" and a 1 px `ink-muted` outline.
    - Cells are not interactive.
  - `MapLegend()`: each shape with its text.
  - `SpacesSummary({ spaces }: { spaces: SpaceState[] })`: the visible `spaces.summary` sentence and the `spaces.note`.
  - `FreeSpacesList({ spaces }: { spaces: SpaceState[] })`: `<details><summary>{spaces.listFree}</summary>` with the free codes as text.
  - `ZoomControls({ zoom, onZoom }: { zoom: ZoomLevel; onZoom: (next: ZoomLevel) => void })`: two buttons labelled `spaces.zoomOut` and `spaces.zoomIn`, 44 px, disabled at the bounds.
  - `LotLive` home variant gets the map card (title, `spaces.sensorsLive`, zoom, map, legend, summary, list). The compact variant gets the map without zoom (M2B-36).

- [ ] **Step 1: Write the failing tests**

```ts
// LotLive.test.tsx addition
test('frame to screen: a spaces delta changes the cell state without a timer (M2B-42)')
// emit spaces spacesFullEventFixture, then act(() => es.emit('spaces', spacesDeltaFixture))
// -> container.querySelector('[data-code="E100"]').dataset.state === 'occupied' in the same act; no timers advanced
// SpaceMap.test.tsx
test('the grid is hidden from assistive tech and the summary reads the counts')
// aria-hidden 'true'; 'Sensores — libres: 211, ocupados: 66, fuera de servicio: 3.'
test('each state has its own shape, not only color')    // E067 no [data-shape]; E001 has [data-shape="car"]; E278 contains '×'
test('the legend repeats each shape with text')         // 'Disponible', 'Ocupado', 'Fuera de servicio'
test('the free list gives codes as text')               // summary 'Ver espacios libres'; contains 'E067', not 'E001'
test('zoom buttons are labelled and stop at the bounds') // zoomOut disabled at 1; two zoomIn -> zoomIn disabled
test('no spaces yet shows the skeleton; not ready shows Actualizando mapa… at 60 %')
test('the note says the map shows sensor readings')     // 'El mapa muestra lo que leen los sensores.'
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/web -- src/features/lot`
Expected: FAIL (modules missing).

- [ ] **Step 3: Implement** the map parts with Tailwind and tokens only, using inline SVG for the car and the trees. Wire them into `LotLive`, with `ready` taken from `Connection.spacesReady`.

- [ ] **Step 4: Verify**

Run: `npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web && npm run build -w apps/web`
Expected: all pass.

- [ ] **Step 5: Accessibility pass.**
  - NVDA hears one summary sentence, not 280 cells.
  - The free list opens with the keyboard.
  - The zoom buttons are reachable and labelled.
  - At 320 px and at 200 % zoom, the page has no horizontal scroll.

- [ ] **Step 6: Commit**

```bash
git add apps/web
git commit -m "feat(web): add the illustrated lot map with legend, summary and zoom"
```

---

## Task 10: Mobile stream — `expo/fetch` transport, lot API, manager, AppState (slice M1, about 400 lines)

**Files:**
- Create: `apps/mobile/src/services/{sse.ts,sse.test.ts}`, `apps/mobile/src/features/lot/{api.ts,api.test.ts}`, `apps/mobile/src/features/lot/live/{connection-store.ts,manager.ts,manager.test.ts,useLotId.ts,useLiveLot.ts}`, `apps/mobile/src/testing/stream.ts`

**Interfaces:**
- Consumes: Task 2 `createSseParser`, `streamUrl`, `decodeFrame`, `reduceLive`, `initialLiveState`, `nextDelay`, `parseRetryAfter`, `lotQueryKeys` and the constants; Task 1 schemas; `http`, `unwrap` and `getApiBaseUrl`.
- Produces:
  - Transport types in `services/sse.ts`:
    - `type StreamResponse = { status: number; headers: { get(name: string): string | null }; body: { getReader(): { read(): Promise<{ done: boolean; value?: Uint8Array }> } } | null; text(): Promise<string> }`.
    - `type StreamFetch = (url: string, init: { headers: Record<string, string>; signal: AbortSignal }) => Promise<StreamResponse>`.
    - `type StreamEnd = { kind: 'ended' } | { kind: 'aborted' } | { kind: 'network' } | { kind: 'http'; status: number; code: string | null; retryAfterMs: number | null }`.
  - `openStream(url: string, opts: { signal: AbortSignal; onFrame: (frame: SseFrame) => void; fetchImpl?: StreamFetch }): Promise<StreamEnd>` (M2B-21):
    - It calls `expo/fetch` directly, not the M1 `http` client and its 3 s timeout, with `Accept: text/event-stream`.
    - A `200` with `text/event-stream` is read through `TextDecoder` (`{ stream: true }`) into one parser.
    - Any other response reads the body as text. `code` comes from a JSON error envelope, or is `null` for anything else, such as HTML from a proxy. `retryAfterMs` is `parseRetryAfter(headers.get('retry-after'))`.
  - Lot API in `features/lot/api.ts`. Each call is a public `http.request` through `unwrap`:
    - `getLots(): Promise<LotSummary[]>`;
    - `getLot(id: string): Promise<Lot>`;
    - `getAvailability(id: string): Promise<Availability>`;
    - `getSpaces(id: string): Promise<SpaceState[]>`;
    - `getTariff(id: string): Promise<Tariff>`.
  - `type Connection = { status: ConnectionStatus; lastFrameAt: number | null; spacesReady: boolean }`, and `createConnectionStore` with the same shape as web Task 7.
  - `type MobileLiveDeps = { fetchImpl: StreamFetch; random: () => number; now: () => number; appState: { addEventListener(type: 'change', listener: (state: AppStateStatus) => void): { remove(): void } }; fetchSnapshot: (lotId: string) => Promise<{ availability: Availability; spaces: SpaceState[] }>; onLotGone: () => void }`.
  - `acquireLiveLot(origin: string, lotId: string, queryClient: QueryClient, deps?: Partial<MobileLiveDeps>): { store: ConnectionStore; release(): void }`, plus `resetLiveLotsForTests(): void`. The rules are design §5.4–§5.6:
    - a 429 waits `max(retryAfterMs ?? 0, nextDelay(attempt, random, HTTP_FAILURE_FLOOR_MS))`;
    - a 404 `lot.not_found` stops and calls `onLotGone`;
    - `background` or `inactive` aborts;
    - `active` reopens with `{kind: 'open'}` and status `connecting`.
  - `useLotId(): { lotId: string | null; status: 'pending' | 'error' | 'success'; refetch: () => void }` reads the first item of `lotQueryKeys.all` (M2B-7).
  - `useLiveLot(lotId: string | null): Connection` takes the origin from `getApiBaseUrl()`. `onLotGone` invalidates `lotQueryKeys.all` once per mount.
  - `testing/stream.ts`: `controllableStream(): { fetchImpl: StreamFetch; push(text: string): void; end(): void; fail(): void; respond(status: number, body: string, headers?: Record<string, string>): void; calls: Array<{ url: string; signal: AbortSignal }> }`.

- [ ] **Step 1: Builder checks first (design §14, R2).** On the demo Android phone in Expo Go, open a throwaway screen that streams `GET /v1/lots/{id}/availability/stream?include=spaces` with `expo/fetch`. Check each item and record the result in the PR:
  1. `response.body` delivers chunks incrementally, and `abort()` ends the read.
  2. `TextDecoder` with `{ stream: true }` exists on Hermes.
  3. `Intl.DateTimeFormat` with `timeZone: 'America/Santo_Domingo'` works.
  4. The API access log shows which client IP the phone's SSE uses (Y2).

  Fallbacks:
  - If item 1 fails, the manager polls REST every 5 s (`POLL_FALLBACK_MS = 5_000`) with the same state machine, and `connecting` never becomes `live`. That path adds the key `availability.polling` ("Updates every few seconds") to both apps.
  - If item 2 fails, use a byte-to-char decoder; the payloads are ASCII.
  - If item 3 fails, use a fixed −04:00 offset and note it in a comment.

  Delete the throwaway screen before committing.

- [ ] **Step 2: Write the failing tests**

```ts
// services/sse.test.ts
test('sends Accept text/event-stream and the abort signal')
test('chunked frames split mid-line are parsed once')           // push('event: availa'), push('bility\ndata: {…}\n'), push('\n') -> 1 frame
test('a multi-byte character split across chunks decodes once')
test('a non-200 returns status, code and retryAfterMs')         // 429 errorBody('rate_limit.exceeded'), Retry-After '10' -> {kind:'http', status:429, code:'rate_limit.exceeded', retryAfterMs:10000}
test('an HTML error body gives code null')                      // 502 '<html>' -> code null
// features/lot/api.test.ts (stub http.request with testing/response.reply)
test('getLots returns the items')
test('getAvailability surfaces lot.not_found as an ApiError')
// live/manager.test.ts (controllableStream, fake timers, random () => 1, appState stub)
test('the first frame makes it live and writes the cache')
test('a malformed frame keeps the stream live')                 // push 'event: availability\ndata: oops\n\n' -> no throw, status 'live', count unchanged
test('a 429 waits Retry-After when it is larger than the backoff') // Retry-After 20 -> no call at 19_999; call at 20_000
test('a 429 with Retry-After soon waits the 10 s floor')       // headers {'retry-after':'soon'}, body '<html>' -> no call at 9_999; call at 10_000; never NaN
test('404 lot.not_found re-discovers the lot once')             // onLotGone called once; no reconnect after 60_000
test('end of stream reconnects with backoff from 1 s')
test('45 s without a frame goes stale, aborts and reconnects')
test('background aborts the stream and foreground reconnects with a fresh snapshot') // signal.aborted; on 'active' a new call; first frame -> 'live'
test('two subscribers share one stream; the last release aborts after 2 s')
```

- [ ] **Step 3: Run to verify they fail**

Run: `npm test -w apps/mobile -- src/services/sse.test.ts src/features/lot`
Expected: FAIL (modules missing).

- [ ] **Step 4: Implement** `services/sse.ts`, the lot API, the store, the manager, `useLotId` and `useLiveLot`. The manager imports `fetch` from `expo/fetch` only as the default `fetchImpl`.

- [ ] **Step 5: Verify**

Run: `npm test -w apps/mobile && npm run lint -w apps/mobile && npm run typecheck -w apps/mobile && npm run check:layout`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add apps/mobile
git commit -m "feat(mobile): add the expo fetch availability stream with backoff and app-state handling"
```

---

## Task 11: Mobile Welcome and Inicio — header, hero, count card, lot info, rates (slice M2, about 400 lines)

**Files:**
- Create: `apps/mobile/src/lib/format/{money.ts,time.ts,format.test.ts}`, `apps/mobile/src/features/lot/{useLotData.ts,useAnnouncer.ts,LotLive.tsx,LotLive.test.tsx,LotHomeScreen.tsx,lot.layout.test.tsx}`, `apps/mobile/src/features/lot/components/{HeroPlaceholder,CountCard,StatusPill,FreshnessLine,LotInfo,RatesList,LotErrorPanel,LotSkeleton}.tsx`, `apps/mobile/src/app/(tabs)/home/{_layout.tsx,index.tsx}`
- Modify: `apps/mobile/src/features/auth/{WelcomeScreen.tsx,WelcomeScreen.test.tsx}`, `apps/mobile/src/app/(tabs)/_layout.tsx` (register `home` with label `nav.home`; Task 12 sets the order), `apps/mobile/src/lib/i18n/locales/{es-DO,en}.json` (`nav.home` "Inicio" / "Home")

**Interfaces:**
- Consumes:
  - Task 10: `useLotId`, `useLiveLot`, the lot API.
  - Task 5: `ScreenScroll`, `WavyHeader`, `renderAtSize`, `scrollPadding`, `countHorizontalScrolls`.
  - Task 2: `availabilityFigures`, `planAnnouncement`.
  - Task 6a: copy keys.
  - The existing `Button`, `AppText`, `ParkingSign`, `LanguageSwitch`.
- Produces:
  - `formatMoney(cents: number, language: AppLanguage, fallback: (amount: string) => string): string` and `formatClock(at: string | number, language: AppLanguage): string`, with the same rules as web Task 6b.
  - `type LotData = { lotId: string | null; lot?: Lot; availability?: Availability; spaces?: SpaceState[]; tariff?: Tariff; connection: Connection; loadError: boolean; retry: () => void }`.
  - `useLotData(): LotData`:
    - It runs the four queries keyed by `lotQueryKeys`. Availability and spaces get `staleTime: Infinity`, `refetchOnWindowFocus: false`. Lot and tariff get `staleTime: 300_000`.
    - It calls `useLiveLot(lotId)`.
  - `useAnnouncer(input: AnnounceInput | null): void` sends `AccessibilityInfo.announceForAccessibility` through `planAnnouncement`.
  - `LotLive({ actions }: { actions?: ReactNode })`:
    - Order: count card, then `actions` right under it, then the map slot (Task 12), then `LotInfo` and `RatesList`.
    - States follow the design §4.3 matrix.
  - Presentational components:
    - `CountCard({ figures, status }: { figures: AvailabilityFigures; status: LotStatus })`: the count is 64 sp with `maxFontSizeMultiplier={1.5}`.
    - `StatusPill({ status })`.
    - `FreshnessLine({ connection, updatedAt, lastFrameAt, snapshotAt })`.
    - `LotInfo({ lot }: { lot: Lot })`: "Abrir en Maps" uses `Linking.openURL(lot.map_url)`, `accessibilityRole="link"` and the hint "Opens Maps".
    - `RatesList({ tariff })`, `LotErrorPanel({ onRetry })`, `LotSkeleton()`, `HeroPlaceholder()` (`TODO(brand)`).
  - `WelcomeScreen()`: `WavyHeader` with `ParkingSign`, the language switch and the lot name, then `HeroPlaceholder`, then `LotLive` with Log in and Sign up as `actions`, inside `ScreenScroll`.
  - `LotHomeScreen()`: `WavyHeader` without the switch, then `LotLive` with no actions, inside `ScreenScroll edge="tabBar"`.
  - Routes:
    - `(tabs)/home/_layout.tsx`: a `Stack` with `headerShown: false`.
    - `(tabs)/home/index.tsx`: renders `LotHomeScreen`.

- [ ] **Step 1: Write the failing tests**

```ts
// lib/format/format.test.ts
test('formatMoney prints RD$ in both languages')          // (5000,'es-DO') and (5000,'en') -> 'RD$50.00'
test('formatClock ignores the machine time zone')         // ('2026-11-15T14:47:00Z','es-DO') -> '10:47 a. m.'; 'en' -> '10:47 AM'
// LotLive.test.tsx (controllableStream, fake timers at 2026-11-15T14:50:00Z, http stub with fixtures, es-DO)
test('frame to screen: a pushed availability frame updates the count without a timer (M2B-42)')
// after the first frame, act(() => push(sseText('availability', {...reduced, version: 121, available: 210}, 121)))
// -> getByText('210') in the same act; jest.getTimerCount() unchanged
test('stale keeps the last values and shows the frame time')   // 'Desactualizado — última actualización 10:52 a. m.' + 'Reconectando…'
test('closed replaces the number with Cerrado')
test('the count never grows past 1.5x font scale')             // count AppText maxFontSizeMultiplier === 1.5
test('Abrir en Maps opens the server map_url')                 // Linking.openURL called with lotFixture.map_url
test('a status change is announced at once and a count change at most every 30 s') // spy announceForAccessibility
// WelcomeScreen.test.tsx
test('Welcome renders the actions with the API down')          // getLots rejects client.network -> 'Iniciar sesión', 'Crear cuenta', 'Reintentar'
test('Welcome shows the count, status and rates from fixtures')
// lot.layout.test.tsx
test.each(['small', 'large'])('Welcome and Inicio at %s keep the 16 dp gutter and never scroll sideways')
// renderAtSize(<WelcomeScreen/>, size) -> scrollPadding() {left 16, right 16, bottom 24 + 34}; countHorizontalScrolls() 0
// renderAtSize(<LotHomeScreen/>, size) -> bottom 24 (tab bar owns the inset)
test('the header adds the top inset')                          // WavyHeader paddingTop === 16 + 24
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/mobile -- src/lib/format src/features/lot src/features/auth/WelcomeScreen.test.tsx`
Expected: FAIL (modules missing; Welcome is still the M1 screen).

- [ ] **Step 3: Implement** the formatting, `useLotData`, `useAnnouncer`, `LotLive`, the presentational parts, both screens and the two route files. Follow mockup 3 and 4 for the header wave, the card overlapping the header edge, and the spacing.

- [ ] **Step 4: Verify**

Run: `TZ=Europe/Madrid npm test -w apps/mobile -- src/lib/format && npm run check:i18n && npm run check:layout && npm test -w apps/mobile && npm run lint -w apps/mobile && npm run typecheck -w apps/mobile`
Expected: all pass.

- [ ] **Step 5: Device and TalkBack pass (M2B-52).** On 360 × 640 and on a large phone, both at font scale 1.3, check Welcome:
  - Log in and Sign up are visible without scrolling on the small phone;
  - equal gutters, nothing clipped;
  - the notch is clear.
  With TalkBack: the count is read once, status changes are announced, and the Maps link announces its hint. Record the result in the PR.

- [ ] **Step 6: Commit**

```bash
git add apps/mobile
git commit -m "feat(mobile): show live availability, lot info and rates on welcome and inicio"
```

---

## Task 12: Mobile map preview, full map, tab order and landing (slice M3, about 400 lines)

**Files:**
- Create: `apps/mobile/src/features/lot/{map-layout.ts,map-layout.test.ts,LotMapScreen.tsx,LotMapScreen.test.tsx}`, `apps/mobile/src/features/lot/components/{SpaceMap,SpaceCell,MapLegend,SpacesSummary,MapPreviewCard}.tsx`, `apps/mobile/src/features/session/{tabs.ts,tabs.test.ts}`, `apps/mobile/src/app/(tabs)/home/map.tsx`, `apps/mobile/src/app/(auth)/map.tsx`
- Modify: `apps/mobile/src/features/lot/{LotLive.tsx,LotLive.test.tsx,LotHomeScreen.tsx,lot.layout.test.tsx}`, `apps/mobile/src/features/auth/WelcomeScreen.tsx` (both pass `mapHref`), `apps/mobile/src/app/(tabs)/_layout.tsx`, `apps/mobile/src/app/(tabs)/home/_layout.tsx` (add `map`), `apps/mobile/src/features/session/{root-route.ts,root-route.test.ts}`, `docs/architecture.md` §13

**Interfaces:**
- Consumes: Task 11 `useLotData`, `LotLive`; Task 2 `countSpaces`; Task 5 `SCREEN_GUTTER`, `TabIcon`, `TabLabel`, `tabBarStyle`, the layout test helpers.
- Produces:
  - `PREVIEW_COUNT = 40`, `MAP_COLUMNS = 10`, `MAP_GAP = 4`, `MAP_CARD_PADDING = 16`.
  - `cellSize(windowWidth: number, columns = MAP_COLUMNS, gap = MAP_GAP, cardPadding = MAP_CARD_PADDING): number` = `Math.floor((windowWidth − 2 × 16 − 2 × cardPadding − gap × (columns − 1)) / columns)` (M2B-52, design §9.3).
  - `SpaceMap({ spaces, ready, limit }: { spaces: SpaceState[] | null; ready: boolean; limit?: number })`:
    - The grid is a wrapped `View` with `accessibilityElementsHidden` and `importantForAccessibility="no-hide-descendants"`.
    - `null` renders a static skeleton. `ready === false` renders at 60 % opacity with `spaces.updating`.
  - `SpaceCell({ code, state, size }: { code: string; state: SpaceStatus; size: number })`:
    - `testID` is `space-<code>`.
    - The shape child has `testID` `shape-free`, `shape-car` or `shape-hatch`, drawn with `View`s in the design §9.3 colors.
  - `MapLegend()`.
  - `SpacesSummary({ spaces }: { spaces: SpaceState[] })`: the summary covers all 280, then the note, then a "List free spaces" toggle that expands a text list (design §11).
  - `MapPreviewCard({ spaces, ready, mapHref }: { spaces: SpaceState[] | null; ready: boolean; mapHref: '/map' | '/home/map' })`:
    - It shows the first 40 by code, `spaces.previewCount`, the legend and the summary.
    - "Ver mapa completo" is a 48 dp button that calls `router.push(mapHref)` (M2B-49).
  - `LotLive({ actions, mapHref }: { actions?: ReactNode; mapHref: '/map' | '/home/map' })` gains `mapHref` and renders `MapPreviewCard`. `WelcomeScreen` passes `/map`, and `LotHomeScreen` passes `/home/map`.
  - `LotMapScreen({ edge }: { edge: 'screen' | 'tabBar' })`:
    - It shows all 280 cells, with the count, status and freshness line, the legend and the summary.
    - It uses `ScreenScroll` with `edge="tabBar"` inside tabs and `edge="screen"` before login, via a prop `edge: 'screen' | 'tabBar'`.
    - Back returns to the lot screen.
  - `TAB_ORDER = ['home', 'vehicles', 'profile'] as const` in `features/session/tabs.ts`. `(tabs)/_layout.tsx` renders `Tabs.Screen` in this order, with `TabIcon` and `nav.home|vehicles|profile`, and sets `unstable_settings = { initialRouteName: 'home' }` (M2B-48).
  - `rootHref('tabs')` returns `'/home'` (Inicio is the post-login landing). `VehicleFormScreen`'s `dismissTo('/vehicles')` is unchanged.

- [ ] **Step 1: Write the failing tests**

```ts
// map-layout.test.ts
test('cellSize fits 10 columns inside the card at 360 and 430 dp')  // 360 -> 26; 430 -> 33; 10*size + 9*4 <= width - 32 - 32
// LotLive.test.tsx addition
test('frame to screen: a spaces delta changes the cell without a timer (M2B-42)')
// push full spaces, then act(() => push(sseText('spaces', spacesDeltaFixture)))
// -> within(getByTestId('space-E100', {includeHiddenElements: true})).getByTestId('shape-car', {includeHiddenElements: true})
test('the preview shows the first 40 spaces and says 40 de 280 espacios') // space-E040 present, space-E041 absent
test('Ver mapa completo opens the full map')                        // router.push('/home/map'); on Welcome '/map'
test('the map is hidden from accessibility and the summary covers all 280') // grid importantForAccessibility 'no-hide-descendants'; 'Sensores — libres: 211, ocupados: 66, fuera de servicio: 3.'
// LotMapScreen.test.tsx
test('the full map shows 280 cells in code order')
test('List free spaces expands a text list')                        // contains 'E067'
// lot.layout.test.tsx addition
test.each(['small', 'large'])('the full map at %s keeps the gutter, never scrolls sideways and its cells fit the card')
// scrollPadding() left/right 16; countHorizontalScrolls() 0; cell width === cellSize(window width) and <= card inner width
// tabs.test.ts and root-route.test.ts
test('tabs are Inicio, Vehículos, Perfil in that order')            // TAB_ORDER deep-equals ['home','vehicles','profile']
test('a signed-in driver lands on Inicio')                          // rootHref('tabs') === '/home'
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/mobile -- src/features/lot src/features/session`
Expected: FAIL (modules missing; `rootHref('tabs')` is still `/vehicles`).

- [ ] **Step 3: Implement** the map parts, the preview card, the full-map screen and its two routes, the tab order and the landing. Update `docs/architecture.md` §13:
  - the tabs in M2 are Inicio, Vehículos, Perfil;
  - Inicio is the landing;
  - Welcome shows the lot;
  - there is a full-map screen.

- [ ] **Step 4: Verify**

Run: `npm run check:i18n && npm run check:layout && npm test -w apps/mobile && npm run lint -w apps/mobile && npm run typecheck -w apps/mobile && (cd apps/mobile && npx expo install --check)`
Expected: all pass.

- [ ] **Step 5: Device check (M2B-52, design §12.1).** Do this before merging. On a 360 × 640 device and on a large phone, both at font scale 1.3, check Welcome, Inicio, the full map, Vehículos and Perfil:
  - no horizontal scroll;
  - equal gutters;
  - nothing clipped at the edges;
  - the notch and the home indicator are clear;
  - the tab bar is fully visible.
  Then a TalkBack pass over the map card and the full map: one summary, and the free list is readable. Record the result in the PR. Fix any offending screen in this task.

- [ ] **Step 6: Commit**

```bash
git add apps/mobile docs/architecture.md
git commit -m "feat(mobile): add the map preview, full map screen and inicio as first tab and landing"
```

---

## Task 13: Console BFF — admin-guarded simulator route (slice S1, about 380 lines, sensitive path)

**Files:**
- Create: `apps/web/src/lib/simulator/{allowlist.ts,allowlist.test.ts,upstream.ts,handler.ts,handler.test.ts}`, `apps/web/src/app/api/admin/simulator/[...path]/route.ts`
- Modify: `apps/web/src/proxy.ts` (add `'/api/admin/:path*'` to `config.matcher`), `apps/web/src/proxy.test.ts`, `docs/architecture.md` §5 and §12 (the console route, its credential and the per-request role check)
- Verify, do not change: `deploy/compose/docker-compose.yml` already gives `web` both `SIMULATOR_INTERNAL_URL` and `SIMULATOR_CONTROL_TOKEN`.

**Interfaces:**
- Consumes: Task 1 `CAR_ID_PATTERN`, `SPACE_CODE_PATTERN`, `userSchema`, `parseEnvelope`; existing `verifyMutation`, `bffError`, `passthrough`, `apiUrl`, `ACCESS_COOKIE`, `REFRESH_COOKIE`, `bffRequest` (tests).
- Produces:
  - `type SimulatorRoute = { method: 'GET' | 'POST'; upstreamPath: string }`.
  - `matchSimulatorRoute(method: string, segments: string[], search: URLSearchParams): SimulatorRoute | null` (M2B-28, design §7.2):
    - The seven entries, with literal segments matched exactly, `carId` against `CAR_ID_PATTERN` and `code` against `SPACE_CODE_PATTERN`.
    - For `events`, `after` must appear at most once, match `^(0|[1-9]\d{0,18})$` and be ≤ 9223372036854775807 (compared as `BigInt`), otherwise the result is `null`. The upstream path is `/control/events` or `/control/events?after=<n>`.
    - Every other query parameter, on every route, is dropped.
  - `simulatorConfig(env?: NodeJS.ProcessEnv): { baseUrl: string; token: string } | null`. It reads `SIMULATOR_INTERNAL_URL` without a trailing `/` and `SIMULATOR_CONTROL_TOKEN`. An empty value gives `null` (M2B-29).
  - `SIMULATOR_TIMEOUT_MS = 5_000`, `MAX_BODY_BYTES = 4_096`.
  - `handleSimulator(request: NextRequest, segments: string[]): Promise<NextResponse>`. The checks run in this order (design §7.1):
    1. No config → `404 http.not_found`.
    2. No allow-list match → `404 http.not_found`.
    3. Session and role, checked on every request (M2B-27, plan decision 5):
       - no access cookie → `401 auth.unauthenticated`, or `503 internal.unexpected` when a refresh cookie exists;
       - `GET /v1/me` with the bearer: a 401 → `401 auth.unauthenticated`, a role other than `admin` → `403 auth.forbidden`, unreachable → `502 internal.unexpected`.
    4. On POST, `verifyMutation` (CC-3).
    5. On POST, the body must be at most 4 096 bytes (`413 http.payload_too_large`) and parse as a JSON object (`400 http.invalid_json`). The original text is forwarded byte for byte, never re-serialized, so seed digits survive (M2B-33).
    6. `fetch(baseUrl + upstreamPath)` with `X-Simulator-Token`, `Accept: application/json`, `redirect: 'manual'` and a 5 s timeout. Unreachable → `502 internal.unexpected`.
    7. The answer goes through `passthrough(upstream, text)`, which keeps status, `content-type`, `retry-after`, `x-request-id` and `Cache-Control: no-store`. A 204 has no body. The token is never echoed.
  - `route.ts`: `GET` and `POST` only, each `(request: NextRequest, { params }: { params: Promise<{ path: string[] }> }) => handleSimulator(request, (await params).path)`.

- [ ] **Step 1: Write the failing tests**

```ts
// lib/simulator/allowlist.test.ts
test('matches the seven console routes')            // GET status; POST scenarios/start, scenarios/stop, cars, cars/car-000123/leave, sensors/E017/fault; GET events
test('rejects crafted ids')                          // cars/CAR-000001/leave, cars/car-1/leave, sensors/E1000/fault, sensors/e017/fault -> null
test('after must be one non-negative int64')         // after=-1, 1e3, 99999999999999999999, after=1&after=2 -> null; after=0 -> '/control/events?after=0'
test('unknown query params are dropped')             // status?x=1 -> '/control/status'; events?after=5&x=1 -> '/control/events?after=5'
// lib/simulator/handler.test.ts (// @vitest-environment node; stubFetch routes by URL: api.test/v1/me and sim.test/control/*)
test('missing config is 404 and calls nothing')      // SIMULATOR_CONTROL_TOKEN '' -> 404 http.not_found; fetch not called
test('rejects crafted ids and after values without calling upstream') // the Review Focus 4 list, incl. 'cars/car-000001%2F..%2Fstatus/leave' -> 404, no sim.test call
test('no session is 401; a refresh cookie alone is 503')
test('a driver gets 403 auth.forbidden')             // /v1/me returns userFixture
test('POST without CSRF is 403 and never reaches the simulator')
test('an admin GET sets X-Simulator-Token upstream and never echoes it') // upstream header === token; response headers and body lack the token
test('a 204 passes through with no body')
test('a body over 4 KB is 413 and a non-object body is 400')
test('seed digits are forwarded byte for byte')      // '{"scenario":"weekday","speed":30,"seed":9007199254740993}' arrives unchanged
test('upstream down is 502 internal.unexpected')
test('upstream 401 simulator.unauthorized passes through')
// proxy.test.ts addition
test('the matcher covers /api/admin and refreshes for it') // config.matcher contains '/api/admin/:path*'; a request with only mp_refresh gets refreshed
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/web -- src/lib/simulator src/proxy.test.ts`
Expected: FAIL (modules missing; matcher lacks `/api/admin`).

- [ ] **Step 3: Implement** the allow-list, `upstream.ts` (`simulatorConfig`, the upstream URL builder), the handler, the route and the matcher line. Read `node_modules/next/dist/docs/01-app/01-getting-started/15-route-handlers.md` first (async `params`).

- [ ] **Step 4: Verify**

Run: `npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web && npm run build -w apps/web`
Expected: all pass.

Run with compose up and an admin session in a browser: `curl -s -b "mp_access=<admin access>" http://localhost:3000/api/admin/simulator/status`
Expected: `200` with `data.running`. Without the cookie, the same call is `401`.

- [ ] **Step 5: Update `docs/architecture.md`** §5 and §12 (the console route, the admin check on every request, the token held only on the server).

- [ ] **Step 6: Security checklist and risk review** (sensitive: `lib/simulator`, `proxy.ts`). Complete the PR template's checklist. Run `review-risk` or `/security-review` on the diff and link it. Confirm all four:
  - the token is read only from `SIMULATOR_CONTROL_TOKEN` (never `NEXT_PUBLIC_*`) and is absent from every response;
  - the role is checked per request;
  - only the seven operations pass;
  - no query parameter is forwarded except a valid `after`.

- [ ] **Step 7: Commit**

```bash
git add apps/web docs/architecture.md
git commit -m "feat(web): add the admin-guarded simulator console route"
```

---

## Task 14: Console UI, part 1 — page, status, start and stop (slice S2, about 400 lines)

**Files:**
- Create: `apps/web/src/app/[locale]/(admin)/admin/simulator/page.tsx`, `apps/web/src/features/simulator/{api.ts,api.test.ts,seed.ts,seed.test.ts,controls.ts,controls.test.ts,store.ts,queries.ts,SimulatorConsole.tsx,SimulatorConsole.test.tsx,StatusPanel.tsx,StartForm.tsx,StopControls.tsx,NotConfiguredPanel.tsx}`, `apps/web/src/providers/simulator-store-provider.tsx`
- Modify: `apps/web/src/lib/api/client.ts` (and its test), `apps/web/src/features/admin/AdminLanding.tsx` (a "Simulator console" link, M2B-38), `apps/web/src/i18n/messages/{es-DO,en}.json` (`simulator.*` for this task)

**Interfaces:**
- Consumes:
  - Task 13 route.
  - Task 1: `simulatorStatusSchema`, `CONSOLE_SCENARIOS`.
  - Task 8: `LotSection variant="compact"`.
  - Task 6b: `formatClock`.
  - Existing: `apiFetch`, `applyApiError`, `Banner`, `Field`, `Button`, `requireAdmin`.
- Produces:
  - `apiFetch` extension (backward compatible):
    - `ApiInit` gains `bodyText?: string`. It is sent verbatim with `Content-Type: application/json` and cannot be combined with `body`.
    - `parse` gains an optional third argument `text: string` (the raw response text).
  - `features/simulator/seed.ts` (M2B-33):
    - `extractRawSeed(text: string): string | null` uses `/"seed"\s*:\s*(\d+)/`.
    - `startFormSchema`: `scenario` ∈ `CONSOLE_SCENARIOS`; `speed` is an integer 1–120 with default 30; `seed` is `''` or digits ≤ `9223372036854775807` (`integer`, `max:9223372036854775807`); `start_at` is `''` or `YYYY-MM-DDTHH:mm`.
    - `type StartFormValues`.
    - `toStartBody(values: StartFormValues): string` writes the seed digits unquoted, omits empty fields, and sends `start_at` as `<local>:00-04:00`.
  - `type StatusResult = { status: SimulatorStatus; rawSeed: string }`.
  - `fetchStatus(): Promise<StatusResult>`, `startScenario(values: StartFormValues): Promise<StatusResult>`, `stopScenario(drain: boolean): Promise<StatusResult>`. All paths are under `/api/admin/simulator/`.
  - `simulatorKeys = { status: ['simulator', 'status'], events: ['simulator', 'events'] } as const`.
  - `useSimulatorStatus()`: `refetchInterval: 2_000`, `refetchIntervalInBackground: false`, `retry: false` (M2B-30).
  - Every console mutation (start and stop here; inject, leave and fault in Task 15) invalidates `simulatorKeys.status` on success (design §7.3).
  - `type ConsoleMode = 'notConfigured' | 'unreachable' | 'idle' | 'running' | 'draining'`.
  - `consoleMode(result: { data?: StatusResult; error?: unknown }, draining: boolean): ConsoleMode`:
    - `404 http.not_found` or `401 simulator.unauthorized` → `notConfigured`;
    - `client.network` or 502 → `unreachable`.
  - `deriveControls(status: SimulatorStatus | undefined, draining: boolean): { canStart: boolean; canStopNow: boolean; canDrain: boolean; canInject: boolean; canLeave: boolean; canFault: boolean }` (M2B-44, M2B-45):
    - `canStart = !draining && !(running && scenario !== 'idle')`;
    - `canInject = !draining`;
    - `canStopNow = running`;
    - `canDrain = running && !draining`;
    - `canLeave = running`;
    - `canFault = running && !draining`.
  - `createConsoleStore()`:
    - `type ConsoleState = { draining: boolean; setDraining(value: boolean): void }`. Task 15 extends it additively.
    - `SimulatorStoreProvider` and `useConsoleStore<T>(selector: (s: ConsoleState) => T): T` follow the `store/ui.ts` pattern.
  - `SimulatorConsole({ lot }: { lot: ReactNode })`:
    - Two columns from `lg`: controls on the left, `lot` and (Task 15) the log on the right.
    - It clears `draining` once status reports `running: false`.
    - A 409 or 422 shows a `Banner` with `errors.<code>`, and field errors go through `applyApiError`.
  - Panels:
    - `StatusPanel({ mode, result }: { mode: ConsoleMode; result?: StatusResult })`: the scenario name, the badge, `rawSeed` with a copy button, speed ×N, virtual time and started-at in Santo Domingo, cars inside, and "Lot reached full".
    - `StartForm({ disabled }: { disabled: boolean })`: a number field for speed, not a slider.
    - `StopControls({ canStopNow, canDrain }: { canStopNow: boolean; canDrain: boolean })`: "Stop after cars leave" sets `draining` on 202.
    - `NotConfiguredPanel()`.
  - `page.tsx`: calls `requireAdmin` and renders `SimulatorConsole` with `lot={<LotSection variant="compact" />}`. There is no `loading.tsx` (CC-7).
  - Keys:
    - `simulator.title`, `refreshNote`, `adminLink`, `notConfigured`, `unreachable`;
    - `simulator.status.{title,running,idle,draining,scenario,speed,seed,copySeed,copied,virtualTime,carsInside,startedAt,reachedFull}`;
    - `simulator.scenario.{idle,weekday,weekend_peak,fill_to_full,sensor_drift}`;
    - `simulator.start.{title,scenario,speed,seed,startAt,startAtHint,submit,disabledRunning}`;
    - `simulator.stop.{title,now,drain,drainingHint}`.

- [ ] **Step 1: Write the failing tests**

```ts
// seed.test.ts
test('a seed above 2^53 round-trips exactly')        // extractRawSeed('{"data":{"seed":9007199254740993,…}}') === '9007199254740993';
                                                     // toStartBody({scenario:'weekday', speed:30, seed:'9007199254740993', start_at:''}) === '{"scenario":"weekday","speed":30,"seed":9007199254740993}'
test('start_at is sent as Santo Domingo RFC 3339')   // '2026-11-24T07:00' -> '"start_at":"2026-11-24T07:00:00-04:00"'
test('startFormSchema bounds seed and speed')        // seed 'abc' -> 'integer'; '9223372036854775808' -> 'max:9223372036854775807'; speed 0 -> 'min:1'
// controls.test.ts
test('idle allows start and inject only')
test('a running non-idle scenario blocks start')     // weekday running -> canStart false
test('the implicit idle session still allows start and inject') // running true, scenario 'idle' -> canStart true, canInject true
test('draining leaves only Stop now and Leave')      // canStopNow true, canLeave true, the rest false
test('consoleMode maps 404 and simulator.unauthorized to notConfigured, network to unreachable')
// api.test.ts / client.test.ts
test('apiFetch sends bodyText verbatim with CSRF')   // init body === the exact string; X-CSRF-Token set
test('fetchStatus keeps the raw seed digits')
// SimulatorConsole.test.tsx (stubFetch, fake timers)
test('shows the not configured panel on 404')        // 'El simulador no está configurado en este servidor.'
test('polls status every 2 s while visible')         // 1 call; +2_000 -> 2 calls
test('a start 409 shows the translated banner')      // errors.simulator.already_running text, no raw code
test('draining disables Start and Stop after cars leave, keeps Stop now, and clears when not running')
test('the status panel shows a seed above 2^53 digit for digit') // status response text with "seed":9007199254740993 -> panel text '9007199254740993'
test('a successful start refetches status at once')          // after 202, a status call before the 2 s tick
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/web -- src/features/simulator src/lib/api/client.test.ts`
Expected: FAIL (modules missing).

- [ ] **Step 3: Implement** the `apiFetch` extension, `seed.ts`, `controls.ts` (holding `deriveControls` and `consoleMode`), the API, the store and provider, the queries, the panels, the page, the landing link and the keys. Follow the mockup section 5 for the layout and copy.

- [ ] **Step 4: Verify**

Run: `npm run check:i18n && npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web && npm run build -w apps/web`
Expected: all pass.

- [ ] **Step 5: Accessibility pass.** Keyboard + NVDA on the console: every control is reachable, the disabled reasons are text, the banner is announced, and the copy button has a label.

- [ ] **Step 6: Commit**

```bash
git add apps/web
git commit -m "feat(web): add the simulator console page with status, start and stop"
```

---

## Task 15: Console UI, part 2 — inject, held cars, leave, fault, event log (slice S3, about 400 lines)

**Files:**
- Create: `apps/web/src/features/simulator/{InjectForm.tsx,HeldCars.tsx,FaultForm.tsx,EventLog.tsx,log-cursor.ts,log-cursor.test.ts,actions.test.tsx,EventLog.test.tsx}`
- Modify: `apps/web/src/features/simulator/{api.ts,store.ts,queries.ts,SimulatorConsole.tsx}`, `apps/web/src/lib/format/time.ts` (add `formatLogTime`), `apps/web/src/i18n/messages/{es-DO,en}.json` (`simulator.inject.*`, `held.*`, `fault.*`, `fault.kind.*`, `log.*`)

**Interfaces:**
- Consumes: Task 14 store, queries, `deriveControls`, `apiFetch`; Task 1 `injectCarSchema`, `sensorFaultSchema`, `simulatorEventSchema`, `injectCarResultSchema`, `emptyDataSchema`, `CAR_ID_PATTERN`, `SPACE_CODE_PATTERN`.
- Produces:
  - API:
    - `injectCar(values: InjectCarValues): Promise<string>` returns `car_id`;
    - `leaveCar(carId: string, loseTicket: boolean): Promise<void>`;
    - `setFault(code: string, fault: SensorFaultValues['fault']): Promise<void>` (expects 204);
    - `fetchEvents(after: number): Promise<SimulatorEvent[]>` uses `parseEnvelope(z.array(simulatorEventSchema))`, because this envelope has no `meta.page`.
  - `formatLogTime(at: string, locale: Locale): string`: `HH:mm:ss`, 24 h, Santo Domingo (mockup 5).
  - Log cursor (M2B-31):
    - `MAX_LOG_ROWS = 500`.
    - `type LogRow = { kind: 'event'; event: SimulatorEvent } | { kind: 'gap'; from: number; to: number } | { kind: 'run'; scenario: string; startedAt: string }`.
    - `type LogState = { cursor: number; resetPending: boolean; startedAt: string | null; rows: LogRow[] }`, `initialLogState: LogState`.
    - `nextAfter(state: LogState): number` returns 0 while `resetPending`, otherwise `cursor`.
    - `applyEvents(state: LogState, after: number, events: SimulatorEvent[]): LogState`:
      - It inserts a gap row `{from: after + 1, to: first.seq − 1}` when `first.seq > after + 1`.
      - It puts the newest rows first, caps them at 500, sets `cursor` to the max seq, and clears `resetPending`.
    - `markPollFailed(state: LogState): LogState` sets `resetPending`.
    - `noteRun(state: LogState, status: SimulatorStatus): LogState` adds a run row when `started_at` changes. It never resets the cursor.
  - `ConsoleState` gains `heldCars: string[]`, `addHeld(id: string): void`, `removeHeld(id: string): void`, `clearHeld(): void`, `log: LogState` and `setLog(next: LogState): void`. Held cars are cleared when status goes from running to not running, not when `started_at` changes, because an idle takeover keeps its cars (M2B-44).
  - `useSimulatorEvents()` polls every 2 s while visible with `nextAfter(log)`. On success it runs `applyEvents`. A failure of this poll or of the status poll runs `markPollFailed`.
  - Forms:
    - `InjectForm({ disabled }: { disabled: boolean })`:
      - credential radio (ticket / employee card); the card field appears for employee card (`EMP-001` placeholder);
      - "Hold inside until released"; dwell minutes (optional).
      - When `hold` is set, success adds the car id to Held cars.
    - `HeldCars({ disabled }: { disabled: boolean })`:
      - one row per id with "Lost ticket" and "Leave";
      - a free-text car id field checked against `CAR_ID_PATTERN` (rule `pattern`);
      - a 404 `simulator.car_not_found` removes the row and shows a banner.
    - `FaultForm({ disabled }: { disabled: boolean })`:
      - code field (`SPACE_CODE_PATTERN`, rule `pattern`), fault select (`fault.kind.stuck_occupied|stuck_free|silent|none`);
      - a 204 shows the status text `simulator.fault.applied` with `{code}`;
      - a 422 `known_space` maps to the field.
  - `EventLog()`:
    - `<table>` with a caption and the headers #, time, device, type, result, reason, car; newest first.
    - `reason` uses `errors.<reason>` when the catalog has it, otherwise the raw code in monospace (M2B-35).
    - A `result` containing `deny` gets a "Denied" text badge.
    - Gap and run rows; the empty state `simulator.log.empty`.
    - One visually hidden `role="status"` with `simulator.log.newEvents` (`{count}`), at most every 10 s (`LOG_ANNOUNCE_INTERVAL_MS = 10_000`).
    - The table itself is not a live region (design §7.3).

- [ ] **Step 1: Write the failing tests**

```ts
// log-cursor.test.ts
test('the cursor is kept across started_at changes')        // cursor 1846, noteRun with a new started_at -> cursor 1846, a run row added
test('a failed poll then a success restarts from 0')        // markPollFailed -> nextAfter 0; applyEvents(…, 0, [seq 1, 2]) -> cursor 2, resetPending false
test('a gap row when the first seq skips')                  // after 1796, events from 1841 -> gap {from 1797, to 1840}
test('newest first and capped at 500 rows')
// actions.test.tsx (stubFetch on /api/admin/simulator/*, status fixtures)
test('inject is enabled while idle and a held car is listed') // idle status; POST cars body {credential:'ticket', hold:true} -> {car_id:'car-000012'} -> row 'car-000012'
test('employee card requires the card number')              // validation 'required' under the card field, no request
test('leave sends lose_ticket and removes the row')         // POST cars/car-000012/leave body {lose_ticket:true}
test('leave 404 car_not_found removes the row with a banner')
test('a typed car id must look like car-000123')            // 'car-12' -> validation.pattern text, no request
test('fault 204 shows Falla aplicada en E017')
test('fault 422 known_space maps to the code field')
test('held cars clear when the run ends')                   // status running -> not running
test('inject, leave and fault refetch status at once')       // a status call follows each success before the 2 s tick
// EventLog.test.tsx
test('denials carry a Denegado text badge')
test('reasons are translated when known and raw otherwise') // gate.lot_full -> 'El parqueo está lleno.'; 'weird.code' in <code>
test('the table has a caption and column headers')
test('the empty log says No events yet. Start a scenario.')
test('new events are announced at most every 10 s')
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/web -- src/features/simulator`
Expected: FAIL (modules missing).

- [ ] **Step 3: Implement** the API calls, the log cursor, the store extension, the events query, the three forms, the log and the keys. Wire them into `SimulatorConsole`, gating each control with `deriveControls`.

- [ ] **Step 4: Verify**

Run: `npm run check:i18n && npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web && npm run build -w apps/web`
Expected: all pass.

- [ ] **Step 5: Run the console against compose.**
  1. Start `weekday` at speed 30.
  2. Inject a held car, then set `stuck_occupied` on a free space.
  3. Leave the held car, then stop with drain.
  Expected: each step shows its status text, the log fills newest first, and the copied seed replays the same run.

- [ ] **Step 6: Accessibility pass.** NVDA on the log: the caption and headers are read, "Denegado" is read as text, the new-events status speaks at most every 10 s, and every form error is announced.

- [ ] **Step 7: Commit**

```bash
git add apps/web
git commit -m "feat(web): add inject, held cars, sensor faults and the event log to the console"
```

---

## Task 16: Docs sync and the M2 demo script (slice D1, about 200 lines)

**Files:**
- Create: `docs/m2-demo-script.md`
- Modify: `docs/spec.md` §8 M2 (the weekday box and the Demo line) and §10.2 (new item 41), `docs/architecture.md` §12, §13 and §16

**Interfaces:**
- Consumes: the working clients and console from Tasks 1–15.
- Produces: `docs/m2-demo-script.md`, which the M2 Track B PR description references.

- [ ] **Step 1: Run the full gate**

Run: `npm run check:react && npm run check:i18n && npm run check:tokens && npm run check:layout && npm run test:scripts && npm test -w packages/contracts && npm run typecheck -w packages/contracts && npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web && npm run build -w apps/web && npm test -w apps/mobile && npm run lint -w apps/mobile && npm run typecheck -w apps/mobile && (cd apps/mobile && npx expo install --check) && (cd apps/server && TEST_DATABASE_URL='postgres://postgres:test@localhost:55432/postgres?sslmode=disable' go test -race -run 'TestAvailability_PassToSSELatency' ./cmd/api/)`
Expected: every command exits 0. The Go test needs the test Postgres from the M2 backend plan's Global Constraints: `docker run -d --name mypark-pg-test -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:18`.

- [ ] **Step 2: Write `docs/m2-demo-script.md`** in English, following `cognitive-doc-design`:
  - **Setup:**
    - `docker compose -f deploy/compose/docker-compose.yml up -d`;
    - the web at `http://localhost:3000`;
    - the phone in Expo Go with `EXPO_PUBLIC_API_URL=http://<LAN-IP>:8080`;
    - the seeded admin and a driver account.
  - **Steps:** the eight steps of design §12.4, verbatim in substance, each with its "Expect" column. Step 2 times the web and phone counts by eye against the console log (< 2 s).
  - **Notes:**
    - the shared SSE bucket (Y2, R1), with the IPs recorded in Tasks 7 and 10;
    - the map and the count can differ on purpose (M2B-15);
    - step 6's 12 real minutes (M2C-13).
  - **Accessibility check:** NVDA on the home page and the console, TalkBack on Welcome, Inicio and the full map, and the 360 dp device check (M2B-52).

- [ ] **Step 3: Run the demo end to end.** Record any step that deviates and fix the owning code first.
Expected: all eight steps behave as listed.

- [ ] **Step 4: Update `docs/spec.md` §8 M2.**
  - Tick the weekday box only when all three of these are true:
    1. the four frame-to-screen tests pass:
       - web: *frame to screen: a pushed availability frame updates the count without a timer* (Task 8) and *…a spaces delta changes the cell state…* (Task 9);
       - mobile: the same pair (Tasks 11, 12);
    2. `TestAvailability_PassToSSELatency` passes (Step 1);
    3. demo steps 2–5 ran on both clients (Step 3).
  - If any of them is missing, leave the box unticked and add a dated note naming what is missing.
  - Replace the Demo line's "The demo needs Track B (console, web and mobile maps)" with "Demo run on <date>: every step in `docs/m2-demo-script.md` passed on web, phone and console".

- [ ] **Step 5: Add `docs/spec.md` §10.2 item 41.** It says that the M2 client calls M2B-7 to M2B-52 are accepted as listed in the M2 clients design (`docs/superpowers/specs/2026-11-15-m2-clients-availability-console-design.md` §2), including the visual-direction decisions M2B-46 to M2B-52. It also records FR-AVAIL-6 hidden until M3 (design §15), and the seven plan decisions at the top of this plan (Inicio route, admin sidebar, fixtures entry, shared pure rules, no-session console response, no draining fixture, when held cars clear).

- [ ] **Step 6: Update `docs/architecture.md`.**
  - §12: the shell (sidebar and top bar), the `features/lot` and `features/simulator` folders, and the i18n namespaces `lot`, `availability`, `spaces`, `tariff`, `money`, `simulator` plus `common.nav` additions.
  - §13: the mobile `nav.home` key and the `check:layout` guard.
  - §16: the `FakeEventSource` and `controllableStream` doubles in place of MSW (M2B-41), and the frame-to-screen tests.

- [ ] **Step 7: Verify nothing else moved**

Run: `npm run check:i18n && npm run check:tokens && git status --porcelain`
Expected: the checks pass, and only the three docs appear.

- [ ] **Step 8: Commit**

```bash
git add docs/m2-demo-script.md docs/spec.md docs/architecture.md
git commit -m "docs: add m2 demo script and record client decisions"
```

---

## Task dependencies

| Task | Design slice | Depends on |
|---|---|---|
| 1 contracts schemas | C1 | — |
| 2 live rules | C2 | 1 |
| 3 tokens, navy buttons | V1 | — |
| 4 web shell | V2 (web) | 3 |
| 5 mobile shell | V2 (mobile) | 3 |
| 6a shared copy, parity | W1 (part 1) | — |
| 6b env, format, compose, BFF | W1 (part 2) | 1, 6a |
| 7 web stream | W2 | 2, 6b |
| 8 web home | W3 | 7, 4 |
| 9 web map | W4 | 8 |
| 10 mobile stream | M1 | 2, 6a (shared keys) |
| 11 mobile screens | M2 | 10, 5 |
| 12 mobile map, tabs | M3 | 11 |
| 13 console BFF | S1 | 1 |
| 14 console UI 1 | S2 | 13, 9 (the compact block includes the map; design §13 over R6) |
| 15 console UI 2 | S3 | 14 |
| 16 docs, demo | D1 | all |

## Self-review notes

- **Spec coverage:**

  | Design section | Tasks |
  |---|---|
  | §1.2 G1 (live block for anyone) | 8, 11 |
  | §1.2 G2 (frame to screen) | 8, 9, 11, 12 |
  | §1.2 G3 (stale, backoff, 429, 404) | 2, 7, 10 |
  | §1.2 G4 (map without color, one summary) | 9, 12 |
  | §1.2 G5 (console) | 14, 15 |
  | §1.2 G6 (token never in browser; drivers denied) | 13 |
  | §1.2 G7 (locales, AA pairs) | 3, 6a, every UI task |
  | §1.4 anti-goals | 2 (version guard, URL), 7/10 (stale, reconnect), 8/11 (announcer, API down), 9/12 (map independent), 13 (token, role) |
  | §2 M2B-1, M2B-46–M2B-52 (placement, look, tabs, preview, tokens, copy, device layout) | 3, 4, 5, 8, 11, 12 |
  | §2 M2B-2–M2B-6 (transport, reconnect owner, map, M3 hide, stale, console scope) | 2, 7, 10, 13–15 |
  | §2 M2B-7–M2B-15 | 1, 2, 7, 8, 10 |
  | §2 M2B-16–M2B-22 | 6b, 7, 8, 10, 11 |
  | §2 M2B-23–M2B-26 | 6b, 9, 11, 12 |
  | §2 M2B-27–M2B-36, M2B-44, M2B-45 | 13, 14, 15 |
  | §2 M2B-37, M2B-38 (superseded parts) | 4, 12 (`(tabs)/home`, sidebar), 14 (landing link) |
  | §2 M2B-39 (CORS, `API_PUBLIC_URL`, no CSP change) | 6b |
  | §2 M2B-40, M2B-41, M2B-42, M2B-43 | 3; Global Constraints; 8, 9, 11, 12; slicing |
  | §3.2 Y1–Y11 | 7 (Y1), 7/10 builder checks (Y2), 7 docs (Y3), 8 (Y4), 6b (Y8), 14 (Y7), 12 (Y9), 3 (Y10), 6a copy (Y11) |
  | §4.1–§4.4 screens and states | 4, 8, 9, 11, 12, 14, 15 |
  | §5.1–§5.6 stream | 2, 7, 10 |
  | §6 data flow | 7, 8, 10, 11 |
  | §7 console | 13, 14, 15 |
  | §8 contracts | 1, 2 |
  | §9 design system | 3, 4, 5, 9, 12 |
  | §10 i18n | 4, 6a, 11, 14, 15 |
  | §11 accessibility | 8, 9, 11, 12, 15; each task's pass |
  | §12.1 automated tests | the owning tasks; M1 restyle in 3; layout in 5, 11, 12; scripts in 3, 5, 6a |
  | §12.2 frame to screen | 8, 9 (web), 11, 12 (mobile) |
  | §12.3, §12.4 exit and demo | 16 |
  | §13 docs table | 3, 4 (M1 design note), 6b (compose), 7 and 13 (architecture §5, §12), 12 (§13), 16 (spec, demo) |
  | §14 builder checks | 7 (pagehide, IP), 10 (expo/fetch, TextDecoder, Intl, IP), 5, 11 and 12 (safe areas, device), 6b (DOP output) |
  | §15 changes against spec | 16 |

- **Fixes made during review:**
  1. `Connection.spacesReady` is defined in Task 7 instead of being patched in Task 9.
  2. `AvailabilityFigures` is defined in Task 2 instead of Task 8.
  3. The design's `(tabs)/index.tsx` collides with `app/index.tsx`, so the route became `(tabs)/home/`.
  4. V2 was split into two tasks.
  5. `/control/events` uses `parseEnvelope(z.array(…))`, because `parseListEnvelope` requires `meta.page`.
  6. W1 is split into Task 6a (copy, parity check) and Task 6b (env, formatting, compose, BFF). The copy alone is about 280 JSON lines, so one slice would pass 400, and 6b keeps the sensitive `lib/bff` change small. 6b is marked sensitive.
  7. `apiFetch` gains `bodyText` and the raw text, so seed digits survive both directions.
  8. Mobile icons are drawn with `View`s, because neither `@expo/vector-icons` nor an SVG package is a dependency.
  9. `nav.home` moved into Task 11, which registers the tab, so no slice shows a raw key.
  10. Task 12 lists both `mapHref` call sites, and `LotMapScreen` takes an explicit `edge` prop.
  11. Added two design requirements that had no test: mutations invalidate status (§7.3; Tasks 14, 15), and the seed above 2^53 round-trips in the status panel display, not only in the start body (§12.1; Task 14).
  12. The proxy body already treats every `/api/` path as an API route, so Task 13 needs only the matcher line.
- **Type consistency.** These names are used unchanged from their defining task onward:
  - From Task 1: `LotStatus`, `SpaceStatus`, `CONSOLE_SCENARIOS`, `CAR_ID_PATTERN`, `SPACE_CODE_PATTERN`.
  - From Task 2: `SseFrame`, `LiveFrame`, `LiveState`, `ConnectionStatus`, `AvailabilityFigures`, `AnnounceInput`, `lotQueryKeys`.
  - From Task 5: `SCREEN_GUTTER`.
  - From Task 7 (and 10 for mobile): `Connection { status, lastFrameAt, spacesReady }`, `acquireLiveLot`, `resetLiveLotsForTests`.
  - From Task 8: `LotSnapshot`, `LotLive` (web `variant`; mobile `actions`, then `mapHref` from Task 12).
  - From Task 14: `StatusResult`, `ConsoleState` (extended additively in Task 15), `deriveControls`.
- **Review Focus:** each item has its test in the owning task: 1 → Tasks 2 and 10; 2 → Tasks 2 and 10; 3 → Tasks 6b and 7; 4 → Task 13; 5 → Tasks 6b and 11.
- **Step scan:** no TBD, "handle edge cases" or untested step was found. Every code step names its signature and file, and every run step names the command and the expected result.
- **Proportion:** the plan is 1,666 lines against the design's 631 (about 2.6×). By words it is about 16.2k against 12.8k (about 1.3×). The extra lines come from one-item-per-line formatting, not from code. The plan states signatures, rules, values and test assertions. The only bodies are the backoff and cell-size formulas, which the design fixes, and the exact JSON strings the seed tests pin.
