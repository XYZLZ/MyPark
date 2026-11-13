import { cleanup, render } from '@testing-library/react-native';
import { ActivityIndicator, Animated, Pressable, ScrollView, Text, TextInput, View } from 'react-native';

/**
 * Pays the one-time module-load cost of the React Native primitives before any test starts.
 * `react-native` exposes its components through lazy getters, so the first render in each test file used to load
 * and transform them inside the first test: ~2.3 s with a cold transform cache (CI), ~0.4 s warm, against Jest's
 * 5 s test budget. A beforeAll hook has its own budget, which keeps every test timeout at the default.
 */
beforeAll(async () => {
  await render(
    <ScrollView>
      <Animated.View>
        <View>
          <Pressable>
            <Text>warm-up</Text>
          </Pressable>
          <TextInput />
          <ActivityIndicator />
        </View>
      </Animated.View>
    </ScrollView>,
  );
  cleanup();
}, 30_000);
