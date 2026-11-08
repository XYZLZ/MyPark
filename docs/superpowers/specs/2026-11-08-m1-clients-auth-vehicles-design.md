# M1 Track B — Web and Mobile Clients: Accounts + Vehicles: Design

**Status:** approved by the user on 2026-11-08, after the builder check · **Date:** 2026-11-08 · **Milestone:** M1 (Nov 16 – Nov 20) · **Track:** B (`apps/web`, `apps/mobile`, `packages/contracts`) · **Approved mockup:** [`2026-11-08-m1-clients-mockup.html`](./2026-11-08-m1-clients-mockup.html) (v3)

This document designs the two clients for the M1 backend: register, verify, log in, manage vehicles, edit a profile, delete an account, and reach the admin area. Web comes first, then mobile, on one branch.

**The three decisions that shape everything else:**

1. **`proxy.ts` is the only web code that ever calls `POST /v1/auth/refresh`.** Refresh tokens are strictly single-use, and Server Components cannot set cookies. One refresher with an in-flight lock plus a short result cache is what stops a double refresh from logging a user out (§5.3).
2. **Both clients translate stable codes and nothing else.** Every response goes through `parseEnvelope`. `message` is never shown (§7, §10).
3. **One token set, three files, one parity check.** Web and mobile have the same token names and values, enforced by CI (§9).

> **How to read this.** §1–§3 are why and what is fixed. §4 lists screens. §5–§7 are the mechanics. §8–§11 cover shared code, design, i18n and accessibility. §12–§14 cover testing, build order and what is left to the builder. User decisions are in §2.1. Every other decision made here is an **(our call)**, referenced inline as CC-n (so it doesn't collide with the backend's OC-n) and collected in the one review table in §2.2.
>
> **Sources:** `docs/spec.md` (§2, FR-AUTH, FR-VEH, FR-I18N, FR-BRAND, §6, §8), `docs/architecture.md` (§3, §5, §12, §13), `docs/policies.md`, the M1 backend design, `apps/server/api/openapi.yaml`, `packages/contracts`, the existing client code, and the pinned Next.js 16 docs in `node_modules/next/dist/docs/` and Expo SDK 58 docs. Framework behavior stated below was checked against those, not recalled.

---

## 1. Intent

### 1.1 Why

The backend is done and tested, but no person can use it yet. Three M1 exit criteria are still marked *needs Track B*. M1 also sets the patterns every later client feature will copy: session handling, the BFF, form and error handling, design tokens and primitives. Getting these right now is much cheaper than retrofitting them across M2–M4.

### 1.2 Goals

| # | Goal | Verified by |
|---|---|---|
| G1 | A driver registers, verifies by emailed code, and logs in on web and mobile | Demo script §12.3, steps 1–6 and 10 |
| G2 | A driver adds, edits and removes vehicles, shown as license plates, with every error translated | Component tests, demo script |
| G3 | Web tokens never reach browser JavaScript | Cookie tests, `/api/auth/login` response test |
| G4 | Concurrent requests never trigger a double refresh | Named `refresh-coordinator` tests (§12.1) |
| G5 | An admin reaches the admin area, and a driver gets 403 | Layout guard test, demo script |
| G6 | New terms block the app until accepted, while logout and deletion still work | Gate tests on both clients |
| G7 | Web and mobile look like one product, with the same token names and values | `check:tokens` in CI |
| G8 | Every M1 error code exists in es-DO and en, in both clients | `check:i18n` extended to the code catalog |

### 1.3 Non-goals (M1)

- Availability, Reserve, My reservations, payments. These areas are **hidden** until their milestone, with no placeholders.
- Screens to create or list admins (M4). Change password while logged in. Biometric lock. Dark mode.
- Playwright e2e (M5). Full script-level CSP (M5, CC-20).
- Official brand assets. Tokens stay marked `TODO(brand)`.

### 1.4 Anti-goals (failure even if it "works")

| Anti-goal | Guard |
|---|---|
| A token is readable from browser JavaScript | httpOnly cookies; BFF responses never contain tokens; test on the login response (§5.2) |
| A double refresh logs a user out | One refresher, in-flight lock, result cache (§5.3); mobile single-flight (§6.2) |
| A Spanish user sees English or a raw code | Codes translated from `errors.<code>`; CI checks every code; fallback key per locale (§10) |
| The password crosses screens | Only the email is carried, in memory (CC-13); forms reset on success |
| A driver sees the admin area | Server-side `requireAdmin()` in the `(admin)` layout and page (§5.6); the API enforces it again |
| Web and mobile drift apart in color | `scripts/check-design-tokens.mjs` compares all token files in CI (CC-9) |
| A refresh loop on a wrong password | Only 401 `auth.unauthenticated` triggers refresh. `auth.password_incorrect` is 403 (§6.2) |
| A transient API failure logs a user out, apart from the accepted lost-refresh-response case (§6.2) | Only `auth.refresh_invalid` and `auth.session_revoked` end a session (§5.3, §6.2) |
| Hardcoded user-facing strings | Keys in both locales; policies §2.8 |

---

## 2. Decisions

### 2.1 Made by the user (2026-11-08)

Numbering is as supplied; there is no item 21.

**Scope and order**

1. Web first, then mobile, on one branch.
2. Admin UI in M1: admin login, a basic admin landing page, and the accept-invitation page. Screens to create and list admins wait for M4. Exit criterion: an admin reaches the admin area; a driver gets 403 (server-side role guard in the `(admin)` layout).
3. Later driver areas are hidden until their milestone, with no "coming soon" placeholders. Mobile tabs in M1: Vehicles and Profile.
4. Public home before availability exists: name, hours and location if the API offers them, plus Log in and Sign up. The API-health widget goes away.

**Flows**

5. Mobile first open: one welcome screen with Log in and Sign up. No carousel.
6. After the email code is verified, go to login with the email pre-filled. The password is never carried between screens.
7. After login, land on Vehicles, with an "add your first car" prompt when empty.
8. Session lifetime as set by the backend (30 days idle, 90 days maximum). No biometric lock in M1.
9. New terms: a full screen blocks the app after login until accepted; logout and account deletion still work.

**Forms and account**

10. Sign-up is one screen; phone optional, pre-filled with +1 for the DR.
11. Terms and privacy text: short placeholder pages in both languages, marked as an academic demo.
12. Vehicle make, model and color are free text.
13. Language switch: web header, mobile Profile, and the mobile welcome screen.
14. Account deletion: Profile, danger zone, explain consequences, enter password, then back to welcome.
15. No change-password while logged in; Profile links to the forgot-password flow.

**Look**

16. Follow the impeccable rules (product register) in both clients, with the same colors in web and mobile, and a soft, not square, UI. Mockup v3 is approved.
17. Tokens from the user's Banreservas app screenshots (visual estimates, still marked `TODO(brand)` per FR-BRAND-3); radii, motion, typeface, parking-sign mark, plate rendering and web auth panel as listed in §9. Every control has default, hover, pressed, focus, loading, disabled and error states. Web and mobile tokens have the same names and values; both current token files are updated and a new `brand-action` token is added.

**Build**

18. Demo on an Android phone with Expo Go on the local network.
19. Add react-hook-form with zod in both apps.
20. Tests: component tests with mocked API responses plus a written demo script against the real backend. Playwright stays in M5.
22. Same delivery process as the backend.

**Already decided defaults**

- Web tokens only in httpOnly cookies. Single-flight refresh per session.
- Cookies `Secure` in production only, so local HTTP works.
- Field errors under the field, other errors in a banner, rate limits show a wait time. A 401 on logout counts as success.
- Fix the M0 web leftovers: `/me` and `/qr` misrouted by the locale regex (require real locale codes), map `en-US` to `en`, add a localized not-found page.
- Correct `docs/architecture.md` §5's "left-most X-Forwarded-For" wording to match the code (right-most untrusted hop) in this milestone's docs slice.

Security over convenience: refresh tokens stay strictly single-use. A refresh response lost after rotation ends the session and asks the user to log in again (§6.2). No backend replay window is added.

### 2.2 Our calls (review these)

