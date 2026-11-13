/** @type {import('jest').Config} */
module.exports = {
  preset: 'jest-expo',
  // Transpile RN/Expo packages and the workspace contracts package (shipped as TS source).
  transformIgnorePatterns: [
    'node_modules/(?!((jest-)?react-native|@react-native(-community)?|expo(nent)?|@expo(nent)?/.*|@expo-google-fonts/.*|react-navigation|@react-navigation/.*|@mypark/.*|uniwind))',
  ],
  // Loads the RN primitives once per test file, outside any test's 5 s budget (see the file).
  setupFilesAfterEnv: ['<rootDir>/src/testing/warm-up.tsx'],
  // Test files live next to their sources; route files under src/app must stay route-only.
  testMatch: ['<rootDir>/src/**/*.test.{ts,tsx}'],
  testPathIgnorePatterns: ['/node_modules/', '<rootDir>/src/app/'],
};
