// Guards the mobile layout rules of M2B-52: no horizontal scrolling, no fixed width wider than
// the narrowest window minus both gutters, and every feature screen scrolls through ScreenScroll.
// This is the static half. The device pass (360x640 and a large phone at font scale 1.3) is the real
// overflow and clipping check.
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join, relative, sep } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const SRC = join(root, 'apps', 'mobile', 'src');

/** 360 dp window minus two 16 dp gutters; mirrors MAX_FIXED_WIDTH in apps/mobile/src/design/layout.ts. */
const MAX_FIXED_WIDTH = 328;
const REM = 16;

/** Files exempt from every rule. Each entry needs a reason and an owner for removing it. */
export const LAYOUT_ALLOW = [
  // M1 Welcome uses a raw ScrollView. Task 11 rewrites it onto ScreenScroll and removes this entry (PF12).
  'src/features/auth/WelcomeScreen.tsx',
];

const STYLE_WIDTH = /\b(width|minWidth|maxWidth)\s*:\s*(\d+(?:\.\d+)?)\b/g;
const ARBITRARY_WIDTH = /(?<![\w-])(?:min-w|w)-\[(\d+(?:\.\d+)?)(px|rem)\]/g;
const SCALE_WIDTH = /(?<![\w[-])(?:min-w|w)-(\d+(?:\.\d+)?)(?![\w%\]/-])/g;

/** react-native components that scroll; Animated.* variants and `import { X as Y }` aliases are added per file. */
const SCROLLERS = ['ScrollView', 'FlatList', 'SectionList', 'FlashList', 'VirtualizedList'];

function stripComments(text) {
  return text
    .replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, ' '))
    .replace(/(^|\s)\/\/.*$/gm, '$1');
}

function lineOf(text, index) {
  return text.slice(0, index).split('\n').length;
}

/**
 * Reads a JSX opening tag from `start` to its closing `>`, counting braces so an arrow function or JSX inside a
 * prop does not end it early. Returns the depth-0 text with each `{...}` group collapsed to `{false}`, `{...}`
 * (a spread) or `{x}`.
 */
function readTag(text, start) {
  let depth = 0;
  let group = '';
  let out = '';
  let quote = '';
  for (let i = start; i < text.length; i += 1) {
    const ch = text[i];
    if (depth === 0) {
      if (quote) {
        if (ch === quote) quote = '';
        out += ch;
      } else if (ch === '"' || ch === "'") {
        quote = ch;
        out += ch;
      } else if (ch === '{') {
        depth = 1;
        group = '';
      } else if (ch === '>') {
        return out;
      } else out += ch;
    } else if (ch === '{') {
      depth += 1;
      group += ch;
    } else if (ch === '}') {
      depth -= 1;
      if (depth === 0) {
        const inner = group.trim();
        out += inner.startsWith('...') ? '{...}' : inner === 'false' ? '{false}' : '{x}';
      } else group += ch;
    } else group += ch;
  }
  return out;
}

function scrollerNames(text) {
  const names = new Set(SCROLLERS.flatMap((n) => [n, `Animated.${n}`]));
  for (const m of text.matchAll(/import\s*\{([^}]*)\}\s*from\s*['"]react-native['"]/g)) {
    for (const part of m[1].split(',')) {
      const [orig, alias] = part.trim().split(/\s+as\s+/);
      if (alias && SCROLLERS.includes(orig)) names.add(alias);
    }
  }
  return names;
}

function scan(path, raw) {
  const text = stripComments(raw);
  const found = [];
  const add = (index, message) => found.push(`${path}:${lineOf(text, index)} ${message}`);

  // A `horizontal` prop (or a spread that could carry one) on a scroll view or list.
  const names = scrollerNames(text);
  for (const m of text.matchAll(/<([A-Za-z][\w.]*)(?=[\s/>])/g)) {
    if (!names.has(m[1])) continue;
    const attrs = readTag(text, m.index + m[0].length);
    if (/(^|\s)horizontal(?!\s*=\s*(\{false\}|"false"|'false'))(?=[\s=/]|$)/.test(attrs)) {
      add(m.index, 'horizontal scroll view or list (nothing may scroll sideways)');
    } else if (attrs.includes('{...}')) {
      add(m.index, 'spread props on a scroll view or list could carry horizontal; pass props explicitly');
    }
  }
  for (const m of text.matchAll(STYLE_WIDTH)) {
    if (Number(m[2]) > MAX_FIXED_WIDTH) add(m.index, `${m[1]}: ${m[2]} is wider than ${MAX_FIXED_WIDTH}`);
  }
  for (const m of text.matchAll(ARBITRARY_WIDTH)) {
    if (Number(m[1]) * (m[2] === 'rem' ? REM : 1) > MAX_FIXED_WIDTH) add(m.index, `${m[0]} is wider than ${MAX_FIXED_WIDTH}`);
  }
  for (const m of text.matchAll(SCALE_WIDTH)) {
    if (Number(m[1]) * 4 > MAX_FIXED_WIDTH) add(m.index, `${m[0]} is wider than ${MAX_FIXED_WIDTH}`);
  }
  // A screen must really use ScreenScroll: an import and a <ScreenScroll element, not a comment or an unused import.
  const imported = /import\s*\{[^}]*\bScreenScroll\b[^}]*\}\s*from/.test(text);
  if (/(^|\/)src\/features\/.*Screen\.tsx$/.test(path) && !(imported && /<ScreenScroll\b/.test(text))) {
    found.push(`${path} is a screen that does not render ScreenScroll`);
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
