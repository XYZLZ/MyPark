import { Stack } from 'expo-router';

// Welcome is the first screen even when a notice sends the user back here.
export const unstable_settings = { anchor: 'welcome' };

/** Signed-out flow: welcome, login, sign up, verify and the legal pages. */
export default function AuthLayout() {
  return <Stack screenOptions={{ headerShown: false }} />;
}
