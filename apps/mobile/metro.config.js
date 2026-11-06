// Expo SDK 52+ detects npm workspaces automatically, so the default config
// already watches the repo root and resolves @mypark/contracts from it.
const { getDefaultConfig } = require('expo/metro-config');
const { withUniwindConfig } = require('uniwind/metro');

/** @type {import('expo/metro-config').MetroConfig} */
const config = getDefaultConfig(__dirname);

// withUniwindConfig must be the outermost wrapper.
module.exports = withUniwindConfig(config, {
  cssEntryFile: './src/global.css',
  dtsFile: './src/types/uniwind-types.d.ts',
});
