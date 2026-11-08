# MyPark

Real-time parking availability and reservations for the Banreservas parking facility in Zona
Colonial, Santo Domingo ("MyPark · Banreservas Zona Colonial"). This monorepo holds the
Go API and background services, the Next.js web app, the Expo mobile app, and the API contracts
they share.

## Toolchain

| Tool       | Version                                          |
| ---------- | ------------------------------------------------ |
| Go         | 1.27                                             |
| Node.js    | 22 (`.nvmrc`; `engines.node` is `>=22`)          |
| npm        | 11 (workspaces; one root `package-lock.json`)    |
| TypeScript | 6.0.3 (Expo SDK 58 + Next 16.4 compatible)       |
| Docker     | Docker Engine with Compose v2                    |
| PostgreSQL | 18 (`postgres:18` image)                         |

npm is the only supported package manager. Do not use pnpm or yarn.

Add dependencies by editing package.json, then run `npm install` (scoped overrides make `npm install <pkg>` fail).

## Quick start

Run the whole stack (Postgres, migrations, seed, API, worker, web) with Docker:

```bash
docker compose -f deploy/compose/docker-compose.yml up -d --build
```

Services published on the host:

| Service | URL |
|---|---|
| Web | <http://localhost:3000/es-DO> |
| API | <http://localhost:8080> |
| Simulator | <http://localhost:8090> |
| Mailpit UI | <http://localhost:8025> |

Every published port except the API (8080)
is bound to `127.0.0.1`; the API listens on all interfaces so a phone running Expo on the LAN can
reach it.

To run the web app outside Docker against the running API:

```bash
API_INTERNAL_URL=http://localhost:8080 npm run dev -w apps/web
```

To run the mobile app, point `EXPO_PUBLIC_API_URL` at the API as the device sees it:

| Target | `EXPO_PUBLIC_API_URL` |
|---|---|
| Physical device (Expo Go / dev client) | `http://<LAN-IP>:8080` |
| Android emulator | `http://10.0.2.2:8080` |

```bash
(cd apps/mobile && EXPO_PUBLIC_API_URL=http://<LAN-IP>:8080 npx expo start)
```

No `.env` file is needed: every variable has an inline dev-only default in
`deploy/compose/docker-compose.yml`. Override any of them in your shell, for example
`SEED_ADMIN_PASSWORD='another-long-dev-password' docker compose -f deploy/compose/docker-compose.yml up -d --build`.
Never reuse the dev credentials outside local development.

Auth and seed variables (M1):

| Variable | Used by | Purpose |
|---|---|---|
| `SEED_ADMIN_EMAIL`, `SEED_ADMIN_FIRST_NAME`, `SEED_ADMIN_LAST_NAME`, `SEED_ADMIN_PASSWORD` | seed | First admin, created only when no admin exists. Dev defaults: `admin@mypark.local`, `MyPark`, `Admin` |
| `JWT_SIGNING_KEY`, `JWT_KEY_ID` | api | Ed25519 seed (base64) and its `kid` |
| `JWT_VERIFY_KEYS` | api | Optional `kid:base64(public key)` pairs, comma-separated, for keys being rotated out |
| `AUTH_CODE_PEPPER` | api | Pepper (at least 32 bytes) for hashing one-time codes |
| `TRUSTED_PROXY_CIDRS`, `MAX_VEHICLES_PER_USER` | api | Proxies allowed to set `X-Forwarded-For`, and the per-driver vehicle cap |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_FROM`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_STARTTLS` | worker | Outgoing email; defaults send to Mailpit |

With `APP_ENV=production` the api and seed refuse the committed dev signing key, pepper and seed password.

Install JavaScript dependencies from the repo root:

```bash
npm install
```

## Repository layout

```text
apps/
  server/      Go API, worker, migrations, seed and simulator
  web/         Next.js web app
  mobile/      Expo mobile app
packages/
  contracts/   API contracts shared by web and mobile
deploy/
  compose/     Docker Compose stack for local development
docs/          Product spec, architecture and policies
```

## Documentation

- [Product spec](docs/spec.md)
- [Architecture](docs/architecture.md)
- [Policies](docs/policies.md)