| # | Decision | Why | § |
|---|---|---|---|
| CC-1 | **`proxy.ts` is the sole refresher**, for pages and `/api/v1/*`. It holds the in-flight lock and a 30 s result cache bound to the browser's `mp_csrf`. Trigger: the access cookie is gone but the refresh cookie exists (access cookie `Max-Age = expires_in − 60`). RSC and route handlers never refresh. **Accepted risk:** the cache is a narrow BFF-side grace window (§5.3) | Server Components can't set cookies. One entry point means one lock, with no state shared across Next bundles | §5.3 |
| CC-2 | BFF shape: a **deny-by-default allow-list** behind one catch-all `/api/v1/[...path]` that mirrors OpenAPI paths, plus a dedicated `/api/auth/login`. All mutations go through it; no Server Actions in M1 | Same model as the server's route policies. Client paths match the contract | §5.5 |
| CC-3 | CSRF cookie `mp_csrf` is set by `proxy.ts` and rotated at login. Origin is matched against a new env var, `WEB_ALLOWED_ORIGINS`. Mutating routes require both checks | Login also needs CSRF protection, so the token must exist before a session. `Host` is unreliable behind a proxy | §5.4 |
| CC-4 | `Secure` is keyed on `APP_ENV=production` (new env on the `web` service), **not** `NODE_ENV` | The web image sets `NODE_ENV=production`, so `NODE_ENV` would break local HTTP | §5.2 |
| CC-5 | The BFF forwards the incoming `X-Forwarded-For` unchanged and never invents one. Compose pins the `mypark-internal` subnet inside `172.16.0.0/12`, default `${MYPARK_SUBNET:-172.28.0.0/24}` so a clash can be overridden | See contradiction X1 in §3.2. Pinning makes the trusted-proxy assumption true instead of likely | §5.7 |
| CC-6 | A 401 seen during RSC goes to `GET /api/session/end`, inside the proxy matcher so a refresh happens first. Only `auth.session_revoked` or having no cookies ends the session | RSC can't clear cookies, and a lapsed access cookie must never cost the user a valid refresh token. A forced-logout link also can't work | §5.6 |
| CC-7 | `forbidden()` with `experimental.authInterrupts` for a real 403 on the admin area | The exit criterion says 403. A rendered panel would return 200 | §5.6 |
| CC-8 | `packages/contracts` gains strict response schemas, request/form schemas, `parseListEnvelope`, normalizers and typed fixtures. Schemas are compile-time checked against OpenAPI | One definition for both clients. `parseEnvelope` drops `meta.page`, which "2 de 5" needs | §8 |
| CC-9 | Token parity script `scripts/check-design-tokens.mjs`. `brand-primary-contrast` stays `#ffffff`, `brand-accent-contrast` becomes the `ink` value. Pass/fail pairs live in the script | Cheap drift guard, no new package | §9.1 |
| CC-10 | `brand-action-press` ships as **`#1196d7`** instead of the user's `#1095d6` | `#0e2747` on `#1095d6` is 4.49:1, under AA by 0.01. FR-BRAND-4 requires a derived shade. Reverting is one line in each token file | §9.2 |
| CC-11 | Focus ring on navy surfaces is `surface` (white), not navy. Inputs get a 2 px navy border on focus; the tint ring is decoration only | A navy ring on navy is 1.25:1, which fails focus visibility (2.4.7) and non-text contrast (1.4.11) | §9.3 |
| CC-12 | Mobile controls are ≥ 48 dp even where the mockup draws 42 px inputs or small language pills | NFR-A11Y-1 requires 44 pt | §9.5 |
| CC-13 | The email for pre-fill lives in an in-memory zustand store (`authFlow`), **never in a URL** | Keeps email out of history and logs. A hard refresh just shows an empty, editable field | §5.8, §6.5 |
| CC-14 | No MSW in M1. Tests stub at the fetch boundary with typed fixtures | Avoids a dependency, and RN support is awkward. Differs from architecture §16 | §12 |
| CC-15 | i18n: nested `errors.<domain>.<reason>`, client-only `errors.client.*` codes, and `validation.<rule>` keys that use the server's rule names. `check:i18n` also verifies every catalog code. Locale fixes use an ISO 639-1 primary-subtag check and map `es-*` to `es-DO` as well as `en-*` to `en` | One vocabulary for client and server validation; "real locale codes" needs a concrete definition | §10 |
| CC-16 | Timeouts. Mobile: 20 s for login, register, reset and delete (argon2 behind a 4-slot semaphore), 10 s for refresh, 3 s default otherwise. BFF and RSC: 20 s for argon2 routes, 10 s for refresh, 5 s otherwise | The 3 s default would fail argon2 calls under load | §5.5, §6.3 |
| CC-17 | Mobile session restore has four outcomes, including a `restoreFailed` screen for network errors that **keeps** the token. A secure-store write failure deletes the stale token and stays in memory | A stale stored token would revoke the session on the next start | §6.4 |
| CC-18 | Mobile language persists in `expo-secure-store` under `mypark.language`. No AsyncStorage dependency | No new dependency for one short value | §6.6 |
| CC-19 | Mobile uses JS `Tabs` from expo-router, RN core `Animated` for the press effect, and no Reanimated, `@expo/ui` or netinfo in M1 | Two tabs, one pill style, works in Expo Go, no new native dependency | §9.5 |
| CC-20 | Web security headers in M1: `X-Content-Type-Options`, `Referrer-Policy` and a `frame-ancestors`/`base-uri`/`form-action`/`object-src` CSP via `next.config.ts` `headers()`. Script-level CSP waits for M5 | A nonce would force every page dynamic; M5 has the security review | §5.4 |
| CC-21 | Legal pages: web at `/[locale]/legal/{terms,privacy}` (matches the seeded URLs), mobile as bundled screens that do not open those URLs | The phone can't reach the seeded `localhost:3000` URLs | §4, X3 |
| CC-22 | Forms are full pages. Delete confirmation is inline on web and a native `Alert` on mobile. No custom modals | Product register: "modal as first thought" is a ban | §4 |
| CC-23 | Phone input normalizes spaces and dashes; a value of only `+1` is sent as empty | The pre-fill must not fail the server's phone rule | §8.3 |
| CC-24 | Signed-in redirects: the public home shows a Vehicles link instead of Log in/Sign up, a signed-in visitor to `/login` goes to `/vehicles`, and an admin on a driver page goes to `/admin` | Avoids dead ends. Admins can't use `/v1/vehicles` (OC-16) | §4.1, §5.6 |
| CC-25 | Fonts: web uses `next/font/google`; mobile uses `@expo-google-fonts/*` loaded at runtime with `useFonts` | The `expo-font` config plugin needs a build, and M1 demos in Expo Go | §9.4 |
| CC-26 | `mp_csrf` lasts 30 days like `mp_refresh`, so tabs restored after a browser restart still match the refresh coordinator's cache. An absent cookie never matches | A session cookie would disappear while the refresh cookie survives, and the next parallel requests would trip reuse detection | §5.2 |
| CC-27 | A suspended user sees the blocked panel while the access token is live, about 14 minutes, then is logged out when refresh returns 401. M1 can only suspend through a database edit; M4 adds suspension and revisits this with a distinct refresh code | Keeping the blocked state now needs a backend and contract change for a feature that arrives in M4 | §4, §5.6 |

---

## 3. Constraints and known conflicts

### 3.1 Constraints

| Area | Constraint (source) |
|---|---|
| Web | Next.js 16.4.5, React 19.3.0, Tailwind 4.3, next-intl 4.14.9, TanStack Query 5, zustand 5, zod 4.6.7, vitest. Server Components by default; container/presentational split (architecture §12) |
| Mobile | Expo SDK 58, expo-router, Uniwind 1.13, i18next, TanStack Query, zustand, `expo/fetch` default transport (architecture §13) |
| Contract | `apps/server/api/openapi.yaml` is the source of truth. `packages/contracts` provides `parseEnvelope`, `successEnvelope`, `listEnvelope`, `ApiError`, `openapi.d.ts`, `error-codes.ts`. Any OpenAPI change needs both developers' approval (policies §2.2) |
| Envelope | Every response is `{data, meta}` or `{error, meta}`. Lists put an array in `data` and paging in `meta.page`. `204` has no body |
| Dependencies | Exact pins for framework packages. Mobile Expo packages match `npx expo install --check`. On this Windows repo `npm install <pkg>` fails with ERESOLVE: edit `package.json`, then run `npm install` (policies §2.6, architecture §1) |
| Secrets | No `.env` files. Env defaults go inline in compose. `EXPO_PUBLIC_API_URL` is set in the shell (policies §2.5) |
| A11y | WCAG 2.2 AA on web; screen readers, dynamic type and 44 pt targets on mobile (NFR-A11Y-1, policies §2.9) |
| Process | ≤ 400 changed lines per PR or chained PR, conventional commits, no AI attribution, docs updated in the same PR (policies §2.1, §2.2, §2.10). Auth is a sensitive path: security checklist plus an automated risk review |

**New dependencies, with the one-line justification policies require:**

| Package | Where | Justification |
|---|---|---|
| `react-hook-form` (7.91.0) | web, mobile | User decision 19: uncontrolled form state with field-level errors and `setError` for server errors |
| `@hookform/resolvers` | web, mobile | Bridges zod 4 schemas to react-hook-form. Pin a version whose peers accept `react-hook-form ^7.55` and `zod ^4` |
| `@expo-google-fonts/barlow`, `@expo-google-fonts/barlow-semi-condensed` (0.4.1) | mobile | Barlow in Expo Go without a custom build (CC-25) |

### 3.2 Contradictions found (resolved here or flagged)

