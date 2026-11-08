# M1 Track B — Web and Mobile Clients: Accounts + Vehicles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the web and mobile clients for the M1 backend: register, verify, log in, manage vehicles, edit a profile, delete an account, reach the admin area, and pass the M1 exit criteria in a written demo against the real backend.

**Architecture:**
- `packages/contracts` gains the shared zod schemas, normalizers and typed fixtures. Both clients translate stable codes through `parseEnvelope` and never show `message`.
- Web is a BFF: tokens live in httpOnly cookies, `proxy.ts` is the only code that calls `POST /v1/auth/refresh`, and one deny-by-default catch-all forwards browser calls to Go.
- Mobile holds the access token in memory and the refresh token in `expo-secure-store`. One refresh promise serves every concurrent 401. Navigation uses three `Stack.Protected` groups.

**Tech Stack:** Next.js 16.4.5, React 19.3.0, TypeScript 6.0.3, Tailwind 4.3, next-intl 4.14.9, TanStack Query 5, zustand 5, zod 4.6.7, react-hook-form 7.91.0, vitest 5. Expo SDK 58, expo-router, Uniwind 1.13, i18next, jest-expo with RN Testing Library 14.

**Spec:** `docs/superpowers/specs/2026-11-08-m1-clients-auth-vehicles-design.md` (referred to below as "spec §N"). Executors keep it open, with the approved visual reference `docs/superpowers/specs/2026-11-08-m1-clients-mockup.html`. This plan points at the spec's tables instead of copying them.

## Global Constraints

- **Versions:** Next.js 16.4.5, React 19.3.0 with a single copy (`npm run check:react`), TypeScript 6.0.3, Expo SDK 58, Uniwind. Before touching a Next.js API, read the matching guide in `node_modules/next/dist/docs/` (`apps/web/AGENTS.md`). Expo docs are pinned to `https://docs.expo.dev/versions/v58.0.0/` (`apps/mobile/AGENTS.md`).
- **Packages:** npm workspaces, never pnpm or yarn. On this Windows repo `npm install <pkg>` fails with ERESOLVE, so edit `package.json`, then run `npm install` at the root. `(cd apps/mobile && npx expo install --check)` must stay clean.
- **Dependencies:** never add a JS dependency beyond these, with their one-line justifications in the PR (spec §3.1):
  - `react-hook-form` `7.91.0` (web, mobile): uncontrolled form state with field-level errors and `setError`.
  - `@hookform/resolvers` `7.0.3` (web, mobile): bridges zod 4 to react-hook-form. Its peers accept `react-hook-form ^7.55` and `zod ^4`, and every other peer is optional.
  - `@expo-google-fonts/barlow` and `@expo-google-fonts/barlow-semi-condensed` `0.4.1` (mobile): Barlow in Expo Go without a custom build (CC-25). OFL-1.1, approved by user decision 17 (X8).
- **Tokens:** same names and values in `apps/web/src/design/tokens.css`, `apps/mobile/src/global.css` and `apps/mobile/src/design/tokens.ts`, enforced by `npm run check:tokens`. All colors carry `TODO(brand)`. Light theme only.
- **Copy:** every user-facing string is a key in both `es-DO` and `en`, with key parity (`npm run check:i18n`). `es-DO` is the source. Next-intl messages use `{param}`; i18next messages use `{{param}}`. No hardcoded strings in UI, including `accessibilityLabel`.
- **Errors:** every response goes through `parseEnvelope`, `parseListEnvelope` or `parseNoContent`. Clients translate `errors.<code>` and `validation.<rule>`, fall back to `errors.fallback`, and never display `ApiError.message`.
- **Forms:** react-hook-form + `zodResolver` with shared schemas. Field names equal the API's JSON names. Zod issue messages use `rule` or `rule:param`, e.g. `{ error: 'min:10' }`. No Server Actions in M1. Forms reset on success. The password is never stored outside the form.
- **Web state:** tokens only in httpOnly cookies. Server data lives in RSC props or TanStack Query. zustand holds client UI only, created per provider (the `store/ui.ts` pattern). Containers live in `features/`, pure props-driven primitives in `components/ui/`.
- **Components:** explicit variants, not boolean mode props. `ref` is a regular prop and context is read with `use()` (React 19).
- **Mobile:** routes only in `src/app/`, with no tests or helpers there (jest ignores that tree). Files in `src/app/` export one screen that renders a container from `src/features/`. `EXPO_PUBLIC_API_URL` is read only in `lib/env.ts`. Controls are at least 48 dp. Never construct class names dynamically (Uniwind). Use `accent-` prefixed props for non-style colors.
- **Accessibility:** WCAG 2.2 AA on web, 44 pt minimum and screen-reader labels on mobile (spec §11). Each slice ends with the keyboard + NVDA quick pass on web or a TalkBack pass on the demo phone (policies §2.9).
- **Secrets:** no `.env` files. Compose uses inline `${VAR:-dev-default}`. `EXPO_PUBLIC_API_URL` is set in the shell.
- **Tests:** TDD, test first. Component tests stub at the fetch boundary with typed fixtures from `@mypark/contracts/fixtures`. No MSW (CC-14). No `sleep`-based assertions: use fake timers or an injected `now`. Web tests that touch `NextRequest`, `AbortSignal` or route handlers start with `// @vitest-environment node`, because the default environment is jsdom.
- **Commands:** run from the repo root in Git Bash on Windows and in Linux CI. Quote paths containing `(` or `[`.
- **Commits:** Conventional Commits, scopes `web|mobile|contracts|deploy|docs|ci`. **No `Co-Authored-By` lines and no AI attribution of any kind.** Each task is one PR-sized slice: at most 400 changed lines, chained, excluding generated files and `package-lock.json`.
- **Sensitive paths:** Tasks 4, 7 and 9 touch `lib/session`, `proxy.ts`, `lib/bff` or `services/session.ts`. Their PRs need the security checklist and an automated risk review (policies §2.2).

## Review Focus

1. **Crafted paths on the BFF catch-all** (`/api/v1/vehicles/..%2Fadmin%2Fadmins`, `/api/v1/vehicles/not-a-uuid`, `/api/v1/vehicles/<uuid>/extra`). Expected: `404 http.not_found` with no upstream call. Test: Task 4 *denies traversal, encoded separators and non-uuid ids*.
2. **A `next` value that decodes to another origin** (`/\evil.example`, `/%5Cevil.example`, `/%2F%2Fevil.example`). Expected: ignored, and the user lands on the role's home page. Test: Task 5 *safeNext rejects backslash and percent-encoded forms*.
3. **Plates and phones typed the way people type them** (`"--"`, `"a-1 23"`, `"+1 809-555-1234"`, `"+1  "`, `"(809) 555-1234"`). Expected: normalized where the rules allow it, otherwise a translated field error. Never a 500 or an empty plate sent to the API. Tests: Task 1 *rejects a plate made only of separators*, *normalizes a spaced +1 phone*, *a bare +1 with spaces is empty*.
4. **Double tap on a submit** (add vehicle, confirm delete, sign up). Expected: exactly one request while one is in flight. Tests: Task 6 *double click on save sends one request*, Task 11 *double tap on save sends one request*.
5. **A 429 with no usable wait time** (`details.retry_after_seconds` and `Retry-After` both absent, or `Retry-After: soon`). Expected: a 60 s wait message and locked submit, never `NaN s` or an unlocked form. Tests: Task 5 *retryAfterSeconds falls back to 60*, Task 10 *retryAfterSeconds falls back to 60*.

---

## File Structure

```
packages/contracts/src/
├─ envelope.ts                                 # T1 parseListEnvelope, parseNoContent
├─ m1/{auth,user,vehicle,legal,normalize,validation}.ts   # T1 schemas, builders, rule list
├─ fixtures/m1.ts                              # T1 typed fixtures (subpath export ./fixtures)
└─ m1.schemas.types.test.ts                    # T1 compile-time OpenAPI alignment
scripts/
├─ check-design-tokens.mjs + .test.mjs         # T2
└─ check-i18n-parity.mjs + .test.mjs           # T3 catalog coverage
apps/web/src/
├─ proxy.ts                                    # T4 session, T7 page guard
├─ lib/session/{cookies,refresh,page-guard,session-end}.ts   # T4, T7. refresh.ts: proxy-only import
├─ lib/bff/{allowlist,csrf,upstream,handler,login}.ts        # T4
├─ lib/api/{server,client}.ts                  # T4, T5
├─ lib/auth/{guards,safe-next}.ts              # T7, T5
├─ lib/forms/{apply-api-error,use-retry-lock}.ts  lib/i18n/translate-error.ts   # T5, T3
├─ lib/security-headers.ts  i18n/{iso-639-1,navigation}.ts   # T2
├─ store/auth-flow.ts  providers/auth-flow-provider.tsx      # T5
├─ components/ui/*  components/SiteHeader.tsx  # T2, T3
├─ features/{home,auth,vehicles,profile,legal,account,session,admin}/   # T2, T5, T6, T7
└─ app/api/{v1/[...path],auth/login,session/end}/route.ts    # T4, T7
   app/[locale]/{not-found,forbidden,error}.tsx, [...rest]/page.tsx, (public)/**, (driver)/**, (admin)/**
apps/mobile/src/
├─ services/{httpClient,tokenStorage,session,runtime}.ts     # T8, T9
├─ store/{auth,auth-flow,settings}.ts          # T8, T10, T9
├─ components/ui/*                             # T8
├─ lib/forms/apply-api-error.ts  lib/i18n/translate-error.ts # T10, T9
├─ features/{session,auth,vehicles,profile,legal}/           # T9, T10, T11
└─ app/_layout.tsx, index.tsx, restore-failed.tsx, (auth)/**, (recovery)/**, (gate)/**, (tabs)/**
deploy/compose/docker-compose.yml              # T4
.github/workflows/ci-js.yml                    # T2
docs/{architecture,policies,spec}.md, docs/m1-demo-script.md # T2, T4, T12
```

Tasks 1–12 are the spec §13 slices in order: C1, W1–W6, M1–M4, D1.

---

## Task 1: Contracts — schemas, normalizers, list envelope, fixtures (slice C1, about 350 lines)

**Files:**
- Create: `packages/contracts/src/m1/{auth.ts,user.ts,vehicle.ts,legal.ts,normalize.ts,validation.ts}`, `packages/contracts/src/m1/{normalize.test.ts,schemas.test.ts,validation.test.ts}`, `packages/contracts/src/m1.schemas.types.test.ts`, `packages/contracts/src/fixtures/m1.ts`
- Modify: `packages/contracts/src/envelope.ts`, `packages/contracts/src/envelope.test.ts`, `packages/contracts/src/index.ts`, `packages/contracts/package.json` (add `"./fixtures": "./src/fixtures/m1.ts"` to `exports`), `apps/server/api/openapi.yaml:1108` (X5)

**Interfaces:**
- Consumes: existing `parseEnvelope`, `ApiError`, `pageSchema`, `Page`, `metaSchema`, `errorEnvelopeSchema`, `components` from `./openapi`.
- Produces (all from `@mypark/contracts` unless noted):
  - `parseListEnvelope<S extends z.ZodType>(schema: S): (body: unknown, status?: number) => { items: z.output<S>[]; page: Page }`
  - `parseNoContent(body: unknown, status: number): void`. Returns on a 2xx with no body and throws the `ApiError` from an error envelope. Anything else throws `internal.unexpected`.
  - `VALIDATION_RULES = ['required','email','min','max','oneof','phone','code6','password_common','plate','unknown'] as const`, `type ValidationRule`
  - `CLIENT_ERROR_CODES = ['client.network','client.timeout'] as const`, `type ClientErrorCode`
  - These two arrays are hand-written, single-line `as const` literals in `m1/validation.ts`, because `scripts/check-i18n-parity.mjs` reads them with a regex (node cannot import TS).
  - `validationFieldErrorSchema`, `type FieldError = { field: string; rule: string; param?: string }`
  - `parseFieldErrors(details: Record<string, unknown>): FieldError[]` (malformed entries are dropped)
  - `parseIssueMessage(message: string): { rule: ValidationRule; param?: string }` (`'min:10'` gives `{rule:'min',param:'10'}`; an unlisted rule gives `{rule:'unknown'}`)
  - `normalizePlate(input: string): string`, `isValidPlate(input: string): boolean`, `normalizePhone(input: string): string`
  - Response schemas (strict): `userSchema`, `tokenResponseSchema`, `vehicleSchema`, `legalCurrentSchema`, `acceptedStatusSchema`, `adminUserSchema`, and `loginResultSchema` = `{ user, legal_acceptance_required }` (what `/api/auth/login` returns to the browser)
  - Types: `User`, `TokenResponse`, `Vehicle`, `LegalCurrent`, `LoginResult`, `Locale = 'es-DO' | 'en'`
  - Form schemas: `loginSchema {email,password}`, `registerSchema {email,password,first_name,last_name,phone,accept_legal}`, `verifySchema {email,code}`, `emailOnlySchema {email}` (forgot, resend), `resetSchema {email,code,new_password}`, `acceptInvitationSchema {email,code,password}`, `vehicleInputSchema {plate,make,model,color}`, `updateMeSchema {first_name,last_name,phone}`, `deleteMeSchema {password}`, `acceptLegalSchema {terms_version,privacy_version}`. Each has a `z.output` type named `<Name>Values`, e.g. `LoginValues`, `VerifyValues`, `ResetValues`, `VehicleInputValues`, `UpdateMeValues`, `DeleteMeValues`, `AcceptLegalValues`.
  - Builders: `toLoginRequest(form: z.output<typeof loginSchema>, clientKind: 'web' | 'mobile'): components['schemas']['LoginRequest']`, `toRegisterRequest(form: z.output<typeof registerSchema>, legal: LegalCurrent, locale: Locale): components['schemas']['RegisterRequest']`
  - `@mypark/contracts/fixtures`: `userFixture`, `adminFixture`, `tokenResponseFixture` (`access_token:'access-1'`, `refresh_token:'refresh-1'`, `expires_in:900`, `refresh_expires_in:2592000`), `vehicleFixture` (`plate:'A123456'`), `legalCurrentFixture` (both versions `2026-11-16`), `successBody<T>(data: T)`, `listBody<T>(items: T[], limit: number)`, `errorBody(code: string, details?: Record<string, unknown>)`, `validationBody(fields: FieldError[])`. Every fixture is `schema.parse(...)`d at module load so it cannot drift.

