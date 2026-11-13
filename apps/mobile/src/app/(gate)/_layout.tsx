import { Stack } from 'expo-router';

/** Signed in, but the legal texts changed: the gate is the only screen. */
export default function GateLayout() {
  return <Stack screenOptions={{ headerShown: false }} />;
}
