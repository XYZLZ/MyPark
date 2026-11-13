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

If host port 5432 is already taken, prefix the `up` command with `POSTGRES_PORT=5433`. Run every command below from the repository root (open a second terminal for Expo).

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
| 15 | DB, web, phone | Run the "publish terms" statement, **wait at least 60 s** (the API caches current versions), then reload as Ana | Gate blocks on both clients and shows the legal texts; Log out and account deletion stay available; Accept continues |
| 16 | DB, web | Run the "suspend" statement, then reload as Ana | Account-blocked panel with Log out |
| 16b | DB | Run the "restore" statement | Ana can use the app again |
| 17 | Phone | Profile, danger zone, enter password | Back to the signed-out screens (Welcome or Login); logging in with Ana now fails |

Step 16b is needed because a suspended user cannot delete the account. Run it within the 15-minute access-token lifetime of step 16, before a refresh ends the session.

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
