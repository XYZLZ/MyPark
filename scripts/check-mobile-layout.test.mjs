import assert from 'node:assert/strict';
import { test } from 'node:test';
import { findLayoutViolations, LAYOUT_ALLOW, readRealFiles } from './check-mobile-layout.mjs';

const file = (text, path = 'src/components/Thing.tsx') => [{ path, text }];

test('flags a horizontal ScrollView', () => {
  assert.equal(findLayoutViolations(file('<ScrollView horizontal>')).length, 1);
  assert.equal(findLayoutViolations(file('<FlatList horizontal={true} data={[]} />')).length, 1);
  assert.equal(findLayoutViolations(file('<ScrollView horizontal={false}>')).length, 0);
  assert.equal(findLayoutViolations(file('<ScrollView keyboardShouldPersistTaps="handled">')).length, 0);
});

test('flags fixed widths above 328', () => {
  assert.equal(findLayoutViolations(file('style={{ width: 340 }}')).length, 1);
  assert.equal(findLayoutViolations(file('const s = { minWidth: 400, maxWidth: 500 };')).length, 2);
  assert.equal(findLayoutViolations(file('className="w-[340px]"')).length, 1);
  assert.equal(findLayoutViolations(file('className="min-w-[400px] flex-1"')).length, 1);
  assert.equal(findLayoutViolations(file('className="w-84"')).length, 1);
  assert.equal(findLayoutViolations(file('className="w-80 w-[328px] w-full"')).length, 0);
  assert.equal(findLayoutViolations(file('style={{ width: 328 }}')).length, 0);
  assert.equal(findLayoutViolations(file('style={{ width: "100%" }}')).length, 0);
});

test('flags a screen that bypasses ScreenScroll', () => {
  const path = 'src/features/x/FooScreen.tsx';
  assert.equal(findLayoutViolations(file('export function FooScreen() { return null; }', path)).length, 1);
  const ok = "import { ScreenScroll } from '@/components/ui/ScreenScroll';";
  assert.equal(findLayoutViolations(file(ok, path)).length, 0);
});

test('the allow-list exempts a file', () => {
  const path = 'src/features/x/FooScreen.tsx';
  const text = '<ScrollView horizontal>';
  assert.equal(findLayoutViolations(file(text, path)).length, 2);
  assert.deepEqual(findLayoutViolations(file(text, path), [path]), []);
});

test('passes on the real repo', () => {
  assert.ok(LAYOUT_ALLOW.length > 0);
  assert.deepEqual(findLayoutViolations(readRealFiles()), []);
});
