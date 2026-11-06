# MyPark — Policies

There are two kinds of policy in this document. **Business policies** (§1) define how the service treats drivers, money and data. **Engineering policies** (§2) define how we build and ship. Business values are defaults that admins can change (versioned, see `spec.md` §5). Engineering policies change only through a PR to this file.

> Items tagged **(our call)** were not explicitly decided by the product owner and should be reviewed.

---

## 1. Business policies

### 1.1 Reservations

| Policy | Rule |
|---|---|
| What a reservation buys | A **guaranteed pooled spot** during the hold window `[arrival − 15 min, arrival + 20 min]`. It never guarantees a specific space |
| Eligibility | Verified, active driver account with at least one registered vehicle **(our call)** |
| Limits | 1 active reservation per driver; bookings up to 7 days ahead; at most 30% of **effective** capacity (spaces in service) reserved at any instant. A drop in capacity never cancels existing confirmed reservations **(our call)** |
| Versions | The tariff and policy in effect **at booking** apply to the reservation and its stay **(our call)** |
| Payment | Flat reservation fee (default RD$150) charged upfront. A reservation is confirmed only after payment succeeds. Unpaid reservations expire after 10 min **(our call)** |
| Entry | The driver presents the QR at the entry barrier inside the hold window. Earlier than 15 min before arrival is denied as a reservation, but the driver can enter as a drive-up if there is room |
| Hourly charges | The reservation fee is separate from and **not credited** to hourly parking charges, which apply from check-in |
| Transfer | Reservations can't be transferred to another account. The vehicle can be changed until the booked arrival time **(our call)** |

### 1.2 Cancellation and refunds

| Situation | Outcome |
|---|---|
| Driver cancels **before** the booked arrival time | Full refund of the reservation fee |
| Driver cancels **at or after** the booked arrival time | No refund |
| No-show (not checked in by arrival + 20 min) | Reservation expires and the fee is **forfeited** |
| Driver presents the QR within the window while the lot is physically full | Entry denied (`gate.lot_physically_full`); the system cancels the reservation **immediately** (`facility_unavailable`), with a full refund and an incident logged **(our call)** |
| Lot closed by the administration after booking | System cancellation of every overlapping reservation (`facility_closed`), with a full refund and an email to the driver **(our call)** |
| Admin cancellation | Refund per the rules above, unless the admin overrides with a written reason (audited) |
| Payment failure | No charge is made. The driver may retry until the payment timeout |

Refunds go back to the original payment method (simulated in Phase 1). They are tracked separately from the payment (pending → succeeded/failed, retried automatically). The driver sees the refund status on the reservation and receives an email. A refund that fails after all retries is escalated to an admin.

### 1.3 Tariffs

| Policy | Rule |
|---|---|
| Structure | First hour RD$50; each additional hour or fraction RD$25; free if the whole stay is ≤ 10 min; daily cap RD$400 per rolling 24 h block from entry; lost ticket RD$500 (replaces the first 24 h time charge) |
| Exit after paying | The driver has 15 min after paying to exit. Extra time after that is billed **(our call)** |
| Zero-due exits | Grace stays (≤ 10 min) and employee stays exit without visiting the pay station. A RD$0 payment record is kept for traceability **(our call)** |
| Changes | Only admins change tariffs. Each change is a new version with an effective-from time. **Running stays and confirmed reservations keep the version they started with** |
| Transparency | Current rates are always shown publicly on web and mobile before entry and before reserving |
| Currency | Dominican pesos (DOP). Amounts are shown as `RD$` and stored as integer centavos |

### 1.4 Employee benefit

- Facility employees with an **active employee card** park for free (RD$0) on card-based stays.
- Employee vehicles **count toward capacity** like any other vehicle and are denied entry when public availability is 0.
- Cards are registered and revoked only by admins. A revoked card is denied at the next presentation.
- The benefit covers card stays only. An employee who books a reservation through the app pays the normal reservation fee **(our call)**.
- Card misuse (lending the card, sharing it with non-employees) is grounds for revocation. Card stays are visible in the audit trail.