| # | Conflict | Resolution |
|---|---|---|
| X1 | **Client IP.** The contract assumes the BFF forwards the browser IP. In practice Next sets `x-forwarded-for` with `??=` from the socket, so a client-supplied value survives, and behind Docker NAT the socket peer is the bridge gateway, which sits inside `172.16.0.0/12` | CC-5. M1 limitation: local web users share one rate-limit bucket, and a local caller can spoof the header (web is bound to loopback). Phase 2: Caddy overwrites the header. Register is 3/min per IP, so the demo script warns about it |
| X2 | **Brand source.** policies §1.9 says colors "are not sampled or recreated from screenshots"; user decision 17 uses screenshot estimates | User decision wins for placeholders, which stay `TODO(brand)`. The docs slice amends §1.9 to allow estimated placeholders until official assets arrive |
| X3 | **Legal URLs.** The seed hardcodes `http://localhost:3000/{locale}/legal/{kind}`, and the web port is loopback-bound, so a phone can't open them | CC-21 |
| X4 | **No lot endpoint in M1** (spec §6 home "with live availability, hours, location") | Decision 4 is conditional on the API. The home shows name and actions only. M2 adds the rest |
| X5 | **Stale validation docs.** OpenAPI's `ValidationOrCodeInvalid` example shows rule `len`, and backend design §6.2 lists `e164` and `len,numeric`. The server emits `code6`, `phone` and `plate` | Clients translate the rules the server actually emits (§10.2). Fix the OpenAPI example in the docs slice (both developers approve) |
| X6 | **Mockup contrast.** Press state is 4.49:1. Gold on `brand-primary` is 4.40:1 (large text only) | CC-10; gold text stays on `brand-primary-strong` or at large sizes (§9.2) |
| X7 | **`parseEnvelope` drops `meta`**, but "2 de 5" needs the cap. The server reports it in `meta.page.limit` | CC-8: add `parseListEnvelope` |
| X8 | **License.** Barlow is OFL-1.1; policies §2.6 lists only MIT, Apache-2.0, BSD and ISC without approval | User decision 17 is the approval. Record it in the dependency PR |
| X9 | **Testing tool.** Architecture §16 and spec §8 mention MSW; user decision 20 only says mocked responses | CC-14. Docs slice updates architecture §16 |
| X10 | **Stale M0 values.** `app.json` still has `#0b3a7e` in splash and adaptive icon and `userInterfaceStyle: automatic` | Fixed in the foundations slice (§13); the token parity script covers `app.json` |
| X11 | **Architecture §13** lists five mobile tabs | M1 ships two (decision 3). Docs slice notes the M1 subset |

---

## 4. Screens

States legend: **L** loading, **E** empty, **Err** error, **B** blocked. Every form also has validation, submitting and banner states from §7.

### 4.1 Web (`apps/web/src/app/[locale]/…`)

Containers live in `features/<feature>/`, primitives in `components/ui/`. Each page is a Server Component that renders a container.

| Screen | Route file | Data source | States |
|---|---|---|---|
| Public home | `(public)/page.tsx` (rewrite; delete `features/health`) | Session cookie presence only; no API read | Signed out: Log in, Sign up. Signed in: link to Vehicles (CC-24) |
| Log in | `(public)/login/page.tsx` | `POST /api/auth/login` | Err: banner; `auth.email_not_verified` goes to Verify; suspended banner |
| Sign up | `(public)/register/page.tsx` | RSC `GET /v1/legal/current`, then `POST /api/v1/auth/register` | Err: API down shows a retry panel, because no versions means no sign-up |
| Verify email | `(public)/verify/page.tsx` | `verify`, `verify/resend` | Resend shows the same "sent" message always |
| Forgot password | `(public)/forgot-password/page.tsx` | `password/forgot` | Same "sent" message always |
| Reset password | `(public)/reset-password/page.tsx` | `password/reset` | On success, logs out any local session, then Login pre-filled (§5.8) |
| Terms, Privacy | `(public)/legal/[kind]/page.tsx` | Message files. `kind` must be `terms` or `privacy`, otherwise `notFound()` | Static, marked as academic demo |
| Admin login | `(public)/admin/login/page.tsx` | `POST /api/auth/login` | Outside the `(admin)` guard. Same form, admin heading |
| Accept invitation | `(public)/accept-invitation/page.tsx` | `invitations/accept` | On success, Admin login pre-filled |
| Vehicles | `(driver)/vehicles/page.tsx` | RSC list as `initialData`, then TanStack Query `['vehicles']` | L: skeleton on refetch only. E: ghost plate and "add your first car". Err: panel with retry. At cap: Add disabled with reason |
| Add vehicle | `(driver)/vehicles/new/page.tsx` | `POST /api/v1/vehicles` | `vehicle.limit_reached` banner |
| Edit vehicle | `(driver)/vehicles/[id]/edit/page.tsx` | RSC `GET /v1/vehicles/{id}`, `PATCH` | `vehicle.not_found`: back to list with banner |
| Profile | `(driver)/profile/page.tsx` | RSC `getMe()`, `PATCH /api/v1/me` | Edit name and phone (email read-only), forgot-password link, Log out, Log out everywhere, danger zone |
| Legal gate | Rendered by `(driver)/layout.tsx` instead of children | `getMe()` + `GET /v1/legal/current`, `POST /api/v1/me/legal-acceptance` | **B**: full screen; language switch is local only; includes Log out and an inline delete form |
| Account blocked | Rendered by `(driver)/layout.tsx` | `403 auth.account_suspended` | **B**: message and Log out. Shown while the access token is live; after that, refresh ends the session (CC-27) |
| Admin landing | `(admin)/admin/page.tsx` | `requireAdmin()` | Greeting and Log out. No links to M4 features |
| Not found | `not-found.tsx` + `[...rest]/page.tsx` calling `notFound()` | — | Localized |
| Forbidden | `forbidden.tsx` | — | Localized, real 403 (CC-7) |
| Error | `error.tsx` | — | Retry button; codes translated |

The vehicle list row shows the plate, make + model, color, Edit and Delete. Delete expands an inline confirmation inside the row (CC-22). `SiteHeader` becomes presentational with the navy bar, parking sign, wordmark, nav (Vehicles, Profile when signed in) and language links.

### 4.2 Mobile (`apps/mobile/src/app/…`)

| Screen | Route file | Data source | States |
|---|---|---|---|
| Root | `_layout.tsx`, `index.tsx` (redirect; health screen removed) | Auth store | Splash held during `restoring` and font load |
| Restore failed | `restore-failed.tsx` | — | Retry and Log out. Token kept (CC-17) |
| Welcome | `(auth)/welcome.tsx` | — | Parking sign, Sign up, Log in, language switch |
| Log in | `(auth)/login.tsx` | `POST /v1/auth/login`, `client_kind: "mobile"` | As web |
| Sign up | `(auth)/register.tsx` | `GET /v1/legal/current`, `register` | Err: retry, as web |
| Verify email | `(auth)/verify.tsx` | `verify`, `resend` | As web |
| Terms, Privacy | `(auth)/legal/[kind].tsx` | Bundled locale strings (CC-21) | Static |
| Forgot, Reset | `(recovery)/forgot-password.tsx`, `(recovery)/reset-password.tsx` | `password/forgot`, `password/reset` | Reachable signed-in from Profile, so outside the guards |
| Legal gate | `(gate)/legal-acceptance.tsx` | `GET /v1/legal/current`, `POST /v1/me/legal-acceptance`, `DELETE /v1/me` | **B**: logout and inline delete work |
| Vehicles | `(tabs)/vehicles/index.tsx` | TanStack Query `['vehicles']` | L: skeleton. E: ghost plate and prompt. Err: retry |
| Add vehicle | `(tabs)/vehicles/new.tsx` | `POST /v1/vehicles` | As web |
| Edit vehicle | `(tabs)/vehicles/[id].tsx` | `GET`, `PATCH`, `DELETE` | Delete uses native `Alert` (CC-22) |
| Profile | `(tabs)/profile/index.tsx` | `GET /v1/me`, `PATCH /v1/me` | Language switch, forgot-password link, Log out, Log out everywhere, danger zone |
| Delete account | `(tabs)/profile/delete.tsx` | `DELETE /v1/me` | Consequences, password, then Welcome |

Navigation: each tab has its own `_layout.tsx` Stack, so `new` and `[id]` push inside the tab. The root `_layout.tsx` uses three `Stack.Protected` groups (confirmed for SDK 58): `(auth)` when signed out, `(gate)` when signed in with `legalPending`, `(tabs)` when signed in and clear. `index.tsx` is a `<Redirect>` chosen from the store. Because guards are client-side, the API remains the enforcement layer, and a 403 `legal.acceptance_required` flips `legalPending` (§7).

---

## 5. Web session and BFF

### 5.1 Layers

```
browser ──(cookies, X-CSRF-Token)──► proxy.ts ──► /api/v1/* or /api/auth/login route handlers ──(bearer)──► Go API
                                         │
                                         └──► RSC pages ──(lib/api/server.ts, bearer)──► Go API
```

`lib/api/server.ts` is the only code that calls Go directly from the web app, apart from the refresher. It reads the access cookie with `cookies()`, adds `Authorization` and `X-Forwarded-For`, and parses with `parseEnvelope`. Reads use `cache: 'no-store'` and a 5 s timeout; argon2 routes use 20 s. `getMe()` is wrapped in `React.cache` for per-render deduplication.

