// Guards the mobile layout rules of M2B-52: no horizontal scrolling, no fixed width wider than
// the narrowest window minus both gutters, and every feature screen scrolls through ScreenScroll.
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join, relative, sep } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const SRC = join(root, 'apps', 'mobile', 'src');

/** 360 dp window minus two 16 dp gutters; mirrors MAX_FIXED_WIDTH in apps/mobile/src/design/layout.ts. */
const MAX_FIXED_WIDTH = 328;

/** Files exempt from every rule. Each entry needs a reason and an owner for removing it. */
export const LAYOUT_ALLOW = [
  // M1 Welcome uses a raw ScrollView. Task 11 rewrites it onto ScreenScroll and removes this entry (PF12).
  'src/features/auth/WelcomeScreen.tsx',
];

const HORIZONTAL = /\bhorizontal\b(?!\s*=\s*\{\s*false\s*\})(?!\s*=\s*["']false["'])/;
const STYLE_WIDTH = /\b(width|minWidth|maxWidth)\s*:\s*(\d+(?:\.\d+)?)\b/g;
const ARBITRARY_WIDTH = /(?<![\w-])(?:min-w|w)-\[(\d+(?:\.\d+)?)px\]/g;
const SCALE_WIDTH = /(?<![\w[-])w-(\d+(?:\.\d+)?)(?![\w%\]/-])/g;

function lineOf(text, index) {
  return text.slice(0, index).split('\n').length;
}

function scan(path, text) {
  const found = [];
  const add = (index, message) => found.push(`${path}:${lineOf(text, index)} ${message}`);

  // A `horizontal` prop on a scroll view or list: the opening tag decides, not the word in a comment.
  for (const tag of text.matchAll(/<(?:ScrollView|FlatList|SectionList|FlashList|Animated\.ScrollView)\b[^>]*>/gs)) {
    if (HORIZONTAL.test(tag[0])) add(tag.index, 'horizontal scroll view or list (nothing may scroll sideways)');
  }
  for (const m of text.matchAll(STYLE_WIDTH)) {
    if (Number(m[2]) > MAX_FIXED_WIDTH) add(m.index, `${m[1]}: ${m[2]} is wider than ${MAX_FIXED_WIDTH}`);
  }
  for (const m of text.matchAll(ARBITRARY_WIDTH)) {
    if (Number(m[1]) > MAX_FIXED_WIDTH) add(m.index, `${m[0]} is wider than ${MAX_FIXED_WIDTH}`);
  }
  for (const m of text.matchAll(SCALE_WIDTH)) {
    if (Number(m[1]) * 4 > MAX_FIXED_WIDTH) add(m.index, `${m[0]} is wider than ${MAX_FIXED_WIDTH}`);
  }
  if (/(^|\/)src\/features\/.*Screen\.tsx$/.test(path) && !/\bScreenScroll\b/.test(text)) {
    found.push(`${path} is a screen that does not use ScreenScroll`);
  }
  return found;
}

/** @param {Array<{ path: string; text: string }>} files @param {readonly string[]} [allow] */
export function findLayoutViolations(files, allow = LAYOUT_ALLOW) {
  return files.filter((f) => !allow.includes(f.path)).flatMap((f) => scan(f.path, f.text));
}

function walk(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) return entry.name === 'testing' ? [] : walk(full);
    return /\.(ts|tsx)$/.test(entry.name) && !/\.test\./.test(entry.name) ? [full] : [];
  });
}

/** Every checked source file, with paths relative to apps/mobile (`src/...`). */
export function readRealFiles() {
  return walk(SRC).map((full) => ({
    path: relative(join(root, 'apps', 'mobile'), full).split(sep).join('/'),
    text: readFileSync(full, 'utf8'),
  }));
}

const isCli = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href;
if (isCli) {
  const problems = findLayoutViolations(readRealFiles());
  if (problems.length) {
    console.error(`layout check failed:\n${problems.map((p) => `  - ${p}`).join('\n')}`);
    process.exit(1);
  }
  console.log('check-mobile-layout.mjs: no layout violations.');
}
