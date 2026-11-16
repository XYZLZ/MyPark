import { availabilitySchema, heartbeatSchema } from '../m2/availability';
import { lotSchema, lotSummarySchema } from '../m2/lot';
import { simulatorEventSchema, simulatorStatusSchema } from '../m2/simulator';
import { lotSpacesSchema, spaceStateSchema, spacesEventSchema, type SpaceStatus } from '../m2/spaces';
import { tariffSchema } from '../m2/tariff';

/** Typed fixtures for client tests. Each is parsed by its schema at module load, so it cannot drift. Values come from the approved mockup. */

const LOT_ID = '3f2b8c1e-6a4d-4e5f-9b7a-1c2d3e4f5a6b';

export const lotSummaryFixture = lotSummarySchema.parse({
  id: LOT_ID,
  name: 'Parqueo Banreservas Zona Colonial',
  slug: 'banreservas-zona-colonial',
});

export const lotFixture = lotSchema.parse({
  ...lotSummaryFixture,
  address: 'Calle Arzobispo Meriño No. 386, Zona Colonial, Santo Domingo',
  latitude: 18.473,
  longitude: -69.884,
  map_url: 'https://www.google.com/maps/search/?api=1&query=18.4730,-69.8840',
  timezone: 'America/Santo_Domingo',
  capacity: 280,
  opening_hours: { always_open: true },
  status: 'open',
});

const availabilityBase = {
  lot_id: LOT_ID,
  effective_holds: 0,
  upcoming_reservations_2h: 0,
  updated_at: '2026-11-15T14:47:00Z',
};

/** Three spaces out of service: effective capacity 277. */
export const availabilityReducedFixture = availabilitySchema.parse({
  ...availabilityBase,
  available: 211,
  occupied: 66,
  effective_capacity: 277,
  capacity: 280,
  status: 'open',
  version: 120,
});

export const availabilityOpenFixture = availabilitySchema.parse({
  ...availabilityReducedFixture,
  effective_capacity: 280,
  available: 214,
});

export const availabilityFullFixture = availabilitySchema.parse({
  ...availabilityReducedFixture,
  available: 0,
  occupied: 277,
  status: 'full',
  version: 121,
});

export const availabilityClosedFixture = availabilitySchema.parse({
  ...availabilityReducedFixture,
  status: 'closed',
  version: 122,
});

export const heartbeatFixture = heartbeatSchema.parse({ as_of: '2026-11-15T14:47:15Z' });

function spaceStatus(n: number): SpaceStatus {
  if (n <= 66) return 'occupied';
  if (n >= 278) return 'out_of_service';
  return 'free';
}

export const spacesFixture = lotSpacesSchema.parse({
  spaces: Array.from({ length: 280 }, (_, i) => ({ code: `E${String(i + 1).padStart(3, '0')}`, state: spaceStatus(i + 1) })),
});

export const spacesFullEventFixture = spacesEventSchema.parse({ full: true, spaces: spacesFixture.spaces });

export const spacesDeltaFixture = spacesEventSchema.parse({
  full: false,
  spaces: [spaceStateSchema.parse({ code: 'E100', state: 'occupied' })],
});

export const tariffFixture = tariffSchema.parse({
  effective_from: '2026-01-01T00:00:00Z',
  currency: 'DOP',
  first_hour_cents: 5000,
  additional_hour_cents: 2500,
  daily_cap_cents: 40000,
  lost_ticket_cents: 50000,
  exit_grace_minutes: 10,
  post_payment_exit_minutes: 15,
  reservation_fee_cents: 0,
});

export const simulatorStatusIdleFixture = simulatorStatusSchema.parse({
  scenario: '',
  running: false,
  seed: 0,
  speed: 30,
  virtual_now: '2026-11-15T14:47:00Z',
  cars_inside: 0,
  reached_full: false,
});

export const simulatorStatusRunningFixture = simulatorStatusSchema.parse({
  scenario: 'weekday',
  running: true,
  seed: 42,
  speed: 30,
  virtual_now: '2026-11-15T14:47:00Z',
  cars_inside: 66,
  started_at: '2026-11-15T14:30:00Z',
  reached_full: false,
});

export const simulatorEventsFixture = [
  { seq: 1841, at: '2026-11-15T14:46:10Z', device: 'entry-barrier-1', type: 'barrier.passed', result: 'open' },
  { seq: 1845, at: '2026-11-15T14:46:40Z', device: 'dispenser-1', type: 'entry.credential_presented', result: 'deny', reason: 'gate.credential_in_use', car_id: 'car-000213' },
  { seq: 1846, at: '2026-11-15T14:46:55Z', device: 'dispenser-1', type: 'entry.credential_presented', result: 'deny', reason: 'gate.lot_full', car_id: 'car-000214' },
].map((event) => simulatorEventSchema.parse(event));

/** Wire text of one SSE frame: `event: …\n[id: …\n]data: <json>\n\n`. */
export function sseText(event: 'availability' | 'spaces' | 'heartbeat', data: unknown, id?: number): string {
  return `event: ${event}\n${id === undefined ? '' : `id: ${id}\n`}data: ${JSON.stringify(data)}\n\n`;
}
