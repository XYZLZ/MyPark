import { expectTypeOf, test } from 'vitest';
import type { z } from 'zod';
import type { components } from './openapi';
import {
  availabilitySchema,
  heartbeatSchema,
  injectCarResultSchema,
  injectCarSchema,
  leaveCarSchema,
  lotSchema,
  lotSpacesSchema,
  lotSummarySchema,
  sensorFaultSchema,
  simulatorEventSchema,
  simulatorStatusSchema,
  spaceStateSchema,
  spacesEventSchema,
  startScenarioSchema,
  stopScenarioSchema,
  tariffSchema,
} from './index';

type Schemas = components['schemas'];

test('M2 response schemas match the OpenAPI types', () => {
  expectTypeOf<z.output<typeof lotSummarySchema>>().toEqualTypeOf<Schemas['LotSummary']>();
  expectTypeOf<z.output<typeof lotSchema>>().toEqualTypeOf<Schemas['Lot']>();
  expectTypeOf<z.output<typeof availabilitySchema>>().toEqualTypeOf<Schemas['Availability']>();
  expectTypeOf<z.output<typeof spaceStateSchema>>().toEqualTypeOf<Schemas['SpaceState']>();
  expectTypeOf<z.output<typeof lotSpacesSchema>>().toEqualTypeOf<Schemas['LotSpaces']>();
  expectTypeOf<z.output<typeof tariffSchema>>().toEqualTypeOf<Schemas['Tariff']>();
  expectTypeOf<z.output<typeof spacesEventSchema>>().toEqualTypeOf<Schemas['SpacesEvent']>();
  expectTypeOf<z.output<typeof heartbeatSchema>>().toEqualTypeOf<Schemas['HeartbeatEvent']>();
  expectTypeOf<z.output<typeof simulatorStatusSchema>>().toEqualTypeOf<Schemas['SimulatorStatus']>();
  expectTypeOf<z.output<typeof simulatorEventSchema>>().toEqualTypeOf<Schemas['SimulatorEvent']>();
  expectTypeOf<z.output<typeof injectCarResultSchema>>().toEqualTypeOf<Schemas['InjectCarResponse']>();
  expectTypeOf<z.output<typeof stopScenarioSchema>>().toEqualTypeOf<Schemas['StopScenarioRequest']>();
  expectTypeOf<z.output<typeof leaveCarSchema>>().toEqualTypeOf<Schemas['LeaveCarRequest']>();
  expectTypeOf<z.output<typeof sensorFaultSchema>>().toEqualTypeOf<Schemas['SensorFaultRequest']>();
});

test('M2 request schemas produce valid OpenAPI request bodies', () => {
  expectTypeOf<z.output<typeof startScenarioSchema>>().toExtend<Schemas['StartScenarioRequest']>();
  expectTypeOf<z.output<typeof injectCarSchema>>().toExtend<Schemas['InjectCarRequest']>();
});