zustand holds client UI state only (`authFlow`, CC-13). Server data stays in TanStack Query or RSC props. `store/ui.ts` keeps its theme field unused, since M1 is light only.

### 5.2 Cookies

| Cookie | Content | Attributes |
|---|---|---|
| `mp_access` | JWT | `HttpOnly; SameSite=Lax; Path=/; Max-Age = expires_in − 60` |
| `mp_refresh` | Opaque token | `HttpOnly; SameSite=Lax; Path=/; Max-Age = refresh_expires_in` |
| `mp_csrf` | 32 random bytes, base64url | Readable by JS; `SameSite=Lax; Path=/`; `Max-Age` 30 days (matches `mp_refresh`), rotated at login. An absent cookie never matches a result-cache entry, and `proxy.ts` issues a new one (CC-26) |

`Secure` is added only when `APP_ENV=production` (CC-4). `__Host-` prefixes need `Secure`, so they wait for production. Tokens appear in no response body, query string or client prop.

### 5.3 Refresh: the single-flight design (CC-1)

**Why one place.** The pinned Next docs state that setting cookies is not supported during Server Component rendering. If an RSC refreshed, the new refresh token could not be saved, so the next request would present the rotated token and revoke the session. Route handlers can set cookies, but refreshing in both places means two entry points compiled into separate bundles. So only `proxy.ts` refreshes, and it now runs for pages **and** `/api/v1/*`. Route handlers and RSC read tokens that are already fresh.

**The proxy flow**

```ts
// proxy.ts (shape, not final code)
const isBff = pathname.startsWith('/api/v1/');
if (!isBff) { /* unsupportedLocaleRedirect, as today */ }

const session = await ensureSession(request);        // lib/session/refresh.ts
const forwarded = withSessionCookieHeader(request, session);

const response = isBff
  ? NextResponse.next({ request: { headers: forwarded.headers } })
  : guardPublicAndProtected(forwarded) ?? intl(forwarded);

applySetCookies(response, session);   // new tokens, cleared tokens, csrf if missing
return response;
```

Matcher: `['/((?!api|trpc|_next|_vercel|.*\\..*).*)', '/api/v1/:path*', '/api/session/:path*']`. `/api/auth/login` stays outside it on purpose, because a login needs no session.

| Step | Detail |
|---|---|
| Trigger | `mp_access` absent **and** `mp_refresh` present. No JWT decoding. The 60 s margin in `Max-Age` means the access token still has time left when the cookie lapses |
| Lock | `lib/session/refresh.ts` keeps `flights: Map<sha256(refresh), Promise<Outcome>>` and `results: Map<sha256(refresh), {outcome, expiresAt}>` on `globalThis[Symbol.for('mypark.session.refresh')]`, so dev hot reload doesn't reset it. Only the proxy imports this module; an ESLint `no-restricted-imports` rule enforces that |
| Call | `POST {API_INTERNAL_URL}/v1/auth/refresh` with `{refresh_token}`, `X-Forwarded-For`, and `AbortSignal.timeout(10_000)`. It is **not** tied to the incoming request's signal, so a client that navigates away can't abort a rotation others depend on |
| Result cache | A successful outcome stays for **30 s**, longer than the slowest response that can carry `Set-Cookie` (the 20 s argon2 routes) plus the time the browser needs to store it, capped at 1,000 entries (oldest evicted). It records the initiating request's `mp_csrf`. A request that carries the old cookie after the flight finished gets the same successor pair only when its `mp_csrf` matches. Otherwise it goes to Go and trips reuse detection, as it should. Joining an in-flight refresh uses the same check |
| Same request sees new tokens | The proxy rewrites the forwarded `Cookie` request header (`NextResponse.next({ request: { headers } })`, or `intl(new NextRequest(request, { headers }))` for pages). Next's own `x-middleware-set-cookie` merge only runs for Node `IncomingHttpHeaders`, so it can't be relied on for route handlers |

**Outcomes**

| Refresh result | Response | Forwarded cookies | Effect |
|---|---|---|---|
| 200 | Set new `mp_access`, `mp_refresh` | New tokens | Render or handler proceeds |
| 401 `auth.refresh_invalid`, `auth.session_revoked` | Delete both cookies | Neither cookie | Treated as anonymous; protected pages go to login |
| 429, 5xx, network error, timeout | No cookie change | Unchanged (no `mp_access`, `mp_refresh` present) | RSC shows a "session temporarily unavailable" panel; BFF answers `503 internal.unexpected`. The user is **not** logged out |

**Sequences**

*Two tabs navigate at once, with an expired access cookie:*

1. Requests A and B both reach the proxy with `mp_refresh=R1` and no `mp_access`.
2. A starts the flight. B finds it in `flights` and awaits the same promise.
3. Go rotates R1 to R2 once. Both responses carry R2 and the new access token.

*A late arrival.* A prefetch sent before the browser stored R2 arrives after the flight ended, still carrying R1 and the same `mp_csrf`. It finds the entry in `results` and gets R2 too. Without this cache it would present R1 and trip reuse detection.

**Limits, stated plainly**

| Limit | Consequence | When it matters |
|---|---|---|
| One Node process | The lock and cache are per process. Compose runs one `web` container, so this holds for Phase 1 | More than one replica needs a shared lock (Postgres advisory lock or Redis) or stickiness on the refresh cookie. **Do not scale `web` until then** |
| Web restart mid-flight | The cache is lost. A request still carrying R1 reuses it and that session ends | Rare, and the user can log back in |
| Response lost after rotation, > 30 s | Browser keeps R1, next refresh is reuse, session ends | Accepted, same as the backend's no-grace decision (OC-5) |
| Refresh timeout | Go may have rotated anyway. The proxy doesn't retry with R1; a later request might, and that ends the session | 10 s budget; refresh does no hashing, so this is unlikely |

**Accepted risk.** The cache is a narrow grace window on the BFF side. OC-5 rejected a grace window in Go because it weakens reuse detection, and this does too, within limits: for 30 s, a request with the rotated R1 and the matching `mp_csrf` receives the successor pair instead of revoking the session. The residual risk is a thief holding **both** of the browser's cookies inside that window. `mp_refresh` is httpOnly and neither cookie is reachable cross-site. Without the cache, an ordinary parallel prefetch would log the user out.

### 5.4 CSRF and origin (CC-3, CC-20)

`POST`, `PATCH` and `DELETE` on `/api/*` require both:

1. `Origin` exactly matches one entry in `WEB_ALLOWED_ORIGINS` (compose default `http://localhost:3000,http://127.0.0.1:3000`). A missing `Origin` is rejected.
2. Header `X-CSRF-Token` equals the `mp_csrf` cookie, compared in constant time and non-empty.

Failure returns `403 auth.forbidden` in the standard envelope. The client's `lib/api/client.ts` reads `mp_csrf` from `document.cookie` and adds the header. Security headers are set in `next.config.ts` `headers()` (CC-20).

### 5.5 BFF routes (CC-2)

`app/api/v1/[...path]/route.ts` reads this allow-list from `lib/bff/allowlist.ts`. Anything not listed gets `404 http.not_found`. Client paths match OpenAPI paths, so `paths` types apply directly.

| Client path (under `/api`) | Upstream | Auth | After success |
|---|---|---|---|
| `POST /auth/login` (dedicated handler) | `POST /v1/auth/login` with `client_kind: "web"` set server-side | public | Set session cookies, rotate CSRF, return `{user, legal_acceptance_required}` only |
| `GET /session/end?locale=&next=` (dedicated handler) | `GET /v1/me` | cookie | §5.6 |
| `POST /v1/auth/register` | same | public | — |
| `POST /v1/auth/verify` | same | public | — |
| `POST /v1/auth/verify/resend` | same | public | — |
| `POST /v1/auth/password/forgot` | same | public | — |
| `POST /v1/auth/password/reset` | same | public | — |
| `POST /v1/auth/invitations/accept` | same | public | — |
| `POST /v1/auth/logout` | same | bearer | **Always** clear cookies. 401 upstream becomes 204 |
| `POST /v1/auth/logout-all` | same | bearer | Same as logout |
| `GET /v1/legal/current` | same | public | — |
| `GET`, `PATCH /v1/me` | same | bearer | — |
| `DELETE /v1/me` | same | bearer | On 204, clear cookies |
| `POST /v1/me/legal-acceptance` | same | bearer | — |
| `GET`, `POST /v1/vehicles` | same | bearer | — |
| `GET`, `PATCH`, `DELETE /v1/vehicles/{id}` | same | bearer | — |

**Handler rules:** the status and body pass through unchanged, with `Retry-After` and `X-Request-Id`. Requests carry a 20 s timeout for argon2 routes and 5 s otherwise. A bearer route with no `mp_access` returns `503 internal.unexpected` when `mp_refresh` exists (transient refresh failure) and `401 auth.unauthenticated` when it doesn't. Upstream unreachable returns `502 internal.unexpected`. Handlers return codes only; `next/root-params` is not available in route handlers, and translation happens in the client.