Rules for the schemas are spec §8.3. Notes the implementer needs: `email` trims before validating and reports `required` when empty. The login and delete `password` has no minimum. `phone` applies `normalizePhone` before the regex and an empty result is valid. `plate` checks length 2–15 on the typed value, then `isValidPlate`, and the output is the typed value, not the normalized one. `accept_legal` is `z.literal(true, { error: 'required' })` and exists only in the form.

- [ ] **Step 1: Write the failing tests**

```ts
// m1/normalize.test.ts
test('normalizePlate uppercases and strips spaces and dashes')   // 'a-1 23' -> 'A123'
test('isValidPlate accepts 2 to 10 alphanumerics after normalizing') // 'A1' true; 'A12345678901' false
test('rejects a plate made only of separators')                  // isValidPlate('--') false; vehicleInputSchema plate '--' -> issue 'plate'
test('normalizes a spaced +1 phone')                             // '+1 809-555-1234' -> '+18095551234'
test('a bare +1 with spaces is empty')                           // normalizePhone('+1') === '' && normalizePhone(' +1  ') === ''
// m1/schemas.test.ts
test('registerSchema accepts the +1 pre-fill as no phone')       // phone '+1' -> output phone ''
test('registerSchema rejects a local-format phone with rule phone') // '(809) 555-1234' -> issue message 'phone'
test('password needs 10 characters on register but not on login') // register 'short' -> 'min:10'; loginSchema 'short' ok
test('password longer than 128 fails with max:128')
test('code must be six digits')                                  // '12345' and '12a456' -> 'code6'
test('plate keeps what the user typed')                          // output.plate === 'a-1 23'
test('email is trimmed and blank is required')                   // ' a@b.do ' -> 'a@b.do'; '' -> 'required'
test('registerSchema requires the legal checkbox')               // accept_legal false -> 'required'
test('toRegisterRequest fills versions and locale and drops the checkbox') // accepted_terms_version '2026-11-16', locale 'en', no accept_legal key
test('toLoginRequest sets client_kind')
test('userSchema rejects an unknown property')                   // strict: {...userFixture, extra:1} fails
// m1/validation.test.ts
test('VALIDATION_RULES holds every rule the server emits')       // required,email,min,max,oneof,phone,code6,password_common,plate
test('parseIssueMessage splits rule and param')                  // 'oneof:es-DO en' -> {rule:'oneof',param:'es-DO en'}
test('parseIssueMessage maps an unlisted rule to unknown')       // 'e164' -> {rule:'unknown'}
test('parseFieldErrors drops malformed entries')
// envelope.test.ts additions
test('parseListEnvelope returns items and page')                 // listBody([v],5) -> {items:[v],page:{limit:5,next_cursor:null}}
test('parseListEnvelope throws the ApiError from an error envelope')
test('parseNoContent accepts a 204 with no body')
test('parseNoContent throws the envelope error on 409')          // code 'vehicle.in_use'
test('parseNoContent treats a non-envelope error body as internal.unexpected') // ('<html>', 502)
// m1.schemas.types.test.ts: one expectTypeOf per schema
expectTypeOf<z.output<typeof vehicleSchema>>().toEqualTypeOf<Schemas['Vehicle']>()
// same for User, TokenResponse, LegalCurrent, AcceptedStatus, AdminUser;
// and verifySchema/VerifyEmailRequest, resetSchema/ResetPasswordRequest, acceptInvitationSchema/AcceptInvitationRequest,
// vehicleInputSchema/CreateVehicleRequest, deleteMeSchema/DeleteMeRequest, acceptLegalSchema/AcceptLegalRequest;
expectTypeOf<ReturnType<typeof toRegisterRequest>>().toEqualTypeOf<Schemas['RegisterRequest']>()
expectTypeOf<z.output<typeof updateMeSchema>>().toExtend<Schemas['UpdateMeRequest']>()
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w packages/contracts`
Expected: FAIL (cannot resolve `./normalize`, `parseListEnvelope` undefined).

- [ ] **Step 3: Implement** the `m1/` modules, the two envelope helpers, the fixtures, the `exports` subpath and the `index.ts` re-exports. Generated `RegisterRequest.locale` is required because the OpenAPI field has a default, so `toRegisterRequest` always sets it.

- [ ] **Step 4: Run tests and typecheck**

Run: `npm test -w packages/contracts && npm run typecheck -w packages/contracts`
Expected: all pass, no compile errors. The type tests only bite under `typecheck`.

- [ ] **Step 5: Fix the stale example (X5).** In `apps/server/api/openapi.yaml:1108` change `{ field: code, rule: len, param: "6" }` to `{ field: code, rule: code6 }`. Both developers approve this PR (policies §2.2).

Run: `npm run gen -w packages/contracts && git diff --stat -- packages/contracts/src/openapi.d.ts`
Expected: no diff for `openapi.d.ts`, since examples are not emitted. If it changes, commit the regenerated file.

- [ ] **Step 6: Commit**

```bash
git add packages/contracts apps/server/api/openapi.yaml
git commit -m "feat(contracts): add m1 schemas, normalizers, list envelope and fixtures"
```

---

## Task 2: Web foundations — tokens, parity script, Barlow, header, locale fixes (slice W1, about 400 lines)

**Files:**
- Create: `scripts/check-design-tokens.mjs`, `scripts/check-design-tokens.test.mjs`, `apps/web/src/i18n/iso-639-1.ts`, `apps/web/src/i18n/navigation.ts`, `apps/web/src/lib/security-headers.ts`, `apps/web/src/lib/security-headers.test.ts`, `apps/web/src/components/ui/{Button.tsx,Button.test.tsx,ParkingSign.tsx,LanguageSwitch.tsx,LanguageSwitch.test.tsx}`, `apps/web/src/features/home/{PublicHome.tsx,PublicHome.test.tsx}`, `apps/web/src/app/[locale]/not-found.tsx`, `apps/web/src/app/[locale]/[...rest]/page.tsx`, `apps/web/src/components/SiteHeader.test.tsx`
- Modify: `apps/web/src/design/tokens.css`, `apps/web/src/app/globals.css`, `apps/web/next.config.ts`, `apps/web/src/i18n/routing.ts`, `apps/web/src/i18n/routing.test.ts`, `apps/web/src/i18n/request.test.ts` (asserts `health.healthy`; change to `common.appName`), `apps/web/src/i18n/messages/{es-DO,en}.json` (drop `health`, add `common`, `home`, `notFound`), `apps/web/src/app/[locale]/layout.tsx`, `apps/web/src/app/[locale]/(public)/layout.tsx`, `apps/web/src/app/[locale]/(public)/page.tsx`, `apps/web/src/app/[locale]/(driver)/layout.tsx`, `apps/web/src/app/[locale]/(admin)/layout.tsx`, `apps/web/src/components/SiteHeader.tsx`, `apps/mobile/src/global.css`, `apps/mobile/src/design/tokens.ts`, `apps/mobile/src/app/_layout.tsx` (rename `brand.primary` to `tokens.brandPrimary`, `brand.primaryContrast` to `tokens.brandPrimaryContrast`), `apps/mobile/app.json` (X10: both `#0b3a7e` become `#163b63`, `userInterfaceStyle: "light"`), `package.json` (`"check:tokens": "node scripts/check-design-tokens.mjs"`), `.github/workflows/ci-js.yml` (web job step `npm run check:tokens` after "i18n key parity"), `docs/policies.md` §2.6 (Barlow OFL note)
- Delete: `apps/web/src/features/health/HealthStatus.tsx`, `apps/web/src/features/health/HealthStatus.test.tsx`, `apps/web/src/lib/api/server.ts` (re-created in Task 4)

**Interfaces:**
- Consumes: nothing from Task 1 at runtime.
- Produces:
  - `apps/mobile/src/design/tokens.ts`: `export const tokens = { brandPrimary: '#1c4a78', /* ...every token in spec §9.1, camelCase */ radiusControl: '12px', radiusPanel: '16px', radiusPlate: '10px', motionDuration: '180ms', motionEase: 'cubic-bezier(.22, 1, .36, 1)' } as const`. One flat `key: 'value'` per line: the script parses it. The `brand` export is removed.
  - Web and mobile `@theme`: `--color-<name>` for all 20 tokens in spec §9.1, `--radius-control`, `--radius-panel`, `--radius-plate`, `--motion-duration`, `--motion-ease`.
  - `scripts/check-design-tokens.mjs` exports: `parseCssTokens(text: string): Map<string,string>`, `parseTsTokens(text: string): Map<string,string>`, `contrastRatio(a: string, b: string): number`, `checkTokens(files?: {webCss:string; mobileCss:string; mobileTs:string; appJson:string}): string[]`.
  - `apps/web/src/lib/security-headers.ts`: `export const securityHeaders: { key: string; value: string }[]`.
  - `apps/web/src/i18n/navigation.ts`: `export const { Link, redirect, usePathname, useRouter, getPathname } = createNavigation(routing)`.
  - `resolveLocale(input: string | undefined): Locale` (extended), `unsupportedLocaleRedirect(pathname: string): string | null` (ISO check).
  - `Button(props: ComponentProps<'button'> & { variant?: 'primary'|'onDark'|'quiet'|'danger'; loading?: boolean; loadingLabel?: string })`, `ParkingSign({ size }: { size: 'sm'|'lg' })`, `LanguageSwitch({ onSwitch }: { onSwitch?: (locale: Locale) => void })` (links with `lang` and `hrefLang`), `SiteHeader({ appName, nav, languageSwitch }: { appName: string; nav?: ReactNode; languageSwitch: ReactNode })` with a skip link, `PublicHome({ signedIn }: { signedIn: boolean })`.
  - Layout decision: `SiteHeader` moves out of `app/[locale]/layout.tsx` into each route group layout and the `not-found`, `forbidden` and `error` files, so the root layout never reads cookies. `(public)/layout.tsx` renders the header above a plain `<main id="main" className="flex-1">`, because the auth panel is full-bleed (mockup). `(driver)` and `(admin)` keep their width-constrained `<main id="main">`.

- [ ] **Step 1: Write the failing tests**

```js
// scripts/check-design-tokens.test.mjs
test('parseCssTokens reads --color, --radius and --motion entries from @theme')
test('parseTsTokens maps camelCase keys to kebab token names') // brandActionPress -> brand-action-press
test('contrastRatio matches the documented values')  // ('#0e2747','#1196d7') ≈ 4.55; ('#0e2747','#1095d6') ≈ 4.49; ('#ffffff','#1ba7ea') ≈ 2.71
test('checkTokens reports a value that differs between web and mobile')
test('checkTokens reports a token missing from tokens.ts')
test('checkTokens flags #0b3a7e left in app.json')
test('checkTokens flags a required pair below its threshold')  // action-contrast on a #1095d6 press
test('checkTokens passes on the real repo')          // checkTokens() deep-equals []
```

```ts
// security-headers.test.ts
test('sets nosniff, referrer policy and the frame/base/form/object CSP')
// X-Content-Type-Options 'nosniff'; Referrer-Policy 'strict-origin-when-cross-origin';
// Content-Security-Policy "frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'"
// routing.test.ts additions
test('resolveLocale maps base languages')            // 'en-US'->'en', 'es-MX'->'es-DO', 'fr-FR'->'es-DO'
test('/me and /qr are not treated as locales')       // unsupportedLocaleRedirect('/me') null; '/qr' null; '/qr/abc' null
test('a real but unsupported locale still redirects') // '/fr'->'/es-DO'; '/en-US/lots'->'/en/lots'; '/es-MX'->'/es-DO'
// Button.test.tsx
test('loading sets aria-busy, shows the verb and stays focusable') // aria-busy='true', not disabled
test.each(['primary','onDark','quiet','danger'])('renders the %s variant')
// LanguageSwitch.test.tsx
test('each link carries lang and hrefLang')          // en link: lang='en' hreflang='en'
test('marks the current language without relying on color') // aria-current='true' on the active link
// SiteHeader.test.tsx
test('starts with a skip link to main content')
// PublicHome.test.tsx
test('signed out shows Log in and Sign up, in es-DO and en')
test('signed in shows a Vehicles link instead')      // CC-24
```

