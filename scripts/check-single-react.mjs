#!/usr/bin/env node
// Checks that the whole workspace resolves to exactly one copy of react and
// react-dom, at the pinned version.
//
//   node scripts/check-single-react.mjs     (or: npm run check:react)
//
// Exits 1 on a second version or a missing package.

import { execFileSync } from 'node:child_process';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..');

export const EXPECTED = { react: '19.3.0', 'react-dom': '19.3.0' };

/**
 * Unique, sorted versions of `name` anywhere in an `npm ls --all --json` tree.
 * Deduped and overridden entries count as their version; entries without a
 * version (an unmet optional peer, for example) are not copies and are skipped.
 */
export function collectVersions(tree, name) {
  const versions = new Set();
  const walk = (node) => {
    for (const [depName, dep] of Object.entries(node?.dependencies ?? {})) {
      if (depName === name && typeof dep?.version === 'string') versions.add(dep.version);
      walk(dep);
    }
  };
  walk(tree);
  return [...versions].sort();
}

/** Human-readable problems; empty means every package resolves to its pin. */
export function checkSingleReact(tree, expected = EXPECTED) {
  const problems = [];
  for (const [name, version] of Object.entries(expected)) {
    const found = collectVersions(tree, name);
    if (found.length === 0) problems.push(`${name}: not found`);
    else if (found.length !== 1 || found[0] !== version) {
      problems.push(`${name}: expected only ${version}, found ${found.join(', ')}`);
    }
  }
  return problems;
}

function readTree() {
  const args = ['ls', ...Object.keys(EXPECTED), '--all', '--json'];
  const options = { cwd: repoRoot, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 };
  try {
    // npm.cmd on Windows can only be spawned through a shell.
    return execFileSync('npm', args, { ...options, shell: process.platform === 'win32' });
  } catch (error) {
    // npm ls exits non-zero on peer warnings but still prints the tree.
    if (error.stdout) return error.stdout;
    throw error;
  }
}

const isCli = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href;
if (isCli) {
  const problems = checkSingleReact(JSON.parse(readTree()));
  if (problems.length) {
    console.error(`single React check failed:\n${problems.map((p) => `  - ${p}`).join('\n')}`);
    process.exit(1);
  }
  console.log(Object.entries(EXPECTED).map(([name, version]) => `${name} ${version} ✓`).join(' '));
}
