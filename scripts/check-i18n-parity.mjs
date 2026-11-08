#!/usr/bin/env node
// Checks that every locale file in a directory has exactly the same keys.
//
//   node scripts/check-i18n-parity.mjs     (or: npm run check:i18n)
//
// Exits 1 on any difference. The mobile directory is optional until the
// mobile app exists. It also checks the error and validation catalog
// (checkCatalog): every error code and validation rule has a key in all four
// locale files, and errors.* / validation.* match between web and mobile.

import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { basename, dirname, join, relative } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..');

export const LOCALE_DIRS = [
  { dir: join(repoRoot, 'apps/web/src/i18n/messages'), optional: false },
  { dir: join(repoRoot, 'apps/mobile/src/lib/i18n/locales'), optional: true },
];

function isPlainObject(value) {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function flattenKeys(obj, prefix = '') {
  return Object.entries(obj).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    return isPlainObject(value) ? flattenKeys(value, path) : [path];
  });
}

/** Dotted leaf keys present in one object but not the other, sorted. */
export function diffKeys(a, b) {
  const keysA = new Set(flattenKeys(a));
  const keysB = new Set(flattenKeys(b));
  return {
    missingInA: [...keysB].filter((key) => !keysA.has(key)).sort(),
    missingInB: [...keysA].filter((key) => !keysB.has(key)).sort(),
  };
}

/** Returns a list of human-readable problems; empty means parity holds. */
export function checkParity(dirs = LOCALE_DIRS) {
  const problems = [];

  for (const { dir, optional } of dirs) {
    const label = relative(repoRoot, dir) || dir;
    if (!existsSync(dir)) {
      if (!optional) problems.push(`${label}: directory not found`);
      continue;
    }

    const files = readdirSync(dir).filter((file) => file.endsWith('.json')).sort();
    if (files.length < 2) {
      problems.push(`${label}: expected at least two locale files, found ${files.length}`);
      continue;
    }

    const [reference, ...others] = files;
    const referenceMessages = JSON.parse(readFileSync(join(dir, reference), 'utf8'));
    for (const file of others) {
      const messages = JSON.parse(readFileSync(join(dir, file), 'utf8'));
      const { missingInA, missingInB } = diffKeys(referenceMessages, messages);
      if (missingInB.length) problems.push(`${label}/${file} is missing: ${missingInB.join(', ')}`);
      if (missingInA.length) problems.push(`${label}/${reference} is missing: ${missingInA.join(', ')}`);
    }
  }

  return problems;
}

const CATALOG_DEFAULTS = {
  webDir: join(repoRoot, 'apps/web/src/i18n/messages'),
  mobileDir: join(repoRoot, 'apps/mobile/src/lib/i18n/locales'),
  errorCodesFile: join(repoRoot, 'packages/contracts/src/error-codes.ts'),
  validationFile: join(repoRoot, 'packages/contracts/src/m1/validation.ts'),
};

/** String literals of `export const NAME = [...]` (multi-line generated or single-line hand-written). */
export function readConstArray(file, name) {
  const source = readFileSync(file, 'utf8');
  const match = new RegExp(String.raw`export const ${name}\s*=\s*\[([^\]]*)\]`).exec(source);
  if (!match) throw new Error(`${relative(repoRoot, file) || file}: no 'export const ${name} = [...]' found`);
  return [...match[1].matchAll(/'([^']+)'|"([^"]+)"/g)].map((m) => m[1] ?? m[2]);
}

function readLocales(dir) {
  const out = {};
  for (const file of readdirSync(dir).filter((f) => f.endsWith('.json')).sort()) {
    out[file] = JSON.parse(readFileSync(join(dir, file), 'utf8'));
  }
  return out;
}

function scopedKeys(messages, root) {
  return new Set(flattenKeys(messages).filter((key) => key === root || key.startsWith(`${root}.`)));
}

/** Problems with the errors.* and validation.* catalog; empty means it is complete and aligned. */
export function checkCatalog(options = CATALOG_DEFAULTS) {
  const { webDir, mobileDir, errorCodesFile, validationFile } = { ...CATALOG_DEFAULTS, ...options };
  const problems = [];
  const required = [
    'errors.fallback',
    ...[...readConstArray(errorCodesFile, 'ERROR_CODES'), ...readConstArray(validationFile, 'CLIENT_ERROR_CODES')].map(
      (code) => `errors.${code}`,
    ),
    ...readConstArray(validationFile, 'VALIDATION_RULES').map((rule) => `validation.${rule}`),
  ];

  const apps = { web: readLocales(webDir), mobile: readLocales(mobileDir) };
  for (const [app, locales] of Object.entries(apps)) {
    for (const [file, messages] of Object.entries(locales)) {
      const present = new Set(flattenKeys(messages));
      const missing = required.filter((key) => !present.has(key));
      if (missing.length) problems.push(`${app}/${file} is missing: ${missing.join(', ')}`);
    }
  }

  for (const [file, webMessages] of Object.entries(apps.web)) {
    const mobileMessages = apps.mobile[file];
    if (!mobileMessages) continue;
    for (const root of ['errors', 'validation']) {
      const web = scopedKeys(webMessages, root);
      const mobile = scopedKeys(mobileMessages, root);
      const webOnly = [...web].filter((key) => !mobile.has(key)).sort();
      const mobileOnly = [...mobile].filter((key) => !web.has(key)).sort();
      if (webOnly.length) problems.push(`${file} ${root}.* only on web: ${webOnly.join(', ')}`);
      if (mobileOnly.length) problems.push(`${file} ${root}.* only on mobile: ${mobileOnly.join(', ')}`);
    }
  }

  return problems;
}

const isCli = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href;
if (isCli) {
  const problems = [...checkParity(), ...checkCatalog()];
  if (problems.length) {
    console.error(`i18n check failed:\n${problems.map((p) => `  - ${p}`).join('\n')}`);
    process.exit(1);
  }
  console.log(`${basename(fileURLToPath(import.meta.url))}: locale keys match.`);
}
