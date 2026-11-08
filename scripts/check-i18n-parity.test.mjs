import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { after, test } from 'node:test';
import { checkCatalog, checkParity, diffKeys, readConstArray } from './check-i18n-parity.mjs';

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

// --- catalog checks -------------------------------------------------------

const CODES_FILE = ['export const ERROR_CODES = [', '  "vehicle.in_use",', '  "vehicle.plate_taken",', '] as const;', ''].join('\n');
const RULES_FILE = [
  "export const VALIDATION_RULES = ['required', 'code6'] as const;",
  "export const CLIENT_ERROR_CODES = ['client.network'] as const;",
  '',
].join('\n');

function catalog() {
  return {
    errors: { fallback: 'x', vehicle: { in_use: 'x', plate_taken: 'x' }, client: { network: 'x' } },
    validation: { required: 'x', code6: 'x' },
  };
}

function writeCatalogFixture(name, overrides = {}) {
  const base = join(root, name);
  const webDir = join(base, 'web');
  const mobileDir = join(base, 'mobile');
  mkdirSync(webDir, { recursive: true });
  mkdirSync(mobileDir, { recursive: true });
  const files = { web: catalog(), mobile: catalog(), ...overrides };
  for (const locale of ['es-DO', 'en']) {
    writeFileSync(join(webDir, `${locale}.json`), JSON.stringify(files.web));
    writeFileSync(join(mobileDir, `${locale}.json`), JSON.stringify(files.mobile));
  }
  const errorCodesFile = join(base, 'error-codes.ts');
  const validationFile = join(base, 'validation.ts');
  writeFileSync(errorCodesFile, CODES_FILE);
  writeFileSync(validationFile, RULES_FILE);
  return { webDir, mobileDir, errorCodesFile, validationFile };
}

test('readConstArray reads a generated multi-line list', () => {
  const { errorCodesFile } = writeCatalogFixture('multi');
  assert.deepEqual(readConstArray(errorCodesFile, 'ERROR_CODES'), ['vehicle.in_use', 'vehicle.plate_taken']);
});

test('readConstArray reads a hand-written single-line list', () => {
  const { validationFile } = writeCatalogFixture('single-line');
  assert.deepEqual(readConstArray(validationFile, 'VALIDATION_RULES'), ['required', 'code6']);
  assert.deepEqual(readConstArray(validationFile, 'CLIENT_ERROR_CODES'), ['client.network']);
});

test('readConstArray throws when the list is absent', () => {
  const { validationFile } = writeCatalogFixture('absent-list');
  assert.throws(() => readConstArray(validationFile, 'NOPE'), /NOPE/);
});

test('checkCatalog passes on a complete fixture', () => assert.deepEqual(checkCatalog(writeCatalogFixture('complete')), []));

test('checkCatalog reports an ERROR_CODE missing from a locale file', () => {
  const mobile = catalog();
  delete mobile.errors.vehicle.in_use;
  const problems = checkCatalog(writeCatalogFixture('missing-code', { mobile }));
  assert.ok(problems.some((p) => /mobile/.test(p) && /errors\.vehicle\.in_use/.test(p)), problems.join('\n'));
});

test('checkCatalog reports a client code missing from a locale file', () => {
  const web = catalog();
  delete web.errors.client;
  const problems = checkCatalog(writeCatalogFixture('missing-client', { web }));
  assert.ok(problems.some((p) => /web/.test(p) && /errors\.client\.network/.test(p)), problems.join('\n'));
});

test('checkCatalog reports a validation rule with no key', () => {
  const web = catalog();
  delete web.validation.code6;
  const problems = checkCatalog(writeCatalogFixture('missing-rule', { web }));
  assert.ok(problems.some((p) => /validation\.code6/.test(p)), problems.join('\n'));
});

test('checkCatalog reports errors.* drift between web and mobile', () => {
  const web = catalog();
  web.errors.extra = 'x';
  const problems = checkCatalog(writeCatalogFixture('drift-catalog', { web }));
  assert.ok(problems.some((p) => /errors\.extra/.test(p)), problems.join('\n'));
});

test('checkCatalog passes on the real repo', () => assert.deepEqual(checkCatalog(), []));
