import type { SpaceStatus } from '@mypark/contracts';
import { View } from 'react-native';

/** Cells are 2:3, like a bay seen from above. */
export const cellHeight = (size: number) => Math.round(size * 1.5);

/**
 * The shape of one state, drawn with Views (no icon package) so the state never rests on color alone: free is an empty
 * green outline, occupied a car in the occupied token, out of service a hatch with a cross (design §9.3).
 */
export function SpaceShape({ state, size, testID }: { state: SpaceStatus; size: number; testID?: string }) {
  const frame = { width: size, height: cellHeight(size) };
  if (state === 'occupied') {
    return (
      <View testID={testID} style={frame} className="rounded-[3px] bg-lot-concrete">
        <View
          style={{ position: 'absolute', left: '13%', right: '13%', top: '9%', bottom: '9%' }}
          className="rounded-[3px] bg-space-occupied"
        />
        <View
          style={{ position: 'absolute', left: '28%', right: '28%', top: '22%', height: '16%' }}
          className="rounded-[1px] bg-surface/80"
        />
      </View>
    );
  }
  if (state === 'out_of_service') {
    const bar = { position: 'absolute', width: '80%', height: 2, top: '50%', left: '10%' } as const;
    return (
      <View testID={testID} style={frame} className="overflow-hidden rounded-[3px] border border-ink-muted bg-surface-sunken">
        <View style={{ ...bar, transform: [{ rotate: '56deg' }] }} className="bg-ink" />
        <View style={{ ...bar, transform: [{ rotate: '-56deg' }] }} className="bg-ink" />
      </View>
    );
  }
  return <View testID={testID} style={frame} className="rounded-[3px] border-2 border-status-open bg-surface" />;
}

/** One bay of the lot. Not interactive; the grid around it is hidden from assistive tech. */
export function SpaceCell({ code, state, size }: { code: string; state: SpaceStatus; size: number }) {
  return (
    <View testID={`space-${code}`} style={{ width: size, height: cellHeight(size) }}>
      <SpaceShape state={state} size={size} testID={SHAPE_ID[state]} />
    </View>
  );
}

const SHAPE_ID = { free: 'shape-free', occupied: 'shape-car', out_of_service: 'shape-hatch' } as const;
