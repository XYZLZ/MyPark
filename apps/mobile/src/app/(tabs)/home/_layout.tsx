import { Stack } from 'expo-router';

/** The Inicio tab: the live lot. Its screen draws its own header. */
export default function HomeLayout() {
  return <Stack screenOptions={{ headerShown: false }} />;
}