Registered catch-all and not-found behavior is verified by Step 5, because `[...rest]/page.tsx` is one line calling `notFound()`.

- [ ] **Step 2: Run to verify they fail**

Run: `npm run test:scripts; npm test -w apps/web -- src/i18n src/lib/security-headers.test.ts src/components src/features/home`
Expected: FAIL (`check-design-tokens.mjs` not found, `/me` redirected, missing modules).

- [ ] **Step 3: Implement the tokens and the script.** Set the values from spec §9.1 in all three files, each color with a `TODO(brand)` comment. `brand-action-press` is `#1196d7` (CC-10). Contrast pairs live in the script as `MUST_PASS = [[fg, bg, min], ...]` from spec §9.2, with `min` 4.5 for text and 3 for `control-line` and gold on primary, plus `MUST_FAIL` for white on action and gold on white. The disabled pair is exempt.

- [ ] **Step 4: Implement the web foundations.** `next/font/google` for Barlow 400/500/600/700 and Barlow Semi Condensed 600 as CSS variables on `<html>` (spec §9.4). `globals.css` sets `color-scheme: light`, `@media (prefers-reduced-motion: reduce)` removes transitions and scale, and the body uses `bg-surface-sunken text-ink`. `next.config.ts` adds `headers()` applying `securityHeaders` to `/:path*` (CC-20). `(public)/page.tsx` reads `(await cookies()).has('mp_access') || has('mp_refresh')` and renders `PublicHome`. Log in and Sign up links resolve once Task 5 lands.

- [ ] **Step 5: Verify**

Run: `npm run check:tokens && npm run test:scripts && npm test -w apps/web && npm run typecheck -w apps/web && npm run lint -w apps/web && npm run build -w apps/web && npm test -w apps/mobile && npm run typecheck -w apps/mobile`
Expected: all pass. `check:tokens` prints a success line.

Run: `npm run start -w apps/web` in one shell, then `curl -sI http://localhost:3000/me | head -3` and `curl -sI http://localhost:3000/es-DO/nope | head -1`.
Expected: `/me` redirects to `/es-DO/me`; `/es-DO/nope` is `404`; the localized not-found page renders.

- [ ] **Step 6: Accessibility pass.** Keyboard + NVDA over the home page: skip link works, focus ring is visible on navy (3 px surface ring, CC-11).

- [ ] **Step 7: Commit**

```bash
git add scripts apps/web apps/mobile package.json .github/workflows/ci-js.yml docs/policies.md
git commit -m "feat(web): add design tokens, parity check, header and locale fixes"
```

---

## Task 3: Web primitives and the i18n catalog (slice W2, about 350 lines)

**Files:**
- Create: `apps/web/src/components/ui/{Field.tsx,Field.test.tsx,Banner.tsx,Banner.test.tsx,Plate.tsx,Plate.test.tsx,AuthPanel.tsx,Checkbox.tsx,EmptyState.tsx,Skeleton.tsx,PageHeader.tsx}`, `apps/web/src/lib/i18n/{translate-error.ts,translate-error.test.ts}`
- Modify: `scripts/check-i18n-parity.mjs`, `scripts/check-i18n-parity.test.mjs`, `apps/web/src/i18n/messages/{es-DO,en}.json`, `apps/mobile/src/lib/i18n/locales/{es-DO,en}.json`

**Interfaces:**
- Consumes: `ERROR_CODES` file, `VALIDATION_RULES`, `CLIENT_ERROR_CODES`, `parseIssueMessage` (Task 1), `Button` (Task 2).
- Produces:
  - `check-i18n-parity.mjs`: `readConstArray(file: string, name: string): string[]`, `checkCatalog(options?: { webDir: string; mobileDir: string; errorCodesFile: string; validationFile: string }): string[]`. The CLI runs `checkParity()` then `checkCatalog()`.
  - `Field` compound: `Field.Root({ name, invalid, children })`, `Field.Label`, `Field.Control` (slot that injects `id`, `aria-invalid`, `aria-describedby`), `Field.Hint`, `Field.Error({ message })`. Field ids come from `useId()`.
  - `Banner({ tone, children, ref }: { tone: 'error'|'status'; children: ReactNode; ref?: Ref<HTMLDivElement> })`: `role="alert"` for error, `role="status"` otherwise, `tabIndex={-1}`.
  - `Plate({ value }: { value: string })` real text, 142 px, navy top band. `AuthPanel({ title, children })`. `Checkbox`, `EmptyState({ title, body, action })`, `Skeleton`, `PageHeader({ title, action })`.
  - `apps/web/src/lib/i18n/translate-error.ts`: `type Translator = { (key: string, values?: Record<string, string|number>): string; has(key: string): boolean }`, `translateError(t: Translator, error: ApiError): string`, `translateFieldMessage(t: Translator, message: string): string`. A field message is either `rule[:param]` (validation) or `errors:<code>` (a code shown under a field). `translateError` passes `{ seconds }` from `details.retry_after_seconds` and `{ limit }` from `details.limit`.

Message structure is spec §10.1, with these additions fixed here: `errors.fallback`, and every code in `ERROR_CODES` and `CLIENT_ERROR_CODES`. Web uses `{seconds}`, mobile `{{seconds}}`. This task writes `errors.*` and `validation.*` into all four files, so the web/mobile parity check is green from now on. Task 9 adds only mobile screen strings.

- [ ] **Step 1: Write the failing tests**

```js
// check-i18n-parity.test.mjs additions
test('readConstArray reads a generated multi-line list')
test('readConstArray reads a hand-written single-line list')
test('checkCatalog reports an ERROR_CODE missing from a locale file') // 'errors.vehicle.in_use' missing in mobile en.json
test('checkCatalog reports a client code missing from a locale file')
test('checkCatalog reports a validation rule with no key')    // 'validation.code6'
test('checkCatalog reports errors.* drift between web and mobile') // extra key on web only
test('checkCatalog passes on the real repo')                  // every M1 error code, four files: deep-equals []
```

```ts
// translate-error.test.ts
test('translates a known code')                               // 'vehicle.plate_taken' -> 'Esa placa ya está registrada.'
test('passes seconds for rate limits')                        // retry_after_seconds 42 -> '... en 42 s.'
test('passes limit for vehicle.limit_reached')
test('unknown code uses errors.fallback, never message')      // not.toContain('secret server text')
test('translateFieldMessage handles rule, rule:param and errors: forms') // 'min:10', 'password_common', 'errors:vehicle.plate_taken'
test('an unlisted rule uses validation.unknown')
// Field.test.tsx
test('label is associated with the control')
test('invalid control gets aria-invalid and aria-describedby pointing at the error')
test('valid control has no aria-invalid')
// Banner.test.tsx
test('error tone is an alert, status tone is a status')
// Plate.test.tsx
test('the plate is real text a screen reader can read')       // getByText('A123456')
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm run test:scripts; npm test -w apps/web -- src/lib/i18n src/components/ui`
Expected: FAIL (`checkCatalog` is not exported, modules missing).

- [ ] **Step 3: Implement** the script extension, the primitives from spec §9.3 and §9.5, and the catalog in all four files. Use the copy in spec §10.1. Focus: `box-shadow` of 2 px surface plus 3 px `brand-primary`; inputs get a 2 px `brand-primary` border (CC-11). No side-stripe borders on `Banner` (spec §9): use a tinted background.

- [ ] **Step 4: Verify**

Run: `npm run check:i18n && npm run test:scripts && npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add scripts apps/web apps/mobile/src/lib/i18n/locales
git commit -m "feat(web): add form primitives and the error and validation catalog"
```

---

## Task 4: BFF and session — cookies, single-flight refresh, proxy, allow-list, login (slice W3, about 400 lines, sensitive path)

**Files:**
- Create: `apps/web/src/lib/session/{cookies.ts,cookies.test.ts,refresh.ts,refresh.test.ts}`, `apps/web/src/lib/bff/{allowlist.ts,allowlist.test.ts,csrf.ts,csrf.test.ts,upstream.ts,handler.ts,handler.test.ts,login.ts,login.test.ts}`, `apps/web/src/lib/api/{server.ts,server.test.ts}`, `apps/web/src/app/api/v1/[...path]/route.ts`, `apps/web/src/app/api/auth/login/route.ts`, `apps/web/src/proxy.test.ts`
- Modify: `apps/web/src/proxy.ts`, `apps/web/eslint.config.mjs`, `deploy/compose/docker-compose.yml`, `docs/architecture.md` §12 (BFF shape)

**Interfaces:**
- Consumes: `parseEnvelope`, `parseNoContent`, `tokenResponseSchema`, `userSchema`, `legalCurrentSchema`, `ApiError`, fixtures (Task 1); `routing`, `unsupportedLocaleRedirect` (Task 2).
- Produces:
  - `lib/session/cookies.ts`:
    - `ACCESS_COOKIE = 'mp_access'`, `REFRESH_COOKIE = 'mp_refresh'`, `CSRF_COOKIE = 'mp_csrf'`, `CSRF_MAX_AGE_SECONDS = 2_592_000`
    - `type TokenPair = { accessToken: string; refreshToken: string; expiresIn: number; refreshExpiresIn: number }`
    - `type RefreshOutcome = { kind: 'refreshed'; tokens: TokenPair } | { kind: 'ended' } | { kind: 'unavailable' }`
    - `type SessionState = { refresh: RefreshOutcome | null; csrf: { value: string; issued: boolean } }`
    - `isSecureCookie(): boolean` (`process.env.APP_ENV === 'production'`, CC-4)
    - `newCsrfToken(): string` (32 random bytes, base64url)
    - `setSessionCookies(res: NextResponse, tokens: TokenPair): void`, `clearSessionCookies(res: NextResponse): void`, `setCsrfCookie(res: NextResponse, value: string): void`
    - `withSessionCookieHeader(request: NextRequest, state: SessionState): Headers`, `applySetCookies(res: NextResponse, state: SessionState): void`
  - `lib/session/refresh.ts` (only `proxy.ts` may import it):
    - `RESULT_TTL_MS = 30_000`, `MAX_RESULTS = 1_000`, `REFRESH_TIMEOUT_MS = 10_000`
    - `type RefreshInput = { refreshToken: string; csrf: string | undefined; forwardedFor: string | null }`
    - `createRefreshCoordinator(deps: { fetchImpl?: typeof fetch; now?: () => number }): { refresh(input: RefreshInput): Promise<RefreshOutcome> }`
    - `getRefreshCoordinator()`: singleton on `globalThis[Symbol.for('mypark.session.refresh')]`
    - `ensureSession(request: NextRequest): Promise<SessionState>`
  - `proxy.ts`: `export default async function proxy(request: NextRequest): Promise<NextResponse>`, `config.matcher = ['/((?!api|trpc|_next|_vercel|.*\\..*).*)', '/api/v1/:path*', '/api/session/:path*']`
  - `lib/bff/allowlist.ts`: `type BffRoute = { upstreamPath: string; auth: 'public'|'bearer'; timeoutMs: number; after: 'none'|'clear-always'|'clear-on-204' }`, `ARGON2_TIMEOUT_MS = 20_000`, `DEFAULT_TIMEOUT_MS = 5_000`, `matchBffRoute(method: string, segments: string[]): BffRoute | null`
  - `lib/bff/csrf.ts`: `verifyMutation(request: NextRequest): NextResponse | null` (a 403 response, or null when allowed)
  - `lib/bff/upstream.ts`: `bffError(status: number, code: string): NextResponse`, `forwardedHeaders(request: NextRequest): Record<string, string>`, `apiUrl(path: string): string`
  - `lib/bff/handler.ts`: `handleBff(request: NextRequest, segments: string[]): Promise<NextResponse>`
  - `lib/bff/login.ts`: `handleLogin(request: NextRequest): Promise<NextResponse>`
  - `lib/api/server.ts`: `serverFetch<T>(path: string, parse: (body: unknown, status?: number) => T, init?: { method?: string; body?: unknown; timeoutMs?: number; auth?: boolean }): Promise<T>`, `getMe = cache(async (): Promise<User> => ...)`, `getLegalCurrent = cache(async (): Promise<LegalCurrent> => ...)`

