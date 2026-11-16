# MyPark — session start

Real-time parking availability and reservations for the Banreservas Zona Colonial lot (Santo Domingo).
Monorepo: Go API + worker + simulator (`apps/server`), Next.js web (`apps/web`), Expo mobile
(`apps/mobile`), shared contracts (`packages/contracts`), compose in `deploy/compose`.

## Where we are (2026-11-16)

| Milestone | Theme | Status |
|---|---|---|
| M0 | Foundations | Done |
| M1 | Auth + vehicles (backend + clients) | Done, on `main` |
| M2 | Live availability, gates, simulator, console | Done, on `main` (`3db159e`) |
| M3 | Reservations + payments | **Next** — no design or plan yet |
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
