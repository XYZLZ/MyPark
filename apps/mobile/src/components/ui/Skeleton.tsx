import { View } from 'react-native';

/** A static placeholder shape (no shimmer, so there is nothing to disable for reduce motion). */
export function Skeleton({ className }: { className?: string }) {
  return (
    <View
      accessible={false}
      importantForAccessibility="no-hide-descendants"
      className={`rounded-control bg-hairline${className ? ` ${className}` : ' h-6 w-full'}`}
    />
  );
}