**Decisions fixed here:**
- The spec §5.3 sketch's `isBff = pathname.startsWith('/api/v1/')` would send `/api/session/end` into next-intl. The check is `pathname.startsWith('/api/')`.
- 20 s routes: `POST /v1/auth/register`, `POST /v1/auth/password/reset`, `POST /v1/auth/invitations/accept`, `DELETE /v1/me`, and the login handler. Everything else is 5 s.
- Logout and logout-all with no `mp_access`: no upstream call, clear both cookies, return 204. This takes precedence over the 503 bearer rule in spec §5.5, because the user asked to leave. An upstream failure on logout still clears cookies and passes the status through.
- No query string is forwarded. No M1 route takes one.
- A request whose `mp_csrf` does not match a running flight makes a separate upstream call that is **not** registered in `flights`, so it cannot displace the real flight.
- Refresh outcomes: 200 with a valid body is `refreshed`. 401 with `auth.refresh_invalid` or `auth.session_revoked` is `ended`. Everything else, including a malformed 200, is `unavailable`.

- [ ] **Step 1: Write the failing refresh-coordinator tests** in `lib/session/refresh.test.ts`, with `// @vitest-environment node`, a stubbed `fetchImpl` returning `successBody(tokenResponseFixture)` and an injected `now`.

```ts
test('joins an in-flight refresh')                       // second refresh() before resolve: fetch called once, same tokens
test('refreshes exactly once for N concurrent requests') // 20 parallel, same R1 and csrf: fetch called once
test('late arrival is served from the result cache')     // resolve, then same R1 + csrf at now+29_000: fetch still once, tokens equal
test('a late arrival with a different mp_csrf is not served') // after resolve, csrf 'other': fetch called twice
test('a late arrival with no mp_csrf is not served')     // csrf undefined never matches (CC-26)
test('a concurrent request with a different mp_csrf does not join and does not displace the flight')
test('cache entry expires after 30 s')                   // now+30_001: fetch called twice
test('evicts the oldest entry past 1,000 results')       // 1001 tokens; the first one misses, the last one hits
test('401 clears cookies')                               // errorBody('auth.refresh_invalid'), status 401 -> {kind:'ended'}
test('session_revoked clears cookies')
test('5xx keeps cookies')                                // 503 -> {kind:'unavailable'}; not cached: next call hits fetch
test('429 and network errors keep cookies')
test('a malformed 200 keeps cookies')
test('does not abort when the caller disconnects')       // fetch init.signal is AbortSignal.timeout(10_000), not any request signal
test('sends refresh_token and forwards X-Forwarded-For unchanged')
test('omits X-Forwarded-For when there is none')
test('survives a module reload through the globalThis singleton') // vi.resetModules(): same instance
```

- [ ] **Step 2: Write the failing cookie, proxy, BFF and login tests** (all `// @vitest-environment node`).

```ts
// cookies.test.ts
test('access cookie lives expires_in minus 60')          // 900 -> Max-Age=840; HttpOnly; SameSite=lax; Path=/
test('refresh cookie lives refresh_expires_in')          // 2592000
test('csrf cookie is readable by JS and lasts 30 days')  // no HttpOnly; Max-Age=2592000
test('Secure only when APP_ENV is production')           // stubEnv NODE_ENV production + APP_ENV development -> no Secure
test('withSessionCookieHeader replaces tokens after a refresh')
test('withSessionCookieHeader removes tokens when the session ended')
test('withSessionCookieHeader keeps the refresh cookie when unavailable')
// proxy.test.ts: vi.mock the coordinator only; real next-intl
test('proxy forwards the rewritten Cookie header to route handlers') // x-middleware-request-cookie contains access-1
test('page requests keep the next-intl rewrite with new tokens')     // spec §14: /es-DO/vehicles still rewrites
test('refreshes only when access is absent and refresh is present')
test('does not refresh when mp_access is present')
test('issues mp_csrf when missing and keeps an existing one')
test('sets new cookies on the response after a refresh')
test('clears both cookies when the session ended')
test('leaves cookies alone when refresh is unavailable')
test('/api/session/end is treated as an API route, not a locale path') // no redirect to /es-DO/api/...
// allowlist.test.ts
test.each(spec §5.5 rows)('allows %s %s')
test('denies unknown paths')                             // ['admin','admins'] null
test('denies login and refresh through the catch-all')   // POST ['auth','login'] null; POST ['auth','refresh'] null
test('denies a method mismatch')                         // PUT ['vehicles'] null; DELETE ['auth','logout'] null
test('denies traversal, encoded separators and non-uuid ids') // ['vehicles','..%2Fadmin'], ['vehicles','not-a-uuid'], ['vehicles', uuid, 'extra']
test('uses 20 s for argon2 routes and 5 s otherwise')
// csrf.test.ts
test('rejects a missing Origin')                         // 403, error.code 'auth.forbidden'
test('rejects an Origin not in WEB_ALLOWED_ORIGINS')     // 'http://evil.example'
test('rejects a missing token')
test('rejects a token that does not equal the cookie')
test('rejects an empty cookie and empty header')
test('accepts an exact Origin and matching token')
// handler.test.ts
test('unknown path returns 404 http.not_found without calling upstream')
test('rejects a mutation with bad CSRF before calling upstream')
test('adds the bearer token on bearer routes only')
test('passes status, body, Retry-After and X-Request-Id through')
test('forwards X-Forwarded-For unchanged')
test('returns 503 when refresh exists but access does not')  // internal.unexpected
test('returns 401 when neither cookie exists')               // auth.unauthenticated
test('returns 502 when upstream is unreachable')
test('uses a 20 s timeout on register')                      // AbortSignal.timeout spy
test('logout clears cookies when upstream says 401 and answers 204')
test('logout with no access cookie clears cookies and answers 204')
test('logout clears cookies when upstream fails')
test('DELETE /v1/me clears cookies on 204 only')
test('never forwards upstream Set-Cookie')
// login.test.ts
test('login response has no token and sets cookies')     // JSON.stringify(body) not contains 'access-1' or 'refresh-1'; mp_access and mp_refresh HttpOnly
test('returns only user and legal_acceptance_required')  // loginResultSchema.parse(body.data)
test('rotates the csrf cookie')
test('forces client_kind web even if the browser sends mobile') // upstream body client_kind === 'web'
test('rejects bad Origin and bad CSRF')
test('passes 401 auth.invalid_credentials through and sets no cookies')
test('a non-JSON body returns 400 http.invalid_json')
test('a malformed upstream success returns 502 internal.unexpected')
test('uses the 20 s argon2 timeout')
// server.test.ts
test('getMe sends the bearer from cookies, no-store and a 5 s timeout')
test('public reads send no bearer')
test('forwards the incoming X-Forwarded-For and omits it when absent')
```

- [ ] **Step 3: Run to verify they fail**

Run: `npm test -w apps/web -- src/lib/session src/lib/bff src/lib/api src/proxy.test.ts`
Expected: FAIL (modules not found).

- [ ] **Step 4: Implement `lib/session/*`, `proxy.ts`, `lib/bff/*`, `lib/api/server.ts` and the two route files.** Route handlers are one-liners. In Next 16 `params` is a `Promise`: `{ params }: { params: Promise<{ path: string[] }> }`. Follow the flow and tables in spec §5.3–§5.5. Pages use `intl(new NextRequest(request, { headers }))`; API paths use `NextResponse.next({ request: { headers } })`. CSRF comparison uses `crypto.timingSafeEqual` on equal-length buffers.

- [ ] **Step 5: Add the import restriction** to `apps/web/eslint.config.mjs`: `no-restricted-imports` with pattern `**/lib/session/refresh` for `src/**`, ignoring `src/proxy.ts`, `src/proxy.test.ts` and `src/lib/session/**`.

Run: `printf "import '@/lib/session/refresh';\n" > apps/web/src/lib/tmp-restricted.ts; npx -w apps/web eslint src/lib/tmp-restricted.ts; rm apps/web/src/lib/tmp-restricted.ts`
Expected: one `no-restricted-imports` error.

- [ ] **Step 6: Update compose.** Add to `web.environment`: `APP_ENV: ${APP_ENV:-development}` and `WEB_ALLOWED_ORIGINS: ${WEB_ALLOWED_ORIGINS:-http://localhost:3000,http://127.0.0.1:3000}`. Pin the network under `networks.default`: `ipam: { config: [{ subnet: "${MYPARK_SUBNET:-172.28.0.0/24}" }] }` (CC-5).

Run: `docker compose -f deploy/compose/docker-compose.yml config | grep -A4 ipam`
Expected: `subnet: 172.28.0.0/24`.

- [ ] **Step 7: Update `docs/architecture.md` §12** with the BFF shape: the `proxy.ts` refresher, the `/api/v1/[...path]` allow-list, `/api/auth/login`, `/api/session/end`, and the three cookies.

- [ ] **Step 8: Verify**

Run: `npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web && npm run build -w apps/web`
Expected: all pass.

- [ ] **Step 9: Confirm client IP on the standalone server (spec §14).** Start a stub upstream and the standalone build, then call a public BFF route with a client-supplied header.

```bash
node -e "require('http').createServer((q,s)=>{console.log('xff',q.headers['x-forwarded-for']);s.setHeader('content-type','application/json');s.end('{}')}).listen(18080)" &
API_INTERNAL_URL=http://127.0.0.1:18080 PORT=3100 node apps/web/.next/standalone/apps/web/server.js &
sleep 3; curl -s -H 'X-Forwarded-For: 203.0.113.9' http://127.0.0.1:3100/api/v1/legal/current >/dev/null
kill %1 %2
```

Expected: stub prints `xff 203.0.113.9`. If the header is missing inside `proxy.ts` or the handler, recompose per spec §14 and keep the §5.7 behavior.

- [ ] **Step 10: Commit** (two units)

```bash
git add deploy/compose/docker-compose.yml
git commit -m "feat(deploy): add web session env and pin the compose subnet"
git add apps/web docs/architecture.md
git commit -m "feat(web): add bff, cookie session and single-flight refresh"
```

---

## Task 5: Web auth screens (slice W4, about 400 lines)

**Files:**
- Create: `apps/web/src/lib/api/{client.ts,client.test.ts}`, `apps/web/src/lib/auth/{safe-next.ts,safe-next.test.ts}`, `apps/web/src/lib/forms/{apply-api-error.ts,apply-api-error.test.ts,use-retry-lock.ts,use-retry-lock.test.ts}`, `apps/web/src/store/{auth-flow.ts,auth-flow.test.ts}`, `apps/web/src/providers/auth-flow-provider.tsx`, `apps/web/src/testing/render.tsx`, `apps/web/src/features/auth/{LoginForm,RegisterForm,VerifyForm,ForgotPasswordForm,ResetPasswordForm,LegalUnavailable}.tsx` and a `.test.tsx` for each, `apps/web/src/features/legal/LegalDocument.tsx`
- Create routes under `apps/web/src/app/[locale]/(public)/`: `login/page.tsx`, `register/page.tsx`, `verify/page.tsx`, `forgot-password/page.tsx`, `reset-password/page.tsx`, `legal/[kind]/page.tsx`
- Modify: `apps/web/package.json` (add `react-hook-form` `7.91.0`, `@hookform/resolvers` `7.0.3`), `package-lock.json`, `apps/web/src/providers/app-providers.tsx`, `apps/web/src/providers/query-provider.tsx`, `apps/web/src/i18n/messages/{es-DO,en}.json`

**Interfaces:**
- Consumes: Task 1 schemas and fixtures, `translateError`, `translateFieldMessage` (Task 3), `Field`, `Banner`, `AuthPanel`, `Checkbox` (Task 3), `Button` (Task 2), `getLegalCurrent` (Task 4), `POST /api/auth/login` and `/api/v1/*` (Task 4).
- Produces:
  - `lib/api/client.ts`: `apiFetch<T>(path: string, parse: (body: unknown, status?: number) => T, init?: { method?: 'GET'|'POST'|'PATCH'|'DELETE'; body?: unknown }): Promise<T>`, `readCsrfToken(): string`, `handleSessionError(error: unknown, effects: { refresh(): void }): void`. A network failure throws `ApiError` with code `client.network`. A `Retry-After` header is copied into `details.retry_after_header`.
  - Containers: `LoginForm({ heading, next, reason }: { heading: 'driver' | 'admin'; next?: string; reason?: string })`, `RegisterForm({ legal }: { legal: LegalCurrent })`, `VerifyForm()`, `ForgotPasswordForm()`, `ResetPasswordForm()`, `LegalUnavailable()` (retry panel when `GET /v1/legal/current` fails on sign-up).
  - `lib/auth/safe-next.ts`: `safeNext(next: string | null | undefined): string | null`
  - `lib/forms/apply-api-error.ts`: `applyApiError<T extends FieldValues>(form: UseFormReturn<T>, error: unknown, t: Translator): void`, `retryAfterSeconds(error: ApiError): number`
  - `lib/forms/use-retry-lock.ts`: `useRetryLock(): { locked: boolean; seconds: number; lockFor(seconds: number): void }`
  - `store/auth-flow.ts`: `createAuthFlowStore()`, `type AuthFlowState = { email: string; setEmail(email: string): void; clear(): void }`. `providers/auth-flow-provider.tsx`: `AuthFlowProvider`, `useAuthFlow<T>(selector: (s: AuthFlowState) => T): T`.
  - `query-provider.tsx`: `QueryCache` and `MutationCache` `onError` run `handleSessionError(error)`; `retry` allows at most two retries, only for `client.network` and status ≥ 500.
  - `testing/render.tsx`: `renderWithProviders(ui: ReactElement, opts?: { locale?: 'es-DO'|'en' })`.
  - Field-mapped codes in `applyApiError`: `auth.code_invalid` → `code`, `auth.password_incorrect` → `password`, `vehicle.plate_taken` → `plate`. These set `message: 'errors:<code>'`. All other non-validation codes go to `root.serverError`.

