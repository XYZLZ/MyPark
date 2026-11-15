import { z } from 'zod';

export const SIMULATOR_SCENARIOS = ['idle', 'weekday', 'weekend_peak', 'fill_to_full', 'sensor_drift', 'reservation_arrivals', 'no_show'] as const;
/** The scenarios the console offers (M2B-34). */
export const CONSOLE_SCENARIOS = ['idle', 'weekday', 'weekend_peak', 'fill_to_full', 'sensor_drift'] as const;
export type ConsoleScenario = (typeof CONSOLE_SCENARIOS)[number];

export const CAR_ID_PATTERN = /^car-\d{6}$/;

export const simulatorStatusSchema = z.strictObject({
  scenario: z.enum(['', ...SIMULATOR_SCENARIOS]),
  running: z.boolean(),
  seed: z.number(),
  speed: z.number(),
  virtual_now: z.string(),
  cars_inside: z.number(),
  started_at: z.string().optional(),
  reached_full: z.boolean(),
});

export const simulatorEventSchema = z.strictObject({
  seq: z.number(),
  at: z.string(),
  device: z.string(),
  type: z.string(),
  result: z.string(),
  reason: z.string().optional(),
  car_id: z.string().optional(),
});

export const injectCarResultSchema = z.strictObject({ car_id: z.string() });
/** Body of the `leave` 202 response. */
export const emptyDataSchema = z.strictObject({});

// Request schemas: issue messages are `rule` or `rule:param`.
export const startScenarioSchema = z.strictObject({
  scenario: z.enum(CONSOLE_SCENARIOS, { error: 'oneof' }),
  speed: z
    .number({ error: 'required' })
    .int({ error: 'integer' })
    .min(1, { error: 'min:1' })
    .max(120, { error: 'max:120' })
    .default(30),
  seed: z.number().int({ error: 'integer' }).min(0, { error: 'min:0' }).optional(),
  start_at: z.iso.datetime({ offset: true, error: 'pattern' }).optional(),
});

export const stopScenarioSchema = z.strictObject({ drain: z.boolean().default(false) });

export const injectCarSchema = z
  .strictObject({
    credential: z.enum(['ticket', 'employee_card'], { error: 'oneof' }),
    card: z.string().trim().max(32, { error: 'max:32' }).regex(/^EMP-\d{3}$/, { error: 'pattern' }).optional(),
    hold: z.boolean().default(false),
    dwell_minutes: z.number().int({ error: 'integer' }).min(1, { error: 'min:1' }).max(600, { error: 'max:600' }).optional(),
  })
  .superRefine((value, ctx) => {
    if (value.credential === 'employee_card' && !value.card) {
      ctx.addIssue({ code: 'custom', path: ['card'], message: 'required' });
    }
  })
  .transform((value): { credential: 'ticket' | 'employee_card'; card?: string; hold: boolean; dwell_minutes?: number } => {
    const { card, ...rest } = value;
    return value.credential === 'employee_card' && card ? { ...rest, card } : rest;
  });

export const leaveCarSchema = z.strictObject({ lose_ticket: z.boolean().default(false) });

export const sensorFaultSchema = z.strictObject({
  fault: z.enum(['stuck_occupied', 'stuck_free', 'silent', 'none'], { error: 'oneof' }),
});

export type SimulatorStatus = z.output<typeof simulatorStatusSchema>;
export type SimulatorEvent = z.output<typeof simulatorEventSchema>;
export type StartScenarioValues = z.output<typeof startScenarioSchema>;
export type InjectCarValues = z.output<typeof injectCarSchema>;
export type SensorFaultValues = z.output<typeof sensorFaultSchema>;