### 1.5 Availability information

- Public availability is informational. Only a confirmed reservation guarantees a spot.
- The server sends a heartbeat every 15 s. When the connection drops or no event or heartbeat arrives for **45 s**, clients mark the data **stale**, show a "last updated" time and must not present the number as live **(our call)**.

### 1.6 Accounts

| Policy | Rule |
|---|---|
| Registration | Self-service for drivers with email verification. Admins are created only by the seed or by invitation from another admin |
| Accuracy | Drivers must register vehicles they are authorized to drive. A plate can be active on only one account |
| Suspension | Admins may suspend accounts for abuse (e.g. repeated no-shows, fraudulent payment attempts). Suspended accounts keep read access to their history but cannot reserve |
| Repeated no-shows | Three no-shows within 30 days blocks new reservations for 7 days **(our call, configurable)** |
| Deletion | A driver may delete their account at any time. Personal data is anonymized (§1.8). Financial records are kept in anonymized form |

### 1.7 Acceptable use

The following are prohibited: automated scraping or reservation bots; attempts to forge QR codes or device signatures; abusing the simulated payment gateway outside testing; and any attempt to access other users' data. Violations lead to suspension and, where applicable, are reported to the institution.

### 1.8 Privacy and data protection

MyPark processes personal data under **Dominican Republic Law No. 172-13 on the comprehensive protection of personal data**. A legal review is **required before any production use** (Phase 2). Phase 1 uses test data only.

| Principle | How MyPark applies it |
|---|---|
| Purpose limitation | Data is used only to provide availability, reservations, access and billing, and to compute aggregate demand analytics |
| Data minimization | We collect name, email, optional phone, vehicle plate/make/model/color and payment metadata (provider reference, brand, last 4). **No raw card data** and no location tracking |
| Consent and notice | Registration requires accepting the privacy notice and terms (versioned, with acceptance timestamp stored) |
| Data subject rights | Access (profile + reservations + payments view), rectification (edit profile/vehicles), deletion (account deletion → anonymization), and objection via support contact |
| Security | See §2.7. Access to personal data in the admin area is role-restricted and audited |
| Analytics | Only aggregate occupancy (no personal identifiers) is kept indefinitely |

**Retention (our call, configurable):**

| Data | Retention |
|---|---|
| Account profile | While the account exists; anonymized within 30 days of deletion |
| Reservations, stays, payments | 5 years (financial record-keeping), with personal fields anonymized after account deletion |
| Device events | 12 months raw, then dropped (aggregates kept) |
| Occupancy snapshots | Indefinite (non-personal) |
| Audit log | 2 years |
| Refresh tokens / auth codes | Purged 30 days after expiry |
| Emails in outbox | 90 days |

### 1.9 Brand use

The facility belongs to Banreservas. The product is **MyPark**, shown as "MyPark · Banreservas Zona Colonial", and it uses the Banreservas name, logo and brand colors in web and mobile.

| Rule | Detail |
|---|---|
| Permitted scope | **Academic and demo use only**, with the knowledge of the facility staff |
| Production | Any production or public deployment (Phase 2+) requires the institution's **written authorization** |
| Assets | Only official brand assets supplied by the team are used. Colors are not sampled or recreated from screenshots. Until the assets arrive, tokens hold placeholders marked `TODO(brand)` |
| Integrity | The logo is not altered, recolored or combined with third-party marks. Accessibility adjustments use approved shades only |
| Withdrawal | If the institution objects, brand elements are removed by swapping tokens and assets; no code changes required |

---

## 2. Engineering policies

### 2.1 Version control

