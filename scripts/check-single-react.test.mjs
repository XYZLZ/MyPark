import assert from 'node:assert/strict';
import { test } from 'node:test';
import { checkSingleReact, collectVersions } from './check-single-react.mjs';

test('collectVersions finds nested duplicates', () => {
  const tree = {
    dependencies: {
      react: { version: '19.3.0' },
      'apps-mobile': { dependencies: { react: { version: '19.2.3' } } },
    },
  };
  assert.deepEqual(collectVersions(tree, 'react'), ['19.2.3', '19.3.0']);
});

test('collectVersions returns empty when absent', () => assert.deepEqual(collectVersions({}, 'react'), []));

test('collectVersions counts deduped and overridden entries as their version', () => {
  // npm 11 `npm ls --all --json` shape: the hoisted copy carries resolved and
  // overridden; deduped references repeat only the version.
  const tree = {
    name: 'mypark',
    dependencies: {
      '@mypark/web': {
        version: '0.0.0',
        overridden: false,
        dependencies: {
          react: { version: '19.3.0', resolved: 'https://registry.npmjs.org/react/-/react-19.3.0.tgz', overridden: true },
          'react-dom': { version: '19.3.0', overridden: true, dependencies: { react: { version: '19.3.0' } } },
        },
      },
      '@mypark/mobile': {
        version: '0.0.0',
        dependencies: {
          'expo-font': { version: '58.0.11', dependencies: { react: { version: '19.3.0' } } },
          // An unmet optional peer has no version and is not a copy of react.
          'some-lib': { version: '1.0.0', dependencies: { react: { required: '^18', missing: true } } },
        },
      },
    },
  };
  assert.deepEqual(collectVersions(tree, 'react'), ['19.3.0']);
  assert.deepEqual(collectVersions(tree, 'react-dom'), ['19.3.0']);
});

test('checkSingleReact reports mismatches and missing packages', () => {
  const ok = { dependencies: { react: { version: '19.3.0' }, 'react-dom': { version: '19.3.0' } } };
  assert.deepEqual(checkSingleReact(ok), []);

  const bad = { dependencies: { react: { version: '19.3.0' }, x: { dependencies: { react: { version: '19.2.3' } } } } };
  const problems = checkSingleReact(bad);
  assert.equal(problems.length, 2);
  assert.match(problems[0], /react: expected only 19\.3\.0, found 19\.2\.3, 19\.3\.0/);
  assert.match(problems[1], /react-dom: not found/);
});
