# M2 demo script

Shows live availability on the web, the phone and the Simulator console, against the real backend. Run it from the repository root. It takes about 20 minutes, most of it the 12-minute wait in step 6.

**Status:** written, not yet run end to end. Run it once, record the date in `docs/spec.md` §8 M2, and tick the checklists at the bottom only for what you saw.

## Setup

```bash
docker compose -f deploy/compose/docker-compose.yml up -d --build
cd apps/mobile && EXPO_PUBLIC_API_URL=http://<LAN-IP>:8080 npx expo start
```

| Piece | Where |
|---|---|
| Web | `http://localhost:3000` |
| API (the browser opens the live stream here) | `http://localhost:8080`, from `API_PUBLIC_URL` |
| Console | `http://localhost:3000/es-DO/admin/simulator` (the Simulator item in the admin sidebar) |
| Phone | Expo Go on the same network. Allow port 8080 through the firewall |
| Admin | `admin@mypark.local` / `mypark-dev-admin-2026` at `/es-DO/admin/login` |
| Driver | Any verified account (the M1 script, steps 2 to 4, creates one). Optional: step 1 needs none |

The console needs `SIMULATOR_INTERNAL_URL` and `SIMULATOR_CONTROL_TOKEN` on the `web` service. The compose file sets both. Without them the console says the simulator is not configured.

## Steps

| # | Where | Do | Expect |
|---|---|---|---|
| 1 | Web, phone | Open the web home and the phone Welcome, signed out. Switch language on both | Count, status, map, lot info and rates, in es-DO and en |
| 2 | Console | Log in as admin, open the console, start `weekday` at speed 30. Keep web, phone and console visible together | The count and the map move on both screens within 2 s of the log row. The log fills. Time the web and the phone against the log by eye |
| 3 | Console | Straight away, add one car with "Hold inside until released" checked (works while idle too: it starts an implicit idle session) | The car id appears under Held cars, and its entry shows in the log |
| 4 | Console | Under Sensor fault, pick a free space (codes look like `E014`; find one on the map), choose "Stuck occupied", apply | That map cell changes. The count does not, because the count comes from the entry and exit counter, not the sensors |
| 5 | Terminal | `docker compose -f deploy/compose/docker-compose.yml stop api`, wait 50 s, then `... start api` | Both clients show Stale with the last update time, then return to live. No manual reload |
| 6 | Console | About 12 **real** minutes after step 3, press Leave on the held car | It pays RD$50 and exits, and the count rises. Business time follows the server clock, so leaving earlier exits at RD$0 inside the 10 min grace |
| 7 | Console | Stop the run, then start `fill_to_full` | Status Full on web and phone. The log shows denied entries with `gate.lot_full` |
| 8 | Phone | Background the app for 1 min, then return | Fresh values and no Stale label once reconnected |

## Notes

- **Shared stream bucket.** The API limits stream connections per client IP (30 per minute). Through Docker Desktop, browsers and the phone probably reach the API from the same address, so every tab and the phone can share one bucket. A client that hits the limit backs off and retries, with a floor of 10 s. Open as few tabs as you can.
  - The real IP the API logs for the browser and the phone is still to be checked (design Y2, risk R1). Record it here after the first run: `browser: ____`, `phone: ____`.
- **The map and the count can differ on purpose.** The count is the entry and exit counter. The map shows what each sensor reads. Step 4 shows it.
- **Step 6 is slow on purpose.** The tariff has a 10 minute grace, and the server clock drives it. Start step 3 early and do steps 4, 5 and 8 while you wait.
- **Event log limits.** The log keeps the last 500 events. It cannot tell that the simulator restarted between two successful polls, so after a restart you may see no "The simulator restarted." row. Reload the page to reset it.
- **Notice banner (web, wide screens).** The logout-failed notice is first in the page order but sits below the hero and counter at the `lg` breakpoint. It is not hidden.

## Accessibility check

- [ ] NVDA on the web home page: the count, the status and the map summary are announced once, and updates are polite
- [ ] NVDA on the console: start, add car, leave and fault results are announced, and focus lands on the heading or the result
- [ ] Keyboard only on the console: every action reachable and visible focus
- [ ] TalkBack on the phone Welcome, Inicio and the full map: the map is skipped and the summary and free-space list are read
- [ ] 360 dp phone at font scale 1.0 and 1.3, and a large phone (430 x 932) at 1.3: no horizontal scroll and no clipped text (M2B-52)
- [ ] Web at 200 % zoom and at 320 px wide: no horizontal scroll, the map reflows to 10 columns

## Device and browser checks still pending

Nothing below has been run. Tick each one only after you did it.

**Phone**
- [ ] `expo/fetch` streams incrementally in Expo Go on Hermes, `abort()` ends the read, and `TextDecoder` works (otherwise the 5 s polling fallback applies)
- [ ] iPhone with a notch: the header is full-bleed under the status bar, with no surface strip
- [ ] The Button focus ring shows on a device. It relies on Uniwind `outline` support. If it does not, switch to a `focus:ring-*` or a contrasting border
- [ ] Hermes `Intl` with `America/Santo_Domingo` and `DOP` formats the time and the price (otherwise the fixed -04:00 offset and `money.dop` apply)

**Web**
- [ ] `pagehide` and `pageshow` close and reopen the stream under the Next router (otherwise close on hidden after 60 s)
- [ ] SSE client IP in the API log (see Notes)
- [ ] The production build on port 3001 works against the compose API (`curl`), since compose owns 3000

**Console**
- [ ] With an admin session on the compose stack, `curl` the BFF: `GET /api/admin/simulator/status` returns the status, and a driver session gets 403
- [ ] Full console run on compose with the steps above
