// Side-effect stylesheet imports (src/global.css), compiled by Uniwind through Metro.
declare module '*.css';
// The one Node module a test reaches for (format.test.ts); the app has no @types/node.
declare module 'node:vm' {
  export function runInThisContext(code: string): unknown;
}
