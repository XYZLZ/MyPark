import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { findLayoutViolations, LAYOUT_ALLOW, readRealFiles } from './check-mobile-layout.mjs';

const file = (text, path = 'src/components/Thing.tsx') => [{ path, text }];
const count = (text, path) => findLayoutViolations(file(text, path)).length;
const SCREEN = 'src/features/x/FooScreen.tsx';

test('flags a horizontal ScrollView', () => {
  assert.equal(count('<ScrollView horizontal>'), 1);
  assert.equal(count('<FlatList horizontal={true} data={[]} />'), 1);
  assert.equal(count('<ScrollView horizontal={false}>'), 0);
  assert.equal(count('<ScrollView keyboardShouldPersistTaps="handled">'), 0);
});

test('flags horizontal after an arrow function or JSX inside another prop', () => {
  assert.equal(count('<FlatList renderItem={() => <Row />} horizontal />'), 1);
  assert.equal(count('<FlatList renderItem={({ item }) => <Row a={{ b: 1 }} />} horizontal={true} data={[]} />'), 1);
  assert.equal(count('<FlatList renderItem={() => <Row />} horizontal={false} />'), 0);
  assert.equal(count('<FlatList renderItem={() => <Row />} data={[]} />'), 0);
});

test('flags a scroll component with a generic type argument', () => {
  assert.equal(count('<FlatList<Item> horizontal data={[]} />'), 1);
  assert.equal(count('<FlatList<Item> data={[]} horizontal={true} />'), 1);
  assert.equal(count('<FlatList<Item> data={[]} />'), 0);
  assert.equal(count('<FlatList<Item> {...props} />'), 1);
});

test('the allow-list is empty: no screen is exempt any more (PF12)', () => {
  assert.deepEqual(LAYOUT_ALLOW, []);
});

test('flags the Animated variants and spread props', () => {
  assert.equal(count('<Animated.FlatList horizontal data={[]} />'), 1);
  assert.equal(count('<Animated.SectionList horizontal={true} />'), 1);
  assert.equal(count('<Animated.ScrollView horizontal>'), 1);
  assert.equal(count('<ScrollView {...props} />'), 1);
});

test('flags aliased scroll components', () => {
  assert.equal(count("import { FlatList as List } from 'react-native';\nconst x = <List horizontal />;"), 1);
});

test('flags fixed widths above 328', () => {
  assert.equal(count('style={{ width: 340 }}'), 1);
  assert.equal(count('const s = { minWidth: 400, maxWidth: 500 };'), 2);
  assert.equal(count('className="w-[340px]"'), 1);
  assert.equal(count('className="min-w-[400px] flex-1"'), 1);
  assert.equal(count('className="w-84"'), 1);
  assert.equal(count('className="min-w-84"'), 1);
  assert.equal(count('className="w-[21rem]"'), 1);
  assert.equal(count('className="min-w-[25rem]"'), 1);
  assert.equal(count('className="w-80 w-[328px] w-[20rem] min-w-80 w-full"'), 0);
  assert.equal(count('style={{ width: 328 }}'), 0);
  assert.equal(count('style={{ width: "100%" }}'), 0);
});

test('flags a screen that bypasses ScreenScroll', () => {
  assert.equal(count('export function FooScreen() { return null; }', SCREEN), 1);
  const ok = "import { ScreenScroll } from '@/components/ui/ScreenScroll';\nexport const A = () => <ScreenScroll>x</ScreenScroll>;";
  assert.equal(count(ok, SCREEN), 0);
});

test('an import alone, a comment or a string does not satisfy the ScreenScroll rule', () => {
  const imp = "import { ScreenScroll } from '@/components/ui/ScreenScroll';\nexport const A = () => null;";
  assert.equal(count(imp, SCREEN), 1);
  assert.equal(count('// uses <ScreenScroll>\nexport const A = () => null;', SCREEN), 1);
  assert.equal(count('/* <ScreenScroll> */ export const A = () => null;', SCREEN), 1);
  const noImport = 'export const A = () => <ScreenScroll>x</ScreenScroll>;';
  assert.equal(count(noImport, SCREEN), 1);
});

test('the allow-list exempts a file', () => {
  const text = '<ScrollView horizontal>';
  assert.equal(findLayoutViolations(file(text, SCREEN)).length, 2);
  assert.deepEqual(findLayoutViolations(file(text, SCREEN), [SCREEN]), []);
});

test('every allow-list entry exists on disk', () => {
  const mobile = join(fileURLToPath(import.meta.url), '..', '..', 'apps', 'mobile');
  for (const entry of LAYOUT_ALLOW) assert.ok(existsSync(join(mobile, entry)), `${entry} no longer exists: remove it from LAYOUT_ALLOW`);
});

test('passes on the real repo', () => {
  assert.deepEqual(findLayoutViolations(readRealFiles()), []);
});