**Decisions fixed here:**
- The redirect to `/api/session/end?locale=&next=` fires only for 401 `auth.unauthenticated` and `auth.session_revoked`. Spec §5.8's "a 401 on any client call" would bounce a failed login, since `auth.invalid_credentials` is also 401.
- After a successful login, navigation is `window.location.assign(target)`, so the router cache and locale reset with the new cookies.
- `retryAfterSeconds` uses `details.retry_after_seconds`, then `details.retry_after_header`, then 60 when neither is a positive integer.
- `/api/session/end` is created in Task 7. Until then the redirect 404s, and no screen that can receive that 401 exists yet.

- [ ] **Step 1: Write the failing tests**

```ts
// safe-next.test.ts
test('accepts a same-origin path')                       // '/es-DO/vehicles?x=1'
test('rejects //evil.example')
test('safeNext rejects backslash and percent-encoded forms') // '/\\evil.example', '/%5Cevil.example', '/%2F%2Fevil.example'
test('rejects an absolute URL, an empty value and null')
// client.test.ts
test('adds X-CSRF-Token on mutations, read from mp_csrf')
test('sends no CSRF header on GET')
test('a failed fetch becomes client.network')
test('a non-envelope body becomes internal.unexpected')
// query-provider behavior, in client.test.ts through handleSessionError
test('auth.unauthenticated redirects to /api/session/end with locale and next')
test('auth.session_revoked redirects to /api/session/end')
test('auth.invalid_credentials stays on the form')       // no location.assign
test('legal.acceptance_required calls router.refresh')
// apply-api-error.test.ts
test.each(['required','email','min','max','oneof','phone','code6','password_common','plate'])
  ('maps the server rule %s to its field and message')   // setError(field,{type:'server',message:'rule' or 'rule:param'})
test('shouldFocus is true only on the first field')
test('a field the form lacks falls back to the banner')
test('vehicle.plate_taken lands on plate')               // message 'errors:vehicle.plate_taken'
test('auth.password_incorrect lands on password')
test('other codes go to root.serverError, translated')
test('retryAfterSeconds prefers details, then the header')
test('retryAfterSeconds falls back to 60')               // both absent; Retry-After 'soon'; 0 -> 60
// use-retry-lock.test.ts (fake timers)
test('locks, counts down and unlocks')
// auth-flow.test.ts
test('holds only the email and clears')                  // Object.keys of state data: ['email']
// LoginForm.test.tsx
test('validates before calling the API')                 // empty submit: fetch not called; required errors
test('success redirects by role to the user locale')     // driver, user.locale 'en' -> '/en/vehicles'; admin -> '/es-DO/admin'
test('success honors a safe next')
test('wrong credentials show one banner')                // 401 auth.invalid_credentials, role=alert
test('email_not_verified goes to verify keeping only the email') // store email set; push '/verify'; no query string
test('account_suspended shows a banner')
test('429 shows the wait time and disables submit')      // details {retry_after_seconds:42} -> '42'
test('session_expired reason shows a status banner')     // ?reason=session_expired
test('password is not carried on any exit')              // JSON.stringify(store.getState()) and every location/router arg not containing the password
// RegisterForm.test.tsx
test('sends legal versions, locale and an empty phone for +1')
test('requires the terms checkbox')
test('field error mapping includes password_common')     // validationBody([{field:'password',rule:'password_common'}]) -> 'Esa contraseña es muy común. Elige otra.'
test('202 goes to verify with the email and resets the form')
test('the password never reaches the store or the url')
test('double submit sends one request')
// LegalUnavailable.test.tsx
test('legal fetch failure shows a retry panel')          // retry calls router.refresh
// VerifyForm.test.tsx
test('pre-fills the email from authFlow and uses one-time-code')
test('code_invalid is a field error with a resend offer') // errors.auth.code_invalid under code
test('verified goes to login, email kept, password empty')
test('resend shows the same sent message on every outcome')
// ForgotPasswordForm.test.tsx
test('shows the same sent message')
// ResetPasswordForm.test.tsx
test('success logs out locally then goes to login pre-filled') // POST /api/v1/auth/logout before navigation
test('code_invalid maps to code')
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/web -- src/lib/api/client.test.ts src/lib/auth src/lib/forms src/store/auth-flow.test.ts src/features/auth`
Expected: FAIL (modules not found).

- [ ] **Step 3: Install the form dependencies.** Edit `apps/web/package.json`, then run `npm install`.

Run: `npm ls react-hook-form @hookform/resolvers && npm run check:react && git diff --stat package-lock.json`
Expected: one copy of each package; React single version; the lockfile adds only those packages and `@standard-schema/utils`.

- [ ] **Step 4: Implement** the library modules, the store and provider, the forms and the routes. Every page is a Server Component rendering a container. Register reads `getLegalCurrent()` on the server. `legal/[kind]/page.tsx` calls `notFound()` unless `kind` is `terms` or `privacy`. Autocomplete values are `email`, `current-password`, `new-password`, `one-time-code` (spec §11). Banners take focus when no field is at fault. Copy goes in both locale files.

- [ ] **Step 5: Verify**

Run: `npm run check:i18n && npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web && npm run build -w apps/web`
Expected: all pass.

- [ ] **Step 6: Accessibility pass.** Keyboard + NVDA over login, register and verify: focus moves to the first invalid field, banners are announced, paste works in password and code.

- [ ] **Step 7: Commit**

```bash
git add apps/web package-lock.json
git commit -m "feat(web): add auth screens with shared form error handling"
```

---

## Task 6: Web vehicles (slice W5, about 300 lines)

**Files:**
- Create: `apps/web/src/features/vehicles/{api.ts,VehiclesScreen.tsx,VehiclesScreen.test.tsx,VehicleRow.tsx,VehicleForm.tsx,VehicleForm.test.tsx}`, `apps/web/src/app/[locale]/(driver)/vehicles/page.tsx`, `apps/web/src/app/[locale]/(driver)/vehicles/new/page.tsx`, `apps/web/src/app/[locale]/(driver)/vehicles/[id]/edit/page.tsx`
- Modify: `apps/web/src/lib/api/server.ts`, `apps/web/src/lib/api/server.test.ts`, `apps/web/src/i18n/messages/{es-DO,en}.json`

**Interfaces:**
- Consumes: `vehicleSchema`, `vehicleInputSchema`, `parseListEnvelope`, `parseNoContent`, fixtures (Task 1); `apiFetch`, `applyApiError`, `useRetryLock`, `renderWithProviders` (Task 5); `Plate`, `EmptyState`, `Skeleton`, `PageHeader`, `Field`, `Banner` (Task 3).
- Produces:
  - `lib/api/server.ts`: `listVehicles(): Promise<{ items: Vehicle[]; page: Page }>`, `getVehicle(id: string): Promise<Vehicle>`
  - `features/vehicles/api.ts`: `fetchVehicles(): Promise<{ items: Vehicle[]; page: Page }>`, `createVehicle(input: VehicleInputValues): Promise<Vehicle>`, `updateVehicle(id: string, input: VehicleInputValues): Promise<Vehicle>`, `deleteVehicle(id: string): Promise<void>`, `VEHICLES_QUERY_KEY = ['vehicles'] as const`
  - `VehiclesScreen({ initial, initialUpdatedAt, notice }: { initial: { items: Vehicle[]; page: Page }; initialUpdatedAt: number; notice?: 'vehicle_not_found' })`
  - `VehicleForm({ mode, vehicle }: { mode: 'create' | 'edit'; vehicle?: Vehicle })`

Guards are added to these three pages in Task 7. Until then the API enforces access.

- [ ] **Step 1: Write the failing tests**

```ts
// VehiclesScreen.test.tsx
test('empty state shows the ghost plate and add-your-first-car')
test('lists plate, make and model, color, Edit and Delete')
test('shows the count from meta.page.limit')             // '2 de 5' with limit 5 and two vehicles
test('at cap, Add is disabled with a visible reason')    // listBody five vehicles, limit 5
test('delete expands an inline confirmation in the row') // no role=dialog
test('confirm deletes and refetches')                    // DELETE /api/v1/vehicles/<id>, then GET
test('double click on confirm sends one DELETE')
test('vehicle.in_use shows a banner')
test('vehicle.not_found invalidates and shows a banner')
test('refetch failure shows a retry panel')
test('notice vehicle_not_found shows a banner')
// VehicleForm.test.tsx
test('create posts what the user typed')                 // plate 'a-1 23'
test('double click on save sends one request')
test('plate_taken lands under plate, in es-DO and en')   // 'Esa placa ya está registrada.' / 'That plate is already registered.'
test('server rule plate lands under plate')
test('limit_reached shows a banner with the limit')      // details {limit:5}
test('edit pre-fills and patches')
test('success invalidates vehicles and returns to the list')
// server.test.ts additions
test('listVehicles returns items and page')
test('getVehicle encodes the id')
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/web -- src/features/vehicles src/lib/api/server.test.ts`
Expected: FAIL.

- [ ] **Step 3: Implement.** `useQuery` takes the RSC data as `initialData` with `initialDataUpdatedAt`. A skeleton shows on refetch only. The submit handler returns early while `mutation.isPending` (Review Focus 4). The edit page treats `ApiError` status 404 or 422 from `getVehicle` as not found and redirects to `/vehicles?notice=vehicle_not_found`.

- [ ] **Step 4: Verify**

Run: `npm run check:i18n && npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add apps/web
git commit -m "feat(web): add vehicle list, create, edit and delete"
```

---

## Task 7: Web profile, guards, legal gate, admin (slice W6, about 400 lines, sensitive path)

**Files:**
- Create: `apps/web/src/lib/auth/{guards.ts,guards.test.ts}`, `apps/web/src/lib/session/{page-guard.ts,page-guard.test.ts,session-end.ts,session-end.test.ts}`, `apps/web/src/app/api/session/end/route.ts`, `apps/web/src/app/[locale]/{forbidden.tsx,error.tsx,error.test.tsx}`, `apps/web/src/app/[locale]/(driver)/layout.test.tsx`, `apps/web/src/features/profile/{ProfileForm.tsx,ProfileForm.test.tsx,DeleteAccountForm.tsx,DeleteAccountForm.test.tsx,LogoutButtons.tsx}`, `apps/web/src/features/legal/{LegalGate.tsx,LegalGate.test.tsx}`, `apps/web/src/features/account/AccountBlocked.tsx`, `apps/web/src/features/session/SessionUnavailable.tsx`, `apps/web/src/features/admin/{AdminLanding.tsx,AcceptInvitationForm.tsx,AcceptInvitationForm.test.tsx}`, `apps/web/src/components/ui/PersistedLanguageSwitch.tsx`
- Create routes: `apps/web/src/app/[locale]/(driver)/profile/page.tsx`, `apps/web/src/app/[locale]/(admin)/admin/page.tsx`, `apps/web/src/app/[locale]/(public)/admin/login/page.tsx`, `apps/web/src/app/[locale]/(public)/accept-invitation/page.tsx`
- Modify: `apps/web/src/app/[locale]/(driver)/layout.tsx`, `apps/web/src/app/[locale]/(admin)/layout.tsx`, `apps/web/src/app/[locale]/(driver)/vehicles/page.tsx`, `apps/web/src/app/[locale]/(driver)/vehicles/new/page.tsx`, `apps/web/src/app/[locale]/(driver)/vehicles/[id]/edit/page.tsx`, `apps/web/src/proxy.ts`, `apps/web/src/proxy.test.ts`, `apps/web/next.config.ts`, `apps/web/src/i18n/messages/{es-DO,en}.json`

