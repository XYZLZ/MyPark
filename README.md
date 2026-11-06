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

## Quick start

Run the whole backend stack (Postgres, migrations, seed, API, worker) with Docker:

```bash
docker compose -f deploy/compose/docker-compose.yml up -d --build
```

No `.env` file is needed: every variable has an inline dev-only default. Override values in
`deploy/compose/.env` or in your shell. Never reuse the dev credentials outside local development.

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