**Logout and the 401 rule.** The proxy refreshes before `/api/v1/auth/logout` runs, so a 401 from Go really means the session is already gone. It is safe to treat it as success.

### 5.6 Guards

`lib/auth/guards.ts` exposes `requireDriver()` and `requireAdmin()`. Layouts do **not** re-run on client navigation, so both the layout and each page call them. `getMe()` is cached per render, so there is no extra API call.

| Case | `requireDriver()` | `requireAdmin()` |
|---|---|---|
| No cookies | Redirect to login (the proxy normally does this first) | Redirect to admin login |
| Access absent, refresh present | "Session temporarily unavailable" panel | Same |
| 401 from `/v1/me` | Redirect to `/api/session/end` (CC-6) | Same |
| 403 `auth.account_suspended` | Account-blocked panel with Log out | Same |
| Role mismatch | Admin: redirect to `/admin` | Driver: `forbidden()` |
| Legal versions differ | Render `LegalGate` | Exempt (OC-15) |

`forbidden()` needs `experimental.authInterrupts: true` in `next.config.ts` and returns a real 403 only before streaming starts, so `(admin)` must have no `loading.tsx`. Place `forbidden.tsx` in `app/[locale]/`, since `app/[locale]/layout.tsx` is the root layout. If the pinned Next version won't resolve it there, fall back to a rendered panel, and the API's 403 still enforces the rule.

**Proxy page guard.** With no session cookies at all, `/vehicles`, `/profile` and `/admin` (except `/admin/login`) redirect to the matching login with `next`. With a live `mp_access`, `/login`, `/register` and `/admin/login` redirect to `/vehicles`; `requireDriver()` then sends an admin on to `/admin`. In a page, the guards act only on redirects and `forbidden()`. A gated or blocked driver is handled by the layout, which doesn't render `children`.

`GET /api/session/end` is inside the proxy matcher, so a lapsed access cookie is refreshed first. It then calls `GET /v1/me` and decides:

| State after the proxy | Upstream | Action |
|---|---|---|
| No session cookies | — | Redirect to `/{locale}/login?reason=session_expired` |
| `mp_access` present | 401 `auth.session_revoked` | Clear both cookies, redirect to login as above |
| `mp_access` present | 401 `auth.unauthenticated` | Delete `mp_access` only and redirect to `next`, once (`retried=1` prevents a loop), so the proxy refreshes |
| `mp_access` absent, `mp_refresh` present | — | Refresh failed transiently. Redirect to `next`, which shows the unavailable panel |
| Anything else | 2xx, 403, 5xx | Redirect to `next`. Cookies untouched |

A signed-in user who is sent this link therefore stays signed in. `next` must start with one `/` and contain no `//` or backslash; this same `safeNext` check is used for the post-login redirect.

### 5.7 Client IP (CC-5, X1)

The BFF and proxy send the incoming `X-Forwarded-For` value unchanged, and omit the header when there is none. `cmd/api` walks it right to left and takes the first address outside `TRUSTED_PROXY_CIDRS`. `web` must be in that range. Compose sets `TRUSTED_PROXY_CIDRS` to `172.16.0.0/12` but doesn't pin the network, so Docker could pick another pool. Compose therefore pins `mypark-internal`'s subnet inside that range, on an uncommon /24 that `MYPARK_SUBNET` can override. **Phase 2:** Caddy overwrites the client's value. Until then this is a known, local-only limitation.

### 5.8 Sequences

**Login**

1. RHF validates with `loginSchema`. `POST /api/auth/login`.
2. Handler checks origin and CSRF, calls Go with `client_kind: "web"`, sets cookies, returns user and legal flag.
3. Client redirects to `safeNext(next)` or, by role, `/{user.locale}/vehicles` or `/{user.locale}/admin`. `user.locale` wins over the URL locale (FR-I18N-2).
4. On `auth.email_not_verified`, store the email in `authFlow` and go to Verify. The password is discarded with the form.

**Register → verify → login:** `register` → 202 → Verify with email from `authFlow` → 204 → Login with the email pre-filled, focus on password. Forms reset on success.

**Password reset while signed in:** `password/reset` → 204 → `POST /api/v1/auth/logout` to clear cookies, since the reset revoked the session → Login pre-filled.

**Language switch:** `router.replace(pathname, {locale})` from next-intl's `createNavigation` wrappers. When signed in and not behind the legal gate, also `PATCH /api/v1/me {locale}`. `PATCH` is not legal-exempt, so the gate skips it.

**Interactive data:** RSC preloads, `useQuery` takes it as `initialData` with `initialDataUpdatedAt`. Mutations go through `/api/v1/*` and invalidate `['vehicles']`. `retry` retries only network and 5xx errors, at most twice. A 401 on any client call redirects to `/api/session/end`. A 403 `legal.acceptance_required` calls `router.refresh()`.

---

## 6. Mobile session

### 6.1 Modules

| File | Role |
|---|---|
| `services/tokenStorage.ts` | `expo-secure-store` get, set and delete for `mypark.refresh_token` |
| `store/auth.ts` | zustand, memory only: `status`, `accessToken`, `user`, `legalPending`. Not persisted |
| `services/httpClient.ts` | Extended: `request({method, path, body, auth, timeoutMs})`. Keeps the `fetchImpl` injection used by tests |
| `services/session.ts` | `restore`, `applyTokens`, `refresh`, `logout`, `signOutLocal` |
| `features/*/api.ts` | Typed calls that parse with contracts schemas |

`status` is one of `restoring | signedOut | signedIn | restoreFailed`.

### 6.2 Refresh on 401

- `auth: true` requests send `Authorization: Bearer`. Requests run through one refresh promise: if a refresh is running, wait for it.
- **Only 401 with `auth.unauthenticated` triggers a refresh**, followed by one retry of the original request. A 401 happens before any side effect, so retrying a POST is safe.
- `auth.session_revoked` signs out with no refresh. `auth.invalid_credentials` belongs to login, which is `auth: false`. `auth.password_incorrect` is 403, so it can never loop.
- **Logout** goes through the same path: an expired access token is refreshed first, then logout retried, so the server session is really revoked. A final 401, a network error or anything else still clears the local session. The 401 only counts as success **after** that refresh attempt.

| Refresh result | Action |
|---|---|
| 200 | Write the new refresh token to secure storage **before** resolving, set access in memory |
| 401 `auth.refresh_invalid`, `auth.session_revoked` | Clear storage and memory, `queryClient.clear()`, status `signedOut`, Login with a "session ended" banner |
| Network, timeout, 5xx | Keep tokens, fail the request with `errors.client.*`. The next request tries again |
| 429 | Keep tokens, no refresh until `Retry-After` has passed |

**Accepted limit (user decision, security first).** Refresh tokens stay strictly single-use, with no replay window in Go. If the response to a refresh is lost after Go has rotated the token (signal drop, the OS suspending the app, or the app dying before the secure-store write finishes), the next attempt presents a rotated token and the session ends. The window is small and the outcome is a re-login with no data lost. The client shows "Tu sesión terminó. Inicia sesión de nuevo." / "Your session ended. Log in again." on the login screen, with the email pre-filled when the app still knows it.

### 6.3 Transport and timeouts (CC-16)

`expo/fetch` with `AbortController`, as in the current client.

| Call | Timeout |
|---|---|
| Login, register, reset, `DELETE /v1/me` | 20 s |
| Refresh | 10 s |
| Everything else | 3 s (`DEFAULT_TIMEOUT_MS`) |

Abort maps to `ApiError` with code `client.timeout`, a rejected fetch to `client.network`. A 204 has no body, which `readJson` already tolerates. `getApiHealth` goes away with the health screen. `focusManager` and `AppState` are wired per TanStack's RN guide. 

### 6.4 Session restore on start (CC-17)

The splash stays up while `restoring` is true. Then:

| Secure-store read | Refresh | Result |
|---|---|---|
| Resolves with null | — | `signedOut` |
| Rejects | — | `signedOut`; best-effort delete |
| Token found | 200 | `signedIn`, with `legalPending` from the response |
| Token found | 401 `refresh_invalid` or `session_revoked` | Delete token, `signedOut` |
| Token found | Network, timeout, 5xx, 429 | `restoreFailed`: Retry or Log out. **Token kept** |

**Write failure.** If `setItemAsync` rejects after a rotation, the stored token is now stale and would revoke the session on the next start. Delete it (best effort), keep the new token in memory for this run, and show a one-time notice that the next open will need a login.

### 6.5 Flows

Same as web §5.8, with these differences:

- `client_kind: "mobile"`. Tokens come back in the response and go to memory and secure storage.
- The email for pre-fill is in `authFlow`, in memory.
- After login, `user.locale` becomes the app language.
- Logout, logout-all and deletion: revoke on the server where possible, then clear storage and memory, `queryClient.clear()`, and route to Welcome. The language setting is kept.
- Reset while signed in: `signOutLocal()` on success, since the reset revoked the session, then Login pre-filled.
- `EXPO_PUBLIC_API_URL` must be the LAN address of the API. Reading it stays in `lib/env.ts`.

