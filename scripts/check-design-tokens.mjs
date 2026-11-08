#!/usr/bin/env node
// Checks that the design tokens match in web and mobile and that the
// documented contrast pairs still hold (spec §9.1 and §9.2).
//
//   node scripts/check-design-tokens.mjs     (or: npm run check:tokens)
//
// Exits 1 on any problem.

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..');

const PATHS = {
  webCss: 'apps/web/src/design/tokens.css',
  mobileCss: 'apps/mobile/src/global.css',
  mobileTs: 'apps/mobile/src/design/tokens.ts',
  appJson: 'apps/mobile/app.json',
};

const LABELS = {
  webCss: 'web tokens.css',
  mobileCss: 'mobile global.css',
  mobileTs: 'mobile tokens.ts',
};

/** Every token in spec §9.1, as CSS names without the `--color-` prefix. */
export const REQUIRED_TOKENS = [
  'brand-primary',
  'brand-primary-strong',
  'brand-primary-tint',
  'brand-primary-contrast',
  'brand-accent',
  'brand-accent-contrast',
  'brand-action',
  'brand-action-hover',
  'brand-action-press',
  'brand-action-contrast',
  'ink',
  'ink-muted',
  'surface',
  'surface-sunken',
  'hairline',
  'control-line',
  'danger',
  'danger-tint',
  'disabled-bg',
  'disabled-ink',
  'radius-control',
  'radius-panel',
  'radius-plate',
  'motion-duration',
  'motion-ease',
];

// [foreground, background, minimum ratio] from spec §9.2. 4.5 is normal text;
// 3 is large text or UI components. The disabled pair is exempt (WCAG).
export const MUST_PASS = [
  ['brand-action-contrast', 'brand-action', 4.5],
  ['brand-action-contrast', 'brand-action-hover', 4.5],
  ['brand-action-contrast', 'brand-action-press', 4.5],
  ['brand-primary-contrast', 'brand-primary', 4.5],
  ['brand-primary-contrast', 'brand-primary-strong', 4.5],
  ['brand-accent', 'brand-primary-strong', 4.5],
  ['brand-accent', 'brand-primary', 3],
  ['brand-accent-contrast', 'brand-accent', 4.5],
  ['ink', 'surface', 4.5],
  ['ink', 'surface-sunken', 4.5],
  ['ink-muted', 'surface', 4.5],
  ['ink-muted', 'surface-sunken', 4.5],
  ['control-line', 'surface', 3],
  ['control-line', 'surface-sunken', 3],
  ['danger', 'surface', 4.5],
  ['danger', 'danger-tint', 4.5],
  ['danger', 'surface-sunken', 4.5],
  ['brand-primary', 'surface', 4.5],
  ['brand-primary', 'surface-sunken', 4.5],
  ['brand-primary', 'brand-primary-tint', 4.5],
];

// Pairs the spec documents as failing. They must keep failing as text, which
// is why the action color carries dark text and gold never sits on white.
export const MUST_FAIL = [
  ['brand-primary-contrast', 'brand-action', 4.5],
  ['brand-accent', 'surface', 4.5],
];

function stripComments(text) {
  return text.replace(/\/\*[\s\S]*?\*\//g, '');
}

/** `--color-x`, `--radius-x` and `--motion-x` declarations, keyed by token name. */
export function parseCssTokens(text) {
  const tokens = new Map();
  const declaration = /--(color-|radius-|motion-)([\w-]+)\s*:\s*([^;]+);/g;
  for (const [, prefix, name, value] of stripComments(text).matchAll(declaration)) {
    const key = prefix === 'color-' ? name : `${prefix}${name}`;
    tokens.set(key, value.trim().toLowerCase());
  }
  return tokens;
}

function kebab(name) {
  return name.replace(/[A-Z]/g, (letter) => `-${letter.toLowerCase()}`);
}

/** One `key: 'value'` per line, camelCase keys mapped to kebab token names. */
export function parseTsTokens(text) {
  const tokens = new Map();
  const entry = /^\s*(\w+)\s*:\s*'([^']*)'\s*,?\s*$/gm;
  for (const [, key, value] of stripComments(text).matchAll(entry)) {
    tokens.set(kebab(key), value.trim().toLowerCase());
  }
  return tokens;
}

function channels(hex) {
  let digits = hex.trim().replace(/^#/, '');
  if (digits.length === 3) digits = [...digits].map((d) => d + d).join('');
  if (!/^[0-9a-f]{6}$/i.test(digits)) throw new Error(`Not a hex color: ${hex}`);
  return [0, 2, 4].map((i) => parseInt(digits.slice(i, i + 2), 16) / 255);
}

function luminance(hex) {
  const [r, g, b] = channels(hex).map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** WCAG 2.x contrast ratio between two hex colors. */
export function contrastRatio(a, b) {
  const [light, dark] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (light + 0.05) / (dark + 0.05);
}

export function readRealFiles() {
  return Object.fromEntries(
    Object.entries(PATHS).map(([key, path]) => [key, readFileSync(join(repoRoot, path), 'utf8')]),
  );
}

/** Returns a list of problems; empty means the tokens agree and the contrast holds. */
export function checkTokens(files = readRealFiles()) {
  const problems = [];
  const web = parseCssTokens(files.webCss);
  const sources = {
    mobileCss: parseCssTokens(files.mobileCss),
    mobileTs: parseTsTokens(files.mobileTs),
  };

  for (const name of REQUIRED_TOKENS) {
    const expected = web.get(name);
    if (expected === undefined) {
      problems.push(`${LABELS.webCss} is missing token "${name}"`);
      continue;
    }
    for (const [key, tokens] of Object.entries(sources)) {
      const actual = tokens.get(name);
      if (actual === undefined) problems.push(`${LABELS[key]} is missing token "${name}"`);
      else if (actual !== expected) {
        problems.push(`${LABELS[key]} has ${name}=${actual}, but ${LABELS.webCss} has ${expected}`);
      }
    }
  }

  for (const [key, tokens] of Object.entries(sources)) {
    for (const name of tokens.keys()) {
      if (!REQUIRED_TOKENS.includes(name)) problems.push(`${LABELS[key]} has unknown token "${name}"`);
    }
  }

  if (/#0b3a7e/i.test(files.appJson)) {
    problems.push('apps/mobile/app.json still contains the old brand color #0b3a7e');
  }

  const ratioOf = (fg, bg) => {
    const [fgValue, bgValue] = [web.get(fg), web.get(bg)];
    if (!fgValue || !bgValue) return null;
    return contrastRatio(fgValue, bgValue);
  };

  for (const [fg, bg, min] of MUST_PASS) {
    const ratio = ratioOf(fg, bg);
    if (ratio !== null && ratio < min) {
      problems.push(`${fg} on ${bg} is ${ratio.toFixed(2)}:1, below the required ${min}:1`);
    }
  }
  for (const [fg, bg, max] of MUST_FAIL) {
    const ratio = ratioOf(fg, bg);
    if (ratio !== null && ratio >= max) {
      problems.push(`${fg} on ${bg} is ${ratio.toFixed(2)}:1, but the spec documents it as failing text contrast`);
    }
  }

  return problems;
}

const isCli = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href;
if (isCli) {
  const problems = checkTokens();
  if (problems.length) {
    console.error(`design token check failed:\n${problems.map((p) => `  - ${p}`).join('\n')}`);
    process.exit(1);
  }
  console.log(`check-design-tokens.mjs: ${REQUIRED_TOKENS.length} tokens match and contrast pairs hold.`);
}
