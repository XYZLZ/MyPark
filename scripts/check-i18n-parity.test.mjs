import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { after, test } from 'node:test';
import { checkParity, diffKeys } from './check-i18n-parity.mjs';

const root = mkdtempSync(join(tmpdir(), 'i18n-parity-'));
after(() => rmSync(root, { recursive: true, force: true }));

function writeLocales(name, files) {
  const dir = join(root, name);
  mkdirSync(dir, { recursive: true });
  for (const [file, content] of Object.entries(files)) {
    writeFileSync(join(dir, file), JSON.stringify(content));
  }
  return dir;
}

test('diffKeys reports nested missing keys', () =>
  assert.deepEqual(diffKeys({ a: { b: 1, c: 1 } }, { a: { b: 1 } }), { missingInA: [], missingInB: ['a.c'] }));

test('diffKeys reports keys missing on both sides, sorted', () =>
  assert.deepEqual(diffKeys({ x: 1, a: { b: 1 } }, { a: { c: 1, b: 1 }, y: { z: 1 } }), {
    missingInA: ['a.c', 'y.z'],
    missingInB: ['x'],
  }));

test('diffKeys treats a leaf and a nested object under the same key as different keys', () =>
  assert.deepEqual(diffKeys({ a: 'text' }, { a: { b: 'text' } }), { missingInA: ['a.b'], missingInB: ['a'] }));

test('checkParity passes when every locale file has the same keys', () => {
  const dir = writeLocales('ok', { 'es-DO.json': { a: { b: 'x' } }, 'en.json': { a: { b: 'y' } } });
  assert.deepEqual(checkParity([{ dir, optional: false }]), []);
});

test('checkParity reports a key missing from one locale', () => {
  const dir = writeLocales('drift', { 'es-DO.json': { a: 'x', b: 'y' }, 'en.json': { a: 'x' } });
  const problems = checkParity([{ dir, optional: false }]);
  assert.equal(problems.length, 1);
  assert.match(problems[0], /en\.json is missing: b/);
});

test('checkParity skips an optional directory that does not exist', () =>
  assert.deepEqual(checkParity([{ dir: join(root, 'absent'), optional: true }]), []));

test('checkParity fails on a required directory that does not exist', () => {
  const problems = checkParity([{ dir: join(root, 'absent'), optional: false }]);
  assert.equal(problems.length, 1);
  assert.match(problems[0], /not found/);
});

test('checkParity fails when a directory has fewer than two locale files', () => {
  const dir = writeLocales('single', { 'es-DO.json': { a: 'x' } });
  assert.match(checkParity([{ dir, optional: false }])[0], /at least two/);
});