### 6.6 Language (FR-I18N-2, CC-18)

Precedence: `user.locale` after login, then the stored choice, then `pickLanguage(deviceTags)`. `store/settings.ts` hydrates from `expo-secure-store` before the splash hides. Changing language while signed in and not gated also sends `PATCH /v1/me {locale}`. Register sends the current language as `locale`.

---

## 7. Errors and forms (both clients)

### 7.1 Form pattern

Every form uses react-hook-form with `zodResolver` and a shared schema. **Field names equal the API's JSON names**, so `details.fields[].field` maps directly through `setError(field, {type: 'server', message: 'rule:param'}, {shouldFocus: index === 0})`. A field the form doesn't have falls back to the banner. `root.serverError` holds banner errors. The shared `applyApiError(form, error, t)` lives in each app's `lib/forms/`.

`password_common` is server-only: the client doesn't have the list, so it arrives as a `validation.failed` rule and lands under the password field.

### 7.2 Code → behavior

| Code (HTTP) | Surface | Behavior |
|---|---|---|
| `validation.failed` (422) | Field | Translate `validation.<rule>` with `{param}` |
| `auth.invalid_credentials` (401) | Banner | Same message for wrong email and wrong password |
| `auth.email_not_verified` (403) | — | To Verify with email pre-filled. Prompt to request a code |
| `auth.account_suspended` (403) | Banner on login; blocked panel elsewhere | Log out available |
| `auth.code_invalid` (422) | Field `code` | Offer resend. Same text for every failure |
| `auth.password_incorrect` (403) | Field `password` | Delete account only. Never refresh |
| `auth.unauthenticated`, `auth.session_revoked` (401) | Redirect | Web §5.8; mobile §6.2 |
| `auth.refresh_invalid` (401) | Redirect | Session ended |
| `auth.forbidden` (403) | Banner or `forbidden()` | |
| `legal.acceptance_required` (403) | Gate | Web `router.refresh()`; mobile `legalPending = true` |
| `legal.version_outdated` (409) | Banner | Re-read `/v1/legal/current`; ask the user to accept again |
| `vehicle.plate_taken` (409) | Field `plate` | |
| `vehicle.limit_reached` (409) | Banner | `details.limit` in the message |
| `vehicle.in_use` (409) | Banner | Active from M3; handled now |
| `vehicle.not_found` (404) | Banner on list | Return to list, invalidate cache |
| `rate_limit.exceeded` (429) | Banner | `details.retry_after_seconds`, falling back to `Retry-After`. Wait time shown; submit disabled until it passes |
| `internal.unexpected`, `http.*`, `health.not_ready`, `user.email_taken` | Banner | Generic translated message |
| `client.network`, `client.timeout` (client-only) | Banner | Retry |

**Anti-enumeration:** verify/resend and forgot return `{status:"accepted"}`, and register returns the same `202`. The UI shows one identical "if an account exists…" message for all outcomes.

**Parsing.** A response with no valid envelope throws `ApiError` with `internal.unexpected`, per `parseEnvelope`, and shows the generic banner.

---

## 8. Shared code boundaries

### 8.1 What goes where

