# MyPark mobile (Expo SDK 58)

Driver app for MyPark · Banreservas Zona Colonial. Expo Router, Uniwind (Tailwind v4), i18next, zustand and TanStack Query, inside the npm-workspaces monorepo.

## Expo docs are pinned to SDK 58. Do not trust your training data.

- Versioned docs root: https://docs.expo.dev/versions/v58.0.0/
- Every SDK package has an LLM-friendly page at `https://docs.expo.dev/versions/v58.0.0/sdk/<package>/index.md`, where `<package>` is usually the package name without the `expo-` prefix. Some packages also drop their hyphens (`expo-secure-store` → `securestore`), so check the docs index when unsure. Examples:
  - expo-localization: https://docs.expo.dev/versions/v58.0.0/sdk/localization/index.md
  - expo-router: https://docs.expo.dev/versions/v58.0.0/sdk/router/index.md
  - expo-secure-store: https://docs.expo.dev/versions/v58.0.0/sdk/securestore/index.md
- Index of all docs: https://docs.expo.dev/llms.txt. Monorepos: https://docs.expo.dev/guides/monorepos/

## Commands (from the repo root, Git Bash or Linux)

```bash
npm test -w apps/mobile          # jest-expo + React Native Testing Library
npm run lint -w apps/mobile      # expo lint
npx -w apps/mobile tsc --noEmit  # typecheck
(cd apps/mobile && npx expo install --check)   # SDK-compatible versions
(cd apps/mobile && EXPO_PUBLIC_API_URL=http://<LAN-IP>:8080 npx expo start)
```

## Dependencies

- One root `package-lock.json`. Never add a nested lockfile; never use pnpm/yarn/bun.
- Expo packages: take the version `npx expo install --check` expects (see `expo/bundledNativeModules.json`). While the root `overrides` has a scoped entry, `npm install <pkg>` can fail with ERESOLVE: edit `package.json` by hand, then run `npm install` at the root.
- React is 19.3.0 across the workspace; `npm ls react` must show a single version.

## Conventions

- Routes only in `src/app/` (Expo Router). No tests or helpers there.
- `src/features/<feature>/` containers and presentational pieces, `src/services/` I/O, `src/store/` zustand, `src/lib/` i18n, env and utils.
- Tests sit next to their source as `*.test.ts(x)`.
- No hardcoded user-facing strings: add keys to both `src/lib/i18n/locales/*.json` (`npm run check:i18n`).
- Styling via `className` (Uniwind). Brand tokens live in `src/global.css` `@theme` and `src/design/tokens.ts` (`TODO(brand)` placeholders).
- `EXPO_PUBLIC_API_URL` must be read as a literal `process.env.EXPO_PUBLIC_API_URL` (see `src/lib/env.ts`).
