import { z } from 'zod';

export const SPACE_CODE_PATTERN = /^E\d{3}$/;

export const spaceStateSchema = z.strictObject({
  code: z.string(),
  state: z.enum(['free', 'occupied', 'out_of_service']),
});

export const lotSpacesSchema = z.strictObject({ spaces: z.array(spaceStateSchema) });

/** Payload of the `spaces` SSE event: a full snapshot or a delta. */
export const spacesEventSchema = z.strictObject({
  full: z.boolean(),
  spaces: z.array(spaceStateSchema),
});

export type SpaceState = z.output<typeof spaceStateSchema>;
export type SpaceStatus = SpaceState['state'];
export type LotSpaces = z.output<typeof lotSpacesSchema>;
export type SpacesEvent = z.output<typeof spacesEventSchema>;
