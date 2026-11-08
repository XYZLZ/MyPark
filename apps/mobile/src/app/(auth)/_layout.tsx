import { Stack } from 'expo-router';

/** Signed-out flow: welcome, login, sign up, verify and the legal pages (Task 10). */
export default function AuthLayout() {
  return <Stack screenOptions={{ headerShown: false }} />;
}
