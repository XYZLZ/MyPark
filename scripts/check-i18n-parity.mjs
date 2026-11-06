#!/usr/bin/env node
// Checks that every locale file in a directory has exactly the same keys.
//
//   node scripts/check-i18n-parity.mjs     (or: npm run check:i18n)
//
// Exits 1 on any difference. The mobile directory is optional until the
// mobile app exists.

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

const isCli = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href;
if (isCli) {
  const problems = checkParity();
  if (problems.length) {
    console.error(`i18n key parity failed:\n${problems.map((p) => `  - ${p}`).join('\n')}`);
    process.exit(1);
  }
  console.log(`${basename(fileURLToPath(import.meta.url))}: locale keys match.`);
}
