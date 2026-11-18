# MyPark — session start

Real-time parking availability and reservations for the Banreservas Zona Colonial lot (Santo Domingo).
Monorepo: Go API + worker + simulator (`apps/server`), Next.js web (`apps/web`), Expo mobile
(`apps/mobile`), shared contracts (`packages/contracts`), compose in `deploy/compose`.

## Where we are (2026-11-18)

| Milestone | Theme | Status |
|---|---|---|
| M0 | Foundations | Done |
| M1 | Auth + vehicles (backend + clients) | Done, on `main` |
| M2 | Live availability, gates, simulator, console | Done, on `main` (`3db159e`) |
| M3 | Reservations + payments | Track A (backend) done on `feat/m3-backend`, **not merged**. Track B (clients) **next**. Design: `docs/superpowers/specs/2026-11-16-m3-backend-reservations-payments-design.md`; plan: `docs/superpowers/plans/2026-11-16-m3-backend-reservations-payments.md` |
| M4 | Admin + analytics + tariff config | Not started |
| M5 | Hardening + demo | Not started |

Dates and scope per milestone: `docs/spec.md` §8.

### M2 leftovers (human or follow-up)

- Run the demo: `docs/m2-demo-script.md` (marked "written, not yet run"). Afterwards tick spec §8 weekday box
  and replace the Demo line.
- Re-run Go `TestAvailability_PassToSSELatency` (needs a fresh postgres:18 container).
- Device/browser checklist in `docs/m2-demo-script.md` (TalkBack, NVDA, iPhone notch, font scale 1.3,
  360 dp, 200% zoom, Hermes `expo/fetch` streaming).
- Known limits and deferred minors: `docs/spec.md` §10.2 item 41.
- Compose changed the API CORS default (Track A block) — mention it to the backend owner.

### M3 leftovers (human or follow-up)

- Both developers approve the contract changes made after the S1 handoff (policies §2.2): `SimulatorEvent` gained optional `credential` and `credential_ref`; `VALIDATION_RULES` gained `luhn`, `number`, `card_expiry`, `cursor`, `numeric` and `idempotency_key`.
- Final whole-branch review of `feat/m3-backend` is pending. Then the user decides on merge or push.
- Track B (web + mobile): reservation screens, pay form with the three test cards, the decline (`402`) and timeout (`202` poll) paths, QR screen, receipts. Add the m3 zod schemas over the S1 fixtures.
- Live `no_show` takes up to 25 real minutes (design R1); the demo uses a CI run or starts it first.
- M4 hooks: lot closure (FR-RES-15, `facility_closed`), the reconciliation job and views, admin refund and cancel override. The admin refund also settles late-charge alerts (`refund_failed`, `details.reason` `duplicate_charge`, `orphan_charge`, `gateway_inconsistent`). The admin tariff and policy forms must validate `max_active_per_user = 1`, `slot_min = 5` and a reservation fee above zero (M3B-48, M3B-49).
- Accepted, not fixed (spec §10.2 #43): the no-show block is read unlocked in create; the pay key fingerprint excludes the full card number; NFR-TEST-1 is measured as statement coverage (at least 90%).
- **Two CI-equivalent checks fail on this branch and will fail the PR; the user must decide before merge.** Neither comes from M3: `git diff main -- apps/mobile/package.json package.json package-lock.json` is empty.
    - `npx expo install --check` wants expo ~58.0.26, expo-router ~58.0.32 and jest-expo ~58.0.15 (upstream patch releases).
    - `npm audit --omit=dev --audit-level=high` reports GHSA-fc5q-39xc-vjpf (picomatch ReDoS) through the jest chain. `npm audit fix --force` would be a breaking downgrade (expo 52, jest 29), so it was not applied.
- Deploy: worker before API (an old worker voids reservation stays without restoring the hold). `SEED_SIMULATOR_DRIVERS` must be `false` in production.
- Phase 2: a real gateway adapter must guarantee one refund per charge across refund keys, and needs a timeout-to-alert rule for payments stuck `pending`.

## Key docs (read before designing)

- `docs/spec.md` — requirements, business rules, milestones, decision register (§10).
- `docs/architecture.md` — system design. `docs/policies.md` — review, security, commit policy.
- `docs/superpowers/specs/*-design.md` — per-milestone designs (+ HTML mockups).
- `docs/superpowers/plans/*.md` — per-milestone implementation plans.

## How we work

1. **Design** a milestone slice with `superpowers:brainstorming` → spec in `docs/superpowers/specs/`
   (+ an HTML mockup for UI; user approves the visual direction).
2. **Plan** with `superpowers:writing-plans` → `docs/superpowers/plans/`.
3. **Execute** with `superpowers:subagent-driven-development` on a `feat/<milestone>-<area>` branch:
   one implementer per task, a task-scoped review after each (opus for security/concurrency),
   fix rounds, then a final whole-branch review and one fix wave. Workspace in `.superpowers/sdd/`
   (git-ignored, deleted when done).
4. **Finish**: run the CI-equivalent checks below, then ask the user before merge/push.

Contract-first: backend publishes endpoints in `apps/server/api/openapi.yaml`, regenerate with
`npm run gen:contracts`, clients build against `packages/contracts`.

Skills per area: Supabase/Postgres skills for DB, Go skills for the server, frontend/Expo/uniwind
skills for UI.

## Rules

- Conventional commits. **No Co-Authored-By or AI attribution** of any kind.
- Never push, merge or force-push without the user's explicit OK.
- Mobile screens go through `ScreenScroll` (gutter + safe areas); no horizontal overflow at 360 dp
  (`check:layout` enforces it).
- UI copy only via i18n (es-DO default, en); web/mobile shared keys must stay in parity.
- Big integers from the simulator (seeds) stay as raw text, never `Number`.
- npm workspaces only; add deps by editing `package.json`, then `npm install`.

## Checks (CI-equivalent)

```bash
npm run check:react && npm run check:i18n && npm run check:layout && npm run check:tokens && npm run test:scripts
npm run typecheck -w packages/contracts && npm run typecheck -w apps/web && npm run typecheck -w apps/mobile
npm run lint -w apps/web && npm run lint -w apps/mobile
npm test -w packages/contracts && npm test -w apps/web && npm test -w apps/mobile
npm run build -w apps/web
(cd apps/mobile && npx expo install --check)
npm audit --omit=dev --audit-level=high
```

Go (when `apps/server` changes): `go vet ./...`, `go test -race ./...` (run in Docker `golang:1.27`,
DB at `host.docker.internal:55432`), sqlc diff, govulncheck, and golangci-lint v2.15.0:

```bash
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)/apps/server":/src -w /src golangci/golangci-lint:v2.15.0 golangci-lint run ./...
```

## Environment notes (Windows + Git Bash)

- Compose locally needs `POSTGRES_PORT=5433`. Stack: `docker compose -f deploy/compose/docker-compose.yml up -d --build`.
- `TZ=` prefixes don't survive Git Bash/npm; tests set the time zone in-process.
- LSP diagnostics are often stale — trust typecheck/build.
- CI runs only on pushes to `main` and on PRs.
