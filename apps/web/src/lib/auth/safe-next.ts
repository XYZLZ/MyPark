const ORIGIN = 'http://same-origin.invalid';
const MAX_DECODE_PASSES = 3;

// Backslash, control characters and whitespace: browsers strip or rewrite them
// when parsing a URL, so `/\t/evil` or `/\evil` can become `//evil`.
const UNSAFE = /[\\\u0000- \u007f]/;

function isLocalPath(value: string): boolean {
  return value.startsWith('/') && !value.startsWith('//') && !UNSAFE.test(value);
}

/**
 * Returns `next` only when it is a path on this origin, otherwise null (the
 * caller falls back to the role's home page). Every percent-decoded form is
 * checked too, so `/%5Cevil.example` and `/%2F%2Fevil.example` are refused.
 */
export function safeNext(next: string | null | undefined): string | null {
  if (!next) return null;

  let current = next;
  for (let pass = 0; pass <= MAX_DECODE_PASSES; pass++) {
    if (!isLocalPath(current)) return null;
    let decoded: string;
    try {
      decoded = decodeURIComponent(current);
    } catch {
      return null;
    }
    if (decoded === current) break;
    current = decoded;
  }

  // Still changing after the last pass: more encoding layers than any real link has.
  try {
    if (decodeURIComponent(current) !== current) return null;
    return new URL(next, ORIGIN).origin === ORIGIN ? next : null;
  } catch {
    return null;
  }
}