**Interfaces:**
- Consumes: `getMe`, `getLegalCurrent`, cookies constants (Task 4); `safeNext`, `apiFetch`, `applyApiError`, `useAuthFlow`, `renderWithProviders` (Task 5); `updateMeSchema`, `deleteMeSchema`, `acceptLegalSchema`, `acceptInvitationSchema` (Task 1).
- Produces:
  - `lib/auth/guards.ts`:
    - `type DriverGate = { state: 'ready'; user: User } | { state: 'legal'; user: User; legal: LegalCurrent } | { state: 'suspended' } | { state: 'unavailable' }`
    - `requireDriver(locale: Locale): Promise<DriverGate>`
    - `type AdminGate = { state: 'ready'; user: User } | { state: 'suspended' } | { state: 'unavailable' }`
    - `requireAdmin(locale: Locale): Promise<AdminGate>`
  - `lib/session/page-guard.ts`: `guardPublicAndProtected(request: NextRequest): NextResponse | null`. It strips an optional locale prefix before matching.
  - `lib/session/session-end.ts`: `handleSessionEnd(request: NextRequest): Promise<NextResponse>`
  - `LegalGate({ legal, user }: { legal: LegalCurrent; user: User })`, `DeleteAccountForm()`, `LogoutButtons({ scope }: { scope: 'this' | 'both' })`, `ProfileForm({ user }: { user: User })`, `PersistedLanguageSwitch()` (wraps `LanguageSwitch` and sends `PATCH /api/v1/me {locale}`).

**Decisions fixed here:**
- Legal is out of date when `user.legal.terms_version !== legal.terms.version` or the same for privacy. If `getLegalCurrent()` fails after `getMe()` succeeds, the state is `unavailable`, which blocks instead of skipping the gate.
- Layouts return before rendering `children` for every state except `ready`. Pages call the guard too, for its redirects and `forbidden()`, and render nothing unless `ready`.
- `(driver)/layout.tsx` renders `SiteHeader` with Vehicles and Profile nav and `PersistedLanguageSwitch`. In the `legal` state it passes the plain `LanguageSwitch` (`PATCH` is not legal-exempt).
- `(admin)` has no `loading.tsx`, so `forbidden()` can return a real 403.
- `redirect` targets on 401 use `next = /{locale}/vehicles` for drivers and `/{locale}/admin` for admins.

- [ ] **Step 1: Write the failing tests**

```ts
// guards.test.ts: mock getMe, getLegalCurrent, next/navigation, next/headers
test('no cookies redirects a driver to login')
test('access absent, refresh present is unavailable')
test('401 redirects to /api/session/end')                // with locale and next
test('403 account_suspended is suspended')
test('admin at driver area is redirected to /admin')
test('driver at admin area gets forbidden()')            // the 403 admin guard
test('admin area is exempt from the legal gate')         // outdated versions -> ready
test('legal gate renders when a version differs')        // state 'legal' for terms or privacy mismatch
test('legal fetch failure does not skip the gate')       // state 'unavailable'
test('ready when both versions match')
test('getMe failure with 5xx rethrows for error.tsx')
// page-guard.test.ts
test('no cookies on /vehicles, /profile and /admin redirect to the matching login with next')
test('admin login itself is not guarded')
test('live mp_access on /login, /register and /admin/login goes to /vehicles')
test('matches with and without a locale prefix')
test('a refresh cookie alone does not redirect away from protected pages')
// session-end.test.ts: spec §5.6 table
test('no cookies redirect to login with session_expired')
test('session_revoked clears both cookies and redirects to login')
test('unauthenticated deletes only mp_access and retries next once') // retried=1 in the location
test('a second unauthenticated with retried=1 goes to login without clearing mp_refresh')
test('a lapsed access cookie never clears the refresh cookie')       // no access, refresh present: Set-Cookie untouched, redirect to next
test.each([200, 403, 500])('upstream %s redirects to next, cookies untouched')
test('unsafe next is replaced')                          // next '//evil.example'
test('session end works for a GET from another site without logging out a valid session') // 200 upstream: no Set-Cookie
// (driver)/layout.test.tsx: await DriverLayout({ children, params }) with requireDriver mocked
test('legal state renders LegalGate instead of children')
test('suspended state renders AccountBlocked with Log out instead of children')
test('unavailable state renders the session-unavailable panel, not a logout')
test('ready state renders children')
// error.test.tsx
test('offers Retry and shows a translated message, never error.message')
// LegalGate.test.tsx: legal blocking gate
test('blocks with accept, log out and delete, and no app content')
test('accept sends the current versions and refreshes')  // POST body {terms_version,privacy_version}
test('version_outdated shows a banner and re-reads')
test('logout works behind the gate')
test('delete works behind the gate')
test('language switch behind the gate does not call PATCH')
// ProfileForm.test.tsx
test('email is read-only')
test('patches name and phone; +1 alone clears the phone')
test('links to forgot password')
// DeleteAccountForm.test.tsx
test('explains consequences and requires the password')
test('password_incorrect lands on password and never refreshes') // 403: no navigation
test('success returns to the home page')
test('the password field resets on success')
// AcceptInvitationForm.test.tsx
test('success goes to admin login with the email kept')
test('password_common lands on password')
test('code_invalid lands on code')
// proxy.test.ts additions
test('the page guard runs after the refresh')            // refresh first, then redirect logic sees new cookies
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/web -- src/lib/auth/guards.test.ts src/lib/session/page-guard.test.ts src/lib/session/session-end.test.ts src/features/legal src/features/profile src/features/admin src/proxy.test.ts "src/app/\[locale\]"`
Expected: FAIL.

- [ ] **Step 3: Implement** the guards, page guard, session-end handler, route, layouts, gate, profile, admin screens and `error.tsx` (client component with Retry, translated). Set `experimental.authInterrupts: true` in `next.config.ts`. Add `await requireDriver(locale)` to the three vehicle pages. `(public)/admin/login/page.tsx` renders `LoginForm` with `heading="admin"`.

- [ ] **Step 4: Verify**

Run: `npm run check:i18n && npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web && npm run build -w apps/web`
Expected: all pass.

- [ ] **Step 5: Confirm the real 403 (spec §14).**