| Policy | Rule |
|---|---|
| Repository | `github.com/XYZLZ/MyPark`. Go module `github.com/XYZLZ/MyPark/apps/server` |
| Default branch | `main` is protected and always deployable. Branch protection requires the CI checks to pass and 1 approval, and **no direct pushes** (admins included) |
| Team | Two developers who review each other's work |
| Branch names | `<type>/<short-kebab-topic>`, e.g. `feat/reservation-cap`, `fix/sse-reconnect`, `chore/ci-cache` |
| Commits | **Conventional Commits**: `type(scope): summary` with types `feat, fix, refactor, perf, test, docs, build, ci, chore`. Scopes: `server, web, mobile, contracts, deploy, docs` |
| Attribution | **No AI attribution** in commits or PRs: no `Co-Authored-By` trailers or "generated with" lines |
| History | Squash-merge PRs into `main`. The PR title becomes the conventional commit |
| Work units | Each commit is a coherent, reviewable unit that includes its tests and docs |

### 2.2 Pull requests and review

- **Size budget:** ≤ 400 changed lines (excluding generated files, lockfiles and migrations' generated sqlc output). Bigger changes are split into **chained PRs**, each one independently reviewable and green.
- The PR description lists the requirement IDs (e.g. `FR-RES-4`), what to review first, what is out of scope, and how it was tested.
- Every PR needs **1 approval from the other developer**. Authors never approve their own PRs.
- **Sensitive paths** are migrations, auth, payments, device signatures and reservation capacity. They do not need a second human reviewer. Instead **(our call)**:
  1. The author completes the **security checklist** in the PR template (authz + ownership checks, input validation, idempotency, lock ordering and transaction scope, secrets/logging, migration safety and rollback, tests for failure paths).
  2. An **automated risk review** runs and its findings are linked or summarized in the PR (e.g. the `review-risk` agent or `/security-review`). Critical/high findings must be fixed or explicitly accepted by both developers before merge.
- Contract changes (`openapi.yaml`, error codes) after a milestone handoff need approval from the other developer, because they affect both tracks.
- Reviewers check behavior against `spec.md`, not just code style.

### 2.3 Testing

| Area | Expectation |
|---|---|
| Domain rules (fees, availability, cap, state machines, QR) | **TDD**: write the test first. ≥ 90% branch coverage |
| Use cases / adapters | Integration tests against real Postgres (no DB mocks for SQL behavior) |
| Concurrency-sensitive code | A dedicated parallel test (e.g. 200 concurrent reservation attempts) |
| HTTP handlers | Contract tests for status codes and error codes |
| Web / mobile | Component tests for containers with logic, and e2e for critical driver flows by M5 |
| Determinism | Tests use an injected clock and seeded randomness. No `time.Sleep`-based assertions |
| Flaky tests | Quarantined within 24 h with an issue, and fixed before the next milestone |

### 2.4 Database and migrations

- Migrations are **forward-only** (goose `up`). Fixes ship as new migrations. Applied migrations are never edited.
- Every schema change follows `supabase-postgres-best-practices`: identity PKs, `timestamptz`, `text` + `check`, indexed FKs, partial indexes for hot subsets, and short transactions.
- Destructive changes (drop column/table, type change) use expand → migrate → contract across separate PRs.
- The app role (`mypark_app`) never runs DDL. Append-only tables never get UPDATE/DELETE grants.
- Every new query on a large table is checked with `EXPLAIN` during review.

### 2.5 Secrets and configuration

- Secrets are **never committed**. Only `.env.example` files with placeholder values are committed.
- Local secrets live in untracked `.env` files. CI secrets live in the CI secret store.
- Keys are rotatable: JWT signing key (supports `kid`), QR HMAC key, secretbox key, device secrets.
- Logs never contain secrets, tokens, passwords, codes, full card numbers or full emails/plates.

### 2.6 Dependencies

| Policy | Rule |
|---|---|
| Package manager | **npm** with workspaces (not pnpm, not yarn). One root `package-lock.json` |
| Versions | Exact pins for framework packages (Next.js, React, Expo SDK packages via `npx expo install`). Caret ranges allowed only for dev tooling |
| React single version | `react`/`react-dom` pinned to the version required by the current Expo SDK (19.3.0 for SDK 58) in **both** apps. CI fails if `npm ls react` shows more than one version |
| Expo alignment | `npx expo install --check` must pass. Expo packages are upgraded only together with an SDK upgrade |
| Go modules | `go mod tidy` must be clean. Prefer the standard library. A new dependency needs a one-line justification in the PR |
| Audit | `npm audit --omit=dev` and `govulncheck` run in CI. High/critical findings block release |
| Licenses | Only OSI-approved permissive licenses (MIT, Apache-2.0, BSD, ISC) without approval |

### 2.7 Security baseline (OWASP ASVS Level 1)

| Control | Rule |
|---|---|
| Passwords | argon2id. Minimum 10 characters; checked against a common-password list; no composition rules |
| Sessions | 15-min access tokens; rotating refresh tokens with reuse detection; web tokens only in `httpOnly; Secure; SameSite=Lax` cookies |
| CSRF | BFF mutations require an `Origin` match + double-submit token |
| Rate limits | Login 5/min per IP+email; register / code requests 3/min per IP; reservation create 10/min per user; device API 100/s per device |
| CORS | Explicit allow-list (web origin, Expo dev origins in development only). No wildcard with credentials |
| Input | All input validated at the edge (zod in clients, explicit validation in Go). SQL only through sqlc parameterized queries |
| Authorization | Deny by default. Every use case checks role **and** ownership. Admin actions are audited |
| Device API | HMAC-SHA256 signature, ±60 s timestamp window, idempotent event ids, per-device revocation |
| Headers | `Strict-Transport-Security` (deployed), `Content-Security-Policy`, `X-Content-Type-Options`, `Referrer-Policy` |
| Errors | No stack traces or internal details in responses; only `code` + `request_id` |

### 2.8 Internationalization

- **No hardcoded user-facing strings** in web, mobile or emails. Every string is a message key in `es-DO` and `en`.
- `es-DO` is the source locale. A PR that adds a key must add it in both locales (CI checks key parity).
- The API returns **error codes**, never translated prose. New codes are added to the catalog and translated in both clients in the same PR.
- Dates use `America/Santo_Domingo`; money uses `RD$` with locale-aware formatting.

### 2.9 Accessibility

- The web targets **WCAG 2.2 AA**: semantic HTML, visible focus, 4.5:1 text contrast, full keyboard support, labels on every input, and live regions for availability changes.
- Mobile: `accessibilityLabel`/`accessibilityRole` on interactive elements, ≥ 44 pt targets, dynamic type support, no information conveyed by color alone (the lot map uses shape or labels as well as color).
- Each milestone demo includes a quick keyboard + screen-reader check of new screens.

### 2.10 Documentation

- Project documentation, code, comments and commit messages are written in **English**. The UI is bilingual es-DO/en (§2.8) **(our call)**.
- `docs/spec.md`, `docs/architecture.md` and `docs/policies.md` are the source of truth. A PR that changes behavior, architecture or policy **updates the relevant doc in the same PR**.
- `apps/server/api/openapi.yaml` is updated with any API change, and generated types are regenerated.
- New architectural decisions are added to the ADR table in `architecture.md` §17.

### 2.11 Definition of done

A change is done when:

- [ ] It meets the acceptance criteria of the referenced FR/NFR IDs.
- [ ] Tests are written first for domain rules, and all tests pass locally and in CI.
- [ ] Lint, typecheck, `sqlc` diff, i18n key parity and the React single-version check are green.
- [ ] User-facing strings exist in es-DO and en, and error codes are translated.
- [ ] Accessibility basics are verified for new UI.
- [ ] Docs and OpenAPI are updated where needed.
- [ ] The PR is ≤ 400 changed lines (or part of a chain), reviewed and approved.
- [ ] No secrets, no AI attribution, conventional commit title.
