import assert from 'node:assert/strict';
import { test } from 'node:test';
import { checkTokens, contrastRatio, MUST_FAIL, parseCssTokens, parseTsTokens, readRealFiles } from './check-design-tokens.mjs';

test('parseCssTokens reads --color, --radius and --motion entries from @theme', () => {
  const css = `
    /* --color-ignored: #000000; */
    @theme {
      --color-brand-primary: #1C4A78;
      --radius-control: 12px;
      --motion-ease: cubic-bezier(.22, 1, .36, 1);
      --font-sans: Barlow;
    }`;
  const tokens = parseCssTokens(css);
  assert.deepEqual(
    [...tokens],
    [
      ['brand-primary', '#1c4a78'],
      ['radius-control', '12px'],
      ['motion-ease', 'cubic-bezier(.22, 1, .36, 1)'],
    ],
  );
});

test('parseTsTokens maps camelCase keys to kebab token names', () => {
  const ts = `export const tokens = {
    /* TODO(brand): x */
    brandActionPress: '#1196d7',
    ink: '#14233d',
    radiusControl: '12px',
    motionEase: 'cubic-bezier(.22, 1, .36, 1)',
  } as const;`;
  const tokens = parseTsTokens(ts);
  assert.equal(tokens.get('brand-action-press'), '#1196d7');
  assert.equal(tokens.get('ink'), '#14233d');
  assert.equal(tokens.get('radius-control'), '12px');
  assert.equal(tokens.get('motion-ease'), 'cubic-bezier(.22, 1, .36, 1)');
  assert.equal(tokens.size, 4);
});

test('contrastRatio matches the documented values', () => {
  const round = (n) => Math.round(n * 100) / 100;
  assert.equal(round(contrastRatio('#0e2747', '#1196d7')), 4.55);
  assert.equal(round(contrastRatio('#0e2747', '#1095d6')), 4.49);
  assert.equal(round(contrastRatio('#ffffff', '#1ba7ea')), 2.71);
  assert.equal(contrastRatio('#fff', '#ffffff'), 1);
});

test('contrastRatio matches the design 9.1b values', () => {
  const round = (n) => Math.round(n * 100) / 100;
  assert.equal(round(contrastRatio('#ffffff', '#0b3a75')), 11.19);
  assert.equal(round(contrastRatio('#c4610a', '#e3e8ef')), 3.37);
  assert.equal(round(contrastRatio('#f39a1e', '#ffffff')), 2.22);
  assert.equal(round(contrastRatio('#1e7a46', '#163b63')), 2.13);
});

function realFiles() {
  return readRealFiles();
}

test('checkTokens reports a value that differs between web and mobile', () => {
  const files = realFiles();
  files.mobileCss = files.mobileCss.replace('--color-danger: #b3261e', '--color-danger: #b3261f');
  const problems = checkTokens(files);
  assert.ok(problems.some((p) => p.includes('danger') && p.includes('mobile global.css')), problems.join('\n'));
});

test('checkTokens reports a token missing from tokens.ts', () => {
  const files = realFiles();
  files.mobileTs = files.mobileTs.replace(/^\s*hairline:.*\n/m, '');
  const problems = checkTokens(files);
  assert.ok(problems.some((p) => p.includes('hairline') && p.includes('tokens.ts')), problems.join('\n'));
});

test('checkTokens flags #0b3a7e left in app.json', () => {
  const files = realFiles();
  files.appJson = files.appJson.replace('"#163b63"', '"#0b3a7e"');
  const problems = checkTokens(files);
  assert.ok(problems.some((p) => p.includes('#0b3a7e')), problems.join('\n'));
});

test('checkTokens flags a required pair below its threshold', () => {
  const files = realFiles();
  // Same value in all three files, so only the contrast rule can object.
  for (const key of ['webCss', 'mobileCss', 'mobileTs']) files[key] = files[key].replaceAll('#1196d7', '#1095d6');
  const problems = checkTokens(files);
  assert.equal(problems.length, 1, problems.join('\n'));
  assert.match(problems[0], /brand-action-contrast on brand-action-press/);
  assert.match(problems[0], /4\.49/);
});

test('checkTokens reports a new token missing from global.css', () => {
  const files = realFiles();
  files.mobileCss = files.mobileCss.replace(/--color-space-occupied:\s*[^;]+;/, '');
  const problems = checkTokens(files);
  assert.ok(problems.some((p) => p.includes('"space-occupied"') && p.includes('mobile global.css')), problems.join('\n'));
});

test('checkTokens flags brand-orange if it ever passes as text', () => {
  assert.ok(MUST_FAIL.some(([fg, bg]) => fg === 'brand-orange' && bg === 'surface'));
  const files = realFiles();
  // Same value everywhere, so only the contrast rule can object.
  for (const key of ['webCss', 'mobileCss', 'mobileTs']) files[key] = files[key].replaceAll('#f39a1e', '#7a4a00');
  const problems = checkTokens(files);
  assert.ok(problems.some((p) => p.includes('brand-orange on surface')), problems.join('\n'));
});

test('checkTokens passes on the real repo', () => {
  assert.deepEqual(checkTokens(), []);
});