Run: `docker compose -f deploy/compose/docker-compose.yml up -d --build`, sign in as a driver in a browser, open `/es-DO/admin` with the Network panel.
Expected: status `403` and the localized forbidden page. If `forbidden.tsx` does not resolve under `app/[locale]/`, apply the fallback in spec §5.6 (rendered panel; the API's 403 still enforces) and record it in the PR.

- [ ] **Step 6: Accessibility pass.** Keyboard + NVDA over profile, gate, and delete flows.

- [ ] **Step 7: Commit**

```bash
git add apps/web
git commit -m "feat(web): add profile, legal gate, role guards and admin area"
```

---

## Task 8: Mobile foundations — primitives, HTTP client, auth store, token storage (slice M1, about 400 lines)

**Files:**
- Create: `apps/mobile/src/components/ui/{AppText,Button,TextField,Checkbox,Banner,Plate,ParkingSign,ScreenScroll,EmptyState,Skeleton,LanguageSwitch}.tsx`, `apps/mobile/src/components/ui/{Button,TextField,Checkbox,Banner,LanguageSwitch}.test.tsx`, `apps/mobile/src/services/{tokenStorage.ts,tokenStorage.test.ts}`, `apps/mobile/src/store/{auth.ts,auth.test.ts}`
- Modify: `apps/mobile/package.json` (add `react-hook-form` `7.91.0`, `@hookform/resolvers` `7.0.3`, `@expo-google-fonts/barlow` `0.4.1`, `@expo-google-fonts/barlow-semi-condensed` `0.4.1`), `package-lock.json`, `apps/mobile/src/global.css` (`--font-barlow-regular`, `-medium`, `-semibold`, `-bold`, `--font-plate`), `apps/mobile/src/services/httpClient.ts`, `apps/mobile/src/services/httpClient.test.ts`

**Interfaces:**
- Consumes: `ApiError`, `User`, `ClientErrorCode` (Task 1); `tokens` (Task 2).
- Produces:
  - `services/httpClient.ts` (keeps `createHttpClient(baseUrl, fetchImpl = fetch, options)`, and keeps `get` and `getApiHealth` until Task 9 removes the health screen):
    - `DEFAULT_TIMEOUT_MS = 3000`, `ARGON2_TIMEOUT_MS = 20_000`, `REFRESH_TIMEOUT_MS = 10_000`
    - `type HttpRequest = { method: 'GET'|'POST'|'PATCH'|'DELETE'; path: string; body?: unknown; auth?: boolean; timeoutMs?: number }`
    - `type HttpResponse = { status: number; body: unknown }` plus `retryAfter: string | null` on `request` results
    - `type RefreshResult = { kind: 'refreshed' } | { kind: 'ended' } | { kind: 'rate_limited'; retryAfterSeconds: number } | { kind: 'unavailable'; code: ClientErrorCode }`
    - `type AuthAdapter = { getAccessToken(): string | null; refresh(): Promise<RefreshResult>; onSessionRevoked(): Promise<void>; onLegalRequired(): void }`
    - `HttpClient` gains `request(req: HttpRequest): Promise<HttpResponse & { retryAfter: string | null }>` and `setAuthAdapter(adapter: AuthAdapter): void`
    - `unwrap<T>(res: HttpResponse & { retryAfter: string | null }, parse: (body: unknown, status?: number) => T): T`: runs `parse` and, when it throws an `ApiError`, copies `retryAfter` into `details.retry_after_header`. Every `features/*/api.ts` call goes through it.
  - `services/tokenStorage.ts`: `REFRESH_TOKEN_KEY = 'mypark.refresh_token'`, `type TokenStorage = { get(): Promise<string | null>; set(token: string): Promise<void>; remove(): Promise<void> }`, `tokenStorage: TokenStorage`
  - `store/auth.ts`: `type AuthStatus = 'restoring'|'signedOut'|'signedIn'|'restoreFailed'`, `type AuthNotice = 'session_ended'|'storage_failed'|null`, `useAuthStore` with `status`, `accessToken`, `user`, `legalPending`, `notice`, and actions `setSession(s: { accessToken: string; user: User; legalPending: boolean }): void`, `setAccessToken(t: string): void`, `setStatus(s: AuthStatus): void`, `clearSession(notice?: AuthNotice): void`, `setLegalPending(v: boolean): void`, `setUser(u: User): void`, `clearNotice(): void`. Not persisted.
  - Primitives per spec §9.5: `AppText({ weight, variant, ...TextProps })` with `weight: 'regular'|'medium'|'semibold'|'bold'|'plate'` mapped through a literal class map; `Button({ variant, label, loading, disabled, onPress })`; `TextField({ label, error, hint, ref, ...TextInputProps })`; `Checkbox({ label, checked, onChange, error })`; `Banner({ tone, children })`; `Plate({ value })`; `ParkingSign({ size })`; `ScreenScroll({ children })` with `contentInsetAdjustmentBehavior="automatic"`; `EmptyState({ title, body, action })`; `Skeleton`; `LanguageSwitch({ value, onChange })`.

The 401 flow in `request`, when `auth` is true: a body with code `auth.unauthenticated` calls `adapter.refresh()`. `refreshed` retries once. `ended` returns the 401. `rate_limited` throws `ApiError('rate_limit.exceeded')` with `details.retry_after_seconds`. `unavailable` throws `ApiError(code)`. A body with `auth.session_revoked` calls `onSessionRevoked()` and returns the 401. A 403 with `legal.acceptance_required` calls `onLegalRequired()` and returns the 403.

- [ ] **Step 1: Write the failing tests**

```ts
// httpClient.test.ts additions (existing get/getApiHealth tests stay until Task 9)
test('request sends method, JSON body and Accept')
test('auth true sends the bearer from the adapter; auth false sends none')
test('abort maps to client.timeout')                     // ApiError code
test('a rejected fetch maps to client.network')
test('a 204 resolves with an undefined body')
test('exposes Retry-After')
test('unwrap copies Retry-After into the ApiError details')
test('timeout constants match the spec')                 // 3000, 20000, 10000
test('a per-request timeout overrides the default')
test('unauthenticated refreshes and retries once')       // two fetch calls to the path, second has the new bearer
test('a second 401 after the retry is returned, not retried') // refresh called once
test('transient refresh failure fails the request with the client code') // ApiError 'client.network'
test('rate-limited refresh fails with rate_limit.exceeded')
test('legal.acceptance_required notifies the adapter')
// tokenStorage.test.ts (expo-secure-store mocked)
test('uses mypark.refresh_token for get, set and remove')
// auth.test.ts
test('starts restoring with no tokens')
test('clearSession wipes tokens and user and records the notice')
// Button.test.tsx
test('loading keeps it pressable and announces busy')    // accessibilityState {busy:true}, not disabled
test('disabled is reported to assistive tech')
test('has a 48 dp minimum height')                       // className contains 'min-h-12'
test('skips the press animation when reduce motion is on') // isReduceMotionEnabled -> true: Animated.timing not called
// TextField.test.tsx
test('announces the error politely and includes it in the label') // accessibilityLiveRegion 'polite'
// Checkbox.test.tsx
test('reports role and checked state')
// Banner.test.tsx
test('error tone is an alert')
// LanguageSwitch.test.tsx
test('marks the selected language and names each in its own language') // accessibilityState.selected; 'Español', 'English'
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/mobile -- src/services src/store/auth.test.ts src/components`
Expected: FAIL.

- [ ] **Step 3: Install the dependencies.** Edit `apps/mobile/package.json`, then run `npm install`.

Run: `npm ls react-hook-form @hookform/resolvers && npm run check:react && (cd apps/mobile && npx expo install --check)`
Expected: one copy of each; single React; `Dependencies are up to date`.

- [ ] **Step 4: Implement** the client, storage, store and primitives. The mobile `Button` uses core `Animated` for a 180 ms scale, since Uniwind's free tier has no transitions (CC-19). Controls render at least 48 dp even where the mockup draws smaller (CC-12). Plate font scale is capped so it stays legible (spec §11).

- [ ] **Step 5: Verify**

Run: `npm test -w apps/mobile && npm run lint -w apps/mobile && npm run typecheck -w apps/mobile && npm run check:tokens`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add apps/mobile package-lock.json
git commit -m "feat(mobile): add primitives, authenticated http client and auth store"
```

---

## Task 9: Mobile session and navigation (slice M2, about 400 lines, sensitive path)

**Files:**
- Create: `apps/mobile/src/services/{session.ts,session.test.ts,runtime.ts}`, `apps/mobile/src/features/session/{root-route.ts,root-route.test.ts,RestoreFailedScreen.tsx,RestoreFailedScreen.test.tsx,change-language.ts,change-language.test.ts}`, `apps/mobile/src/store/settings.test.ts`, `apps/mobile/src/lib/i18n/{translate-error.ts,translate-error.test.ts}`, `apps/mobile/src/app/restore-failed.tsx`
- Modify: `apps/mobile/src/app/_layout.tsx`, `apps/mobile/src/app/index.tsx`, `apps/mobile/src/app/(auth)/_layout.tsx`, `apps/mobile/src/store/settings.ts`, `apps/mobile/src/services/httpClient.ts`, `apps/mobile/src/services/httpClient.test.ts`, `apps/mobile/src/lib/i18n/locales/{es-DO,en}.json`
- Delete: `apps/mobile/src/features/health/HealthStatus.tsx`, `apps/mobile/src/features/health/HealthStatus.test.tsx`

**Interfaces:**
- Consumes: `HttpClient`, `AuthAdapter`, `RefreshResult`, `REFRESH_TIMEOUT_MS`, `tokenStorage`, `useAuthStore` (Task 8); `tokenResponseSchema`, `parseEnvelope`, fixtures (Task 1).
- Produces:
  - `services/session.ts`: `type SessionService = { restore(): Promise<void>; applyTokens(tokens: TokenResponse): Promise<void>; refresh(): Promise<RefreshResult>; logout(scope?: 'this'|'all'): Promise<void>; signOutLocal(notice?: AuthNotice): Promise<void> }`, `createSessionService(deps: { http: HttpClient; storage: TokenStorage; store: typeof useAuthStore; queryClient: QueryClient; onUserLocale(locale: AppLanguage): void; now?: () => number }): SessionService`
  - `services/runtime.ts`: `http`, `queryClient`, `session` singletons, and `http.setAuthAdapter(...)` wiring.
  - `features/session/root-route.ts`: `type RootRoute = 'restoring'|'restore-failed'|'auth'|'gate'|'tabs'`, `selectRootRoute(status: AuthStatus, legalPending: boolean): RootRoute`, `rootHref(route: RootRoute): string | null` (`/restore-failed`, `/welcome`, `/legal-acceptance`, `/vehicles`, and null for `restoring`).
  - `features/session/change-language.ts`: `changeLanguage(language: AppLanguage): Promise<void>` (sets and persists; sends `PATCH /v1/me {locale}` best-effort only when signed in and not gated).
  - `store/settings.ts`: `LANGUAGE_KEY = 'mypark.language'`, `language`, `hydrated`, `setLanguage(language: AppLanguage | null): void` (persists best-effort), `hydrateSettings(): Promise<void>`.
  - `lib/i18n/translate-error.ts`: `translateError(i18n: I18n, error: ApiError): string`, `translateFieldMessage(i18n: I18n, message: string): string`. Same semantics as web.

Root layout, per spec §4.2: hold the splash with `SplashScreen.preventAutoHideAsync()` until fonts load, settings hydrate and `status !== 'restoring'`. Fonts are `useFonts` with the five families in spec §9.4 (CC-25). `Stack.Protected` guards: `(auth)` when route is `auth`, `(gate)` when `gate`, `(tabs)` when `tabs`, `restore-failed` when `restore-failed`. `(recovery)` sits outside the guards. The groups `(gate)`, `(tabs)` and `(recovery)` land in Tasks 10–11; until then expo-router logs a missing-route warning, and tests and typecheck are unaffected. `focusManager` and `AppState` are wired per TanStack's RN guide.

- [ ] **Step 1: Write the failing tests**

```ts
// session.test.ts: real createHttpClient, injected fetchImpl, fake storage, real store
test('single-flight on concurrent 401s')                 // 5 parallel auth requests, all 401 unauthenticated: exactly one POST /v1/auth/refresh, all 5 retried
test('no refresh on 403 password_incorrect')
test('no refresh on session_revoked')                    // signs out, notice 'session_ended'
test('wrong password on login never refreshes')          // auth:false 401 invalid_credentials
test('logout refreshes first on auth.unauthenticated')   // order: logout 401, refresh 200, logout 204; then cleared
test('logout clears locally on a final 401')
test('logout clears locally on a network error')
test('logout with no access token clears locally without calling the server')
test('logout keeps the language setting')
test('signOutLocal clears storage, memory and queries')
test('refresh writes the new token before resolving')    // storage.set pending: refresh() not resolved
test('refresh uses the 10 s timeout and auth false')
test('refresh_invalid clears storage, memory and queries') // queryClient.clear called; status signedOut; notice 'session_ended'
test('network, timeout and 5xx keep tokens')             // storage.remove not called; next request refreshes again
test('429 blocks refresh until Retry-After passes')      // now+29s no POST; now+31s POST, with Retry-After: 30
// restore table, spec §6.4
test('restore: null token is signedOut')
test('restore: a rejected read is signedOut with a best-effort delete')
test('restore: 200 is signedIn with legalPending from the response')
test('restore: refresh_invalid deletes the token and is signedOut')
test('restore: session_revoked deletes the token and is signedOut')
test.each(['network','timeout',503,429])('restore: %s is restoreFailed and keeps the token')
test('restore: retry after restoreFailed can succeed')
// secure-store failure outcomes
test('a write failure deletes the stale token and stays in memory') // set rejects: remove called; status signedIn; accessToken set; notice 'storage_failed'
test('a write failure where delete also fails does not throw')
test('applyTokens calls onUserLocale with user.locale')
// root-route.test.ts
test.each([
  ['restoring', false, 'restoring'], ['restoreFailed', false, 'restore-failed'],
  ['signedOut', false, 'auth'], ['signedIn', true, 'gate'], ['signedIn', false, 'tabs'],
])('%s with legalPending %s goes to %s')
test('a signedOut user never reaches gate even with a stale legalPending')
// RestoreFailedScreen.test.tsx
test('offers Retry and Log out and keeps the token until Log out')
// settings.test.ts
test('language persists')                                // setLanguage('en'): setItemAsync('mypark.language','en')
test('hydrate restores a stored language')
test('hydrate ignores an unknown stored value')          // 'fr' -> null
test('hydrate survives a read failure')                  // rejects -> null, hydrated true
test('a write failure keeps the in-memory language')
// change-language.test.ts
test('patches the account when signed in and not gated')
test('does not patch behind the legal gate')
test('a failed patch keeps the local change')
// translate-error.test.ts
test('uses errors.<code> and falls back to errors.fallback, never message')
test('validation.min interpolates its param in both languages')
test('rate limit interpolates seconds')
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/mobile -- src/services/session.test.ts src/features/session src/store/settings.test.ts src/lib/i18n/translate-error.test.ts`
Expected: FAIL.

- [ ] **Step 3: Implement** `session.ts` with one module-level in-flight promise per service. `refresh()` returns the running promise if there is one. It posts `{refresh_token}` with `auth:false` and `REFRESH_TIMEOUT_MS`, and it parses with `tokenResponseSchema`. `applyTokens` writes to storage **before** setting state and before resolving.

- [ ] **Step 4: Implement the runtime, screens and layouts.** Remove `get`, `getApiHealth`, their tests and the health screen. `index.tsx` becomes `<Redirect href={rootHref(route)} />`, rendering nothing while restoring. `(auth)/_layout.tsx` keeps its `Stack`. Add `common`, `session` and `restoreFailed` keys to both mobile locale files.

- [ ] **Step 5: Verify**

Run: `npm run check:i18n && npm test -w apps/mobile && npm run lint -w apps/mobile && npm run typecheck -w apps/mobile && (cd apps/mobile && npx expo install --check)`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add apps/mobile
git commit -m "feat(mobile): add session restore, single-flight refresh and protected navigation"
```

---

## Task 10: Mobile auth screens (slice M3, about 400 lines)

**Files:**
- Create: `apps/mobile/src/lib/forms/{apply-api-error.ts,apply-api-error.test.ts,use-retry-lock.ts}`, `apps/mobile/src/store/{auth-flow.ts,auth-flow.test.ts}`, `apps/mobile/src/testing/render.tsx`, `apps/mobile/src/features/auth/api.ts`, `apps/mobile/src/features/auth/api.test.ts`, `apps/mobile/src/features/auth/{WelcomeScreen,LoginScreen,RegisterScreen,VerifyScreen,ForgotPasswordScreen,ResetPasswordScreen,LegalDocumentScreen}.tsx` and a `.test.tsx` for each except `LegalDocumentScreen`
- Create routes: `apps/mobile/src/app/(auth)/{welcome,login,register,verify}.tsx`, `apps/mobile/src/app/(auth)/legal/[kind].tsx`, `apps/mobile/src/app/(recovery)/{_layout,forgot-password,reset-password}.tsx`
- Modify: `apps/mobile/src/lib/i18n/locales/{es-DO,en}.json`

**Interfaces:**
- Consumes: Task 1 schemas and fixtures, `translateError`, `translateFieldMessage`, `session`, `http`, `changeLanguage` (Task 9), primitives and `ARGON2_TIMEOUT_MS` (Task 8).
- Produces:
  - `lib/forms/apply-api-error.ts`: `applyApiError<T extends FieldValues>(form: UseFormReturn<T>, error: unknown, i18n: I18n): void`, `retryAfterSeconds(error: ApiError): number`. Same field-mapped codes and fallback as web. Banner errors also call `AccessibilityInfo.announceForAccessibility`.
  - `lib/forms/use-retry-lock.ts`: same signature as web.
  - `store/auth-flow.ts`: `useAuthFlowStore` with `email`, `setEmail(email: string): void`, `clear(): void`. Memory only.
  - `features/auth/api.ts`: `login(form: LoginValues): Promise<TokenResponse>`, `fetchLegal(): Promise<LegalCurrent>`, `register(body: RegisterRequest): Promise<void>`, `verify(form: VerifyValues): Promise<void>`, `resendVerification(email: string): Promise<void>`, `forgotPassword(email: string): Promise<void>`, `resetPassword(form: ResetValues): Promise<void>`.
  - `testing/render.tsx`: `renderScreen(ui: ReactElement, opts?: { language?: AppLanguage }): Promise<void>` (RN Testing Library 14 `render` is async).

Legal screens render bundled locale strings and never open the seeded URLs (CC-21). Register sends the current language as `locale` (spec §6.6).

- [ ] **Step 1: Write the failing tests**

```ts
// api.test.ts
test('argon2 calls use the 20 s timeout')                // login, register, resetPassword: timeoutMs 20000
test('login sends client_kind mobile')
// apply-api-error.test.ts
test.each(['required','email','min','max','oneof','phone','code6','password_common','plate'])
  ('maps the server rule %s to its field and message')
test('focuses only the first invalid field')
test('a field the form lacks falls back to the banner')
test('retryAfterSeconds prefers details, then the header')
test('retryAfterSeconds falls back to 60')
// WelcomeScreen.test.tsx
test('offers Sign up, Log in and the language switch, with no carousel')
// LoginScreen.test.tsx
test('success applies tokens and sets the language from the user') // session.applyTokens called; user.locale
test('wrong credentials show one banner')
test('email_not_verified goes to verify keeping only the email')
test('shows the session-ended banner once')              // notice 'session_ended': 'Tu sesión terminó. Inicia sesión de nuevo.'
test('pre-fills the email from authFlow, password empty')
test('429 shows the wait and disables submit')
test('the password never crosses screens')               // JSON.stringify(flow.getState()), router.push and router.replace args contain no password
// RegisterScreen.test.tsx
test('sends legal versions, the current language and empty phone for +1')
test('field error mapping includes password_common')
test('requires the terms checkbox')
test('legal fetch failure shows retry')
test('success goes to verify and resets the form')
// VerifyScreen.test.tsx
test('bad code is a translated field error with resend') // errors.auth.code_invalid
test('verified goes to login with the email and an empty password')
test('resend shows the same message on every outcome')
// ForgotPasswordScreen.test.tsx
test('shows the same sent message')
// ResetPasswordScreen.test.tsx
test('success signs out locally and goes to login pre-filled') // session.signOutLocal called before navigation
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/mobile -- src/lib/forms src/store/auth-flow.test.ts src/features/auth`
Expected: FAIL.

- [ ] **Step 3: Implement** the helpers, stores, API module, containers and thin route files. Use `textContentType` and `autoComplete` per spec §11. Move focus with `setFocus` on the first invalid field. Banners use a live region on Android and `announceForAccessibility` on iOS.

- [ ] **Step 4: Verify**

Run: `npm run check:i18n && npm test -w apps/mobile && npm run lint -w apps/mobile && npm run typecheck -w apps/mobile`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add apps/mobile
git commit -m "feat(mobile): add welcome, sign-up, verify, login and recovery screens"
```

---

## Task 11: Mobile vehicles, profile, legal gate (slice M4, about 400 lines)

**Files:**
- Create: `apps/mobile/src/features/vehicles/{api.ts,VehiclesScreen.tsx,VehiclesScreen.test.tsx,VehicleFormScreen.tsx,VehicleFormScreen.test.tsx}`, `apps/mobile/src/features/profile/{api.ts,api.test.ts,ProfileScreen.tsx,ProfileScreen.test.tsx,DeleteAccountForm.tsx,DeleteAccountForm.test.tsx}`, `apps/mobile/src/features/legal/{LegalGateScreen.tsx,LegalGateScreen.test.tsx}`
- Create routes: `apps/mobile/src/app/(gate)/{_layout,legal-acceptance}.tsx`, `apps/mobile/src/app/(tabs)/_layout.tsx`, `apps/mobile/src/app/(tabs)/vehicles/{_layout,index,new,[id]}.tsx`, `apps/mobile/src/app/(tabs)/profile/{_layout,index,delete}.tsx`
- Modify: `apps/mobile/src/lib/i18n/locales/{es-DO,en}.json`

**Interfaces:**
- Consumes: `http`, `session`, `queryClient`, `changeLanguage` (Task 9), `applyApiError`, `useRetryLock`, `renderScreen`, `useAuthFlowStore`, `fetchLegal` (Task 10), primitives (Task 8), Task 1 schemas and fixtures.
- Produces:
  - `features/vehicles/api.ts`: `VEHICLES_QUERY_KEY = ['vehicles'] as const`, `listVehicles(): Promise<{ items: Vehicle[]; page: Page }>`, `getVehicle(id: string): Promise<Vehicle>`, `createVehicle(input: VehicleInputValues): Promise<Vehicle>`, `updateVehicle(id: string, input: VehicleInputValues): Promise<Vehicle>`, `deleteVehicle(id: string): Promise<void>`
  - `features/profile/api.ts`: `getMe(): Promise<User>`, `updateMe(input: UpdateMeValues): Promise<User>`, `deleteMe(form: DeleteMeValues): Promise<void>` (20 s), `acceptLegal(body: AcceptLegalValues): Promise<void>`
  - `DeleteAccountForm({ onDeleted }: { onDeleted(): void })`, shared by the Profile delete screen and the gate.

`(tabs)/_layout.tsx` uses JS `Tabs` from expo-router with two tabs and the active pill (CC-19). Each tab has its own `Stack`. Vehicle delete uses native `Alert` (CC-22).

- [ ] **Step 1: Write the failing tests**

```ts
// VehiclesScreen.test.tsx
test('loading shows a skeleton')
test('empty shows the ghost plate and add-your-first-car')
test('lists plates as text and the count')               // '2 de 5'
test('at cap disables Add with a visible reason')
test('error shows retry')
// VehicleFormScreen.test.tsx
test('create posts what the user typed')
test('double tap on save sends one request')
test('plate_taken lands under plate in both languages')
test('limit_reached shows a banner')
test('edit loads the vehicle and patches')
test('delete confirms with a native Alert before calling the API') // Alert.alert spied; no DELETE until the destructive button
test('vehicle_in_use shows a banner')
test('not_found returns to the list and invalidates')
// ProfileScreen.test.tsx
test('email is read-only; name and phone patch')
test('language switch persists and patches')
test('links to forgot password')
test('log out and log out everywhere go through the session service')
// DeleteAccountForm.test.tsx + api.test.ts
test('argon2 calls use the 20 s timeout')                // deleteMe timeoutMs 20000
test('explains consequences and requires the password')
test('password_incorrect lands on password and never refreshes')
test('success signs out and calls onDeleted')
test('the language setting survives account deletion')
// LegalGateScreen.test.tsx
test('legal gate blocks')                                // no vehicles content; accept, log out and delete visible
test('accept sends current versions and clears legalPending')
test('version_outdated shows a banner and re-reads')
test('logout works behind the gate')
test('delete works behind the gate')
```

- [ ] **Step 2: Run to verify they fail**

Run: `npm test -w apps/mobile -- src/features/vehicles src/features/profile src/features/legal`
Expected: FAIL.

- [ ] **Step 3: Implement** the containers and route files. Save handlers return early while `mutation.isPending` (Review Focus 4). On mobile, a 403 `legal.acceptance_required` flips `legalPending` through the adapter (Task 9), so these screens need no gate logic.

- [ ] **Step 4: Verify**

Run: `npm run check:i18n && npm run check:tokens && npm test -w apps/mobile && npm run lint -w apps/mobile && npm run typecheck -w apps/mobile && (cd apps/mobile && npx expo install --check)`
Expected: all pass.

- [ ] **Step 5: Confirm navigation in Expo Go (spec §14).** Run `cd apps/mobile && EXPO_PUBLIC_API_URL=http://<LAN-IP>:8080 npx expo start` and open the app on the demo Android phone. Check first-screen selection for signed out, gated and clear states, and a TalkBack pass.
Expected: Welcome, the gate and Vehicles respectively. If `Stack.Protected` lands on the wrong first screen, adjust `anchor` or `redirectTo` and record it in the PR.

- [ ] **Step 6: Commit**

```bash
git add apps/mobile
git commit -m "feat(mobile): add vehicles, profile, account deletion and legal gate"
```

---

## Task 12: Docs and the demo script (slice D1, about 200 lines)

**Files:**
- Create: `docs/m1-demo-script.md`
- Modify: `docs/architecture.md` §5, §13, §16, §17; `docs/policies.md` §1.9, §2.7; `docs/spec.md` §8 (M1 exit boxes), §10.2

**Interfaces:**
- Consumes: the working clients from Tasks 1–11.
- Produces: `docs/m1-demo-script.md`, referenced by the M1 PR description.

- [ ] **Step 1: Run the full gate before touching docs**

Run: `npm run check:react && npm run check:i18n && npm run check:tokens && npm run test:scripts && npm test -w packages/contracts && npm run typecheck -w packages/contracts && npm test -w apps/web && npm run lint -w apps/web && npm run typecheck -w apps/web && npm run build -w apps/web && npm test -w apps/mobile && npm run lint -w apps/mobile && npm run typecheck -w apps/mobile && (cd apps/mobile && npx expo install --check)`
Expected: every command exits 0.

- [ ] **Step 2: Write `docs/m1-demo-script.md`** with this content (English, policies §2.10).

````markdown
# M1 demo script

Runs against the real backend. Web at `http://localhost:3000`, Mailpit at `http://localhost:8025`, the app in Expo Go on an Android phone on the same network.

## Setup

```bash
# Once, if the stack ran before the subnet was pinned. Keeps the database volume.
docker compose -f deploy/compose/docker-compose.yml down
docker compose -f deploy/compose/docker-compose.yml up -d --build
cd apps/mobile && EXPO_PUBLIC_API_URL=http://<LAN-IP>:8080 npx expo start
```

Allow port 8080 through Windows Firewall. Open the project in Expo Go.

Run sign-ups at least one minute apart. Local web users share one rate-limit bucket (register is 3 per minute per IP).

Shorthand for the database edits below, in Git Bash:

```bash
psql_mypark() { docker compose -f deploy/compose/docker-compose.yml exec -T postgres psql -U postgres -d mypark -c "$1"; }
```

## Steps

| # | Where | Do | Expect |
|---|---|---|---|
| 1 | Phone | Open the app, switch language on Welcome | es-DO and en both work |
| 2 | Phone | Sign up as `ana.demo@example.com`, leave the phone as `+1` | Goes to Verify |
| 3 | Phone | Enter `000000` | Translated error under the code field |
| 4 | Mailpit, phone | Read the code, enter it | Goes to Login, email pre-filled, password empty |
| 5 | Phone | Log in | Vehicles is empty, with "add your first car" |
| 6 | Phone | Kill and reopen the app | Session restored, no login |
| 7 | Phone | Add `A123456`, then add the same plate on a second car | Plate shown; second attempt shows the plate-taken error under the plate |
| 8 | Web | Log in with the same account | Same vehicle list |
| 9 | Web | Edit a vehicle, delete it, switch language | Plate form works; errors translate |
| 10 | Web | Sign up `luis.demo@example.com`, verify, log in | Same flow; login pre-filled |
| 11 | Web | Open two tabs, delete `mp_access` in dev tools, reload both | Still logged in, in both |
| 12 | Web | As the driver, open `/es-DO/admin` | 403 page (check the Network panel) |
| 13 | Web | Log out, log in at `/es-DO/admin/login` as `admin@mypark.local` / `mypark-dev-admin-2026` | Admin landing |
| 14 | Web | Open `/es-DO/vehicles` as admin | Redirect to `/es-DO/admin` |
| 15 | DB, web, phone | Run the "publish terms" statement, **wait at least 60 s** (the API caches current versions), then reload as Ana | Gate blocks on both clients; Log out and account deletion stay available; Accept continues |
| 16 | DB, web | Run the "suspend" statement, then reload as Ana | Account-blocked panel with Log out |
| 16b | DB | Run the "restore" statement | Ana can use the app again |
| 17 | Phone | Profile, danger zone, enter password | Back to Welcome; logging in with Ana now fails |

Step 16b is needed because a suspended user cannot delete the account. Run it within about 14 minutes of step 16, before refresh ends the session.

## Database edits (M1 has no API for these)

**Publish terms (step 15)**

```bash
psql_mypark "insert into legal_documents (kind, version, published_at, url_es, url_en) values ('terms', '2026-12-01', now() - interval '1 minute', 'http://localhost:3000/es-DO/legal/terms', 'http://localhost:3000/en/legal/terms'), ('privacy', '2026-12-01', now() - interval '1 minute', 'http://localhost:3000/es-DO/legal/privacy', 'http://localhost:3000/en/legal/privacy');"
```

To run the demo again, first `delete from legal_documents where version = '2026-12-01';`.

**Suspend (step 16)**

```bash
psql_mypark "update users set status = 'suspended' where email = 'ana.demo@example.com';"
```

**Restore (step 16b)**

```bash
psql_mypark "update users set status = 'active' where email = 'ana.demo@example.com';"
```

## Accessibility check

Keyboard and NVDA over the web login, vehicles and profile screens. TalkBack over the phone's Welcome, Login and Vehicles screens.
````

- [ ] **Step 3: Run the demo script end to end.** Record any step that deviates and fix the owning code before continuing.
Expected: all 18 rows behave as listed.

- [ ] **Step 4: Update `docs/architecture.md`**
  - §5 "Client IP" row: replace "the left-most `X-Forwarded-For` entry" with "the right-most entry outside `TRUSTED_PROXY_CIDRS`", and add the M1 limitation (X1, CC-5). Replace "`proxy.ts` refreshes the token transparently" with the proxy-only rule (CC-1).
  - §13: note that M1 ships the Vehicles and Profile tabs only (X11).
  - §16: replace "MSW" for web and mobile with typed fixtures stubbed at the fetch boundary (X9, CC-14).
  - §17: add `ADR-13 | proxy.ts is the sole web refresher, with an in-flight lock and a 30 s result cache bound to mp_csrf | Refresh in RSC or route handlers; a backend replay window | RSC cannot set cookies, one entry point means one lock, and refresh tokens stay strictly single-use`.

- [ ] **Step 5: Update `docs/policies.md`**
  - §1.9 Assets row: allow colors estimated from screenshots as `TODO(brand)` placeholders until official assets arrive (X2).
  - §2.7 Sessions row: `Secure` applies when `APP_ENV=production` (CC-4).

- [ ] **Step 6: Update `docs/spec.md`**
  - §8 M1 exit criteria: mark the three *Needs Track B* boxes done and cite `docs/m1-demo-script.md`.
  - §10.2: add item 38, "M1 client calls CC-1 to CC-27 are accepted as listed in the M1 clients design (`docs/superpowers/specs/2026-11-08-m1-clients-auth-vehicles-design.md` §2.2)."

- [ ] **Step 7: Verify nothing else moved**

Run: `npm run check:i18n && npm run check:tokens && git status --porcelain`
Expected: checks pass; only the four docs appear.

- [ ] **Step 8: Commit**

```bash
git add docs/m1-demo-script.md docs/architecture.md docs/policies.md docs/spec.md
git commit -m "docs: add m1 demo script and record client decisions"
```