| Lives in `packages/contracts` | Lives in each app |
|---|---|
| Generated `openapi.d.ts`, `error-codes.ts` | Translated messages (`errors.*`, `validation.*`) |
| Strict response schemas: `userSchema`, `tokenResponseSchema`, `vehicleSchema`, `legalCurrentSchema`, `acceptedStatusSchema`, `adminUserSchema` | HTTP clients and timeouts |
| Request/form schemas: `loginSchema`, `registerSchema`, `verifySchema`, `resetSchema`, `vehicleInputSchema`, `updateMeSchema`, `deleteMeSchema`, `acceptLegalSchema` | Forms, screens, navigation, session storage |
| `normalizePlate`, `isValidPlate`, `normalizePhone` | `applyApiError`, retry policy |
| `parseEnvelope`, `parseListEnvelope` (new: `{items, page}`) | Design token files (kept in sync by CI) |
| `ValidationRule` union and `validationFieldErrorSchema` | |
| Typed fixtures in `src/fixtures/m1.ts` (each parsed by its schema, so they can't drift) | |

Source layout: `packages/contracts/src/m1/{auth,user,vehicle,legal,normalize}.ts`, exported from `src/index.ts`.

### 8.2 Staying aligned with OpenAPI

| Check | Mechanism |
|---|---|
| Response schemas | `src/m1.schemas.types.test.ts`: `expectTypeOf<z.output<typeof vehicleSchema>>().toEqualTypeOf<components['schemas']['Vehicle']>()`, one per schema. Compile error on drift |
| Request schemas | Same, against `*Request` schemas |
| Strictness | Response schemas use `z.strictObject`, matching `additionalProperties: false`. An added API field regenerates contracts in the same PR (policies §2.2) |
| Generated files | `npm run gen:contracts` and the existing CI drift check |

### 8.3 Validation rules in client schemas

The client mirrors server rules so feedback is immediate. The server stays authoritative.

| Field | Client rule | Issue key |
|---|---|---|
| `email` | Trimmed, valid email, ≤ 254 | `required`, `email`, `max` |
| `password`, `new_password` | 10–128 characters | `required`, `min`, `max` |
| `password` (login, delete) | Required, ≤ 128. **No minimum** | `required`, `max` |
| `first_name`, `last_name` | 1–60, 1–80, trimmed | `required`, `max` |
| `phone` | `normalizePhone` strips spaces and dashes. `+1` alone means empty. Then `^\+[1-9][0-9]{6,14}$` | `phone` |
| `code` | `^[0-9]{6}$`, `one-time-code` | `code6` |
| `plate` | 2–15 characters, then `normalizePlate` matches `^[A-Z0-9]{2,10}$`. Send what the user typed | `required`, `min`, `max`, `plate` |
| `make`, `model`, `color` | 1–50, 1–50, 1–30 | `required`, `max` |

A checkbox for terms and privacy is required on sign-up, and `accepted_terms_version` and `accepted_privacy_version` come from `GET /v1/legal/current`.

Zod messages use the same `rule` or `rule:param` format as server errors (§7.1), e.g. `z.string().min(10, { error: 'min:10' })`. The field component splits it and translates `validation.<rule>` with `{param}`.

---

## 9. Design system

Rules from `impeccable` product register: one family, fixed rem scale, restrained color with one action color, states standardized, skeletons not spinners for content, empty states that teach, no modals as a first thought, no side-stripe borders, no gradient text, motion that conveys state only. The mockup is the visual reference.

### 9.1 Tokens

The names below become `--color-<name>` in `apps/web/src/design/tokens.css` and in the `@theme` of `apps/mobile/src/global.css`, and camelCase entries in `apps/mobile/src/design/tokens.ts`. All colors carry `TODO(brand)`.

| Token | Value | Use |
|---|---|---|
| `brand-primary` | `#1c4a78` | Header bars, links, selected text |
| `brand-primary-strong` | `#163b63` | Auth and welcome background |
| `brand-primary-tint` | `#e3eef8` | Selected pill, hover on quiet actions |
| `brand-primary-contrast` | `#ffffff` | Text on primary (kept from M0) |
| `brand-accent` | `#f2a431` | Gold highlight on navy only |
| `brand-accent-contrast` | `#14233d` | Text on gold (CC-9) |
| `brand-action` | `#1ba7ea` | Main action |
| `brand-action-hover` | `#41b6ef` | Hover |
| `brand-action-press` | `#1196d7` | Pressed (user value `#1095d6`, CC-10) |
| `brand-action-contrast` | `#0e2747` | Text on action |
| `ink` | `#14233d` | Body text |
| `ink-muted` | `#4f5d76` | Secondary text |
| `surface` | `#ffffff` | Panels, inputs |
| `surface-sunken` | `#f1f4f9` | Page background |
| `hairline` | `#dde3ec` | Row dividers (decorative) |
| `control-line` | `#7f8ca3` | Input borders |
| `danger` | `#b3261e` | Errors, destructive actions |
| `danger-tint` | `#fbeae9` | Error banner background |
| `disabled-bg` | `#e3e7ee` | Disabled background |
| `disabled-ink` | `#5f6c83` | Disabled text |

| Other | Value |
|---|---|
| Radii | `--radius-control: 12px`, `--radius-panel: 16px`, `--radius-plate: 10px`; buttons pill (`rounded-full`) |
| Motion | `--motion-duration: 180ms`, `--motion-ease: cubic-bezier(.22, 1, .36, 1)`, reduced motion respected |
| Theme | Light only. Web sets `color-scheme: light`; `app.json` sets `userInterfaceStyle: "light"` |

`scripts/check-design-tokens.mjs` (`npm run check:tokens`, added to CI) compares the three files, flags any `#0b3a7e` left in `app.json`, and asserts the contrast pairs below.

### 9.2 Contrast (computed)

| Pair | Ratio | Rule |
|---|---|---|
| Action contrast on action / hover | 5.54 / 6.55 | Pass |
| Action contrast on press `#1196d7` | 4.55 | Pass (`#1095d6` is 4.49, fails) |
| White on action | 2.71 | Fails, which is why text is dark |
| White on primary / strong | 9.13 / 11.41 | Pass |
| Gold on strong | 5.51 | Pass |
| Gold on primary | 4.40 | **Large text or graphics only** |
| Gold on white | 2.07 | Never for text |
| Ink on surface / sunken | 15.70 / 14.24 | Pass |
| Muted on surface / sunken | 6.65 / 6.03 | Pass |
| Control-line on surface / sunken | 3.40 / 3.08 | Passes the 3:1 UI rule |
| Danger on surface / tint / sunken | 6.54 / 5.62 / 5.93 | Pass |
| Primary on surface / sunken / tint | 9.13 / 8.28 / 7.76 | Pass |
| Disabled ink on disabled bg | 4.28 | WCAG exempts disabled controls; still legible |

### 9.3 States

| State | Button | Field |
|---|---|---|
| Default | Action fill, contrast text | Surface, `control-line` border |
| Hover (web only) | `brand-action-hover` | `ink-muted` border |
| Pressed | `brand-action-press`, scale .98 | — |
| Focus | Web: `box-shadow` of 2 px surface + 3 px `brand-primary`. On navy: 3 px surface ring only (CC-11) | 2 px `brand-primary` border; tint ring is decoration (CC-11) |
| Loading | Spinner + verb, `aria-busy`, not disabled so focus stays | — |
| Disabled | `disabled-bg`, `disabled-ink` | Same, with `hairline` border |
| Error | Banner above; button unaffected | `danger` border, message under the field |

Hover is web-only; native has no hover. Touch uses `active:`. `focus:` is honored for hardware keyboards.

### 9.4 Typeface (CC-25)

| | Web | Mobile |
|---|---|---|
| Barlow 400/500/600/700 | `next/font/google` with CSS variable on `<html>` | `Barlow_400Regular`, `_500Medium`, `_600SemiBold`, `_700Bold` via `useFonts` before splash hides |
| Barlow Semi Condensed 600 | Plate only | Plate only |
| Mapping | `font-medium`, `font-semibold` | Uniwind maps `--font-*` to a single family name, so weights are separate families. `AppText` takes a `weight` prop that picks one |

### 9.5 Primitives

Components use explicit variants, not boolean props. Web components are props-driven and free of data fetching.

| Primitive | Web (`components/ui/`) | Mobile (`components/ui/`) |
|---|---|---|
| Text | Semantic HTML | `AppText` (weight, variant, respects font scale) |
| Button | `Button` variants: `primary`, `onDark`, `quiet`, `danger` | Same, on `Pressable` with `active:`, `disabled:`, `focus:`, min 48 dp |
| Field | Compound `Field.Root`, `.Label`, `.Control`, `.Hint`, `.Error` with generated ids | `TextField`: label `Text`, `TextInput`, error `Text` |
| Checkbox | Native `<input type="checkbox">` styled | `Pressable` with `accessibilityRole="checkbox"` |
| Banner | `role="alert"` for errors, `role="status"` otherwise | `accessibilityRole="alert"`, `accessibilityLiveRegion="polite"` |
| Plate | Real text, 142 px, navy top band, 10 px radius | Same; `AppText` plate font |
| ParkingSign | The "P" mark, `aria-hidden` | Same, with `accessibilityElementsHidden` |
| Auth shell | `AuthPanel`: soft panel on navy with two faint rounded bay lines | `ScreenScroll` on surface; Welcome on `brand-primary-strong` |
| Layout | `SiteHeader`, `PageHeader` | `ScreenScroll`, tab bar with the active pill |
| Language switch | Links with `lang` and `hrefLang` | Two `Pressable`s, `accessibilityState.selected`, ≥ 48 dp (CC-12) |
| Empty state | Ghost plate + title + action | Same |
| Skeleton | CSS, respects reduced motion | Static shape |

Web motion uses CSS transitions at the motion tokens. Uniwind's free tier has no transition support, so the mobile `Button` uses core `Animated` (180 ms scale), skipped when `AccessibilityInfo.isReduceMotionEnabled()` is true (CC-19).

---

## 10. Internationalization

### 10.1 Structure

Files: `apps/web/src/i18n/messages/{es-DO,en}.json` and `apps/mobile/src/lib/i18n/locales/{es-DO,en}.json`. Each app has its own files. `es-DO` is the source (policies §2.8).

| Namespace | Example keys |
|---|---|
| `common` | `appName`, `loading`, `save`, `cancel`, `logout` |
| `auth.login`, `auth.register`, `auth.verify`, `auth.forgot`, `auth.reset`, `auth.invitation` | `title`, `submit`, field labels |
| `vehicles` | `title`, `count`, `empty.title`, `empty.body`, `form.*`, `delete.confirm` |
| `profile` | `title`, `dangerZone.*` |
| `legal` | `terms.title`, `terms.body`, `privacy.*`, `gate.*`, `demoNotice` |
| `errors.<domain>.<reason>` | `errors.vehicle.plate_taken`, `errors.client.network`, `errors.fallback` |
| `validation.<rule>` | `required`, `email`, `min`, `max`, `oneof`, `phone`, `code6`, `password_common`, `plate`, `unknown` |

**Copy examples (neutral):**

| Key | es-DO | en |
|---|---|---|
| `errors.vehicle.plate_taken` | Esa placa ya está registrada. | That plate is already registered. |
| `errors.rate_limit.exceeded` | Demasiados intentos. Inténtalo de nuevo en {seconds} s. | Too many attempts. Try again in {seconds} s. |
| `validation.password_common` | Esa contraseña es muy común. Elige otra. | That password is too common. Choose another. |
| `validation.min` | Debe tener al menos {param} caracteres. | Must be at least {param} characters. |
| `legal.demoNotice` | Texto de ejemplo para una demostración académica. | Sample text for an academic demo. |

Translating a code: `t('errors.' + code)`, falling back to `errors.fallback` when the key is missing, never to `message`. Dates use `America/Santo_Domingo` (FR-I18N-4).

### 10.2 Rules the server emits

From the validator tags in `apps/server`: `required`, `email`, `min`, `max`, `oneof`, `phone`, `code6`, `password_common`, and `plate` from vehicles. The client uses the same names (CC-15). An unlisted rule uses `validation.unknown`.

### 10.3 Parity checks

`scripts/check-i18n-parity.mjs` already checks key parity. It gains:

1. Every entry in `ERROR_CODES` (read from `packages/contracts/src/error-codes.ts`) has `errors.<code>` in all four files.
2. Every `ValidationRule` has `validation.<rule>`.
3. `errors.*` and `validation.*` key sets match between web and mobile.

### 10.4 Routing fixes (M0 leftovers)

- `unsupportedLocaleRedirect`: replace the shape-only `LOCALE_TAG` with a check of the primary subtag against an ISO 639-1 set. `/me` and `/qr` then reach next-intl and become `/es-DO/me`, a clean 404.
- `resolveLocale`: after an exact match, map by base language (`en-US` → `en`, `es-MX` → `es-DO`). Update `routing.test.ts` and `request.test.ts`.
- Add `app/[locale]/not-found.tsx` and `app/[locale]/[...rest]/page.tsx`, per next-intl's error-files guide.
- Add `src/i18n/navigation.ts` with `createNavigation(routing)`. `(public)/layout.tsx` and friends use its `Link`.

---

## 11. Accessibility

| Requirement | Web (WCAG 2.2 AA) | Mobile |
|---|---|---|
| Labels | `<label htmlFor>` on every input; optional fields say so in text | `accessibilityLabel` from the same key; `accessibilityHint` for format help |
| Error association | `aria-invalid` + `aria-describedby` to the error id | Error text has `accessibilityLiveRegion="polite"`; the input's label or hint includes it when invalid |
| Focus on failure | RHF `shouldFocus` moves to the first invalid field; banners get focus when no field is at fault | `setFocus` on the first invalid field; `AccessibilityInfo.announceForAccessibility` for banners |
| Announcements | `role="alert"` for errors, `role="status"` for "sent" and saved. Page titles change per route | Live regions on Android, `announceForAccessibility` on iOS |
| Keyboard | Everything operable; skip link in `SiteHeader`; visible focus (§9.3); 2.4.11 not obscured by sticky header | `focus:` styles for hardware keyboards; logical order |
| Target size | ≥ 24 px (2.5.8), our controls ≥ 44 px | ≥ 48 dp |
| Auth (3.3.8) | Paste allowed; `autocomplete` of `email`, `current-password`, `new-password`, `one-time-code`; no cognitive test | `textContentType` and `autoComplete` equivalents |
| Redundant entry (3.3.7) | Email pre-filled through verify and reset | Same |
| Color alone | Plate is text; errors have text; language selected state is not color-only | Same; `accessibilityState.selected` |
| Language | `<html lang>`; language links carry `lang` | The switch announces the language in its own language |
| Dynamic type | Rem sizes, no fixed-height text containers, 200% zoom | Font scale respected; plate font scale capped so the plate stays legible |
| Motion | `prefers-reduced-motion` removes transitions and scale | Reduce-motion skips the press effect |
| Screen-reader check | Keyboard + NVDA quick pass per slice (policies §2.9) | TalkBack pass on the demo phone |

Plates are real text, so a screen reader can read them.

---

## 12. Testing

### 12.1 Automated

| Layer | Tooling | Required tests |
|---|---|---|
| Contracts | vitest | Schema `expectTypeOf`, `parseListEnvelope`, `normalizePlate`, `normalizePhone` |
| Scripts | `node --test` | Token parity, i18n catalog coverage |
| Web, session | vitest with fetch stub and fake timers | `refresh-coordinator`: *joins an in-flight refresh*; *late arrival is served from the result cache*; *refreshes exactly once for N concurrent requests*; *a late arrival with a different `mp_csrf` is not served*; *cache entry expires after 30 s*; *401 clears cookies*; *5xx keeps cookies*; *does not abort when the caller disconnects*. Proxy forwards rewritten `Cookie` header |
| Web, BFF | vitest on handlers | Allow-list denies unknown paths; CSRF and Origin rejection; login response has no token and sets cookies; logout clears cookies on 401; `DELETE /v1/me` clears cookies on 204; XFF forwarded; `503` when the refresh cookie exists but access doesn't |
| Web, guards | vitest | Driver at admin → `forbidden()`; admin at driver → redirect; legal gate renders; `/api/session/end` follows the table in §5.6, including *a lapsed access cookie never clears the refresh cookie*; `safeNext` rejects `//evil.example` |
| Web, screens | Testing Library + typed fixtures | Each form: success, field error mapping including `password_common`, banner, 429 wait. Vehicles: empty, list, at cap, delete confirm |
| Web, i18n | vitest | `/me` and `/qr` not redirected as locales; `en-US` → `en` |
| Mobile, session | jest-expo with injected `fetchImpl` | *single-flight on concurrent 401s*; *no refresh on 403 password_incorrect*; *no refresh on session_revoked*; *logout refreshes first on auth.unauthenticated*; *argon2 calls use the 20 s timeout*; restore table §6.4; secure-store write failure |
| Mobile, screens | RN Testing Library | Forms as web; legal gate blocks; language persists; Vehicles states |

### 12.2 M1 exit criteria

| Exit criterion (spec §8) | Evidence |
|---|---|
| Register → verification email → verify → login, on web and mobile | Component tests + demo steps 1–6 (mobile) and 10 (web) |
| Vehicle CRUD with plate uniqueness; all errors in both languages | Component tests, `check:i18n`, demo steps 7–9 |
| Admin logs into the admin area; driver cannot (403) | Guard test, demo steps 12–14 |
| Demo: account on mobile, add a car, log in on web | Demo steps 1–8 |

### 12.3 Demo script (outline, written in full in the docs slice)

**Setup:** `docker compose -f deploy/compose/docker-compose.yml up -d --build`. Mailpit at `http://localhost:8025`. In Git Bash: `cd apps/mobile && EXPO_PUBLIC_API_URL=http://<LAN-IP>:8080 npx expo start`. Allow port 8080 through Windows Firewall. Open in Expo Go on Android, on the same network.

| # | Step | Expect |
|---|---|---|
| 1 | Phone: open, switch language on Welcome | es-DO and en both work |
| 2 | Sign up, phone left as `+1` | Goes to Verify |
| 3 | Bad code | Translated field error |
| 4 | Read code in Mailpit, enter it | Goes to Login, email pre-filled, password empty |
| 5 | Log in | Vehicles empty, "add your first car" |
| 6 | Kill and reopen the app | Session restored |
| 7 | Add a car, then the same plate on a second car | Plate shown; second attempt shows `plate_taken` |
| 8 | Web: log in with the same account | Same vehicles |
| 9 | Web: edit and delete one vehicle, switch language | Plate form, translated errors |
| 10 | Web: sign up a second account, verify, log in | Same flow on web; login pre-filled |
| 11 | Open two tabs, clear `mp_access` in dev tools, reload both | Still logged in |
| 12 | Web: `/admin` as driver | 403 page |
| 13 | Log out, log in as seeded admin (`admin@mypark.local`, compose default password) | Admin landing |
| 14 | `/vehicles` as admin | Redirect to `/admin` |
| 15 | Publish a new terms version | Gate blocks; logout and delete work; accept to continue |
| 16 | Suspend the user | Blocked state |
| 17 | Delete the account from Profile | Back to Welcome; login fails |

**Steps 15 and 16 need a documented database edit.** M1 has no API to publish terms or suspend users, so the script gives the exact `docker compose exec postgres psql` statements. Run sign-ups at least a minute apart, because local web users share one rate-limit bucket (X1).

---

## 13. Build order

Same process as the backend (decision 22): this design, then a plan, then slices built on `feat/m1-clients` with fresh-context review. Each slice is a coherent commit set of ≤ 400 changed lines (excluding generated files and lockfiles), chained if pushed as PRs. Slices touching `lib/session`, `proxy.ts`, `lib/bff` or `services/session.ts` are sensitive paths: security checklist and automated risk review (policies §2.2).

| # | Slice | Contents | Approx. lines | Depends on |
|---|---|---|---|---|
| C1 | Contracts | Schemas, normalizers, `parseListEnvelope`, fixtures, `ValidationRule`, type tests | 350 | — |
| W1 | Web foundations | Tokens, `check:tokens`, Barlow, `SiteHeader`, primitives (part 1), i18n fixes, remove health, not-found, security headers | 400 | C1 |
| W2 | Web primitives + i18n | Field, Banner, Plate, AuthPanel, error and validation catalog, parity script extension | 350 | W1 |
| W3 | BFF and session | Cookies, `lib/session/refresh.ts`, `proxy.ts`, allow-list, login, CSRF, compose env (`APP_ENV`, `WEB_ALLOWED_ORIGINS`, subnet) | 400 | C1 |
| W4 | Web auth screens | Login, register, verify, forgot, reset, legal pages, `applyApiError`, `authFlow` | 400 | W2, W3 |
| W5 | Web vehicles | List, add, edit, delete | 300 | W4 |
| W6 | Web profile + gates + admin | Profile, delete account, `requireDriver`, `requireAdmin`, `forbidden`, legal gate, admin login, accept-invitation, landing | 400 | W4 |
| M1 | Mobile foundations | Tokens, fonts, `app.json`, primitives, `httpClient`, `store/auth`, `tokenStorage` | 400 | C1 |
| M2 | Mobile session + navigation | `services/session.ts`, single-flight, restore, `Stack.Protected` layout, language persistence, i18n catalog | 400 | M1 |
| M3 | Mobile auth screens | Welcome, login, register, verify, forgot, reset, legal | 400 | M2 |
| M4 | Mobile vehicles + profile | Tabs, vehicle screens, Profile, delete, legal gate | 400 | M3 |
| D1 | Docs + demo | Items below; written demo script; `spec.md` §8 exit boxes | 200 | all |

### Docs to update (policies §2.10)

| Document | Change | Slice |
|---|---|---|
| `architecture.md` §5 | "right-most untrusted hop"; proxy-only refresh; client-IP limitation | D1 |
| `architecture.md` §12, §13, §16 | BFF shape, M1 tab subset, typed-fixture tests instead of MSW; ADR for proxy-only refresh in §17 | W3, D1 |
| `policies.md` §1.9 | Allow screenshot estimates as `TODO(brand)` placeholders | D1 |
| `policies.md` §2.6 | Note the Barlow OFL approval | W1 |
| `openapi.yaml` | Replace the stale `len` example with `code6` (both developers approve) | C1 |
| `spec.md` §10.2 | Add accepted CC-1…CC-25 | D1 |
| `deploy/compose/docker-compose.yml` | `web` env and pinned subnet | W3 |

---

## 14. Left to the builder

- Exact component props, file splits and message wording beyond the examples, within the conventions above.
- Internal names (`mp_*`, `authFlow`, helper and file names) may change, as long as the behavior and the anti-goals hold.
- Confirm in a test that `x-forwarded-for` is present inside `proxy.ts` on the standalone server, and that `intl(new NextRequest(request, { headers }))` keeps next-intl's rewrite. If not, compose the proxy another way and keep the behavior in §5.3.
- Confirm `forbidden.tsx` resolves under `app/[locale]/`; otherwise use the fallback in §5.6.
- Confirm in Expo Go that `Stack.Protected` with the `(gate)` and `(tabs)` groups reaches the right first screen, and adjust the `redirectTo` or `anchor` if not.
- Pin `@hookform/resolvers` to a version whose peer ranges fit, and confirm with `npm ls`.
- Skeleton shapes, spacing scale values and the exact Welcome layout, following the mockup.
- `EAS preview` concerns are M5: cleartext HTTP for the APK and excluding secure-store entries from Android Auto Backup.
